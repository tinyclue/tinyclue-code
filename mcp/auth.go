// MCP http server 的 OAuth 授权支持（本文件 = 设计文档 + 两条路径的调度中心）。
//
// 分两条路径（核心设计）：
//   - 启动后台连接（non-interactive）：用轻量 startupHandler（见 auth_startup.go）——有盘上
//     token 则返回它（x/oauth2 静默刷新），无则返回 nil（不带 Authorization → 服务器 401）；
//     Authorize 一律直接返回 ErrNeedsAuth。绝不打开浏览器、不注册客户端、不阻塞。
//     关键原因：go-sdk 的 AuthorizationCodeHandler.Authorize 在弹出浏览器之前会先做
//     动态客户端注册（DCR），启动连接若直接用它且无 token，会给服务器留下脏客户端注册。
//   - 交互授权（interactive，/mcp 面板的 Authenticate / Re-authenticate）：走 go-sdk 完整
//     AuthorizationCodeHandler（见 auth_interactive.go）——PRM/AS 元数据发现 → DCR → PKCE →
//     浏览器 → localhost 回调 → 换 token → NewTokenSource 落盘。
//
// 公共支撑：token 持久化（复用 tinyclue/oauth 的 TokenStore，落盘 <TinyClueDir()>/mcp-auth/）
// 与浏览器打开/请求头（oauth.BrowserOpener + auth_http.go）。下次启动以 InitialTokenSource
// 复用（能静默刷新则免授权）。
//
// token 过期三层：access token 过期、refresh 有效 → x/oauth2 TokenSource 静默刷新（无感）；
// refresh 也失效 → 下一次连接 401 → ErrNeedsAuth → 服务器进入 needs-auth 状态，提示用户 /mcp 重授权；
// refresh 在会话中途失效（已 Connected 后工具调用 401）→ callTool 检测 ErrNeedsAuth →
// needsAuthMidSession（server.go）把 server 翻回 needs-auth 并派发 McpNeedsAuth。
package mcp

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/auth"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// ErrNeedsAuth 是"服务器需要授权"的哨兵错误。errors.Is 判定：启动连接遇 401 + Bearer
// 挑战时用它标记服务器进入 needs-auth 状态（区别于普通连接错误）。
var ErrNeedsAuth = errors.New("MCP server requires authorization")

// ErrAuthCancelled 是"用户中止进行中交互授权"的哨兵错误（/mcp 面板 busy 时按 [c] 取消）。
// 被取消的交互连接在 server.go connect 里映射回 needs-auth（而非 Error），reconnect 据此
// 返回本错误，供面板文案显示"已取消"。
var ErrAuthCancelled = errors.New("MCP authorization cancelled by user")

// buildAuthHandler 按连接模式选择 OAuthHandler（两条路径的调度中心，见文件头设计）：
//   - non-interactive（启动后台）→ startupHandler（auth_startup.go，轻量，遇 401 返回 ErrNeedsAuth）；
//   - interactive（/mcp）→ buildInteractiveHandler（auth_interactive.go，完整授权码流程）。
func buildAuthHandler(name string, interactive bool, onAuthStart func(server, url string)) (auth.OAuthHandler, func(), error) {
	if !interactive {
		store := oauth.NewTokenStore("mcp-auth", name)
		return &startupHandler{name: name, ts: store.LoadPersistedSource(context.Background())}, func() {}, nil
	}
	return buildInteractiveHandler(name, onAuthStart)
}
