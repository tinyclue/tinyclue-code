// Package tui 提供终端 UI 主模型：组件树遍历、事件分发与渲染调度。
//
// tui_v2.go 是配套 renderer_v2.go（RendererV2）的主模型 ModelV2（v1 Model 与 v1
// Renderer 已移除）。相比 v1 Model 的改进：
//   - renderer 字段类型换成 *renderer.RendererV2（同步输出差分渲染，见 renderer_v2.go）；
//   - 修复 ctrl+l 清屏后不立即重绘（v1 清屏后屏幕空白直到下一次按键）。
//
// 组件树遍历、前缀/背景/子组件递归、事件分发、渲染节流等编排逻辑与 v1 完全一致；
// 包级自由函数（renderComponent / setCollapsedRecursive / hasExpandedRecursive）随
// v1 tui.go 移除并入本文件。
package tui

import (
	"context"
	"fmt"
	"os"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/component/autocomplete"
	"github.com/tinyclue/tinyclue-code/tui/component/editor"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/renderer"
	"github.com/tinyclue/tinyclue-code/tui/terminal"

	uv "github.com/charmbracelet/ultraviolet"
)

// ModelV2 — Elm Architecture 主模型（RendererV2 版）。
type ModelV2 struct {
	ctx             context.Context
	containers      []*core.Container                      // 有序渲染列表
	containerMap    map[core.ContainerType]*core.Container // 类型索引
	cancel          context.CancelFunc
	renderTimer     *RenderTimer // 16ms 渲染节流
	program         *terminal.Program
	renderer        *renderer.RendererV2 // 同步输出差分渲染 + 硬件光标定位
	focusedComp     core.Component       // 当前焦点组件，用于假光标渲染
	terminalFocused bool                 // 终端焦点状态，失焦时隐藏光标
	onInterrupt     func()
}

// NewV2 创建一个 ModelV2。
func NewV2(ctx context.Context) *ModelV2 {
	return &ModelV2{
		ctx:             ctx,
		containerMap:    make(map[core.ContainerType]*core.Container),
		renderer:        renderer.NewV2(),
		terminalFocused: true,
	}
}

// WithCancel 设置取消函数并返回副本。
func (m *ModelV2) WithCancel(cancel context.CancelFunc) *ModelV2 {
	m.cancel = cancel
	return m
}

func (m *ModelV2) AddContainer(container *core.Container) {
	m.containers = append(m.containers, container)
	m.containerMap[container.ContainerType] = container
}

func (m *ModelV2) GetContainer(ct core.ContainerType) *core.Container {
	return m.containerMap[ct]
}

func (m *ModelV2) RemoveContainer(ct core.ContainerType) {
	if _, ok := m.containerMap[ct]; !ok {
		return
	}
	delete(m.containerMap, ct)
	for i, c := range m.containers {
		if c.ContainerType == ct {
			m.containers = append(m.containers[:i], m.containers[i+1:]...)
			return
		}
	}
}

func (m *ModelV2) SetOnInterrupt(onInterrupt func()) {
	m.onInterrupt = onInterrupt
}

// SetFocus 设置焦点组件，控制反转色假光标的渲染。
func (m *ModelV2) SetFocus(comp core.Component) {
	m.focusedComp = comp
}

// Send 向事件循环投递一条消息。
func (m *ModelV2) Send(msg core.Msg) {
	if m.program != nil {
		m.program.Send(msg)
	}
}

func (m *ModelV2) Run(ctx context.Context, cancel context.CancelFunc) {
	// event loop 跑在独立协程，m.program.Run() 会阻塞
	m.program = terminal.New(m, terminal.WithSignalHandler())
	m.renderTimer = NewRenderTimer()
	m.renderTimer.SetProgram(m.program)
	go func() {
		if err := m.program.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "\nexit: %v\n", err)
		}
		cancel()
	}()
}

