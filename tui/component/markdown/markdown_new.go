// Package markdown 提供 MarkdownNewComponent，使用 goldmark 解析 markdown 后逐 token 渲染为 ANSI 样式终端文本。
// 相比 MarkdownComponent（基于 glamour），此组件完全自主控制每个 token 类型的渲染样式，
// 特别是表格使用盒式边框字符（┌─┬┐）、引用块使用灰色竖线 + 斜体等
package markdown

import (
	"fmt"
	"strings"
	"sync"

	"github.com/tinyclue/tinyclue-code/tui/component"
	textcomp "github.com/tinyclue/tinyclue-code/tui/component/text"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	tableast "github.com/yuin/goldmark/extension/ast"
	gmtext "github.com/yuin/goldmark/text"
)

// MarkdownNewComponent 实现 core.Component，使用 goldmark + 自定义 token 渲染器渲染 markdown。
// 支持流式更新：外部调用 UpdateText() 替换内容后触发 ReqeustRender() 即可。
// 嵌入了 ComponentBase，支持层级缩进和子组件。
type MarkdownNewComponent struct {
	*component.ComponentBase
	*component.CollapsibleBase
	mu              sync.RWMutex
	mdText          string
	bgColor         string // ANSI 背景色序列，非空时每行渲染结果会被填充背景色并铺满终端宽度
	cachedFullWidth int    // FullWidth 缓存，Render 时设置
}

// SetBackground 设置整块文本的背景色。color 为 ANSI 背景序列（如 "\033[48;5;236m"），
// 空字符串表示不设背景。设置后每行渲染结果会被填充背景色并铺满终端宽度。
func (m *MarkdownNewComponent) SetBackground(color string) {
	m.bgColor = color
}

// NewV2 创建一个 MarkdownNewComponent。
func NewV2() *MarkdownNewComponent {
	return &MarkdownNewComponent{
		ComponentBase:   component.NewComponentBase(),
		CollapsibleBase: component.NewCollapsibleBase(),
	}
}

// UpdateText 替换组件内的 markdown 文本。线程安全。
func (m *MarkdownNewComponent) UpdateText(text string) {
	m.mu.Lock()
	m.mdText = text
	m.mu.Unlock()
}

// AddText 追加文本到组件末尾。线程安全。
func (m *MarkdownNewComponent) AddText(text string) {
	m.mu.Lock()
	m.mdText += text
	m.mu.Unlock()
}

// Text 返回当前 markdown 文本。线程安全。
func (m *MarkdownNewComponent) Text() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.mdText
}

func (m *MarkdownNewComponent) DoBefore(data core.Data) error { return nil }
func (m *MarkdownNewComponent) DoUpdate(data core.Data) error { return nil }

// Render 将 markdown 解析为 AST 后逐 token 渲染为 ANSI 终端行。
func (m *MarkdownNewComponent) Render(data core.Data) core.View {
	mdText := m.Text()
	if mdText == "" {
		return core.View{}
	}

	termWidth := terminal.DefaultTerminalContext.GetWidth()
	fullWidth := termWidth // 用于背景填充，不做缩进
	if termWidth < 10 {
		termWidth = 10
	} else {
		termWidth = termWidth - 10
	}

	// 使用 goldmark GFM 解析
	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
	)
	source := []byte(mdText)
	doc := md.Parser().Parse(gmtext.NewReader(source))

	r := &mdRenderer{
		termWidth: termWidth,
		source:    source,
	}
	r.renderBlocks(doc)

	allLines := r.lines

	// 缓存 FullWidth 给 BackgroundProvider
	m.cachedFullWidth = fullWidth

	// 折叠截断
	allLines = m.ApplyCollapse(allLines)
	return core.View{Lines: allLines}
}

func (m *MarkdownNewComponent) BackgroundColor() string { return m.bgColor }
func (m *MarkdownNewComponent) FullWidth() int          { return m.cachedFullWidth }

// ── goldmark AST 渲染器 ──

// mdRenderer 遍历 goldmark AST 并生成 ANSI 行。
type mdRenderer struct {
	lines     []string
	termWidth int
	source    []byte
	listDepth int // 嵌套列表深度
}

