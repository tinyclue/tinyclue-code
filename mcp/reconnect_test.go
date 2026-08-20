package mcp

import (
	"context"
	"testing"
	"time"

	sdk_mcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinyclue/tinyclue-code/config"
)

// stubSDKSession 是 sdkSession 的测试替身：Wait() 立即返回（模拟断线），close 无副作用。
type stubSDKSession struct{}

func (s *stubSDKSession) ListTools(context.Context, *sdk_mcp.ListToolsParams) (*sdk_mcp.ListToolsResult, error) {
	return &sdk_mcp.ListToolsResult{}, nil
}
func (s *stubSDKSession) CallTool(context.Context, *sdk_mcp.CallToolParams) (*sdk_mcp.CallToolResult, error) {
	return &sdk_mcp.CallToolResult{}, nil
}
func (s *stubSDKSession) Close() error { return nil }
func (s *stubSDKSession) Wait() error  { return sdk_mcp.ErrConnectionClosed }

// TestHTTPAutoReconnectLoop 验证断线 → watchDisconnect → reconnectLoop 全链：
// 用 Wait() 立即返回的假 session 模拟断线，connect 指向必然失败地址（127.0.0.1:1），
// 重连尝试耗尽后 status 为 StatusError、reconnecting 标志复位。
// （覆盖连接关闭的自动重连机制本身；go-sdk SSE 断线时延属其内部 MaxRetries 行为，不在此测。）
func TestHTTPAutoReconnectLoop(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir()) // 隔离 mcp-auth token 存储（防读真实用户 token）
	origMax, origInit, origMaxBackoff := maxReconnectAttempts, initialBackoffMs, maxBackoffMs
	maxReconnectAttempts, initialBackoffMs, maxBackoffMs = 3, time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { maxReconnectAttempts, initialBackoffMs, maxBackoffMs = origMax, origInit, origMaxBackoff })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess := &stubSDKSession{}
	m := NewManager()
	st := &ServerState{
		named:  config.NamedServer{Name: "srv", Server: config.Server{Type: "http", URL: "http://127.0.0.1:1/mcp"}},
		sess:   sess,
		status: StatusConnected,
	}
	m.mu.Lock()
	m.servers = []*ServerState{st}
	m.mu.Unlock()
	// 注入 manager 的事件出口与调用路由（与生产 instantiate 一致）。
	st.notify = m.forwardEvent
	st.caller = m.mcpCaller()

	done := make(chan struct{})
	go func() {
		st.watchDisconnect(ctx, sess)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("watchDisconnect did not finish (reconnect loop hung)")
	}

	st.mu.RLock()
	status := st.status
	st.mu.RUnlock()
	if status != StatusError {
		t.Fatalf("status = %v, want StatusError (all reconnect attempts failed)", status)
	}
	if st.reconnecting.Load() {
		t.Fatal("reconnecting flag not cleared after loop")
	}
}

// TestReconnectStopsOnDisabled 验证禁用状态下自动重连立即停止（watcher 不拉起已禁用 server）。
func TestReconnectStopsOnDisabled(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	origMax, origInit, origMaxBackoff := maxReconnectAttempts, initialBackoffMs, maxBackoffMs
	maxReconnectAttempts, initialBackoffMs, maxBackoffMs = 3, time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { maxReconnectAttempts, initialBackoffMs, maxBackoffMs = origMax, origInit, origMaxBackoff })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := NewManager()
	st := &ServerState{
		named:    config.NamedServer{Name: "srv", Server: config.Server{Type: "http", URL: "http://127.0.0.1:1/mcp"}},
		sess:     &stubSDKSession{},
		status:   StatusConnected,
		disabled: true, // 已禁用：watcher 不得重连
	}
	m.mu.Lock()
	m.servers = []*ServerState{st}
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		st.watchDisconnect(ctx, st.sess)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("watchDisconnect did not finish")
	}
	// 不应发起重连：status 保持原样（disabled 不影响 status，仅 watcher 短路）。
	st.mu.RLock()
	status := st.status
	st.mu.RUnlock()
	if status != StatusConnected {
		t.Fatalf("disabled server status = %v, want unchanged StatusConnected (no reconnect)", status)
	}
}
