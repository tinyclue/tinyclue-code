package subscription

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// ── 假 OAuth 授权服务器 ──

// newFakeOAuthServer 起一个 httptest 服务器：/authorize 返回 302 跳到 redirect_uri 并附
// code/state/iss（浏览器模拟用），/token 返回 access+refresh。stateOverride 非空时 /authorize
// 用固定 state 代替回显（测 state 校验）；iss 缺省回显 ts.URL 自身（与 AuthURL 同源）。
func newFakeOAuthServer(t *testing.T, stateOverride string) *httptest.Server {
	t.Helper()
	// 先声明再赋值：handler 闭包引用 ts.URL，需保证 ts 已声明（一步 := 会让闭包在作用域之前）。
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			redirect := r.URL.Query().Get("redirect_uri")
			loc, err := url.Parse(redirect)
			if err != nil {
				http.Error(w, "bad redirect_uri", http.StatusBadRequest)
				return
			}
			q := loc.Query()
			q.Set("code", "e2e-code")
			state := r.URL.Query().Get("state")
			if stateOverride != "" {
				state = stateOverride
			}
			q.Set("state", state)
			q.Set("iss", ts.URL) // RFC 9207 issuer 回显
			loc.RawQuery = q.Encode()
			http.Redirect(w, r, loc.String(), http.StatusFound)
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"e2e-at","token_type":"Bearer","refresh_token":"e2e-rt"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

// fakeBrowser 模拟浏览器跟随 /authorize 的 302 跳回回调地址（避免真开浏览器）。
func fakeBrowser(u string) error {
	_, err := http.Get(u)
	return err
}

// ── 授权码流程 ──

func TestLoginE2E(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := newFakeOAuthServer(t, "")
	Register(ProviderConfig{
		Name:     "e2e",
		AuthURL:  ts.URL + "/authorize",
		TokenURL: ts.URL + "/token",
		ClientID: "e2e-client",
		Scopes:   []string{"offline_access"},
	})

	var gotURL string
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(u string) error { gotURL = u; return fakeBrowser(u) }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := Login(ctx, "e2e")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if tok.AccessToken != "e2e-at" {
		t.Fatalf("access token = %q, want e2e-at", tok.AccessToken)
	}
	if tok.RefreshToken != "e2e-rt" {
		t.Fatalf("refresh token = %q, want e2e-rt", tok.RefreshToken)
	}
	// 授权 URL 走 PKCE S256（带 code_challenge）且指向本地回调。
	u, err := url.Parse(gotURL)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if u.Query().Get("code_challenge") == "" {
		t.Fatal("authorize URL missing code_challenge (PKCE)")
	}
	if !strings.HasPrefix(u.Query().Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("redirect_uri = %q, want localhost loopback", u.Query().Get("redirect_uri"))
	}
}

func TestLoginAndSavePersists(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := newFakeOAuthServer(t, "")
	Register(ProviderConfig{Name: "persist", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token", ClientID: "c"})

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := LoginAndSave(ctx, "persist"); err != nil {
		t.Fatalf("LoginAndSave: %v", err)
	}
	pa, err := oauth.NewTokenStore(authStoreSubdir, "persist").Load()
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if pa.Token.AccessToken != "e2e-at" {
		t.Fatalf("persisted access token = %q, want e2e-at", pa.Token.AccessToken)
	}
	// 后续可直接取 access token（AccessToken 复用会话）。
	at, err := AccessToken(ctx, "persist")
	if err != nil || at != "e2e-at" {
		t.Fatalf("AccessToken = %q, %v; want e2e-at", at, err)
	}
}

func TestLoginStateMismatch(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := newFakeOAuthServer(t, "wrong-state") // 回调 state 与发出的不同
	Register(ProviderConfig{Name: "mismatch", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token", ClientID: "c"})

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Login(ctx, "mismatch"); err == nil {
		t.Fatal("Login should fail on state mismatch")
	}
}

func TestLoginClientIDEmpty(t *testing.T) {
	Register(ProviderConfig{Name: "reserved", AuthURL: "https://as/auth", TokenURL: "https://as/token", ClientID: ""})
	_, err := Login(context.Background(), "reserved")
	if err == nil {
		t.Fatal("Login with empty client id should error")
	}
	if !strings.Contains(err.Error(), "TINYCLUE_OAUTH_CLIENT_ID_RESERVED") {
		t.Fatalf("err = %v, want env var hint", err)
	}
}

func TestProviderEnvOverride(t *testing.T) {
	t.Setenv("TINYCLUE_OAUTH_CLIENT_ID_GOOGLE", "env-client")
	Register(ProviderConfig{Name: "google", AuthURL: "https://as/auth", TokenURL: "https://as/token", ClientID: ""})
	pc, ok := Provider("google")
	if !ok {
		t.Fatal("google should be registered")
	}
	if pc.ClientID != "env-client" {
		t.Fatalf("client id = %q, want env-client (env override)", pc.ClientID)
	}
	// 未注册厂商。
	if _, ok := Provider("nope"); ok {
		t.Fatal("Provider(nope) should be false")
	}
}

// TestLoginIssuerMatch 验证厂商配置 Issuer 且回调 iss 一致 → 登录成功。
func TestLoginIssuerMatch(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := newFakeOAuthServer(t, "")
	Register(ProviderConfig{
		Name: "iss-ok", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token",
		ClientID: "c", Issuer: ts.URL,
	})

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Login(ctx, "iss-ok"); err != nil {
		t.Fatalf("Login with matching issuer should succeed: %v", err)
	}
}

// TestLoginIssuerMismatch 验证回调 iss 与配置的 Issuer 不一致 → 登录失败。
func TestLoginIssuerMismatch(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := newFakeOAuthServer(t, "")
	Register(ProviderConfig{
		Name: "iss-bad", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token",
		ClientID: "c", Issuer: "https://evil.example", // 与回调回显的 ts.URL 不符
	})

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := Login(ctx, "iss-bad"); err == nil || !strings.Contains(err.Error(), "issuer mismatch") {
		t.Fatalf("Login err = %v, want issuer mismatch", err)
	}
}

// TestAccessTokenRefreshTimeout 验证过期 token 触发刷新时受 tokenFetchTimeout 约束：
// 刷新端点卡死也不无限阻塞（防 init 阶段传 Background ctx 时 hang）。
func TestAccessTokenRefreshTimeout(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 模拟刷新端点卡死：sleep 明显长于 tokenFetchTimeout（300ms）。实测 oauth2 的刷新请求
		// 客户端超时不会取消服务器端 r.Context()，故 handler 需自行结束，避免 ts.Close() 等满整个 sleep。
		time.Sleep(1 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"access_token":"new","token_type":"Bearer"}`)
	}))
	t.Cleanup(ts.Close)

	Register(ProviderConfig{Name: "slow", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token", ClientID: "c"})
	pc, _ := Provider("slow")
	expired := &oauth2.Token{AccessToken: "old", RefreshToken: "rt", Expiry: time.Now().Add(-time.Hour)}
	if err := oauth.NewTokenStore(authStoreSubdir, "slow").Save(pc.Config(""), expired); err != nil {
		t.Fatalf("save expired session: %v", err)
	}

	orig := tokenFetchTimeout
	tokenFetchTimeout = 300 * time.Millisecond
	t.Cleanup(func() { tokenFetchTimeout = orig })

	start := time.Now()
	if _, err := AccessToken(context.Background(), "slow"); err == nil {
		t.Fatal("AccessToken should error on refresh timeout")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("AccessToken blocked %s, want bounded by tokenFetchTimeout", elapsed)
	}
}
