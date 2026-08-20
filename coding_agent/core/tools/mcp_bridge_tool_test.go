package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// newTestBridgeTool 构造一个带 toy server 元数据的 BridgeTool，不注入 caller（各用例自行 SetCaller）。
func newTestBridgeTool() *BridgeTool {
	return NewMCPBridgeTool("toy", "echo", "echo a message", apitypes.Parameters{
		Type: "object",
		Properties: map[string]apitypes.SchemaItem{
			"msg": {Type: "string"},
		},
		Required: []string{"msg"},
	})
}

// fakeCaller 返回一个记录入参的 MCPToolCaller。
func fakeCaller(result string, isError bool, err error) (MCPToolCaller, *map[string]any) {
	var got map[string]any
	return func(_ context.Context, _, _ string, args map[string]any) (string, bool, error) {
		got = args
		return result, isError, err
	}, &got
}

func TestBridgeToolNameAndTool(t *testing.T) {
	bt := newTestBridgeTool()
	if got := bt.Name(); got != "mcp__toy__echo" {
		t.Fatalf("Name = %q, want mcp__toy__echo", got)
	}
	tool := bt.GetTool()
	if tool.Name != "mcp__toy__echo" || tool.Description != "echo a message" {
		t.Fatalf("GetTool = %+v", tool)
	}
	// 任意 MCP schema 无法满足 OpenAI strict（additionalProperties:false + 全 required），必须为 false。
	if tool.Strict {
		t.Fatal("GetTool.Strict must be false")
	}
	if tool.Parameters.Type != "object" {
		t.Fatalf("Parameters.Type = %q, want object", tool.Parameters.Type)
	}
}

func TestBridgeToolBeforeToolCallAsk(t *testing.T) {
	withTempProjectRoot(t) // 隔离真实项目 settings.local.json，保证无匹配规则 → Ask。
	bt := newTestBridgeTool()
	tuc := core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: bt.Name()},
	}
	res := bt.BeforeToolCall(context.Background(), tuc)
	if res.Action != core_types.BeforeToolCallAsk {
		t.Fatalf("Action = %v, want ask", res.Action)
	}
	if !strings.Contains(res.Message, "echo") || !strings.Contains(res.Message, "toy") {
		t.Fatalf("Message = %q, want mention tool and server", res.Message)
	}
}

func TestBridgeToolExecuteSuccess(t *testing.T) {
	bt := newTestBridgeTool()
	caller, got := fakeCaller("hello", false, nil)
	bt.SetCaller(caller)

	tuc := core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: bt.Name(), Arguments: map[string]any{"msg": "hi"}},
	}
	tc, err := bt.Execute(context.Background(), tuc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	text, ok := tc.Content.(core_types.TextContent)
	if !ok || text.Text != "hello" {
		t.Fatalf("Content = %+v, want hello", tc.Content)
	}
	if (*got)["msg"] != "hi" {
		t.Fatalf("args forwarded = %v", *got)
	}

	msg := bt.BuildToolResult(tc)
	if msg.ToolCallId != "call_1" || len(msg.Contents) != 1 || msg.Contents[0].Text != "hello" {
		t.Fatalf("BuildToolResult = %+v", msg)
	}
}

func TestBridgeToolExecuteIsError(t *testing.T) {
	bt := newTestBridgeTool()
	caller, _ := fakeCaller("boom", true, nil)
	bt.SetCaller(caller)
	tc, err := bt.Execute(context.Background(), core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: bt.Name()},
	})
	if err == nil {
		t.Fatal("Execute should error when server reports isError")
	}
	text, ok := tc.Content.(core_types.TextContent)
	if !ok || text.Text != "boom" {
		t.Fatalf("Content = %+v, want boom preserved", tc.Content)
	}
}

func TestBridgeToolExecuteTransportError(t *testing.T) {
	bt := newTestBridgeTool()
	caller, _ := fakeCaller("", false, errors.New("connection reset"))
	bt.SetCaller(caller)
	tc, err := bt.Execute(context.Background(), core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{ID: "call_1", Name: bt.Name()},
	})
	if err == nil {
		t.Fatal("Execute should error on transport failure")
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("err = %v, want to wrap caller error", err)
	}
	if text, ok := tc.Content.(core_types.TextContent); !ok || !strings.Contains(text.Text, "connection reset") {
		t.Fatalf("Content = %+v, want error surfaced", tc.Content)
	}
}
