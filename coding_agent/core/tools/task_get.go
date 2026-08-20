package tools

import (
	"context"
	"fmt"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type TaskGet struct {
	*ToolBase
}

func NewTaskGet() *TaskGet {
	return &TaskGet{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "retrieve a task by ID",
		},
	}
}

func (bt *TaskGet) Name() string {
	return core_types.TASK_GET_TOOL_NAME
}

func (bt *TaskGet) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.TASK_GET_TOOL_NAME,
		Description: prompt.GetTaskGetPrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"taskId",
			},
			Properties: map[string]apitypes.SchemaItem{
				"taskId": {
					Type:        "string",
					Description: "The ID of the task to retrieve",
				},
			},
		},
		Strict: true,
	}
}

func (bt *TaskGet) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	args := toolUseContext.ToolCall.Arguments

	taskId, _ := args["taskId"].(string)
	if taskId == "" {
		return core_types.ValidateContent{
			Result:  false,
			Message: "taskId is required",
		}
	}

	return core_types.ValidateContent{Result: true}
}

func (bt *TaskGet) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	args := toolCall.Arguments
	taskManager := toolUseContext.AgentUseContext.TaskManagerApi

	taskId, _ := args["taskId"].(string)

	task, err := taskManager.Get(taskId)
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("TaskGet failed: %w", err))
	}
	if task == nil {
		return core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.TaskGetContent{
				Task: nil,
			},
		}, nil
	}

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TaskGetContent{
			Task: &core_types.TaskGetTask{
				ID:          task.ID,
				Subject:     task.Subject,
				Description: task.Description,
				Status:      string(task.Status),
				Blocks:      task.Blocks,
				BlockedBy:   task.BlockedBy,
			},
		},
	}, nil
}

func (bt *TaskGet) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.TaskGetContent)
	if !ok {
		return bt.BuildToolResultByText("", toolContext)
	}

	if content.Task == nil {
		return bt.BuildToolResultByText("Task not found", toolContext)
	}

	task := content.Task
	lines := []string{
		fmt.Sprintf("Task #%s: %s", task.ID, task.Subject),
		fmt.Sprintf("Status: %s", task.Status),
		fmt.Sprintf("Description: %s", task.Description),
	}

	if len(task.BlockedBy) > 0 {
		ids := make([]string, len(task.BlockedBy))
		for i, id := range task.BlockedBy {
			ids[i] = "#" + id
		}
		lines = append(lines, "Blocked by: "+strings.Join(ids, ", "))
	}
	if len(task.Blocks) > 0 {
		ids := make([]string, len(task.Blocks))
		for i, id := range task.Blocks {
			ids[i] = "#" + id
		}
		lines = append(lines, "Blocks: "+strings.Join(ids, ", "))
	}

	return bt.BuildToolResultByText(strings.Join(lines, "\n"), toolContext)
}
