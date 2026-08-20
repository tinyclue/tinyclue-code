package widget

import (
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"sync"
)

// StatusWidgetState 表示状态小圆点的颜色状态。
type StatusWidgetState int

const (
	StatusWidgetDefault StatusWidgetState = iota // 白色
	StatusWidgetSuccess                          // 绿色
	StatusWidgetFail                             // 红色
	StatusWidgetRunning                          // 灰色闪烁
)

// StatusWidget 表示组件首行的状态小圆点，支持多种颜色状态。实现了 Widget 接口。
type StatusWidget struct {
	component.WidgetBase
	state StatusWidgetState
	mu    sync.RWMutex
}

// NewStatusWidget 创建状态小圆点，默认白色。
func NewStatusWidget() *StatusWidget {
	return &StatusWidget{state: StatusWidgetDefault}
}

// SetState 设置小圆点的颜色状态。
func (d *StatusWidget) SetState(state StatusWidgetState) {
	d.mu.Lock()
	d.state = state
	d.mu.Unlock()
}

// State 返回当前颜色状态。
func (d *StatusWidget) State() StatusWidgetState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.state
}

// Render 返回带 ANSI 颜色转义序列的 "⏺" 字符。实现 Widget 接口。
func (d *StatusWidget) Render() string {
	d.mu.RLock()
	defer d.mu.RUnlock()

	switch d.state {
	case StatusWidgetSuccess:
		return types.FgGreen + "⏺" + types.Reset
	case StatusWidgetFail:
		return types.FgRed + "⏺" + types.Reset
	case StatusWidgetRunning:
		return types.FgGray + "\033[5m⏺\033[25m" + types.Reset
	default:
		return types.FgWhite + "⏺" + types.Reset
	}
}

// Reset 重置小圆点为默认颜色。
func (d *StatusWidget) Reset() {
	d.SetState(StatusWidgetDefault)
}

// String 返回无 ANSI 转义的纯文本表示。
func (d *StatusWidget) String() string {
	return "⏺"
}
