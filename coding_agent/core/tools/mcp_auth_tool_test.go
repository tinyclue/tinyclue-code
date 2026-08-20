package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func TestMcpAuthTool(t *testing.T) {
	var authed string
	at := NewMcpAuthTool("my srv", func(server string) error {
		authed = server
		return nil
	})

	// 名字 normalize + 前缀一致（与 BridgeTool 替换语义兼容）。
	if got, want := at.Name(), "mcp__my_srv__authenticate"; got != want {
		t.Fatalf("name = %q, want %q", got, want)
	}
	if at.IsDeferredTool() {
		t.Fatal("auth tool should be always visible (not deferred)")
	}

	tool := at.GetTool()
	if tool.Name != at.Name() {
		t.Fatalf("GetTool.Name = %q, want %q", tool.Name, at.Name())
	}
	if !strings.Contains(tool.Description, "requires authentication") {
		t.Fatalf("GetTool.Description = %q, want mention of authentication", tool.Description)
	}
	// 无参数工具必须给出合法空 schema：零值 Parameters 会序列化出 required/properties:null，
	// OpenAI 兼容端点拒绝 required:null（"null is not of type array" 400）。
	if tool.Parameters.Type != "object" || tool.Parameters.Required == nil || tool.Parameters.Properties == nil {
		t.Fatalf("GetTool.Parameters = %+v, want Type=object with non-nil Required/Properties", tool.Parameters)
	}
	if b, err := json.Marshal(tool.Parameters); err != nil || strings.Contains(string(b), "null") {
		t.Fatalf("GetTool.Parameters json = %s (err=%v), want no null fields", b, err)
	}

	// Execute：调用注入的 authFn（原始 server 名）并返回"Started the OAuth flow"文案。
	tuc := core_types.ToolUseContext{ToolCall: apitypes.ToolCall{ID: "t1", Name: at.Name()}}
	tc, err := at.Execute(context.Background(), tuc)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if authed != "my srv" {
		t.Fatalf("authFn server = %q, want raw 'my srv'", authed)
	}
	text, ok := tc.Content.(core_types.TextContent)
	if !ok || !strings.Contains(text.Text, "Started the OAuth flow") {
		t.Fatalf("result = %+v, want 'Started the OAuth flow…' text", tc.Content)
	}

	// authFn 返回错误 → Execute 报错。
	bad := NewMcpAuthTool("srv", func(string) error { return errors.New("boom") })
	if _, err := bad.Execute(context.Background(), tuc); err == nil {
		t.Fatal("execute with failing authFn should error")
	}

	// 未注入 authFn → Execute 报错。
	if _, err := NewMcpAuthTool("srv", nil).Execute(context.Background(), tuc); err == nil {
		t.Fatal("execute without authFn should error")
	}
}
