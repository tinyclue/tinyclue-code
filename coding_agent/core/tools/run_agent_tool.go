package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"strings"
)

type RunAgentTool struct {
	*ToolBase
	runner core_types.SubAgentRunner
}

func NewRunAgentTool(runner core_types.SubAgentRunner) *RunAgentTool {
	return &RunAgentTool{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "launch sub-agents for delegated tasks",
		},
		runner: runner,
	}
}

func (t *RunAgentTool) Name() string {
	return core_types.RUN_AGENT_TOOL_NAME
}

func (t *RunAgentTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.RUN_AGENT_TOOL_NAME,
		Description: prompt.GetRunAgentPrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"description",
				"prompt",
				"subagent_type",
			},
			Properties: map[string]apitypes.SchemaItem{
				"description": {
					Type:        "string",
					Description: "A short (3-5 word) description of the task",
				},
				"prompt": {
					Type:        "string",
					Description: "The task for the agent to perform",
				},
				"subagent_type": {
					Type:        "string",
					Description: "The type of specialized agent to use for this task",
				},
				"run_in_background": {
					Type:        "boolean",
					Description: "Set to true to run this agent in the background. You will be notified when it completes.",
				},
				//先不研发SendMessage和worktree
				//"name": {
				//	Type:        "string",
				//	Description: "Name for the spawned agent. Makes it addressable via SendMessage({to: name}) while running.",
				//},
				"isolation": {
					Type:        "string",
					Enum:        []any{"worktree"},
					Description: "Isolation mode. \"worktree\" creates a temporary git worktree so the agent works on an isolated copy of the repo.",
				},
			},
		},
		Strict: true,
	}
}

func (t *RunAgentTool) ValidateInput(_ context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	agentTypeStr, ok := toolUseContext.ToolCall.Arguments["subagent_type"].(string)
	if !ok || agentTypeStr == "" {
		return core_types.ValidateContent{
			Result:  false,
			Message: "subagent_type is required and must be a non-empty string",
		}
	}

	agentType := core_types.AgentType(agentTypeStr)
	if agentType == core_types.GENERAL {
		return core_types.ValidateContent{
			Result:  false,
			Message: "subagent_type cannot be GENERAL",
		}
	}

	found := false
	for _, def := range core_types.AgentDefinitions {
		if def.AgentType == agentType {
			found = true
			break
		}
	}
	if !found {
		return core_types.ValidateContent{
			Result:  false,
			Message: "unknown subagent_type: " + agentTypeStr,
		}
	}

	return core_types.ValidateContent{Result: true}
}

func (t *RunAgentTool) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	if t.runner == nil {
		return t.ErrorReturn(toolCall, fmt.Errorf("sub-agent runner not available"))
	}

	// 解析参数
	agentType := core_types.AgentType(toolUseContext.ToolCall.Arguments["subagent_type"].(string))
	promptStr, _ := toolUseContext.ToolCall.Arguments["prompt"].(string)
	description, _ := toolUseContext.ToolCall.Arguments["description"].(string)
	runInBackground, _ := toolUseContext.ToolCall.Arguments["run_in_background"].(bool)
	isolation, _ := toolUseContext.ToolCall.Arguments["isolation"].(string)

	runnerCtx := &core_types.AgentRunnerContext{
		SourceID:           toolUseContext.AgentUseContext.AgentId,
		AgentType:          agentType,
		Prompt:             promptStr,
		Description:        description,
		RunInBackground:    runInBackground,
		Isolation:          isolation,
		ToolUseId:          toolUseContext.ToolCall.ID,
		OnSubAgentComplete: toolUseContext.AgentUseContext.OnSubAgentComplete,
	}

	// 判断同步/异步
	background := false
	for _, def := range core_types.AgentDefinitions {
		if def.AgentType == agentType {
			background = def.Background
			break
		}
	}
	runnerCtx.Background = background
	runnerCtx.Tools = toolUseContext.Tools

	content, err := t.runner.RunAgent(ctx, runnerCtx)
	if err != nil {
		return t.ErrorReturn(toolCall, err)
	}

	return core_types.ToolContext{
		ToolCall: toolUseContext.ToolCall,
		Content:  content,
	}, nil
}

