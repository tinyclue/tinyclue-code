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
	"unicode/utf8"

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
	s, _ := normalizeForFuzzyMatchWithMap(text)
	return s
}

// normalizeForFuzzyMatchWithMap 同 normalizeForFuzzyMatch，同时返回字节偏移映射
// origIndex：origIndex[i] 为归一化后第 i 个字节在原始 text 中的起始字节偏移，
// origIndex[len(normalized)] == len(text)。该映射用于把模糊匹配命中的位置映射回原始
// 文本，从而只替换命中区间、保持文件其余字节原样（避免整文件被有损归一化重写）。
func normalizeForFuzzyMatchWithMap(text string) (normalized string, origIndex []int) {
	// ── 第 1 阶段：NFKC。按段（起始符 + 后续组合符）处理——组合只发生在段内，
	// 逐段归一化与整体归一化结果一致，且能记录每段输出的源偏移。
	nfkc, mapNFKC := nfkcWithMap(text)

	// ── 第 2 阶段：清除每行末尾空白。
	trimmed, mapTrim := trimLineEndsWithMap(nfkc)

	// ── 第 3 阶段：逐字符替换 Unicode 等价物。
	var b strings.Builder
	b.Grow(len(trimmed))
	mapRep := make([]int, 0, len(trimmed)+1)
	for i := 0; i < len(trimmed); {
		r, size := utf8.DecodeRuneInString(trimmed[i:])
		out := string(fuzzyReplaceRune(r))
		for k := 0; k < len(out); k++ {
			b.WriteByte(out[k])
			mapRep = append(mapRep, i)
		}
		i += size
	}
	mapRep = append(mapRep, len(trimmed))

	// ── 合成最终映射：归一化字节 j → 替换后位置 → 裁剪后位置 → nfkc 位置 → 原始 text 位置。
	normalized = b.String()
	origIndex = make([]int, len(normalized)+1)
	for j := 0; j <= len(normalized); j++ {
		origIndex[j] = mapNFKC[mapTrim[mapRep[j]]]
	}
	return normalized, origIndex
}

// nfkcWithMap 对 text 做 NFKC 归一化，返回结果及其到 text 的字节偏移映射 mapNFKC：
// mapNFKC[i] 为归一化后第 i 个字节在 text 中的起始偏移，mapNFKC[len(nfkc)] == len(text)。
// 归一化对段（起始符 + 后续组合符）的输出是常数偏移的阶跃函数，因此每个输出字节记一次
// 源偏移即可，末尾补一个最终边界。
func nfkcWithMap(text string) (nfkc string, mapNFKC []int) {
	var b strings.Builder
	b.Grow(len(text))
	mapNFKC = make([]int, 0, len(text)+1)

	for i := 0; i < len(text); {
		segStart := i
		_, size := utf8.DecodeRuneInString(text[i:])
		i += size
		// 吞掉后续组合标记（ccc != 0）；组合只发生在段内，段边界即组合边界。
		for i < len(text) {
			r2, size2 := utf8.DecodeRuneInString(text[i:])
			if isCombiningStarterBoundary(r2) {
				break
			}
			i += size2
		}
		seg := text[segStart:i]
		nf := norm.NFKC.String(seg)
		for k := 0; k < len(nf); k++ {
			mapNFKC = append(mapNFKC, segStart)
			b.WriteByte(nf[k])
		}
	}
	nfkc = b.String()
	mapNFKC = append(mapNFKC, len(text))
	return nfkc, mapNFKC
}

// trimLineEndsWithMap 去掉每行末尾空白（空格/tab/回车），返回结果及其到 nfkc 的偏移
// 映射 mapTrim：mapTrim[i] 为裁剪后第 i 个字节在 nfkc 中的偏移。
func trimLineEndsWithMap(nfkc string) (string, []int) {
	var b strings.Builder
	b.Grow(len(nfkc))
	mapTrim := make([]int, 0, len(nfkc)+1)

	for start := 0; start <= len(nfkc); {
		nl := strings.IndexByte(nfkc[start:], '\n')
		lineEnd := len(nfkc)
		if nl != -1 {
			lineEnd = start + nl
		}
		line := nfkc[start:lineEnd]
		trimmed := strings.TrimRight(line, " \t\r")
		// TrimRight 只去掉尾部字节，trimmed 是 line 的前缀，前缀字节偏移保持不变。
		for k := 0; k < len(trimmed); k++ {
			b.WriteByte(trimmed[k])
			mapTrim = append(mapTrim, start+k)
		}
		if nl == -1 {
			break
		}
		b.WriteByte('\n')
		mapTrim = append(mapTrim, lineEnd)
		start = lineEnd + 1
	}
	mapTrim = append(mapTrim, len(nfkc))
	return b.String(), mapTrim
}

// isCombiningStarterBoundary 判断 rune 是否为组合边界起始符（ccc == 0），即其不应与
// 前一个起始符发生规范组合。用栈上缓冲避免按 rune 分配字符串。
func isCombiningStarterBoundary(r rune) bool {
	var b [utf8.UTFMax]byte
	n := utf8.EncodeRune(b[:], r)
	return norm.NFKC.Properties(b[:n]).CCC() == 0
}

// fuzzyReplaceRune 返回 rune 的 ASCII 等价替换；无对应替换时返回原值。
func fuzzyReplaceRune(r rune) rune {
	switch {
	case r == '\u2018' || r == '\u2019' || r == '\u201A' || r == '\u201B':
		return '\''
	case r == '\u201C' || r == '\u201D' || r == '\u201E' || r == '\u201F':
		return '"'
	case r == '\u2010' || r == '\u2011' || r == '\u2012' || r == '\u2013' || r == '\u2014' || r == '\u2015' || r == '\u2212':
		return '-'
	case r == '\u00A0':
		return ' '
	case r >= '\u2002' && r <= '\u200A':
		return ' '
	case r == '\u202F' || r == '\u205F' || r == '\u3000':
		return ' '
	default:
		return r
	}
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

	// 4. 若任意一个 edit 需要模糊匹配，构建模糊归一化空间用于定位命中位置，并记录到原始
	//    空间的偏移映射；最终拼装回到原始空间，避免未触碰的区域被整文件模糊重写（NFKC /
	//    行尾去空白 / 引号破折号替换均为有损操作）。
	baseContent := normalizedContent
	var baseMap []int // 模糊空间字节偏移 → normalizedContent 字节偏移（anyFuzzy 时非 nil）
	if anyFuzzy {
		baseContent, baseMap = normalizeForFuzzyMatchWithMap(normalizedContent)
	}

	// 5. 二次匹配 + 唯一性校验（命中位置在 baseContent 所在空间）
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

	// 7. 逆序拼装（防止位置偏移）。命中位置在模糊空间时，先映射回原始空间再替换，
	//    未触碰的区域保持字节原样。
	newContent := normalizedContent
	for i := len(matched) - 1; i >= 0; i-- {
		edit := matched[i]
		start, end := edit.matchIndex, edit.matchIndex+edit.matchLength
		if anyFuzzy {
			start = baseMap[edit.matchIndex]
			end = baseMap[edit.matchIndex+edit.matchLength]
		}
		newContent = newContent[:start] + edit.newText + newContent[end:]
	}

	// 8. 无变更检查
	if normalizedContent == newContent {
		return appliedEditsResult{}, noChangeError(path, len(normalizedEdits))
	}

	return appliedEditsResult{baseContent: normalizedContent, newContent: newContent}, nil
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
