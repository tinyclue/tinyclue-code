package tools

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type WriteTool struct {
	*ToolBase
}

func NewWriteTool() *WriteTool {
	return &WriteTool{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "create or overwrite files",
		},
	}
}

func (wt *WriteTool) Name() string {
	return core_types.WRITE_TOOL_NAME
}

//
//func (wt *WriteTool) BeforeToolCall(_ context.Context, toolUseContext core_types.ToolUseContext) (core_types.BeforeToolCallResult, error) {
//	return core_types.BeforeToolCallResult{
//		Action:  core_types.BeforeToolCallAsk,
//		Message: "Confirm write operation on: " + toolUseContext.ToolCall.Arguments["path"].(string),
//	}, nil
//}

func (wt *WriteTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.WRITE_TOOL_NAME,
		Description: prompt.WRITE_TOOL_PROMPT,
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"path",
				"content",
			},
			Properties: map[string]apitypes.SchemaItem{
				"path": {
					Type:        "string",
					Description: "Path to the file to write (relative or absolute)",
				},
				"content": {
					Type:        "string",
					Description: "Content to write to the file",
				},
			},
		},
		Strict: true,
	}
}

func (wt *WriteTool) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	select {
	case <-ctx.Done():
		return wt.ErrorReturn(toolCall, ctx.Err())
	default:
	}
	path, _ := toolCall.Arguments["path"].(string)
	path = filepath.Clean(path)
	if path == "" || path == "." {
		emitProgress(toolUseContext, "Error: path is required")
		return wt.ErrorReturn(toolCall, fmt.Errorf("write: path is required"))
	}

	content, _ := toolCall.Arguments["content"].(string)

	// 读取原文件内容（文件已存在时），用于后续 diff
	var original string
	if origBytes, err := os.ReadFile(path); err == nil {
		original = string(origBytes)
	}

	// 自动创建父目录
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			errMsg := fmt.Sprintf("Error creating parent directories: %v", err)
			emitProgress(toolUseContext, errMsg)
			return wt.ErrorReturn(toolCall, fmt.Errorf("write: %w", err))
		}
	}

	// 写入文件
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		errMsg := fmt.Sprintf("Error writing file: %v", err)
		emitProgress(toolUseContext, errMsg)
		return wt.ErrorReturn(toolCall, fmt.Errorf("write: %w", err))
	}

	lineCount := strings.Count(content, "\n") + 1
	text := fmt.Sprintf("Successfully wrote (%d lines, %d bytes) to %s", lineCount, len(content), path)

	tc := core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TextContent{
			Text: text,
		},
	}

	// 计算 diff（原文件存在时）
	if original != "" {
		if numberedDiff, unifiedDiff := utils.ComputeDiff(original, content, path); numberedDiff != "" {
			tc.Content = core_types.TextContent{
				Text: text,
				Details: map[string]any{
					"diff":         numberedDiff,
					"unified_diff": unifiedDiff,
				},
			}
		}
	}

	return tc, nil
}

func (wt *WriteTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	text := ""
	if tc, ok := toolContext.Content.(core_types.TextContent); ok {
		text = tc.Text
	}
	return wt.BuildToolResultByText(text, toolContext)
}
