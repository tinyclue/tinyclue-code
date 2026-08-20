package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"strings"
)

type TaskUpdate struct {
	*ToolBase
}

func NewTaskUpdate() *TaskUpdate {
	return &TaskUpdate{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "update a task",
		},
	}
}

func (bt *TaskUpdate) Name() string {
	return core_types.TASK_UPDATE_TOOL_NAME
}

func (bt *TaskUpdate) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.TASK_UPDATE_TOOL_NAME,
		Description: prompt.GetTaskUpdatePrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"taskId",
			},
			Properties: map[string]apitypes.SchemaItem{
				"taskId": {
					Type:        "string",
					Description: "The ID of the task to update",
				},
				"subject": {
					Type:        "string",
					Description: "New subject for the task",
				},
				"description": {
					Type:        "string",
					Description: "New description for the task",
				},
				"activeForm": {
					Type:        "string",
					Description: `Present continuous form shown in spinner when in_progress (e.g., "Running tests")`,
				},
				"status": {
					Type:        "string",
					Description: "New status for the task (pending, in_progress, completed, deleted)",
				},
				"addBlocks": {
					Type:        "array",
					Description: "Task IDs that this task blocks",
					Items: &apitypes.SchemaItem{
						Type: "string",
					},
				},
				"addBlockedBy": {
					Type:        "array",
					Description: "Task IDs that block this task",
					Items: &apitypes.SchemaItem{
						Type: "string",
					},
				},
				"owner": {
					Type:        "string",
					Description: "New owner for the task",
				},
				"metadata": {
					Type:        "object",
					Description: "Metadata keys to merge into the task. Set a key to null to delete it.",
				},
			},
		},
		Strict: true,
	}
}

func (bt *TaskUpdate) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
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

