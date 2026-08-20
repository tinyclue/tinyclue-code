// Package renderer 封装 TUI 的差分渲染与硬件光标定位。
//
// 的渲染引擎。相比 v1/v2-物理行模型的根本差异：
//
//  1. 1 行 = 1 物理行：不再维护 computeLayout/physRows 物理行折算。组件负责把每行
//     折到 ≤ 终端列宽（见 types.WrapTextWithAnsi），渲染器只做行字符串 diff。
//  2. 行分隔模型：行间 \r\n、最后一行后无尾随换行 → 隐藏硬件光标停最后内容行。
//  3. 双坐标模型：cursorRow=内容底行（视口计算），hardwareCursorRow=硬件光标实际行。
//  4. positionHardwareCursor：每次渲染后（含无变更帧）把硬件光标相对移到 CURSOR_MARKER
//     所在输入行，并用 \x1b[col+1]G 绝对列定位 —— IME 候选窗锚定在输入框光标处。
//  5. 屏幕相对位移 computeLineDiff = (targetRow - viewportTop) - (hwRow - prevViewportTop)。
//  6. 行尾 SEGMENT_RESET（\x1b[0m\x1b]8;;\x07）隔离未闭合样式/超链接。
//  7. CSI 2026 同步输出包裹每次渲染；清屏只发生在 resize/上方布局变化/clearOnShrink。
//  8. 宽度溢出 crash guard：行宽 > 列宽时记日志 + 截断（pi 是 throw，我们为稳定性改为截断）。
package renderer

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// ── ANSI 控制序列（v2）──

const (
	ansiSyncBegin   = "\x1b[?2026h"          // CSI 2026 同步输出开始
	ansiSyncEnd     = "\x1b[?2026l"          // CSI 2026 同步输出结束
	ansiClearScreen = "\x1b[2J\x1b[H\x1b[3J" // 清屏 + 归位 + 清 scrollback
	ansiClearLine2K = "\x1b[2K"              // 清除整行
	ansiMoveToCol   = "\x1b[%dG"             // 绝对列定位（1 起）
	ansiCursorUp    = "\033[A"
	ansiCursorDown  = "\033[B"
	ansiShowCursor  = "\033[?25h"
	ansiHideCursor  = "\033[?25l"
)

// ── RendererV2 ──

// RendererV2 封装差分渲染与硬件光标定位。与 v1 Renderer 不同：
//   - 1 行 = 1 物理行，行间 \r\n、无尾随换行，光标停最后内容行；
//   - 双坐标模型（内容底行 / 硬件光标实际行）；
//   - 每次渲染后 positionHardwareCursor 把硬件光标移到输入行（IME 修复）；
//   - 屏幕相对位移，viewportTop 仅在 commit 时按底部锚定重算。
type RendererV2 struct {
	cols              int      // 终端列数
	termHeight        int      // 终端行数
	prev              []string // 上一帧（已归一化 + 行尾 SEGMENT_RESET）
	cursorRow         int      // 帧内 CURSOR_MARKER 逻辑行（供测试断言 / CursorPosition）
	cursorCol         int      // 帧内 CURSOR_MARKER 逻辑列
	hardwareCursorRow int      // 硬件光标当前帧行（双坐标模型的硬件坐标）
	prevViewportTop   int      // 上一帧可视区顶部（渲染开始时即此值）
	maxLen            int      // 会话最大行数（clearOnShrink 依据）
	hasRendered       bool
	forceRedraw       bool
	widthChanged      bool // SetCols 检测到列宽变化
	heightChanged     bool // SetTermHeight 检测到高度变化
	clearOnShrink     bool // 是否在帧变矮时全量清屏重绘（默认关，pi 对齐；TINYCLUE_CLEAR_ON_SHRINK=1 开）
	renderViewportTop int  // 本次渲染实际采用的 viewportTop（diff 经 scroll-by-print 递增），commit 用
}

// NewV2 创建一个 RendererV2。
func NewV2() *RendererV2 {
	return &RendererV2{
		clearOnShrink: os.Getenv("TINYCLUE_CLEAR_ON_SHRINK") == "1",
	}
}

func (r *RendererV2) SetCols(cols int) {
	if cols != r.cols {
		r.widthChanged = true
		r.cols = cols
	}
}

func (r *RendererV2) SetTermHeight(h int) {
	if h != r.termHeight {
		r.heightChanged = true
		r.termHeight = h
	}
}

// OnResize 响应窗口尺寸变化：清空上一帧状态，下次渲染全量重绘。
func (r *RendererV2) OnResize() {
	r.prev = nil
	r.forceRedraw = true
	r.maxLen = 0
	r.prevViewportTop = 0
	r.hardwareCursorRow = 0
	r.widthChanged = false
	r.heightChanged = false
}