// doRender 遍历组件构建帧，然后委托 renderer 完成差分渲染与光标定位。
func (m *ModelV2) doRender(data core.Data) error {
	var newFrame []string

	for _, c := range m.containers {
		if c.IsHidden() {
			continue
		}
		for _, comp := range c.Children() {
			// 设置焦点状态，控制反转色光标的渲染
			if f, ok := comp.(core.Focusable); ok {
				f.SetFocused(m.terminalFocused && comp == m.focusedComp)
			}
			// 跳过隐藏组件（已完成的 thinking 等），但不从容器中移除
			if h, ok := comp.(interface{ IsHidden() bool }); ok && h.IsHidden() {
				continue
			}
			view := renderComponent(comp, data)
			newFrame = append(newFrame, view.Lines...)
		}
	}

	m.renderer.SetCols(terminal.DefaultTerminalContext.GetWidth())
	m.renderer.Render(newFrame)
	return nil
}

// Update 实现 terminal.Model 接口。
func (m *ModelV2) Update(data core.Data) error {
	switch data.Msg.(type) {
	case core.InterruptMsg:
		if m.onInterrupt != nil {
			m.onInterrupt()
		}
		m.dispatchMsg(data)
		return nil
	case core.QuitMsg:
		m.renderer.RestoreCursor()
		m.renderTimer.Stop()
		if m.cancel != nil {
			m.cancel()
		}
		return nil
	}

	// ── 特殊按键：清屏 / 折叠切换 ──
	if keyMsg, ok := data.Msg.(core.KeyPressMsg); ok {
		switch {
		case keyMsg.MatchString("ctrl+l"):
			m.renderer.ClearScreen()
			// 修复 v1：清屏后必须立即重绘，否则屏幕空白直到下一次按键
			m.timerRender(data)
			return nil
		case keyMsg.MatchString("ctrl+o"):
			m.setAllCollapsed(false)
			m.timerRender(data)
			return nil
		case keyMsg.MatchString("esc"):
			// 优先 1：有展开组件则折叠（与 ctrl+o 配对）
			if m.hasExpanded() {
				m.setAllCollapsed(true)
				m.timerRender(data)
				return nil
			}
			// 优先 2：模态面板显示时让面板消费 ESC
			if m.hasActivePanel() {
				m.dispatchMsg(data)
				m.timerRender(data)
				return nil
			}
			// 兜底：正常分发
			m.dispatchMsg(data)
			m.timerRender(data)
			return nil
		}
	}

	m.dispatchMsg(data)
	return nil
}

func (m *ModelV2) DoCompentBeforeUpdate(data core.Data) {
	for _, c := range m.containers {
		if c.IsHidden() {
			continue
		}
		for _, comp := range c.Children() {
			comp.DoBefore(data)
		}
	}
}

func (m *ModelV2) DoCompentUpdate(data core.Data) {
	for _, c := range m.containers {
		if c.IsHidden() {
			continue
		}
		for _, comp := range c.Children() {
			comp.DoUpdate(data)
		}
	}
}

func (m *ModelV2) timerRender(data core.Data) {
	m.renderTimer.RequestRender(data)
}

func (m *ModelV2) RequestRender() {
	// renderTimer 在 Run() 才创建；Init 阶段重放（resume -c）等 render 请求先于
	// 首帧，此时静默忽略（首帧渲染自会带上已加入容器的组件）。
	if m.renderTimer == nil {
		return
	}
	m.renderTimer.RequestRender(core.Data{Msg: core.ReqRenderMsg{}})
}

