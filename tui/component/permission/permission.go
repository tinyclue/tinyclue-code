// Package permission provides a confirmation panel for tool execution approval.
package permission

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"strings"
)

// Action 是用户在权限面板中选择的操作。
type Action int

const (
	ActionAllow Action = iota
	ActionAllowAlways
	ActionDeny
)

// option 是权限面板中的选项条目。
type option struct {
	action Action
	label  string
}

// Panel 是一个工具执行权限确认面板。
// 用户通过 ↑/↓ 选择 Allow/Deny，按 Enter 确认。
type Panel struct {
	options   []option
	cursorSel int
	toolName  string
	toolArgs  string
	message   string

	onSubmit func(action Action)
	onCancel func()

	*component.ComponentBase
}

// New 创建一个新的权限确认面板。
// rememberLabel 为第三个选项 "Allow, and don't ask again" 的文案；为空则不展示该选项（向后兼容）。
func New(toolName, toolArgs, message, rememberLabel string) *Panel {
	options := []option{
		{action: ActionAllow, label: "Allow"},
	}
	if rememberLabel != "" {
		options = append(options, option{action: ActionAllowAlways, label: rememberLabel})
	}
	options = append(options, option{action: ActionDeny, label: "Deny"})
	return &Panel{
		options:       options,
		cursorSel:     0, // default to Allow
		toolName:      toolName,
		toolArgs:      toolArgs,
		message:       message,
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit 注册确认回调。
func (p *Panel) OnSubmit(fn func(action Action)) {
	p.onSubmit = fn
}

// OnCancel 注册取消回调。
func (p *Panel) OnCancel(fn func()) {
	p.onCancel = fn
}

// DoBefore implements core.Component.
func (p *Panel) DoBefore(core.Data) error { return nil }

// DoUpdate handles keyboard input.
func (p *Panel) DoUpdate(data core.Data) error {
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		p.handleInput(m)
	}
	return nil
}

func (p *Panel) handleInput(key core.KeyPressMsg) {
	switch {
	case key.MatchString("up", "ctrl+p"):
		p.cursorSel--
		if p.cursorSel < 0 {
			p.cursorSel = len(p.options) - 1
		}
	case key.MatchString("down", "ctrl+n"):
		p.cursorSel++
		if p.cursorSel >= len(p.options) {
			p.cursorSel = 0
		}
	case key.MatchString("enter"):
		if p.cursorSel >= 0 && p.cursorSel < len(p.options) {
			if p.onSubmit != nil {
				p.onSubmit(p.options[p.cursorSel].action)
			}
		}
	case key.MatchString("esc"):
		if p.onCancel != nil {
			p.onCancel()
		}
	}
}

// Render implements core.Component.
func (p *Panel) Render(data core.Data) core.View {
	width := terminal.DefaultTerminalContext.GetWidth()
	if width < 10 {
		width = 10
	}

	var lines []string

	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf(" %sTool Execution Confirmation%s", types.Bold, types.Reset))
	lines = append(lines, "")

	// Tool name
	lines = append(lines, fmt.Sprintf("  Tool: %s%s%s", types.FgLightBlue, p.toolName, types.Reset))

	// Arguments（多行命令按 \n 分段、每段折行到宽度；首行带 "  Args: " 前缀，
	// 续行缩进 8 列对齐到命令文本起始列，避免续行落在列 0 造成错位）。
	if p.toolArgs != "" {
		wrapWidth := width - 10
		if wrapWidth < 20 {
			wrapWidth = 20
		}
		for i, al := range wrapText(p.toolArgs, wrapWidth) {
			if i == 0 {
				lines = append(lines, fmt.Sprintf("  Args: %s", al))
			} else {
				lines = append(lines, fmt.Sprintf("        %s", al))
			}
		}
	}

	// Custom message
	if p.message != "" {
		lines = append(lines, "")
		msgLines := wrapText(p.message, width-4)
		for _, ml := range msgLines {
			lines = append(lines, fmt.Sprintf("  %s", ml))
		}
	}

	lines = append(lines, "")

	// Options
	for i, opt := range p.options {
		cursor := " "
		if i == p.cursorSel {
			cursor = "→"
		}
		line := fmt.Sprintf(" %s %s", cursor, opt.label)
		if i == p.cursorSel {
			line = types.FgLightBlue + types.Bold + line + types.Reset
		}
		lines = append(lines, line)
	}

	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  ↑↓ navigate · enter confirm · esc deny"+types.Reset)

	return core.View{Lines: lines}
}

// wrapText 将长文本按宽度换行，先按 \n 分段再对每段做宽度折行。
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	var result []string
	for _, p := range strings.Split(text, "\n") {
		runes := []rune(p)
		for len(runes) > 0 {
			if len(runes) <= width {
				result = append(result, string(runes))
				break
			}
			result = append(result, string(runes[:width]))
			runes = runes[width:]
		}
	}
	return result
}
