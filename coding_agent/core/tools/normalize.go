package tools

import "strings"

// NormalizeNameForMCP 把 server/tool 名转成 MCP 工具名合法片段：
// 用于构造桥接名 mcp__<server>__<tool>：名字合法可路由（Execute 用原始 server/tool 调用）。
func NormalizeNameForMCP(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}
