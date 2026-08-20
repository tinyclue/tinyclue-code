package core

import (
	"sync"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// charsPerToken 约 4 字符 ≈ 1 token 的估算系数（对齐 pi/tinyclue 启发式）。
const charsPerToken = 4

// TokenCounter session 级流式 token 计数器：订阅主/子两个事件队列，
// 解析业务流式事件（主 agent 文本/思考 delta、工具参数；子 agent 原样转发的
// 流式事件），累加字节并换算估算 token 数。估算值变化时回调全部已注册回调。
//
// 纯业务计数：不负责渲染。渲染方（statusline.TokenCountPart）通过
// RegisterCallback 注册回调，拿到估算值后自行做平滑收敛与样式。
// 每次主 agent 轮次开始（AgentStart）时重置计数（子队列不转发 AgentStart，
// 不会误重置）。
type TokenCounter struct {
	mu        sync.Mutex
	bytes     int64                // 流式累加的响应字节数
	estimate  int                  // 估算 token 数 = bytes / 4
	callbacks []func(estimate int) // token 估算变化回调（可注册多个）
}

// NewTokenCounter 创建 session 级流式 token 计数器。
func NewTokenCounter() *TokenCounter {
	return &TokenCounter{}
}

// Start 订阅主/子两个事件队列，开始处理流式事件的 token 计数。
// 与 TuiEvent.Run 同生命周期（进程内常驻）。
func (tc *TokenCounter) Start() {
	// 主队列：主 agent 流式事件 + AgentStart 轮次边界
	Subscribe(AgentEventQueue, func(msg types.EventMessage) {
		tc.handleMain(msg)
	})
	// 子队列：子 agent 原样转发的流式事件（只计数、不重置）
	Subscribe(SubAgentEventQueue, func(msg types.EventMessage) {
		tc.handleSub(msg)
	})
}

// RegisterCallback 注册 token 估算变化回调（估算值变化时触发）。
// 可多次调用注册多个回调，估算值变化时全部按注册顺序同步调用；
// 回调运行在事件处理 goroutine 中，回调内应避免阻塞。
func (tc *TokenCounter) RegisterCallback(fn func(estimate int)) {
	tc.mu.Lock()
	tc.callbacks = append(tc.callbacks, fn)
	tc.mu.Unlock()
}

// handleMain 处理主队列事件：轮次边界（AgentStart）重置计数，其余提取字节数累加。
func (tc *TokenCounter) handleMain(msg types.EventMessage) {
	if msg.EventType == types.AgentStart {
		tc.Reset()
		return
	}
	tc.count(msg)
}

// handleSub 处理子队列事件：子 agent 流式事件只计数、不重置。
// 子队列不转发 AgentStart，不会触碰轮次边界。
func (tc *TokenCounter) handleSub(msg types.EventMessage) {
	tc.count(msg)
}

// count 从事件中提取应计入的字节数并累加（主/子事件共用的唯一提取点）。
func (tc *TokenCounter) count(msg types.EventMessage) {
	n, ok := types.TokenEventBytes(msg.EventType, msg.Message)
	if !ok || n <= 0 {
		return
	}
	tc.RecordBytes(n)
}

// RecordBytes 累加一次流式 delta 的字节数；估算 token 值变化时
// 同步调用全部已注册回调。
func (tc *TokenCounter) RecordBytes(n int) {
	tc.mu.Lock()
	tc.bytes += int64(n)
	estimate := int(tc.bytes / charsPerToken)
	changed := estimate != tc.estimate
	tc.estimate = estimate
	callbacks := tc.callbacks
	tc.mu.Unlock()
	if !changed {
		return
	}
	for _, fn := range callbacks {
		if fn != nil {
			fn(estimate)
		}
	}
}

// Bytes 返回当前累计的流式字节数。
func (tc *TokenCounter) Bytes() int64 {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.bytes
}

// Estimate 返回当前估算的 token 数（bytes / 4）。
func (tc *TokenCounter) Estimate() int {
	tc.mu.Lock()
	defer tc.mu.Unlock()
	return tc.estimate
}

// Reset 清空计数（新轮次）。
func (tc *TokenCounter) Reset() {
	tc.mu.Lock()
	tc.bytes = 0
	tc.estimate = 0
	tc.mu.Unlock()
}
