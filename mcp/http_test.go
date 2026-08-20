package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// newMCPTestHome 创建隔离的 home（$TINYCLUE_CONFIG_DIR）并把进程 cwd 切到临时目录：
// 配置加载只看到本测试写入的 mcp.json（项目级 .mcp.json 从临时空目录向上找不到），
// 避免耦合仓库根的真实 .mcp.json（playwright 等 stdio server 会让测试变慢/不稳定）。
func newMCPTestHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYCLUE_CONFIG_DIR", home)
	t.Chdir(t.TempDir())
	return home
}

// writeHomeMCPConfig 在临时 home（$TINYCLUE_CONFIG_DIR）写用户级 mcp.json。
func writeHomeMCPConfig(t *testing.T, home string, servers map[string]any) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// summaryByName 把 Manager 摘要转成按名字索引的 map（项目级 .mcp.json 可能额外加载其他 server，
// 断言只针对本测试配置的 server，与既有 manager_test.go 的模式一致）。
func summaryByName(m *Manager) map[string]ServerSummary {
	byName := map[string]ServerSummary{}
	for _, s := range m.Summary() {
		byName[s.Name] = s
	}
	return byName
}

// hasServerTool 报告 m 的工具里是否含 <server> 的桥接工具（mcp__<server>__<tool>）。
func hasServerTool(m *Manager, server, tool string) bool {
	want := "mcp__" + server + "__" + tool
	for _, t := range m.Tools() {
		if t.Name() == want {
			return true
		}
	}
	return false
}

// TestManagerHTTPE2E 无鉴权 http server 端到端：连接 → 工具可见 → 调用往返 → Reconnect。
func TestManagerHTTPE2E(t *testing.T) {
	fake := newFakeAuthServer(t, false)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"httpd": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	// 1. 工具可见。
	names := map[string]bool{}
	for _, tool := range m.Tools() {
		names[tool.Name()] = true
	}
	if !names["mcp__httpd__echo"] {
		t.Fatalf("tools = %v, missing mcp__httpd__echo", names)
	}

	// 2. echo 往返。
	res, err := m.CallTool(ctx, "httpd", "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if res.Content != "hi" {
		t.Fatalf("echo = %q, want hi", res.Content)
	}

	// 3. 状态 connected。
	httpd, ok := summaryByName(m)["httpd"]
	if !ok || httpd.Status != StatusConnected || httpd.ToolCount != 1 {
		t.Fatalf("httpd summary = %+v", httpd)
	}

	// 4. Reconnect 正常。
	tools, err := m.Reconnect("httpd")
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__httpd__echo" {
		t.Fatalf("reconnect tools = %v", tools)
	}
}

// TestManagerServerNeedsAuthNotification 验证每次进入 needs-auth 都触发 ServerNeedsAuth
// （供 McpAuthTool 注册并回显"去 /mcp 授权"提示）。
func TestManagerServerNeedsAuthNotification(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	ch := make(chan string, 1)
	m.SetEvents(func(et McpEventType, d McpEventContext) {
		if et == McpNeedsAuth {
			ch <- d.Name
		}
	})
	m.Init(ctx)
	defer m.Close()

	select {
	case name := <-ch:
		if name != "protected" {
			t.Fatalf("ServerNeedsAuth = %q, want protected", name)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no onServerNeedsAuth notification")
	}
}

// TestManagerPlain401NotNeedsAuth 验证：无 WWW-Authenticate（非 Bearer 挑战）的裸 401 服务器
// 被当作普通连接错误（StatusError），而非 needs-auth——needs-auth 判定仅限 Bearer 挑战，
// 避免把非 OAuth 服务器误判为 needs-auth。
func TestManagerPlain401NotNeedsAuth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied", http.StatusUnauthorized) // 无 WWW-Authenticate
	}))
	t.Cleanup(ts.Close)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"plain": map[string]any{"type": "http", "url": ts.URL + "/mcp"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	p, ok := summaryByName(m)["plain"]
	if !ok || p.Status != StatusError {
		t.Fatalf("plain summary = %+v, want StatusError", p)
	}
}

// TestManagerHTTPNeedsAuth 启动连接遇 401：StatusNeedsAuth、无工具、调用报错
// （needs-auth 事件订阅行为由 TestManagerServerNeedsAuthNotification 单独覆盖）。
func TestManagerHTTPNeedsAuth(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	if hasServerTool(m, "protected", "echo") {
		t.Fatal("needs-auth server should expose no tools")
	}
	protected, ok := summaryByName(m)["protected"]
	if !ok || protected.Status != StatusNeedsAuth {
		t.Fatalf("protected summary = %+v, want StatusNeedsAuth", protected)
	}
	if _, err := m.CallTool(ctx, "protected", "echo", map[string]any{"msg": "x"}); err == nil {
		t.Fatal("call on needs-auth server should error")
	}
}

