package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"strings"
)

type TaskList struct {
	*ToolBase
}

func NewTaskList() *TaskList {
	return &TaskList{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "list all tasks",
		},
	}
}

func (bt *TaskList) Name() string {
	return core_types.TASK_LIST_TOOL_NAME
}

func (bt *TaskList) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.TASK_LIST_TOOL_NAME,
		Description: prompt.GetTaskListPrompt(),
		Parameters: apitypes.Parameters{
			Type:       "object",
			Required:   []string{},
			Properties: map[string]apitypes.SchemaItem{},
		},
		Strict: true,
	}
}

func (bt *TaskList) ValidateInput(_ context.Context, _ core_types.ToolUseContext) core_types.ValidateContent {
	return core_types.ValidateContent{Result: true}
}

func (bt *TaskList) Execute(_ context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	taskManager := toolUseContext.AgentUseContext.TaskManagerApi

	allTasks, err := taskManager.List()
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("TaskList failed: %w", err))
	}

	// Build set of completed task IDs for blocking filter.
	resolvedIDs := make(map[string]bool)
	for _, t := range allTasks {
		if t.Status == core_types.TaskStatusCompleted {
			resolvedIDs[t.ID] = true
		}
	}

	tasks := make([]core_types.TaskListTask, 0, len(allTasks))
	for _, t := range allTasks {
		// Filter out resolved blockers.
		blockedBy := make([]string, 0, len(t.BlockedBy))
		for _, b := range t.BlockedBy {
			if !resolvedIDs[b] {
				blockedBy = append(blockedBy, b)
			}
		}

		tasks = append(tasks, core_types.TaskListTask{
			ID:        t.ID,
			Subject:   t.Subject,
			Status:    string(t.Status),
			Owner:     t.Owner,
			BlockedBy: blockedBy,
		})
	}

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TaskListContent{
			Tasks: tasks,
		},
	}, nil
}

func (bt *TaskList) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.TaskListContent)
	if !ok {
		return bt.BuildToolResultByText("", toolContext)
	}

	if len(content.Tasks) == 0 {
		return bt.BuildToolResultByText("No tasks found", toolContext)
	}

	lines := make([]string, 0, len(content.Tasks))
	for _, task := range content.Tasks {
		owner := ""
		if task.Owner != "" {
			owner = " (" + task.Owner + ")"
		}
		blocked := ""
		if len(task.BlockedBy) > 0 {
			ids := make([]string, len(task.BlockedBy))
			for i, b := range task.BlockedBy {
				ids[i] = "#" + b
			}
			blocked = " [blocked by " + strings.Join(ids, ", ") + "]"
		}
		lines = append(lines, fmt.Sprintf("#%s [%s] %s%s%s", task.ID, task.Status, task.Subject, owner, blocked))
	}

	return bt.BuildToolResultByText(strings.Join(lines, "\n"), toolContext)
}
