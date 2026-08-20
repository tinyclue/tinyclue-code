package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type EnterPlanMode struct {
	*ToolBase
}

func NewEnterPlanMode() *EnterPlanMode {
	return &EnterPlanMode{
		ToolBase: &ToolBase{
			deferred: true,
		},
	}
}

func (bt *EnterPlanMode) Name() string {
	return core_types.ENTER_PLAN_MODE_TOOL_NAME
}

func (bt *EnterPlanMode) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_types.ENTER_PLAN_MODE_TOOL_NAME,
		Description: prompt.GetEnterPlanModeToolPrompt(),
		Parameters: apitypes.Parameters{
			Type:       "object",
			Properties: map[string]apitypes.SchemaItem{},
			Required:   []string{},
		},
		Strict: true,
	}
	return result
}

func (bt *EnterPlanMode) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.PlanModeContent{
			Message: "Entered plan mode. You should now focus on exploring the codebase and designing an implementation approach.",
		},
		Commands: []core_types.ToolCommand{core_types.EnterPlanCommand},
	}, nil
}

func (bt *EnterPlanMode) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, _ := toolContext.Content.(core_types.PlanModeContent)

	instructions := fmt.Sprintf(`%s

In plan mode, you should:
1. Thoroughly explore the codebase to understand existing patterns
2. Identify similar features and architectural approaches
3. Consider multiple approaches and their trade-offs
4. Use AskUserQuestion if you need to clarify the approach
5. Design a concrete implementation strategy
6. When ready, use ExitPlanMode to present your plan for approval

Remember: DO NOT write or edit any files yet. This is a read-only exploration and planning phase.`, content.Message)

	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		Contents:   []apitypes.ContentBlock{{Type: "text", Text: instructions}},
		IsError:    false,
	}
}