// ClearScreen 执行清屏（ctrl+l）并归位光标，丢弃上一帧，下次渲染作为首帧输出。
func (r *RendererV2) ClearScreen() {
	fmt.Print("\x1b[2J\x1b[H")
	r.prev = nil
	r.hasRendered = false
	r.forceRedraw = false
	r.maxLen = 0
	r.prevViewportTop = 0
	r.hardwareCursorRow = 0
}

// CursorPosition 返回最近一次渲染记录的 CURSOR_MARKER 逻辑行/列，供测试断言使用。
func (r *RendererV2) CursorPosition() (row, col int) { return r.cursorRow, r.cursorCol }

// RestoreCursor 把硬件光标移到帧底部并显示，用于退出前还原终端状态。
// 渲染后硬件光标被 positionHardwareCursor 停在输入行，若直接换行会落在
// 残留内容上（如编辑器底边框），shell 提示符与内容重叠。因此先下移到
// 上一帧内容底部，再换行到全新一行。
func (r *RendererV2) RestoreCursor() {
	if len(r.prev) > 0 {
		lastRow := len(r.prev) - 1
		if d := lastRow - r.hardwareCursorRow; d > 0 {
			cursorDown(d)
		}
	}
	showCursor()
	fmt.Print("\r\n")
}

// Render 渲染一帧：剥光标标记 → 归一化 + 行尾 SEGMENT_RESET → 差分渲染 → 定位硬件光标。
func (r *RendererV2) Render(frame []string) {
	lines := append([]string(nil), frame...)

	// 能力#4：可视区内自底向上提取 CURSOR_MARKER（在行尾归一化之前），返回其逻辑行/列。
	cursorPos := extractCursorPosition(lines, r.termHeight)
	if cursorPos != nil {
		r.cursorRow, r.cursorCol = cursorPos.row, cursorPos.col
	} else {
		r.cursorRow, r.cursorCol = -1, 0
	}

	// 能力#13/#14：归一化（泰/老挝 AM 元音）+ 每行行尾 SEGMENT_RESET。
	lines = normalizeLines(lines)

	hideCursor()
	beginSync()

	strategy := "diff"
	cleared := false
	switch {
	case !r.hasRendered:
		strategy = "first"
		r.fullRender(lines, false)
	case r.widthChanged || r.heightChanged || r.forceRedraw:
		strategy = "resize"
		r.fullRender(lines, true)
		cleared = true
	case r.clearOnShrink && len(lines) < r.maxLen:
		strategy = "shrink"
		r.fullRender(lines, true)
		cleared = true
	default:
		r.diffRender(lines)
	}

	endSync()

	// 能力#6：定位硬件光标到输入行（IME 修复）。在同步输出区外（pi 同）。
	r.positionHardwareCursor(cursorPos, len(lines))

	log.Debug(context.Background(), "render",
		"strategy", strategy,
		"rows", len(lines),
		"viewportTop", r.renderViewportTop,
		"hw", r.hardwareCursorRow,
		"cleared", cleared)
	r.commit(lines)
}

// beginSync / endSync 包裹一次渲染为原子批，终端在 endSync 时才呈现。
func beginSync() { fmt.Print(ansiSyncBegin) }
func endSync()   { fmt.Print(ansiSyncEnd) }

// extractCursorPosition 在可视区 [max(0,len-height), len) 内自底向上扫描 CURSOR_MARKER，
// 剥掉标记并返回其逻辑行/列。标记前的列 = VisualWidth(beforeMarker)。
func extractCursorPosition(lines []string, height int) *cursorPos {
	viewportTop := max(0, len(lines)-height)
	for row := len(lines) - 1; row >= viewportTop; row-- {
		l := lines[row]
		if idx := strings.Index(l, core.CURSOR_MARKER); idx >= 0 {
			col := types.VisualWidth(l[:idx])
			lines[row] = l[:idx] + l[idx+len(core.CURSOR_MARKER):]
			return &cursorPos{row: row, col: col}
		}
	}
	return nil
}

type cursorPos struct{ row, col int }

// normalizeLines 对每行做归一化并追加行尾 SEGMENT_RESET，隔离未闭合的样式/超链接。
func normalizeLines(lines []string) []string {
	res := make([]string, len(lines))
	for i, l := range lines {
		res[i] = types.NormalizeTerminalOutput(l) + types.SegmentReset
	}
	return res
}

