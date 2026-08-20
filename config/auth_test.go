package config

import "testing"

// TestAuthOAuthMarker 验证 oauth 标记（Type=="oauth"、空 Key）的读写：
// AuthFor ok 但 key 为空、AuthTypeFor 返回 oauth、ProviderNames 计入；api-key 路径不受影响。
func TestAuthOAuthMarker(t *testing.T) {
	c := &Config{dir: t.TempDir()}

	if _, ok := c.AuthFor("anthropic"); ok {
		t.Fatal("AuthFor ok before any auth")
	}
	if got := c.AuthTypeFor("anthropic"); got != "" {
		t.Fatalf("AuthTypeFor(anthropic) = %q, want empty", got)
	}

	if err := c.SetAuth("anthropic", &AuthEntry{Type: "oauth"}); err != nil {
		t.Fatalf("SetAuth oauth marker: %v", err)
	}
	key, ok := c.AuthFor("anthropic")
	if !ok {
		t.Fatal("AuthFor should be ok for oauth marker")
	}
	if key != "" {
		t.Fatalf("AuthFor key = %q, want empty for oauth marker", key)
	}
	if got := c.AuthTypeFor("anthropic"); got != "oauth" {
		t.Fatalf("AuthTypeFor(anthropic) = %q, want oauth", got)
	}

	// api-key 仍按 key 返回，不受 oauth 标记影响。
	if err := c.SetAuth("deepseek", &AuthEntry{Type: "api-key", Key: "sk-123"}); err != nil {
		t.Fatalf("SetAuth api-key: %v", err)
	}
	if key, ok := c.AuthFor("deepseek"); !ok || key != "sk-123" {
		t.Fatalf("AuthFor(deepseek) = %q, %v; want sk-123, true", key, ok)
	}
	if got := c.AuthTypeFor("deepseek"); got != "api-key" {
		t.Fatalf("AuthTypeFor(deepseek) = %q, want api-key", got)
	}

	// ProviderNames 同时计入 oauth 标记与 api-key 厂商（排序）。
	names := c.ProviderNames()
	want := []string{"anthropic", "deepseek"}
	if len(names) != 2 || names[0] != want[0] || names[1] != want[1] {
		t.Fatalf("ProviderNames = %v, want %v", names, want)
	}
}