// renderBlocks 渲染块级子节点，块之间自动插入空行分隔。
func (r *mdRenderer) renderBlocks(parent ast.Node) {
	child := parent.FirstChild()
	for child != nil {
		r.renderBlock(child)
		child = child.NextSibling()
		if child != nil && !isInlineContainer(child) {
			r.lines = append(r.lines, "")
		}
	}
}

// isInlineContainer 判断该块不自带空行分隔符。
func isInlineContainer(n ast.Node) bool {
	switch n.(type) {
	case *tableast.Table, *ast.List, *ast.ListItem:
		return true
	}
	return false
}

// renderBlock 渲染单个块级节点。
func (r *mdRenderer) renderBlock(node ast.Node) {
	switch n := node.(type) {
	case *ast.Paragraph:
		r.renderParagraph(n)
	case *ast.Heading:
		r.renderHeading(n)
	case *ast.FencedCodeBlock:
		r.renderFencedCodeBlock(n)
	case *ast.CodeBlock:
		r.renderIndentedCodeBlock(n)
	case *ast.List:
		r.renderList(n)
	case *tableast.Table:
		r.renderTable(n)
	case *ast.Blockquote:
		r.renderBlockquote(n)
	case *ast.ThematicBreak:
		r.lines = append(r.lines, "---")
	default:
		for child := node.FirstChild(); child != nil; child = child.NextSibling() {
			r.renderBlock(child)
		}
	}
}

// ── 段落渲染 ──

// renderParagraph 收集子内联节点 → wordwrap → 追加行。
func (r *mdRenderer) renderParagraph(n *ast.Paragraph) {
	inlineText := r.renderInline(n)
	if inlineText == "" {
		return
	}
	r.lines = append(r.lines, types.WrapTextWithAnsi(inlineText, r.termWidth)...)
}

// ── 标题渲染 ──

const (
	styleBold      = "\033[1m"
	styleDim       = "\033[2m"
	styleItalic    = "\033[3m"
	styleUnderline = "\033[4m"
	resetBold      = "\033[22m" // also resets dim
	resetItalic    = "\033[23m"
	resetUnderline = "\033[24m"
)

// renderHeading 渲染标题。
//   - H1: bold + italic + underline
//   - H2–H6: bold
func (r *mdRenderer) renderHeading(n *ast.Heading) {
	inlineText := r.renderInline(n)
	if inlineText == "" {
		inlineText = " "
	}
	var styled string
	if n.Level == 1 {
		styled = styleBold + styleItalic + styleUnderline + inlineText + resetUnderline + resetItalic + resetBold
	} else {
		styled = styleBold + inlineText + resetBold
	}
	r.lines = append(r.lines, types.WrapTextWithAnsi(styled, r.termWidth)...)
}

// ── 代码块渲染 ──

// renderFencedCodeBlock 渲染围栏代码块。优先复用 text 包的 HighlightCode/RenderDiffBlock。
func (r *mdRenderer) renderFencedCodeBlock(n *ast.FencedCodeBlock) {
	lang := string(n.Language(r.source))
	codeText := collectCodeText(n, r.source)

	if codeText == "" {
		return
	}
	codeText = strings.ReplaceAll(codeText, "\t", "    ")
	codeText = strings.TrimRight(codeText, "\n")

	// diff 块：复用 text.RenderDiffBlock（需重建含 ``` 标记的输入）
	if lang == "diff" {
		fencedContent := "```" + lang + "\n" + codeText + "```"
		lines := textcomp.RenderDiffBlock(fencedContent, r.termWidth, "")
		r.lines = append(r.lines, lines...)
		return
	}

	// 语法高亮
	codeLines := strings.Split(codeText, "\n")
	if lang != "" {
		hl := textcomp.HighlightCode(codeText, lang)
		if hl != "" {
			hl = strings.ReplaceAll(hl, "\033[0m", "\033[39m")
			hlLines := strings.Split(hl, "\n")
			// hlLines 可能与 codeLines 行数不一致，逐行对齐
			for i, line := range codeLines {
				if i < len(hlLines) && hlLines[i] != "" {
					r.lines = append(r.lines, r.codeLine(hlLines[i])...)
				} else {
					r.lines = append(r.lines, r.codeLine(line)...)
				}
			}
			return
		}
	}

	// 纯文本 fallback
	for _, line := range codeLines {
		r.lines = append(r.lines, r.codeLine(line)...)
	}
}

