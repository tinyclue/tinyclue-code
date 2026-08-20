// Package types 提供 TUI 组件使用的 ANSI 颜色常量和公共工具值。
package types

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// narrowCond 以窄模式（EastAsianWidth=false）计算字符宽度，匹配终端实际渲染。
// runewidth 默认把 ambiguous 字符（box-drawing、·、• 等）计为宽(2)，但终端按窄(1)渲染，
// 用于布局/填充计算必须关闭该假设，否则组件算出的行宽偏大，导致行被多填充而超宽折行。
var narrowCond = func() *runewidth.Condition {
	c := runewidth.NewCondition()
	c.EastAsianWidth = false
	return c
}()

// StripAnsi 去除字符串中的 ANSI 转义序列，保留可见字符。
// 与 pi/utils.ts 的 extractAnsiCode 一致，覆盖三类：
//   - CSI：\x1b[ ... 直到最终字节 [mGKHJ]
//   - OSC：\x1b] ... 直到 BEL(\x07) 或 ST(\x1b\\)，用于超链接(OSC 8)
//   - APC：\x1b_ ... 直到 BEL(\x07) 或 ST(\x1b\\)，用于光标标记(CURSOR_MARKER)
//
// 只剥 CSI 的正则（AnsiPattern）会把 SEGMENT_RESET 里的 OSC 8 超链接关闭序列计进宽度，
// 导致含超链接/Reset 的行超宽误判，因此这里必须用状态机完整剥离。
func StripAnsi(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if code, ok := extractAnsiCode(s, i); ok {
			i += len(code)
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// extractAnsiCode 从 pos 开始解析一个 ANSI 转义序列，返回序列本身及是否解析成功。
func extractAnsiCode(s string, pos int) (string, bool) {
	if pos >= len(s) || s[pos] != '\x1b' {
		return "", false
	}
	next := byte(0)
	if pos+1 < len(s) {
		next = s[pos+1]
	}

	switch next {
	case '[': // CSI：ESC [ ... 最终字节 [mGKHJ]
		j := pos + 2
		for j < len(s) && !isCsiFinal(s[j]) {
			j++
		}
		if j < len(s) {
			return s[pos : j+1], true
		}
		return "", false
	case ']', '_': // OSC/APC：ESC ]/ESC _ ... BEL 或 ST(ESC \)
		j := pos + 2
		for j < len(s) {
			if s[j] == '\x07' {
				return s[pos : j+1], true
			}
			if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
				return s[pos : j+2], true
			}
			j++
		}
		return "", false
	default:
		return "", false
	}
}

// isCsiFinal 判断字节是否为 CSI 序列的最终字节（pi 用 [mGKHJ]，覆盖光标定位与样式）。
func isCsiFinal(b byte) bool {
	switch b {
	case 'm', 'G', 'K', 'H', 'J', 'A', 'B':
		return true
	}
	return false
}

// VisualWidth 返回去掉 ANSI 后的视觉宽度（基于窄模式），渲染器与组件共用，
// 保证"组件算出的行宽"与"渲染器据以折行的行宽"一致。
// 内部用 StripAnsi 完整剥离 CSI/OSC/APC 后再计数。
func VisualWidth(s string) int {
	return narrowCond.StringWidth(StripAnsi(s))
}
