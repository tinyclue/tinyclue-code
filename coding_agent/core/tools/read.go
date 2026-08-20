package tools

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type ReadTool struct {
	*ToolBase
}

func NewReadTool() *ReadTool {
	return &ReadTool{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "read files, images",
		},
	}
}

func (rt *ReadTool) Name() string {
	return core_types.READ_TOOL_NAME
}

//
//func (rt *ReadTool) BeforeToolCall(_ context.Context, toolUseContext core_types.ToolUseContext) (core_types.BeforeToolCallResult, error) {
//	return core_types.BeforeToolCallResult{
//		Action:  core_types.BeforeToolCallAsk,
//		Message: "Confirm read operation on: " + toolUseContext.ToolCall.Arguments["path"].(string),
//	}, nil
//}

func (rt *ReadTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.READ_TOOL_NAME,
		Description: prompt.READ_TOOL_PROMPT,
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"path",
			},
			Properties: map[string]apitypes.SchemaItem{
				"path": {
					Type:        "string",
					Description: "Path to the file to read (relative or absolute)",
				},
				"offset": {
					Type:        "number",
					Description: "Line number to start reading from (1-indexed)",
				},
				"limit": {
					Type:        "number",
					Description: "Maximum number of lines to read",
				},
			},
		},
		Strict: true,
	}
}

const (
	maxOutputLines = 2000
	maxOutputBytes = 50 * 1024 // 50KB
)

func (rt *ReadTool) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	select {
	case <-ctx.Done():
		return rt.ErrorReturn(toolCall, ctx.Err())
	default:
	}
	path, _ := toolCall.Arguments["path"].(string)
	path = filepath.Clean(path)
	if path == "" || path == "." {
		emitProgress(toolUseContext, "Error: path is required")
		return rt.ErrorReturn(toolCall, fmt.Errorf("read: path is required"))
	}

	// 中间状态：开始读取
	//emitProgress(toolUseContext,fmt.Sprintf("reading file: %s", path))

	info, err := os.Stat(path)
	if err != nil {
		errMsg := fmt.Sprintf("Error reading file: %v", err)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read: %w", err))
	}

	if info.IsDir() {
		errMsg := fmt.Sprintf("Error: %s is a directory, not a file", path)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read: %s is a directory", path))
	}

	if utils.IsImageFile(path) {
		return rt.readImage(ctx, toolUseContext, toolCall, path)
	}
	return rt.readText(toolUseContext, toolCall, path, info)
}

func (rt *ReadTool) readImage(ctx context.Context, toolUseContext core_types.ToolUseContext, toolCall apitypes.ToolCall, path string) (core_types.ToolContext, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		errMsg := fmt.Sprintf("Error reading image: %v", err)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read image: %w", err))
	}

	mimeType, err := utils.DetectImageMimeType(path)
	if err != nil {
		errMsg := fmt.Sprintf("Error reading image: %v", err)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read image: %w", err))
	}

	supportsVision := api_provider.GetClient().Adapter().SupportsVision()

	resized, err := utils.ResizeImage(data, mimeType)
	if err != nil {
		errMsg := fmt.Sprintf("Error processing image: %v", err)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read image: %w", err))
	}

	var text string
	var imageData string
	var imageMime string

	if resized == nil {
		// 缩放失败（缩到 1x1 仍超限）
		text = fmt.Sprintf("Read image file [%s]\n[Image could not be resized below the size limit.]", mimeType)
	} else if mimeType == "image/webp" {
		// WebP 不做缩放，直接返回原始数据
		text = fmt.Sprintf("Read image file [%s]\n[WebP images cannot be resized, returning original.]", mimeType)
		imageData = resized.Data
		imageMime = mimeType
	} else {
		text = fmt.Sprintf("Read image file [%s]", resized.MimeType)
		if note := utils.FormatDimensionNote(resized); note != "" {
			text += "\n" + note
		}
		imageData = resized.Data
		imageMime = resized.MimeType
	}

	if !supportsVision {
		text += "\n[This model does not support vision, cannot process image content.]"
	}

	tc := core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.ImageContent{
			Text:     text,
			Data:     imageData,
			MimeType: imageMime,
		},
	}
	emitProgressByContext(toolUseContext, tc)
	return tc, nil
}

