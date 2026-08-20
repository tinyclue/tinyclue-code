// Package widget 提供组件首行的可选前缀装饰挂件实现。
package widget

import (
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// PromptWidget 是静态灰色 "❯" 提示符挂件，用于用户消息首行。
type PromptWidget struct {
	component.WidgetBase
}

// Render 返回灰色 "❯"。
func (p *PromptWidget) Render() string { return types.FgWhite + "❯" + types.Reset }
