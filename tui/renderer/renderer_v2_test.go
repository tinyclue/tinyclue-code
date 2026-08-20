package renderer

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// capture 临时接管 os.Stdout，执行 f 并返回其写出的全部字节。
func capture(f func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

// leadingRunLen 返回 out 开头连续 sub 出现的次数。
func leadingRunLen(out, sub string) int {
	n := 0
	for strings.HasPrefix(out, sub) {
		out = out[len(sub):]
		n++
	}
	return n
}

func newTestV2(cols, height int) *RendererV2 {
	r := NewV2()
	r.SetCols(cols)
	r.SetTermHeight(height)
	return r
}

// syncRegion 返回一次渲染中 \x1b[?2026h ... \x1b[?2026l 同步输出区的内容。
func syncRegion(out string) string {
	i := strings.Index(out, ansiSyncBegin)
	j := strings.LastIndex(out, ansiSyncEnd)
	if i < 0 || j < i {
		return out
	}
	return out[i+len(ansiSyncBegin) : j]
}

// ── 最小终端模拟器 ──

// termScreen 解析渲染器输出中的控制序列，在 rows×cols 网格上重建屏幕状态：
//   - \r\n / \r / 可见字符写入网格；
//   - \x1b[nA / \x1b[nB / \x1b[nG 光标移动；
//   - \x1b[2K 整行清除、\x1b[2J 清屏归位；
//   - 光标越过底部时终端滚动；
//   - SGR / OSC 8 / APC / 模式设置（\x1b[?2026h/l、\x1b[?25l）剥离不计。
type termScreen struct {
	rows, cols int
	grid       [][]string
	curRow     int
	curCol     int
	clearCount int
}

func newTermScreen(rows, cols int) *termScreen {
	g := make([][]string, rows)
	for i := range g {
		g[i] = make([]string, cols)
	}
	return &termScreen{rows: rows, cols: cols, grid: g}
}

func (s *termScreen) feed(out string) {
	i := 0
	for i < len(out) {
		if out[i] != '\x1b' {
			switch out[i] {
			case '\r':
				s.curCol = 0
				i++
			case '\n':
				s.moveDown(1)
				s.curCol = 0
				i++
			default:
				r, size := utf8.DecodeRuneInString(out[i:])
				if size == 1 && out[i] < 0x20 && out[i] != '\t' {
					i++ // 其它控制字符忽略
					continue
				}
				s.write(r)
				i += size
			}
			continue
		}
		if i+1 >= len(out) {
			break
		}
		switch out[i+1] {
		case '[':
			j := i + 2
			for j < len(out) && !isLetter(out[j]) {
				j++
			}
			if j >= len(out) {
				i = j
				continue
			}
			param, final := out[i+2:j], out[j]
			s.applyCSI(param, final)
			i = j + 1
		case ']', '_':
			// OSC/APC：跳到 BEL 或 ST
			j := i + 2
			for j < len(out) && out[j] != '\x07' && !(out[j] == '\x1b' && j+1 < len(out) && out[j+1] == '\\') {
				j++
			}
			if j < len(out) && out[j] == '\x07' {
				i = j + 1
			} else if j < len(out) {
				i = j + 2
			} else {
				i = j
			}
		case '\\':
			i += 2 // ST 单独出现
		default:
			i++
		}
	}
}

func (s *termScreen) applyCSI(param string, final byte) {
	switch final {
	case 'A':
		s.curRow = max(0, s.curRow-csiNum(param, 1))
	case 'B':
		s.moveDown(csiNum(param, 1))
	case 'G':
		s.curCol = min(max(csiNum(param, 1)-1, 0), s.cols-1)
	case 'K':
		if strings.HasPrefix(param, "2") {
			for c := 0; c < s.cols; c++ {
				s.grid[s.curRow][c] = ""
			}
		} else {
			for c := s.curCol; c < s.cols; c++ {
				s.grid[s.curRow][c] = ""
			}
		}
	case 'H':
		s.curRow, s.curCol = 0, 0
	case 'J':
		if strings.HasPrefix(param, "2") {
			for r := 0; r < s.rows; r++ {
				for c := 0; c < s.cols; c++ {
					s.grid[r][c] = ""
				}
			}
			s.curRow, s.curCol = 0, 0
			s.clearCount++
		}
		// 3J 清 scrollback：无屏幕效果
	case 'm', 'h', 'l':
		// SGR / 模式设置：忽略
	}
}

func (s *termScreen) moveDown(n int) {
	if s.curRow+n >= s.rows {
		for k := s.curRow + n - s.rows + 1; k > 0; k-- {
			// 深拷贝每行，避免滚动后各行共享底层数组、写入互相污染。
			for r := 1; r < s.rows; r++ {
				s.grid[r-1] = append([]string(nil), s.grid[r]...)
			}
			s.grid[s.rows-1] = make([]string, s.cols)
		}
		s.curRow = s.rows - 1
	} else {
		s.curRow += n
	}
}

func (s *termScreen) write(r rune) {
	if s.curRow >= 0 && s.curRow < s.rows && s.curCol >= 0 && s.curCol < s.cols {
		s.grid[s.curRow][s.curCol] = string(r)
	}
	s.curCol++
}

func (s *termScreen) rowText(r int) string { return strings.Join(s.grid[r], "") }

func (s *termScreen) cursor() (row, col int) { return s.curRow, s.curCol }

func csiNum(param string, def int) int {
	p := strings.TrimPrefix(param, "?")
	if p == "" {
		return def
	}
	n, err := strconv.Atoi(p)
	if err != nil || n < 0 {
		return def
	}
	return n
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func assertGrid(t *testing.T, sc *termScreen, want []string) {
	t.Helper()
	if len(want) > sc.rows {
		t.Fatalf("assertGrid want %d rows but screen has %d", len(want), sc.rows)
	}
	for r, wantRow := range want {
		if got := sc.rowText(r); got != wantRow {
			t.Errorf("screen row %d = %q, want %q", r, got, wantRow)
		}
	}
}

// ── 测试 ──

// TestV2FirstRender 首帧：同步输出包裹、行间 \r\n、末尾无尾随换行、不清屏。
func TestV2FirstRender(t *testing.T) {
	r := newTestV2(10, 24)
	out := capture(func() { r.Render([]string{"line0", "line1"}) })

	if !strings.HasPrefix(out, ansiHideCursor+ansiSyncBegin) {
		t.Errorf("first frame must start with hide-cursor + sync-begin, out=%q", out)
	}
	want := "\x1b[2Kline0" + types.SegmentReset + "\r\n\x1b[2Kline1" + types.SegmentReset
	if got := syncRegion(out); got != want {
		t.Errorf("first frame sync region = %q, want %q", got, want)
	}
	if strings.Contains(out, "\x1b[2J") {
		t.Error("first frame must not clear screen")
	}
	sc := newTermScreen(24, 10)
	sc.feed(out)
	assertGrid(t, sc, []string{"line0", "line1"})
}

// TestV2PositionHardwareCursor 位置定位（IME）：硬件光标经 \x1b[{col+1}G 落到标记行/列。
func TestV2PositionHardwareCursor(t *testing.T) {
	r := newTestV2(10, 24)
	out := capture(func() { r.Render([]string{"line0", ">ab" + core.CURSOR_MARKER + "c"}) })

	if !strings.Contains(out, "\x1b[4G") {
		t.Errorf("must emit absolute column move \\x1b[4G, out=%q", out)
	}
	sc := newTermScreen(24, 10)
	sc.feed(out)
	if row, col := sc.cursor(); row != 1 || col != 3 {
		t.Errorf("hardware cursor = (%d,%d), want (1,3)", row, col)
	}
	if row, col := r.CursorPosition(); row != 1 || col != 3 {
		t.Errorf("CursorPosition = (%d,%d), want (1,3)", row, col)
	}
}

// TestV2StreamingGrowInPlace 流式原地增长：仅重绘变更行，无清屏、无光标上移开头。
func TestV2StreamingGrowInPlace(t *testing.T) {
	r := newTestV2(10, 24)
	out1 := capture(func() { r.Render([]string{"line0", "abc", "tail"}) })
	out2 := capture(func() { r.Render([]string{"line0", "abcd", "tail"}) })

	if strings.Contains(out2, "\x1b[2J") {
		t.Error("streaming growth must not clear screen")
	}
	if n := leadingRunLen(out2, ansiCursorUp); n != 0 {
		t.Errorf("streaming growth must not start with cursor-up, out=%q", out2)
	}
	if n := strings.Count(syncRegion(out2), ansiClearLine2K); n != 1 {
		t.Errorf("must redraw exactly the changed line (1 x 2K), got %d, out=%q", n, out2)
	}
	if !strings.Contains(out2, "\x1b[2Kabcd") {
		t.Errorf("changed line not redrawn in place, out=%q", out2)
	}
	sc := newTermScreen(24, 10)
	sc.feed(out1)
	sc.feed(out2)
	assertGrid(t, sc, []string{"line0", "abcd", "tail"})
}

// TestV2AppendScrollByPrint 追加超出屏幕：\r\n 打印触发终端滚动，无清屏。
func TestV2AppendScrollByPrint(t *testing.T) {
	r := newTestV2(10, 3)
	out1 := capture(func() { r.Render([]string{"a", "b", "c"}) })
	out2 := capture(func() { r.Render([]string{"a", "b", "c", "d", "e", "f"}) })

	if strings.Contains(out2, "\x1b[2J") {
		t.Error("append scroll must not clear screen")
	}
	if !strings.Contains(out2, "\x1b[2Kd") || !strings.Contains(out2, "\x1b[2Ke") || !strings.Contains(out2, "\x1b[2Kf") {
		t.Errorf("appended lines not printed, out=%q", out2)
	}
	sc := newTermScreen(3, 10)
	sc.feed(out1)
	sc.feed(out2)
	assertGrid(t, sc, []string{"d", "e", "f"}) // 顶部行被滚动推出屏幕
	if row, _ := sc.cursor(); row != 2 {
		t.Errorf("cursor row = %d, want 2 (bottom)", row)
	}
}

// TestV2ShrinkClearStale 收缩清残留：新帧变短时清除旧帧残留行，无清屏。
func TestV2ShrinkClearStale(t *testing.T) {
	r := newTestV2(10, 24)
	out1 := capture(func() { r.Render([]string{"a", "b", "c", "d"}) })
	out2 := capture(func() { r.Render([]string{"a", "b"}) })

	if strings.Contains(out2, "\x1b[2J") {
		t.Error("shrink must not clear screen")
	}
	if !strings.Contains(out2, "\x1b[2A") {
		t.Errorf("shrink must move back up by extra rows, out=%q", out2)
	}
	sc := newTermScreen(24, 10)
	sc.feed(out1)
	sc.feed(out2)
	assertGrid(t, sc, []string{"a", "b", "", ""}) // 旧残留行被清除
	if row, col := sc.cursor(); row != 1 || col != 0 {
		t.Errorf("cursor after shrink = (%d,%d), want (1,0)", row, col)
	}
	if r.hardwareCursorRow != 1 {
		t.Errorf("hardwareCursorRow = %d, want 1 (new frame bottom)", r.hardwareCursorRow)
	}
}

// TestV2AboveViewportSkip 可视区之上变更：帧高度不变且变更在可视区之上 → 跳过重绘。
func TestV2AboveViewportSkip(t *testing.T) {
	r := newTestV2(10, 3)
	out1 := capture(func() { r.Render([]string{"l0", "l1", "l2", "l3", "l4"}) })
	out2 := capture(func() { r.Render([]string{"L0", "l1", "l2", "l3", "l4"}) })

	if got := syncRegion(out2); got != "" {
		t.Errorf("above-viewport change must skip redraw, sync region = %q", got)
	}
	sc := newTermScreen(3, 10)
	sc.feed(out1)
	sc.feed(out2)
	assertGrid(t, sc, []string{"l2", "l3", "l4"}) // 可视区内容不变
}

// TestV2ResizeFullRedraw resize：强制清屏全量重绘，清 scrollback。
func TestV2ResizeFullRedraw(t *testing.T) {
	r := newTestV2(10, 24)
	capture(func() { r.Render([]string{"line0", "line1"}) })
	r.SetCols(20)
	r.OnResize()
	out := capture(func() { r.Render([]string{"line0", "line1"}) })

	if !strings.Contains(out, "\x1b[2J") {
		t.Error("resize must clear screen")
	}
	if !strings.Contains(out, "\x1b[3J") {
		t.Error("resize must clear scrollback")
	}
	sc := newTermScreen(24, 20)
	sc.feed(out)
	if sc.clearCount != 1 {
		t.Errorf("screen clear count = %d, want 1", sc.clearCount)
	}
	assertGrid(t, sc, []string{"line0", "line1"})
}

// TestV2WidthCrashGuard 宽度溢出 crash guard：超宽行截断到列宽、不 panic、
// 经工程 log（debug 级别）记录 crash guard 日志。
func TestV2WidthCrashGuard(t *testing.T) {
	r := newTestV2(10, 24)
	long := strings.Repeat("A", 30)
	out := capture(func() { r.Render([]string{long}) })

	sc := newTermScreen(24, 10)
	sc.feed(out)
	if got := sc.rowText(0); got != strings.Repeat("A", 10) {
		t.Errorf("wide line must be truncated to 10 cells, got %q", got)
	}
	if !strings.Contains(out, "\x1b[0m\x1b]8;;\x07") {
		t.Error("truncated line must still close with SEGMENT_RESET")
	}

	// 渲染日志走工程 log（debug 级别）：开 debug + 挂临时日志文件后断言 crash guard 记录。
	log.SetLevel(slog.LevelDebug)
	dir := t.TempDir()
	logPath := filepath.Join(dir, "render.log")
	if err := log.SetFileLog(dir, "render.log", 10*1024*1024, 1); err != nil {
		t.Fatalf("SetFileLog: %v", err)
	}
	// 二次渲染用不同的超宽行：内容有变更 → diff 重绘 → 再次触发 crash guard。
	// （同一行重复渲染无 diff，guardLine 不会被调用。）
	capture(func() { r.Render([]string{strings.Repeat("B", 30)}) })
	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read render log: %v", err)
	}
	if !strings.Contains(string(b), "render crash guard") {
		t.Errorf("render log must contain crash guard entry, got %q", string(b))
	}
}

// TestV2CursorMarkerStripped 光标标记剥离：输出不含 CURSOR_MARKER，内容正确。
func TestV2CursorMarkerStripped(t *testing.T) {
	r := newTestV2(10, 24)
	out := capture(func() { r.Render([]string{"a", ">b" + core.CURSOR_MARKER + "c"}) })

	if strings.Contains(out, core.CURSOR_MARKER) {
		t.Error("output must not contain CURSOR_MARKER")
	}
	if row, col := r.CursorPosition(); row != 1 || col != 2 {
		t.Errorf("CursorPosition = (%d,%d), want (1,2)", row, col)
	}
	sc := newTermScreen(24, 10)
	sc.feed(out)
	assertGrid(t, sc, []string{"a", ">bc"})
}

// TestV2SegmentReset 每行行尾以 SEGMENT_RESET 结束，隔离未闭合样式/超链接。
func TestV2SegmentReset(t *testing.T) {
	r := newTestV2(10, 24)
	out := capture(func() { r.Render([]string{"a", "styled"}) })

	if !strings.Contains(out, "a"+types.SegmentReset) {
		t.Errorf("line must end with SEGMENT_RESET, out=%q", out)
	}
	if !strings.Contains(out, "styled"+types.SegmentReset) {
		t.Errorf("line must end with SEGMENT_RESET, out=%q", out)
	}
}

// TestV2RestoreCursor 退出还原：硬件光标先下移到帧底部，再显示光标 + 换行，
// 避免 shell 提示符叠在残留内容上（如编辑器底边框）。
func TestV2RestoreCursor(t *testing.T) {
	r := newTestV2(10, 24)
	// 3 行帧，光标标记在输入行 row=1 → positionHardwareCursor 停 row=1。
	capture(func() { r.Render([]string{"a", ">b" + core.CURSOR_MARKER, "c"}) })
	out := capture(func() { r.RestoreCursor() })

	// 从 row=1 下移到帧底部 row=2（d=1）。
	if !strings.Contains(out, "\x1b[B") {
		t.Errorf("RestoreCursor must move down to frame bottom, out=%q", out)
	}
	if !strings.Contains(out, ansiShowCursor) {
		t.Errorf("RestoreCursor must show cursor, out=%q", out)
	}
	if !strings.HasSuffix(out, "\r\n") {
		t.Errorf("RestoreCursor must end with newline, out=%q", out)
	}
}

// TestV2DualCoordinate 双坐标模型：cursorRow=内容光标行，hardwareCursorRow=硬件光标实际行。
func TestV2DualCoordinate(t *testing.T) {
	r := newTestV2(10, 24)
	capture(func() { r.Render([]string{"a", ">bc" + core.CURSOR_MARKER, "tail"}) })
	if r.cursorRow != 1 || r.cursorCol != 3 || r.hardwareCursorRow != 1 {
		t.Fatalf("after marker render: cursorRow=(%d,%d) hwRow=%d, want (1,3) and 1",
			r.cursorRow, r.cursorCol, r.hardwareCursorRow)
	}

	// 无标记帧：cursorRow 复位为 -1，硬件光标停在重绘的内容底行。
	capture(func() { r.Render([]string{"a", ">bc", "tail2"}) })
	if r.cursorRow != -1 {
		t.Errorf("no-marker render cursorRow = %d, want -1", r.cursorRow)
	}
	if r.hardwareCursorRow != 2 {
		t.Errorf("no-marker render hardwareCursorRow = %d, want 2 (content bottom)", r.hardwareCursorRow)
	}
}
