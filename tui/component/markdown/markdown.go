// Package markdown 提供 MarkdownComponent，使用 glamour 将 markdown 渲染为带 ANSI 样式的终端文本。
package markdown

import (
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"

	"github.com/tinyclue/tinyclue-code/tui/component"
	textcomp "github.com/tinyclue/tinyclue-code/tui/component/text"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// 颜色常量已统一在 tui/types/consts.go 中定义

// MarkdownComponent 实现 core.Component，用 glamour 渲染 markdown 内容。
// 支持流式更新：外部调用 UpdateText() 替换内容后触发 ReqeustRender() 即可。
// Render() 内部会全量重解析 markdown，由 TUI 框架的 diffRender 保证终端级增量输出。
// 嵌入了 ComponentBase，支持层级缩进和子组件。
type MarkdownComponent struct {
	*component.ComponentBase
	*component.CollapsibleBase
	mu              sync.RWMutex
	text            string
	bgColor         string // ANSI 背景色序列，非空时整块 Markdown 将铺满该背景色
	cachedFullWidth int    // FullWidth 缓存，Render 时设置
}

// SetBackground 设置整块 Markdown 的背景色。color 为 ANSI 背景序列（如 "\033[48;5;236m"），
// 空字符串表示不设背景。设置后每行渲染结果会被填充背景色并铺满终端宽度。
func (m *MarkdownComponent) SetBackground(color string) {
	m.bgColor = color
}

// New 创建一个 MarkdownComponent，默认 80 列宽度。
func New() *MarkdownComponent {
	return &MarkdownComponent{
		ComponentBase:   component.NewComponentBase(),
		CollapsibleBase: component.NewCollapsibleBase(),
	}
}

// UpdateText 替换组件内的 markdown 文本。线程安全。
func (m *MarkdownComponent) UpdateText(text string) {
	m.mu.Lock()
	m.text = text
	m.mu.Unlock()
}

func (m *MarkdownComponent) AddText(text string) {
	m.mu.Lock()
	m.text += text
	m.mu.Unlock()
}

// Text 返回当前 markdown 文本。线程安全。
func (m *MarkdownComponent) Text() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.text
}

func (m *MarkdownComponent) DoBefore(data core.Data) error { return nil }
func (m *MarkdownComponent) DoUpdate(data core.Data) error { return nil }

// Render 将 markdown 渲染为带 ANSI 样式的终端行，只返回自身内容（不含子组件渲染）。
func (m *MarkdownComponent) Render(data core.Data) core.View {
	text := m.Text()
	if text == "" {
		return core.View{}
	}

	termWidth := terminal.DefaultTerminalContext.GetWidth()
	fullWidth := termWidth // 用于背景填充，不做缩进
	if termWidth < 10 {
		termWidth = 10
	} else {
		termWidth = termWidth - 10
	}

	// 使用自定义样式：基于 dark 主题，去掉 heading 前缀和左边距
	cfg := styles.DarkStyleConfig
	cfg.Document.Margin = nil
	cfg.Paragraph = ansi.StyleBlock{} // 去掉默认的 3 空格段落缩进
	margin2 := uint(2)
	cfg.CodeBlock.Margin = &margin2 // 代码块缩进 2 空格

	// 反引号内联代码：蓝色字，无灰底
	colorBlue := "153"
	cfg.Code.Color = &colorBlue
	cfg.Code.BackgroundColor = nil
	cfg.Code.Prefix = ""
	cfg.Code.Suffix = ""
	cfg.H1.StylePrimitive.Prefix = ""
	cfg.H2.StylePrimitive.Prefix = ""
	cfg.H3.StylePrimitive.Prefix = ""
	cfg.H4.StylePrimitive.Prefix = ""
	cfg.H5.StylePrimitive.Prefix = ""
	cfg.H6.StylePrimitive.Prefix = ""

	// 删除线：加 ~~ 包裹作为可见回退（部分终端不渲染 CrossedOut 属性）
	cfg.Strikethrough.BlockPrefix = "~~"
	cfg.Strikethrough.BlockSuffix = "~~"

	// 任务列表：[✓] → [x]，用 ANSI 转义序列区分颜色
	cfg.Task.Ticked = types.FgGreen + "[x]" + types.Reset + " "
	cfg.Task.Unticked = types.FgGray + "[ ]" + types.Reset + " "
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(cfg),
		glamour.WithWordWrap(termWidth),
	)
	if err != nil {
		return core.View{Lines: strings.Split(text, "\n")}
	}

	// 拆分为片段并逐段渲染
	segs := textcomp.ParseSegments(text)
	var allLines []string
	for _, seg := range segs {
		if seg.IsCode {
			if seg.IsDiff {
				lines := textcomp.RenderDiffBlock(seg.Content, termWidth, seg.Lang)
				allLines = append(allLines, lines...)
			} else {
				lines := renderCodeBlock(r, seg.Content)
				allLines = append(allLines, lines...)
			}
		} else {
			lines := renderRegular(r, seg.Content)
			allLines = append(allLines, lines...)
		}
	}

	// 缓存 FullWidth 给 BackgroundProvider
	m.cachedFullWidth = fullWidth

	// 折叠截断
	allLines = m.ApplyCollapse(allLines)
	return core.View{Lines: allLines}
}
func (m *MarkdownComponent) BackgroundColor() string { return m.bgColor }

