package core

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"sync"
)

const (
	UserInputQueue         = "user_input_queue"
	AgentEventQueue        = "agent_event_queue"
	AgentNotificationQueue = "agent_notification_queue"
	// SubAgentEventQueue 子 agent 流式事件队列：原样转发子 agent 的流式事件，
	// 只供业务消费（TokenCounter 计数），TuiEvent 不订阅，避免污染主对话显示。
	SubAgentEventQueue = "sub_agent_event_queue"
)

// subscriber 表示一个事件订阅者。
// 拥有独立的消费 goroutine 和已消费位置。
type subscriber struct {
	handler func(types.EventMessage)
	pos     int           // 已消费的消息在队列中的索引
	done    chan struct{} // 取消信号
}

// EventQueue 是一个命名事件队列。
// 采用共享消息列表 + sync.Cond 广播通知，每个订阅者独立拉取，互不影响。
type EventQueue struct {
	mu          sync.Mutex
	cond        *sync.Cond
	msgs        []types.EventMessage // 所有订阅者共享的消息列表
	subscribers []*subscriber
}

// AgentEvent 是事件总线，管理多个命名队列的发布与订阅。
type AgentEvent struct {
	queues map[string]*EventQueue
	mu     sync.RWMutex
}

var defaultAgentEvent *AgentEvent

func init() {
	defaultAgentEvent = NewAgentEvent()
	defaultAgentEvent.RegisterQueue(UserInputQueue)
	defaultAgentEvent.RegisterQueue(AgentEventQueue)
	defaultAgentEvent.RegisterQueue(AgentNotificationQueue)
	defaultAgentEvent.RegisterQueue(SubAgentEventQueue)
}

func NewAgentEvent() *AgentEvent {
	return &AgentEvent{
		queues: make(map[string]*EventQueue),
	}
}

// RegisterQueue 注册一个命名事件队列。
func (ae *AgentEvent) RegisterQueue(name string) {
	ae.mu.Lock()
	defer ae.mu.Unlock()
	q := &EventQueue{}
	q.cond = sync.NewCond(&q.mu)
	ae.queues[name] = q
}

// Publish 向指定队列投递消息，追加到共享消息列表并广播通知所有订阅者。
// 发布者只负责生产，不关心订阅者消费情况。
func (ae *AgentEvent) Publish(queueName string, msg types.EventMessage) {
	ae.mu.RLock()
	q, ok := ae.queues[queueName]
	ae.mu.RUnlock()
	if !ok {
		return
	}

	q.mu.Lock()
	q.msgs = append(q.msgs, msg)
	q.cond.Broadcast()
	q.mu.Unlock()
}

// Subscribe 订阅指定队列，handler 在独立的 goroutine 中按序接收所有消息。
// 每个订阅者独立拉取，消费慢不影响其他订阅者。
// 返回 cancel 函数，调用后取消订阅并停止消费。
func (ae *AgentEvent) Subscribe(queueName string, handler func(types.EventMessage)) func() {
	ae.mu.RLock()
	q, ok := ae.queues[queueName]
	ae.mu.RUnlock()
	if !ok {
		return func() {}
	}

	sub := &subscriber{
		handler: handler,
		done:    make(chan struct{}),
	}

	q.mu.Lock()
	sub.pos = len(q.msgs) // 从当前尾部开始，不消费历史消息
	q.subscribers = append(q.subscribers, sub)
	q.mu.Unlock()

	go func() {
		q.mu.Lock()
		defer q.mu.Unlock()
		for {
			// 检查是否已取消
			select {
			case <-sub.done:
				return
			default:
			}

			// 消费所有未读消息
			if sub.pos < len(q.msgs) {
				msg := q.msgs[sub.pos]
				sub.pos++
				q.mu.Unlock()
				sub.handler(msg)
				q.mu.Lock()
				continue
			}

			// 没有新消息，等待广播
			q.cond.Wait()
		}
	}()

	return func() {
		close(sub.done)
		q.mu.Lock()
		defer q.mu.Unlock()
		for i, s := range q.subscribers {
			if s == sub {
				q.subscribers = append(q.subscribers[:i], q.subscribers[i+1:]...)
				break
			}
		}
		q.cond.Broadcast()
	}
}

// ── 包级函数（操作默认实例） ──

func RegisterQueue(name string) {
	defaultAgentEvent.RegisterQueue(name)
}

func Publish(queueName string, msg types.EventMessage) {
	defaultAgentEvent.Publish(queueName, msg)
}

func Subscribe(queueName string, handler func(types.EventMessage)) func() {
	return defaultAgentEvent.Subscribe(queueName, handler)
}
