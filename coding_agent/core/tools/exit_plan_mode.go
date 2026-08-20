package tools

import (
	"context"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"strings"
)

type ExitPlanMode struct {
	*ToolBase
}

func NewExitPlanMode() *ExitPlanMode {
	return &ExitPlanMode{
		ToolBase: &ToolBase{
			deferred: true,
		},
	}
}

func (bt *ExitPlanMode) Name() string {
	return core_types.EXIT_PLAN_MODE_TOOL_NAME
}

func (bt *ExitPlanMode) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_types.EXIT_PLAN_MODE_TOOL_NAME,
		Description: prompt.GetExitPlanModeToolPrompt(),
		Parameters: apitypes.Parameters{
			Type:       "object",
			Properties: map[string]apitypes.SchemaItem{},
			Required:   []string{},
		},
		Strict: true,
	}
	return result
}

func (tb *ExitPlanMode) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	if toolUseContext.AgentUseContext.PlanState != nil && !toolUseContext.AgentUseContext.PlanState.Active {
		return core_types.ValidateContent{
			Result:  false,
			Message: "You are not in plan mode. This tool is only for exiting plan mode after writing a plan. If your plan was already approved, continue with implementation.",
		}
	}
	return core_types.ValidateContent{
		Result: true,
	}
}

// 返回 ask，teammate 绕过面板）。子 agent 不弹确认面板（由主 agent 统一审批）。
// 把 plan 内容经 Data 带给 TUI 面板展示；批准后由面板经 ModifiedArgs 回传。
func (bt *ExitPlanMode) BeforeToolCall(_ context.Context, toolUseContext core_types.ToolUseContext) core_types.BeforeToolCallResult {
	if toolUseContext.AgentUseContext.SubAgent {
		return core_types.BeforeToolCallResult{Action: core_types.BeforeToolCallAllow}
	}
	plan := ""
	filePath := ""
	if toolUseContext.AgentUseContext.PlanState != nil {
		filePath = toolUseContext.AgentUseContext.PlanState.FilePath
		plan, _ = utils.ReadFile(filePath)
	}
	return core_types.BeforeToolCallResult{
		Action:  core_types.BeforeToolCallAsk,
		Message: "Exit plan mode?",
		Data: map[string]any{
			"plan":         plan,
			"planFilePath": filePath,
		},
	}
}

func (bt *ExitPlanMode) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	// 优先取面板批准时回传的计划（允许用户编辑后的内容）；否则回退磁盘 PlanState.FilePath。
	filePath, _ := toolUseContext.ToolCall.Arguments["planFilePath"].(string)
	plan, _ := toolUseContext.ToolCall.Arguments["plan"].(string)
	if filePath == "" && toolUseContext.AgentUseContext.PlanState != nil {
		filePath = toolUseContext.AgentUseContext.PlanState.FilePath
	}
	if plan == "" {
		plan, _ = utils.ReadFile(filePath)
	}
	return core_types.ToolContext{
		ToolCall: toolUseContext.ToolCall,
		Content: core_types.ExitPlanModeContent{
			Plan:     plan,
			FilePath: filePath,
		},
		Commands: []core_types.ToolCommand{core_types.ExitPlanCommand},
	}, nil
}

func (bt *ExitPlanMode) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, _ := toolContext.Content.(core_types.ExitPlanModeContent)

	if content.IsSubAgent {
		text := "User has approved the plan. There is nothing else needed from you now. Please respond with \"ok\""
		return bt.BuildToolResultByText(text, toolContext)
	}
	if content.Plan == "" {
		text := "User has approved exiting plan mode. You can now proceed."
		return bt.BuildToolResultByText(text, toolContext)
	}
	// 不是拆分 ContentBlock 数组）。用户批准时输入的反馈由 agent_loop 层追加为独立 text block
	text := `User has approved your plan. You can now start coding. Start with updating your todo list if applicable

Your plan has been saved to: ${filePath}
You can refer back to it if needed during implementation.

## Approved Plan:
${plan}`
	text = strings.ReplaceAll(text, "${filePath}", content.FilePath)
	text = strings.ReplaceAll(text, "${plan}", content.Plan)

	return bt.BuildToolResultByText(text, toolContext)
}