// guardLine 能力#11 宽度溢出 crash guard：行宽 > 列宽时记日志并截断到列宽。
// pi 是 throw 停止，我们为稳定性改为截断（绝不让渲染器 panic）。截断会丢行尾
// SEGMENT_RESET，因此补回。所有行的归一化 + SEGMENT_RESET 在 normalizeLines 完成，
// 这里只处理截断。
func (r *RendererV2) guardLine(i int, l string) string {
	if r.cols > 0 && types.VisualWidth(l) > r.cols {
		log.Debug(context.Background(), "render crash guard",
			"line", i,
			"width", types.VisualWidth(l),
			"cols", r.cols)
		return types.TruncateToWidth(l, r.cols, "") + types.SegmentReset
	}
	return l
}

// fullRender 全量重绘整帧。clear=true 时清屏 + 归位 + 清 scrollback。
// 行间 \r\n、最后一行后无尾随换行 → 隐藏硬件光标停最后内容行（newLen-1）。
func (r *RendererV2) fullRender(lines []string, clear bool) {
	if clear {
		fmt.Print(ansiClearScreen)
	}
	for i, l := range lines {
		if i > 0 {
			fmt.Print("\r\n")
		}
		fmt.Print(ansiClearLine2K + r.guardLine(i, l))
	}
	r.hardwareCursorRow = max(0, len(lines)-1)
	r.renderViewportTop = max(0, max(r.termHeight, len(lines))-r.termHeight)
}

// diffRender 差分渲染（对齐 pi doRender 的 diff 路径）。
//
// 不变量：硬件光标的实际帧行由 hardwareCursorRow 精确记录（渲染开始时即上帧
// positionHardwareCursor 的落点，通常是输入行）；所有光标位移是屏幕相对移动
// lineDiff(targetRow) = (targetRow - viewportTop) - (hwRow - prevViewportTop)，
// 同帧坐标系、viewport 自动抵消，无需绝对屏幕行。
func (r *RendererV2) diffRender(lines []string) {
	prevLen, newLen := len(r.prev), len(lines)
	first, last := diffRange(r.prev, lines)

	viewportTop := r.prevViewportTop
	r.renderViewportTop = viewportTop
	hwRow := r.hardwareCursorRow

	lineDiff := func(targetRow int) int {
		return (targetRow - viewportTop) - (hwRow - r.prevViewportTop)
	}

	// 追加内容（旧帧缺行按 "" 比较，diffRange 已处理）：新帧更长时变更区延伸到新帧末尾。
	appended := newLen > prevLen
	if appended {
		if first < 0 {
			first = prevLen // 追加的均为空行，仍视为从旧帧末尾开始变更
		}
		last = newLen - 1
	}
	if first < 0 {
		// 无内容变更：光标已停在输入行，positionHardwareCursor 稍后仍会执行。
		return
	}

	// 帧收缩到可视区窗口之内：当前视口仍停留在旧滚动位置（例如权限面板消失后，
	// 上一帧因超高超宽把视口顶到了底部）。差分只能在屏幕内上/下移动，无法把视口
	// 上滚回弹；若新帧底部锚定视口（max(0,newLen-height)）已跑到当前视口之上，
	// 只能全量清屏重绘，让视口回到新帧的底部锚定位置，避免残留旧滚动内容。
	if newLen < prevLen && max(0, newLen-r.termHeight) < r.prevViewportTop {
		r.fullRender(lines, true)
		return
	}

	// ① 变更全部发生在删除行（first >= newLen）：无内容可重绘，只清除旧帧残留行。
	if first >= newLen {
		if prevLen > newLen {
			targetRow := max(0, newLen-1)
			if targetRow < r.prevViewportTop {
				r.fullRender(lines, true)
				return
			}
			if d := lineDiff(targetRow); d > 0 {
				cursorDown(d)
			} else if d < 0 {
				cursorUp(-d)
			}
			fmt.Print("\r")
			extra := prevLen - newLen
			if extra > r.termHeight {
				r.fullRender(lines, true)
				return
			}
			clearStartOffset := 0
			if newLen > 0 {
				clearStartOffset = 1
			}
			if extra > 0 && clearStartOffset > 0 {
				fmt.Printf("\x1b[%dB", clearStartOffset)
			}
			for i := 0; i < extra; i++ {
				fmt.Print("\r" + ansiClearLine2K)
				if i < extra-1 {
					fmt.Print("\x1b[1B")
				}
			}
			moveBack := max(0, extra-1+clearStartOffset)
			if moveBack > 0 {
				fmt.Printf("\x1b[%dA", moveBack)
			}
			r.hardwareCursorRow = targetRow
		}
		return
	}

	// ② 变更起始行在上一帧可视区之上：无法把光标上移到可视区外，只能全量重绘。
	if first < r.prevViewportTop {
		// 变更整体在可视区之上且帧高度不变 → 可见内容未变，跳过重绘
		//（例如可视区之上的 spinner 滴答，无需浪费一次重绘）。
		if newLen == prevLen && last < r.prevViewportTop {
			return
		}
		r.fullRender(lines, true)
		return
	}

	// ③ 追加内容且变更起点 = 旧帧末尾 → 目标行 = first-1（新行从旧帧末尾下一行开始）。
	moveTargetRow := first
	if appended && first == prevLen && first > 0 {
		moveTargetRow = first - 1
	}

	// ④ scroll-by-print：目标行在上一帧可视区之下 → 先移到底部屏幕行，打印 \r\n 触发滚动。
	prevBottom := r.prevViewportTop + r.termHeight - 1
	if moveTargetRow > prevBottom {
		curScreen := clamp(hwRow-r.prevViewportTop, 0, r.termHeight-1)
		if d := r.termHeight - 1 - curScreen; d > 0 {
			cursorDown(d)
		}
		scroll := moveTargetRow - prevBottom
		for i := 0; i < scroll; i++ {
			fmt.Print("\r\n")
		}
		viewportTop += scroll
		r.renderViewportTop = viewportTop
		hwRow = moveTargetRow
	}

	// ⑤ 移到首变更行，逐行重绘 [first, renderEnd]。
	if d := lineDiff(moveTargetRow); d > 0 {
		cursorDown(d)
	} else if d < 0 {
		cursorUp(-d)
	}
	if appended && first == prevLen && first > 0 {
		fmt.Print("\r\n") // 追加：从旧帧末尾行下方开始写
	} else {
		fmt.Print("\r")
	}
	renderEnd := min(last, newLen-1)
	for i := first; i <= renderEnd; i++ {
		if i > first {
			fmt.Print("\r\n")
		}
		fmt.Print(ansiClearLine2K + r.guardLine(i, lines[i]))
	}
	hwRow = renderEnd

	// ⑥ 旧帧比新帧长：清除残留行并移回新帧底部。
	if prevLen > newLen {
		if renderEnd < newLen-1 {
			cursorDown(newLen - 1 - renderEnd)
			hwRow = newLen - 1
		}
		extra := prevLen - newLen
		for i := 0; i < extra; i++ {
			fmt.Print("\r\n" + ansiClearLine2K)
		}
		fmt.Printf("\x1b[%dA", extra)
		hwRow = newLen - 1
	}

	r.hardwareCursorRow = hwRow
}

