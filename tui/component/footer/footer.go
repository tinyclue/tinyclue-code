// Package footer 提供底部信息组件 FooterComponent，显示会话用量和快捷键提示。
package footer

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// FooterComponent 显示会话 token 用量、上下文占用和快捷键提示。
type FooterComponent struct {
	*component.ComponentBase
	interruptedNano atomic.Int64 // 首次 Ctrl+C 的 UnixNano，0 表示非等待态；用于 500ms 双击退出提示
	revertTimer     *time.Timer  // RevertTimeout 到期恢复提示文字
	requestRender   func()       // 触发 TUI 重绘的回调
	data            FooterData
}

type FooterData struct {
	DefaultModel    string
	ReasoningEffort string
	McpInfo         string // MCP server 状态摘要，如 "MCP 2/3"；空则不显示
	PlanMode        bool   // 计划模式是否开启（由 interactive 经 InteractiveOnProcess 进度链填充）

	Input       int
	Output      int
	CacheRead   int
	CacheWrite  int
	TotalTokens int
	CostTotal   float64

	ContextUsage   int     // 当前上下文已用 token（估算）
	ContextWindow  int     // 模型上下文窗口上限
	ContextPercent float64 // 上下文占用百分比，保留 2 位小数
}

// New 创建一个 FooterComponent。
func New() *FooterComponent {
	return &FooterComponent{
		ComponentBase: component.NewComponentBase(),
	}
}

func (f *FooterComponent) SetData(data FooterData) {
	f.data = data
}

func (f *FooterComponent) DoBefore(_ core.Data) error { return nil }

// SetRequestRender 设置触发 TUI 重绘的回调，由外部（interactive.go）注入。
func (f *FooterComponent) SetRequestRender(fn func()) {
	f.requestRender = fn
}

// DoUpdate 处理事件消息，检测 InterruptMsg 进入"再按 Ctrl+C 退出"等待态。
func (f *FooterComponent) DoUpdate(data core.Data) error {
	switch data.Msg.(type) {
	case core.InterruptMsg:
		f.interruptedNano.Store(time.Now().UnixNano())
		if f.revertTimer != nil {
			f.revertTimer.Stop()
		}
		f.revertTimer = time.AfterFunc(types.RevertTimeout, func() {
			f.interruptedNano.Store(0)
			if f.requestRender != nil {
				f.requestRender()
			}
		})
	}
	return nil
}

// promptIndent 是 footer 各行左侧缩进列数：与 editor 光标行文本左缘对齐。
// editor 输入前缀宽度 = promptWidget.Width() + 尾部空格 = 1 + 1 = 2（"> " 提示符），
// 用户消息的 "❯ " 前缀同为 2 列，缩进后 footer 与输入/聊天文本整体左对齐。
const promptIndent = 2

// Render 返回底部信息行：第 1 行用量数据 + 右对齐模型名，第 2 行 plan mode 状态常驻（⏸ on / ⏵ off），第 3 行快捷键提示。
func (f *FooterComponent) Render(_ core.Data) core.View {
	var lines []string
	d := f.data
	width := terminal.DefaultTerminalContext.GetWidth()

	indent := strings.Repeat(" ", promptIndent)

	// 第 1 行：用量 + MCP 状态（左侧），模型名右对齐（计划模式标记已移至第 2 行，不再拼在此处）。
	line1 := fmt.Sprintf("%s input, %s output, %s cache read, %s cache write ($%.2f) | %s/%s (%.2f%%)",
		formatCostTokens(d.Input), formatCostTokens(d.Output), formatCostTokens(d.CacheRead),
		formatCostTokens(d.CacheWrite), d.CostTotal,
		formatTokens(d.ContextUsage), formatTokens(d.ContextWindow), d.ContextPercent)
	if d.McpInfo != "" {
		line1 += " | " + d.McpInfo
	}
	modelName := d.DefaultModel + " • " + d.ReasoningEffort
	padding := width - promptIndent - types.VisualWidth(line1) - types.VisualWidth(modelName)
	if padding > 0 {
		line1 = indent + line1 + strings.Repeat(" ", padding) + modelName
	} else {
		// 一行放不下：只保留用量文本并截断到 width，不再拼 modelName（避免超宽折行错位）。
		line1 = indent + types.TruncateToWidth(line1, width-promptIndent, "")
	}
	lines = append(lines, types.FgWhite+line1+types.Reset)

	if d.PlanMode {
		lines = append(lines, indent+types.FgPlanMode+"⏸ plan mode on"+types.Reset)
	} else {
		lines = append(lines, indent+types.FgGray+"⏵ plan mode off"+types.Reset)
	}

	// 第 3 行：退出提示（双击 Ctrl+C 等待态保留）。
	exitHint := "Ctrl-C to exit"
	if f.interruptedNano.Load() != 0 {
		exitHint = "Press Ctrl-C again to exit"
	}
	lines = append(lines, indent+types.FgGray+exitHint+types.Reset)
	return core.View{Lines: lines}
}

// formatTokens 将 token 数格式化为可读单位：<1000 原样，<1000000 显示 K，≥1000000 显示 M。
func formatTokens(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.2fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%.0fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

// formatCostTokens 将 token 数格式化为可读显示（小写 k，一位小数），0 显示 "0"。
func formatCostTokens(n int) string {
	if n == 0 {
		return "0"
	}
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