// dispatchMsg 分发消息到对应处理逻辑，不涉及渲染。
func (m *ModelV2) dispatchMsg(data core.Data) error {
	switch data.Msg.(type) {
	// ── 渲染节流：timer 批准新一帧渲染 ──
	case core.RenderGrantMsg:
		m.doRender(data)
	// ── 窗口大小变化 ──
	case core.WindowSizeMsg:
		if ws, ok := data.Msg.(core.WindowSizeMsg); ok {
			m.renderer.SetTermHeight(uv.WindowSizeEvent(ws).Height)
			m.renderer.OnResize()
		}
		m.timerRender(data)
	// ── 按键 ──
	case core.KeyPressMsg:
		m.DoCompentBeforeUpdate(data)
		m.DoCompentUpdate(data)
		m.timerRender(data)
	// ── 粘贴 ──
	case core.PasteMsg:
		m.DoCompentBeforeUpdate(data)
		m.DoCompentUpdate(data)
		m.timerRender(data)
	// ── 终端焦点 ──
	case core.InterruptMsg:
		m.DoCompentUpdate(data)
		m.timerRender(data)
	// ── /mcp 面板动作异步完成：组件刷新（SetItems）后重绘 ──
	case core.McpRefreshMsg:
		m.DoCompentUpdate(data)
		m.timerRender(data)
	// ── /login 订阅登录异步完成：面板结束忙碌态（成功/取消关面板，失败停留可选其他厂商）──
	case core.OAuthDoneMsg:
		m.DoCompentUpdate(data)
		m.timerRender(data)
	case core.FocusMsg:
		m.terminalFocused = true
		m.timerRender(data)
	case core.BlurMsg:
		m.terminalFocused = false
		m.timerRender(data)
	}
	return nil
}

// setAllCollapsed 递归遍历所有容器中的组件，将实现了 Collapsible 且 IsCollapsible()==true
// 的组件统一设置为指定的折叠/展开状态。无需外部注册。
func (m *ModelV2) setAllCollapsed(v bool) {
	for _, c := range m.containers {
		if c.IsHidden() {
			continue
		}
		for _, comp := range c.Children() {
			setCollapsedRecursive(comp, v)
		}
	}
}

// hasActivePanel 检查 config_panel 容器是否有子组件（modelpanel/loginpanel 正在显示）。
func (m *ModelV2) hasActivePanel() bool {
	if panel := m.containerMap[core.ConfigPanelContainerType]; panel != nil && panel.Len() > 0 {
		return true
	}
	return false
}

// hasExpanded 遍历所有可见组件的可折叠子树，检查是否有组件处于展开状态。
func (m *ModelV2) hasExpanded() bool {
	for _, c := range m.containers {
		if c.IsHidden() {
			continue
		}
		for _, comp := range c.Children() {
			if hasExpandedRecursive(comp) {
				return true
			}
		}
	}
	return false
}

// renderComponent 递归渲染组件树：裸内容 → 前缀 → 背景 → 子组件。
func renderComponent(comp core.Component, data core.Data) core.View {
	view := comp.Render(data)

	// 统一应用前缀（widget + level）
	if p, ok := comp.(component.PrefixProvider); ok {
		view.Lines = component.ApplyPrefix(view.Lines, p.Widget(), p.Level())
	}

	// 统一应用背景（覆盖前缀区 + 内容 + 右填充）
	if bg, ok := comp.(component.BackgroundProvider); ok {
		if c := bg.BackgroundColor(); c != "" {
			for i, line := range view.Lines {
				view.Lines[i] = component.FillBackground(line, c, bg.FullWidth())
			}
		}
	}

	// 统一递归子组件
	for _, child := range comp.Children() {
		childView := renderComponent(child, data)
		view.Lines = append(view.Lines, childView.Lines...)
	}

	return view
}

func setCollapsedRecursive(comp core.Component, v bool) {
	if col, ok := comp.(component.Collapsible); ok && col.IsCollapsible() {
		col.SetCollapsed(v)
	}
	for _, child := range comp.Children() {
		setCollapsedRecursive(child, v)
	}
}

func hasExpandedRecursive(comp core.Component) bool {
	if col, ok := comp.(component.Collapsible); ok && col.IsCollapsible() && !col.IsCollapsed() {
		return true
	}
	for _, child := range comp.Children() {
		if hasExpandedRecursive(child) {
			return true
		}
	}
	return false
}

// 确保 core.Component 接口在编译期被检查。
var _ core.Component = (*editor.Editor)(nil)
var _ core.Component = (*autocomplete.AutoComplete)(nil)