// TestManagerHTTPInteractiveAuth 交互授权全链路：启动 needs-auth → [a] 授权 →
// connected + 工具 + token 落盘 → 重启静默复用（免授权）。
func TestManagerHTTPInteractiveAuth(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)

	// 1. 启动连接：needs-auth。
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("startup protected = %+v, want needs-auth", p)
	}

	// 2. 交互授权：connected + 工具可见 + 调用往返。
	tools, err := m.Authenticate("protected")
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__protected__echo" {
		t.Fatalf("tools = %v", tools)
	}
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("after auth protected = %+v, want connected", p)
	}
	res, err := m.CallTool(ctx, "protected", "echo", map[string]any{"msg": "authed"})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if res.Content != "authed" {
		t.Fatalf("echo = %q, want authed", res.Content)
	}

	// 3. token 已落盘。
	pa, err := oauth.NewTokenStore("mcp-auth", "protected").Load()
	if err != nil {
		t.Fatalf("load token: %v", err)
	}
	if pa.Token.AccessToken != fakeAccessToken {
		t.Fatalf("persisted token = %q, want %q", pa.Token.AccessToken, fakeAccessToken)
	}
	m.Close()

	// 4. 重启静默复用：新 Manager 直接 connected，无 needs-auth 事件（自然也无回显）。
	m2 := NewManager()
	var needsAuth []string
	m2.SetEvents(func(et McpEventType, d McpEventContext) {
		if et == McpNeedsAuth {
			needsAuth = append(needsAuth, d.Name)
		}
	})
	m2.Init(ctx)
	defer m2.Close()

	if p, ok := summaryByName(m2)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("restart protected = %+v, want connected", p)
	}
	if len(needsAuth) != 0 {
		t.Fatalf("restart McpNeedsAuth = %v, want none", needsAuth)
	}
	res, err = m2.CallTool(ctx, "protected", "echo", map[string]any{"msg": "again"})
	if err != nil {
		t.Fatalf("restart echo: %v", err)
	}
	if res.Content != "again" {
		t.Fatalf("restart echo = %q, want again", res.Content)
	}
}

// TestManagerConcurrentAuthSingleBrowser 验证并发交互授权（模型工具 + /mcp 面板 [a] 同时触发）
// 只开一个浏览器：第二个 Authenticate 返回"授权已在进行中"，浏览器 opener 仅被调用一次。
func TestManagerConcurrentAuthSingleBrowser(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	var opens atomic.Int32
	opened := make(chan struct{}, 1)
	release := make(chan struct{})
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error {
		if opens.Add(1) == 1 {
			opened <- struct{}{}
		}
		<-release // 模拟浏览器授权等待：首个授权阻塞在此
		return nil
	}
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)

	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("startup protected = %+v, want needs-auth", p)
	}

	// 首个授权在 goroutine 中发起，阻塞在浏览器打开（此时 authing 已置位）。
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = m.Authenticate("protected")
	}()
	select {
	case <-opened:
	case <-time.After(10 * time.Second):
		t.Fatal("first auth never opened browser")
	}

	// 第二个并发授权：立即返回"已在进行中"，不重复打开浏览器。
	if _, err := m.Authenticate("protected"); err == nil || !strings.Contains(err.Error(), "authorization already in progress") {
		t.Fatalf("second auth err = %v, want authorization already in progress", err)
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("browser opened %d times, want 1", n)
	}

	// 释放首个授权并关闭 manager：ctx 取消令回调等待结束，goroutine 退出。
	close(release)
	m.Close()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("first auth goroutine did not exit after close")
	}
}

// TestManagerCancelAuth 验证进行中的交互授权可被中止（/mcp busy 时 [c]）：浏览器打开、回调等待中
// CancelAuth → authCtx 取消 → cb.wait 立即返回 → server 回 needs-auth（非 Error，伪工具仍在），
// Authenticate 返回 ErrAuthCancelled。
func TestManagerCancelAuth(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	var opens atomic.Int32
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error { opens.Add(1); return nil }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	authStarted := make(chan string, 1)
	m.SetEvents(func(et McpEventType, d McpEventContext) {
		if et == McpAuthStarted {
			authStarted <- d.Name
		}
	})
	m.Init(ctx)

	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("startup protected = %+v, want needs-auth", p)
	}

	// 发起交互授权：浏览器打开后回调等待阻塞（cb.wait）。
	done := make(chan error, 1)
	go func() {
		_, err := m.Authenticate("protected")
		done <- err
	}()
	select {
	case name := <-authStarted:
		if name != "protected" {
			t.Fatalf("authStarted = %q, want protected", name)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("auth never started")
	}

	// 取消：应中止回调等待并回到 needs-auth，Authenticate 返回 ErrAuthCancelled。
	if err := m.CancelAuth("protected"); err != nil {
		t.Fatalf("cancel auth: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrAuthCancelled) {
			t.Fatalf("Authenticate err = %v, want ErrAuthCancelled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Authenticate did not return after cancel")
	}
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("after cancel protected = %+v, want needs-auth", p)
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("browser opened %d times, want 1", n)
	}
	m.Close()
}
