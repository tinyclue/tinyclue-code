package types

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ── NormalizeTerminalOutput ──
//
// 部分终端对泰语/老挝语 AM 元音（\u0e33、\u0eb3）的预组合形式在差分重绘时渲染不一致，
// 兼容分解（\u0e4d\u0e32、\u0ecd\u0eb2）有相同的单元格宽度但避免残留伪影。
// 与 pi/utils.ts 的 normalizeTerminalOutput 一致。

// NormalizeTerminalOutput 将文本中的预组合 AM 元音替换为兼容分解形式。
func NormalizeTerminalOutput(s string) string {
	if !strings.Contains(s, "\u0e33") && !strings.Contains(s, "\u0eb3") {
		return s
	}
	var sb strings.Builder
	for _, r := range s {
		switch r {
		case '\u0e33':
			sb.WriteString("\u0e4d\u0e32")
		case '\u0eb3':
			sb.WriteString("\u0ecd\u0eb2")
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// ── OSC 8 超链接 ──

// Osc8Hyperlink 记录一个激活的 OSC 8 超链接。保留原始终结符（BEL 或 ST），
// 因为部分终端只对 BEL 结尾的链接可点击（OAuth 登录 URL 用 BEL），换行重开时
// 用原终结符才能保持整段可点击。
type Osc8Hyperlink struct {
	params     string // id=xxx 等，可为空
	url        string
	terminator string // "\x07" 或 "\x1b\\"
}

func parseOsc8Hyperlink(ansiCode string) (*Osc8Hyperlink, bool) {
	if !strings.HasPrefix(ansiCode, "\x1b]8;") {
		return nil, false
	}
	terminator := "\x07"
	if !strings.HasSuffix(ansiCode, "\x07") {
		terminator = "\x1b\\"
	}
	body := ansiCode[4:]
	if terminator == "\x07" {
		body = strings.TrimSuffix(body, "\x07")
	} else {
		body = strings.TrimSuffix(body, "\x1b\\")
	}
	sep := strings.Index(body, ";")
	if sep == -1 {
		return nil, true // 视为关闭序列
	}
	params := body[:sep]
	url := body[sep+1:]
	if url == "" {
		return nil, true // 关闭序列
	}
	return &Osc8Hyperlink{params: params, url: url, terminator: terminator}, true
}

func formatOsc8Hyperlink(h *Osc8Hyperlink) string {
	return "\x1b]8;" + h.params + ";" + h.url + h.terminator
}

func formatOsc8Close(terminator string) string {
	return "\x1b]8;;" + terminator
}

// ── AnsiCodeTracker ──
//
// 追踪激活的 ANSI SGR 样式，用于在换行处重开样式（跨行保持），
// 以及在行尾只关闭需要关闭的属性（下划线 + 超链接，保留背景色）。
// 与 pi/utils.ts 的 AnsiCodeTracker 一致。

type AnsiCodeTracker struct {
	bold, dim, italic, underline, blink, inverse, hidden, strikethrough bool
	fgColor, bgColor                                                    string // 完整 SGR 参数，如 "31"、"38;5;240"
	hyperlink                                                           *Osc8Hyperlink
}

// Process 处理一个转义序列，更新样式状态。只关心 SGR（\x1b[...m）与 OSC 8 超链接。
func (t *AnsiCodeTracker) Process(ansiCode string) {
	if strings.HasPrefix(ansiCode, "\x1b]8;") {
		if h, ok := parseOsc8Hyperlink(ansiCode); ok {
			t.hyperlink = h // nil 表示关闭
		}
		return
	}
	if !strings.HasSuffix(ansiCode, "m") || !strings.HasPrefix(ansiCode, "\x1b[") {
		return
	}

	params := ansiCode[2 : len(ansiCode)-1]
	if params == "" || params == "0" {
		t.reset()
		return
	}

	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			code = -1
		}

		// 256 色 / RGB 色：38;5;N、38;2;R;G;B（48 同理背景）
		if code == 38 || code == 48 {
			if i+2 < len(parts) && parts[i+1] == "5" {
				color := parts[i] + ";" + parts[i+1] + ";" + parts[i+2]
				if code == 38 {
					t.fgColor = color
				} else {
					t.bgColor = color
				}
				i += 3
				continue
			}
			if i+4 < len(parts) && parts[i+1] == "2" {
				color := strings.Join(parts[i:i+5], ";")
				if code == 38 {
					t.fgColor = color
				} else {
					t.bgColor = color
				}
				i += 5
				continue
			}
		}

		switch code {
		case 0:
			t.reset()
		case 1:
			t.bold = true
		case 2:
			t.dim = true
		case 3:
			t.italic = true
		case 4:
			t.underline = true
		case 5:
			t.blink = true
		case 7:
			t.inverse = true
		case 8:
			t.hidden = true
		case 9:
			t.strikethrough = true
		case 21, 22:
			t.bold = false
			t.dim = false
		case 23:
			t.italic = false
		case 24:
			t.underline = false
		case 25:
			t.blink = false
		case 27:
			t.inverse = false
		case 28:
			t.hidden = false
		case 29:
			t.strikethrough = false
		case 39:
			t.fgColor = ""
		case 49:
			t.bgColor = ""
		default:
			if (code >= 30 && code <= 37) || (code >= 90 && code <= 97) {
				t.fgColor = strconv.Itoa(code)
			} else if (code >= 40 && code <= 47) || (code >= 100 && code <= 107) {
				t.bgColor = strconv.Itoa(code)
			}
		}
	}
}

func (t *AnsiCodeTracker) reset() {
	t.bold, t.dim, t.italic, t.underline, t.blink = false, false, false, false, false
	t.inverse, t.hidden, t.strikethrough = false, false, false
	t.fgColor, t.bgColor = "", ""
	// SGR reset 不影响 OSC 8 超链接状态
}

// Clear 清空全部状态，含超链接。
func (t *AnsiCodeTracker) Clear() {
	t.reset()
	t.hyperlink = nil
}

// GetActiveCodes 返回当前激活样式的重开序列（换行处使用）。
func (t *AnsiCodeTracker) GetActiveCodes() string {
	var codes []string
	if t.bold {
		codes = append(codes, "1")
	}
	if t.dim {
		codes = append(codes, "2")
	}
	if t.italic {
		codes = append(codes, "3")
	}
	if t.underline {
		codes = append(codes, "4")
	}
	if t.blink {
		codes = append(codes, "5")
	}
	if t.inverse {
		codes = append(codes, "7")
	}
	if t.hidden {
		codes = append(codes, "8")
	}
	if t.strikethrough {
		codes = append(codes, "9")
	}
	if t.fgColor != "" {
		codes = append(codes, t.fgColor)
	}
	if t.bgColor != "" {
		codes = append(codes, t.bgColor)
	}
	var sb strings.Builder
	if len(codes) > 0 {
		sb.WriteString("\x1b[" + strings.Join(codes, ";") + "m")
	}
	if t.hyperlink != nil {
		sb.WriteString(formatOsc8Hyperlink(t.hyperlink))
	}
	return sb.String()
}

func (t *AnsiCodeTracker) HasActiveCodes() bool {
	return t.bold || t.dim || t.italic || t.underline || t.blink ||
		t.inverse || t.hidden || t.strikethrough ||
		t.fgColor != "" || t.bgColor != "" || t.hyperlink != nil
}

// GetLineEndReset 返回行尾需要关闭的属性序列：下划线（保留背景）+ 超链接。
// 超链接由下一行开头的 GetActiveCodes 重开。
func (t *AnsiCodeTracker) GetLineEndReset() string {
	var sb strings.Builder
	if t.underline {
		sb.WriteString("\x1b[24m") // 仅关闭下划线
	}
	if t.hyperlink != nil {
		sb.WriteString(formatOsc8Close(t.hyperlink.terminator))
	}
	return sb.String()
}

// updateTrackerFromText 扫描文本中的转义序列并依次喂给 tracker。
func updateTrackerFromText(text string, tracker *AnsiCodeTracker) {
	for i := 0; i < len(text); {
		if code, ok := extractAnsiCode(text, i); ok {
			tracker.Process(code)
			i += len(code)
		} else {
			i++
		}
	}
}

// ── CJK 断行 ──
//
// CJK 字符独立成 token，保证中英文混排时按字符折行而不是整串当单词。
// 与 pi 的 cjkBreakRegex（Han/Hiragana/Katakana/Hangul/Bopomofo）一致。
func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul, unicode.Bopomofo)
}

