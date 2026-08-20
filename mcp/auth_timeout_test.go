package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// TestInteractiveAuthTimeout 验证交互授权有兜底超时：defaultAuthTimeout 覆盖为短值 +
// 浏览器打开器替换为 no-op（回调永不完成），Authenticate 应返回错误而非无限挂起。
func TestInteractiveAuthTimeout(t *testing.T) {
	fake := newFakeAuthServer(t, true)
	home := newMCPTestHome(t)
	writeHomeMCPConfig(t, home, map[string]any{
		"protected": map[string]any{"type": "http", "url": fake.URL + "/mcp"},
	})

	// 不实际访问授权 URL → 回调永不完成，只能靠连接超时兜底。
	orig := oauth.BrowserOpener
	oauth.BrowserOpener = func(string) error { return nil }
	t.Cleanup(func() { oauth.BrowserOpener = orig })

	origTimeout := defaultAuthTimeout
	defaultAuthTimeout = 500 * time.Millisecond
	t.Cleanup(func() { defaultAuthTimeout = origTimeout })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	start := time.Now()
	_, err := m.Authenticate("protected")
	if err == nil {
		t.Fatal("Authenticate should error on timeout")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("Authenticate took %s, want bounded by defaultAuthTimeout", elapsed)
	}

	// busy 语义：面板侧 Authenticate 超时后 status 不再 Connecting（超时已回落 Error）。
	st := m.serverByName("protected")
	st.mu.RLock()
	status := st.status
	st.mu.RUnlock()
	if status == StatusConnecting {
		t.Fatal("status still Connecting after timeout, busy never cleared")
	}
}
