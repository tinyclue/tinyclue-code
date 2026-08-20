package types

import "context"

// SubAgentRunner 子 agent 启动器，由 core 包实现，通过构造注入给 RunAgentTool。
type SubAgentRunner interface {
	RunAgent(ctx context.Context, params *AgentRunnerContext) (SubAgentContent, error)
}

// AgentRunnerContext 运行子 agent 的参数。
type AgentRunnerContext struct {
	SourceID           string
	AgentType          AgentType
	Prompt             string
	Description        string
	RunInBackground    bool
	Isolation          string
	Background         bool
	ToolUseId          string
	Tools              []AgentToolApi
	OnSubAgentComplete func(SubAgentContent) // 由上层设置，子 agent 完成后回调（sync/async 均触发）
}
