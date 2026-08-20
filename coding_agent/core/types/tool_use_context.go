package types

import (
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// ToolUseContext 工具执行上下文。
type ToolUseContext struct {
	Tools           []AgentToolApi
	ToolCall        apitypes.ToolCall
	AgentUseContext *AgentUseContext
	OnProgress      func(ToolContext) // 进度回调，由调用方传入，不通过工具实例绑定
}

func NewToolUseContext(tools []AgentToolApi, toolCall apitypes.ToolCall, agentUseContext *AgentUseContext) ToolUseContext {
	return ToolUseContext{
		Tools:           tools,
		ToolCall:        toolCall,
		AgentUseContext: agentUseContext,
	}
}

func (tc ToolUseContext) WithOnProgress(cb func(ToolContext)) ToolUseContext {
	tc.OnProgress = cb
	return tc
}
