// login.go：订阅登录（授权码 + PKCE 本地回环）入口。x/oauth2 负责 PKCE/state/换 token/刷新，
// 底层构件（回调服务器、浏览器打开器、会话落盘）复用 tinyclue/oauth 包。
package subscription

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/oauth"
)

// authStoreSubdir 是订阅登录会话的落盘目录（<TinyClueDir()>/oauth-auth）。
const authStoreSubdir = "oauth-auth"

// tokenFetchTimeout 是取 access token（含可能触发的刷新）的总超时。
// 调用方（api_provider.NewFromConfig）可能传 context.Background() 无截止时间，
// 这里就近兜底，避免启动阶段刷新端点不可达时无限阻塞。测试可缩短。
var tokenFetchTimeout = 10 * time.Second

// errNoClientID 提示预留厂商需要用户自备 client id。
func errNoClientID(name string) error {
	return fmt.Errorf("subscription: provider %q requires a client id — set TINYCLUE_OAUTH_CLIENT_ID_%s",
		name, strings.ToUpper(name))
}

// Login 完整授权码流程：起本地回调 → PKCE(state+verifier) → 打开浏览器 →
// 等回调校验 state → code 换 token。浏览器授权完成即返回；ctx 取消返回错误（可取消）。
func Login(ctx context.Context, name string) (*oauth2.Token, error) {
	pc, ok := Provider(name)
	if !ok {
		return nil, fmt.Errorf("subscription: unknown provider %q", name)
	}
	if pc.ClientID == "" {
		return nil, errNoClientID(name)
	}

	cb, err := oauth.NewCallbackServer()
	if err != nil {
		return nil, err
	}
	defer cb.Close()

	cfg := pc.Config(cb.URL())
	verifier := oauth2.GenerateVerifier()
	state, err := newState()
	if err != nil {
		return nil, err
	}
	authURL := cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier))

	if err := oauth.BrowserOpener(authURL); err != nil {
		return nil, fmt.Errorf("subscription: open browser: %w", err)
	}

	res, err := cb.Wait(ctx)
	if err != nil {
		return nil, err
	}
	if res.Err != nil {
		return nil, res.Err
	}
	if res.State != state {
		return nil, errors.New("subscription: state mismatch")
	}
	// iss（RFC 9207）校验：厂商配置了 Issuer 且回调带了 iss 时强制一致（防御纵深，防回调伪造）；
	// 任意一方为空则跳过（兼容未实现 RFC 9207 的授权服务器）。
	if pc.Issuer != "" && res.Iss != "" && res.Iss != pc.Issuer {
		return nil, fmt.Errorf("subscription: issuer mismatch: got %q, want %q", res.Iss, pc.Issuer)
	}
	return cfg.Exchange(ctx, res.Code, oauth2.VerifierOption(verifier))
}

// LoginAndSave 登录并把会话落盘（subscribe goroutine 一步完成）。
// 保存的 config 不含 redirect_uri：刷新走 TokenSource 只依赖端点+client_id+refresh token。
func LoginAndSave(ctx context.Context, name string) error {
	tok, err := Login(ctx, name)
	if err != nil {
		return err
	}
	pc, _ := Provider(name)
	return oauth.NewTokenStore(authStoreSubdir, name).Save(pc.Config(""), tok)
}

// newState 生成防 CSRF 的 state 参数（16 字节随机 hex）。
func newState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("subscription: generate state: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// AccessToken 返回当前可用的 access token：从盘上会话构建可刷新 source，
// x/oauth2 就近自动刷新（expiry 邻近/过期），saving source 把刷新结果写盘。
// token 过期时会触发一次刷新（网络请求），用 tokenFetchTimeout 兜底，防止调用方
// 传入无截止时间的 ctx（如 context.Background()）时无限阻塞。
func AccessToken(ctx context.Context, name string) (string, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, tokenFetchTimeout)
	defer cancel()
	ts := oauth.NewTokenStore(authStoreSubdir, name)
	src := ts.LoadPersistedSource(fetchCtx)
	if src == nil {
		return "", fmt.Errorf("subscription: no saved session for %q", name)
	}
	tok, err := src.Token()
	if err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}
