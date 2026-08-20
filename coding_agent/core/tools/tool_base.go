package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"time"
)

type ToolBase struct {
	deferred   bool
	searchHint string
}

func NewToolBase() *ToolBase {
	return &ToolBase{}
}

func (tb *ToolBase) ErrorReturn(toolCall apitypes.ToolCall, err error) (types.ToolContext, error) {
	toolContext := types.ToolContext{
		ToolCall: toolCall,
		Content:  types.TextContent{Text: fmt.Sprintf("%v", err)},
	}
	return toolContext, err
}

func (tb *ToolBase) BuildToolResultByText(text string, toolContext types.ToolContext) apitypes.ToolResultMessage {
	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		IsError:    false,
		CreatedAt:  time.Now(),
		Contents:   []apitypes.ContentBlock{{Type: "text", Text: text}},
	}
}

func (tb *ToolBase) BuildToolResultByBlock(contents []apitypes.ContentBlock, toolContext types.ToolContext) apitypes.ToolResultMessage {
	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		IsError:    false,
		CreatedAt:  time.Now(),
		Contents:   contents,
	}
}

func (tb *ToolBase) IsDeferredTool() bool {
	return tb.deferred
}

// emitProgress 通过 ToolUseContext 发送进度回调（流式增量文本），工具内部代替 bt.TriggerCallbacks 使用。
func emitProgress(ctx types.ToolUseContext, delta string) {
	if ctx.OnProgress != nil {
		ctx.OnProgress(types.ToolContext{
			ToolCall: ctx.ToolCall,
			Content:  types.TextContent{Delta: delta},
		})
	}
}

// emitProgressByContext 通过 ToolUseContext 发送已构建的 ToolContext 进度回调。
func emitProgressByContext(ctx types.ToolUseContext, tc types.ToolContext) {
	if ctx.OnProgress != nil {
		ctx.OnProgress(tc)
	}
}

func (tb *ToolBase) GetSearchHint() string {
	return tb.searchHint
}

func (tb *ToolBase) BeforeToolCall(_ context.Context, _ types.ToolUseContext) types.BeforeToolCallResult {
	return types.BeforeToolCallResult{
		Action: types.BeforeToolCallAllow,
	}
}

func (tb *ToolBase) ValidateInput(ctx context.Context, toolUseContext types.ToolUseContext) types.ValidateContent {
	return types.ValidateContent{
		Result:  true,
		Message: "",
	}
}
