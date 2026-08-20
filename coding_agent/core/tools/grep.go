package tools

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tinyclue/tinyclue-code/config"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/ripgrep"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type Grep struct {
	*ToolBase
}

func NewGrep() *Grep {
	return &Grep{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "search file contents with regex (ripgrep)",
		},
	}
}

func (bt *Grep) Name() string {
	return core_types.GREP_TOOL_NAME
}

func (bt *Grep) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.GREP_TOOL_NAME,
		Description: prompt.GetGrepPrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"pattern",
			},
			Properties: map[string]apitypes.SchemaItem{
				"pattern": {
					Type:        "string",
					Description: "The regular expression pattern to search for in file contents",
				},
				"path": {
					Type:        "string",
					Description: "File or directory to search in (rg PATH). Defaults to current working directory.",
				},
				"glob": {
					Type:        "string",
					Description: `Glob pattern to filter files (e.g. "*.js", "*.{ts,tsx}") - maps to rg --glob`,
				},
				"output_mode": {
					Type:        "string",
					Description: `Output mode: "content" shows matching lines (supports -A/-B/-C context, -n line numbers, head_limit), "files_with_matches" shows file paths (supports head_limit), "count" shows match counts (supports head_limit). Defaults to "files_with_matches".`,
					Enum:        []any{"content", "files_with_matches", "count"},
				},
				"-B": {
					Type:        "number",
					Description: "Number of lines to show before each match (rg -B). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-A": {
					Type:        "number",
					Description: "Number of lines to show after each match (rg -A). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-C": {
					Type:        "number",
					Description: "Alias for context.",
				},
				"context": {
					Type:        "number",
					Description: "Number of lines to show before and after each match (rg -C). Requires output_mode: \"content\", ignored otherwise.",
				},
				"-n": {
					Type:        "boolean",
					Description: "Show line numbers in output (rg -n). Requires output_mode: \"content\", ignored otherwise. Defaults to true.",
				},
				"-i": {
					Type:        "boolean",
					Description: "Case insensitive search (rg -i)",
				},
				"type": {
					Type:        "string",
					Description: "File type to search (rg --type). Common types: js, py, rust, go, java, etc. More efficient than include for standard file types.",
				},
				"head_limit": {
					Type:        "number",
					Description: "Limit output to first N lines/entries, equivalent to \"| head -N\". Works across all output modes: content (limits output lines), files_with_matches (limits file paths), count (limits count entries). Defaults to 250 when unspecified. Pass 0 for unlimited (use sparingly — large result sets waste context).",
				},
				"offset": {
					Type:        "number",
					Description: "Skip first N lines/entries before applying head_limit, equivalent to \"| tail -n +N | head -N\". Works across all output modes. Defaults to 0.",
				},
				"multiline": {
					Type:        "boolean",
					Description: "Enable multiline mode where . matches newlines and patterns can span lines (rg -U --multiline-dotall). Default: false.",
				},
			},
		},
		Strict: true,
	}
}

func (bt *Grep) ValidateInput(ctx context.Context, toolUseContext core_types.ToolUseContext) core_types.ValidateContent {
	searchPath, _ := toolUseContext.ToolCall.Arguments["path"].(string)
	if searchPath != "" {
		resolved := expandPath(searchPath)

		// SECURITY: Skip filesystem operations for UNC paths to prevent NTLM credential leaks.
		if strings.HasPrefix(resolved, "\\\\") || strings.HasPrefix(resolved, "//") {
			return core_types.ValidateContent{Result: true}
		}

		_, err := os.Stat(resolved)
		if err != nil {
			if os.IsNotExist(err) {
				return core_types.ValidateContent{
					Result:  false,
					Message: fmt.Sprintf("Path does not exist: %s. The current working directory is: %s.", searchPath, config.CLI.Cwd),
				}
			}
			return core_types.ValidateContent{
				Result:  false,
				Message: fmt.Sprintf("Cannot access path '%s': %v", searchPath, err),
			}
		}
	}
	return core_types.ValidateContent{Result: true}
}

