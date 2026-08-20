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

// AnthropicAdapter 实现 types.ProtocolAdapter，适配 Anthropic Messages API。
type AnthropicAdapter struct {
	baseURL       string
	apiKey        string
	model         string
	version       string
	modelCost     *types.ModelCost
	maxTokens     int
	contextWindow int
	authType      string // "api-key"/"oauth"
}

// NewAnthropicAdapter 创建 AnthropicAdapter。
func NewAnthropicAdapter(baseURL, apiKey, model, version string, modelCost *types.ModelCost, maxTokens, contextWindow int, authType string) *AnthropicAdapter {
	return &AnthropicAdapter{
		baseURL:       baseURL,
		apiKey:        apiKey,
		model:         model,
		version:       version,
		modelCost:     modelCost,
		maxTokens:     maxTokens,
		contextWindow: contextWindow,
		authType:      authType,
	}
}

var _ types.ProtocolAdapter = (*AnthropicAdapter)(nil)

func (a *AnthropicAdapter) Name() string                { return "anthropic" }
func (a *AnthropicAdapter) Model() string               { return a.model }
func (a *AnthropicAdapter) ModelCost() *types.ModelCost { return a.modelCost }
func (a *AnthropicAdapter) BaseURL() string             { return a.baseURL }
func (a *AnthropicAdapter) Endpoint() string            { return "/messages" }
func (a *AnthropicAdapter) SupportsVision() bool {
	return strings.HasPrefix(a.model, "claude-3") || strings.HasPrefix(a.model, "claude-4")
}

func (a *AnthropicAdapter) ContextWindow() int {
	if a.contextWindow > 0 {
		return a.contextWindow
	}
	return types.ContextWindow(a.model)
}

func (a *AnthropicAdapter) MaxTokens() int {
	if a.maxTokens > 0 {
		return a.maxTokens
	}
	return types.MaxTokens(a.model)
}

// anthropicStopReason 将 Anthropic 的 stop_reason 映射为统一的 StopReason。
func anthropicStopReason(raw string) types.StopReason {
	switch raw {
	case "end_turn", "stop_sequence", "pause_turn":
		return types.StopReason{FinishReason: types.FinishReasonStop}
	case "max_tokens", "model_context_window_exceeded":
		return types.StopReason{FinishReason: types.FinishReasonLength}
	case "tool_use", "server_tool_use":
		return types.StopReason{FinishReason: types.FinishReasonToolUse}
	default:
		return types.StopReason{FinishReason: types.FinishReasonError, ReasonMessage: raw}
	}
}

// ── 协议转换 ──