func (t *RunAgentTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	data, _ := toolContext.Content.(core_types.SubAgentContent)

	if data.Status == "async_launched" {
		return t.BuildAsyncResult(toolContext)
	}
	// failed/killed 也走 BuildSyncResult，带上错误信息或终止状态
	return t.BuildSyncResult(toolContext)
}

func (t *RunAgentTool) BuildSyncResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	data, _ := toolContext.Content.(core_types.SubAgentContent)
	if data.Status == string(core_types.JobStatusFailed) || data.Status == string(core_types.JobStatusKilled) {
		return t.BuildToolResultByText(data.Error, toolContext)
	}
	if len(data.Context) == 0 {
		return t.BuildToolResultByText("(Subagent completed but returned no output.)", toolContext)
	}
	if data.AgentType == core_types.EXPLORE_AGENT_TYPE || data.AgentType == core_types.PLAN_AGENT_TYPE {
		return t.BuildToolResultByBlock(data.Context, toolContext)
	}
	contexts := data.Context
	//the other agent type needs append worktreeText
	worktreeText := buildWorktreeText(data)
	contexts = append(contexts, apitypes.ContentBlock{
		Type: "text",
		Text: worktreeText,
	})
	return t.BuildToolResultByBlock(contexts, toolContext)
}

func buildWorktreeText(data core_types.SubAgentContent) string {
	worktreeInfoText := ""
	if data.WorkTreePath != "" {
		worktreeInfoText = `\nworktreePath: ${worktreePath}\nworktreeBranch: ${worktreeBranch}`
		worktreeInfoText = strings.ReplaceAll(worktreeInfoText, "${worktreePath}", data.WorkTreePath)
		worktreeInfoText = strings.ReplaceAll(worktreeInfoText, "${worktreeBranch}", data.WorkTreeBranch)
	}
	text := `agentId: ${data.agentId} (use SendMessage with to: '${data.agentId}' to continue this agent)${worktreeInfoText}
<usage>total_tokens: ${data.totalTokens}
tool_uses: ${data.totalToolUseCount}
duration_ms: ${data.totalDurationMs}</usage>`
	text = strings.ReplaceAll(text, "${data.agentId}", data.AgentId)
	text = strings.ReplaceAll(text, "${worktreeInfoText}", worktreeInfoText)
	text = strings.ReplaceAll(text, "${data.totalTokens}", fmt.Sprintf("%d", data.TotalTokens))
	text = strings.ReplaceAll(text, "${data.totalToolUseCount}", fmt.Sprintf("%d", data.TotalToolUseCount))
	text = strings.ReplaceAll(text, "${data.totalDurationMs}", fmt.Sprintf("%d", data.TotalDurationMs))

	return text
}

func (t *RunAgentTool) BuildAsyncResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	data, _ := toolContext.Content.(core_types.SubAgentContent)

	prefix := `Async agent launched successfully.\nagentId: ${data.agentId} (internal ID - do not mention to user. Use SendMessage with to: '${data.agentId}' to continue this agent.)\nThe agent is working in the background. You will be notified automatically when it completes.`
	prefix = strings.ReplaceAll(prefix, "${data.agentId}", data.AgentId)

	instructions := `Do not duplicate this agent's work — avoid working with the same files or topics it is using. Work on non-overlapping tasks, or briefly tell the user what you launched and end your response.\noutput_file: ${data.outputFile}\nIf asked, you can check progress before completion by using ${fileReadToolName} or ${bashToolName} tail on the output file.`
	instructions = strings.ReplaceAll(instructions, "${data.outputFile}", data.OutputFilePath)
	instructions = strings.ReplaceAll(instructions, "${fileReadToolName}", core_types.READ_TOOL_NAME)
	instructions = strings.ReplaceAll(instructions, "${bashToolName}", core_types.BASH_TOOL_NAME)

	text := prefix + "\n" + instructions
	return t.BuildToolResultByText(text, toolContext)
}
