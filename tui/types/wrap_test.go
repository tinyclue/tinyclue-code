package types

import (
	"strings"
	"testing"
)

// TestWrapAsciiWord 纯 ASCII：按单词折行，不截断单词。
func TestWrapAsciiWord(t *testing.T) {
	got := WrapTextWithAnsi("hello world foo bar", 10)
	// "hello " 6 + "world" 5 > 10 → 折行；"world " 6 + "foo" 3 = 9 ≤ 10
	want := []string{"hello", "world foo", "bar"}
	assertLines(t, got, want)
}

// TestWrapCJK 中文文本：按字符折行，每行 ≤ width。
func TestWrapCJK(t *testing.T) {
	got := WrapTextWithAnsi("这是一个很长的中文句子用来测试折行", 10)
	for i, l := range got {
		if w := VisualWidth(l); w > 10 {
			t.Errorf("line %d width = %d > 10, line=%q", i, w, l)
		}
	}
	if !strings.Contains(strings.Join(got, "\n"), "这是一个") {
		t.Errorf("CJK content missing, got=%q", got)
	}
}

// TestWrapAnsiStyleReopen 样式跨行重开：加粗文本折行后，第二行行首重新打开加粗。
func TestWrapAnsiStyleReopen(t *testing.T) {
	// "\x1b[1m" + "bold text that wraps here" → 折成两行
	input := "\x1b[1mbold text that wraps over here\x1b[0m"
	got := WrapTextWithAnsi(input, 15)
	if len(got) < 2 {
		t.Fatalf("expected multiple wrapped lines, got=%q", got)
	}
	if !strings.HasPrefix(got[1], "\x1b[1m") {
		t.Errorf("wrapped continuation must reopen bold, got=%q", got[1])
	}
	if strings.HasSuffix(got[0], "\x1b[0m") {
		t.Errorf("wrapped line end must NOT close bold (continuation reopens it), got=%q", got[0])
	}
	if VisualWidth(got[0]) > 15 || VisualWidth(got[1]) > 15 {
		t.Errorf("wrapped lines exceed width: %q, %q", got[0], got[1])
	}
}

// TestWrapLongWordBreak 超长无空格单词：逐字符断行，每行 ≤ width。
func TestWrapLongWordBreak(t *testing.T) {
	long := strings.Repeat("A", 30)
	got := WrapTextWithAnsi(long, 10)
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got=%q", got)
	}
	for i, l := range got {
		if VisualWidth(l) > 10 {
			t.Errorf("line %d width = %d > 10", i, VisualWidth(l))
		}
	}
}

// TestWrapNewline 换行符保留为独立行。
func TestWrapNewline(t *testing.T) {
	got := WrapTextWithAnsi("line one\nline two", 100)
	assertLines(t, got, []string{"line one", "line two"})
}

// TestTruncateToWidth 截断到指定宽度。
func TestTruncateToWidth(t *testing.T) {
	got := TruncateToWidth("hello world", 8, "...")
	if VisualWidth(got) != 8 {
		t.Errorf("truncated width = %d, want 8, got=%q", VisualWidth(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("must end with ellipsis, got=%q", got)
	}
}

// TestTruncateToWidthFits 文本不超宽时不截断。
func TestTruncateToWidthFits(t *testing.T) {
	s := "\x1b[32mhello\x1b[0m"
	if got := TruncateToWidth(s, 10, "..."); got != s {
		t.Errorf("must return original, got=%q", got)
	}
}

// TestTruncateToWidthAnsi ANSI 序列不计宽，截断后保留样式可被行尾 Reset 关闭。
func TestTruncateToWidthAnsi(t *testing.T) {
	s := "\x1b[32mgreen colored long text here\x1b[0m"
	got := TruncateToWidth(s, 10, "")
	if VisualWidth(got) > 10 {
		t.Errorf("width = %d > 10, got=%q", VisualWidth(got), got)
	}
	if !strings.Contains(got, "\x1b[32m") {
		t.Errorf("must preserve color open code, got=%q", got)
	}
}

// TestNormalizeTerminalOutput 泰语 AM 元音分解。
func TestNormalizeTerminalOutput(t *testing.T) {
	if got := NormalizeTerminalOutput("a\u0e33b"); got != "a\u0e4d\u0e32b" {
		t.Errorf("got=%q", got)
	}
	if got := NormalizeTerminalOutput("plain"); got != "plain" {
		t.Errorf("got=%q", got)
	}
}

// TestAnsiCodeTrackerGetLineEndReset 下划线行尾只关下划线，重开保留加粗。
func TestAnsiCodeTrackerGetLineEndReset(t *testing.T) {
	tr := &AnsiCodeTracker{}
	tr.Process("\x1b[1;4m") // bold + underline
	if got := tr.GetLineEndReset(); got != "\x1b[24m" {
		t.Errorf("line end reset = %q, want underline-off only", got)
	}
	active := tr.GetActiveCodes()
	if !strings.Contains(active, "1") || !strings.Contains(active, "4") {
		t.Errorf("active codes must reopen bold AND underline: %q", active)
	}
}

func assertLines(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d lines %q, want %d lines %q", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}
