// Package editor provides a terminal text editor component inspired by
// bubbletea's textarea, designed for use with diff-based rendering.
package editor

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/component/buffer"
	"github.com/tinyclue/tinyclue-code/tui/component/widget"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	uv "github.com/charmbracelet/ultraviolet"
)

const (
	pasteThreshold          = 800   // 粘贴字符数超过即折叠
	truncationThreshold     = 10000 // 输入总长超过即截断中间
	truncationPreviewLength = 1000  // 截断保留的首尾字符数（各一半）
)

// Editor 是一个多行文本编辑器组件。核心编辑和光标能力由嵌入的 TextBuffer 提供，
// Editor 在其之上添加粘贴折叠和组件接口。
type Editor struct {
	*buffer.TextBuffer
	width              int              // content area width in columns
	promptWidget       component.Widget // 输入区前缀挂件（默认 InputPromptWidget），仅渲染层生效，不进入缓冲区内容
	modified           bool             // whether the buffer has been modified since last reset
	pastes             map[int]string   // collapsed paste content, keyed by paste seq (id)
	pasteSeq           int              // next paste sequence number
	onSubmit           func(text string)
	activeAutoComplete bool
	history            *HistoryStore // 提交历史存储（nil 时不启用翻历史）
	historyIndex       int           // 历史导航游标：0 = 草稿态；1 = 最新历史条目
	historyDraft       historyDraft  // 首次上键时保存的当前输入草稿（保留折叠标记）
	*component.ComponentBase
}

// historyDraft 保存首次上键时的输入：Display 为缓冲原文（含折叠标记），Pastes 为对应粘贴内容（按 id 键控）。
type historyDraft struct {
	display string
	pastes  map[int]string
}

func (e *Editor) SetOnSubmit(onSubmit func(text string)) {
	e.onSubmit = onSubmit
}

func (e *Editor) DoOnSubmit(text string) {
	if e.onSubmit != nil {
		if strings.TrimSpace(text) != "" {
			// 历史记录：若缓冲内容（展开后）即本次提交文本，则连同缓冲里的粘贴折叠标记一起入历史，
			// 导航恢复时仍显示折叠标记；autocomplete/外部直接调用无对应缓冲则只记提交文本。
			if e.history != nil {
				if raw := e.TextBuffer.Text(); raw != text && e.expandPasteMarkers(raw) == text {
					e.history.Add(raw, e.pastes)
				} else {
					e.history.Add(text, nil)
				}
			}
			e.onSubmit(text)
			e.SetText("")
		}
	}
}

func (e *Editor) ActiveAutoComplete(state bool) {
	e.activeAutoComplete = state
}

// New creates a new editor with default settings.
func New() *Editor {
	e := &Editor{
		TextBuffer:    buffer.New(),
		width:         80,
		promptWidget:  &widget.InputPromptWidget{},
		ComponentBase: component.NewComponentBase(),
	}
	return e
}

// SetText 设置缓冲区内容并重置编辑/粘贴/历史导航状态。
// 历史导航自身写入须走 setTextWithHistory（保留 historyIndex），避免把导航态清掉。
func (e *Editor) SetText(s string) {
	e.TextBuffer.SetText(s)
	e.resetEditState()
	e.resetHistory()
}

// SetHistoryStore 注入提交历史存储（交互层接线，nil 时不启用翻历史）。
func (e *Editor) SetHistoryStore(h *HistoryStore) {
	e.history = h
}

// resetEditState 重置编辑与粘贴状态（modified/pastes/pasteSeq）。
func (e *Editor) resetEditState() {
	e.modified = false
	e.pastes = nil
	e.pasteSeq = 0
}

// resetHistory 复位历史导航：回到草稿态。
func (e *Editor) resetHistory() {
	e.historyIndex = 0
	e.historyDraft = historyDraft{}
}

// setTextWithHistory 历史导航写入：重置编辑/粘贴状态，但保留 historyIndex
// （导航态由 historyUp/historyDown 维护，若走 SetText 会把自己复位）。
// display 为原始缓冲文本（含折叠标记），pastes 按 id 键控对应内容，一并还原，
// 使恢复后仍显示折叠标记、且 e.Text() 能正确展开；id 缺失则展开时保留字面量。
func (e *Editor) setTextWithHistory(display string, pastes map[int]string) {
	e.TextBuffer.SetText(display)
	e.modified = false
	e.pastes = clonePastes(pastes)
	e.pasteSeq = maxPasteID(pastes)
}