// ── SplitIntoTokensWithAnsi ──
//
// 把文本切成空格/单词 token，CJK 字符独立 token，ANSI 序列挂到下一个可见字符。
// 与 pi/utils.ts 的 splitIntoTokensWithAnsi 一致。
func SplitIntoTokensWithAnsi(text string) []string {
	const (
		kindNone = iota
		kindSpace
		kindWord
	)

	var tokens []string
	current := ""
	pendingAnsi := ""
	currentKind := kindNone

	flushCurrent := func() {
		if current != "" {
			tokens = append(tokens, current)
			current = ""
			currentKind = kindNone
		}
	}

	i := 0
	for i < len(text) {
		if code, ok := extractAnsiCode(text, i); ok {
			pendingAnsi += code
			i += len(code)
			continue
		}

		end := i
		for end < len(text) {
			if _, ok := extractAnsiCode(text, end); ok {
				break
			}
			end++
		}

		for _, r := range text[i:end] {
			segIsSpace := r == ' '
			if !segIsSpace && isCJK(r) {
				flushCurrent()
				tokens = append(tokens, pendingAnsi+string(r))
				pendingAnsi = ""
				continue
			}
			kind := kindWord
			if segIsSpace {
				kind = kindSpace
			}
			if current != "" && currentKind != kind {
				flushCurrent()
			}
			if pendingAnsi != "" {
				current += pendingAnsi
				pendingAnsi = ""
			}
			currentKind = kind
			current += string(r)
		}
		i = end
	}

	// 剩余的 pending ANSI 挂到最后一个 token
	if pendingAnsi != "" {
		if current != "" {
			current += pendingAnsi
		} else if len(tokens) > 0 {
			tokens[len(tokens)-1] += pendingAnsi
		} else {
			current = pendingAnsi
		}
	}
	if current != "" {
		tokens = append(tokens, current)
	}
	return tokens
}

