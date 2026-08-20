package protocol

import "encoding/json"

// ── Anthropic Messages API 数据结构 ──

// AnthropicImageSource 表示 Anthropic API 中的图片数据源。
type AnthropicImageSource struct {
	Type      string `json:"type"`       // "base64"
	MediaType string `json:"media_type"` // "image/png"
	Data      string `json:"data"`       // base64 编码的图片数据
}

// AnthropicContentBlock 对应 Anthropic Messages API 中 content 数组的一项。
type AnthropicContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Data      string          `json:"data,omitempty"` // redacted_thinking
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"` // tool_result 的内容：字符串或内容块数组
	IsError   bool            `json:"is_error,omitempty"`
}

type AnthropicMessage struct {
	Role    string                  `json:"role"`
	Content []AnthropicContentBlock `json:"content"`
}

// AnthropicRequest 对应 Anthropic Messages API 的请求体。
type AnthropicRequest struct {
	Model           string             `json:"model"`
	Messages        []AnthropicMessage `json:"messages"`
	MaxTokens       int                `json:"max_tokens"`
	Stream          bool               `json:"stream"`
	Tools           []AnthropicTool    `json:"tools,omitempty"`
	ReasoningEffort string             `json:"reasoning_effort,omitempty"`
}

type AnthropicTool struct {
	Type           string          `json:"type,omitempty"` // "custom" 或 "web_search_20250305"
	Name           string          `json:"name"`
	Description    string          `json:"description,omitempty"`
	InputSchema    json.RawMessage `json:"input_schema,omitempty"`
	MaxUses        int             `json:"max_uses,omitempty"`
	AllowedDomains []string        `json:"allowed_domains,omitempty"`
	BlockedDomains []string        `json:"blocked_domains,omitempty"`
}

// AnthropicResponseContentBlock 对应 Anthropic Messages API 响应中 content 数组的一项。
type AnthropicResponseContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Data      string          `json:"data,omitempty"` // redacted_thinking 的数据
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
}

// AnthropicResponse 对应 Anthropic Messages API 的响应体。
type AnthropicResponse struct {
	ID         string                          `json:"id"`
	Model      string                          `json:"model"`
	Content    []AnthropicResponseContentBlock `json:"content"`
	StopReason string                          `json:"stop_reason"`
	Usage      *struct {
		InputTokens           int `json:"input_tokens"`
		OutputTokens          int `json:"output_tokens"`
		CacheReadInputTokens  int `json:"cache_read_input_tokens,omitempty"`
		CacheWriteInputTokens int `json:"cache_write_input_tokens,omitempty"`
	} `json:"usage"`
}

// AnthropicStreamChunk 流式响应的一个 chunk。
type AnthropicStreamChunk struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
	Delta *struct {
		Type        string `json:"type"` // "text_delta", "thinking_delta", "input_json_delta"
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"` // input_json_delta tool call arguments
		StopReason  string `json:"stop_reason"`  // message_delta 的结束原因
	} `json:"delta"`
	ContentBlock *struct {
		Type     string          `json:"type"` // "text", "thinking", "redacted_thinking", "tool_use"
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		Data     string          `json:"data"`  // redacted_thinking 的数据
		ID       string          `json:"id"`    // tool_use 的 id
		Name     string          `json:"name"`  // tool_use 的 function name
		Input    json.RawMessage `json:"input"` // tool_use 的初始参数
	} `json:"content_block"`
	Message *struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		StopReason string `json:"stop_reason"`
	} `json:"message"`
	Usage *struct {
		InputTokens           int `json:"input_tokens"`
		OutputTokens          int `json:"output_tokens"`
		CacheReadInputTokens  int `json:"cache_read_input_tokens,omitempty"`
		CacheWriteInputTokens int `json:"cache_write_input_tokens,omitempty"`
	} `json:"usage"`
}
