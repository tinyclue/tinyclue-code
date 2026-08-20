package tools

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type ToolSearch struct {
	*ToolBase
}

func NewToolSearch() *ToolSearch {
	return &ToolSearch{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "",
		},
	}
}

func (bt *ToolSearch) Name() string {
	return core_types.TOOL_SEARCH_TOOL_NAME
}

func (bt *ToolSearch) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_types.TOOL_SEARCH_TOOL_NAME,
		Description: prompt.TOOL_SEARCH_PROMPT,
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"query",
			},
			Properties: map[string]apitypes.SchemaItem{
				"query": {
					Type:        "string",
					Description: "Query to find deferred tools. Use \"select:<tool_name>\" for direct selection, or keywords to search.",
				},
				"max_results": {
					Type:        "number",
					Description: "Maximum number of results to return (default: 5)",
					Default:     5,
				},
			},
		},
		Strict: true,
	}
	return result
}

// ─────────────────────────────────────────────
// Execute
// ─────────────────────────────────────────────

func (bt *ToolSearch) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall

	query, _ := toolCall.Arguments["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return bt.ErrorReturn(toolCall, fmt.Errorf("query is required"))
	}

	maxResults := 5
	if v, ok := toolCall.Arguments["max_results"]; ok {
		if f, ok := v.(float64); ok {
			maxResults = int(f)
		}
	}

	deferredTools := bt.getDeferredTools(toolUseContext.Tools)

	// 1. select: prefix → exact selection
	if strings.HasPrefix(strings.ToLower(query), "select:") {
		namesStr := strings.TrimSpace(query[7:])
		matchedNames := bt.handleSelect(namesStr, deferredTools, toolUseContext.Tools)
		return core_types.ToolContext{
			ToolCall: toolCall,
			Content: core_types.ToolSearchContent{
				Matchs:             matchedNames,
				Query:              query,
				TotalDeferredTools: len(deferredTools),
			},
		}, nil
	}

	// 2. Keyword search with scoring
	matchedNames := bt.searchKeywords(query, deferredTools, maxResults)

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.ToolSearchContent{
			Matchs:             matchedNames,
			Query:              query,
			TotalDeferredTools: len(deferredTools),
		},
	}, nil
}

// ─────────────────────────────────────────────
// select: prefix 处理
// ─────────────────────────────────────────────

func (bt *ToolSearch) handleSelect(namesStr string, deferredTools, allTools []core_types.AgentToolApi) []string {
	requested := strings.Split(namesStr, ",")
	var found []string
	for _, name := range requested {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		t := findToolByName(deferredTools, name)
		if t == nil {
			t = findToolByName(allTools, name)
		}
		if t != nil {
			found = append(found, t.Name())
		}
	}
	return found
}

// ─────────────────────────────────────────────
// 关键词搜索 + 打分排序
// ─────────────────────────────────────────────

type scoredMatch struct {
	name  string
	score int
}