// ── WrapTextWithAnsi ──
//
// 带 ANSI 的按词折行：每行可见宽度 ≤ width，样式跨行重开，超长词逐字符断行。
// 只做折行，不做填充/背景。与 pi/utils.ts 的 wrapTextWithAnsi 一致。
func WrapTextWithAnsi(text string, width int) []string {
	if text == "" {
		return []string{""}
	}
	if width <= 0 {
		return strings.Split(text, "\n")
	}

	var result []string
	tracker := &AnsiCodeTracker{}
	for _, inputLine := range strings.Split(text, "\n") {
		prefix := ""
		if len(result) > 0 {
			prefix = tracker.GetActiveCodes() // 上一行未闭合的样式重开
		}
		for _, wl := range wrapSingleLine(prefix+inputLine, width) {
			result = append(result, wl)
		}
		updateTrackerFromText(inputLine, tracker)
	}
	if len(result) > 0 {
		return result
	}
	return []string{""}
}

// wrapSingleLine 折行单行文本（不含 \n）。
func wrapSingleLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	if width <= 0 || VisualWidth(line) <= width {
		return []string{line}
	}

	var wrapped []string
	tracker := &AnsiCodeTracker{}
	tokens := SplitIntoTokensWithAnsi(line)

	currentLine := ""
	currentVisibleLength := 0

	for _, token := range tokens {
		tokenVisibleLength := VisualWidth(token)
		isWhitespace := strings.TrimSpace(token) == ""

		// token 本身超宽 → 逐字符断行
		if tokenVisibleLength > width && !isWhitespace {
			if currentLine != "" {
				if lr := tracker.GetLineEndReset(); lr != "" {
					currentLine += lr
				}
				wrapped = append(wrapped, currentLine)
				currentLine = ""
				currentVisibleLength = 0
			}
			broken := breakLongWord(token, width, tracker)
			for i := 0; i < len(broken)-1; i++ {
				wrapped = append(wrapped, broken[i])
			}
			currentLine = broken[len(broken)-1]
			currentVisibleLength = VisualWidth(currentLine)
			continue
		}

		totalNeeded := currentVisibleLength + tokenVisibleLength
		if totalNeeded > width && currentVisibleLength > 0 {
			lineToWrap := strings.TrimRight(currentLine, " ")
			if lr := tracker.GetLineEndReset(); lr != "" {
				lineToWrap += lr
			}
			wrapped = append(wrapped, lineToWrap)
			if isWhitespace {
				currentLine = tracker.GetActiveCodes()
				currentVisibleLength = 0
			} else {
				currentLine = tracker.GetActiveCodes() + token
				currentVisibleLength = tokenVisibleLength
			}
		} else {
			currentLine += token
			currentVisibleLength += tokenVisibleLength
		}
		updateTrackerFromText(token, tracker)
	}

	if currentLine != "" {
		wrapped = append(wrapped, currentLine)
	}
	if len(wrapped) > 0 {
		for i, l := range wrapped {
			wrapped[i] = strings.TrimRight(l, " ")
		}
		return wrapped
	}
	return []string{""}
}

