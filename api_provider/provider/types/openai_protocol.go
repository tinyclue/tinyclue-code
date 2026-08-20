// Package protocol 定义各家厂商的协议数据结构。
package protocol

import "encoding/json"

// ── OpenAI 兼容协议数据结构 ──

// OpenAIContentPart 表示多模态消息中的一个内容片段。
type OpenAIContentPart struct {
	Type     string          `json:"type"` // "text" 或 "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
}

// OpenAIImageURL 表示 OpenAI 消息中的图片 URL。
type OpenAIImageURL struct {
	URL string `json:"url"` // "data:image/png;base64,..."
}

type OpenAIMessage struct {
	Role             string           `json:"role"`
	Content          json.RawMessage  `json:"content"`
	ReasoningContent string           `json:"reasoning_content,omitempty"` // DeepSeek 推理内容
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string           `json:"tool_call_id,omitempty"`
}

type OpenAIChatRequest struct {
	Model           string              `json:"model"`
	Messages        []OpenAIMessage     `json:"messages"`
	Temperature     float64             `json:"temperature,omitempty"`
	MaxTokens       int                 `json:"max_tokens,omitempty"`
	Stream          bool                `json:"stream"`
	Tools           []OpenAIRequestTool `json:"tools,omitempty"`
	ReasoningEffort string              `json:"reasoning_effort,omitempty"`
}

type OpenAIChoice struct {
	Message      *OpenAIMessage `json:"message,omitempty"`
	Delta        *OpenAIMessage `json:"delta,omitempty"`
	FinishReason *string        `json:"finish_reason"`
}

// OpenAIToolCall 对应 delta.tool_calls 中的一项。
type OpenAIToolCall struct {
	Index    int                    `json:"index,omitempty"`
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function OpenAIToolCallFunction `json:"function,omitempty"`
}

type OpenAIToolCallFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// OpenAIRequestTool 对应 Chat API 请求中的 tools 参数。
type OpenAIRequestTool struct {
	Type     string                `json:"type"`
	Function OpenAIRequestFunction `json:"function"`
}

type OpenAIRequestFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

type OpenAIUsage struct {
	PromptTokens         int `json:"prompt_tokens"`
	CompletionTokens     int `json:"completion_tokens"`
	TotalTokens          int `json:"total_tokens"`
	PromptCacheHitTokens int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptTokensDetails  *struct {
		CachedTokens int `json:"cached_tokens,omitempty"`
		WriteTokens  int `json:"cache_write_tokens,omitempty"`
	} `json:"prompt_tokens_details,omitempty"`
}

type OpenAIChatResponse struct {
	ID      string         `json:"id"`
	Model   string         `json:"model"`
	Choices []OpenAIChoice `json:"choices"`
	Usage   *OpenAIUsage   `json:"usage,omitempty"`
}
