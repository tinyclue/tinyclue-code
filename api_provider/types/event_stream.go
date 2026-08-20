package types

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// CancelOnDone 在 ctx 取消时自动关闭 closer（用于中断 decoder.Decode 等阻塞读）。
// 使用 context.AfterFunc，不取消时不占用 goroutine。
func CancelOnDone(ctx context.Context, closer io.Closer) {
	context.AfterFunc(ctx, func() {
		closer.Close()
	})
}

// streamToolCall 流式累积中的工具调用暂存结构。
type streamToolCall struct {
	ID           string
	Name         string
	Arguments    map[string]any
	RawArguments string
}

// EventStream 统一封装 API 调用结果的事件流和响应构建。
//   - 同步 Chat:     返回 *EventStream，通过 Response() 获取结果（无 channel）
//   - 流式 ChatStream: 返回 *EventStream，通过 Chan() 消费事件流
type EventStream struct {
	ch       chan StreamEvent // 流式时非 nil，同步时 nil
	response *ChatResponse    // 始终有效

	// ---- 内容累积状态 ----
	AccumulatedText     string
	AccumulatedThinking string
	AccumulatedUsage    Usage      // 流式累积的 usage，Finalize 时写入 Content
	AccumulatedStop     StopReason // 流式累积的 stop reason，Finalize 时写入 Content
	SentTextStart       bool
	SentThinkingStart   bool
	SentThinkingEnd     bool
	SentToolCallStart   bool
	ToolCallMap         map[int]*streamToolCall

	closeOnce    sync.Once
	finalizeOnce sync.Once
}

// NewEventStream 创建 EventStream。同步/流式共用构造。
func NewEventStream(provider, model, apiURL string) *EventStream {
	return &EventStream{
		response: &ChatResponse{
			Content: AssistantMessage{
				Provider:  provider,
				Model:     model,
				API:       apiURL,
				Timestamp: time.Now().UnixMilli(),
			},
			Error: Error{IsSuccess: true, Code: 200},
		},
		ToolCallMap: make(map[int]*streamToolCall),
	}
}

// InitStream 将 EventStream 切换为流式模式（初始化 channel）。仅流式场景调用一次。
func (s *EventStream) InitStream() {
	s.ch = make(chan StreamEvent, 1)
}

// Chan 返回只读 channel（流式用）。同步模式返回 nil。
func (s *EventStream) Chan() <-chan StreamEvent { return s.ch }

// Response 返回 ChatResponse（同步/流式通用）。
func (s *EventStream) Response() *ChatResponse { return s.response }

// HasError 快捷判断是否出错。
func (s *EventStream) HasError() bool { return !s.response.Error.IsSuccess }

// Close 安全关闭 channel（流式用，可重复调用）。
func (s *EventStream) Close() {
	if s.ch != nil {
		s.closeOnce.Do(func() { close(s.ch) })
	}
}

// SendError 发送错误事件（流式）或直接设错（同步），
// 并将流式累积的数据（AccumulatedText / AccumulatedThinking 等）刷入 response.Content。
func (s *EventStream) SendError(code int, errMsg, rawMsg string) {
	s.flushAccumulatedContent(StopReason{FinishReason: FinishReasonError, ReasonMessage: errMsg})
	s.response.Error = Error{IsSuccess: false, Code: code, ErrorMessage: errMsg, RawMessage: rawMsg}
	s.sendErrorEvent(errMsg)
}

// SendAborted 发送用户中断事件，
// 并将流式累积的数据（AccumulatedText / AccumulatedThinking 等）刷入 response.Content。
func (s *EventStream) SendAborted(errMsg string) {
	s.flushAccumulatedContent(StopReason{FinishReason: FinishReasonAborted, ReasonMessage: errMsg})
	s.response.Error = Error{IsSuccess: false, Code: ErrCodeAborted, ErrorMessage: errMsg}
	s.sendErrorEvent(errMsg)
}

func (s *EventStream) sendErrorEvent(errMsg string) {
	s.response.Content.Timestamp = time.Now().UnixMilli()
	if s.ch != nil {
		s.ch <- StreamEvent{EventType: EventTypeError, Delta: errMsg, Response: s.response}
		s.Close()
	}
}

