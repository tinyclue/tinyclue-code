package prompt

import (
	"fmt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"strings"
)

const WRITING_THE_PROMPT_SECTION = `

## Writing the prompt

Brief the agent like a smart colleague who just walked into the room — it hasn't seen this conversation, doesn't know what you've tried, doesn't understand why this task matters.
- Explain what you're trying to accomplish and why.
- Describe what you've already learned or ruled out.
- Give enough context about the surrounding problem that the agent can make judgment calls rather than just following a narrow instruction.
- If you need a short response, say so ("report in under 200 words").
- Lookups: hand over the exact command. Investigations: hand over the question — prescribed steps become dead weight when the premise is wrong.

Terse command-style prompts produce shallow, generic work.

**Never delegate understanding.** Don't write "based on your findings, fix the bug" or "based on the research, implement it." Those phrases push synthesis onto the agent instead of doing it yourself. Write prompts that prove you understood: include file paths, line numbers, what specifically to change.
`

const CURRENT_EXAMPLES = `Example usage:

<example_agent_descriptions>
"test-runner": use this agent after you are done writing code to run tests
"greeting-responder": use this agent to respond to user greetings with a friendly joke
</example_agent_descriptions>

<example>
user: "Please write a function that checks if a number is prime"
assistant: I'm going to use the ${fileWriteToolName} tool to write the following code:
<code>
function isPrime(n) {
  if (n <= 1) return false
  for (let i = 2; i * i <= n; i++) {
    if (n % i === 0) return false
  }
  return true
}
</code>
<commentary>
Since a significant piece of code was written and the task was completed, now use the test-runner agent to run the tests
</commentary>
assistant: Uses the ${agentToolName} tool to launch the test-runner agent
</example>

<example>
user: "Hello"
<commentary>
Since the user is greeting, use the greeting-responder agent to respond with a friendly joke
</commentary>
assistant: "I'm going to use the ${agentToolName} tool to launch the greeting-responder agent"
</example>
`

const SHARED = `Launch a new agent to handle complex, multi-step tasks autonomously.

The ${agentToolName} tool launches specialized agents (subprocesses) that autonomously handle complex tasks. Each agent type has specific capabilities and tools available to it.

${agentListSection}

When using the ${agentToolName} tool, specify a subagent_type parameter to select which agent type to use. If omitted, the general-purpose agent is used.

`

const WHEN_NOT_TO_USE_SECTION = `When NOT to use the ${agentToolName} tool:
- If you want to read a specific file path, use the ${fileReadToolName} tool or ${fileSearchHint} instead of the ${agentToolName} tool, to find the match more quickly
- If you are searching for a specific class definition like "class Foo", use ${contentSearchHint} instead, to find the match more quickly
- If you are searching for code within a specific file or set of 2-3 files, use the ${fileReadToolName} tool instead of the ${agentToolName} tool, to find the match more quickly
- Other tasks that are not related to the agent descriptions above
`

const CONCURRENCY_NOTE = `
- Launch multiple agents concurrently whenever possible, to maximize performance; to do that, use a single message with multiple tool uses`

