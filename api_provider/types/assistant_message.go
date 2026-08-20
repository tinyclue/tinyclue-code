package types

import (
	"time"
)

// TextContent AssistantMessage 的文本内容。
type TextContent struct {
	Text string `json:"text"`
}

// ThinkingContent AssistantMessage 的思考内容。
type ThinkingContent struct {
	Thinking string `json:"thinking"`
}

// AssistantMessage 助手的回复消息，包含文本、思考过程、工具调用及元数据。
type AssistantMessage struct {
	Role            MessageRole     `json:"role"`
	TextContent     TextContent     `json:"textContent"`
	ThinkingContent ThinkingContent `json:"thinkingContent,omitempty"`
	ToolCalls       []ToolCall      `json:"toolCalls,omitempty"`
	Usage           Usage           `json:"usage,omitempty"`
	StopReason      StopReason      `json:"stopReason,omitempty"`
	API             string          `json:"api,omitempty"`
	Provider        string          `json:"provider,omitempty"`
	Model           string          `json:"model,omitempty"`
	ResponseModel   string          `json:"responseModel,omitempty"`
	ResponseID      string          `json:"responseId,omitempty"`
	Timestamp       int64           `json:"timestamp,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
}

func (AssistantMessage) RoleName() MessageRole { return AssistantRole }

func NewAssistantMessage(text string, thinking string, toolCalls []ToolCall) AssistantMessage {
	return AssistantMessage{
		Role:            AssistantRole,
		TextContent:     TextContent{Text: text},
		ThinkingContent: ThinkingContent{Thinking: thinking},
		ToolCalls:       toolCalls,
		CreatedAt:       time.Now(),
	}
}