// maxPasteID 返回 pastes 中最大的粘贴 id，作为下一个折叠/截断序号的基准（空表为 0）。
func maxPasteID(pastes map[int]string) int {
	max := 0
	for id := range pastes {
		if id > max {
			max = id
		}
	}
	return max
}

// upOrHistoryUp 先尝试上移光标（多行/折行内容内移动）。到达顶部视觉行后，
// 若光标不在文本开头（行首），先把光标归位到开头，已在开头才翻历史。
func (e *Editor) upOrHistoryUp() {
	if e.TextBuffer.CursorUp(e.innerWidth()) {
		return
	}
	if e.TextBuffer.CursorRow > 0 || e.TextBuffer.CursorCol > 0 {
		e.TextBuffer.MoveToBegin()
		return
	}
	e.historyUp()
}

// downOrHistoryDown 先尝试下移光标。到达底部视觉行后，
// 若光标不在文本末尾（行尾），先把光标归位到末尾，已在末尾才翻历史。
func (e *Editor) downOrHistoryDown() {
	if e.TextBuffer.CursorDown(e.innerWidth()) {
		return
	}
	last := len(e.TextBuffer.Lines) - 1
	if e.TextBuffer.CursorRow < last || e.TextBuffer.CursorCol < len(e.TextBuffer.Lines[last]) {
		e.TextBuffer.MoveToEnd()
		return
	}
	e.historyDown()
}

// historyUp 上翻一条历史：首次上键先保存当前草稿供下键恢复；已到最旧则回滚、草稿不动。
func (e *Editor) historyUp() {
	if e.history == nil {
		return
	}
	entries := e.history.List()
	if e.historyIndex == 0 {
		if len(entries) == 0 {
			return // 无历史可翻，草稿不动
		}
		// 存原始缓冲文本（含折叠标记）+ 对应粘贴内容，恢复草稿时仍显示折叠标记。
		e.historyDraft = historyDraft{
			display: e.TextBuffer.Text(),
			pastes:  clonePastes(e.pastes),
		}
	}
	if e.historyIndex >= len(entries) {
		return // 已到最旧一条，草稿不动
	}
	e.historyIndex++
	entry := entries[e.historyIndex-1]
	e.setTextWithHistory(entry.Display, entry.Pastes)
	e.TextBuffer.MoveToBegin()
}

// historyDown 下翻一条历史：回到草稿态时恢复首次上键保存的草稿；光标移到末尾。
func (e *Editor) historyDown() {
	if e.historyIndex <= 0 {
		return // 已在草稿态
	}
	e.historyIndex--
	if e.historyIndex == 0 {
		e.setTextWithHistory(e.historyDraft.display, e.historyDraft.pastes) // 恢复草稿
	} else {
		entry := e.history.List()[e.historyIndex-1]
		e.setTextWithHistory(entry.Display, entry.Pastes)
	}
	e.TextBuffer.MoveToEnd()
}

// Text 返回缓冲区全文，粘贴标记会被展开为实际内容。
func (e *Editor) Text() string {
	return e.expandPasteMarkers(e.TextBuffer.Text())
}

// Height 返回当前宽度下所有内容占用的总视觉行数（按扣除前缀后的内部宽度计算）。
func (e *Editor) Height() int {
	return e.TextBuffer.Height(e.innerWidth())
}

// innerWidth 返回扣除前缀挂件（挂件 + 尾部空格）后文本的实际可用宽度。
// 文本在此宽度内换行，保证前置前缀后整行恰好等于框宽，避免折行溢出。
func (e *Editor) innerWidth() int {
	w := e.width - e.promptPrefixWidth()
	if w < 1 {
		w = 1
	}
	return w
}

// promptPrefixWidth 返回前缀挂件及其尾部空格的总视觉宽度。
func (e *Editor) promptPrefixWidth() int {
	if e.promptWidget == nil {
		return 0
	}
	return e.promptWidget.Width() + 1 // 挂件 + 尾部空格
}

// 返回所有逻辑行按内部宽度（扣除前缀）换行后的视觉行。粘贴标记原文显示
// （如 [paste #1 +5 lines]），展开仅在提交时通过 Text() 进行。
func (e *Editor) View() []string {
	return e.TextBuffer.View(e.innerWidth())
}