func GetCurrentExamples() string {
	prompt := CURRENT_EXAMPLES
	prompt = strings.ReplaceAll(prompt, "${fileWriteToolName}", core_types.WRITE_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${agentToolName}", core_types.RUN_AGENT_TOOL_NAME)
	return prompt
}

func GetShared() string {
	prompt := SHARED
	prompt = strings.ReplaceAll(prompt, "${agentToolName}", core_types.RUN_AGENT_TOOL_NAME)
	agentListSection := GetAgentSection()

	prompt = strings.ReplaceAll(prompt, "${agentListSection}", agentListSection)

	return prompt
}

func GetWhenNotToUseSection() string {
	prompt := WHEN_NOT_TO_USE_SECTION
	fileSearchHint := `the ` + core_types.GLOB_TOOL_NAME + ` tool`
	contentSearchHint := `the ` + core_types.GLOB_TOOL_NAME + ` tool`
	prompt = strings.ReplaceAll(prompt, "${agentToolName}", core_types.RUN_AGENT_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${fileReadToolName}", core_types.READ_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${fileSearchHint}", fileSearchHint)
	prompt = strings.ReplaceAll(prompt, "${contentSearchHint}", contentSearchHint)

	return prompt
}

func GetAgentSection() string {
	agentListSection := `Available agent types and the tools they have access to:`
	var agentDesc []string
	for _, agent := range core_types.AgentDefinitions {
		if agent.AgentType == core_types.GENERAL {
			continue
		}
		line := `- ${agentType}: ${whenToUse} (Tools: ${toolsDescription})`
		line = strings.ReplaceAll(line, "${agentType}", string(agent.AgentType))
		line = strings.ReplaceAll(line, "${whenToUse}", agent.WhenToUse)
		line = strings.ReplaceAll(line, "${toolsDescription}", GetToolsDescription(agent))
		agentDesc = append(agentDesc, line)
	}
	agentListSection += "\n" + strings.Join(agentDesc, "\n")
	return agentListSection
}

func GetToolsDescription(agent *core_types.BaseAgentDefinition) string {
	if len(agent.DisallowedTools) > 0 {
		return fmt.Sprintf("All tools except %s", strings.Join(agent.DisallowedTools, ","))
	}
	return "All tools"
}

const RUN_AGENT_PROMPT = `${shared}
${whenNotToUseSection}

Usage notes:
- Always include a short description (3-5 words) summarizing what the agent will do${concurrencyNote}
- When the agent is done, it will return a single message back to you. The result returned by the agent is not visible to the user. To show the user the result, you should send a text message back to the user with a concise summary of the result.
- You can optionally run agents in the background using the run_in_background parameter. When an agent runs in the background, you will be automatically notified when it completes — do NOT sleep, poll, or proactively check on its progress. Continue with other work or respond to the user instead.
- **Foreground vs background**: Use foreground (default) when you need the agent's results before you can proceed — e.g., research agents whose findings inform your next steps. Use background when you have genuinely independent work to do in parallel.

- To continue a previously spawned agent, use ${sendMessageToolName} with the agent's ID or name as the ` + "`to`" + ` field. The agent resumes with its full context preserved. Each Agent invocation starts fresh — provide a complete task description.
- The agent's outputs should generally be trusted.
- Clearly tell the agent whether you expect it to write code or just to do research (search, file reads, web fetches, etc.), since it is not aware of the user's intent.
- If the agent description mentions that it should be used proactively, then you should try your best to use it without the user having to ask for it first. Use your judgement.
- If the user specifies that they want you to run agents "in parallel", you MUST send a single message with multiple ${agentToolName} tool use content blocks. For example, if you need to launch both a build-validator agent and a test-runner agent in parallel, send a single message with both tool calls.
- You can optionally set ` + "`" + `isolation: "worktree"` + "`" + ` to run the agent in a temporary git worktree, giving it an isolated copy of the repository. The worktree is automatically cleaned up if the agent makes no changes; if changes are made, the worktree path and branch are returned in the result.
${writingThePromptSection}

${currentExamples}`

func GetRunAgentPrompt() string {
	prompt := RUN_AGENT_PROMPT
	prompt = strings.ReplaceAll(prompt, "${shared}", GetShared())
	prompt = strings.ReplaceAll(prompt, "${whenNotToUseSection}", GetWhenNotToUseSection())
	prompt = strings.ReplaceAll(prompt, "${concurrencyNote}", CONCURRENCY_NOTE)
	prompt = strings.ReplaceAll(prompt, "${sendMessageToolName}", core_types.SEND_MESSAGE_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${agentToolName}", core_types.RUN_AGENT_TOOL_NAME)
	prompt = strings.ReplaceAll(prompt, "${writingThePromptSection}", WRITING_THE_PROMPT_SECTION)
	prompt = strings.ReplaceAll(prompt, "${currentExamples}", GetCurrentExamples())
	return prompt
}