// searchKeywords 对 deferred 工具做关键词搜索和打分，返回 top maxResults 的工具名。
func (bt *ToolSearch) searchKeywords(query string, deferredTools []core_types.AgentToolApi, maxResults int) []string {
	query = strings.ToLower(strings.TrimSpace(query))
	queryTerms := strings.Fields(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// 拆分为 required（+前缀）和 optional 词
	var requiredTerms, optionalTerms []string
	for _, term := range queryTerms {
		if strings.HasPrefix(term, "+") && len(term) > 1 {
			requiredTerms = append(requiredTerms, term[1:])
		} else {
			optionalTerms = append(optionalTerms, term)
		}
	}

	allScoringTerms := queryTerms
	if len(requiredTerms) > 0 {
		allScoringTerms = append(requiredTerms, optionalTerms...)
	}

	// 预编译词边界正则
	type termPattern struct {
		term    string
		pattern *regexp.Regexp
	}
	var patterns []termPattern
	for _, t := range allScoringTerms {
		patterns = append(patterns, termPattern{
			term:    t,
			pattern: regexp.MustCompile(`\b` + regexp.QuoteMeta(t) + `\b`),
		})
	}

	var scored []scoredMatch
	for _, tool := range deferredTools {
		nameParts := parseToolNameParts(tool.Name())
		desc := tool.GetTool().Description
		hint := tool.GetSearchHint()

		descLower := strings.ToLower(desc)
		hintLower := strings.ToLower(hint)
		fullNameLower := strings.ToLower(tool.Name())

		// 必需词过滤
		if len(requiredTerms) > 0 {
			allMatch := true
			for _, rt := range requiredTerms {
				if !matchesTerm(rt, nameParts, fullNameLower, descLower, hintLower) {
					allMatch = false
					break
				}
			}
			if !allMatch {
				continue
			}
		}

		// 打分
		score := 0
		for _, tp := range patterns {
			// 工具名部分精确匹配
			if containsExact(nameParts, tp.term) {
				score += 10
				continue
			}
			// 完整工具名匹配 (CamelCase 拆词后单个词查不到时回退)
			if fullNameLower == tp.term || strings.Contains(fullNameLower, tp.term) {
				score += 8
				continue
			}
			// 工具名部分包含
			if containsPart(nameParts, tp.term) {
				score += 5
				continue
			}
			// searchHint 命中
			if hintLower != "" && tp.pattern.MatchString(hintLower) {
				score += 4
			}
			// 描述文本命中
			if tp.pattern.MatchString(descLower) {
				score += 2
			}
		}

		if score > 0 {
			scored = append(scored, scoredMatch{name: tool.Name(), score: score})
		}
	}

	// 按分数降序，取 topN
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > maxResults {
		scored = scored[:maxResults]
	}

	result := make([]string, len(scored))
	for i, s := range scored {
		result[i] = s.name
	}
	return result
}

// ─────────────────────────────────────────────
// 工具名解析
// ─────────────────────────────────────────────

// parseToolNameParts 将工具名按 CamelCase 拆分为小写词。
// "EditTool" → ["edit", "tool"]  "WebSearch" → ["web", "search"]
func parseToolNameParts(name string) []string {
	var parts []string
	var current []rune
	for _, r := range name {
		if unicode.IsUpper(r) && len(current) > 0 {
			parts = append(parts, strings.ToLower(string(current)))
			current = nil
		}
		current = append(current, r)
	}
	if len(current) > 0 {
		parts = append(parts, strings.ToLower(string(current)))
	}
	return parts
}

// ─────────────────────────────────────────────
// 匹配辅助函数
// ─────────────────────────────────────────────

// matchesTerm 检查一个词是否命中工具的任意可搜索字段。
func matchesTerm(term string, nameParts []string, fullNameLower, descLower, hintLower string) bool {
	for _, part := range nameParts {
		if part == term || strings.Contains(part, term) {
			return true
		}
	}
	if strings.Contains(fullNameLower, term) {
		return true
	}
	if strings.Contains(descLower, term) {
		return true
	}
	return hintLower != "" && strings.Contains(hintLower, term)
}

func containsExact(parts []string, term string) bool {
	for _, p := range parts {
		if p == term {
			return true
		}
	}
	return false
}

func containsPart(parts []string, term string) bool {
	for _, p := range parts {
		if strings.Contains(p, term) {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────
// BuildToolResult — 覆盖 ToolBase
// ─────────────────────────────────────────────

func (bt *ToolSearch) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, _ := toolContext.Content.(core_types.ToolSearchContent)

	result := apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		IsError:    false,
	}

	if len(content.Matchs) == 0 {
		result.Contents = []apitypes.ContentBlock{{Type: "text", Text: "No matching deferred tools found"}}
	} else {
		result.Contents = []apitypes.ContentBlock{{Type: "text", Text: "Matched tools: " + strings.Join(content.Matchs, ", ")}}
		result.ToolReferences = make([]map[string]any, len(content.Matchs))
		for i, name := range content.Matchs {
			result.ToolReferences[i] = map[string]any{
				"type":      "tool_reference",
				"tool_name": name,
			}
		}
	}
	return result
}

// ─────────────────────────────────────────────
// 工具查找
// ─────────────────────────────────────────────

func findToolByName(tools []core_types.AgentToolApi, name string) core_types.AgentToolApi {
	for _, t := range tools {
		if strings.EqualFold(t.Name(), name) {
			return t
		}
	}
	return nil
}

func (bt *ToolSearch) getDeferredTools(agentTools []core_types.AgentToolApi) []core_types.AgentToolApi {
	var result []core_types.AgentToolApi
	for _, t := range agentTools {
		if t.IsDeferredTool() {
			result = append(result, t)
		}
	}
	return result
}
