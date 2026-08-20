// Package text 提供多行文本组件 TextComponent，支持围栏代码块解析、行号显示、diff 高亮和语法高亮。
package text

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

// Segment 是文本的一个片段：普通文本或围栏代码块。
type Segment struct {
	Content string
	IsCode  bool   // true = 围栏代码块（含 ``` 标记），false = 普通文本
	IsDiff  bool   // true = ```diff 围栏代码块，需使用 diff 渲染器
	Lang    string // 代码语言，用于 syntax highlighting（如 "go"）
}

// TextComponent 实现 core.Component，显示多行文本，支持围栏代码块解析、行号显示、diff 高亮和语法高亮。
// 与 MarkdownComponent 不同，TextComponent 不处理 markdown 格式（粗体、标题等），只处理纯文本和围栏代码块。
type TextComponent struct {
	*component.ComponentBase
	*component.CollapsibleBase
	mu              sync.RWMutex
	text            string
	bgColor         string // ANSI 背景色序列，非空时每行渲染结果会被填充背景色并铺满终端宽度
	cachedFullWidth int    // FullWidth 缓存，Render 时设置
}

// New 创建一个 TextComponent。可选的 opts 函数用于配置（如设置层级、状态小圆点）。
func New(text string, opts ...func(*TextComponent)) *TextComponent {
	t := &TextComponent{
		ComponentBase:   component.NewComponentBase(),
		CollapsibleBase: component.NewCollapsibleBase(),
		text:            text,
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// UpdateText 替换组件内的文本。如果 err != nil，文本将显示为浅红色。线程安全。
func (t *TextComponent) UpdateText(text string, err error) {
	t.mu.Lock()
	if err != nil {
		t.text = types.FgLightRed + text + types.Reset
	} else {
		t.text = text
	}
	t.mu.Unlock()
}

// AddText 追加文本到组件末尾。线程安全。
func (t *TextComponent) AddText(text string) {
	t.mu.Lock()
	t.text += text
	t.mu.Unlock()
}

// Text 返回当前文本。线程安全。
func (t *TextComponent) Text() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.text
}

// SetBackground 设置整块文本的背景色。color 为 ANSI 背景序列（如 "\033[48;5;236m"），
// 空字符串表示不设背景。设置后每行渲染结果会被填充背景色并铺满终端宽度。
func (t *TextComponent) SetBackground(color string) {
	t.bgColor = color
}

func (t *TextComponent) DoBefore(data core.Data) error { return nil }
func (t *TextComponent) DoUpdate(data core.Data) error { return nil }

// Render 返回文本组件自身内容行。
func (t *TextComponent) Render(data core.Data) core.View {
	text := t.Text()
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

	// 拆分为片段并逐段渲染
	segs := ParseSegments(text)
	var allLines []string
	for _, seg := range segs {
		if seg.IsCode {
			if seg.IsDiff {
				lines := RenderDiffBlock(seg.Content, termWidth, seg.Lang)
				allLines = append(allLines, lines...)
			} else {
				lines := renderCodeBlock(seg.Content, seg.Lang, termWidth)
				allLines = append(allLines, lines...)
			}
		} else {
			lines := renderRegular(seg.Content, termWidth)
			allLines = append(allLines, lines...)
		}
	}

	// 缓存 FullWidth 给 BackgroundProvider
	t.cachedFullWidth = fullWidth

	// 折叠截断
	allLines = t.ApplyCollapse(allLines)
	return core.View{Lines: allLines}
}

func (t *TextComponent) BackgroundColor() string { return t.bgColor }
func (t *TextComponent) FullWidth() int          { return t.cachedFullWidth }

// ── 片段解析 ──

// ParseSegments 将文本拆分为普通文本和围栏代码块交替片段。
func ParseSegments(text string) []Segment {
	var segs []Segment
	var buf strings.Builder

	for {
		idx := strings.Index(text, "```")
		if idx < 0 {
			buf.WriteString(text)
			break
		}

		// ``` 之前的内容作为普通片段
		buf.WriteString(text[:idx])
		if buf.Len() > 0 {
			segs = append(segs, Segment{Content: buf.String()})
			buf.Reset()
		}

		// 找配对的结束 ```
		text = text[idx:]
		end := strings.Index(text[3:], "```")
		if end < 0 {
			// 没有闭合的围栏，剩余部分当作普通文本
			buf.WriteString(text)
			break
		}
		end += 6 // 开头的 ``` (3) + 结尾的 ``` (3)

		// 检测代码块语言，支持 ```diff <lang> 或 ```go 等格式
		firstNewline := strings.IndexByte(text[3:], '\n')
		var isDiff bool
		var codeLang string
		if firstNewline >= 0 {
			fields := strings.Fields(text[3 : 3+firstNewline])
			if len(fields) > 0 {
				if fields[0] == "diff" {
					isDiff = true
					if len(fields) > 1 {
						codeLang = fields[1] // e.g., "diff go" → "go"
					}
				} else {
					codeLang = fields[0] // e.g., "go", "python"
				}
			}
		}

		// 整个围栏代码块作为一个代码片段
		segs = append(segs, Segment{IsCode: true, IsDiff: isDiff, Lang: codeLang, Content: text[:end]})
		text = text[end:]
	}

	if buf.Len() > 0 {
		segs = append(segs, Segment{Content: buf.String()})
	}

	return segs
}

// ── 普通文本渲染 ──

// renderRegular 渲染普通文本段（不含围栏代码块）。按词折行到 termWidth，
// 保证每行可见宽度 ≤ termWidth（组件级 word-wrap，渲染器不再做物理行折算）。
func renderRegular(text string, termWidth int) []string {
	if text == "" {
		return []string{""}
	}
	return types.WrapTextWithAnsi(text, termWidth)
}

// ── 代码块渲染（带语法高亮） ──

// renderCodeBlock 渲染围栏代码块，使用 chroma 进行语法高亮，每行缩进 2 空格。
// 与 pi 一致：代码块超宽时按词折行而非截断（渲染器 crash guard 兜底）。
func renderCodeBlock(content string, lang string, termWidth int) []string {
	inner := ExtractCodeContent(content)
	if inner == "" {
		return nil
	}
	inner = strings.ReplaceAll(inner, "\t", "    ")
	lines := strings.Split(inner, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}

	// 语法高亮
	fullCode := strings.Join(lines, "\n")
	var hlLines []string
	if lang != "" && fullCode != "" {
		if hl := HighlightCode(fullCode, lang); hl != "" {
			hl = strings.ReplaceAll(hl, "\033[0m", "\033[39m")
			hlLines = strings.Split(hl, "\n")
		}
	}

	// 确保 hlLines 与 lines 行数一致
	if hlLines != nil {
		if len(hlLines) > len(lines) {
			hlLines = hlLines[:len(lines)]
		}
		for len(hlLines) < len(lines) {
			hlLines = append(hlLines, "")
		}
	}

	var result []string
	for i, line := range lines {
		display := line
		if hlLines != nil && hlLines[i] != "" {
			display = hlLines[i]
		}
		result = append(result, types.WrapTextWithAnsi("  "+display, termWidth)...)
	}
	return result
}

// ── Diff 代码块渲染（带行号、+/- 标记、绿/红背景、语法高亮） ──

// RenderDiffBlock 渲染围栏代码块中的 diff 内容（```diff 围栏）。
// 使用 chroma 对代码内容进行语法高亮，背景色覆盖整行终端宽度。
//
// 每行格式：
//   - +行：淡绿背景 + 深绿行号和 + 号，代码内容语法高亮
//   - -行：淡红背景 + 深红行号和 - 号，代码内容语法高亮
//   - 上下文行：灰色行号 + 3空格前缀，无背景色
func RenderDiffBlock(content string, termWidth int, lang string) []string {
	inner := ExtractCodeContent(content)
	if inner == "" {
		return nil
	}
	inner = strings.ReplaceAll(inner, "\t", "    ")

	lines := strings.Split(inner, "\n")

	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) == 0 {
		return nil
	}

	// 预解析每行的标记符和内容部分（跳过 "行号 标记符" 前缀）
	markers := make([]byte, len(lines))
	codeLines := make([]string, len(lines))
	for i, line := range lines {
		markers[i] = ' '
		codeLines[i] = line
		if line == "" {
			continue
		}
		// 跳过前导空格找到数字部分
		j := 0
		for j < len(line) && line[j] == ' ' {
			j++
		}
		// 跳过数字
		for j < len(line) && line[j] >= '0' && line[j] <= '9' {
			j++
		}
		// j 指向分隔空格，标记符在 j+1
		if j+1 < len(line) {
			markers[i] = line[j+1]
			codeLines[i] = line[j+2:]
		}
	}
	fullCode := strings.Join(codeLines, "\n")

	// 语法高亮
	var hlLines []string
	if lang != "" && fullCode != "" {
		if hl := HighlightCode(fullCode, lang); hl != "" {
			hl = strings.ReplaceAll(hl, "\033[0m", "\033[39m")
			hlLines = strings.Split(hl, "\n")
		}
	}

	// 确保 hlLines 与 lines 行数一致
	if hlLines != nil {
		if len(hlLines) > len(lines) {
			hlLines = hlLines[:len(lines)]
		}
		for len(hlLines) < len(lines) {
			hlLines = append(hlLines, "")
		}
	}

	// 固定宽度：每行背景统一填充到 termWidth 宽度
	padTarget := termWidth

	var result []string
	for idx, line := range lines {
		marker := markers[idx]
		codeContent := codeLines[idx]
		if line == "" {
			result = append(result, strings.Repeat(" ", padTarget))
			continue
		}

		// 前缀（行号 + 标记符）宽度，内容预算 = 剩余宽度。超宽内容按词折行，
		// 折出的每一段都以完整前缀 + 背景填充输出，保证整行 ≤ termWidth。
		prefix := line[:len(line)-len(codeContent)]
		pLen := types.VisualWidth(prefix)
		budget := max(1, padTarget-pLen-1)
		codeDisplay := codeContent
		if hlLines != nil && hlLines[idx] != "" {
			codeDisplay = hlLines[idx]
		}
		parts := types.WrapTextWithAnsi(codeDisplay, budget)
		if len(parts) == 0 {
			parts = []string{""}
		}

		for _, part := range parts {
			dLen := types.VisualWidth(part)
			padLen := padTarget - pLen - 1 - dLen

			switch marker {
			case '+':
				var b strings.Builder
				b.WriteString(types.BgGreen)
				b.WriteString(types.FgLightGreen)
				b.WriteString(prefix)
				b.WriteString(types.FgDefault)
				b.WriteString(" ")
				b.WriteString(part)
				if padLen > 0 {
					b.WriteString(types.BgGreen)
					b.WriteString(strings.Repeat(" ", padLen))
				}
				b.WriteString(types.Reset)
				result = append(result, b.String())

			case '-':
				var b strings.Builder
				b.WriteString(types.BgRed)
				b.WriteString(types.FgLightRed)
				b.WriteString(prefix)
				b.WriteString(types.FgDefault)
				b.WriteString(" ")
				b.WriteString(part)
				if padLen > 0 {
					b.WriteString(types.BgRed)
					b.WriteString(strings.Repeat(" ", padLen))
				}
				b.WriteString(types.Reset)
				result = append(result, b.String())

			default:
				// 上下文行（标记符为空格）
				if padLen > 0 {
					result = append(result, fmt.Sprintf("%s%s%s %s%s", types.FgGray, prefix, types.Reset, part, strings.Repeat(" ", padLen)))
				} else {
					result = append(result, fmt.Sprintf("%s%s%s %s", types.FgGray, prefix, types.Reset, part))
				}
			}
		}
	}
	return result
}

// HighlightCode 使用 chroma 对代码进行语法高亮，返回 ANSI 格式的文本。
func HighlightCode(code, lang string) string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		return ""
	}
	iterator, err := lexer.Tokenise(nil, code)
	if err != nil {
		return ""
	}

	var buf strings.Builder
	style := chromastyles.Get("monokai")
	if err := formatters.TTY256.Format(&buf, style, iterator); err != nil {
		return ""
	}
	return buf.String()
}

// ExtractCodeContent 从围栏代码块中提取纯文本内容（去掉围栏标记和语言声明）。
func ExtractCodeContent(fenced string) string {
	_, inner, found := strings.Cut(fenced, "\n")
	if !found {
		return ""
	}
	last := strings.LastIndex(inner, "```")
	if last < 0 {
		return inner
	}
	return inner[:last]
}
