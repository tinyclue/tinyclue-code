package agent_def

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func init() {
	types.AgentDefinitions = append(types.AgentDefinitions, &types.BaseAgentDefinition{
		AgentType:       types.GENERAL,
		WhenToUse:       "",
		DisallowedTools: []string{},
		SystemPrompt: func() string {
			return prompt.GetSystemPrompt()
		},
		Background:             false,
		CriticalSystemReminder: "",
	})
}
