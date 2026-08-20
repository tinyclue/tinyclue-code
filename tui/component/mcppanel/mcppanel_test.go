package mcppanel

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/mcp"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// testItems 构造三态 server 列表：connected http(2 tools) / needs-auth http / disabled stdio。
func testItems() []mcp.ServerSummary {
	return []mcp.ServerSummary{
		{Name: "httpd", Status: mcp.StatusConnected, ToolCount: 2, HTTP: true},
		{Name: "protected", Status: mcp.StatusNeedsAuth, HTTP: true},
		{Name: "toy", Status: mcp.StatusDisabled, HTTP: false},
	}
}

// renderAll 渲染面板并拼成单字符串（断言用）。
func renderAll(p *McpPanel) string {
	return strings.Join(p.Render(core.Data{}).Lines, "\n")
}

// press 发送合成按键（绕过首帧 ready 守卫）。
func press(p *McpPanel, code rune) {
	p.ready = true
	p.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: code}))})
}

func TestRenderList(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())

	out := renderAll(p)
	for _, want := range []string{
		"MCP servers:",
		"httpd",
		"● connected (2 tools)",
		"protected",
		"⚠ needs auth",
		"toy",
		"— disabled",
		"↑↓ navigate · Enter details · Esc exit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("list render missing %q:\n%s", want, out)
		}
	}
	// needs-auth 状态行不再提示 [a] authenticate（已改为 Enter 下钻）。
	if strings.Contains(out, "[a]") {
		t.Errorf("list render should not reference [a]:\n%s", out)
	}
}

func TestRenderMenuConnectedHTTPWithToken(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())
	p.SetAuthState(func(server string) bool { return server == "httpd" })
	p.openMenu(p.items[0]) // httpd connected http + token

	out := renderAll(p)
	for _, want := range []string{
		"httpd",
		"● connected (2 tools)",
		"View tools",
		"Re-authenticate",
		"Clear authentication",
		"Reconnect",
		"Disable",
		"Back",
		"↑↓ navigate · Enter select · Esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("connected-menu missing %q:\n%s", want, out)
		}
	}
}

func TestRenderMenuNeedsAuthNoToken(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())
	p.openMenu(p.items[1]) // protected needs-auth, 无 token

	out := renderAll(p)
	for _, want := range []string{"⚠ needs auth", "Authenticate", "Back"} {
		if !strings.Contains(out, want) {
			t.Errorf("needs-auth-no-token menu missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"Re-authenticate", "Clear authentication", "Reconnect", "Disable", "View tools"} {
		if strings.Contains(out, absent) {
			t.Errorf("needs-auth-no-token menu should not contain %q:\n%s", absent, out)
		}
	}
}

func TestRenderMenuNeedsAuthWithToken(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())
	p.SetAuthState(func(server string) bool { return server == "protected" })
	p.openMenu(p.items[1]) // protected needs-auth + token

	out := renderAll(p)
	for _, want := range []string{"Authenticate", "Clear authentication", "Back"} {
		if !strings.Contains(out, want) {
			t.Errorf("needs-auth-with-token menu missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"Re-authenticate", "Reconnect", "Disable", "View tools"} {
		if strings.Contains(out, absent) {
			t.Errorf("needs-auth-with-token menu should not contain %q:\n%s", absent, out)
		}
	}
}

func TestRenderMenuDisabledStdio(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())
	p.openMenu(p.items[2]) // toy disabled stdio

	out := renderAll(p)
	for _, want := range []string{"Enable", "Back"} {
		if !strings.Contains(out, want) {
			t.Errorf("disabled menu missing %q:\n%s", want, out)
		}
	}
	for _, absent := range []string{"Reconnect", "Disable", "View tools", "Authenticate"} {
		if strings.Contains(out, absent) {
			t.Errorf("disabled menu should not contain %q:\n%s", absent, out)
		}
	}
}

func TestRenderToolsAndDetail(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(80)
	schema := apitypes.Parameters{
		Type: "object",
		Properties: map[string]apitypes.SchemaItem{
			"msg": {Type: "string", Description: "message to echo"},
		},
		Required: []string{"msg"},
	}
	bt := core_tools.NewMCPBridgeTool("httpd", "echo", "Echoes back the message.", schema)
	p := New([]mcp.ServerSummary{{Name: "httpd", Status: mcp.StatusConnected, ToolCount: 1, HTTP: true}})
	p.toolServer = "httpd"
	p.tools = []core_types.AgentToolApi{bt}
	p.toolsSel = 0
	p.v = viewTools

	// 工具列表：去前缀显示名 + 描述。
	out := renderAll(p)
	for _, want := range []string{
		"httpd tools:",
		"echo — Echoes back the message.",
		"↑↓ navigate · Enter details · Esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tools render missing %q:\n%s", want, out)
		}
	}

	// 下钻详情：name / 描述 / input schema / 属性 / required。
	press(p, uv.KeyEnter)
	out = renderAll(p)
	for _, want := range []string{
		"httpd / echo",
		"Echoes back the message.",
		"input schema:",
		"type: object",
		"required: msg",
		"msg (string) [required]",
		"message to echo",
		"Esc back",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderEmptyList(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(nil)

	out := renderAll(p)
	for _, want := range []string{
		"no MCP servers configured",
		"[Enter/Esc to exit]",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("empty list missing %q:\n%s", want, out)
		}
	}
}

