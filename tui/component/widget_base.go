package component

// Widget 是组件首行的可选前缀装饰挂件，渲染后占据固定视觉宽度。
type Widget interface {
	Render() string // 带 ANSI 的前缀字符（如 "\033[32m⏺\033[0m"）
	Width() int     // 视觉宽度（用于后续行对齐）
}

// WidgetBase 是 Widget 的嵌入基类，提供宽度默认值（1）。
type WidgetBase struct{}

// Width 返回默认视觉宽度 1。
func (WidgetBase) Width() int { return 1 }
