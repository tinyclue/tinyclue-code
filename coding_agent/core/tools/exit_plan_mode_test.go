package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// newExitPlanTestCtx 构造 ExitPlanMode 测试用的 ToolUseContext。
func newExitPlanTestCtx(args map[string]any, subAgent bool, planState *core_types.PlanState) core_types.ToolUseContext {
	return core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: core_types.EXIT_PLAN_MODE_TOOL_NAME, Arguments: args},
		AgentUseContext: &core_types.AgentUseContext{
			SubAgent:  subAgent,
			PlanState: planState,
		},
	}
}

func TestExitPlanModeBeforeToolCallAsk(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(filePath, []byte("plan content"), 0o644); err != nil {
		t.Fatal(err)
	}
	bt := NewExitPlanMode()
	tuc := newExitPlanTestCtx(nil, false, &core_types.PlanState{Active: true, FilePath: filePath})

	res := bt.BeforeToolCall(context.Background(), tuc)
	if res.Action != core_types.BeforeToolCallAsk {
		t.Fatalf("Action = %v, want ask", res.Action)
	}
	if got, _ := res.Data["plan"].(string); got != "plan content" {
		t.Errorf("Data[plan] = %q, want %q", got, "plan content")
	}
	if got, _ := res.Data["planFilePath"].(string); got != filePath {
		t.Errorf("Data[planFilePath] = %q, want %q", got, filePath)
	}
}

func TestExitPlanModeBeforeToolCallSubAgentBypass(t *testing.T) {
	bt := NewExitPlanMode()
	tuc := newExitPlanTestCtx(nil, true, &core_types.PlanState{Active: true})

	res := bt.BeforeToolCall(context.Background(), tuc)
	if res.Action != core_types.BeforeToolCallAllow {
		t.Fatalf("Action = %v, want allow for sub agent", res.Action)
	}
}

// TestExitPlanModeExecuteReadsArgs 验证面板批准回传的计划（args["plan"]）优先于磁盘。
func TestExitPlanModeExecuteReadsArgs(t *testing.T) {
	bt := NewExitPlanMode()
	tuc := newExitPlanTestCtx(map[string]any{
		"plan":         "approved plan",
		"planFilePath": "/tmp/plans/a.md",
	}, false, &core_types.PlanState{Active: true, FilePath: "/tmp/plans/other.md"})

	tc, err := bt.Execute(context.Background(), tuc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	content, ok := tc.Content.(core_types.ExitPlanModeContent)
	if !ok {
		t.Fatalf("Content type = %T, want ExitPlanModeContent", tc.Content)
	}
	if content.Plan != "approved plan" {
		t.Errorf("Plan = %q, want panel-returned plan", content.Plan)
	}
	if content.FilePath != "/tmp/plans/a.md" {
		t.Errorf("FilePath = %q, want args planFilePath", content.FilePath)
	}
	if len(tc.Commands) != 1 || tc.Commands[0] != core_types.ExitPlanCommand {
		t.Errorf("Commands = %v, want [ExitPlanCommand]", tc.Commands)
	}
}

// TestExitPlanModeExecuteFallbackToDisk 验证无回传参数时回退读取磁盘计划。
func TestExitPlanModeExecuteFallbackToDisk(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "plan.md")
	if err := os.WriteFile(filePath, []byte("disk plan"), 0o644); err != nil {
		t.Fatal(err)
	}
	bt := NewExitPlanMode()
	tuc := newExitPlanTestCtx(nil, false, &core_types.PlanState{Active: true, FilePath: filePath})

	tc, err := bt.Execute(context.Background(), tuc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	content, _ := tc.Content.(core_types.ExitPlanModeContent)
	if content.Plan != "disk plan" {
		t.Errorf("Plan = %q, want disk plan", content.Plan)
	}
	if content.FilePath != filePath {
		t.Errorf("FilePath = %q, want %q", content.FilePath, filePath)
	}
}

// TestExitPlanModeBuildToolResultSingleBlock 验证批准后计划内联在单个 text block 中
func TestExitPlanModeBuildToolResultSingleBlock(t *testing.T) {
	bt := NewExitPlanMode()
	tc := core_types.ToolContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: core_types.EXIT_PLAN_MODE_TOOL_NAME},
		Content:  core_types.ExitPlanModeContent{Plan: "the plan", FilePath: "/tmp/plans/a.md"},
	}

	result := bt.BuildToolResult(tc)
	if result.IsError {
		t.Error("expected IsError=false on approval")
	}
	if len(result.Contents) != 1 {
		t.Fatalf("Contents len = %d, want 1 (approval + plan inlined)", len(result.Contents))
	}
	text := result.Contents[0].Text
	if !strings.Contains(text, "## Approved Plan:") {
		t.Errorf("result = %q, want to contain '## Approved Plan:'", text)
	}
	if !strings.Contains(text, "the plan") {
		t.Errorf("result = %q, want to contain plan", text)
	}
	if !strings.Contains(text, "/tmp/plans/a.md") {
		t.Errorf("result = %q, want to contain file path", text)
	}
}

func TestExitPlanModeBuildToolResultSubAgent(t *testing.T) {
	bt := NewExitPlanMode()
	tc := core_types.ToolContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: core_types.EXIT_PLAN_MODE_TOOL_NAME},
		Content:  core_types.ExitPlanModeContent{Plan: "the plan", IsSubAgent: true},
	}

	result := bt.BuildToolResult(tc)
	if len(result.Contents) != 1 {
		t.Fatalf("Contents len = %d, want 1 for sub agent", len(result.Contents))
	}
	if !strings.Contains(result.Contents[0].Text, "respond with") {
		t.Errorf("sub agent result = %q, want approval-ok text", result.Contents[0].Text)
	}
}

func TestExitPlanModeBuildToolResultEmptyPlan(t *testing.T) {
	bt := NewExitPlanMode()
	tc := core_types.ToolContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: core_types.EXIT_PLAN_MODE_TOOL_NAME},
		Content:  core_types.ExitPlanModeContent{},
	}

	result := bt.BuildToolResult(tc)
	if len(result.Contents) != 1 {
		t.Fatalf("Contents len = %d, want 1 for empty plan", len(result.Contents))
	}
	if !strings.Contains(result.Contents[0].Text, "approved exiting plan mode") {
		t.Errorf("empty plan result = %q, want approval text", result.Contents[0].Text)
	}
}