func (bt *TaskUpdate) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	args := toolCall.Arguments
	taskManager := toolUseContext.AgentUseContext.TaskManagerApi

	taskId, _ := args["taskId"].(string)

	// 1. Check if task exists.
	existingTask, err := taskManager.Get(taskId)
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("TaskUpdate failed: %w", err))
	}
	if existingTask == nil {
		return core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.TaskUpdateContent{
				Success: false,
				TaskID:  taskId,
				Error:   "Task not found",
			},
		}, nil
	}

	// 2. Handle deletion.
	statusRaw, statusProvided := args["status"].(string)
	if statusProvided && statusRaw == "deleted" {
		deleted, err := taskManager.Delete(taskId)
		if err != nil {
			return bt.ErrorReturn(toolCall, fmt.Errorf("TaskUpdate failed: %w", err))
		}
		updatedFields := []string{}
		if deleted {
			updatedFields = append(updatedFields, "deleted")
		}
		return core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.TaskUpdateContent{
				Success:       deleted,
				TaskID:        taskId,
				UpdatedFields: updatedFields,
				Error: func() string {
					if deleted {
						return ""
					}
					return "Failed to delete task"
				}(),
				StatusChange: &core_types.StatusChange{
					From: string(existingTask.Status),
					To:   "deleted",
				},
			},
		}, nil
	}

	// 3. Build update request with field diffs (only when changed).
	var updatedFields []string
	req := core_types.TaskUpdateRequest{}

	if subject, ok := args["subject"].(string); ok && subject != existingTask.Subject {
		req.Subject = &subject
		updatedFields = append(updatedFields, "subject")
	}
	if desc, ok := args["description"].(string); ok && desc != existingTask.Description {
		req.Description = &desc
		updatedFields = append(updatedFields, "description")
	}
	if af, ok := args["activeForm"].(string); ok && af != existingTask.ActiveForm {
		req.ActiveForm = &af
		updatedFields = append(updatedFields, "activeForm")
	}
	if owner, ok := args["owner"].(string); ok && owner != existingTask.Owner {
		req.Owner = &owner
		updatedFields = append(updatedFields, "owner")
	}
	// Metadata — merge handled inside Update.
	if metadata, ok := args["metadata"].(map[string]any); ok {
		req.Metadata = metadata
		updatedFields = append(updatedFields, "metadata")
	}

	var statusChange *core_types.StatusChange
	if statusProvided {
		taskStatus := core_types.TaskStatus(statusRaw)
		if taskStatus != existingTask.Status {
			req.Status = &taskStatus
			updatedFields = append(updatedFields, "status")
		}
	}

	// 4. Apply updates.
	if len(updatedFields) > 0 {
		_, err := taskManager.Update(taskId, req)
		if err != nil {
			return bt.ErrorReturn(toolCall, fmt.Errorf("TaskUpdate failed: %w", err))
		}
		if req.Status != nil {
			statusChange = &core_types.StatusChange{
				From: string(existingTask.Status),
				To:   string(*req.Status),
			}
		}
	}

	// 5. Handle addBlocks — this task blocks other tasks (bidirectional).
	if addBlocks, ok := args["addBlocks"].([]any); ok && len(addBlocks) > 0 {
		added := false
		for _, b := range addBlocks {
			blockId, ok := b.(string)
			if !ok {
				continue
			}
			alreadyBlocked := false
			for _, existing := range existingTask.Blocks {
				if existing == blockId {
					alreadyBlocked = true
					break
				}
			}
			if !alreadyBlocked {
				if err := taskManager.Block(taskId, blockId); err != nil {
					return bt.ErrorReturn(toolCall, fmt.Errorf("TaskUpdate failed: %w", err))
				}
				added = true
			}
		}
		if added {
			updatedFields = append(updatedFields, "blocks")
		}
	}

	// 6. Handle addBlockedBy — other tasks block this task (bidirectional).
	if addBlockedBy, ok := args["addBlockedBy"].([]any); ok && len(addBlockedBy) > 0 {
		added := false
		for _, b := range addBlockedBy {
			blockerId, ok := b.(string)
			if !ok {
				continue
			}
			alreadyBlocked := false
			for _, existing := range existingTask.BlockedBy {
				if existing == blockerId {
					alreadyBlocked = true
					break
				}
			}
			if !alreadyBlocked {
				if err := taskManager.Block(blockerId, taskId); err != nil {
					return bt.ErrorReturn(toolCall, fmt.Errorf("TaskUpdate failed: %w", err))
				}
				added = true
			}
		}
		if added {
			updatedFields = append(updatedFields, "blockedBy")
		}
	}

	// 7. Verification nudge: only for main agent (not sub-agent) completing a task.
	verificationNudgeNeeded := false
	if !toolUseContext.AgentUseContext.SubAgent && req.Status != nil && *req.Status == core_types.TaskStatusCompleted {
		allTasks, listErr := taskManager.List()
		if listErr == nil {
			allDone := len(allTasks) > 0
			for _, t := range allTasks {
				if t.Status != core_types.TaskStatusCompleted {
					allDone = false
					break
				}
			}
			if allDone && len(allTasks) >= 3 {
				hasVerif := false
				for _, t := range allTasks {
					if strings.Contains(strings.ToLower(t.Subject), "verif") {
						hasVerif = true
						break
					}
				}
				if !hasVerif {
					verificationNudgeNeeded = true
				}
			}
		}
	}

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TaskUpdateContent{
			Success:                 true,
			TaskID:                  taskId,
			UpdatedFields:           updatedFields,
			StatusChange:            statusChange,
			VerificationNudgeNeeded: verificationNudgeNeeded,
		},
	}, nil
}

func (bt *TaskUpdate) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.TaskUpdateContent)
	if !ok {
		return bt.BuildToolResultByText("", toolContext)
	}

	if !content.Success {
		errMsg := content.Error
		if errMsg == "" {
			errMsg = "Task #" + content.TaskID + " not found"
		}
		return bt.BuildToolResultByText(errMsg, toolContext)
	}

	resultText := "Updated task #" + content.TaskID
	if len(content.UpdatedFields) > 0 {
		resultText += " " + strings.Join(content.UpdatedFields, ", ")
	}

	if content.StatusChange != nil && content.StatusChange.To == "completed" {
		resultText += "\n\nTask completed. Call TaskList now to find your next available task or see if your work unblocked others."
	}

	if content.VerificationNudgeNeeded {
		resultText += "\n\nNOTE: You just closed out 3+ tasks and none of them was a verification step. Before writing your final summary, spawn the verification agent (subagent_type=\"VERIFICATION\"). You cannot self-assign PARTIAL by listing caveats in your summary — only the verifier issues a verdict."
	}

	return bt.BuildToolResultByText(resultText, toolContext)
}
