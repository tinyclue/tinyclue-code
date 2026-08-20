package tools

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
)

type EditTool struct {
	*ToolBase
}

func NewEditTool() *EditTool {
	return &EditTool{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "modify file contents in place",
		},
	}
}

func (et *EditTool) Name() string {
	return core_types.EDIT_TOOL_NAME
}

//
//func (et *EditTool) BeforeToolCall(_ context.Context, toolUseContext core_types.ToolUseContext) (core_types.BeforeToolCallResult, error) {
//	return core_types.BeforeToolCallResult{
//		Action:  core_types.BeforeToolCallAsk,
//		Message: "Confirm edit operation on: " + toolUseContext.ToolCall.Arguments["path"].(string),
//	}, nil
//}

func (et *EditTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        core_types.EDIT_TOOL_NAME,
		Description: prompt.EDIT_TOOL_PROMPT,
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"path",
				"edits",
			},
			Properties: map[string]apitypes.SchemaItem{
				"path": {
					Type:        "string",
					Description: "Path to the file to edit (relative or absolute)",
				},
				"edits": {
					Type:        "array",
					Description: "One or more targeted replacements. Each edit is matched against the original file, not incrementally. Do not include overlapping or nested edits. If two changes touch the same block or nearby lines, merge them into one edit instead.",
				},
			},
		},
		Strict: true,
	}
}

// editOp 表示一次替换操作。
type editOp struct {
	OldText string
	NewText string
}

// fuzzyMatchResult 表示模糊匹配的结果。
type fuzzyMatchResult struct {
	found                 bool
	index                 int
	matchLength           int
	usedFuzzyMatch        bool
	contentForReplacement string
}

// matchedEdit 表示已定位到原始内容中的一次替换，用于最终拼装。
type matchedEdit struct {
	editIndex   int
	matchIndex  int
	matchLength int
	newText     string
}

// appliedEditsResult 包含编辑后的内容以及编辑前的基线内容（用于生成 diff）。
type appliedEditsResult struct {
	baseContent string
	newContent  string
}

// ─────────────────────────────────────────────
// BOM / 行尾 / 模糊归一化 工具函数
// ─────────────────────────────────────────────

// stripBom 去除 UTF-8 BOM，返回 BOM 本身和剩余文本。
func stripBom(content string) (bom string, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[3:]
	}
	return "", content
}

// detectLineEnding 检测文本主要的换行符风格。
func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 {
		return "\n"
	}
	if crlfIdx == -1 {
		return "\n"
	}
	if crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

// normalizeToLF 将所有换行符统一为 \n。
func normalizeToLF(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return text
}

// restoreLineEndings 将 \n 替换回检测到的换行符风格。
func restoreLineEndings(text string, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

// normalizeForFuzzyMatch 对文本做渐进式归一化以便模糊匹配：
//   - NFKC 归一化
//   - 清除每行末尾空白
//   - Unicode 引号 → ASCII 引号
//   - Unicode 破折号 → ASCII 连字符
//   - 特殊空格 → 普通空格
func normalizeForFuzzyMatch(text string) string {
	// NFKC
	text = norm.NFKC.String(text)

	// 行尾去空白
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}
	text = strings.Join(lines, "\n")

	// 逐字符替换 Unicode 等价物
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r == '\u2018' || r == '\u2019' || r == '\u201A' || r == '\u201B':
			b.WriteRune('\'')
		case r == '\u201C' || r == '\u201D' || r == '\u201E' || r == '\u201F':
			b.WriteRune('"')
		case r == '\u2010' || r == '\u2011' || r == '\u2012' || r == '\u2013' || r == '\u2014' || r == '\u2015' || r == '\u2212':
			b.WriteRune('-')
		case r == '\u00A0':
			b.WriteRune(' ')
		case r >= '\u2002' && r <= '\u200A':
			b.WriteRune(' ')
		case r == '\u202F' || r == '\u205F' || r == '\u3000':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ─────────────────────────────────────────────
// 匹配 / 计数
// ─────────────────────────────────────────────

// fuzzyFindText 在 content 中查找 oldText。先尝试精确匹配，失败后进入模糊匹配。
func fuzzyFindText(content, oldText string) fuzzyMatchResult {
	// 精确匹配
	if idx := strings.Index(content, oldText); idx != -1 {
		return fuzzyMatchResult{
			found:                 true,
			index:                 idx,
			matchLength:           len(oldText),
			usedFuzzyMatch:        false,
			contentForReplacement: content,
		}
	}

	// 模糊匹配
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	if idx := strings.Index(fuzzyContent, fuzzyOldText); idx != -1 {
		return fuzzyMatchResult{
			found:                 true,
			index:                 idx,
			matchLength:           len(fuzzyOldText),
			usedFuzzyMatch:        true,
			contentForReplacement: fuzzyContent,
		}
	}

	return fuzzyMatchResult{
		found:                 false,
		index:                 -1,
		matchLength:           0,
		usedFuzzyMatch:        false,
		contentForReplacement: content,
	}
}

// countOccurrences 统计 oldText 在 content 中模糊匹配的出现次数。
func countOccurrences(content, oldText string) int {
	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	return strings.Count(fuzzyContent, fuzzyOldText)
}

// ─────────────────────────────────────────────
// 错误消息辅助函数（与 TS 版本保持一致）
// ─────────────────────────────────────────────

func notFoundError(path string, editIndex int, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Could not find the exact text in %s. The old text must match exactly including all whitespace and newlines.", path)
	}
	return fmt.Errorf("Could not find edits[%d] in %s. The oldText must match exactly including all whitespace and newlines.", editIndex, path)
}