// positionHardwareCursor 把硬件光标相对移到 CURSOR_MARKER 所在行，并用绝对列定位，
// 使 IME 候选窗锚定在输入框光标处。之后保持光标隐藏（假光标是文本反转字）。
func (r *RendererV2) positionHardwareCursor(pos *cursorPos, totalLines int) {
	if pos == nil || totalLines <= 0 {
		hideCursor()
		return
	}
	targetRow := clamp(pos.row, 0, totalLines-1)
	targetCol := max(0, pos.col)
	d := targetRow - r.hardwareCursorRow
	if d > 0 {
		cursorDown(d)
	} else if d < 0 {
		cursorUp(-d)
	}
	fmt.Printf(ansiMoveToCol, targetCol+1)
	r.hardwareCursorRow = targetRow
	hideCursor()
}

// commit 提交新帧为当前帧，并按 pi 公式更新可视区顶部（底部锚定）。
func (r *RendererV2) commit(new []string) {
	r.prev = new
	r.prevViewportTop = max(r.renderViewportTop, max(0, len(new)-1)-r.termHeight+1)
	r.maxLen = max(r.maxLen, len(new))
	r.hasRendered = true
	r.forceRedraw = false
	r.widthChanged = false
	r.heightChanged = false
}

func clamp(v, lo, hi int) int { return min(max(v, lo), hi) }

// ── 共享辅助（原 renderer.go，v1 移除后并入 v2）──

func cursorUp(n int)   { fmt.Print(strings.Repeat(ansiCursorUp, n)) }
func cursorDown(n int) { fmt.Print(strings.Repeat(ansiCursorDown, n)) }

func showCursor() { fmt.Print(ansiShowCursor) }
func hideCursor() { fmt.Print(ansiHideCursor) }

// diffRange 返回两帧第一个/最后一个内容不同的逻辑行下标；无差异时 first=-1。
func diffRange(old, new []string) (first, last int) {
	first, last = -1, -1
	n := max(len(old), len(new))
	for i := 0; i < n; i++ {
		var a, b string
		if i < len(old) {
			a = old[i]
		}
		if i < len(new) {
			b = new[i]
		}
		if a != b {
			if first == -1 {
				first = i
			}
			last = i
		}
	}
	return first, last
}
