package editor

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// pasteContent 向编辑器发送粘贴事件（对齐既有测试的写法）。
func pasteContent(e *Editor, content string) {
	e.DoUpdate(core.Data{Msg: core.PasteMsg(uv.PasteEvent{Content: content})})
}

// TestPasteLargeMultiline 验证多行大粘贴折叠为 [Pasted text #N +M lines]（M=换行数），Text() 展开还原。
func TestPasteLargeMultiline(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24) // maxLines = min(24-10, 2) = 2
	e := New()
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		sb.WriteString("line-")
		sb.WriteString(strings.Repeat("x", 20))
		sb.WriteByte('\n')
	}
	content := sb.String()

	pasteContent(e, content)

	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1 +12 lines]") {
		t.Fatalf("expected collapsed marker with 12 lines, got %q", e.TextBuffer.Text())
	}
	if e.Text() != content {
		t.Errorf("Text() should expand to original content")
	}
}

// TestPasteLongSingleLine 验证超长单行（>800 字符、无换行）折叠为 [Pasted text #N]（省略行数）。
func TestPasteLongSingleLine(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	e := New()
	content := strings.Repeat("a", pasteThreshold+1) // 801 字符，单行

	pasteContent(e, content)

	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1]") {
		t.Fatalf("expected marker without +lines, got %q", e.TextBuffer.Text())
	}
	if e.Text() != content {
		t.Errorf("Text() should expand to original content")
	}
}

// TestPasteLineThreshold 验证行数阈值：numLines=3（4 行）折叠，numLines=2（3 行）不折叠。
func TestPasteLineThreshold(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24) // maxLines = 2：numLines > 2 才折叠

	e := New()
	pasteContent(e, "a\nb\nc\nd") // numLines=3
	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1 +3 lines]") {
		t.Fatalf("numLines=3 should collapse, got %q", e.TextBuffer.Text())
	}

	e2 := New()
	pasteContent(e2, "a\nb\nc") // numLines=2
	if strings.Contains(e2.TextBuffer.Text(), "[Pasted text") {
		t.Fatalf("numLines=2 should not collapse, got %q", e2.TextBuffer.Text())
	}
	if e2.Text() != "a\nb\nc" {
		t.Errorf("small paste should be inserted verbatim, got %q", e2.Text())
	}
}

// TestPasteSmallCleaned 验证小粘贴直接插入清理后文本：CRLF 归一化、tab 转 4 空格。
func TestPasteSmallCleaned(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	e := New()

	pasteContent(e, "x\ty\r\nz")

	if strings.Contains(e.TextBuffer.Text(), "[Pasted text") {
		t.Fatalf("small paste should not collapse, got %q", e.TextBuffer.Text())
	}
	if e.Text() != "x    y\nz" {
		t.Errorf("small paste should be cleaned, got %q", e.Text())
	}
}

// TestPasteMultipleSequence 验证连续大粘贴编号递增，Text() 逐个展开。
func TestPasteMultipleSequence(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	e := New()
	content1 := "c1\nc2\nc3\nc4"
	content2 := "d1\nd2\nd3\nd4"

	pasteContent(e, content1) // [Pasted text #1 +3 lines]
	pasteContent(e, content2) // [Pasted text #2 +3 lines]

	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1 +3 lines][Pasted text #2 +3 lines]") {
		t.Fatalf("expected two sequential markers, got %q", e.TextBuffer.Text())
	}
	if e.Text() != content1+content2 {
		t.Errorf("Text() should expand both pastes in order, got %q", e.Text())
	}
}

// TestPasteTruncation 验证输入总长超过 10000 时中间截断为 [...Truncated text #N +M lines...]，
// 保留首尾各 500 字符，Text() 仍能还原原文。
func TestPasteTruncation(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	e := New()
	big := strings.Repeat("m", truncationThreshold+2000) // 12000 字符

	e.SetText(big)
	e.maybeTruncate()

	buf := e.TextBuffer.Text()
	if len(buf) >= len(big) {
		t.Errorf("buffer should be truncated, got len=%d want < %d", len(buf), len(big))
	}
	if !strings.Contains(buf, "[...Truncated text #1 +") {
		t.Errorf("expected truncated marker, got %q", buf)
	}
	if e.Text() != big {
		t.Errorf("Text() should restore full content after truncation")
	}
}

// TestPasteMultilineSplitsLines 验证多行粘贴把 \n 拆成真实逻辑行：
// 缓冲 Lines 数量正确、每行不含内嵌换行、View/Height 反映真实行数（渲染才不越过分隔线）。
func TestPasteMultilineSplitsLines(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	terminal.DefaultTerminalContext.SetWidth(100)
	e := New()
	e.DoBefore(core.Data{})
	content := "PNG 也读不了。\n  DeepSeek-v4-flash\n  \"Unsupported Image\""

	pasteContent(e, content)

	if len(e.TextBuffer.Lines) != 3 {
		t.Fatalf("Lines should be 3 logical lines, got %d: %q", len(e.TextBuffer.Lines), e.TextBuffer.Lines)
	}
	for i, l := range e.TextBuffer.Lines {
		for _, r := range l {
			if r == '\n' {
				t.Errorf("logical line %d should not contain embedded newline", i)
			}
		}
	}
	if v := e.View(); len(v) != 3 {
		t.Errorf("View should have 3 rows, got %d: %q", len(v), v)
	}
	if e.Height() != 3 {
		t.Errorf("Height should be 3, got %d", e.Height())
	}
	if e.Text() != content {
		t.Errorf("Text() should roundtrip, got %q", e.Text())
	}
}

// TestPasteTruncationKeepsSmallInput 验证短输入不触发截断。
func TestPasteTruncationKeepsSmallInput(t *testing.T) {
	terminal.DefaultTerminalContext.SetHeight(24)
	e := New()
	content := strings.Repeat("s", 100)

	e.SetText(content)
	e.maybeTruncate()

	if e.TextBuffer.Text() != content {
		t.Errorf("small input should be unchanged, got %q", e.TextBuffer.Text())
	}
}