func TestBusyMenuRendersVerbAndCancelHint(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)

	// Authenticate 在途：busy 项显示浏览器 verb + [c] 取消提示。
	p := New(testItems())
	p.OnAction(func(string, mcp.Action) {})
	p.openMenu(p.items[1]) // protected needs-auth
	p.fire(mcp.ActionAuthenticate)

	out := renderAll(p)
	for _, want := range []string{"… opening browser for authorization", "[c] cancel authorization"} {
		if !strings.Contains(out, want) {
			t.Errorf("busy authenticate missing %q:\n%s", want, out)
		}
	}

	// Reconnect 在途：显示 "reconnecting"，且绝无 [c] 提示（Reconnect 是纯重连，不可取消）。
	p2 := New(testItems())
	p2.OnAction(func(string, mcp.Action) {})
	p2.openMenu(p2.items[0]) // httpd connected
	p2.fire(mcp.ActionReconnect)

	out2 := renderAll(p2)
	if !strings.Contains(out2, "reconnecting") {
		t.Errorf("busy reconnect missing reconnecting:\n%s", out2)
	}
	if strings.Contains(out2, "[c]") {
		t.Errorf("busy reconnect should not show [c] cancel hint:\n%s", out2)
	}
}

// TestBusyEnterDoesNotClosePanel 回归：busy（动作在途）时 Enter 是激活键、已被用于触发在途动作，
// 再按 Enter 不得关闭面板；仅 Esc 退出（动作在后台继续，结果回显到 chat）。
func TestBusyEnterDoesNotClosePanel(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New(testItems())
	closed := false
	p.OnCancel(func() { closed = true })
	p.OnAction(func(string, mcp.Action) {})
	p.openMenu(p.items[0]) // httpd connected
	p.fire(mcp.ActionReconnect)

	// busy 时 Enter 忽略：面板保持打开。
	press(p, uv.KeyEnter)
	if closed {
		t.Fatal("Enter during busy should not close the panel")
	}
	if !p.busy {
		t.Fatal("panel should still be busy after Enter during action")
	}

	// busy 时 Esc 退出：动作在后台继续。
	press(p, uv.KeyEsc)
	if !closed {
		t.Fatal("Esc during busy should close the panel")
	}
}

func TestRefreshRebuildsMenu(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New([]mcp.ServerSummary{{Name: "httpd", Status: mcp.StatusConnected, ToolCount: 1, HTTP: true}})
	p.SetAuthState(func(server string) bool { return true })
	p.SetReload(func() []mcp.ServerSummary {
		return []mcp.ServerSummary{{Name: "httpd", Status: mcp.StatusNeedsAuth, HTTP: true}}
	})
	p.OnAction(func(string, mcp.Action) {})
	p.openMenu(p.items[0])

	if out := renderAll(p); !strings.Contains(out, "Reconnect") {
		t.Fatalf("connected menu should have Reconnect:\n%s", out)
	}

	// fire Reconnect → 后台动作完成经 McpRefreshMsg 回传（reload 返回 needs-auth）→ 菜单重建。
	p.fire(mcp.ActionReconnect)
	p.DoUpdate(core.Data{Msg: core.McpRefreshMsg{}})

	out := renderAll(p)
	for _, want := range []string{"Authenticate", "Clear authentication"} {
		if !strings.Contains(out, want) {
			t.Errorf("rebuilt menu missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Reconnect") {
		t.Errorf("needs-auth menu should not have Reconnect after refresh:\n%s", out)
	}
}