func (bt *Grep) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	args := toolCall.Arguments

	pattern, _ := args["pattern"].(string)
	if pattern == "" {
		return bt.ErrorReturn(toolCall, fmt.Errorf("pattern is required"))
	}

	path, _ := args["path"].(string)
	globPattern, _ := args["glob"].(string)
	typeFilter, _ := args["type"].(string)
	outputMode, _ := args["output_mode"].(string)

	ctxBefore, _ := args["-B"].(float64)
	ctxAfter, _ := args["-A"].(float64)
	ctxC, _ := args["-C"].(float64)
	contextLines, _ := args["context"].(float64)

	showLineNumbers := true
	if v, ok := args["-n"].(bool); ok {
		showLineNumbers = v
	}
	caseInsensitive, _ := args["-i"].(bool)
	multiline, _ := args["multiline"].(bool)

	headLimit := headLimitDefault
	if v, ok := args["head_limit"].(float64); ok {
		headLimit = int(v)
	}

	offset := 0
	if v, ok := args["offset"].(float64); ok {
		offset = int(v)
	}

	if outputMode == "" {
		outputMode = "files_with_matches"
	}

	// 搜索路径
	var searchDir string
	if path != "" {
		searchDir = expandPath(path)
	} else {
		searchDir = config.CLI.Cwd
	}

	// 开始搜索
	emitProgress(toolUseContext, fmt.Sprintf("searching for: %s", pattern))

	// ── 构建 rg 参数 ──
	rgArgs := buildGrepArgs(pattern, outputMode, showLineNumbers, caseInsensitive, multiline,
		ctxBefore, ctxAfter, ctxC, contextLines, typeFilter, globPattern)

	result, err := ripgrep.RipGrep(ctx, rgArgs, searchDir)
	if err != nil {
		errMsg := fmt.Sprintf("Error: %v", err)
		emitProgress(toolUseContext, errMsg)
		return bt.ErrorReturn(toolCall, err)
	}

	var tc core_types.ToolContext

	switch outputMode {
	case "content":
		limited := applyHeadLimit(result.Lines, headLimit, offset)
		relLines := make([]string, len(limited.Items))
		for i, line := range limited.Items {
			if idx := strings.Index(line, ":"); idx > 0 {
				relLines[i] = toRelativePath(line[:idx]) + line[idx:]
			} else {
				relLines[i] = line
			}
		}
		content := strings.Join(relLines, "\n")
		if content == "" {
			content = "No matches found"
		}
		tc = core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.GrepContent{
				Mode:          "content",
				Content:       content,
				NumLines:      len(limited.Items),
				AppliedLimit:  limited.AppliedLimit,
				AppliedOffset: offset,
				DurationMs:    result.DurationMs,
			},
		}

	case "count":
		limited := applyHeadLimit(result.Lines, headLimit, offset)
		var totalMatches, fileCount int
		var relLines []string
		for _, line := range limited.Items {
			if idx := strings.LastIndex(line, ":"); idx > 0 {
				if count, err := strconv.Atoi(line[idx+1:]); err == nil {
					totalMatches += count
					fileCount++
				}
				relLines = append(relLines, toRelativePath(line[:idx])+line[idx:])
			} else {
				relLines = append(relLines, line)
			}
		}
		content := strings.Join(relLines, "\n")
		if content == "" {
			content = "No matches found"
		}
		tc = core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.GrepContent{
				Mode:          "count",
				Content:       content,
				NumFiles:      fileCount,
				NumMatches:    totalMatches,
				AppliedLimit:  limited.AppliedLimit,
				AppliedOffset: offset,
				DurationMs:    result.DurationMs,
			},
		}

	default: // files_with_matches
		sorted := sortByMtime(result.Lines)
		limited := applyHeadLimit(sorted, headLimit, offset)
		relFiles := make([]string, len(limited.Items))
		for i, f := range limited.Items {
			relFiles[i] = toRelativePath(f)
		}
		tc = core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.GrepContent{
				Mode:          "files_with_matches",
				Filenames:     relFiles,
				NumFiles:      len(relFiles),
				AppliedLimit:  limited.AppliedLimit,
				AppliedOffset: offset,
				DurationMs:    result.DurationMs,
			},
		}
	}

	emitProgressByContext(toolUseContext, tc)
	return tc, nil
}

