package types

type MessageRole string

const (
	SystemRole    MessageRole = "system"
	UserRole      MessageRole = "user"
	AssistantRole MessageRole = "assistant"
	ToolRole      MessageRole = "tool"
)

type FinishReason string

const (
	FinishReasonStop    FinishReason = "stop"     // 正常结束
	FinishReasonLength  FinishReason = "length"   // 达到 max_tokens 上限
	FinishReasonToolUse FinishReason = "tool_use" // 调用工具
	FinishReasonError   FinishReason = "error"    // 服务端错误
	FinishReasonAborted FinishReason = "aborted"  // 被取消
)

type StopReason struct {
	FinishReason  FinishReason
	ReasonMessage string
}