func (e *Editor) DoBefore(data core.Data) error {
	e.width = terminal.DefaultTerminalContext.GetWidth()
	if e.width < 10 {
		e.width = 10
	}
	return nil
}

// ── Update Entry Point ──

// DoUpdate 处理输入事件，内部完成所有键盘到编辑操作的映射。
func (e *Editor) DoUpdate(data core.Data) error {
	if e.activeAutoComplete {
		return nil
	}
	switch m := data.Msg.(type) {
	case core.InterruptMsg:
		e.SetText("")
	case core.KeyPressMsg:
		e.handleKey(m.Key())
	case core.PasteMsg:
		text := m.String()
		r := processPaste(text)
		rows := terminal.DefaultTerminalContext.GetHeight()
		if rows <= 0 {
			rows = 24
		}
		if r.large(maxLinesForRows(rows)) {
			e.collapsePaste(r)
		} else {
			// processPaste 已完成清理（换行/制表符/控制字符），此处原样插入，保留多行粘贴的换行。
			for _, ch := range r.clean {
				e.InsertRune(ch)
			}
		}
	}
	e.maybeTruncate()
	return nil
}

func (e *Editor) handleKey(key uv.Key) {
	switch {
	case key.MatchString("backspace"):
		e.Backspace()
	case key.MatchString("enter"):
		e.DoOnSubmit(e.Text())
	case key.MatchString("delete"):
		e.Delete()
	case key.MatchString("tab"):
		e.InsertRune('\t')
	case key.MatchString("up", "ctrl+p"):
		e.upOrHistoryUp()
	case key.MatchString("down", "ctrl+n"):
		e.downOrHistoryDown()
	case key.MatchString("left", "ctrl+b"):
		e.CursorLeft()
	case key.MatchString("right", "ctrl+f"):
		e.CursorRight()
	case key.MatchString("home", "ctrl+a"):
		e.LineStart()
	case key.MatchString("end", "ctrl+e"):
		e.LineEnd()
	case key.MatchString("pgup"):
		e.TextBuffer.PageUp(e.innerWidth())
	case key.MatchString("pgdown"):
		e.TextBuffer.PageDown(e.innerWidth())
	case key.MatchString("ctrl+k"):
		e.DeleteToLineEnd()
	case key.MatchString("ctrl+u"):
		e.DeleteToLineStart()
	case key.MatchString("ctrl+w"):
		e.DeleteWordLeft()
	case key.MatchString("ctrl+d"):
		e.Delete()
	case key.MatchString("alt+b", "alt+left"):
		e.WordLeft()
	case key.MatchString("alt+f", "alt+right"):
		e.WordRight()
	case key.MatchString("alt+backspace"):
		e.DeleteWordLeft()
	case key.MatchString("alt+d"):
		e.DeleteWordRight()
	default:
		if key.Text != "" {
			for _, r := range key.Text {
				if !unicode.IsControl(r) {
					e.InsertRune(r)
				}
			}
		}
	}
}

// ── Render ──

func (e *Editor) Render(data core.Data) core.View {
	e.width = terminal.DefaultTerminalContext.GetWidth()
	if e.width < 10 {
		e.width = 10
	}
	innerWidth := e.innerWidth()
	visualLines := e.TextBuffer.View(innerWidth)
	e.applyCursor(visualLines, innerWidth)
	e.applyPrompt(visualLines)

	div := types.FgGray + strings.Repeat("─", e.width) + types.Reset
	lines := make([]string, 0, len(visualLines)+2)
	lines = append(lines, div)
	lines = append(lines, visualLines...)
	lines = append(lines, div)
	return core.View{Lines: lines}
}

// applyCursor 在 visualLines 的光标位置插入 CURSOR_MARKER 和反转色假光标。
// 非焦点组件不会插入任何标记。width 为文本内部宽度（已扣除前缀）。
func (e *Editor) applyCursor(visualLines []string, width int) {
	if !e.IsFocused() {
		return
	}
	row := e.TextBuffer.CursorViewRow(width)
	if row >= len(visualLines) {
		return
	}
	line := visualLines[row]
	runes := []rune(line)
	idx := e.RuneIndexAt(runes, e.TextBuffer.CursorViewCol(width))

	before := string(runes[:idx])
	after := string(runes[idx:])
	if after != "" {
		r := []rune(after)
		cursor := types.CursorStyle + string(r[0]) + types.Reset
		visualLines[row] = before + core.CURSOR_MARKER + cursor + string(r[1:])
	} else {
		visualLines[row] = before + core.CURSOR_MARKER + types.CursorStyle + " " + types.Reset
	}
}