// renderIndentedCodeBlock 渲染缩进代码块（无语言信息，纯文本）。
func (r *mdRenderer) renderIndentedCodeBlock(n *ast.CodeBlock) {
	codeText := collectCodeText(n, r.source)
	if codeText == "" {
		return
	}
	codeText = strings.TrimRight(codeText, "\n")
	for _, line := range strings.Split(codeText, "\n") {
		r.lines = append(r.lines, r.codeLine(line)...)
	}
}

// codeLine 渲染一行代码：2 空格缩进 + 折行到 termWidth。
// 与 pi 一致：代码块超宽时按词折行而非截断（渲染器 crash guard 兜底）。
func (r *mdRenderer) codeLine(line string) []string {
	return types.WrapTextWithAnsi("  "+line, r.termWidth)
}

// collectCodeText 从黄金标记线集合中收集代码文本。
// goldmark 的 BaseBlock.Lines() 返回 Segments，每个 Segment 是源中的位置范围。
func collectCodeText(n ast.Node, source []byte) string {
	var buf strings.Builder
	lines := n.Lines()
	if lines == nil {
		return ""
	}
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		buf.Write(seg.Value(source))
	}
	return buf.String()
}

// ── 列表渲染 ──

// renderList 渲染有序/无序列表。
func (r *mdRenderer) renderList(n *ast.List) {
	r.listDepth++
	idx := n.Start
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		if li, ok := child.(*ast.ListItem); ok {
			r.renderListItem(li, idx)
			if n.Start != 0 {
				idx++
			}
		}
	}
	r.listDepth--
}

// renderListItem 渲染单个列表项，支持嵌套列表和段落。
func (r *mdRenderer) renderListItem(n *ast.ListItem, idx int) {
	marker := listMarker(r.listDepth, idx)
	indent := strings.Repeat("  ", r.listDepth)

	var itemLines []string
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Paragraph:
			text := r.renderInline(c)
			if text != "" {
				itemLines = append(itemLines, text)
			}
		case *ast.List:
			subLines := r.collectListLines(c)
			itemLines = append(itemLines, subLines...)
		default:
			text := r.renderInline(c)
			if text != "" {
				itemLines = append(itemLines, text)
			}
		}
	}

	firstPrefix := indent + marker + " "
	contPrefix := indent + "   "
	wrapWidth := r.termWidth - max(types.VisualWidth(firstPrefix), types.VisualWidth(contPrefix))
	for i, line := range itemLines {
		wrappedLines := types.WrapTextWithAnsi(line, wrapWidth)
		for j, wl := range wrappedLines {
			if i == 0 && j == 0 {
				r.lines = append(r.lines, firstPrefix+wl)
			} else {
				r.lines = append(r.lines, contPrefix+wl)
			}
		}
	}
}

// collectListLines 收集嵌套列表的渲染行。
func (r *mdRenderer) collectListLines(n *ast.List) []string {
	saved := r.lines
	r.lines = nil
	r.renderList(n)
	result := r.lines
	r.lines = saved
	return result
}

// listMarker 返回列表标记符。有序用数字，无序用 -。
func listMarker(depth, idx int) string {
	if idx > 0 {
		switch depth {
		case 1:
			return fmt.Sprintf("%d.", idx)
		case 2:
			return fmt.Sprintf("%c.", 'a'+idx-1)
		default:
			return fmt.Sprintf("%d.", idx)
		}
	}
	return "-"
}

// ── 引用块渲染 ──

// renderBlockquote 渲染引用块。每行前加灰色竖线 ▎ + 斜体。
func (r *mdRenderer) renderBlockquote(n *ast.Blockquote) {
	bar := styleDim + "\u258e" + resetBold
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *ast.Paragraph:
			text := r.renderInline(c)
			if text == "" {
				continue
			}
			for _, line := range types.WrapTextWithAnsi(text, r.termWidth-2) {
				r.lines = append(r.lines, bar+" "+styleItalic+line+resetItalic)
			}
		default:
			r.renderBlock(c)
			if len(r.lines) > 0 {
				last := len(r.lines) - 1
				lastLine := r.lines[last]
				if strings.TrimSpace(lastLine) == "" {
					r.lines[last] = lastLine
				} else {
					r.lines[last] = bar + " " + lastLine
				}
			}
		}
	}
}

