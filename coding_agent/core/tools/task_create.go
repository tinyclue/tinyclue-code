package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

//- TaskCreateTool / TaskGetTool / TaskListTool / TaskUpdateTool → 操作 ~/.tinyclue/tasks/*.json（Todo Task 文件存储）
//  - TaskStopTool / TaskOutputTool → 操作 AppState.tasks（运行时进程，比如后台 shell、子 agent）

type TaskCreate struct {
	*ToolBase
}

func NewTaskCreate() *TaskCreate {
	return &TaskCreate{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "create a task in the task list",
		},
	}
}

func (bt *TaskCreate) Name() string {
	return core_types.TASK_CREATE_TOOL_NAME
}

func (bt *TaskCreate) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.TASK_CREATE_TOOL_NAME,
		Description: prompt.GetTaskCreatePrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"subject",
				"description",
			},
			Properties: map[string]apitypes.SchemaItem{
				"subject": {
					Type:        "string",
					Description: "A brief title for the task",
				},
				"description": {
					Type:        "string",
					Description: "What needs to be done",
				},
				"activeForm": {
					Type:        "string",
					Description: `Present continuous form shown in the spinner when the task is in_progress (e.g., "Running tests"). If omitted, the spinner shows the subject instead.`,
				},
				"metadata": {
					Type:        "object",
					Description: "Arbitrary metadata to attach to the task",
				},
			},
		},
		Strict: true,
	}
}

func (bt *TaskCreate) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	args := toolUseContext.ToolCall.Arguments

	subject, _ := args["subject"].(string)
	if subject == "" {
		return core_types.ValidateContent{
			Result:  false,
			Message: "subject is required",
		}
	}

	description, _ := args["description"].(string)
	if description == "" {
		return core_types.ValidateContent{
			Result:  false,
			Message: "description is required",
		}
	}

	return core_types.ValidateContent{Result: true}
}

func (bt *TaskCreate) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	args := toolCall.Arguments

	subject, _ := args["subject"].(string)
	description, _ := args["description"].(string)
	activeForm, _ := args["activeForm"].(string)
	metadata, _ := args["metadata"].(map[string]any)

	taskManager := toolUseContext.AgentUseContext.TaskManagerApi

	var opts []core_types.TaskCreateOption
	if activeForm != "" {
		opts = append(opts, core_types.WithActiveForm(activeForm))
	}
	if metadata != nil {
		opts = append(opts, core_types.WithMetadata(metadata))
	}

	task, err := taskManager.Create(subject, description, opts...)
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("TaskCreate failed: %w", err))
	}

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TaskCreateContent{
			TaskID:  task.ID,
			Subject: task.Subject,
		},
	}, nil
}

func (bt *TaskCreate) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.TaskCreateContent)
	if !ok {
		return bt.BuildToolResultByText("", toolContext)
	}

	text := fmt.Sprintf("Task #%s created successfully: %s", content.TaskID, content.Subject)
	return bt.BuildToolResultByText(text, toolContext)
}
