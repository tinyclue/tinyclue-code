// auth_interactive.go：交互授权（/mcp 面板的 Authenticate / Re-authenticate）的完整授权码流程。
//
// 本地回调监听/浏览器打开/会话落盘由共享 oauth 包提供；本文件只保留 go-sdk 的
// AuthorizationCodeHandler 编排（PRM/AS 元数据发现 → DCR → PKCE → 浏览器 → localhost 回调
// → 换 token → 落盘）。
package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/oauth"
)

// buildInteractiveHandler 构造走完整授权码流程的 handler（/mcp [a]/[r] 触发）。
// 预绑 localhost 回调 server（RedirectURL 与 DCR RedirectURIs 指向它）；NewTokenSource 在
// 换到 token 后落盘；盘上已有有效 token 时 InitialTokenSource 直接复用（免授权）。
// 返回 cleanup（关闭回调监听器），由调用方在 Connect 返回后调用。
func buildInteractiveHandler(name string, onAuthStart func(server, url string)) (auth.OAuthHandler, func(), error) {
	cb, err := oauth.NewCallbackServer()
	if err != nil {
		return nil, nil, err
	}
	cleanup := cb.Close
	store := oauth.NewTokenStore("mcp-auth", name)

	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		RedirectURL: cb.URL(),
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{
			Metadata: &oauthex.ClientRegistrationMetadata{
				RedirectURIs: []string{cb.URL()},
				ClientName:   "tinyclue",
				GrantTypes:   []string{"authorization_code", "refresh_token"},
			},
		},
		RequestRefreshToken: true,
		AuthorizationCodeFetcher: func(ctx context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			if err := oauth.BrowserOpener(args.URL); err != nil {
				return nil, fmt.Errorf("open browser: %w", err)
			}
			// 浏览器成功打开后才通知订阅方（McpAuthStarted → "授权中 — 浏览器已打开"），
			// 避免 opener 失败时提示"已打开"但实际未打开（连接随后失败走 Error）。
			if onAuthStart != nil {
				onAuthStart(name, args.URL)
			}
			// 超时由 connectHTTP 交互分支的 defaultAuthTimeout（connectCtx）兜底，
			// 授权流程（含回调等待、换 token）全程受其约束，这里不再叠加第二层超时。
			res, err := cb.Wait(ctx)
			if err != nil {
				return nil, err
			}
			if res.Err != nil {
				return nil, res.Err
			}
			return &auth.AuthorizationResult{Code: res.Code, State: res.State, Iss: res.Iss}, nil
		},
		NewTokenSource: func(ctx context.Context, cfg *oauth2.Config, tok *oauth2.Token) (oauth2.TokenSource, error) {
			if err := store.Save(cfg, tok); err != nil {
				log.Errorf(context.Background(), "mcp %s: save oauth token: %v", name, err)
			}
			return oauth.NewSavingTokenSource(cfg.TokenSource(ctx, tok), cfg, tok, store.Save), nil
		},
		InitialTokenSource: store.LoadPersistedSource(context.Background()),
	})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	return handler, cleanup, nil
}
