package tools

import (
	"context"
	"fmt"
	"strings"
	"time"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// AskUserQuestionTool 是一个让 agent 向用户提问的多选题工具。
// 每次可提 1-4 个问题，每个问题 2-4 个选项。
type AskUserQuestionTool struct {
	*ToolBase
}

func NewAskUserQuestionTool() *AskUserQuestionTool {
	return &AskUserQuestionTool{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "ask the user a multiple-choice question",
		},
	}
}

func (t *AskUserQuestionTool) Name() string {
	return core_types.ASK_USER_QUESTION_TOOL_NAME
}

func (t *AskUserQuestionTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.ASK_USER_QUESTION_TOOL_NAME,
		Description: prompt.GetAskUserQuestionPrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Properties: map[string]apitypes.SchemaItem{
				"questions": {
					Type:        "array",
					Description: "Questions to ask the user (1-4 questions)",
					Items: &apitypes.SchemaItem{
						Type: "object",
						Properties: map[string]apitypes.SchemaItem{
							"question": {
								Type:        "string",
								Description: `The complete question to ask the user. Should be clear, specific, and end with a question mark. Example: "Which library should we use for date formatting?" If multiSelect is true, phrase it accordingly, e.g. "Which features do you want to enable?"`,
							},
							"header": {
								Type:        "string",
								Description: `Very short label displayed as a chip/tag (max 12 chars). Examples: "Auth method", "Library", "Approach".`,
							},
							"options": {
								Type:        "array",
								Description: `The available choices for this question. Must have 2-4 options. Each option should be a distinct, mutually exclusive choice (unless multiSelect is enabled). There should be no 'Other' option, that will be provided automatically.`,
								Items: &apitypes.SchemaItem{
									Type: "object",
									Properties: map[string]apitypes.SchemaItem{
										"label": {
											Type:        "string",
											Description: `The display text for this option that the user will see and select. Should be concise (1-5 words) and clearly describe the choice.`,
										},
										"description": {
											Type:        "string",
											Description: `Explanation of what this option means or what will happen if chosen. Useful for providing context about trade-offs or implications.`,
										},
										//"preview": {
										//	Type:        "string",
										//	Description: `Optional preview content rendered when this option is focused. Use for mockups, code snippets, or visual comparisons that help users compare options. See the tool description for the expected content format.`,
										//},
									},
									Required: []string{"label", "description"},
								},
							},
							"multiSelect": {
								Type:        "boolean",
								Default:     false,
								Description: `Set to true to allow the user to select multiple options instead of just one. Use when choices are not mutually exclusive.`,
							},
						},
						Required: []string{"question", "header", "options"},
					},
				},
				"answers": {
					Type:        "object",
					Description: "User answers collected by the permission component",
				},
				"annotations": {
					Type:        "object",
					Description: `Optional per-question annotations from the user (e.g., notes on preview selections). Keyed by question text.`,
					Properties: map[string]apitypes.SchemaItem{
						//"preview": {
						//	Type:        "string",
						//	Description: `The preview content of the selected option, if the question used previews.`,
						//},
						"notes": {
							Type:        "string",
							Description: `Free-text notes the user added to their selection.`,
						},
					},
				},
				"metadata": {
					Type:        "object",
					Description: "Optional metadata for tracking and analytics purposes. Not displayed to user.",
					Properties: map[string]apitypes.SchemaItem{
						"source": {
							Type:        "string",
							Description: `Optional identifier for the source of this question (e.g., "remember" for /remember command). Used for analytics tracking.`,
						},
					},
				},
			},
			Required: []string{"questions"},
		},
		Strict: true,
	}
}

func (t *AskUserQuestionTool) ValidateInput(_ context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	questions, ok := toolUseContext.ToolCall.Arguments["questions"].([]any)
	if !ok || len(questions) == 0 {
		return core_types.ValidateContent{
			Result:  false,
			Message: "questions is required and must be a non-empty array",
		}
	}
	if len(questions) < 1 || len(questions) > 4 {
		return core_types.ValidateContent{
			Result:  false,
			Message: "questions must have 1-4 items",
		}
	}
	return core_types.ValidateContent{Result: true}
}

func (t *AskUserQuestionTool) BeforeToolCall(_ context.Context, toolUseContext core_types.ToolUseContext) core_types.BeforeToolCallResult {
	return core_types.BeforeToolCallResult{
		Action:  core_types.BeforeToolCallAsk,
		Message: "Answer questions?",
	}
}

func (t *AskUserQuestionTool) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall

	// Matches TS call: { questions, answers = {}, annotations }
	questions, _ := toolCall.Arguments["questions"].([]any)
	answers, _ := toolCall.Arguments["answers"].(map[string]any)
	annotations, _ := toolCall.Arguments["annotations"].(map[string]any)

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.AskUserQuestionContent{
			Questions:   questions,
			Answers:     answers,
			Annotations: annotations,
		},
	}, nil
}

func (t *AskUserQuestionTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.AskUserQuestionContent)
	if !ok {
		return t.BuildToolResultByText("", toolContext)
	}

	// Build natural language result (matches TS mapToolResultToToolResultBlockParam)
	var parts []string
	for qText, answer := range content.Answers {
		answerStr, _ := answer.(string)
		p := fmt.Sprintf(`"%s"="%s"`, qText, answerStr)
		if annRaw, ok := content.Annotations[qText]; ok {
			if ann, ok := annRaw.(map[string]any); ok {
				//if preview, ok := ann["preview"].(string); ok && preview != "" {
				//	p += fmt.Sprintf(" selected preview:\n%s", preview)
				//}
				if notes, ok := ann["notes"].(string); ok && notes != "" {
					p += fmt.Sprintf(" user notes: %s", notes)
				}
			}
		}
		parts = append(parts, p)
	}

	contentStr := fmt.Sprintf("User has answered your questions: %s. You can now continue with the user's answers in mind.", strings.Join(parts, ", "))

	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		IsError:    false,
		CreatedAt:  time.Now(),
		Contents:   []apitypes.ContentBlock{{Type: "text", Text: contentStr}},
	}
}
