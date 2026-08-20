package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tinyclue/tinyclue-code/config"
)

// slowLegacyServer 复刻 deepwiki 慢网路径：支持 legacy 协议但不支持 SEP-2575。
//   - server/discover → HTTP 400 + 不支持的协议版本错误（与 deepwiki 一致：code=-32001，
//     SDK 判定非 CodeUnsupportedProtocolVersion(-32022) 后回落 legacy initialize）；
//   - initialize → HTTP 200 + 合法 InitializeResult（2025-11-25）；
//   - notifications/initialized → 挂起不响应（慢网下第三次 POST 撞预算的根因）。
//
// release 在测试结束时先放行挂起的 handler 再 ts.Close，避免 Close 等 handler 死等。
func newSlowLegacyServer(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	release := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			// legacy 协议的 standalone SSE 流探测：返回 405（不支持 SSE），
			// connectStandaloneSSE 快速返回，不阻塞后续 initialized 通知。
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "server/discover":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":null,"error":{"code":-32001,"message":"Unsupported protocol version","data":{}}}`)
		case "initialize":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"protocolVersion":"2025-11-25","capabilities":{},"serverInfo":{"name":"slow-legacy","version":"1.0.0"}}}`,
				req.ID)
		default:
			// notifications/initialized 等：挂起，模拟慢网下第三次往返撞预算。
			<-release
		}
	}))
	return ts, func() { close(release); ts.Close() }
}

// TestConnectTimeoutMessage 验证连接超时时的报错不再是裸 context deadline exceeded：
// 头部明示"连接超时(预算 ...)"，且 %w 链保留原始错误（含失败步骤 sending "notifications/initialized"）
// 供用户判定慢在哪一步、errors.Is/As 判定。
func TestConnectTimeoutMessage(t *testing.T) {
	t.Setenv("MCP_TIMEOUT", "300ms")

	ts, cleanup := newSlowLegacyServer(t)
	defer cleanup()

	// 外层兜底超时：即使 MCP_TIMEOUT 未按预期生效也保证测试终止（正常路径 300ms 先到）。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := connectHTTP(ctx, "hang", config.Server{Type: "http", URL: ts.URL + "/mcp"}, false, nil)
	if err == nil {
		t.Fatal("connect to hanging server should fail")
	}
	msg := err.Error()
	if !strings.Contains(msg, "connect timeout (budget 300ms)") {
		t.Errorf("message should name the budget, got:\n%s", msg)
	}
	if !strings.Contains(msg, "context deadline exceeded") {
		t.Errorf("message should preserve original error chain, got:\n%s", msg)
	}
	if !strings.Contains(msg, "connect MCP server") {
		t.Errorf("message should keep the connect prefix, got:\n%s", msg)
	}
	// 原始错误信息：明示卡在第三次往返 notifications/initialized（而非裸 context deadline）。
	if !strings.Contains(msg, "notifications/initialized") {
		t.Errorf("message should surface the failing step, got:\n%s", msg)
	}
}