// ── 表格渲染（盒式边框） ──

// renderTable 渲染 GFM 表格，使用盒式边框字符。
func (r *mdRenderer) renderTable(n *tableast.Table) {
	alignments := n.Alignments
	if len(alignments) == 0 {
		return
	}

	// 收集表头和数据行
	var headers []string
	var rows [][]string
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch c := child.(type) {
		case *tableast.TableHeader:
			headers = r.collectTableRowCells(c)
		case *tableast.TableRow:
			row := r.collectTableRowCells(c)
			if len(row) > 0 {
				rows = append(rows, row)
			}
		}
	}

	if len(headers) == 0 {
		return
	}

	// 计算列宽
	numCols := len(alignments)
	if numCols > len(headers) {
		numCols = len(headers)
	}
	colWidths := make([]int, numCols)
	for i := 0; i < numCols; i++ {
		if i < len(headers) {
			colWidths[i] = types.VisualWidth(headers[i])
		}
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < numCols {
				w := types.VisualWidth(cell)
				if w > colWidths[i] {
					colWidths[i] = w
				}
			}
		}
	}
	for i := range colWidths {
		if colWidths[i] < 3 {
			colWidths[i] = 3
		}
	}

	// 总宽超出 termWidth 时逐列削减最宽列，保证整表 ≤ termWidth（列宽用窄模式，
	// 超出的 cell 由 centerText/alignCell 截断）。
	avail := r.termWidth - 1 - 2*numCols
	if avail < numCols {
		avail = numCols
	}
	for {
		total := 0
		for _, w := range colWidths {
			total += w
		}
		if total <= avail {
			break
		}
		maxIdx := 0
		for i := range colWidths {
			if colWidths[i] > colWidths[maxIdx] {
				maxIdx = i
			}
		}
		if colWidths[maxIdx] <= 1 {
			break
		}
		colWidths[maxIdx]--
	}

	// 顶边框
	top := "┌"
	for i, w := range colWidths {
		if i > 0 {
			top += "┬"
		}
		top += strings.Repeat("─", w+2)
	}
	top += "┐"
	r.lines = append(r.lines, top)

	// 表头行
	headerLine := "│"
	for i := 0; i < numCols; i++ {
		h := ""
		if i < len(headers) {
			h = headers[i]
		}
		headerLine += " " + centerText(h, colWidths[i]) + " │"
	}
	r.lines = append(r.lines, headerLine)

	// 表头分隔线
	sep := "├"
	for i, w := range colWidths {
		if i > 0 {
			sep += "┼"
		}
		sep += strings.Repeat("─", w+2)
	}
	sep += "┤"
	r.lines = append(r.lines, sep)

	// 数据行
	for _, row := range rows {
		line := "│"
		for i := 0; i < numCols; i++ {
			cell := ""
			if i < len(row) {
				cell = row[i]
			}
			align := tableast.AlignNone
			if i < len(alignments) {
				align = alignments[i]
			}
			line += " " + alignCell(cell, colWidths[i], align) + " │"
		}
		r.lines = append(r.lines, line)
	}

	// 底边框
	bottom := "└"
	for i, w := range colWidths {
		if i > 0 {
			bottom += "┴"
		}
		bottom += strings.Repeat("─", w+2)
	}
	bottom += "┘"
	r.lines = append(r.lines, bottom)
}

// collectTableRowCells 从 TableHeader 或 TableRow 节点收集单元格内容。
func (r *mdRenderer) collectTableRowCells(node ast.Node) []string {
	var cells []string
	for child := node.FirstChild(); child != nil; child = child.NextSibling() {
		if tc, ok := child.(*tableast.TableCell); ok {
			text := r.renderInline(tc)
			cells = append(cells, text)
		}
	}
	return cells
}

// ── 内联渲染 ──

// renderInline 递归渲染内联节点，返回 ANSI 字符串。
func (r *mdRenderer) renderInline(parent ast.Node) string {
	var parts []string
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		parts = append(parts, r.renderInlineNode(child))
	}
	return strings.Join(parts, "")
}

