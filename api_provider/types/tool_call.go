package types

// ToolCall 统一的工具调用数据，请求和响应复用。
type ToolCall struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Arguments        map[string]any `json:"arguments"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"` // Google 专用：用于复用 thought 上下文的签名
}
