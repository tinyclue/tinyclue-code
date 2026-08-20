package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	protocol "github.com/tinyclue/tinyclue-code/api_provider/provider/types"
	"github.com/tinyclue/tinyclue-code/api_provider/types"
)

// OpenAIAdapter 实现 types.ProtocolAdapter，适用于 OpenAI 兼容协议（OpenAI、DeepSeek、Doubao 等）。
type OpenAIAdapter struct {
	name          string
	baseURL       string
	apiKey        string
	model         string
	modelCost     *types.ModelCost
	maxTokens     int
	contextWindow int
}

// NewOpenAIAdapter 创建 OpenAIAdapter。
// authType（"api-key"/"oauth"）无需区分：Authorization: Bearer 对两者均正确，仅作签名一致性收下。
func NewOpenAIAdapter(name, baseURL, apiKey, model string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) *OpenAIAdapter {
	return &OpenAIAdapter{
		name:      name,
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		modelCost: modelCost,
	}
}

var _ types.ProtocolAdapter = (*OpenAIAdapter)(nil)

func (a *OpenAIAdapter) Name() string                { return a.name }
func (a *OpenAIAdapter) Model() string               { return a.model }
func (a *OpenAIAdapter) ModelCost() *types.ModelCost { return a.modelCost }
func (a *OpenAIAdapter) BaseURL() string             { return a.baseURL }
func (a *OpenAIAdapter) Endpoint() string            { return "/chat/completions" }
func (a *OpenAIAdapter) SupportsVision() bool {
	m := strings.ToLower(a.model)
	return strings.Contains(m, "gpt-4o") || strings.Contains(m, "gpt-4-vision") || strings.Contains(m, "gpt-4-turbo") || strings.Contains(m, "gemini") || strings.Contains(m, "claude")
}

func (a *OpenAIAdapter) ContextWindow() int {
	if a.contextWindow > 0 {
		return a.contextWindow
	}
	return types.ContextWindow(a.model)
}

func (a *OpenAIAdapter) MaxTokens() int {
	if a.maxTokens > 0 {
		return a.maxTokens
	}
	return types.MaxTokens(a.model)
}

// openAIFinishReason 将 OpenAI/DeepSeek 的 finish_reason 映射为统一的 StopReason。
func openAIFinishReason(raw string) types.StopReason {
	switch raw {
	case "stop":
		return types.StopReason{FinishReason: types.FinishReasonStop}
	case "length":
		return types.StopReason{FinishReason: types.FinishReasonLength}
	case "tool_calls", "function_call":
		return types.StopReason{FinishReason: types.FinishReasonToolUse}
	default:
		return types.StopReason{FinishReason: types.FinishReasonError, ReasonMessage: raw}
	}
}

// ── 协议转换 ──

