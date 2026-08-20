package types

import (
	"encoding/json"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// TokenEventBytes 从流式事件中提取应计入 token 估算的字节数（主 agent 与子 agent 共用）。
// 对齐参考项目 handleMessageFromStream 口径：text_delta / thinking_delta / 工具参数。
// TokenCounter 是唯一消费方；只返回字节数，不返回原始文本，避免内容混入主对话显示。
func TokenEventBytes(eventType AgentEventType, message interface{}) (int, bool) {
	switch eventType {
	case AssistantTextUpdate, AssistantThinkingUpdate:
		if delta, ok := message.(string); ok {
			return len(delta), true
		}
	case ToolExecutionStart:
		if tc, ok := message.(apitypes.ToolCall); ok {
			if raw, err := json.Marshal(tc.Arguments); err == nil {
				return len(raw), true
			}
		}
	}
	return 0, false
}
