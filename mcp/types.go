// types.go：MCP 包的事件契约（eventType + data 模式）。
package mcp

import (
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// McpEventType 标识 ServerState 对外通知的事件类型（eventType + data 模式的 type）。
type McpEventType int

const (
	// McpConnected 连接成功：data.Tools 为应注册的桥接工具（订阅方据此注册真实工具）。
	McpConnected McpEventType = iota
	// McpNeedsAuth 真实 401 进入 needs-auth（订阅方据此注册授权伪工具）。
	McpNeedsAuth
	// McpAuthStarted 交互授权拿到授权 URL：data.URL 为浏览器地址（订阅方据此提示"授权中"）。
	McpAuthStarted
	// McpFailed 连接失败（置 StatusError）：data.Err 为错误（订阅方据此回显连接失败文案）。
	McpFailed
)

func (t McpEventType) String() string {
	switch t {
	case McpConnected:
		return "connected"
	case McpNeedsAuth:
		return "needs-auth"
	case McpAuthStarted:
		return "auth-started"
	case McpFailed:
		return "failed"
	}
	return "unknown"
}

// McpEventContext 是 MCP 事件的 data（eventType + data 模式的数据部分）。各事件按类型取用字段：
// McpConnected / McpNeedsAuth 取 Name（前者 + Tools）；McpAuthStarted 取 Name / URL；
// McpFailed 取 Name / Err。
// 通知即通知：server 只描述"发生了什么"，含义与副作用（转发应用事件）由订阅方
// （manager）决定。ServerState 通过 notify 注入函数发出事件，绝不反向引用 Manager。
type McpEventContext struct {
	Name  string                    // 事件所属 server 名（订阅方据此定位）
	Tools []core_types.AgentToolApi // McpConnected 携带：应注册的桥接工具
	URL   string                    // McpAuthStarted 携带：授权 URL
	Err   error                     // McpFailed 携带：连接错误
}
