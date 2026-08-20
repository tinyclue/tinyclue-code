package types

// BeforeToolCallAction 工具执行前的确认操作类型。
type BeforeToolCallAction string

const (
	BeforeToolCallAllow BeforeToolCallAction = "allow"
	BeforeToolCallDeny  BeforeToolCallAction = "deny"
	BeforeToolCallAsk   BeforeToolCallAction = "ask"
)

// BeforeToolCallResult 是 BeforeToolCall 的返回值，决定工具是否继续执行。
type BeforeToolCallResult struct {
	Action       BeforeToolCallAction
	Message      string         // 展示给用户的说明文字
	ModifiedArgs map[string]any // Action==allow 时可携带修改后的参数
	Data         map[string]any // Action==ask 时透传给权限面板的展示数据（如 ExitPlanMode 的 plan 内容）
	Response     PermissionResponse
}

// PermissionRequest 是从 agent 发送到 TUI 的权限请求。
type PermissionRequest struct {
	ToolName   string
	ToolCallID string
	Args       map[string]any
	Message    string
	// Data 是面板展示数据，由 BeforeToolCallResult.Data 在 ask 时透传（如 ExitPlanMode 的 plan）。
	Data map[string]any
	// ResponseCh 用于将用户的选择异步送回给等待的 agent goroutine。
	ResponseCh chan<- PermissionResponse
}

// PermissionResponse 是用户在 TUI 中的确认结果。
type PermissionResponse struct {
	Action       BeforeToolCallAction
	ModifiedArgs map[string]any
	// Feedback 是用户输入的文本。拒绝时作为拒绝原因，经 RejectMessageWithReasonPrefix 注入
	// 拒绝消息；批准时作为 acceptFeedback，追加到 tool_result 的 ContentBlock 数组末尾
	Feedback string
}
