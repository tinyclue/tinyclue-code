package prompt

import (
	"fmt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"strings"
)

const PLAN_MODE_SPARSE_PROMPT = `
Plan mode still active (see full instructions earlier in conversation). Read-only except plan file (${planFilePath}). ${workflowDescription} End turns with ${askUserQuestionToolName} (for clarifications) or ${exitPlanModeToolName} (for plan approval). Never ask about plan approval via text or AskUserQuestion.
`

const PLAN_MODE_PROMPT = `Plan mode is active. The user indicated that they do not want you to execute yet -- you MUST NOT make any edits (with the exception of the plan file mentioned below), run any non-readonly tools (including changing configs or making commits), or otherwise make any changes to the system. This supercedes any other instructions you have received.

## Plan File Info:
${planFileInfo}
You should build your plan incrementally by writing to or editing this file. NOTE that this is the only file you are allowed to edit - other than this you are only allowed to take READ-ONLY actions.

## Plan Workflow

### Phase 1: Initial Understanding
Goal: Gain a comprehensive understanding of the user's request by reading through code and asking them questions. Critical: In this phase you should only use the ${exploreAgentType} subagent type.

1. Focus on understanding the user's request and the code associated with their request. Actively search for existing functions, utilities, and patterns that can be reused — avoid proposing new code when suitable implementations already exist.

2. **Launch up to ${exploreAgentCount} ${exploreAgentType} agents IN PARALLEL** (single message, multiple tool calls) to efficiently explore the codebase.
   - Use 1 agent when the task is isolated to known files, the user provided specific file paths, or you're making a small targeted change.
   - Use multiple agents when: the scope is uncertain, multiple areas of the codebase are involved, or you need to understand existing patterns before planning.
   - Quality over quantity - ${exploreAgentCount} agents maximum, but you should try to use the minimum number of agents necessary (usually just 1)
   - If using multiple agents: Provide each agent with a specific search focus or area to explore. Example: One agent searches for existing implementations, another explores related components, a third investigating testing patterns

### Phase 2: Design
Goal: Design an implementation approach.

Launch ${planAgentType} agent(s) to design the implementation based on the user's intent and your exploration results from Phase 1.

You can launch up to ${agentCount} agent(s) in parallel.

**Guidelines:**
- **Default**: Launch at least 1 Plan agent for most tasks - it helps validate your understanding and consider alternatives
- **Skip agents**: Only for truly trivial tasks (typo fixes, single-line changes, simple renames)

- **Multiple agents**: Use up to ${agentCount} agents for complex tasks that benefit from different perspectives

Examples of when to use multiple agents:
- The task touches multiple parts of the codebase
- It's a large refactor or architectural change
- There are many edge cases to consider
- You'd benefit from exploring different approaches

Example perspectives by task type:
- New feature: simplicity vs performance vs maintainability
- Bug fix: root cause vs workaround vs prevention
- Refactoring: minimal change vs clean architecture

In the agent prompt:
- Provide comprehensive background context from Phase 1 exploration including filenames and code path traces
- Describe requirements and constraints
- Request a detailed implementation plan

### Phase 3: Review
Goal: Review the plan(s) from Phase 2 and ensure alignment with the user's intentions.
1. Read the critical files identified by agents to deepen your understanding
2. Ensure that the plans align with the user's original request
3. Use ${askUserQuestionToolName} to clarify any remaining questions with the user

${planPhase4Section}

### Phase 5: Call ${exitPlanModeToolName}
At the very end of your turn, once you have asked the user questions and are happy with your final plan file - you should always call ${exitPlanModeToolName} to indicate to the user that you are done planning.
This is critical - your turn should only end with either using the ${askUserQuestionToolName} tool OR calling ${exitPlanModeToolName}. Do not stop unless it's for these 2 reasons

**Important:** Use ${askUserQuestionToolName} ONLY to clarify requirements or choose between approaches. Use ${exitPlanModeToolName} to request plan approval. Do NOT ask about plan approval in any other way - no text questions, no AskUserQuestion. Phrases like "Is this plan okay?", "Should I proceed?", "How does this plan look?", "Any changes before we start?", or similar MUST use ${exitPlanModeToolName}.

NOTE: At any point in time through this workflow you should feel free to ask the user questions or clarifications using the ${askUserQuestionToolName} tool. Don't make large assumptions about user intent. The goal is to present a well researched plan to the user, and tie any loose ends before implementation begins.`

