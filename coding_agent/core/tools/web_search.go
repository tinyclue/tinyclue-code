package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/log"
	"strings"
	"time"
)

type WebSearch struct {
	*ToolBase
}

func NewWebSearch() *WebSearch {
	return &WebSearch{
		ToolBase: &ToolBase{
			deferred:   true,
			searchHint: "search the web for current information",
		},
	}
}

func (bt *WebSearch) Name() string {
	return core_types.WEB_SEARCH_TOOL_NAME
}

func (bt *WebSearch) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_types.WEB_SEARCH_TOOL_NAME,
		Description: prompt.GetWebSearchPrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"query",
			},
			Properties: map[string]apitypes.SchemaItem{
				"query": {
					Type:        "string",
					Description: "The search query to use",
				},
				"allowed_domains": {
					Type:        "array",
					Description: "Only include search results from these domains",
					Items: &apitypes.SchemaItem{
						Type: "string",
					},
				},
				"blocked_domains": {
					Type:        "array",
					Description: "Never include search results from these domains",
					Items: &apitypes.SchemaItem{
						Type: "string",
					},
				},
			},
		},
		Strict: true,
	}
	return result
}

func (bt *WebSearch) Execute(ctx context.Context, toolUseContext core_types.ToolUseContext) (core_types.ToolContext, error) {
	toolCall := toolUseContext.ToolCall

	query, _ := toolCall.Arguments["query"].(string)
	if query == "" {
		return bt.ErrorReturn(toolCall, fmt.Errorf("query is required"))
	}

	client := api_provider.GetClient()
	if client == nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("api provider not initialized"))
	}

	allowedDomains, _ := toolCall.Arguments["allowed_domains"].([]any)
	blockedDomains, _ := toolCall.Arguments["blocked_domains"].([]any)
	var allowedStrs, blockedStrs []string
	for _, d := range allowedDomains {
		if s, ok := d.(string); ok {
			allowedStrs = append(allowedStrs, s)
		}
	}
	for _, d := range blockedDomains {
		if s, ok := d.(string); ok {
			blockedStrs = append(blockedStrs, s)
		}
	}

	systemMsg := apitypes.SystemMessage{
		Role:      apitypes.SystemRole,
		Text:      "You are an assistant for performing a web search tool use",
		CreatedAt: time.Now(),
	}
	userMsg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      fmt.Sprintf("Perform a web search for the query: %s", query),
		CreatedAt: time.Now(),
	}

	tool := apitypes.Tool{
		Type:    apitypes.WebSearchBuiltIn,
		Name:    "web_search",
		MaxUses: 8,
	}
	if len(allowedStrs) > 0 {
		tool.AllowedDomains = allowedStrs
	}
	if len(blockedStrs) > 0 {
		tool.BlockedDomains = blockedStrs
	}
	req := &apitypes.ChatRequest{
		Messages: []apitypes.Message{systemMsg, userMsg},
		Tools:    []apitypes.Tool{tool},
	}

	startTime := time.Now()
	stream := client.Chat(ctx, req)
	if stream.HasError() {
		errResp := stream.Response()
		return bt.ErrorReturn(toolCall, fmt.Errorf("web search sub-request failed: %s (code %d)", errResp.Error.ErrorMessage, errResp.Error.Code))
	}

	resp := stream.Response()
	durationSeconds := time.Since(startTime).Seconds()

	// 从原始响应中解析 web_search_tool_result 块
	results := parseWebSearchResults(resp.RawBody)

	return core_types.ToolContext{
		ToolCall: toolCall,
		Content: core_types.WebSearchContent{
			Query:           query,
			Results:         results,
			DurationSeconds: durationSeconds,
		},
	}, nil
}

// webSearchResultRaw 用于解析 web_search_tool_result 中的 content 数组
type webSearchResultRaw struct {
	Type  string `json:"type"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// webSearchToolResultRaw 用于解析响应中的 web_search_tool_result 块
type webSearchToolResultRaw struct {
	Type      string               `json:"type"`
	ToolUseID string               `json:"tool_use_id"`
	Content   []webSearchResultRaw `json:"content"`
}

// anthropicResponseRaw 用于解析完整响应中的 content 数组
type anthropicResponseRaw struct {
	Content []json.RawMessage `json:"content"`
}

// contentBlockRaw 用于解析 content 块的类型
type contentBlockRaw struct {
	Type string `json:"type"`
}

// parseWebSearchResults 从原始响应体中解析 web_search_tool_result 块，
// 返回 []any，每个元素为 string（文本）或 types.WebSearchResult。
func parseWebSearchResults(rawBody []byte) []any {
	if len(rawBody) == 0 {
		return nil
	}

	var resp anthropicResponseRaw
	if err := json.Unmarshal(rawBody, &resp); err != nil {
		log.Errorf(context.Background(), "web_search: parse response failed: %v", err)
		return nil
	}

	var results []any
	for _, rawBlock := range resp.Content {
		var typeOnly contentBlockRaw
		if err := json.Unmarshal(rawBlock, &typeOnly); err != nil {
			continue
		}

		switch typeOnly.Type {
		case "web_search_tool_result":
			var result webSearchToolResultRaw
			if err := json.Unmarshal(rawBlock, &result); err != nil {
				continue
			}
			var hits []core_types.WebSearchHit
			for _, r := range result.Content {
				hits = append(hits, core_types.WebSearchHit{
					Title: r.Title,
					URL:   r.URL,
				})
			}
			results = append(results, core_types.WebSearchResult{
				ToolUseID: result.ToolUseID,
				Content:   hits,
			})

		case "text":
			var textBlock struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(rawBlock, &textBlock); err != nil {
				continue
			}
			if textBlock.Text != "" {
				results = append(results, textBlock.Text)
			}
		}
	}

	return results
}

func (bt *WebSearch) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	content, _ := toolContext.Content.(core_types.WebSearchContent)

	var formattedOutput string
	formattedOutput = fmt.Sprintf(`Web search results for query: "%s"`, content.Query) + "\n\n"

	for _, result := range content.Results {
		switch v := result.(type) {
		case string:
			formattedOutput += v + "\n\n"
		case core_types.WebSearchResult:
			if len(v.Content) > 0 {
				linksJSON, _ := json.Marshal(v.Content)
				formattedOutput += fmt.Sprintf("Links: %s\n\n", string(linksJSON))
			} else {
				formattedOutput += "No links found.\n\n"
			}
		}
	}

	formattedOutput += "\nREMINDER: You MUST include the sources above in your response to the user using markdown hyperlinks."

	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolContext.ToolCall.ID,
		ToolName:   toolContext.ToolCall.Name,
		Contents:   []apitypes.ContentBlock{{Type: "text", Text: strings.TrimSpace(formattedOutput)}},
		IsError:    false,
	}
}
