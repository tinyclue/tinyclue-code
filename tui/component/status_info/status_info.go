// Package status_info 提供状态信息组件 StatusInfoComponent，带状态挂件显示。
package status_info

import (
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/component/widget"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"sync"
)

// StatusState 表示 StatusInfoComponent 的生命周期状态。
// 挂件层只有 running/done 两种样式：default 与 running 都映射到 running 动画，
// done 映射到完成样式。
type StatusState int

const (
	StatusDefault StatusState = iota // 默认（等价运行中，显示旋转动画）
	StatusRunning                    // 运行中（旋转动画）
	StatusDone                       // 完成（绿色对勾）
)

// StatusInfoComponent 显示状态信息，包含状态挂件、标题和内容。
type StatusInfoComponent struct {
	*component.ComponentBase
	title   string
	content string
	loader  *widget.LoaderWidget
	state   StatusState
	mu      sync.RWMutex
}

// NewStatusInfoComponent 创建状态信息组件，默认状态为 Default（显示运行动画）。
func NewStatusInfoComponent(title string) *StatusInfoComponent {
	return &StatusInfoComponent{
		ComponentBase: component.NewComponentBase(),
		title:         title,
		loader:        widget.NewLoaderWidget(),
		state:         StatusDefault,
	}
}

// SetContent 更新组件内容文本。
func (sic *StatusInfoComponent) SetContent(content string) {
	sic.mu.Lock()
	sic.content = content
	sic.mu.Unlock()
}

// Content 返回当前内容文本。
func (sic *StatusInfoComponent) Content() string {
	sic.mu.RLock()
	defer sic.mu.RUnlock()
	return sic.content
}

// SetTitle 更新标题。
func (sic *StatusInfoComponent) SetTitle(title string) {
	sic.mu.Lock()
	sic.title = title
	sic.mu.Unlock()
}

// SetRequestRender 设置触发 TUI 重绘的回调，转发给状态挂件用于驱动旋转动画。
// 必须在 SetState(StatusRunning) 之前注入。
func (sic *StatusInfoComponent) SetRequestRender(fn func()) {
	sic.loader.SetRequestRender(fn)
}

// SetState 设置状态。default/running 映射为挂件的 running 动画，done 映射为完成样式。
func (sic *StatusInfoComponent) SetState(state StatusState) {
	sic.mu.Lock()
	sic.state = state
	sic.mu.Unlock()

	switch state {
	case StatusDone:
		sic.loader.SetState(widget.LoaderDone)
	default:
		sic.loader.SetState(widget.LoaderRunning)
	}
}

// State 返回当前状态。
func (sic *StatusInfoComponent) State() StatusState {
	sic.mu.RLock()
	defer sic.mu.RUnlock()
	return sic.state
}

func (sic *StatusInfoComponent) DoBefore(data core.Data) error { return nil }
func (sic *StatusInfoComponent) DoUpdate(data core.Data) error { return nil }

// Render 返回状态信息组件自身内容行。
// 格式：{挂件} {title} {content}
func (sic *StatusInfoComponent) Render(data core.Data) core.View {
	sic.mu.RLock()
	title := sic.title
	content := sic.content
	sic.mu.RUnlock()

	stateStr := sic.loader.Render()

	var line string
	if content != "" {
		line = stateStr + " " + title + " " + content
	} else {
		line = stateStr + " " + title
	}

	return core.View{Lines: []string{line}}
}
