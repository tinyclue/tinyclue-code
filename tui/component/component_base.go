// Package component 提供 TUI 组件的通用基础和工具组件。
package component

import (
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"strings"
	"sync"
)

var grayBranch = types.FgGray + "⎿" + types.Reset

// PrefixProvider 是组件提供前缀信息的接口。由 ComponentBase 实现，
// tui 渲染管线通过类型断言检查组件是否需要应用前缀。
type PrefixProvider interface {
	Widget() Widget
	Level() int
}

// ComponentBase 是组件的通用嵌入基类，提供子组件管理、层级缩进和首行挂件支持。
type ComponentBase struct {
	mu       sync.RWMutex
	children []core.Component
	level    int
	widget   Widget // 首行挂件，nil 表示无前缀
	focused  bool   // 组件根据这个值决定是否渲染假光标 + CURSOR_MARKER
	hidden   bool   // true 时 doRender 跳过此组件的渲染，但不从容器中移除
}

func NewComponentBase() *ComponentBase {
	return &ComponentBase{}
}

// Children 返回子组件列表的副本，实现 core.Component 接口。线程安全。
func (b *ComponentBase) Children() []core.Component {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]core.Component, len(b.children))
	copy(result, b.children)
	return result
}

// SetWidget 设置首行挂件（状态小圆点、提示符等）。
func (b *ComponentBase) SetWidget(w Widget) {
	b.widget = w
}

// Widget 返回当前首行挂件。
func (b *ComponentBase) Widget() Widget {
	return b.widget
}

// AddChild 添加子组件。线程安全。
func (b *ComponentBase) AddChild(child core.Component) {
	b.mu.Lock()
	b.children = append(b.children, child)
	b.mu.Unlock()
}

// RemoveChild 移除指定子组件。线程安全。
func (b *ComponentBase) RemoveChild(child core.Component) {
	b.mu.Lock()
	for i, ch := range b.children {
		if ch == child {
			b.children = append(b.children[:i], b.children[i+1:]...)
			b.mu.Unlock()
			return
		}
	}
	b.mu.Unlock()
}

// SetLevel 设置当前组件的缩进层级。
func (b *ComponentBase) SetLevel(level int) {
	b.level = level
}

// Level 返回当前组件的缩进层级。
func (b *ComponentBase) Level() int {
	return b.level
}

// SetFocused 设置焦点状态，控制假光标和 CURSOR_MARKER 的渲染。线程安全。
func (b *ComponentBase) SetFocused(focused bool) {
	b.mu.Lock()
	b.focused = focused
	b.mu.Unlock()
}

// IsFocused 返回当前组件的焦点状态。线程安全。
func (b *ComponentBase) IsFocused() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.focused
}

// SetHidden 设置隐藏状态。隐藏的组件不会被渲染，但保留在容器中。线程安全。
func (b *ComponentBase) SetHidden(hidden bool) {
	b.mu.Lock()
	b.hidden = hidden
	b.mu.Unlock()
}

// IsHidden 返回当前组件的隐藏状态。线程安全。
func (b *ComponentBase) IsHidden() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.hidden
}

// buildPrefix 根据 widget 和 level 计算第一行前缀和后续行对齐前缀。
// 挂件（❯/⏺/❄）仅出现在首行，后续行用空格占位对齐。
func buildPrefix(w Widget, level int) (firstLinePrefix, alignPrefix string) {
	var ww int
	if w != nil {
		firstLinePrefix = w.Render() + " "
		ww = w.Width() + 1 // 挂件 + 空格
	}
	if level > 0 {
		indent := strings.Repeat(" ", level)
		firstLinePrefix += indent + grayBranch + "  "
		// 对齐：挂件(ww) + indent(level) + ⎿(1) + space(2) = ww + level + 3
		alignPrefix = strings.Repeat(" ", level+ww+3)
	} else if w != nil {
		// 对齐：挂件(ww) + space(1) = ww+1
		alignPrefix = strings.Repeat(" ", ww)
	}
	return
}

// ApplyPrefix 对 lines 应用挂件前缀和层级缩进，返回新切片。
// 当 widget==nil 且 level==0 时直接返回原切片（零分配）。
func ApplyPrefix(lines []string, w Widget, level int) []string {
	if w == nil && level == 0 {
		return lines
	}
	firstPrefix, alignPrefix := buildPrefix(w, level)
	result := make([]string, len(lines))
	for i, line := range lines {
		if i == 0 {
			result[i] = firstPrefix + line
		} else {
			result[i] = alignPrefix + line
		}
	}
	return result
}

// BackgroundProvider 是组件提供背景色信息的接口。
type BackgroundProvider interface {
	BackgroundColor() string
	FullWidth() int
}

// FillBackground 对 line 应用背景色 bgColor 并填充到 fullWidth 宽度。
func FillBackground(line string, bgColor string, fullWidth int) string {
	if bgColor == "" || fullWidth <= 0 {
		return line
	}
	line = bgColor + strings.ReplaceAll(line, "\033[0m", "\033[0m"+bgColor)
	w := types.VisualWidth(line)
	if fullWidth > w {
		line += strings.Repeat(" ", fullWidth-w)
	}
	line += types.Reset
	return line
}
