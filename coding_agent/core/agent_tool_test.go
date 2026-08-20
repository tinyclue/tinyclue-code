package core

import (
	"context"
	"reflect"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// fakeTool 最小 AgentToolApi 实现，便于测试运行时增删（/mcp 面板路径）。
type fakeTool struct {
	name     string
	deferred bool
}

func (f *fakeTool) Name() string { return f.name }

func (f *fakeTool) GetTool() apitypes.Tool { return apitypes.Tool{Name: f.name} }

func (f *fakeTool) BeforeToolCall(_ context.Context, _ types.ToolUseContext) types.BeforeToolCallResult {
	return types.BeforeToolCallResult{Action: types.BeforeToolCallAllow}
}

func (f *fakeTool) ValidateInput(_ context.Context, _ types.ToolUseContext) types.ValidateContent {
	return types.ValidateContent{Result: true}
}

func (f *fakeTool) Execute(_ context.Context, _ types.ToolUseContext) (types.ToolContext, error) {
	return types.ToolContext{}, nil
}

func (f *fakeTool) BuildToolResult(types.ToolContext) apitypes.ToolResultMessage {
	return apitypes.ToolResultMessage{}
}

func (f *fakeTool) IsDeferredTool() bool { return f.deferred }

func (f *fakeTool) GetSearchHint() string { return "" }

// enabledNames 取 GetEnableTools 当前可见工具名（按注册顺序）。
// 注意用非 nil 空切片：reflect.DeepEqual(nil, []string{}) 为 false，空集断言会误报。
func enabledNames(at *AgentTool) []string {
	names := []string{}
	for _, tool := range at.GetEnableTools() {
		names = append(names, tool.Name)
	}
	return names
}

func TestAgentToolSyncServerTools(t *testing.T) {
	a := &fakeTool{name: "a"}
	b := &fakeTool{name: "b"}
	at := NewAgentTool(context.Background(), &types.AgentRuntimeContext{AgentId: "test"})
	at.InitWithTools(nil, nil)

	// 初始同步：server toy 暴露 [a b]。
	at.SyncServerTools("toy", []types.AgentToolApi{a, b})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("after first sync = %v, want [a b]", got)
	}

	// 重复同步同集：幂等。
	at.SyncServerTools("toy", []types.AgentToolApi{a, b})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("after idempotent sync = %v, want [a b]", got)
	}

	// server 移除 b：b 陈旧隐藏，a 保留。
	at.SyncServerTools("toy", []types.AgentToolApi{a})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("after prune = %v, want [a]", got)
	}
	if _, ok := at.FindToolByName("b"); ok {
		t.Fatal("FindToolByName(b) should be false after prune")
	}

	// b 回归：重新可见。
	at.SyncServerTools("toy", []types.AgentToolApi{a, b})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("after restore = %v, want [a b]", got)
	}
	if _, ok := at.FindToolByName("b"); !ok {
		t.Fatal("FindToolByName(b) should be true after restore")
	}

	// 禁用 server：全部隐藏。
	at.SyncServerTools("toy", nil)
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{}) {
		t.Fatalf("after disable = %v, want []", got)
	}
	if _, ok := at.FindToolByName("a"); ok {
		t.Fatal("FindToolByName(a) should be false after disable")
	}
}

func TestAgentToolSyncServerToolsIsolation(t *testing.T) {
	a := &fakeTool{name: "a"}
	b := &fakeTool{name: "b"}
	at := NewAgentTool(context.Background(), &types.AgentRuntimeContext{AgentId: "test"})
	at.InitWithTools(nil, nil)

	at.SyncServerTools("s1", []types.AgentToolApi{a})
	at.SyncServerTools("s2", []types.AgentToolApi{b})
	// 禁用 s1 不影响 s2 的工具。
	at.SyncServerTools("s1", nil)
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("after disable s1 = %v, want [b]", got)
	}
	// 启用 s1 恢复 a。
	at.SyncServerTools("s1", []types.AgentToolApi{a})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("after enable s1 = %v, want [a b]", got)
	}
}

func TestAgentToolSyncServerToolsDeferred(t *testing.T) {
	a := &fakeTool{name: "a"}
	d := &fakeTool{name: "d", deferred: true}
	at := NewAgentTool(context.Background(), &types.AgentRuntimeContext{AgentId: "test"})
	at.InitWithTools([]types.AgentToolApi{a}, nil)

	// deferred 工具经同步注册，但未发现前不对外可见。
	at.SyncServerTools("toy", []types.AgentToolApi{d})
	if got := at.GetDeferredToolNames(); !reflect.DeepEqual(got, []string{"d"}) {
		t.Fatalf("deferred = %v, want [d]", got)
	}
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("enabled = %v, want [a] (deferred not discovered)", got)
	}
	// 发现后可见（模拟 ProcessToolRef 发现的 deferred 名）。
	at.AddDiscoveredToolNames([]string{"d"})
	if got := enabledNames(at); !reflect.DeepEqual(got, []string{"a", "d"}) {
		t.Fatalf("enabled after discovery = %v, want [a d]", got)
	}
}
