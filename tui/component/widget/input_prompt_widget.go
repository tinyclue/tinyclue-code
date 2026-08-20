// Package widget 提供组件首行的可选前缀装饰挂件实现。
package widget

import (
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// InputPromptWidget 是输入框 ">" 提示符挂件，用于 Editor 输入区首行。
// 与用于用户消息的 PromptWidget（❯）区分，Editor 内部默认引用本挂件。
type InputPromptWidget struct {
	component.WidgetBase
}

// Render 返回绿色 ">"。
func (p *InputPromptWidget) Render() string {
	return types.FgLightBlue + ">" + types.Reset
	//return ""
}