func (a *OpenAIAdapter) BuildRequest(req *types.ChatRequest, stream bool) ([]byte, error) {
	messages := make([]protocol.OpenAIMessage, len(req.Messages))
	for i, m := range req.Messages {
		msg := protocol.OpenAIMessage{Role: string(m.RoleName())}
		switch v := m.(type) {
		case types.UserMessage:
			content, _ := json.Marshal(v.Text)
			msg.Content = json.RawMessage(content)
		case types.AssistantMessage:
			content, _ := json.Marshal(v.TextContent.Text)
			msg.Content = json.RawMessage(content)
			if len(v.ToolCalls) > 0 {
				tcs := make([]protocol.OpenAIToolCall, len(v.ToolCalls))
				for j, tc := range v.ToolCalls {
					argsStr := ""
					if tc.Arguments != nil {
						b, _ := json.Marshal(tc.Arguments)
						argsStr = string(b)
					}
					tcs[j] = protocol.OpenAIToolCall{
						ID:   tc.ID,
						Type: "function",
						Function: protocol.OpenAIToolCallFunction{
							Name:      tc.Name,
							Arguments: argsStr,
						},
					}
				}
				msg.ToolCalls = tcs
			}
		case types.SystemMessage:
			content, _ := json.Marshal(v.Text)
			msg.Content = json.RawMessage(content)
		case types.ToolResultMessage:
			var toolContent json.RawMessage
			if len(v.Contents) > 0 {
				parts := make([]map[string]any, 0, len(v.Contents))
				for _, c := range v.Contents {
					switch c.Type {
					case "image":
						parts = append(parts, map[string]any{
							"type": "image_url",
							"image_url": map[string]any{
								"url": "data:" + c.ImageMimeType + ";base64," + c.ImageData,
							},
						})
					default:
						parts = append(parts, map[string]any{
							"type": "text",
							"text": c.Text,
						})
					}
				}
				data, _ := json.Marshal(parts)
				toolContent = json.RawMessage(data)
			} else {
				data, _ := json.Marshal("")
				toolContent = json.RawMessage(data)
			}
			msg.Content = toolContent
			msg.ToolCallID = v.ToolCallId
		}
		messages[i] = msg
	}

	apiReq := protocol.OpenAIChatRequest{
		Model:           a.model,
		Messages:        messages,
		Stream:          stream,
		ReasoningEffort: req.ReasoningEffort,
	}
	if req.Temperature != nil {
		apiReq.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		apiReq.MaxTokens = *req.MaxTokens
	}
	if len(req.Tools) > 0 {
		tools := make([]protocol.OpenAIRequestTool, len(req.Tools))
		for j, t := range req.Tools {
			params, err := json.Marshal(t.Parameters)
			if err != nil {
				return nil, fmt.Errorf("%s: marshal tool parameters: %w", a.name, err)
			}
			tools[j] = protocol.OpenAIRequestTool{
				Type: "function",
				Function: protocol.OpenAIRequestFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  params,
					Strict:      t.Strict,
				},
			}
		}
		apiReq.Tools = tools
	}

	return json.Marshal(apiReq)
}

func (a *OpenAIAdapter) SetHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+a.apiKey)
}

func (a *OpenAIAdapter) ParseResponse(respBody []byte) (*types.ChatResponse, error) {
	var openAIResp protocol.OpenAIChatResponse
	if err := json.Unmarshal(respBody, &openAIResp); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", a.name, err)
	}

	if len(openAIResp.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty choices", a.name)
	}

	choice := openAIResp.Choices[0]
	if choice.Message == nil {
		return nil, fmt.Errorf("%s: empty message", a.name)
	}

	chatResp := &types.ChatResponse{}
	msg := types.AssistantMessage{
		Role:          types.AssistantRole,
		Provider:      a.Name(),
		Model:         a.model,
		API:           a.baseURL,
		Timestamp:     time.Now().UnixMilli(),
		CreatedAt:     time.Now(),
		ResponseID:    openAIResp.ID,
		ResponseModel: openAIResp.Model,
	}

	if choice.Message.ReasoningContent != "" {
		msg.ThinkingContent.Thinking = choice.Message.ReasoningContent
	}
	if len(choice.Message.Content) > 0 {
		var textContent string
		if err := json.Unmarshal(choice.Message.Content, &textContent); err == nil && textContent != "" {
			msg.TextContent.Text = textContent
		}
	}
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	chatResp.Content = msg

	if choice.FinishReason != nil {
		chatResp.Content.StopReason = openAIFinishReason(*choice.FinishReason)
	}
	if openAIResp.Usage != nil {
		promptTokens := openAIResp.Usage.PromptTokens
		completionTokens := openAIResp.Usage.CompletionTokens

		cacheReadTokens := openAIResp.Usage.PromptCacheHitTokens
		cacheWriteTokens := 0
		if openAIResp.Usage.PromptTokensDetails != nil {
			if cacheReadTokens == 0 {
				cacheReadTokens = openAIResp.Usage.PromptTokensDetails.CachedTokens
			}
			cacheWriteTokens = openAIResp.Usage.PromptTokensDetails.WriteTokens
		}

		// OpenAI prompt_tokens 包含 cache，扣除后得到非缓存 input
		nonCachedInput := promptTokens - cacheReadTokens - cacheWriteTokens
		if nonCachedInput < 0 {
			nonCachedInput = 0
		}
		chatResp.Content.Usage = types.CalculateUsage(nonCachedInput, completionTokens, cacheReadTokens, cacheWriteTokens)
	}

	return chatResp, nil
}