// renderInlineNode 渲染单个内联节点。
func (r *mdRenderer) renderInlineNode(node ast.Node) string {
	switch n := node.(type) {
	case *ast.Text:
		return r.renderText(n)
	case *ast.String:
		return string(n.Value)
	case *ast.Emphasis:
		return r.renderEmphasis(n)
	case *ast.CodeSpan:
		return r.renderCodeSpan(n)
	case *ast.Link:
		return r.renderLink(n)
	case *ast.AutoLink:
		return r.renderAutoLink(n)
	case *ast.Image:
		return r.renderImage(n)
	case *ast.RawHTML:
		return ""
	case *tableast.Strikethrough:
		return r.renderInline(n)
	case *tableast.TaskCheckBox:
		if n.IsChecked {
			return types.FgGreen + "[x]" + types.Reset + " "
		}
		return types.FgGray + "[ ]" + types.Reset + " "
	default:
		return r.renderInline(n)
	}
}

// renderText 渲染纯文本，处理软/硬换行。
func (r *mdRenderer) renderText(n *ast.Text) string {
	val := string(n.Value(r.source))
	if n.SoftLineBreak() || n.HardLineBreak() {
		if n.HardLineBreak() {
			return val + "\n"
		}
		val = strings.TrimRight(val, "\n") + " "
	}
	return val
}

// renderEmphasis 渲染 Emphasis。Level=1 → 斜体，Level=2 → 粗体。
func (r *mdRenderer) renderEmphasis(n *ast.Emphasis) string {
	inner := r.renderInline(n)
	if inner == "" {
		return ""
	}
	if n.Level == 2 {
		return styleBold + inner + resetBold
	}
	return styleItalic + inner + resetItalic
}

// renderCodeSpan 渲染行内代码（反引号）。浅蓝色字，无额外空格。
func (r *mdRenderer) renderCodeSpan(n *ast.CodeSpan) string {
	codeText := collectCodeSpanText(n, r.source)
	if codeText == "" {
		return ""
	}
	return "\033[38;5;153m" + codeText + types.Reset
}

// collectCodeSpanText 收集 CodeSpan 内的文本。
func collectCodeSpanText(n *ast.CodeSpan, source []byte) string {
	var parts []string
	for child := n.FirstChild(); child != nil; child = child.NextSibling() {
		switch t := child.(type) {
		case *ast.Text:
			parts = append(parts, string(t.Value(source)))
		case *ast.String:
			parts = append(parts, string(t.Value))
		}
	}
	return strings.Join(parts, "")
}

// renderLink 渲染链接。只显示 URL（不显示标题文字），默认终端颜色。
func (r *mdRenderer) renderLink(n *ast.Link) string {
	url := string(n.Destination)
	if url == "" {
		url = r.renderInline(n)
	}
	return url
}

// renderAutoLink 渲染自动链接。默认终端颜色。
func (r *mdRenderer) renderAutoLink(n *ast.AutoLink) string {
	return string(n.URL(r.source))
}

// renderImage 渲染图片。只显示 URL（无颜色修饰）
func (r *mdRenderer) renderImage(n *ast.Image) string {
	return string(n.Destination)
}

// ── 工具函数 ──

// centerText 将文本居中填充到指定宽度。超宽时先截断，保证不溢出。
func centerText(s string, width int) string {
	vLen := types.VisualWidth(s)
	if vLen > width {
		s = types.TruncateToWidth(s, width, "")
		vLen = types.VisualWidth(s)
	}
	if vLen >= width {
		return s
	}
	left := (width - vLen) / 2
	right := width - vLen - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

// alignCell 按对齐方式填充单元格文本到指定宽度。超宽时先截断，保证不溢出。
func alignCell(s string, width int, align tableast.Alignment) string {
	vLen := types.VisualWidth(s)
	if vLen > width {
		s = types.TruncateToWidth(s, width, "")
		vLen = types.VisualWidth(s)
	}
	if vLen >= width {
		return s
	}
	switch align {
	case tableast.AlignRight:
		return strings.Repeat(" ", width-vLen) + s
	case tableast.AlignCenter:
		return centerText(s, width)
	default: // AlignLeft / AlignNone
		return s + strings.Repeat(" ", width-vLen)
	}
}
