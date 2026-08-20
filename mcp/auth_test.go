package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/oauth"
)

// ── 假授权服务器（同时充当 MCP endpoint 与 OAuth 授权服务器）──

const (
	fakeClientID     = "tinyclue-test"
	fakeAccessToken  = "mock-token"
	fakeRefreshToken = "mock-refresh"
	fakeAuthCode     = "mock-code"
)

// newFakeAuthServer 构造一个 httptest 服务器，既是 MCP endpoint（/mcp）也是 OAuth 授权服务器：
//
//   - auth=false：/mcp 直接放行（无鉴权 http e2e）。
//   - auth=true ：/mcp 无 `Bearer mock-token` 返回 401 + WWW-Authenticate；
//     带正确 token 则走真实 streamable MCP（echo 工具）。
//
// OAuth 路径齐备：PRM 发现 404（回落"根即授权服务器"）→ AS 元数据 → DCR（/register）→
// 授权码（/authorize 302 重定向到 redirect_uri，浏览器模拟用）→ 换 token（/token）。
func newFakeAuthServer(t *testing.T, auth bool) *httptest.Server {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "tinyclue-fake-http", Version: "0.1.0"}, nil)
	server.AddTool(&mcp.Tool{
		Name:        "echo",
		Description: "Echoes back the message.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
	}, func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			Msg string `json:"msg"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: args.Msg}}}, nil
	})
	stream := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)

	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-authorization-server":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w,
				`{"issuer":"%s","authorization_endpoint":"%s/authorize","token_endpoint":"%s/token",`+
					`"registration_endpoint":"%s/register","scopes_supported":["offline_access"],`+
					`"code_challenge_methods_supported":["S256"]}`,
				ts.URL, ts.URL, ts.URL, ts.URL)
			return
		case "/register":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"client_id":"%s","client_secret":"test-secret","token_endpoint_auth_method":"none"}`, fakeClientID)
			return
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"access_token":"%s","token_type":"Bearer","refresh_token":"%s"}`, fakeAccessToken, fakeRefreshToken)
			return
		case "/authorize":
			// 浏览器模拟：302 重定向到 redirect_uri 并附 code/state。
			redirect := r.URL.Query().Get("redirect_uri")
			if redirect == "" {
				http.Error(w, "missing redirect_uri", http.StatusBadRequest)
				return
			}
			loc, err := url.Parse(redirect)
			if err != nil {
				http.Error(w, "bad redirect_uri", http.StatusBadRequest)
				return
			}
			q := loc.Query()
			q.Set("code", fakeAuthCode)
			q.Set("state", r.URL.Query().Get("state"))
			loc.RawQuery = q.Encode()
			http.Redirect(w, r, loc.String(), http.StatusFound)
			return
		}

		// 其余路径视作 MCP endpoint；PRM 发现路径（/.well-known/oauth-protected-resource*）404。
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			http.NotFound(w, r)
			return
		}
		if auth && r.Header.Get("Authorization") != "Bearer "+fakeAccessToken {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		stream.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// fakeBrowser 模拟浏览器访问授权 URL：跟随 /authorize 的 302 跳回回调地址，
// 从而让 buildInteractiveHandler 的 cb.wait 收到授权结果（避免真开浏览器）。
func fakeBrowser(u string) error {
	_, err := http.Get(u)
	return err
}

// ── token 持久化 ──

func TestTokenStoreRoundTrip(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	st := oauth.NewTokenStore("mcp-auth", "my srv:dev")
	cfg := &oauth2.Config{
		ClientID: "cid",
		Endpoint: oauth2.Endpoint{AuthURL: "https://as.example/auth", TokenURL: "https://as.example/token"},
		Scopes:   []string{"offline_access"},
	}
	tok := &oauth2.Token{AccessToken: "at", RefreshToken: "rt", TokenType: "Bearer"}

	if err := st.Save(cfg, tok); err != nil {
		t.Fatalf("save: %v", err)
	}
	pa, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pa.Config.ClientID != "cid" || pa.Config.Endpoint.TokenURL != "https://as.example/token" {
		t.Fatalf("config mismatch: %+v", pa.Config)
	}
	if pa.Token.AccessToken != "at" || pa.Token.RefreshToken != "rt" {
		t.Fatalf("token mismatch: %+v", pa.Token)
	}

	// token 文件权限 0600、目录 0700。
	dir, _ := config.TinyClueDir()
	p := filepath.Join(dir, "mcp-auth", oauth.SanitizeName("my srv:dev")+".json")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Dir(p)); err == nil && fi.Mode().Perm() != 0o700 {
		t.Fatalf("token dir mode = %v, want 0700", fi.Mode().Perm())
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"my srv:dev": "my-srv-dev",
		"a.b-c_d":    "a.b-c_d",
		"中文":         "--",
	}
	for in, want := range cases {
		if got := oauth.SanitizeName(in); got != want {
			t.Fatalf("sanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// ── 启动路径（non-interactive）──

func TestStartupHandlerNeedsAuth(t *testing.T) {
	h := &startupHandler{name: "srv"}
	if ts, err := h.TokenSource(context.Background()); err != nil || ts != nil {
		t.Fatalf("TokenSource = %v, %v; want nil, nil", ts, err)
	}
	// 注意：net/http 会把响应头 key 规范化为 "Www-Authenticate"，测试字面量用规范形式。
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Header:     http.Header{"Www-Authenticate": []string{"Bearer"}},
		Body:       io.NopCloser(strings.NewReader("unauthorized")),
	}
	err := h.Authorize(context.Background(), &http.Request{}, resp)
	if !errors.Is(err, ErrNeedsAuth) {
		t.Fatalf("Authorize err = %v, want ErrNeedsAuth", err)
	}
}

func TestStartupHandlerNonBearer401(t *testing.T) {
	h := &startupHandler{name: "srv"}
	// 裸 401（无 WWW-Authenticate）与非 Bearer scheme（Basic/Digest）：不是 OAuth 场景，
	// 应返回普通连接错误，绝不返回 ErrNeedsAuth（否则误判 needs-auth）。
	cases := map[string]http.Header{
		"no challenge": {},
		"basic":        {"Www-Authenticate": []string{`Basic realm="api"`}},
		"digest":       {"Www-Authenticate": []string{`Digest realm="api"`}},
	}
	for name, hdr := range cases {
		resp := &http.Response{
			StatusCode: http.StatusUnauthorized,
			Header:     hdr,
			Body:       io.NopCloser(strings.NewReader("denied")),
		}
		err := h.Authorize(context.Background(), &http.Request{}, resp)
		if errors.Is(err, ErrNeedsAuth) {
			t.Errorf("%s: Authorize = ErrNeedsAuth, want plain error", name)
		}
		if err == nil {
			t.Errorf("%s: Authorize = nil, want error", name)
		}
	}
}

func TestHasBearerChallenge(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
		want bool
	}{
		{"bearer", http.Header{"Www-Authenticate": []string{"Bearer"}}, true},
		{"bearer realm", http.Header{"Www-Authenticate": []string{`Bearer realm="api"`}}, true},
		{"bearer lowercase", http.Header{"Www-Authenticate": []string{"bearer"}}, true},
		{"multiple values", http.Header{"Www-Authenticate": []string{`Basic realm="x"`, "Bearer"}}, true},
		{"basic only", http.Header{"Www-Authenticate": []string{`Basic realm="api"`}}, false},
		{"digest only", http.Header{"Www-Authenticate": []string{`Digest realm="api"`}}, false},
		{"no header", http.Header{}, false},
	}
	for _, c := range cases {
		resp := &http.Response{StatusCode: http.StatusUnauthorized, Header: c.h}
		if got := hasBearerChallenge(resp); got != c.want {
			t.Errorf("hasBearerChallenge(%s) = %v, want %v", c.name, got, c.want)
		}
	}
	if hasBearerChallenge(nil) {
		t.Error("hasBearerChallenge(nil) = true, want false")
	}
}

func TestStartupHandlerUsesPersistedToken(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	cfg := &oauth2.Config{
		ClientID: "cid",
		Endpoint: oauth2.Endpoint{AuthURL: "https://as.example/auth", TokenURL: "https://as.example/token"},
	}
	if err := (oauth.NewTokenStore("mcp-auth", "srv")).Save(cfg, &oauth2.Token{AccessToken: "at"}); err != nil {
		t.Fatal(err)
	}
	h := &startupHandler{name: "srv", ts: oauth.NewTokenStore("mcp-auth", "srv").LoadPersistedSource(context.Background())}
	ts, err := h.TokenSource(context.Background())
	if err != nil || ts == nil {
		t.Fatalf("TokenSource = %v, %v; want non-nil persisted source", ts, err)
	}
}

// ── 回调服务器 ──

func TestCallbackServerReceivesResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cb, err := oauth.NewCallbackServer()
	if err != nil {
		t.Fatal(err)
	}
	defer cb.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(cb.URL() + "/callback?code=abc&state=xyz&iss=https://as")
		if err == nil {
			resp.Body.Close()
		}
	}()
	res, err := cb.Wait(ctx)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if res.Code != "abc" || res.State != "xyz" || res.Iss != "https://as" {
		t.Fatalf("result = %+v", res)
	}
	<-done
}

func TestCallbackServerErrorAndClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cb, err := oauth.NewCallbackServer()
	if err != nil {
		t.Fatal(err)
	}

	go func() {
		resp, err := http.Get(cb.URL() + "/callback?error=access_denied&error_description=nope")
		if err == nil {
			resp.Body.Close()
		}
	}()
	res, err := cb.Wait(ctx)
	if err != nil {
		t.Fatalf("wait err = %v, want nil (error carried in Result.Err)", err)
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "access_denied") {
		t.Fatalf("wait result err = %v, want access_denied", res.Err)
	}

	cb.Close()
	if _, err := cb.Wait(ctx); err == nil {
		t.Fatal("wait after close should error")
	}
}

// ── 完整授权码流程集成 ──

// TestAuthorizationCodeHandlerIntegration 验证交互 handler 全链路：401 → PRM/AS 发现 →
// DCR → 浏览器（fakeBrowser 跳回调）→ 换 token → 落盘 → 可被新 handler 复用。
func TestAuthorizationCodeHandlerIntegration(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	fake := newFakeAuthServer(t, true)

	orig := oauth.BrowserOpener
	oauth.BrowserOpener = fakeBrowser
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	ctx := context.Background()
	handler, cleanup, err := buildInteractiveHandler("integration", nil)
	if err != nil {
		t.Fatalf("buildInteractiveHandler: %v", err)
	}
	defer cleanup()

	if ts, _ := handler.TokenSource(ctx); ts != nil {
		t.Fatal("TokenSource should be nil before authorize (no persisted token)")
	}

	req, _ := http.NewRequest(http.MethodPost, fake.URL+"/mcp", strings.NewReader("{}"))
	resp := &http.Response{
		StatusCode: http.StatusUnauthorized,
		Status:     "401 Unauthorized",
		Header:     http.Header{"WWW-Authenticate": []string{"Bearer"}},
		Body:       io.NopCloser(strings.NewReader("unauthorized")),
	}
	if err := handler.Authorize(ctx, req, resp); err != nil {
		t.Fatalf("authorize: %v", err)
	}

	ts, err := handler.TokenSource(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := ts.Token()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	if tok.AccessToken != fakeAccessToken {
		t.Fatalf("access token = %q, want %q", tok.AccessToken, fakeAccessToken)
	}

	// token 已落盘，可被新的 handler 复用（免授权）。
	pa, err := oauth.NewTokenStore("mcp-auth", "integration").Load()
	if err != nil {
		t.Fatalf("load persisted: %v", err)
	}
	if pa.Token.AccessToken != fakeAccessToken {
		t.Fatalf("persisted token = %q, want %q", pa.Token.AccessToken, fakeAccessToken)
	}

	h2, cleanup2, err := buildInteractiveHandler("integration", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup2()
	if ts2, _ := h2.TokenSource(ctx); ts2 == nil {
		t.Fatal("second handler should reuse persisted token source")
	}
}
