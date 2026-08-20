package types

import (
	"context"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

type ToolRuntimeDetail struct {
	ID                   string
	Tool                 apitypes.ToolCall
	ToolResult           apitypes.ToolResultMessage
	Feedback             *apitypes.UserMessage
	ValidateContent      ValidateContent
	BeforeToolCallResult BeforeToolCallResult
	PermissionResponse   *PermissionResponse
	ToolContext          ToolContext
	Err                  error
}

type ToolRuntimeContext struct {
	Ctx             context.Context
	AgentUseContext *AgentUseContext
	FinalResp       *apitypes.ChatResponse
	ToolsDetail     []*ToolRuntimeDetail // 按 toolCalls 顺序索引；多协程各写自己的 index，互不竞争
}

// GetToolResults 收集整轮所有工具的结果消息（来自 ToolsDetail），按切片顺序（即 tool_use 顺序）。
func (t ToolRuntimeContext) GetToolResults() []apitypes.ToolResultMessage {
	results := make([]apitypes.ToolResultMessage, 0, len(t.ToolsDetail))
	for _, detail := range t.ToolsDetail {
		results = append(results, detail.ToolResult)
	}
	return results
}

// GetFeedbacks 收集批准时用户输入的反馈 user 消息（Feedback 为 nil 的跳过），按切片顺序。
func (t ToolRuntimeContext) GetFeedbacks() []apitypes.UserMessage {
	var feedbacks []apitypes.UserMessage
	for _, detail := range t.ToolsDetail {
		if detail.Feedback == nil {
			continue
		}
		feedbacks = append(feedbacks, *detail.Feedback)
	}
	return feedbacks
}
