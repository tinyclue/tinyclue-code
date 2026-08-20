// Package exitplanmode provides a TUI panel for confirming exit from plan mode.
// 展示计划 + "Yes, start coding" / "No, keep planning" 两个选项，单输入框跟随当前选项：
// 批准时输入 Feedback（作为 acceptFeedback 追加到 tool_result），拒绝时输入 Reason（拒绝原因）。
package exitplanmode

import (
	"fmt"
	"strings"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// Action 是用户在退出计划模式确认面板中选择的操作。
type Action int

const (
	ActionApprove Action = iota
	ActionDeny
)

// Panel 是一个退出计划模式确认面板。
// ↑/↓ 切换 Approve/Deny（各自保留独立输入内容），直接键入追加到当前选项的输入框，
// Enter 确认，Esc 取消。
type Panel struct {
	plan         string
	planFilePath string
	cursorSel    int    // 0=ActionApprove, 1=ActionDeny
	feedback     string // 批准时输入的反馈（acceptFeedback）
	reason       string // 拒绝时输入的原因

	onSubmit func(action Action, reason string)
	onCancel func()

	*component.ComponentBase
}

// New 创建一个退出计划模式确认面板。
// plan 为展示给用户的计划内容；planFilePath 为计划文件路径（可选，仅展示）。
func New(plan, planFilePath string) *Panel {
	return &Panel{
		plan:          plan,
		planFilePath:  planFilePath,
		cursorSel:     0, // default to Approve
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit 注册确认回调。reason 在批准时为 Feedback（acceptFeedback），拒绝时为拒绝原因。
func (p *Panel) OnSubmit(fn func(action Action, reason string)) {
	p.onSubmit = fn
}

// OnCancel 注册取消回调（Esc 取消，agent 端视为无原因拒绝）。
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
			p.cursorSel = 1
		}
	case key.MatchString("down", "ctrl+n"):
		p.cursorSel++
		if p.cursorSel > 1 {
			p.cursorSel = 0
		}
	case key.MatchString("enter"):
		if p.onSubmit != nil {
			if p.cursorSel == int(ActionApprove) {
				p.onSubmit(ActionApprove, p.feedback)
			} else {
				p.onSubmit(ActionDeny, p.reason)
			}
		}
	case key.MatchString("esc"):
		if p.onCancel != nil {
			p.onCancel()
		}
	case key.MatchString("backspace", "ctrl+h"):
		p.deleteLast()
	default:
		if k := key.Key(); k.Text != "" {
			p.appendText(k.Text)
		}
	}
}

// currentText 返回当前选项对应的输入内容。
func (p *Panel) currentText() string {
	if p.cursorSel == int(ActionApprove) {
		return p.feedback
	}
	return p.reason
}

// appendText 把键入字符追加到当前选项对应的输入内容。
func (p *Panel) appendText(text string) {
	if p.cursorSel == int(ActionApprove) {
		p.feedback += text
	} else {
		p.reason += text
	}
}

// deleteLast 删除当前选项输入内容的最后一个字符。
func (p *Panel) deleteLast() {
	cur := p.cursorSel == int(ActionApprove)
	s := p.feedback
	if !cur {
		s = p.reason
	}
	if len(s) == 0 {
		return
	}
	runes := []rune(s)
	s = string(runes[:len(runes)-1])
	if cur {
		p.feedback = s
	} else {
		p.reason = s
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
	lines = append(lines, fmt.Sprintf(" %s%sReady to code?%s", types.FgLightBlue, types.Bold, types.Reset))
	lines = append(lines, "")

	// Plan file path（可选展示）
	if p.planFilePath != "" {
		lines = append(lines, fmt.Sprintf("  %sPlan file: %s%s", types.FgGray, p.planFilePath, types.Reset))
	}

	// Plan content（折行展示；空计划显示占位）
	if p.plan != "" {
		wrapWidth := width - 4
		if wrapWidth < 20 {
			wrapWidth = 20
		}
		for _, pl := range wrapText(p.plan, wrapWidth) {
			lines = append(lines, fmt.Sprintf("  %s", pl))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  %s(no plan yet)%s", types.FgGray, types.Reset))
	}

	lines = append(lines, "")

	// Options
	for i, label := range []string{"Yes, start coding", "No, keep planning"} {
		cursor := " "
		if i == p.cursorSel {
			cursor = "→"
		}
		line := fmt.Sprintf(" %s %s", cursor, label)
		if i == p.cursorSel {
			line = types.FgLightBlue + types.Bold + line + types.Reset
		}
		lines = append(lines, line)
	}

	// 单输入框跟随当前选项：Approve → Feedback（acceptFeedback），Deny → Reason（拒绝原因）
	label, content, placeholder := "Reason", p.reason, "Tell Tinyclue what to change"
	if p.cursorSel == int(ActionApprove) {
		label, content, placeholder = "Feedback", p.feedback, "Add optional feedback"
	}
	displayText := content
	if displayText == "" {
		// 占位符灰显 + 块光标（对齐 askuserquestion Other 编辑态的光标样式）
		displayText = types.FgGray + placeholder + types.Reset + types.CursorStyle + " " + types.Reset
	} else {
		displayText = content + types.CursorStyle + " " + types.Reset
	}
	lines = append(lines, fmt.Sprintf("  %s%s:%s [%s]", types.FgLightBlue, label, types.Reset, displayText))

	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  ↑↓ navigate · type · enter confirm · esc cancel"+types.Reset)

	return core.View{Lines: lines}
}

// wrapText 将长文本按宽度换行，先按 \n 分段再对每段做宽度折行（对齐 permission 包同逻辑）。
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
