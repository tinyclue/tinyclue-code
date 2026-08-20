// Package types 提供 TUI 组件使用的 ANSI 颜色常量和公共工具值。
package types

import (
	"regexp"
	"time"
)

// AnsiPattern 匹配 ANSI 转义序列，用于计算可见宽度和空白行检测。
var AnsiPattern = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// ANSI 颜色及样式常量。
const (
	// ── 前景色 ──
	FgGray       = "\033[90m"       // 灰色
	FgGreen      = "\033[32m"       // 绿色
	FgBlue       = "\033[34m"       // 蓝色
	FgRed        = "\033[31m"       // 红色
	FgWhite      = "\033[37m"       // 白色
	FgDefault    = "\033[39m"       // 默认前景（重置前景色到终端默认）
	FgLightGreen = "\033[38;5;120m" // 浅绿
	FgLightRed   = "\033[38;5;210m" // 浅红
	FgMagenta    = "\033[38;5;213m" // 紫红
	FgLightBlue  = "\033[38;5;117m" // 淡蓝
	FgYellow     = "\033[33m"       // 黄色（needs-auth 等警告态）
	FgPlanMode   = "\033[38;5;66m"  // 计划模式标记色

	// ── 背景色 ──
	BgGray  = "\033[48;5;236m" // 深灰（用于用户消息背景）
	BgGreen = "\033[48;5;22m"  // 深绿（diff +行）
	BgRed   = "\033[48;5;52m"  // 深红（diff -行）

	// ── 样式 ──
	Bold = "\033[1m" // 粗体

	// ── 光标 ──
	CursorStyle = "\033[7m" // 反转色：交换前景/背景色，字符可见但高亮。参考自 pi/editor.ts 的反转色光标方案

	// ── 通用 ──
	Reset = "\033[0m" // 全部属性重置

	// SegmentReset 行尾归一化序列：清样式（SGR 0）+ 关闭 OSC 8 超链接。
	// 与 pi 的 SEGMENT_RESET 一致，防止未闭合的 ANSI 颜色/超链接污染下一行。
	SegmentReset = "\033[0m\033]8;;\x07"
)

// RevertTimeout 是 Ctrl+C 首次按下后提示文字保留时长，超时未二次按下则恢复。
const RevertTimeout = 1000 * time.Millisecond
