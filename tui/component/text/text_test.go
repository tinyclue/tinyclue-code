package text

import (
	"strings"
	"testing"

	"github.com/tinyclue/tinyclue-code/tui/types"
)

// TestRenderCodeBlockWraps 超宽代码行折行到 termWidth 而非截断，与 pi 对齐。
func TestRenderCodeBlockWraps(t *testing.T) {
	long := strings.Repeat("word ", 30)
	content := "```\n" + long + "```"

	lines := renderCodeBlock(content, "", 20)
	if len(lines) < 2 {
		t.Fatalf("long code must wrap into multiple lines, got %d: %q", len(lines), lines)
	}
	for i, l := range lines {
		if w := types.VisualWidth(l); w > 20 {
			t.Errorf("line %d width = %d > 20: %q", i, w, l)
		}
	}
	// 每行保留 2 空格缩进
	if !strings.HasPrefix(lines[0], "  ") {
		t.Errorf("first line must start with 2-space indent, got %q", lines[0])
	}
}

// TestRenderCodeBlockShort 短代码行保持单行 + 缩进。
func TestRenderCodeBlockShort(t *testing.T) {
	content := "```\nvar x = 1\n```"

	lines := renderCodeBlock(content, "", 40)
	if len(lines) != 1 || lines[0] != "  var x = 1" {
		t.Fatalf("short code must stay one line with indent, got %q", lines)
	}
}
