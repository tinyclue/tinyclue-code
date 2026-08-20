// Package tool 提供工具执行状态组件 ToolComponent。
package tool

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// ToolComponent 显示工具执行信息：工具名称和执行的命令/路径。
type ToolComponent struct {
	*component.ComponentBase
	toolName string // Bash / Read / Write / Edit
	subName  string // 执行的命令或文件路径
}

// NewToolComponent 创建工具组件。
func NewToolComponent(toolName, subName string) *ToolComponent {
	return &ToolComponent{
		ComponentBase: component.NewComponentBase(),
		toolName:      toolName,
		subName:       subName,
	}
}

func (t *ToolComponent) DoBefore(data core.Data) error { return nil }
func (t *ToolComponent) DoUpdate(data core.Data) error { return nil }

// Render 返回工具组件自身内容行。超长 subName（如 Bash 长命令）按终端宽度折行，
// 与 pi 对齐（超长命令折行而非截断，由渲染器 crash guard 兜底）。
// 宽度扣去首行挂件前缀（挂件 1 格 + 空格 1 格），避免前缀叠加后超宽。
func (t *ToolComponent) Render(data core.Data) core.View {
	line := fmt.Sprintf("%s(%s)", t.toolName, t.subName)
	w := terminal.DefaultTerminalContext.GetWidth()
	return core.View{Lines: types.WrapTextWithAnsi(line, max(1, w-2))}
}
