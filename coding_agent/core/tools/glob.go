package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tinyclue/tinyclue-code/config"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/ripgrep"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type Glob struct {
	*ToolBase
}

func NewGlob() *Glob {
	return &Glob{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "find files by name pattern or wildcard",
		},
	}
}

func (bt *Glob) Name() string {
	return core_types.GLOB_TOOL_NAME
}

func (bt *Glob) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_types.GLOB_TOOL_NAME,
		Description: prompt.GLOB_PROMPT,
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"pattern",
			},
			Properties: map[string]apitypes.SchemaItem{
				"pattern": {
					Type:        "string",
					Description: "The glob pattern to match files against",
				},
				"path": {
					Type:        "string",
					Description: "The directory to search in. If not specified, the current working directory will be used.",
				},
			},
		},
		Strict: true,
	}
	return result
}

func (bt *Glob) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	searchPath, _ := toolUseContext.ToolCall.Arguments["path"].(string)
	if searchPath != "" {
		resolved := expandPath(searchPath)
		info, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				return core_types.ValidateContent{
					Result:  false,
					Message: fmt.Sprintf("Directory does not exist: %s.", searchPath),
				}
			}
			return core_types.ValidateContent{
				Result:  false,
				Message: fmt.Sprintf("Cannot access path '%s': %v", searchPath, err),
			}
		}
		if !info.IsDir() {
			return core_types.ValidateContent{
				Result:  false,
				Message: fmt.Sprintf("Path is not a directory: %s", searchPath),
			}
		}
	}
	return core_types.ValidateContent{Result: true}
}

func (bt *Glob) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	args := toolCall.Arguments

	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return bt.ErrorReturn(toolCall, fmt.Errorf("pattern is required"))
	}

	path, _ := args["path"].(string)

	// 确定搜索目录（同 TS GlobTool.getPath）
	var searchDir string
	if path != "" {
		searchDir = expandPath(path)
	} else {
		searchDir = config.CLI.Cwd
	}

	// 处理绝对路径的 pattern：提取 baseDir 和 relativePattern
	// ripgrep 的 --glob 只接受相对路径 pattern
	searchPattern := pattern
	if filepath.IsAbs(pattern) {
		baseDir, relPattern := extractGlobBaseDir(pattern)
		if baseDir != "" {
			searchDir = baseDir
		}
		searchPattern = relPattern
	}

	emitProgress(toolUseContext, fmt.Sprintf("searching for: %s", searchPattern))

	// 构建 rg 参数，同 TS glob.ts
	// --files: 列出文件而非搜索内容
	// --glob: 按 pattern 过滤
	// --sort modified: 按修改时间排序（升序，最旧在前）
	// --no-ignore: 不遵守 .gitignore（纯 Go 引擎自动忽略）
	// --hidden: 包含隐藏文件
	rgArgs := []string{
		"--files",
		"--glob", searchPattern,
		"--sort", "modified",
		"--no-ignore",
		"--hidden",
	}

	result, err := ripgrep.RipGrep(ctx, rgArgs, searchDir)
	if err != nil {
		errMsg := fmt.Sprintf("Error: %v", err)
		emitProgress(toolUseContext, errMsg)
		return bt.ErrorReturn(toolCall, err)
	}

	// 引擎返回绝对路径，转为相对路径以节省 token（同 GrepTool）
	var filenames []string
	for _, line := range result.Lines {
		filenames = append(filenames, toRelativePath(line))
	}

	// 限制结果数（同 TS globLimits.maxResults = 100）
	const maxResults = 100
	truncated := len(filenames) > maxResults
	if truncated {
		filenames = filenames[:maxResults]
	}

	tc := core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.GlobContent{
			Filenames:  filenames,
			NumFiles:   len(filenames),
			Truncated:  truncated,
			DurationMs: result.DurationMs,
		},
	}
	emitProgressByContext(toolUseContext, tc)
	return tc, nil
}

func (bt *Glob) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, _ := toolContext.Content.(core_types.GlobContent)

	result := apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		IsError:    false,
	}

	if content.NumFiles == 0 {
		result.Contents = []apitypes.ContentBlock{{Type: "text", Text: "No files found"}}
	} else {
		text := strings.Join(content.Filenames, "\n")
		if content.Truncated {
			text += "\n(Results are truncated. Consider using a more specific path or pattern.)"
		}
		result.Contents = []apitypes.ContentBlock{{Type: "text", Text: text}}
	}
	return result
}

// extractGlobBaseDir 从 glob pattern 中提取静态基础目录。
// 与 TS extractGlobBaseDirectory 一致。
// 例如：/Users/me/projects/*.go → baseDir=/Users/me/projects, relativePattern=*.go
func extractGlobBaseDir(pattern string) (baseDir, relativePattern string) {
	// 查找第一个 glob 特殊字符：* ? [ {
	globIdx := -1
	for i, c := range pattern {
		if c == '*' || c == '?' || c == '[' || c == '{' {
			globIdx = i
			break
		}
	}

	if globIdx == -1 {
		// 无 glob 字符 — 字面路径，取目录部分
		dir := filepath.Dir(pattern)
		file := filepath.Base(pattern)
		return dir, file
	}

	// 取 glob 字符前的静态前缀
	staticPrefix := pattern[:globIdx]

	// 找静态前缀中最后一个路径分隔符
	lastSep := strings.LastIndexAny(staticPrefix, "/\\")

	if lastSep == -1 {
		// 无路径分隔符 — pattern 是相对于 cwd 的
		return "", pattern
	}

	baseDir = pattern[:lastSep]
	relativePattern = pattern[lastSep+1:]

	// 处理根目录 pattern，如 /*.txt
	if baseDir == "" && lastSep == 0 {
		baseDir = "/"
	}

	return baseDir, relativePattern
}
