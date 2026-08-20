// Package welcome 提供欢迎界面组件 WelcomeComponent，显示在 header 区域。
package welcome

import (
	"strings"

	"github.com/mattn/go-runewidth"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// WelcomeComponent 绘制一个长方形欢迎框，宽度为终端宽度的 40%。
type WelcomeComponent struct {
	*component.ComponentBase
	shortCwd string // 显示用的当前工作目录（外部传入，如 "~/data/release/tinyclue"）
}

// New 创建一个 WelcomeComponent。
func New() *WelcomeComponent {
	return &WelcomeComponent{
		ComponentBase: component.NewComponentBase(),
	}
}

// SetShortCwd 设置显示用的当前工作目录，由外部传入（不要在组件内读取全局配置）。
func (w *WelcomeComponent) SetShortCwd(shortCwd string) {
	w.shortCwd = shortCwd
}

func (w *WelcomeComponent) DoBefore(_ core.Data) error { return nil }
func (w *WelcomeComponent) DoUpdate(_ core.Data) error { return nil }

// narrowWidth 以窄模式（EastAsianWidth=false）计算字符宽度，匹配现代终端行为。
// runewidth 默认将 ambiguous 字符（如 box-drawing、·）计为宽(2)，但终端实际渲染为窄(1)。
var narrowWidth = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// stripANSI 移除 ANSI 转义序列。
func stripANSI(s string) string {
	return types.AnsiPattern.ReplaceAllString(s, "")
}

// visualWidth 返回字符串去掉 ANSI 后的视觉宽度（基于窄模式）。
func visualWidth(s string) int {
	return narrowWidth.StringWidth(stripANSI(s))
}

// Render 返回欢迎框的每一行，右侧边框统一对齐到 boxWidth 的最后一列。
func (w *WelcomeComponent) Render(_ core.Data) core.View {
	termWidth := terminal.DefaultTerminalContext.GetWidth()
	boxWidth := max(int(float64(termWidth)*0.4), 40)

	dashW := visualWidth("─")

	var lines []string

	// ── 顶部边框 ──
	// ╭─── Tinyclue v1.0.0  ────────────────────╮
	titleLeft := "╭─── " + types.FgLightBlue + "Tinyclue Code" + types.FgDefault + " v1.0.0  "
	topDash := max((boxWidth-visualWidth(titleLeft)-visualWidth("╮"))/dashW, 0)
	lines = append(lines, padRight(titleLeft+strings.Repeat("─", topDash)+"╮", boxWidth))

	// ── 空行 ──
	lines = append(lines, boxLine("", boxWidth))

	// ── 欢迎语（居中加粗） ──
	welcomeText := types.Bold + "Welcome to Tinyclue Code!" + types.Reset
	lines = append(lines, boxLineCenter(welcomeText, boxWidth))

	// ── 空行 ──
	lines = append(lines, boxLine("", boxWidth))

	// ── 信息行（与顶部"Tinyclue"左对齐） ──
	infoText := types.FgGray + w.shortCwd + types.FgDefault
	lines = append(lines, boxLineIndent(infoText, visualWidth("╭─── "), boxWidth))

	// ── 空行 ──
	lines = append(lines, boxLine("", boxWidth))

	// ── 底部边框 ──
	botDash := max((boxWidth-visualWidth("╰")-visualWidth("╯"))/dashW, 0)
	lines = append(lines, padRight("╰"+strings.Repeat("─", botDash)+"╯", boxWidth))

	return core.View{Lines: lines}
}

// padRight 将 line 填充空格到 visual 宽度 boxWidth，填充在行末。
func padRight(line string, boxWidth int) string {
	if w := visualWidth(line); w < boxWidth {
		line += strings.Repeat(" ", boxWidth-w)
	}
	return line
}

// boxLine 返回 │<content>│ 行，content 左对齐，右侧填充到 boxWidth。
func boxLine(content string, boxWidth int) string {
	borderW := visualWidth("││")
	pad := max(boxWidth-borderW-visualWidth(content), 0)
	return "│" + content + strings.Repeat(" ", pad) + "│"
}

// boxLineCenter 返回 │<content>│ 行，content 居中。
func boxLineCenter(content string, boxWidth int) string {
	borderW := visualWidth("││")
	innerW := boxWidth - borderW
	cw := visualWidth(content)
	left := (innerW - cw) / 2
	right := innerW - cw - left
	return "│" + strings.Repeat(" ", left) + content + strings.Repeat(" ", right) + "│"
}

// boxLineIndent 返回 │<content>│ 行，content 从 indent 列开始左对齐。
func boxLineIndent(content string, indent int, boxWidth int) string {
	leftBorder := visualWidth("│")
	padding := max(indent-leftBorder, 0)
	return boxLine(strings.Repeat(" ", padding)+content, boxWidth)
}