// ──────────────────────────── 辅助函数 ────────────────────────────

const headLimitDefault = 250

type headLimitResult struct {
	Items        []string
	AppliedLimit int // 0 = 未发生截断（未达上限）
}

// VCS 目录列表，与引擎内部一致。
var vcsDirs = []string{
	".git", ".svn", ".hg", ".bzr", ".jj", ".sl",
}

// applyHeadLimit 对 items 应用 head_limit 和 offset。
// headLimit == 0 表示无限制（不截断），headLimit < 0 使用默认值 250。
func applyHeadLimit(items []string, headLimit, offset int) headLimitResult {
	if headLimit == 0 {
		// 0 = unlimited escape hatch
		if offset >= len(items) {
			return headLimitResult{Items: []string{}, AppliedLimit: 0}
		}
		return headLimitResult{Items: items[offset:], AppliedLimit: 0}
	}

	effectiveLimit := headLimit
	if effectiveLimit < 0 {
		effectiveLimit = headLimitDefault
	}

	start := offset
	if start > len(items) {
		start = len(items)
	}
	end := start + effectiveLimit
	if end > len(items) {
		end = len(items)
	}

	sliced := items[start:end]
	wasTruncated := len(items)-start > effectiveLimit
	appliedLimit := 0
	if wasTruncated {
		appliedLimit = effectiveLimit
	}
	return headLimitResult{Items: sliced, AppliedLimit: appliedLimit}
}

// sortByMtime 按修改时间降序排序文件列表（最新在前），
// 修改时间相同时按文件名升序，stat 失败的文件排在最后。
func sortByMtime(files []string) []string {
	type entry struct {
		path  string
		mtime time.Time
	}
	entries := make([]entry, len(files))
	for i, f := range files {
		if info, err := os.Stat(f); err == nil {
			entries[i] = entry{path: f, mtime: info.ModTime()}
		} else {
			entries[i] = entry{path: f}
		}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		// 两者都有 mtime → 按时间降序
		if !entries[i].mtime.IsZero() && !entries[j].mtime.IsZero() {
			if !entries[i].mtime.Equal(entries[j].mtime) {
				return entries[i].mtime.After(entries[j].mtime)
			}
			return entries[i].path < entries[j].path
		}
		// 一个有 mtime → 排前面
		if !entries[i].mtime.IsZero() {
			return true
		}
		if !entries[j].mtime.IsZero() {
			return false
		}
		// 两者都无 → 按文件名
		return entries[i].path < entries[j].path
	})
	result := make([]string, len(entries))
	for i, e := range entries {
		result[i] = e.path
	}
	return result
}

