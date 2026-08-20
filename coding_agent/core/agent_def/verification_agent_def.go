package agent_def

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func init() {
	types.AgentDefinitions = append(types.AgentDefinitions, &types.BaseAgentDefinition{
		AgentType: types.VERIFICATION,
		WhenToUse: prompt.VERIFICATION_WHEN_TO_USE,
		DisallowedTools: []string{
			types.RUN_AGENT_TOOL_NAME,
			types.ENTER_PLAN_MODE_TOOL_NAME,
			types.EXIT_PLAN_MODE_TOOL_NAME,
			types.EDIT_TOOL_NAME,
			types.WRITE_TOOL_NAME,
		},
		SystemPrompt: func() string {
			return prompt.GetVerificationSystemPrompt()
		},
		Background:             true,
		CriticalSystemReminder: prompt.VERIFICATION_REMINDER,
	})
}
