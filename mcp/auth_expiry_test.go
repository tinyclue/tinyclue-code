package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// revokableAuthServer 是一个可控授权状态的假 http server：既是 MCP endpoint（/mcp）也是 OAuth
// 授权服务器。/token 返回短寿命 token（expires_in:1——x/oauth2 的 reuseTokenSource 按
// defaultExpiryDelta=10s 判定"已过期"，1s 寿命恒小于它），于是每次 HTTP 请求都会重新走 /token
// 刷新：revoke() 之前 /token 返回有效 token，revoke() 之后返回 invalid_grant（refresh token 失效）。
// 这把"token 中途过期/吊销"做成确定性测试：下一次请求必然重新刷新，吊销必然被立即感知，
// 无需真实等待 token 到期。
type revokableAuthServer struct {
	ts      *httptest.Server
	mu      sync.Mutex
	revoked bool
}

func newRevokableAuthServer(t *testing.T) *revokableAuthServer {
	t.Helper()
	s := &revokableAuthServer{}

	server := mcp.NewServer(&mcp.Implementation{Name: "tinyclue-revokable", Version: "0.1.0"}, nil)
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

	s.ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			s.mu.Lock()
			revoked := s.revoked
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			if revoked {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprint(w, `{"error":"invalid_grant","error_description":"refresh token expired"}`)
				return
			}
			fmt.Fprintf(w, `{"access_token":"%s","token_type":"Bearer","refresh_token":"%s","expires_in":1}`,
				fakeAccessToken, fakeRefreshToken)
			return
		}
		// 其余路径视作 MCP endpoint（PRM 发现路径 404；交互授权在本测试不使用，无 /authorize）。
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+fakeAccessToken {
			w.Header().Set("WWW-Authenticate", "Bearer")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		stream.ServeHTTP(w, r)
	}))
	t.Cleanup(s.ts.Close)
	return s
}

func (s *revokableAuthServer) URL() string { return s.ts.URL }

func (s *revokableAuthServer) revoke() {
	s.mu.Lock()
	s.revoked = true
	s.mu.Unlock()
}

// saveExpiredToken 预存"已过期"的 token（Expiry 在过去）：x/oauth2 的 reuseTokenSource 判定无效，
// 每次请求都重新走 /token 刷新——吊销才可能在下一个请求被确定性地感知。
func saveExpiredToken(t *testing.T, home, server, baseURL string) {
	t.Helper()
	cfg := &oauth2.Config{
		ClientID: fakeClientID,
		Endpoint: oauth2.Endpoint{
			AuthURL:  baseURL + "/authorize",
			TokenURL: baseURL + "/token",
		},
	}
	tok := &oauth2.Token{
		AccessToken:  fakeAccessToken,
		RefreshToken: fakeRefreshToken,
		Expiry:       time.Now().Add(-time.Hour),
	}
	if err := (oauth.NewTokenStore("mcp-auth", server)).Save(cfg, tok); err != nil {
		t.Fatalf("save expired token: %v", err)
	}
}

// TestManagerMidSessionTokenExpiry 验证已连接会话中途 token 吊销（refresh 失效）：工具调用失败
// 返回 ErrNeedsAuth → needsAuthMidSession 把 server 翻回 needs-auth（关闭会话、清工具、派发
// McpNeedsAuth），而不再停留在 connected 反复失败。
func TestManagerMidSessionTokenExpiry(t *testing.T) {
	fake := newRevokableAuthServer(t)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL() + "/mcp"},
	})
	saveExpiredToken(t, home, "protected", fake.URL())

	var needsAuth atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	m := NewManager()
	m.SetEvents(func(et McpEventType, d McpEventContext) {
		if et == McpNeedsAuth && d.Name == "protected" {
			needsAuth.Add(1)
		}
	})
	m.Init(ctx)
	defer m.Close()

	// 启动即 connected（启动连接经 /token 刷新成功，拿到有效 Bearer）。
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusConnected {
		t.Fatalf("startup protected = %+v, want connected", p)
	}

	// 吊销前调用成功。
	res, err := m.CallTool(ctx, "protected", "echo", map[string]any{"msg": "hi"})
	if err != nil {
		t.Fatalf("echo before revoke: %v", err)
	}
	if res.Content != "hi" {
		t.Fatalf("echo before revoke = %q, want hi", res.Content)
	}

	// 吊销 refresh token：下一次调用刷新失败 → 裸请求 401 → ErrNeedsAuth → needsAuthMidSession。
	fake.revoke()
	if _, err := m.CallTool(ctx, "protected", "echo", map[string]any{"msg": "again"}); err == nil {
		t.Fatal("echo after revoke should error")
	}

	// 服务器翻回 needs-auth：状态、McpNeedsAuth 事件恰好一次、工具移除。
	if p, ok := summaryByName(m)["protected"]; !ok || p.Status != StatusNeedsAuth {
		t.Fatalf("after revoke protected = %+v, want needs-auth", p)
	}
	if n := needsAuth.Load(); n != 1 {
		t.Fatalf("McpNeedsAuth fired %d times, want 1", n)
	}
	if hasServerTool(m, "protected", "echo") {
		t.Fatal("echo tool should be gone after needs-auth transition")
	}
	// 会话已关闭：后续调用不再走 SDK（不会反复 401），直接报"需要授权"——状态机翻回
	// needs-auth 后行为确定，不会留在 connected 反复失败。
	if _, err := m.CallTool(ctx, "protected", "echo", map[string]any{"msg": "x"}); err == nil || !strings.Contains(err.Error(), "requires authorization") {
		t.Fatalf("post-transition call err = %v, want requires authorization", err)
	}
}
