// auth_startup.go：启动后台连接（non-interactive）的轻量 OAuthHandler。
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/oauth2"
)

// startupHandler 是启动后台连接用的轻量 OAuthHandler：
//   - TokenSource 返回盘上持久化 token（无则 nil → 不带 Authorization → 服务器 401）；
//   - Authorize 直接返回 ErrNeedsAuth，零网络副作用（不发现元数据、不 DCR、不浏览器、不阻塞）。
type startupHandler struct {
	name string
	ts   oauth2.TokenSource
}

func (h *startupHandler) TokenSource(context.Context) (oauth2.TokenSource, error) { return h.ts, nil }

// Authorize 判定 401 是否为 OAuth 场景：仅当响应带 Bearer 挑战（WWW-Authenticate: Bearer）
// 才返回 ErrNeedsAuth（→ StatusNeedsAuth）。普通 401（无 WWW-Authenticate 或非 Bearer
// scheme）说明服务器不是 OAuth 保护，按普通连接错误处理，避免把无授权意图的服务器误判为
// needs-auth。
func (h *startupHandler) Authorize(_ context.Context, _ *http.Request, resp *http.Response) error {
	// 不变量：needs-auth 仅当可确认 Bearer 挑战时进入；nil/无挑战一律按普通连接错误。
	if resp == nil {
		return fmt.Errorf("MCP server %s: authorization challenge without response", h.name)
	}
	defer resp.Body.Close()
	if !hasBearerChallenge(resp) {
		return fmt.Errorf("MCP server %s: 401 Unauthorized without Bearer challenge (not OAuth-protected)", h.name)
	}
	return fmt.Errorf("%w: %s", ErrNeedsAuth, h.name)
}

// hasBearerChallenge 报告 401 响应的 WWW-Authenticate 是否含 Bearer scheme（大小写不敏感）。
// 可取值如 "Bearer"、`Bearer realm="..."`；其他 scheme（Basic/Digest 等）不是 MCP OAuth 场景。
func hasBearerChallenge(resp *http.Response) bool {
	if resp == nil {
		return false
	}
	for _, v := range resp.Header.Values("WWW-Authenticate") {
		scheme, _, _ := strings.Cut(strings.TrimSpace(v), " ")
		if strings.EqualFold(scheme, "Bearer") {
			return true
		}
	}
	return false
}
