package adapter

import (
	"net/http"
	"testing"
)

// TestAnthropicAdapterOAuthHeader 验证 oauth 鉴权：SetHeaders 用 Authorization: Bearer
// （不加 x-api-key），并附订阅 beta 头。
func TestAnthropicAdapterOAuthHeader(t *testing.T) {
	a := NewAnthropicAdapter("https://api.example.com", "tok-123", "claude-sonnet-4-6", "2023-06-01", nil, 0, 0, "oauth")
	r, _ := http.NewRequest("POST", "https://api.example.com/messages", nil)
	a.SetHeaders(r)

	if got := r.Header.Get("Authorization"); got != "Bearer tok-123" {
		t.Fatalf("Authorization = %q, want Bearer tok-123", got)
	}
	if got := r.Header.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key = %q, want empty for oauth", got)
	}
	if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
		t.Fatalf("anthropic-beta = %q, want oauth-2025-04-20", got)
	}
}

// TestAnthropicAdapterAPIKeyHeader 验证 api-key 鉴权路径不受影响：x-api-key 生效、无 Bearer。
func TestAnthropicAdapterAPIKeyHeader(t *testing.T) {
	a := NewAnthropicAdapter("https://api.example.com", "sk-abc", "claude-sonnet-4-6", "2023-06-01", nil, 0, 0, "api-key")
	r, _ := http.NewRequest("POST", "https://api.example.com/messages", nil)
	a.SetHeaders(r)

	if got := r.Header.Get("x-api-key"); got != "sk-abc" {
		t.Fatalf("x-api-key = %q, want sk-abc", got)
	}
	if got := r.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization = %q, want empty for api-key", got)
	}
}

// TestOpenAIAdapterBearerHeader 验证 openai 适配器对两种鉴权方式均用 Bearer（api-key 时 key
// 即 API key，oauth 时 key 为 access token，行为一致）。
func TestOpenAIAdapterBearerHeader(t *testing.T) {
	for _, authType := range []string{"api-key", "oauth"} {
		a := NewOpenAIAdapter("deepseek", "https://api.example.com", "sk-xyz", "deepseek-chat", nil, 0, 0, authType)
		r, _ := http.NewRequest("POST", "https://api.example.com/chat/completions", nil)
		a.SetHeaders(r)
		if got := r.Header.Get("Authorization"); got != "Bearer sk-xyz" {
			t.Fatalf("%s: Authorization = %q, want Bearer sk-xyz", authType, got)
		}
	}
}
