package types

// AgentUseContext 是 agent 运行时的上下文，包含 agent 标识和共享状态指针。
// 所有 *State 字段均为共享指针，各组件（Agent、tools、PlanManager）通过读写同一份数据通信。
// AgentState 挂在 AgentRuntimeContext 上（Agent、PlanManager 等常驻组件无需再单独注入）。
type AgentUseContext struct {
	AgentId            string
	SessionId          string
	SubAgent           bool
	PlanState          *PlanState
	TaskManagerApi     TaskManagerApi
	AgentDef           *BaseAgentDefinition
	RetryAttempt       int
	CurrentContext     *CurrentContext
	OnSubAgentComplete func(SubAgentContent) // 子 agent 完成回调，由 executeAndBuildResult 设置，经 RunAgentTool 透传到 AgentRunner
	StaticOverhead     int
}
