// Package core 提供基础组件类型：Component 接口、View、Container。
package core

import "sync"

// CURSOR_MARKER 是 editor 嵌入在渲染行中的光标标记。
// TUI 在 doRender 中扫描此标记以确定终端光标位置，然后将其从帧中移除。
const CURSOR_MARKER = "\x1b_pi:c\x07"

// ContainerType 标识容器在布局中的角色，用于通过 tui.ModelV2 按类型查找容器。
type ContainerType string

const (
	HeaderContainerType          ContainerType = "header"
	ChatContainerType            ContainerType = "chat"
	ThinkingContainerType        ContainerType = "thinking"
	PermissionPanelContainerType ContainerType = "permission_panel"
	StatusContainerType          ContainerType = "status"
	EditorContainerType          ContainerType = "editor"
	ConfigContainerType          ContainerType = "config"
	ConfigPanelContainerType     ContainerType = "config_panel"
	FooterContainerType          ContainerType = "footer"
	TaskPanelContainerType       ContainerType = "task_panel"
)

type Data struct {
	Msg Msg
}

// View 是 Component 渲染后的输出视图。
type View struct {
	Lines []string
}

// Focusable 接口表示组件是否获得焦点，用于自定义光标渲染。
type Focusable interface {
	SetFocused(focused bool)
	IsFocused() bool
}

// Component 必须实现 Render 方法，返回 View。
type Component interface {
	DoBefore(data Data) error
	DoUpdate(data Data) error
	Render(data Data) View
	Children() []Component
}

// Container 包含多个 Component。
type Container struct {
	mu            sync.RWMutex
	ContainerType ContainerType
	children      []Component
	hidden        bool // true 时跳过渲染和事件分发
}

// SetHidden 设置容器的隐藏/显示状态。
func (c *Container) SetHidden(hide bool) *Container {
	c.mu.Lock()
	c.hidden = hide
	c.mu.Unlock()
	return c
}

// IsHidden 返回容器是否隐藏。
func (c *Container) IsHidden() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.hidden
}

func NewContainer(ct ContainerType) *Container {
	return &Container{ContainerType: ct}
}

func (c *Container) AddChild(child Component) {
	c.mu.Lock()
	c.children = append(c.children, child)
	c.mu.Unlock()
}

func (c *Container) RemoveChild(child Component) {
	c.mu.Lock()
	for i, ch := range c.children {
		if ch == child {
			c.children = append(c.children[:i], c.children[i+1:]...)
			c.mu.Unlock()
			return
		}
	}
	c.mu.Unlock()
}

// Children 返回子组件列表的副本。线程安全。
func (c *Container) Children() []Component {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]Component, len(c.children))
	copy(result, c.children)
	return result
}

// Len 返回子组件数量。线程安全。
func (c *Container) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.children)
}

// ClearChildren 清空子组件列表。线程安全。
func (c *Container) ClearChildren() {
	c.mu.Lock()
	c.children = nil
	c.mu.Unlock()
}