// 将流式累积数据（AccumulatedText / AccumulatedThinking / ToolCallMap / Usage）
// 刷入 s.response.Content，供 SendAborted / SendError / Finalize 共用。
func (s *EventStream) flushAccumulatedContent(stop StopReason) {
	msg := AssistantMessage{
		Role:            AssistantRole,
		TextContent:     TextContent{Text: s.AccumulatedText},
		ThinkingContent: ThinkingContent{Thinking: s.AccumulatedThinking},
		Usage:           s.AccumulatedUsage,
		StopReason:      stop,
		API:             s.response.Content.API,
		Provider:        s.response.Content.Provider,
		Model:           s.response.Content.Model,
		Timestamp:       time.Now().UnixMilli(),
		ResponseID:      s.response.Content.ResponseID,
		ResponseModel:   s.response.Content.ResponseModel,
		CreatedAt:       s.response.Content.CreatedAt,
	}
	if s.SentToolCallStart {
		msg.ToolCalls = s.buildToolCalls()
	}
	s.response.Content = msg
}

// buildToolCalls 从 ToolCallMap 构建 ToolCall 切片。
func (s *EventStream) buildToolCalls() []ToolCall {
	calls := make([]ToolCall, 0, len(s.ToolCallMap))
	for _, acc := range s.ToolCallMap {
		var args map[string]any
		if acc.RawArguments != "" {
			json.Unmarshal([]byte(acc.RawArguments), &args)
		}
		acc.Arguments = args
		calls = append(calls, ToolCall{
			ID:        acc.ID,
			Name:      acc.Name,
			Arguments: acc.Arguments,
		})
	}
	return calls
}

// sendEvent 是底层事件发送方法。阻塞发送。
func (s *EventStream) sendEvent(eventType, delta string) bool {
	if s.ch == nil {
		return false
	}
	s.ch <- StreamEvent{EventType: eventType, Delta: delta, Response: s.response}
	return true
}

// SendStart 发出 start 事件。
func (s *EventStream) SendStart(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	return s.sendEvent(EventTypeStart, "")
}

// sendThinkingEnd 在 thinking 已开始且尚未结束时补发 thinking_end 事件（幂等），
// 确保任何 text/tool 输出之前 thinking 块先闭合。
func (s *EventStream) sendThinkingEnd() bool {
	if !s.SentThinkingStart || s.SentThinkingEnd {
		return true
	}
	if !s.sendEvent(EventTypeThinkingEnd, "") {
		return false
	}
	s.SentThinkingEnd = true
	return true
}

// SendTextStart 发出 text_start 事件，确保只发一次。
// 若 thinking 已开始但未结束，先补发 thinking_end——保证"思考先于回答"的事件时序。
func (s *EventStream) SendTextStart(ctx context.Context) bool {
	if s.SentTextStart {
		return true
	}
	if !s.sendThinkingEnd() {
		return false
	}
	if !s.sendEvent(EventTypeTextStart, "") {
		return false
	}
	s.SentTextStart = true
	return true
}

// SendThinkingStart 发出 thinking_start 事件，确保只发一次。
func (s *EventStream) SendThinkingStart(ctx context.Context) bool {
	if s.SentThinkingStart {
		return true
	}
	if !s.sendEvent(EventTypeThinkingStart, "") {
		return false
	}
	s.SentThinkingStart = true
	return true
}

// SendToolCallStart 发出 toolcall_start 事件，确保只发一次。
// 若 thinking 已开始但未结束，先补发 thinking_end（与 SendTextStart 同理）。
func (s *EventStream) SendToolCallStart(ctx context.Context) bool {
	if s.SentToolCallStart {
		return true
	}
	if !s.sendThinkingEnd() {
		return false
	}
	if !s.sendEvent(EventTypeToolCallStart, "") {
		return false
	}
	s.SentToolCallStart = true
	return true
}

// SendDelta 发出 xx_delta 事件。
func (s *EventStream) SendDelta(ctx context.Context, eventType, delta string) bool {
	return s.sendEvent(eventType, delta)
}

// GetToolCall 按 index 获取或创建 streamToolCall。
func (s *EventStream) GetToolCall(index int) *streamToolCall {
	if acc, ok := s.ToolCallMap[index]; ok {
		return acc
	}
	acc := &streamToolCall{}
	s.ToolCallMap[index] = acc
	return acc
}

// Finalize 依次发出 *End 事件、构建 Content、发出 Done 事件。
// 使用 sync.Once 保证幂等：多次调用仅第一次生效。
func (s *EventStream) Finalize(ctx context.Context) bool {
	s.finalizeOnce.Do(func() {
		// 若 thinking 自始至终未被 text/tool 闭合（纯思考/纯工具轮），在此补发
		s.sendThinkingEnd()
		if s.SentTextStart {
			s.sendEvent(EventTypeTextEnd, "")
		}
		if s.SentToolCallStart {
			s.sendEvent(EventTypeToolCallEnd, "")
		}

		s.flushAccumulatedContent(s.AccumulatedStop)
		s.sendEvent(EventTypeDone, "")
	})
	return true
}
