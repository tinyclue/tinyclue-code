package types

type AgentRuntimeContext struct {
	AgentId    string
	AgentType  AgentType
	SubAgent   bool
	SessionId  string
	AgentState *AgentState
}