// breakLongWord 将一个超宽单词逐字符断行。tracker 状态被推进（该词内的样式变化）。
func breakLongWord(word string, width int, tracker *AnsiCodeTracker) []string {
	type seg struct {
		ansi  bool
		value string
	}
	var segments []seg
	i := 0
	for i < len(word) {
		if code, ok := extractAnsiCode(word, i); ok {
			segments = append(segments, seg{ansi: true, value: code})
			i += len(code)
		} else {
			end := i
			for end < len(word) {
				if _, ok := extractAnsiCode(word, end); ok {
					break
				}
				end++
			}
			for _, r := range word[i:end] {
				segments = append(segments, seg{ansi: false, value: string(r)})
			}
			i = end
		}
	}

	var lines []string
	currentLine := tracker.GetActiveCodes()
	currentWidth := 0

	for _, s := range segments {
		if s.ansi {
			currentLine += s.value
			tracker.Process(s.value)
			continue
		}
		grapheme := s.value
		if grapheme == "" {
			continue
		}
		gw := VisualWidth(grapheme)
		if currentWidth+gw > width {
			if lr := tracker.GetLineEndReset(); lr != "" {
				currentLine += lr
			}
			lines = append(lines, currentLine)
			currentLine = tracker.GetActiveCodes()
			currentWidth = 0
		}
		currentLine += grapheme
		currentWidth += gw
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}
	if len(lines) > 0 {
		return lines
	}
	return []string{""}
}

// ── TruncateToWidth ──
//
// 把文本按可见宽度截断，保留 ANSI 序列（不计宽），必要时追加省略号。
// 与 pi/utils.ts 的 truncateToWidth（pad=false）语义一致。
func TruncateToWidth(text string, maxWidth int, ellipsis string) string {
	if maxWidth <= 0 {
		return ""
	}
	if text == "" {
		return ""
	}
	if VisualWidth(text) <= maxWidth {
		return text
	}
	ellipsisWidth := VisualWidth(ellipsis)
	if ellipsisWidth >= maxWidth {
		ellipsis = ""
		ellipsisWidth = 0
	}
	targetWidth := maxWidth - ellipsisWidth

	var sb strings.Builder
	var pendingAnsi strings.Builder
	keptWidth := 0
	for i := 0; i < len(text); {
		if code, ok := extractAnsiCode(text, i); ok {
			pendingAnsi.WriteString(code)
			i += len(code)
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		w := narrowCond.RuneWidth(r)
		if keptWidth+w > targetWidth {
			break
		}
		sb.WriteString(pendingAnsi.String())
		pendingAnsi.Reset()
		sb.WriteRune(r)
		keptWidth += w
		i += size
	}
	sb.WriteString(ellipsis)
	return sb.String()
}
