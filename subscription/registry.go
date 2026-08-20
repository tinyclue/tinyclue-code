// registry.go：订阅登录的厂商静态注册表。框架通用，加厂商 = 加一条注册表条目。
package subscription

import (
	"os"
	"sort"
	"strings"

	"golang.org/x/oauth2"
)

// ProviderConfig 一个订阅厂商的静态配置。
// ClientID 是公开 client（PKCE 代替 client_secret），供 CLI 类应用使用。
type ProviderConfig struct {
	Name     string // 与 config 的 provider 名一致
	AuthURL  string // 授权端点
	TokenURL string // token 端点
	ClientID string // 为空时需 env TINYCLUE_OAUTH_CLIENT_ID_<NAME> 提供
	Issuer   string // 可选：授权服务器 issuer（RFC 9207）；为空时跳过 iss 校验（兼容不回传 iss 的 AS）
	Scopes   []string
}

// Config 组装 x/oauth2 Config（redirect_uri 为本地回调地址）。
func (pc ProviderConfig) Config(redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:    pc.ClientID,
		Endpoint:    oauth2.Endpoint{AuthURL: pc.AuthURL, TokenURL: pc.TokenURL},
		RedirectURL: redirectURL,
		Scopes:      pc.Scopes,
	}
}

// registry 内置厂商。anthropic 开箱即用（Claude Pro/Max 订阅，对齐 Claude Code /login）；
// 其余预留 ClientID 为空，需用户经 env 提供（Google 等不提供公开 client）。
var registry = map[string]ProviderConfig{
	"anthropic": {
		Name:     "anthropic",
		AuthURL:  "https://claude.com/cai/oauth/authorize",
		TokenURL: "https://platform.claude.com/v1/oauth/token",
		ClientID: "9d1c250a-e61b-44d9-88ed-5944d1962f5e",
		// Issuer: 授权端点实测确认为授权服务器 issuer（RFC 9207）后填入，开启回调 iss 校验。
		Scopes: []string{"user:inference", "user:profile", "user:file_upload"},
	},
	// "google": {Name: "google", AuthURL: "...", TokenURL: "...", ClientID: "", Scopes: [...]},
	// "kimi":   {Name: "kimi",   AuthURL: "...", TokenURL: "...", ClientID: "", Scopes: [...]},
}

// ProviderNames 返回已注册的订阅厂商名（排序）。
func ProviderNames() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Provider 返回厂商配置；env TINYCLUE_OAUTH_CLIENT_ID_<大写NAME> 可覆盖 ClientID。
func Provider(name string) (ProviderConfig, bool) {
	pc, ok := registry[name]
	if !ok {
		return pc, false
	}
	if id := os.Getenv("TINYCLUE_OAUTH_CLIENT_ID_" + strings.ToUpper(name)); id != "" {
		pc.ClientID = id
	}
	return pc, true
}

// Register 注册/覆盖厂商配置（测试、扩展用）。
func Register(pc ProviderConfig) {
	if pc.Name != "" {
		registry[pc.Name] = pc
	}
}
