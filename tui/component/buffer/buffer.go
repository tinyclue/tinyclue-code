// Package buffer 提供可复用的文本编辑缓冲区，支持光标管理、视觉换行和光标移动。
// 任何需要光标编辑功能的组件都可以嵌入 TextBuffer。
package buffer

import (
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// AtomicMarker 定义原子段的分界符：从 Prefix 开头、到第一个 Suffix rune 为止
// 的文本被视为单个原子段，光标移动和删除不会进入其内部。
type AtomicMarker struct {
	Prefix []rune
	Suffix rune
}

// 预定义的原子标记。
var (
	PasteMarker = AtomicMarker{
		Prefix: []rune("[Pasted text #"),
		Suffix: ']',
	}
	// [...Truncated text #N +M lines...]，以 ...] 结尾，第一个 ] 恰为末尾。
	TruncatedTextMarker = AtomicMarker{
		Prefix: []rune("[...Truncated text #"),
		Suffix: ']',
	}
	// DefaultMarkers 是 New() 默认安装的原子标记列表。
	DefaultMarkers = []AtomicMarker{PasteMarker, TruncatedTextMarker}
)

// TextBuffer 封装文本行存储、光标位置、显示宽度计算和视觉换行。
// 嵌入此类型的组件自动获得完整的光标移动和基础编辑能力。
type TextBuffer struct {
	Lines     [][]rune // 文本行缓冲区，每个元素是一条逻辑行
	CursorRow int      // 0-indexed 逻辑行号
	CursorCol int      // 0-indexed 逻辑行内 rune 索引
	TabWidth  int
	// AtomicMarkers 定义该缓冲区使用的原子段标记列表。匹配到的第一个标记生效。
	// 用于将粘贴标记等特殊段作为整体处理。
	AtomicMarkers []AtomicMarker
}

func New() *TextBuffer {
	return &TextBuffer{
		Lines:         [][]rune{{}},
		TabWidth:      4,
		AtomicMarkers: DefaultMarkers,
	}
}

// ── 显示宽度计算 ──

// DisplayWidth 返回一行的视觉显示宽度（处理制表符和宽字符）。
func (b *TextBuffer) DisplayWidth(line []rune) int {
	w := 0
	for _, r := range line {
		if r == '\t' {
			w += b.TabWidth - (w % b.TabWidth)
		} else {
			w += runewidth.RuneWidth(r)
		}
	}
	return w
}

// DisplayColOf 返回逻辑行中 runeIdx 位置的视觉显示列。
func (b *TextBuffer) DisplayColOf(line []rune, runeIdx int) int {
	col := 0
	for _, r := range line[:runeIdx] {
		if r == '\t' {
			col += b.TabWidth - (col % b.TabWidth)
		} else {
			col += runewidth.RuneWidth(r)
		}
	}
	return col
}

// RuneIndexAt 将视觉显示列 targetCol 转换为行内 rune 索引。
func (b *TextBuffer) RuneIndexAt(line []rune, targetCol int) int {
	col := 0
	for i, r := range line {
		if col >= targetCol {
			return i
		}
		var rw int
		if r == '\t' {
			rw = b.TabWidth - (col % b.TabWidth)
		} else {
			rw = runewidth.RuneWidth(r)
		}
		col += rw
	}
	return len(line)
}

// ── 视觉换行 ──

// VisualRows 返回一个逻辑行在指定宽度下占据的视觉行数。
func (b *TextBuffer) VisualRows(line []rune, width int) int {
	if len(line) == 0 {
		return 1
	}
	w := b.DisplayWidth(line)
	return (w + width - 1) / width
}

// WrapLine 将单个逻辑行按指定宽度拆分为视觉行。
func (b *TextBuffer) WrapLine(line []rune, width int) []string {
	if len(line) == 0 {
		return []string{""}
	}
	var lines []string
	col := 0
	start := 0
	for i, r := range line {
		var rw int
		if r == '\t' {
			rw = b.TabWidth - (col % b.TabWidth)
		} else {
			rw = runewidth.RuneWidth(r)
		}
		if col+rw > width && col > 0 {
			lines = append(lines, string(line[start:i]))
			start = i
			col = rw
		} else {
			col += rw
		}
	}
	lines = append(lines, string(line[start:]))
	return lines
}

// View 返回所有逻辑行按 width 换行后的视觉行。
func (b *TextBuffer) View(width int) []string {
	var visualLines []string
	for _, line := range b.Lines {
		visualLines = append(visualLines, b.WrapLine(line, width)...)
	}
	return visualLines
}

// Height 返回在指定宽度下所有内容占用的总视觉行数。
func (b *TextBuffer) Height(width int) int {
	n := 0
	for _, line := range b.Lines {
		n += b.VisualRows(line, width)
	}
	return n
}

// ── 光标视觉位置 ──

// CursorViewRow 返回光标在当前宽度下的视觉行号（0-indexed）。
func (b *TextBuffer) CursorViewRow(width int) int {
	rows := 0
	for i := 0; i < b.CursorRow; i++ {
		rows += b.VisualRows(b.Lines[i], width)
	}
	cd := b.DisplayColOf(b.Lines[b.CursorRow], b.CursorCol)
	rows += cd / width
	return rows
}

// CursorViewCol 返回光标在当前换行后所在视觉行内的视觉列（0-indexed）。
func (b *TextBuffer) CursorViewCol(width int) int {
	cd := b.DisplayColOf(b.Lines[b.CursorRow], b.CursorCol)
	return cd % width
}

// findAtomic 扫描当前行所有 AtomicMarkers，返回 col 所在段的边界。
func (b *TextBuffer) findAtomic(col int) (start, end int, ok bool) {
	line := b.Lines[b.CursorRow]
	for _, m := range b.AtomicMarkers {
		pref := m.Prefix
		if len(pref) == 0 || m.Suffix == 0 {
			continue
		}
		for i := 0; i < len(line); {
			if i+len(pref) <= len(line) {
				match := true
				for j, pr := range pref {
					if line[i+j] != pr {
						match = false
						break
					}
				}
				if match {
					segStart := i
					endIdx := -1
					for j := i; j < len(line); j++ {
						if line[j] == m.Suffix {
							endIdx = j
							break
						}
					}
					if endIdx >= 0 {
						segEnd := endIdx + 1
						if segStart <= col && col <= segEnd {
							return segStart, segEnd, true
						}
						i = segEnd
						continue
					}
				}
			}
			i++
		}
	}
	return 0, 0, false
}

// ── 光标移动 ──

// CursorUp 将光标上移一个视觉行。到达顶部时无操作，返回 false（供上层判"已到顶、可翻历史"）。
func (b *TextBuffer) CursorUp(width int) bool {
	vr := b.CursorViewRow(width)
	if vr <= 0 {
		return false
	}
	targetVR := vr - 1
	cd := b.DisplayColOf(b.Lines[b.CursorRow], b.CursorCol)
	targetInRow := cd % width

	rows := 0
	for i, line := range b.Lines {
		v := b.VisualRows(line, width)
		if rows+v > targetVR {
			withinLine := targetVR - rows
			targetDisplayCol := withinLine*width + targetInRow
			lw := b.DisplayWidth(line)
			if targetDisplayCol > lw {
				targetDisplayCol = lw
			}
			b.CursorRow = i
			b.CursorCol = b.RuneIndexAt(line, targetDisplayCol)
			return true
		}
		rows += v
	}
	return false
}

// CursorDown 将光标下移一个视觉行。到达底部时无操作，返回 false（供上层判"已到底、可翻历史"）。
func (b *TextBuffer) CursorDown(width int) bool {
	vr := b.CursorViewRow(width)
	total := b.Height(width)
	if vr >= total-1 {
		return false
	}
	targetVR := vr + 1
	cd := b.DisplayColOf(b.Lines[b.CursorRow], b.CursorCol)
	targetInRow := cd % width

	rows := 0
	for i, line := range b.Lines {
		v := b.VisualRows(line, width)
		if rows+v > targetVR {
			withinLine := targetVR - rows
			targetDisplayCol := withinLine*width + targetInRow
			lw := b.DisplayWidth(line)
			if targetDisplayCol > lw {
				targetDisplayCol = lw
			}
			b.CursorRow = i
			b.CursorCol = b.RuneIndexAt(line, targetDisplayCol)
			return true
		}
		rows += v
	}
	return false
}

// CursorLeft 将光标左移一个字符。如果 FindAtomic 设置且光标在原子段右侧或内部，
// 则跳到段首；在行首时移至上一行行尾。
func (b *TextBuffer) CursorLeft() {
	if b.CursorCol > 0 {
		if start, _, ok := b.findAtomic(b.CursorCol); ok && b.CursorCol > start {
			b.CursorCol = start
			return
		}
		b.CursorCol--
	} else if b.CursorRow > 0 {
		b.CursorRow--
		b.CursorCol = len(b.Lines[b.CursorRow])
	}
}

// CursorRight 将光标右移一个字符。如果 FindAtomic 设置且光标在原子段左侧或内部，
// 则跳到段尾；在行尾时移至下一行行首。
func (b *TextBuffer) CursorRight() {
	if b.CursorCol < len(b.Lines[b.CursorRow]) {
		if start, end, ok := b.findAtomic(b.CursorCol); ok && b.CursorCol >= start && b.CursorCol < end {
			b.CursorCol = end
			return
		}
		b.CursorCol++
	} else if b.CursorRow < len(b.Lines)-1 {
		b.CursorRow++
		b.CursorCol = 0
	}
}

// LineStart 将光标移到行首。
func (b *TextBuffer) LineStart() {
	b.CursorCol = 0
}

// LineEnd 将光标移到行尾。
func (b *TextBuffer) LineEnd() {
	b.CursorCol = len(b.Lines[b.CursorRow])
}

// MoveToBegin 将光标移到缓冲区开头。
func (b *TextBuffer) MoveToBegin() {
	b.CursorRow = 0
	b.CursorCol = 0
}

// MoveToEnd 将光标移到缓冲区末尾。
func (b *TextBuffer) MoveToEnd() {
	b.CursorRow = len(b.Lines) - 1
	b.CursorCol = len(b.Lines[b.CursorRow])
}

// PageUp 将光标上移一个页面（视觉行数等于 height）。
func (b *TextBuffer) PageUp(width int) {
	for i := 0; i < b.Height(width); i++ {
		vr := b.CursorViewRow(width)
		if vr <= 0 {
			break
		}
		b.CursorUp(width)
	}
}

// PageDown 将光标下移一个页面（视觉行数等于 height）。
func (b *TextBuffer) PageDown(width int) {
	h := b.Height(width)
	for i := 0; i < h; i++ {
		vr := b.CursorViewRow(width)
		if vr >= h-1 {
			break
		}
		b.CursorDown(width)
	}
}

// WordLeft 将光标移到前一个单词开头。
func (b *TextBuffer) WordLeft() {
	if b.CursorCol <= 0 {
		if b.CursorRow > 0 {
			b.CursorRow--
			b.CursorCol = len(b.Lines[b.CursorRow])
		}
		return
	}
	pos := b.CursorCol - 1
	line := b.Lines[b.CursorRow]
	for pos >= 0 && unicode.IsSpace(line[pos]) {
		pos--
	}
	if pos < 0 {
		b.CursorCol = 0
		return
	}
	for pos >= 0 && !unicode.IsSpace(line[pos]) {
		pos--
	}
	b.CursorCol = pos + 1
}

// WordRight 将光标移到后一个单词开头。
func (b *TextBuffer) WordRight() {
	line := b.Lines[b.CursorRow]
	if b.CursorCol >= len(line) {
		if b.CursorRow < len(b.Lines)-1 {
			b.CursorRow++
			b.CursorCol = 0
		}
		return
	}
	pos := b.CursorCol
	for pos < len(line) && !unicode.IsSpace(line[pos]) {
		pos++
	}
	for pos < len(line) && unicode.IsSpace(line[pos]) {
		pos++
	}
	b.CursorCol = pos
}

// ── 基础编辑 ──

// InsertRune 在光标位置插入一个字符。制表符展开为空格，换行把当前行在光标处拆成两行。
// 多行粘贴由此产生真实逻辑行，View/Height 的折行与布局才正确；
// 否则 \n 作为字面字符内嵌进一行，渲染行会被终端展开成多物理行，越过分隔线、样式错乱。
func (b *TextBuffer) InsertRune(r rune) {
	line := b.Lines[b.CursorRow]
	if r == '\t' {
		col := 0
		for _, c := range line[:b.CursorCol] {
			if c == '\t' {
				col += b.TabWidth - (col % b.TabWidth)
			} else {
				col += runewidth.RuneWidth(c)
			}
		}
		spaces := b.TabWidth - (col % b.TabWidth)
		newpart := make([]rune, spaces)
		for i := range newpart {
			newpart[i] = ' '
		}
		newLine := make([]rune, 0, len(line)+spaces)
		newLine = append(newLine, line[:b.CursorCol]...)
		newLine = append(newLine, newpart...)
		newLine = append(newLine, line[b.CursorCol:]...)
		b.Lines[b.CursorRow] = newLine
		b.CursorCol += spaces
	} else if r == '\n' {
		prefix := make([]rune, b.CursorCol)
		copy(prefix, line[:b.CursorCol])
		tail := make([]rune, len(line)-b.CursorCol)
		copy(tail, line[b.CursorCol:])
		b.Lines[b.CursorRow] = prefix
		b.Lines = append(b.Lines, nil)
		copy(b.Lines[b.CursorRow+1:], b.Lines[b.CursorRow:len(b.Lines)-1])
		b.Lines[b.CursorRow+1] = tail
		b.CursorRow++
		b.CursorCol = 0
	} else {
		newLine := make([]rune, 0, len(line)+1)
		newLine = append(newLine, line[:b.CursorCol]...)
		newLine = append(newLine, r)
		newLine = append(newLine, line[b.CursorCol:]...)
		b.Lines[b.CursorRow] = newLine
		b.CursorCol++
	}
}

// Backspace 删除光标前的字符。如果设置了 AtomicPrefix/Suffix 且光标在原子段
// 右侧或内部，则删除整个原子段；在行首时与上一行合并。
func (b *TextBuffer) Backspace() {
	if b.CursorCol > 0 {
		if start, end, ok := b.findAtomic(b.CursorCol); ok && b.CursorCol > start && b.CursorCol <= end {
			line := b.Lines[b.CursorRow]
			b.Lines[b.CursorRow] = append([]rune{}, line[:start]...)
			b.Lines[b.CursorRow] = append(b.Lines[b.CursorRow], line[end:]...)
			b.CursorCol = start
			return
		}
		line := b.Lines[b.CursorRow]
		b.Lines[b.CursorRow] = append(line[:b.CursorCol-1], line[b.CursorCol:]...)
		b.CursorCol--
	} else if b.CursorRow > 0 {
		above := b.Lines[b.CursorRow-1]
		current := b.Lines[b.CursorRow]
		b.CursorCol = len(above)
		b.Lines[b.CursorRow-1] = append(above, current...)
		b.Lines = append(b.Lines[:b.CursorRow], b.Lines[b.CursorRow+1:]...)
		b.CursorRow--
	}
}

// Delete 删除光标处的字符。如果设置了 AtomicPrefix/Suffix 且光标在原子段
// 左侧或内部，则删除整个原子段；在行尾时与下一行合并。
func (b *TextBuffer) Delete() {
	line := b.Lines[b.CursorRow]
	if b.CursorCol < len(line) {
		if start, end, ok := b.findAtomic(b.CursorCol); ok && b.CursorCol >= start && b.CursorCol < end {
			b.Lines[b.CursorRow] = append([]rune{}, line[:start]...)
			b.Lines[b.CursorRow] = append(b.Lines[b.CursorRow], line[end:]...)
			return
		}
		b.Lines[b.CursorRow] = append(line[:b.CursorCol], line[b.CursorCol+1:]...)
	} else if b.CursorRow < len(b.Lines)-1 {
		below := b.Lines[b.CursorRow+1]
		b.Lines[b.CursorRow] = append(line, below...)
		b.Lines = append(b.Lines[:b.CursorRow+1], b.Lines[b.CursorRow+2:]...)
	}
}

// DeleteWordLeft 删除光标前的单词。
func (b *TextBuffer) DeleteWordLeft() {
	if b.CursorCol <= 0 {
		return
	}
	line := b.Lines[b.CursorRow]
	pos := b.CursorCol - 1
	for pos >= 0 && unicode.IsSpace(line[pos]) {
		pos--
	}
	for pos >= 0 && !unicode.IsSpace(line[pos]) {
		pos--
	}
	pos++
	b.Lines[b.CursorRow] = append(line[:pos], line[b.CursorCol:]...)
	b.CursorCol = pos
}

// DeleteWordRight 删除光标后的单词。
func (b *TextBuffer) DeleteWordRight() {
	line := b.Lines[b.CursorRow]
	if b.CursorCol >= len(line) {
		return
	}
	pos := b.CursorCol
	for pos < len(line) && !unicode.IsSpace(line[pos]) {
		pos++
	}
	for pos < len(line) && unicode.IsSpace(line[pos]) {
		pos++
	}
	b.Lines[b.CursorRow] = append(line[:b.CursorCol], line[pos:]...)
}

// DeleteToLineEnd 从光标删除到行尾。
func (b *TextBuffer) DeleteToLineEnd() {
	b.Lines[b.CursorRow] = b.Lines[b.CursorRow][:b.CursorCol]
}

// DeleteToLineStart 从光标删除到行首。
func (b *TextBuffer) DeleteToLineStart() {
	b.Lines[b.CursorRow] = b.Lines[b.CursorRow][b.CursorCol:]
	b.CursorCol = 0
}

// ── 批量操作 ──

// SetText 设置整个缓冲区内容并重置光标到开头。
func (b *TextBuffer) SetText(s string) {
	parts := strings.Split(s, "\n")
	b.Lines = make([][]rune, len(parts))
	for i, p := range parts {
		b.Lines[i] = []rune(p)
	}
	if len(b.Lines) == 0 {
		b.Lines = [][]rune{{}}
	}
	b.CursorRow = 0
	b.CursorCol = 0
}

// Text 返回缓冲区全文。
func (b *TextBuffer) Text() string {
	var sb strings.Builder
	for i, line := range b.Lines {
		if i > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(string(line))
	}
	return sb.String()
}