func (a *OpenAIAdapter) ReadStream(ctx context.Context, resp *http.Response, stream *types.EventStream) {
	defer resp.Body.Close()
	defer stream.Close()

	types.CancelOnDone(ctx, resp.Body)

	if !stream.SendStart(ctx) {
		if errors.Is(ctx.Err(), context.Canceled) {
			stream.SendAborted(ctx.Err().Error())
		} else {
			stream.SendError(types.ErrCodeStreamSend, "send start event failed", "")
		}
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 256*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk protocol.OpenAIChatResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			stream.SendError(types.ErrCodeStreamParse, fmt.Sprintf("parse chunk: %v", err), "")
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		if chunk.ID != "" {
			stream.Response().Content.ResponseID = chunk.ID
		}
		if chunk.Model != "" {
			stream.Response().Content.ResponseModel = chunk.Model
		}

		delta := chunk.Choices[0].Delta

		if delta != nil && delta.ReasoningContent != "" {
			if !stream.SendThinkingStart(ctx) {
				stream.SendError(types.ErrCodeStreamSend, "send thinking start event failed", "")
				return
			}
			stream.AccumulatedThinking += delta.ReasoningContent
			if !stream.SendDelta(ctx, types.EventTypeThinkingDelta, delta.ReasoningContent) {
				stream.SendError(types.ErrCodeStreamSend, "send thinking delta event failed", "")
				return
			}
		}

		if delta != nil && len(delta.Content) > 0 {
			var textContent string
			if err := json.Unmarshal(delta.Content, &textContent); err == nil && textContent != "" {
				if !stream.SendTextStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send text start event failed", "")
					return
				}
				stream.AccumulatedText += textContent
				if !stream.SendDelta(ctx, types.EventTypeTextDelta, textContent) {
					stream.SendError(types.ErrCodeStreamSend, "send text delta event failed", "")
					return
				}
			}
		}

		if delta != nil && len(delta.ToolCalls) > 0 {
			if !stream.SendToolCallStart(ctx) {
				stream.SendError(types.ErrCodeStreamSend, "send tool call start event failed", "")
				return
			}
			for _, tc := range delta.ToolCalls {
				acc := stream.GetToolCall(tc.Index)
				if tc.ID != "" {
					acc.ID = tc.ID
				}
				if tc.Function.Name != "" {
					acc.Name = tc.Function.Name
				}
				if tc.Function.Arguments != "" {
					acc.RawArguments += tc.Function.Arguments
				}
			}
		}

		if chunk.Choices[0].FinishReason != nil && *chunk.Choices[0].FinishReason != "" {
			stream.AccumulatedStop = openAIFinishReason(*chunk.Choices[0].FinishReason)
		}
		if chunk.Usage != nil {
			promptTokens := chunk.Usage.PromptTokens
			completionTokens := chunk.Usage.CompletionTokens

			cacheReadTokens := chunk.Usage.PromptCacheHitTokens
			cacheWriteTokens := 0
			if chunk.Usage.PromptTokensDetails != nil {
				if cacheReadTokens == 0 {
					cacheReadTokens = chunk.Usage.PromptTokensDetails.CachedTokens
				}
				cacheWriteTokens = chunk.Usage.PromptTokensDetails.WriteTokens
			}

			// OpenAI prompt_tokens 包含 cache，扣除后得到非缓存 input
			nonCachedInput := promptTokens - cacheReadTokens - cacheWriteTokens
			if nonCachedInput < 0 {
				nonCachedInput = 0
			}
			stream.AccumulatedUsage = types.CalculateUsage(nonCachedInput, completionTokens, cacheReadTokens, cacheWriteTokens)

			if a.modelCost != nil {
				stream.AccumulatedUsage.Cost = types.CalculateCost(*a.modelCost, nonCachedInput, completionTokens, cacheReadTokens, cacheWriteTokens)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			stream.SendAborted(err.Error())
		} else {
			stream.SendError(types.ErrCodeStreamRead, err.Error(), "")
		}
		return
	}

	stream.Finalize(ctx)
}
