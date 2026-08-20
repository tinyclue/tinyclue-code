package types

import (
	"sync"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// ProgressType 进度事件类型。
type ProgressType string

const (
	ProgressTypeAssistantUsage   ProgressType = "assistant_usage"    // Data: apitypes.AssistantMessage
	ProgressTypeSubAgentComplete ProgressType = "sub_agent_complete" // Data: SubAgentContent
	ProgressTypeSummary          ProgressType = "summary"            // Data: string
	ProgressTypeMcpUpdated       ProgressType = "mcp_updated"        // MCP 状态变化，仅触发 UI 刷新
	ProgressTypePlanModeChanged  ProgressType = "plan_mode_changed"  // EnterPlan/ExitPlan 切换，仅触发 UI 刷新
)

// ProgressSummaryMessage 通过 EventSink 传递进度摘要文本（用于 TUI 渲染等非 onProgress 场景）。
type ProgressSummaryMessage struct {
	Summary string
}

// ProgressEvent 进度事件，替代 apitypes.Message 作为 onProgress 链路的承载。
type ProgressEvent struct {
	Type ProgressType
	Data interface{}
}

type CurrentContext struct {
	mu          sync.Mutex
	messages    []AgentMessage
	newMessages []AgentMessage
	onAppend    func(message AgentMessage)
	onProgress  func(event ProgressEvent)
}

func NewCurrentContext(msgs []apitypes.Message) *CurrentContext {
	var messages []AgentMessage
	for _, msg := range msgs {
		messages = append(messages, AgentMessage{
			Message: msg,
		})
	}
	return &CurrentContext{
		messages: messages,
	}
}

func (cc *CurrentContext) WithOnAppend(fn func(message AgentMessage)) *CurrentContext {
	cc.onAppend = fn
	return cc
}

func (cc *CurrentContext) WithOnProgress(fn func(event ProgressEvent)) *CurrentContext {
	cc.onProgress = fn
	return cc
}

func (cc *CurrentContext) GetMessages() []AgentMessage {
	return cc.newMessages
}

// RebuildFromMessages 用新的消息列表重建 currentContext 的全部消息。
// 重置 newMessages 列表（旧的新消息已通过 OnAppend 提交到 session）。
// OnAppend 回调保持不变。
func (cc *CurrentContext) RebuildFromMessages(messages []apitypes.Message) {
	cc.mu.Lock()
	defer cc.mu.Unlock()
	agentMessages := make([]AgentMessage, len(messages))
	for i, msg := range messages {
		agentMessages[i] = AgentMessage{Message: msg}
	}
	cc.messages = agentMessages
	cc.newMessages = make([]AgentMessage, 0)
}

func (cc *CurrentContext) BuildMessages() []apitypes.Message {
	var result []apitypes.Message
	for _, message := range cc.messages {
		result = append(result, message.Message)
	}
	return result
}

func (cc *CurrentContext) GetNewMessages() []AgentMessage {
	return cc.newMessages
}

func (cc *CurrentContext) AddMessage(message apitypes.Message) {
	agentMessage := AgentMessage{
		Message: message,
	}
	cc.appendMessage(agentMessage)
}

func (cc *CurrentContext) appendMessage(message AgentMessage) {
	cc.messages = append(cc.messages, message)
	cc.newMessages = append(cc.newMessages, message)
	cc.onAppend(message)
}

func (cc *CurrentContext) AddRespMessage(resp *apitypes.ChatResponse) {
	agentMessage := AgentMessage{
		Message:  resp.Content,
		Response: resp,
	}
	cc.appendMessage(agentMessage)
	if cc.onProgress != nil {
		cc.onProgress(ProgressEvent{Type: ProgressTypeAssistantUsage, Data: resp.Content})
	}
}

// AddMessages 批量追加规范消息（工具结果 + 反馈 user 消息等）。
func (cc *CurrentContext) AddToolResultMessages(messages []apitypes.ToolResultMessage) {
	for _, item := range messages {
		agentMessage := AgentMessage{
			Message: item,
		}
		cc.appendMessage(agentMessage)
	}
}

func (cc *CurrentContext) AddUserMessages(messages []apitypes.UserMessage) {
	for _, item := range messages {
		agentMessage := AgentMessage{
			Message: item,
		}
		cc.appendMessage(agentMessage)
	}
}

func (cc *CurrentContext) AddUserMetaMessages(messages []apitypes.UserMessage) {
	if len(messages) == 0 {
		return
	}
	for _, message := range messages {
		agentMessage := AgentMessage{
			Message: message,
		}
		cc.appendMessage(agentMessage)
	}
}

func (cc *CurrentContext) LastEntryIsAssistant() bool {
	if len(cc.messages) == 0 {
		return false
	}
	last := cc.messages[len(cc.messages)-1]
	if _, ok := last.Message.(apitypes.AssistantMessage); ok {
		return true
	}
	return false
}

// Stat 发送监控打点到 OnProgress 链路。
func (cc *CurrentContext) Stat(summary string) {
	if cc.onProgress != nil {
		cc.onProgress(ProgressEvent{Type: ProgressTypeSummary, Data: ProgressSummaryMessage{Summary: summary}})
	}
}

// EmitProgress 向 onProgress 链路发送事件，不修改上下文内容。
func (cc *CurrentContext) EmitProgress(event ProgressEvent) {
	if cc.onProgress != nil {
		cc.onProgress(event)
	}
}

// // RemoveLastAssistantEntry 如果 currentEntrys 最后一条是 AssistantMessage，则移除。
func (cc *CurrentContext) RemoveLastAssistantEntry() {
	if len(cc.messages) == 0 {
		return
	}
	last := cc.messages[len(cc.messages)-1]
	if _, ok := last.Message.(apitypes.AssistantMessage); ok {
		cc.messages = cc.messages[:len(cc.messages)-1]
	}
}

// lastAssistantEntry 返回最后一条 assistant 的ChatResponse。
func (cc *CurrentContext) LastAssistantEntry() *apitypes.ChatResponse {
	for i := len(cc.messages) - 1; i >= 0; i-- {
		if _, ok := cc.messages[i].Message.(apitypes.AssistantMessage); ok {
			return cc.messages[i].Response
		}
	}
	return nil
}