// buildGrepArgs 构建传给 ripgrep 引擎的参数列表，与 TS GrepTool.call 逻辑一致。
func buildGrepArgs(pattern, outputMode string, showLineNumbers, caseInsensitive, multiline bool,
	ctxBefore, ctxAfter, ctxC, contextLines float64, typeFilter, globPattern string) []string {

	args := []string{"--hidden"}

	// 排除 VCS 目录，防止版本控制元数据污染搜索结果
	for _, dir := range vcsDirs {
		args = append(args, "--glob", "!"+dir)
	}

	// 限制行长度，防止 base64/压缩内容污染输出
	args = append(args, "--max-columns", "500")

	if multiline {
		args = append(args, "-U", "--multiline-dotall")
	}

	if caseInsensitive {
		args = append(args, "-i")
	}

	if outputMode == "files_with_matches" {
		args = append(args, "-l")
	} else if outputMode == "count" {
		args = append(args, "-c")
	}

	if outputMode == "content" {
		if showLineNumbers {
			args = append(args, "-n")
		} else {
			args = append(args, "--no-line-number")
		}
	}

	if outputMode == "content" {
		if contextLines > 0 {
			args = append(args, "-C", fmt.Sprintf("%.0f", contextLines))
		} else if ctxC > 0 {
			args = append(args, "-C", fmt.Sprintf("%.0f", ctxC))
		} else {
			if ctxBefore > 0 {
				args = append(args, "-B", fmt.Sprintf("%.0f", ctxBefore))
			}
			if ctxAfter > 0 {
				args = append(args, "-A", fmt.Sprintf("%.0f", ctxAfter))
			}
		}
	}

	// pattern 以 - 开头时用 -e 避免被解析为 flag
	if strings.HasPrefix(pattern, "-") {
		args = append(args, "-e", pattern)
	} else {
		args = append(args, pattern)
	}

	if typeFilter != "" {
		args = append(args, "--type", typeFilter)
	}

	// glob：按空白分割，花括号内的模式保持完整，否则按逗号分割
	if globPattern != "" {
		rawPatterns := strings.Fields(globPattern)
		for _, raw := range rawPatterns {
			if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
				args = append(args, "--glob", raw)
			} else {
				for _, p := range strings.Split(raw, ",") {
					p = strings.TrimSpace(p)
					if p != "" {
						args = append(args, "--glob", p)
					}
				}
			}
		}
	}

	return args
}

func (bt *Grep) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, ok := toolContext.Content.(core_types.GrepContent)
	if !ok {
		return bt.BuildToolResultByText("", toolContext)
	}

	mode := content.Mode
	if mode == "" {
		mode = "files_with_matches"
	}

	switch mode {
	case "content":
		resultContent := content.Content
		if resultContent == "" {
			resultContent = "No matches found"
		}
		limitInfo := formatGrepLimitInfo(content.AppliedLimit, content.AppliedOffset)
		if limitInfo != "" {
			resultContent = fmt.Sprintf("%s\n\n[Showing results with pagination = %s]", resultContent, limitInfo)
		}
		return bt.BuildToolResultByText(resultContent, toolContext)

	case "count":
		rawContent := content.Content
		if rawContent == "" {
			rawContent = "No matches found"
		}
		summary := fmt.Sprintf("\n\nFound %d total occurrences across %d files.", content.NumMatches, content.NumFiles)
		limitInfo := formatGrepLimitInfo(content.AppliedLimit, content.AppliedOffset)
		if limitInfo != "" {
			summary += fmt.Sprintf(" with pagination = %s", limitInfo)
		}
		return bt.BuildToolResultByText(rawContent+summary, toolContext)

	default: // files_with_matches
		limitInfo := formatGrepLimitInfo(content.AppliedLimit, content.AppliedOffset)
		if content.NumFiles == 0 {
			return bt.BuildToolResultByText("No files found", toolContext)
		}
		result := fmt.Sprintf("Found %d files", content.NumFiles)
		if limitInfo != "" {
			result += " " + limitInfo
		}
		result += "\n" + strings.Join(content.Filenames, "\n")
		return bt.BuildToolResultByText(result, toolContext)
	}
}

func formatGrepLimitInfo(appliedLimit, appliedOffset int) string {
	var parts []string
	if appliedLimit > 0 {
		parts = append(parts, fmt.Sprintf("limit: %d", appliedLimit))
	}
	if appliedOffset > 0 {
		parts = append(parts, fmt.Sprintf("offset: %d", appliedOffset))
	}
	return strings.Join(parts, ", ")
}