func duplicateError(path string, editIndex int, totalEdits int, occurrences int) error {
	if totalEdits == 1 {
		return fmt.Errorf("Found %d occurrences of the text in %s. The text must be unique. Please provide more context to make it unique.", occurrences, path)
	}
	return fmt.Errorf("Found %d occurrences of edits[%d] in %s. Each oldText must be unique. Please provide more context to make it unique.", occurrences, editIndex, path)
}

func emptyOldTextError(path string, editIndex int, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("oldText must not be empty in %s.", path)
	}
	return fmt.Errorf("edits[%d].oldText must not be empty in %s.", editIndex, path)
}

func noChangeError(path string, totalEdits int) error {
	if totalEdits == 1 {
		return fmt.Errorf("No changes made to %s. The replacement produced identical content. This might indicate an issue with special characters or the text not existing as expected.", path)
	}
	return fmt.Errorf("No changes made to %s. The replacements produced identical content.", path)
}

// ─────────────────────────────────────────────
// 核心：在已归一化（LF）的内容上应用编辑
// ─────────────────────────────────────────────

// applyEditsToNormalizedContent 在 LF 归一化的内容上执行非增量替换。
// 所有 edits[].oldText 均匹配原始内容（而不是增量累加）。
// 返回编辑前的基线内容和编辑后的新内容。
func applyEditsToNormalizedContent(normalizedContent string, edits []editOp, path string) (appliedEditsResult, error) {
	// 1. 对 edits 也做 CRLF→LF 归一化
	normalizedEdits := make([]editOp, len(edits))
	for i, e := range edits {
		normalizedEdits[i] = editOp{
			OldText: normalizeToLF(e.OldText),
			NewText: normalizeToLF(e.NewText),
		}
	}

	// 2. 校验 empty oldText
	for i, e := range normalizedEdits {
		if e.OldText == "" {
			return appliedEditsResult{}, emptyOldTextError(path, i, len(normalizedEdits))
		}
	}

	// 3. 初次匹配，判断是否需要模糊匹配
	initialMatches := make([]fuzzyMatchResult, len(normalizedEdits))
	anyFuzzy := false
	for i, e := range normalizedEdits {
		initialMatches[i] = fuzzyFindText(normalizedContent, e.OldText)
		if initialMatches[i].usedFuzzyMatch {
			anyFuzzy = true
		}
	}

	// 4. 如果任意一个 edit 需要模糊匹配，整个 baseContent 切换到模糊归一化空间
	baseContent := normalizedContent
	if anyFuzzy {
		baseContent = normalizeForFuzzyMatch(normalizedContent)
	}

	// 5. 二次匹配 + 唯一性校验
	matched := make([]matchedEdit, 0, len(normalizedEdits))
	for i, e := range normalizedEdits {
		mr := fuzzyFindText(baseContent, e.OldText)
		if !mr.found {
			return appliedEditsResult{}, notFoundError(path, i, len(normalizedEdits))
		}

		occ := countOccurrences(baseContent, e.OldText)
		if occ > 1 {
			return appliedEditsResult{}, duplicateError(path, i, len(normalizedEdits), occ)
		}

		matched = append(matched, matchedEdit{
			editIndex:   i,
			matchIndex:  mr.index,
			matchLength: mr.matchLength,
			newText:     e.NewText,
		})
	}

	// 6. 按位置升序排序，检查重叠
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].matchIndex < matched[j].matchIndex
	})
	for i := 1; i < len(matched); i++ {
		prev := matched[i-1]
		cur := matched[i]
		if prev.matchIndex+prev.matchLength > cur.matchIndex {
			return appliedEditsResult{}, fmt.Errorf(
				"edits[%d] and edits[%d] overlap in %s. Merge them into one edit or target disjoint regions.",
				prev.editIndex, cur.editIndex, path,
			)
		}
	}

	// 7. 逆序拼装（防止位置偏移）
	newContent := baseContent
	for i := len(matched) - 1; i >= 0; i-- {
		edit := matched[i]
		newContent = newContent[:edit.matchIndex] + edit.newText + newContent[edit.matchIndex+edit.matchLength:]
	}

	// 8. 无变更检查
	if baseContent == newContent {
		return appliedEditsResult{}, noChangeError(path, len(normalizedEdits))
	}

	return appliedEditsResult{baseContent: baseContent, newContent: newContent}, nil
}