func (a *AnthropicAdapter) BuildRequest(req *types.ChatRequest, stream bool) ([]byte, error) {
	var messages []protocol.AnthropicMessage

	for i := 0; i < len(req.Messages); i++ {
		m := req.Messages[i]

		// 合并连续的 ToolResultMessage 为一条 user 消息（含多个 tool_result 块）
		if _, isTool := m.(types.ToolResultMessage); isTool {
			msg := protocol.AnthropicMessage{Role: "user"}
			var contentBlocks []protocol.AnthropicContentBlock
			for j := i; j < len(req.Messages); j++ {
				tr, ok := req.Messages[j].(types.ToolResultMessage)
				if !ok {
					break
				}
				contentBlocks = append(contentBlocks, a.buildToolResultBlock(tr))
				i = j
			}
			msg.Content = contentBlocks
			messages = append(messages, msg)
			continue
		}

		role := string(m.RoleName())
		msg := protocol.AnthropicMessage{Role: role}

		switch v := m.(type) {
		case types.UserMessage:
			msg.Content = []protocol.AnthropicContentBlock{
				{Type: "text", Text: v.Text},
			}

		case types.AssistantMessage:
			blocks := []protocol.AnthropicContentBlock{}
			if v.ThinkingContent.Thinking != "" {
				blocks = append(blocks, protocol.AnthropicContentBlock{Type: "thinking", Thinking: v.ThinkingContent.Thinking})
			}
			if v.TextContent.Text != "" {
				blocks = append(blocks, protocol.AnthropicContentBlock{Type: "text", Text: v.TextContent.Text})
			}
			for _, tc := range v.ToolCalls {
				input := json.RawMessage("{}")
				if tc.Arguments != nil {
					b, _ := json.Marshal(tc.Arguments)
					input = json.RawMessage(b)
				}
				blocks = append(blocks, protocol.AnthropicContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			msg.Content = blocks

		case types.SystemMessage:
			msg.Content = []protocol.AnthropicContentBlock{
				{Type: "text", Text: v.Text},
			}
		}

		messages = append(messages, msg)
	}

	maxTokens := a.maxTokens
	if maxTokens == 0 {
		maxTokens = types.MaxTokens(a.model)
	}
	if maxTokens == 0 {
		maxTokens = 8192
	}
	if req.MaxTokens != nil {
		maxTokens = *req.MaxTokens
	}

	apiReq := &protocol.AnthropicRequest{
		Model:           a.model,
		Messages:        messages,
		MaxTokens:       maxTokens,
		Stream:          stream,
		ReasoningEffort: req.ReasoningEffort,
	}

	if len(req.Tools) > 0 {
		tools := make([]protocol.AnthropicTool, len(req.Tools))
		for j, t := range req.Tools {
			if t.Type == types.WebSearchBuiltIn {
				tools[j] = protocol.AnthropicTool{
					Type:           types.WebSearchBuiltIn,
					Name:           t.Name,
					MaxUses:        t.MaxUses,
					AllowedDomains: t.AllowedDomains,
					BlockedDomains: t.BlockedDomains,
				}
				continue
			}
			params, err := json.Marshal(t.Parameters)
			if err == nil {
				tools[j] = protocol.AnthropicTool{
					Name:        t.Name,
					Description: t.Description,
					InputSchema: params,
				}
			}
		}
		apiReq.Tools = tools
	}

	return json.Marshal(apiReq)
}

// buildToolResultBlock 将单个 ToolResultMessage 转换为 tool_result 内容块。
func (a *AnthropicAdapter) buildToolResultBlock(v types.ToolResultMessage) protocol.AnthropicContentBlock {
	var content json.RawMessage
	if len(v.Contents) > 0 {
		blocks := make([]map[string]any, 0, len(v.Contents))
		for _, c := range v.Contents {
			switch c.Type {
			case "image":
				blocks = append(blocks, map[string]any{
					"type": "image",
					"source": map[string]any{
						"type":       "base64",
						"media_type": c.ImageMimeType,
						"data":       c.ImageData,
					},
				})
			default:
				blocks = append(blocks, map[string]any{
					"type": "text",
					"text": c.Text,
				})
			}
		}
		data, _ := json.Marshal(blocks)
		content = json.RawMessage(data)
	} else {
		data, _ := json.Marshal("")
		content = json.RawMessage(data)
	}
	return protocol.AnthropicContentBlock{
		Type:      "tool_result",
		ToolUseID: v.ToolCallId,
		Content:   content,
		IsError:   v.IsError,
	}
}

func (a *AnthropicAdapter) SetHeaders(r *http.Request) {
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("anthropic-version", a.version)
	if a.authType == "oauth" {
		// 订阅 OAuth：用 Bearer access token 鉴权；订阅 beta 头若线上 401 再启用/调整。
		r.Header.Set("Authorization", "Bearer "+a.apiKey)
		r.Header.Set("anthropic-beta", "oauth-2025-04-20")
		return
	}
	r.Header.Set("x-api-key", a.apiKey)
}

func (a *AnthropicAdapter) ParseResponse(respBody []byte) (*types.ChatResponse, error) {
	var anResp protocol.AnthropicResponse
	if err := json.Unmarshal(respBody, &anResp); err != nil {
		return nil, fmt.Errorf("anthropic: decode response: %w", err)
	}

	chatResp := &types.ChatResponse{}
	msg := types.AssistantMessage{
		Role:          types.AssistantRole,
		Provider:      a.Name(),
		Model:         a.model,
		API:           a.baseURL,
		Timestamp:     time.Now().UnixMilli(),
		CreatedAt:     time.Now(),
		ResponseID:    anResp.ID,
		ResponseModel: anResp.Model,
		StopReason:    anthropicStopReason(anResp.StopReason),
	}
	for _, block := range anResp.Content {
		switch block.Type {
		case "text":
			msg.TextContent.Text += block.Text
		case "thinking":
			msg.ThinkingContent.Thinking += block.Thinking
		case "redacted_thinking":
			msg.ThinkingContent.Thinking += block.Data
		case "tool_use", "server_tool_use":
			var args map[string]any
			if len(block.Input) > 0 {
				json.Unmarshal(block.Input, &args)
			}
			msg.ToolCalls = append(msg.ToolCalls, types.ToolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: args,
			})
		}
	}
	chatResp.Content = msg

	if anResp.Usage != nil {
		chatResp.Content.Usage = types.CalculateUsage(
			anResp.Usage.InputTokens, anResp.Usage.OutputTokens,
			anResp.Usage.CacheReadInputTokens, anResp.Usage.CacheWriteInputTokens,
		)
	}

	return chatResp, nil
}

func (a *AnthropicAdapter) ReadStream(ctx context.Context, resp *http.Response, stream *types.EventStream) {
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

		var chunk protocol.AnthropicStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			stream.SendError(types.ErrCodeStreamParse, fmt.Sprintf("parse chunk: %v", err), "")
			return
		}

		switch chunk.Type {
		case "message_start":
			if chunk.Message != nil {
				stream.Response().Content.ResponseID = chunk.Message.ID
				stream.Response().Content.ResponseModel = chunk.Message.Model
			}

		case "content_block_start":
			if chunk.ContentBlock == nil {
				continue
			}

			switch chunk.ContentBlock.Type {
			case "thinking":
				if !stream.SendThinkingStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send thinking start event failed", "")
					return
				}

				if chunk.ContentBlock.Thinking != "" {
					stream.AccumulatedThinking += chunk.ContentBlock.Thinking
					if !stream.SendDelta(ctx, types.EventTypeThinkingDelta, chunk.ContentBlock.Thinking) {
						stream.SendError(types.ErrCodeStreamSend, "send thinking delta event failed", "")
						return
					}
				}

			case "text":
				if !stream.SendTextStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send text start event failed", "")
					return
				}

				if chunk.ContentBlock.Text != "" {
					stream.AccumulatedText += chunk.ContentBlock.Text
					if !stream.SendDelta(ctx, types.EventTypeTextDelta, chunk.ContentBlock.Text) {
						stream.SendError(types.ErrCodeStreamSend, "send text delta event failed", "")
						return
					}
				}

			case "redacted_thinking":
				if !stream.SendThinkingStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send thinking start event failed", "")
					return
				}

				if chunk.ContentBlock.Data != "" {
					stream.AccumulatedThinking += chunk.ContentBlock.Data
					if !stream.SendDelta(ctx, types.EventTypeThinkingDelta, chunk.ContentBlock.Data) {
						stream.SendError(types.ErrCodeStreamSend, "send thinking delta event failed", "")
						return
					}
				}

			case "tool_use", "server_tool_use":
				if !stream.SendToolCallStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send tool call start event failed", "")
					return
				}

				if chunk.ContentBlock.ID != "" || chunk.ContentBlock.Name != "" {
					acc := stream.GetToolCall(chunk.Index)
					if chunk.ContentBlock.ID != "" {
						acc.ID = chunk.ContentBlock.ID
					}
					if chunk.ContentBlock.Name != "" {
						acc.Name = chunk.ContentBlock.Name
					}
					if len(chunk.ContentBlock.Input) > 0 && string(chunk.ContentBlock.Input) != "{}" {
						acc.RawArguments = string(chunk.ContentBlock.Input)
					}
				}
			}

		case "content_block_delta":
			if chunk.Delta == nil {
				continue
			}

			switch chunk.Delta.Type {
			case "text_delta", "text":
				if chunk.Delta.Text == "" {
					continue
				}

				if !stream.SendTextStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send text start event failed", "")
					return
				}

				stream.AccumulatedText += chunk.Delta.Text
				if !stream.SendDelta(ctx, types.EventTypeTextDelta, chunk.Delta.Text) {
					stream.SendError(types.ErrCodeStreamSend, "send text delta event failed", "")
					return
				}

			case "input_json_delta":
				if chunk.Delta.PartialJSON == "" {
					continue
				}

				if !stream.SendToolCallStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send tool call start event failed", "")
					return
				}

				acc := stream.GetToolCall(chunk.Index)
				acc.RawArguments += chunk.Delta.PartialJSON
				if !stream.SendDelta(ctx, types.EventTypeToolCallDelta, chunk.Delta.PartialJSON) {
					stream.SendError(types.ErrCodeStreamSend, "send tool call delta event failed", "")
					return
				}

			case "thinking_delta", "thinking":
				if chunk.Delta.Thinking == "" {
					continue
				}

				if !stream.SendThinkingStart(ctx) {
					stream.SendError(types.ErrCodeStreamSend, "send thinking start event failed", "")
					return
				}

				stream.AccumulatedThinking += chunk.Delta.Thinking
				if !stream.SendDelta(ctx, types.EventTypeThinkingDelta, chunk.Delta.Thinking) {
					stream.SendError(types.ErrCodeStreamSend, "send thinking delta event failed", "")
					return
				}
			}

		case "message_delta":
			if chunk.Delta != nil && chunk.Delta.StopReason != "" {
				stream.AccumulatedStop = anthropicStopReason(chunk.Delta.StopReason)
			}
			if chunk.Usage != nil {
				stream.AccumulatedUsage = types.CalculateUsage(
					chunk.Usage.InputTokens, chunk.Usage.OutputTokens,
					chunk.Usage.CacheReadInputTokens, chunk.Usage.CacheWriteInputTokens,
				)
				if a.modelCost != nil {
					stream.AccumulatedUsage.Cost = types.CalculateCost(
						*a.modelCost,
						stream.AccumulatedUsage.Input, stream.AccumulatedUsage.Output,
						stream.AccumulatedUsage.CacheRead, stream.AccumulatedUsage.CacheWrite,
					)
				}
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
