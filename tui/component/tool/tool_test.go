package tool

import (
	"strings"
	"testing"

	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// TestToolRenderWraps 超长 subName（如 Bash 长命令）折行到终端宽度而非截断，
// 与 pi 的 bash tool 渲染对齐。宽度扣去挂件前缀（2 格）。
func TestToolRenderWraps(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(20)
	tc := NewToolComponent("Bash", strings.Repeat("x", 60))

	v := tc.Render(core.Data{})
	if len(v.Lines) < 2 {
		t.Fatalf("long bash args must wrap into multiple lines, got %d line(s): %q", len(v.Lines), v.Lines)
	}
	for i, l := range v.Lines {
		if w := types.VisualWidth(l); w > 20 {
			t.Errorf("line %d width = %d > 20: %q", i, w, l)
		}
	}
}

// TestToolRenderShort 短 subName 不折行，保持单行。
func TestToolRenderShort(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(20)
	tc := NewToolComponent("Bash", "ls")

	v := tc.Render(core.Data{})
	if len(v.Lines) != 1 || v.Lines[0] != "Bash(ls)" {
		t.Fatalf("short args must stay one line, got %q", v.Lines)
	}
}