func (rt *ReadTool) readText(toolUseContext core_types.ToolUseContext, toolCall apitypes.ToolCall, path string, info os.FileInfo) (core_types.ToolContext, error) {
	offset, limit := parseOffsetLimit(toolCall.Arguments)

	content, err := os.ReadFile(path)
	if err != nil {
		errMsg := fmt.Sprintf("Error reading file: %v", err)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read text: %w", err))
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// 计算实际行号范围（1-indexed → 0-indexed）
	startLine := offset - 1
	if startLine >= totalLines {
		errMsg := fmt.Sprintf("Error: offset %d exceeds file length (%d lines)", offset, totalLines)
		emitProgress(toolUseContext, errMsg)
		return rt.ErrorReturn(toolCall, fmt.Errorf("read: offset %d out of range (file has %d lines)", offset, totalLines))
	}

	endLine := totalLines
	if limit > 0 && startLine+limit < endLine {
		endLine = startLine + limit
	}

	selectedContent := strings.Join(lines[startLine:endLine], "\n")

	// 用户传了 limit 且文件还没读完——与截断无关，提前返回
	if limit > 0 && startLine+limit < totalLines {
		nextOffset := offset + limit
		remaining := totalLines - (startLine + limit)
		outputText := selectedContent + fmt.Sprintf("\n\n[%d more lines in file. Use offset=%d to continue.]", remaining, nextOffset)
		return core_types.ToolContext{
			ToolCall: toolCall,
			Content:  core_types.TextContent{Text: outputText},
		}, nil
	}

	summary := buildReadSummary(path, totalLines, info.Size(), offset, endLine)

	// 截断处理
	truncResult := utils.TruncateHead(selectedContent, maxOutputLines, maxOutputBytes)
	var outputText string
	switch truncResult.Kind {
	case utils.TruncationNone:
		outputText = truncResult.Content

	case utils.TruncationFirstLineExceeds:
		outputText = truncResult.Content +
			fmt.Sprintf("\n\n[Line %d is %s, exceeds %s limit, showing first %s]",
				offset, utils.FormatSize(len(lines[startLine])), utils.FormatSize(maxOutputBytes), utils.FormatSize(truncResult.OutputBytes))

	case utils.TruncationLines:
		endLineDisplay := offset + truncResult.OutputLines - 1
		nextOffset := endLineDisplay + 1
		outputText = truncResult.Content +
			fmt.Sprintf("\n\n[Showing lines %d-%d of %d (line limit). Use offset=%d to continue.]",
				offset, endLineDisplay, totalLines, nextOffset)

	case utils.TruncationBytes:
		endLineDisplay := offset + truncResult.OutputLines - 1
		nextOffset := endLineDisplay + 1
		outputText = truncResult.Content +
			fmt.Sprintf("\n\n[Showing lines %d-%d of %d (%s limit). Use offset=%d to continue.]",
				offset, endLineDisplay, totalLines, utils.FormatSize(maxOutputBytes), nextOffset)
	}
	outputText = summary + "\n" + outputText
	return core_types.ToolContext{
		ToolCall: toolCall,
		Content:  core_types.TextContent{Text: outputText},
	}, nil
}

func (rt *ReadTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	switch v := toolContext.Content.(type) {
	case core_types.ImageContent:
		contents := []apitypes.ContentBlock{}
		if v.Text != "" {
			contents = append(contents, apitypes.ContentBlock{Type: "text", Text: v.Text})
		}
		if v.Data != "" {
			contents = append(contents, apitypes.ContentBlock{Type: "image", ImageData: v.Data, ImageMimeType: v.MimeType})
		}
		return rt.BuildToolResultByBlock(contents, toolContext)
	default:
		text := ""
		if tc, ok := toolContext.Content.(core_types.TextContent); ok {
			text = tc.Text
		}
		return rt.BuildToolResultByText(text, toolContext)
	}
}

// parseOffsetLimit 从 toolCall 参数中解析 offset 和 limit（JSON 数字反序列化为 float64）。
func parseOffsetLimit(args map[string]any) (offset, limit int) {
	offset = 1
	if v, ok := args["offset"]; ok {
		if f, ok := v.(float64); ok {
			offset = int(f)
		}
	}
	if v, ok := args["limit"]; ok {
		if f, ok := v.(float64); ok {
			limit = int(f)
		}
	}
	if offset < 1 {
		offset = 1
	}
	return
}

// buildReadSummary 生成文件读取的摘要行。
func buildReadSummary(path string, totalLines int, fileSize int64, offset, endLine int) string {
	summary := fmt.Sprintf("Read %s (%d lines, %d bytes)", path, totalLines, fileSize)
	if offset > 1 || endLine < totalLines {
		summary += fmt.Sprintf(", Lines %d-%d (%d/%d lines)", offset, endLine, endLine-offset+1, totalLines)
	}
	return summary
}