// applyPrompt 给每个视觉行前置前缀挂件：首行挂件 + 尾部空格，续行用等宽空格对齐，
// 保证折行后文本左缘对齐。前缀仅在渲染层生效，不进入缓冲区内容。
func (e *Editor) applyPrompt(visualLines []string) {
	if e.promptWidget == nil {
		return
	}
	align := strings.Repeat(" ", e.promptPrefixWidth())
	for i, l := range visualLines {
		if i == 0 {
			visualLines[i] = e.promptWidget.Render() + " " + l
		} else {
			visualLines[i] = align + l
		}
	}
}

// ── Paste Collapse ──

// [Pasted text #N] / [Pasted text #N +M lines] / [...Truncated text #N +M lines...]。
var pasteRefPattern = regexp.MustCompile(`\[(?:Pasted text|\.\.\.Truncated text) #(\d+)(?: \+\d+ lines)?\.*\]`)

type pasteResult struct {
	clean    string
	numLines int
	chars    int
}

func (r pasteResult) large(maxLines int) bool {
	return r.chars > pasteThreshold || r.numLines > maxLines
}

// processPaste 清理粘贴文本（CRLF/制表符/控制字符）并统计换行数与字符数。
func processPaste(text string) pasteResult {
	s := strings.ReplaceAll(text, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", "    ")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r >= 32 {
			b.WriteRune(r)
		}
	}
	clean := b.String()

	return pasteResult{
		clean:    clean,
		numLines: strings.Count(clean, "\n"),
		chars:    utf8.RuneCountInString(clean),
	}
}

func formatPastedTextRef(id, numLines int) string {
	if numLines == 0 {
		return fmt.Sprintf("[Pasted text #%d]", id)
	}
	return fmt.Sprintf("[Pasted text #%d +%d lines]", id, numLines)
}

func formatTruncatedTextRef(id, numLines int) string {
	return fmt.Sprintf("[...Truncated text #%d +%d lines...]", id, numLines)
}

func maxLinesForRows(rows int) int {
	ml := rows - 10
	if ml > 2 {
		ml = 2
	}
	if ml < 1 {
		ml = 1
	}
	return ml
}

func (e *Editor) collapsePaste(r pasteResult) {
	e.pasteSeq++
	if e.pastes == nil {
		e.pastes = make(map[int]string)
	}
	e.pastes[e.pasteSeq] = r.clean
	marker := formatPastedTextRef(e.pasteSeq, r.numLines)
	for _, r := range marker {
		e.InsertRune(r)
	}
}

// 保留首尾各 500 字符，中间折叠成截断标记并存入 pastes。不复位导航态。
func (e *Editor) maybeTruncate() {
	raw := e.TextBuffer.Text()
	if len(raw) <= truncationThreshold {
		return
	}
	runes := []rune(raw)
	preview := truncationPreviewLength / 2
	middle := string(runes[preview : len(runes)-preview])

	e.pasteSeq++
	if e.pastes == nil {
		e.pastes = make(map[int]string)
	}
	e.pastes[e.pasteSeq] = middle
	ref := formatTruncatedTextRef(e.pasteSeq, strings.Count(middle, "\n"))

	e.TextBuffer.SetText(string(runes[:preview]) + ref + string(runes[len(runes)-preview:]))
	e.modified = false
	e.TextBuffer.MoveToEnd()
}

// expandPasteMarkers 把缓冲中的折叠标记展开为实际粘贴内容。id 缺失（如哈希外置文件丢失）
func (e *Editor) expandPasteMarkers(s string) string {
	matches := pasteRefPattern.FindAllStringSubmatchIndex(s, -1)
	if len(matches) == 0 {
		return s
	}
	var result strings.Builder
	last := 0
	for _, m := range matches {
		start, end := m[0], m[1]       // 完整标记边界
		seqStart, seqEnd := m[2], m[3] // 分组 1：# 后的序号数字
		result.WriteString(s[last:start])
		seq, err := strconv.Atoi(s[seqStart:seqEnd])
		if content, ok := e.pastes[seq]; err == nil && ok {
			result.WriteString(content)
		} else {
			result.WriteString(s[start:end])
		}
		last = end
	}
	result.WriteString(s[last:])
	return result.String()
}
