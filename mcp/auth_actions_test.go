package mcp

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// saveTestToken 在隔离 home 里为 server 预存有效 token（模拟重启静默复用路径）。
func saveTestToken(t *testing.T, home, server, baseURL string) {
	t.Helper()
	cfg := &oauth2.Config{
		ClientID: fakeClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  baseURL + "/authorize",
			TokenURL: baseURL + "/token",
		},
	}
	if err := (oauth.NewTokenStore("mcp-auth", server)).Save(cfg, &oauth2.Token{AccessToken: fakeAccessToken}); err != nil {
		t.Fatalf("save token: %v", err)
	}
}

// TestTokenStoreDelete 验证 delete：save → delete → load 报错；二次 delete 幂等；未保存 name 返回 nil。
func TestTokenStoreDelete(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	st := oauth.NewTokenStore("mcp-auth", "srv")
	cfg := &oauth2.Config{ClientID: "cid", Endpoint: oauth2.Endpoint{AuthURL: "https://as/auth", TokenURL: "https://as/token"}}
	if err := st.Save(cfg, &oauth2.Token{AccessToken: "at"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := st.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Load(); err == nil {
		t.Fatal("load after delete should error")
	}
	// 二次 delete 幂等。
	if err := st.Delete(); err != nil {
		t.Fatalf("second delete: %v", err)
	}
	// 未保存 name delete 返回 nil。
	if err := (oauth.NewTokenStore("mcp-auth", "never-saved")).Delete(); err != nil {
		t.Fatalf("delete unsaved: %v", err)
	}
}

// TestActionString 验证新增动作的 String()。
func TestActionString(t *testing.T) {
	cases := map[Action]string{
		ActionReconnect:    "reconnect",
		ActionDisable:      "disable",
		ActionEnable:       "enable",
		ActionAuthenticate: "authenticate",
		ActionCancelAuth:   "cancel",
		ActionClearAuth:    "clear-auth",
		ActionReauth:       "reauth",
	}
	for a, want := range cases {
		if got := a.String(); got != want {
			t.Errorf("Action(%d).String() = %q, want %q", a, got, want)
		}
	}
}

// TestManagerHasSavedToken 验证 token 存在性查询（save 前 false / save 后 true / delete 后 false）。
func TestManagerHasSavedToken(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	m := NewManager()
	if m.HasSavedToken("srv") {
		t.Fatal("HasSavedToken = true before save")
	}
	cfg := &oauth2.Config{ClientID: "cid", Endpoint: oauth2.Endpoint{AuthURL: "https://as/auth", TokenURL: "https://as/token"}}
	if err := (oauth.NewTokenStore("mcp-auth", "srv")).Save(cfg, &oauth2.Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	if !m.HasSavedToken("srv") {
		t.Fatal("HasSavedToken = false after save")
	}
	if err := (oauth.NewTokenStore("mcp-auth", "srv")).Delete(); err != nil {
		t.Fatal(err)
	}
	if m.HasSavedToken("srv") {
		t.Fatal("HasSavedToken = true after delete")
	}
}

// TestManagerReconnectNoBrowser 验证 Reconnect 是纯重连：http 鉴权 server 预存 token（启动即 connected），
// Reconnect 成功且绝不开浏览器（oauth.BrowserOpener 计数为 0）。
func TestManagerReconnectNoBrowser(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})
	saveTestToken(t, home, "protected", fake.URL)

	var opens atomic.Int32
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error { opens.Add(1); return nil }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("startup protected = %+v, want connected", p)
	}

	tools, err := m.Reconnect("protected")
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__protected__echo" {
		t.Fatalf("reconnect tools = %v", tools)
	}
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("after reconnect protected = %+v, want connected", p)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("browser opened %d times, want 0", n)
	}
}

// TestManagerClearAuthentication 验证清认证：connected（预存 token）→ ClearAuthentication →
// token 文件消失、HasSavedToken false、不开浏览器、连接落回 needs-auth；重复调用幂等。
func TestManagerClearAuthentication(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})
	saveTestToken(t, home, "protected", fake.URL)

	var opens atomic.Int32
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error { opens.Add(1); return nil }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("startup protected = %+v, want connected", p)
	}
	if !m.HasSavedToken("protected") {
		t.Fatal("HasSavedToken should be true after startup with saved token")
	}

	// ClearAuthentication：清 token + 非交互重连（不开浏览器）→ needs-auth。
	if err := m.ClearAuthentication("protected"); err != nil {
		t.Fatalf("clear auth: %v", err)
	}
	if m.HasSavedToken("protected") {
		t.Fatal("HasSavedToken should be false after clear")
	}
	if _, err := (oauth.NewTokenStore("mcp-auth", "protected")).Load(); err == nil {
		t.Fatal("token file should be gone after clear")
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("browser opened %d times, want 0", n)
	}
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("after clear protected = %+v, want needs-auth", p)
	}

	// 重复调用幂等。
	if err := m.ClearAuthentication("protected"); err != nil {
		t.Fatalf("second clear auth: %v", err)
	}
}

// TestManagerReauthenticate 验证重授权：connected（预存 token）→ Reauthenticate →
// 清 token + 交互重连（开一次浏览器）→ connected，token 重写盘。
func TestManagerReauthenticate(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})
	saveTestToken(t, home, "protected", fake.URL)

	var opens atomic.Int32
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error { opens.Add(1); return fakeBrowser(u) }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("startup protected = %+v, want connected", p)
	}

	tools, err := m.Reauthenticate("protected")
	if err != nil {
		t.Fatalf("reauth: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__protected__echo" {
		t.Fatalf("reauth tools = %v", tools)
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("browser opened %d times, want 1", n)
	}
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("after reauth protected = %+v, want connected", p)
	}
	if !m.HasSavedToken("protected") {
		t.Fatal("token should be re-saved after reauth")
	}
}

// TestManagerServerTools 验证 ServerTools：connected 返回工具；needs-auth / disabled / 未知名报错。
func TestManagerServerTools(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// connected http：返回工具。
	fake := newFakeAuthServer(t, false)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"httpd": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})
	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	tools, err := m.ServerTools("httpd")
	if err != nil {
		t.Fatalf("ServerTools connected: %v", err)
	}
	if len(tools) != 1 || tools[0].Name() != "mcp__httpd__echo" {
		t.Fatalf("ServerTools = %v", tools)
	}
	// disabled 报错。
	m.Disable("httpd")
	if _, err := m.ServerTools("httpd"); err == nil {
		t.Fatal("ServerTools disabled should error")
	}
	// 未知名报错。
	if _, err := m.ServerTools("nope"); err == nil {
		t.Fatal("ServerTools unknown should error")
	}
	m.Close()

	// needs-auth http：报错。
	fake2 := newFakeAuthServer(t, true)
	home2 := newMCPTestHome(t)
	writeHomeMCPConfig(t, home2, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake2.URL + "/mcp"},
	})
	m2 := NewManager()
	m2.Init(ctx)
	defer m2.Close()

	if p, ok := summaryByName(m2)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("startup protected = %+v, want needs-auth", p)
	}
	if _, err := m2.ServerTools("protected"); err == nil {
		t.Fatal("ServerTools needs-auth should error")
	}
}