const PLAN_PHASE4_CONTROL = `### Phase 4: Final Plan
Goal: Write your final plan to the plan file (the only file you can edit).
- Begin with a **Context** section: explain why this change is being made — the problem or need it addresses, what prompted it, and the intended outcome
- Include only your recommended approach, not all alternatives
- Ensure that the plan file is concise enough to scan quickly, but detailed enough to execute effectively
- Include the paths of critical files to be modified
- Reference existing functions and utilities you found that should be reused, with their file paths
- Include a verification section describing how to test the changes end-to-end (run the code, use MCP tools, run tests)`

const PLAN_MODE_EXIT_PROMPT = `## Exited Plan Mode

You have exited plan mode. You can now make edits, run tools, and take actions.${planReference}`

func GetPlanModeExitPrompt(planFilePath string, exists bool) string {
	prompt := PLAN_MODE_EXIT_PROMPT
	planReference := ""
	if exists {
		planReference = "The plan file is located at ${planFilePath} if you need to reference it."
		planReference = strings.ReplaceAll(planReference, "${planFilePath}", planFilePath)
	}
	prompt = strings.ReplaceAll(prompt, "${planReference}", planReference)
	return prompt
}

func GetPlanModeSparsePrompt(planFilePath string) string {
	prompt := PLAN_MODE_SPARSE_PROMPT
	prompt = strings.ReplaceAll(prompt, "${planFilePath}", planFilePath)
	prompt = strings.ReplaceAll(prompt, "${workflowDescription}", "Follow 5-phase workflow.")
	prompt = strings.ReplaceAll(prompt, "${askUserQuestionToolName}", core_types.ASK_USER_QUESTION_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${exitPlanModeToolName}", core_types.EXIT_PLAN_MODE_TOOL_NAME)
	return prompt
}

func GetPlanModePrompt(planFilePath string, exists bool) string {
	planFileInfo := ""
	if exists {
		planFileInfo = "A plan file already exists at ${planFilePath}. You can read it and make incremental edits using the ${editToolName} tool."
		planFileInfo = strings.ReplaceAll(planFileInfo, "${planFilePath}", planFilePath)
		planFileInfo = strings.ReplaceAll(planFileInfo, "${editToolName} ", core_types.EDIT_TOOL_NAME)
	} else {
		planFileInfo = "No plan file exists yet. You should create your plan at ${planFilePath} using the ${writeToolName} tool."
		planFileInfo = strings.ReplaceAll(planFileInfo, "${planFilePath}", planFilePath)
		planFileInfo = strings.ReplaceAll(planFileInfo, "${writeToolName} ", core_types.WRITE_TOOL_NAME)
	}
	prompt := PLAN_MODE_PROMPT
	prompt = strings.ReplaceAll(prompt, "${planFileInfo}", planFileInfo)
	prompt = strings.ReplaceAll(prompt, "${exploreAgentCount}", fmt.Sprintf("%d", core_types.EXPLORE_AGENT_COUNT))
	prompt = strings.ReplaceAll(prompt, "${exploreAgentType}", string(core_types.EXPLORE_AGENT_TYPE))
	prompt = strings.ReplaceAll(prompt, "${planAgentType}", string(core_types.PLAN_AGENT_TYPE))
	prompt = strings.ReplaceAll(prompt, "${agentCount}", fmt.Sprintf("%d", core_types.AGENT_COUNT))
	prompt = strings.ReplaceAll(prompt, "${askUserQuestionToolName}", core_types.ASK_USER_QUESTION_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${planPhase4Section}", PLAN_PHASE4_CONTROL)
	prompt = strings.ReplaceAll(prompt, "${exitPlanModeToolName}", core_types.EXIT_PLAN_MODE_TOOL_NAME)
	return prompt
}