// ─────────────────────────────────────────────
// parseEdits — 纯转换，不校验内容
// ─────────────────────────────────────────────

func parseEdits(raw []any) ([]editOp, error) {
	edits := make([]editOp, 0, len(raw))
	for i, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("edits[%d] is not an object", i)
		}
		oldText, _ := m["oldText"].(string)
		newText, _ := m["newText"].(string)
		edits = append(edits, editOp{OldText: oldText, NewText: newText})
	}
	return edits, nil
}

// ─────────────────────────────────────────────
// summary / diff 格式化
// ─────────────────────────────────────────────

func formatSummary(path, baseContent, newContent string, editCount int) string {
	origLines := strings.Split(baseContent, "\n")
	resultLines := strings.Split(newContent, "\n")
	lineChanges := utils.CountLineChanges(origLines, resultLines)

	return fmt.Sprintf("Successfully Edited %s: Added %d lines, removed %d lines, %d edit(s) applied",
		path, lineChanges.Additions, lineChanges.Deletions, editCount)
}

// ─────────────────────────────────────────────
// Execute — 主入口
// ─────────────────────────────────────────────

func (et *EditTool) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	select {
	case <-ctx.Done():
		return et.ErrorReturn(toolCall, ctx.Err())
	default:
	}
	// 解析 path
	path, _ := toolCall.Arguments["path"].(string)
	path = filepath.Clean(path)
	if path == "" || path == "." {
		emitProgress(toolUseContext, "Error: path is required")
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: path is required"))
	}

	// 解析 edits
	editsRaw, ok := toolCall.Arguments["edits"].([]any)
	if !ok || len(editsRaw) == 0 {
		emitProgress(toolUseContext, "Error: edits array is required and must not be empty")
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: edits array is required"))
	}

	edits, err := parseEdits(editsRaw)
	if err != nil {
		emitProgress(toolUseContext, fmt.Sprintf("Error: invalid edits: %v", err))
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: invalid edits: %w", err))
	}

	// 读取文件
	content, err := os.ReadFile(path)
	if err != nil {
		errMsg := fmt.Sprintf("Error reading file: %v", err)
		emitProgress(toolUseContext, errMsg)
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: reading file: %w", err))
	}
	rawContent := string(content)

	// BOM 剥离
	bom, contentNoBom := stripBom(rawContent)

	// 行尾检测与归一化
	originalEnding := detectLineEnding(contentNoBom)
	normalizedContent := normalizeToLF(contentNoBom)

	// 执行编辑（核心逻辑）
	result, err := applyEditsToNormalizedContent(normalizedContent, edits, path)
	if err != nil {
		emitProgress(toolUseContext, fmt.Sprintf("Error: %v", err))
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: %w", err))
	}

	// 恢复换行风格 + 重新挂上 BOM
	finalContent := bom + restoreLineEndings(result.newContent, originalEnding)

	// 写回
	if err := os.WriteFile(path, []byte(finalContent), 0644); err != nil {
		errMsg := fmt.Sprintf("Error writing file: %v", err)
		emitProgress(toolUseContext, errMsg)
		return et.ErrorReturn(toolCall, fmt.Errorf("Error: writing file: %w", err))
	}

	// 生成最终结果
	// 摘要
	summary := formatSummary(path, result.baseContent, result.newContent, len(edits))
	numberedDiff, unifiedDiff := utils.ComputeDiff(result.baseContent, result.newContent, path)
	tc := core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.TextContent{
			Text: summary,
			Details: map[string]any{
				"diff":         numberedDiff,
				"unified_diff": unifiedDiff,
			},
		},
	}
	return tc, nil
}

func (et *EditTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	text := ""
	if tc, ok := toolContext.Content.(core_types.TextContent); ok {
		text = tc.Text
	}
	return et.BuildToolResultByText(text, toolContext)
}
