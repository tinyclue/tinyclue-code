package types

type AgentType string

const (
	GENERAL            AgentType = "GENERAL"
	EXPLORE_AGENT_TYPE AgentType = "Explore"
	PLAN_AGENT_TYPE    AgentType = "Plan"
	GENERAL_PURPOSE    AgentType = "GeneralPurpose"
	VERIFICATION       AgentType = "Verification"
)

type BaseAgentDefinition struct {
	AgentType              AgentType `json:"agentType"`
	WhenToUse              string    `json:"whenToUse"`
	DisallowedTools        []string  `json:"disallowedTools,omitempty"`
	SystemPrompt           func() string
	Background             bool
	CriticalSystemReminder string
}

var AgentDefinitions = []*BaseAgentDefinition{}
