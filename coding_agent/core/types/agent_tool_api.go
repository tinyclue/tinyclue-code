package types

import (
	"context"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// AgentToolApi 是工具实例必须实现的接口。
type AgentToolApi interface {
	Name() string
	GetTool() apitypes.Tool
	ValidateInput(ctx context.Context, toolUseContext ToolUseContext) ValidateContent
	BeforeToolCall(ctx context.Context, toolUseContext ToolUseContext) BeforeToolCallResult
	Execute(ctx context.Context, toolUseContext ToolUseContext) (ToolContext, error)
	BuildToolResult(toolContext ToolContext) apitypes.ToolResultMessage
	IsDeferredTool() bool
	GetSearchHint() string
}
