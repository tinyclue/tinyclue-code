package core

import (
	"context"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// mockSink 是 eventSink 的测试桩：记录所有事件，并在 ToolPermissionRequired 事件
// 携带 PermissionRequest 且设置了 reply 时，异步回写用户确认结果。
type mockSink struct {
	events []core_types.AgentEventType
	msgs   []interface{}
	errs   []error
	reply  *core_types.PermissionResponse
}

func (m *mockSink) emit(eventType core_types.AgentEventType, message interface{}, err error) {
	m.events = append(m.events, eventType)
	m.msgs = append(m.msgs, message)
	m.errs = append(m.errs, err)

	if eventType == core_types.ToolPermissionRequired && m.reply != nil {
		if req, ok := message.(core_types.PermissionRequest); ok {
			ch := req.ResponseCh
			reply := *m.reply
			go func() { ch <- reply }()
		}
	}
}

func newTestLoop(sink *mockSink) *AgentLoop {
	return &AgentLoop{eventSink: core_types.EventSink(sink.emit)}
}

func TestRejectMessage(t *testing.T) {
	if got := rejectMessage(""); got != core_types.RejectMessage {
		t.Errorf("rejectMessage(\"\") = %q, want RejectMessage", got)
	}
	want := core_types.RejectMessageWithReasonPrefix + "refine the plan"
	if got := rejectMessage("refine the plan"); got != want {
		t.Errorf("rejectMessage(feedback) = %q, want %q", got, want)
	}
}

// newTestRuntimeContext 构造仅含单个工具详情的 ToolRuntimeContext，供 handler 测试使用。
func newTestRuntimeContext(tool apitypes.ToolCall) *core_types.ToolRuntimeContext {
	return &core_types.ToolRuntimeContext{
		Ctx: context.Background(),
		ToolsDetail: []*core_types.ToolRuntimeDetail{
			{ID: tool.ID, Tool: tool},
		},
	}
}

// TestFailToolResult 验证统一失败结果构造：IsError、文本取 errMsg、写入 ToolContext/Err/ToolResult。
// ToolExecutionEnd 事件发射已上移到 runTools 循环（runToolItem 返回后按 done/err 发送），此处不覆盖。
func TestFailToolResult(t *testing.T) {
	tool := apitypes.ToolCall{ID: "call_1", Name: "Bash"}
	runtimeContext := newTestRuntimeContext(tool)

	failToolResult(0, runtimeContext, "boom")
	detail := runtimeContext.ToolsDetail[0]

	if !detail.ToolResult.IsError {
		t.Error("expected IsError=true")
	}
	if detail.ToolResult.ToolCallId != "call_1" || detail.ToolResult.ToolName != "Bash" {
		t.Errorf("result identity = (%s, %s), want (call_1, Bash)", detail.ToolResult.ToolCallId, detail.ToolResult.ToolName)
	}
	if len(detail.ToolResult.Contents) != 1 || detail.ToolResult.Contents[0].Text != "boom" {
		t.Errorf("Contents = %v, want err text", detail.ToolResult.Contents)
	}
	if tc, ok := detail.ToolContext.Content.(core_types.TextContent); !ok || tc.Text != "boom" {
		t.Errorf("ToolContext.Content = %v, want TextContent boom", detail.ToolContext.Content)
	}
	if detail.Err == nil || detail.Err.Error() != "boom" {
		t.Errorf("Err = %v, want boom", detail.Err)
	}
}

// TestHandlePermissionAsk 验证权限弹窗的三种结果：允许（ModifiedArgs 随 response 返回）/ 拒绝 / 上下文取消。
// ModifiedArgs 的应用由 executeTool 负责（此前的 toolSlot 回写从未被 ExecuteTool 读到）。
func TestHandlePermissionAsk(t *testing.T) {
	tool := apitypes.ToolCall{ID: "call_1", Name: "ExitPlanMode", Arguments: map[string]any{"orig": "v"}}

	t.Run("allow carries modified args and feedback", func(t *testing.T) {
		sink := &mockSink{reply: &core_types.PermissionResponse{
			Action:       core_types.BeforeToolCallAllow,
			ModifiedArgs: map[string]any{"plan": "x"},
			Feedback:     "also update the README",
		}}
		al := newTestLoop(sink)
		signal := NewAgentSignal(context.Background())

		runtimeContext := newTestRuntimeContext(tool)
		resp := al.handlePermissionAsk(signal, 0, runtimeContext)
		if resp.Action != core_types.BeforeToolCallAllow {
			t.Errorf("Action = %v, want allow", resp.Action)
		}
		if resp.Feedback != "also update the README" {
			t.Errorf("Feedback = %q, want carried feedback", resp.Feedback)
		}
		if got := resp.ModifiedArgs["plan"]; got != "x" {
			t.Errorf("resp.ModifiedArgs[plan] = %v, want carried modified args", got)
		}
	})

	t.Run("deny returns deny response", func(t *testing.T) {
		sink := &mockSink{reply: &core_types.PermissionResponse{
			Action:   core_types.BeforeToolCallDeny,
			Feedback: "keep planning",
		}}
		al := newTestLoop(sink)
		signal := NewAgentSignal(context.Background())

		runtimeContext := newTestRuntimeContext(tool)
		resp := al.handlePermissionAsk(signal, 0, runtimeContext)
		if resp.Action != core_types.BeforeToolCallDeny {
			t.Errorf("Action = %v, want deny", resp.Action)
		}
		if resp.Feedback != "keep planning" {
			t.Errorf("Feedback = %q, want reason", resp.Feedback)
		}
	})

	t.Run("ctx cancellation falls back to deny", func(t *testing.T) {
		sink := &mockSink{} // 不设 reply：select 走 ctx Done() 分支
		al := newTestLoop(sink)
		signal := NewAgentSignal(context.Background())
		signal.Abort()

		runtimeContext := newTestRuntimeContext(tool)
		resp := al.handlePermissionAsk(signal, 0, runtimeContext)
		if resp.Action != core_types.BeforeToolCallDeny {
			t.Errorf("Action = %v, want deny on ctx cancel", resp.Action)
		}
	})
}
