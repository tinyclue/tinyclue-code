package main

import (
	"context"
	"fmt"
	"os"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/task"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func main() {
	// 1. Create temp dir for task storage.
	dir, err := os.MkdirTemp("", "task_create_test")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	// 2. Create TaskManager (satisfies task.TaskManagerApi).
	tm := task.NewWithDir("test-agent", dir, "test-session")

	// 3. Create TaskCreate tool.
	tc := tools.NewTaskCreate()

	// 4. Build ToolCall with arguments.
	toolCall := apitypes.ToolCall{
		ID:   "call_1",
		Name: core_types.TASK_CREATE_TOOL_NAME,
		Arguments: map[string]any{
			"subject":     "Implement login",
			"description": "Add OAuth login flow",
			"activeForm":  "Implementing login...",
		},
	}

	// 5. Build AgentUseContext with the TaskManagerApi.
	agentUseCtx := &core_types.AgentUseContext{
		AgentId:        "test-agent",
		SessionId:      "test-session",
		TaskManagerApi: tm,
	}

	// 6. Build ToolUseContext.
	toolUseCtx := core_types.NewToolUseContext(nil, toolCall, agentUseCtx)

	// 7. Execute TaskCreate.
	result, err := tc.Execute(context.Background(), toolUseCtx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Execute failed: %v\n", err)
		os.Exit(1)
	}

	// 8. Verify result content.
	content, ok := result.Content.(core_types.TaskCreateContent)
	if !ok {
		fmt.Fprintf(os.Stderr, "unexpected content type: %T\n", result.Content)
		os.Exit(1)
	}

	fmt.Printf("=== TaskCreate Execute Result ===\n")
	fmt.Printf("  TaskID:  %s\n", content.TaskID)
	fmt.Printf("  Subject: %s\n", content.Subject)

	// 9. Verify via BuildToolResult.
	toolResult := tc.BuildToolResult(result)
	fmt.Printf("  Result:  %s\n", toolResult.Contents[0].Text)

	// 10. List all tasks to verify persistence.
	tasks, err := tm.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "List failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("\n=== Task List (%d task(s)) ===\n", len(tasks))
	for _, t := range tasks {
		fmt.Printf("  #%s: %s [%s]\n", t.ID, t.Subject, t.Status)
	}

	// 11. Test with metadata.
	fmt.Println("\n=== Creating task with metadata ===")
	toolCall2 := apitypes.ToolCall{
		ID:   "call_2",
		Name: core_types.TASK_CREATE_TOOL_NAME,
		Arguments: map[string]any{
			"subject":     "Setup CI",
			"description": "Configure GitHub Actions",
			"metadata": map[string]any{
				"priority": "high",
			},
		},
	}
	toolUseCtx2 := core_types.NewToolUseContext(nil, toolCall2, agentUseCtx)
	result2, err := tc.Execute(context.Background(), toolUseCtx2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Second Execute failed: %v\n", err)
		os.Exit(1)
	}
	content2 := result2.Content.(core_types.TaskCreateContent)
	fmt.Printf("  TaskID:  %s\n", content2.TaskID)
	fmt.Printf("  Subject: %s\n", content2.Subject)

	// List again — second task should have ID "2".
	tasks, _ = tm.List()
	fmt.Printf("\n=== Task List (%d task(s)) ===\n", len(tasks))
	for _, t := range tasks {
		fmt.Printf("  #%s: %s [%s]\n", t.ID, t.Subject, t.Status)
	}
}
