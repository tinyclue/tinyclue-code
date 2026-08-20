package types

import "time"

// ContentBlock 表示 tool result 中的一个内容块。
type ContentBlock struct {
	Type          string `json:"type"` // "text" | "image"
	Text          string `json:"text,omitempty"`
	ImageData     string `json:"imageData,omitempty"`
	ImageMimeType string `json:"imageMimeType,omitempty"`
}

type ToolResultMessage struct {
	Role           MessageRole      `json:"role"`
	ToolCallId     string           `json:"toolCallId"`
	ToolName       string           `json:"toolName"`
	IsError        bool             `json:"isError"`
	CreatedAt      time.Time        `json:"createdAt"`
	ToolReferences []map[string]any `json:"toolReferences,omitempty"`
	Contents       []ContentBlock   `json:"contents"`
}

func (ToolResultMessage) RoleName() MessageRole { return ToolRole }