func (m *MarkdownComponent) FullWidth() int { return m.cachedFullWidth }

// ── 普通 markdown 渲染 ──

// renderRegular 渲染普通 markdown 文本（不含围栏代码块）。
// 对非表格内容应用 \n → \n\n 使单换行可见换行；
// 表格行（以 | 开头）之间保留 \n 不插入空行，确保 glamour 正确识别表格。
func renderRegular(r *glamour.TermRenderer, text string) []string {
	if text == "" {
		return nil
	}

	processed := processTextWithTables(text)

	out, err := r.Render(processed)
	if err != nil {
		return strings.Split(text, "\n")
	}

	out = strings.Trim(out, "\n")
	if out == "" {
		return nil
	}

	lines := strings.Split(out, "\n")
	var allLines []string
	for _, line := range lines {
		if line == "" || strings.TrimSpace(types.AnsiPattern.ReplaceAllString(line, "")) != "" {
			allLines = append(allLines, line)
		}
	}
	return allLines
}

// isTableRow 判断一行是否为表格行（去除首尾空格后以 | 开头）。
func isTableRow(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "|")
}

// processTextWithTables 对文本做换行处理：
//   - 非表格内容：\n → \n\n（使单换行产生可见段落断行）
//   - 表格行（连续以 | 开头的行）：保留单 \n，不插入空行
//
// 返回处理后的文本，可以直接交给 glamour 渲染。
func processTextWithTables(text string) string {
	lines := strings.Split(text, "\n")
	var buf strings.Builder

	i := 0
	for i < len(lines) {
		if isTableRow(lines[i]) {
			// 收集连续表格行
			rowStart := i
			for i < len(lines) && isTableRow(lines[i]) {
				i++
			}
			rowEnd := i

			for j := rowStart; j < rowEnd; j++ {
				buf.WriteString(lines[j])
				if j+1 < rowEnd {
					buf.WriteByte('\n')
				}
			}
			buf.WriteString("\n\n") // 表格后段落间距
		} else if lines[i] == "" {
			// 跳过空行——上一行已通过 \n\n 产生段落间距
			i++
		} else {
			buf.WriteString(lines[i])
			buf.WriteString("\n\n")
			i++
		}
	}

	return strings.TrimRight(buf.String(), "\n")
}

// ── 代码块渲染 ──

// renderCodeBlock 渲染围栏代码块，使用 glamour 进行语法高亮。
// content 应包含 ``` 围栏标记。结果每行缩进 2 空格，不带行号和 │。
func renderCodeBlock(r *glamour.TermRenderer, content string) []string {
	out, err := r.Render(content)
	if err != nil {
		// fallback：取围栏内的纯文本
		inner := textcomp.ExtractCodeContent(content)
		if inner == "" {
			return nil
		}
		lines := strings.Split(inner, "\n")
		for i, line := range lines {
			lines[i] = "  " + line
		}
		return lines
	}

	out = strings.Trim(out, "\n")
	if out == "" {
		return nil
	}

	// 清理首尾 ANSI 填充行（glamour 在代码块周围添加的空白填充），保留中间空行
	lines := strings.Split(out, "\n")
	for len(lines) > 0 {
		line := lines[0]
		visible := strings.TrimSpace(types.AnsiPattern.ReplaceAllString(line, ""))
		if line == "" || visible == "" {
			lines = lines[1:]
		} else {
			break
		}
	}
	for len(lines) > 0 {
		line := lines[len(lines)-1]
		visible := strings.TrimSpace(types.AnsiPattern.ReplaceAllString(line, ""))
		if line == "" || visible == "" {
			lines = lines[:len(lines)-1]
		} else {
			break
		}
	}
	if len(lines) == 0 {
		return nil
	}

	// 每行缩进 2 空格（pi 兼容风格）
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return lines
}
