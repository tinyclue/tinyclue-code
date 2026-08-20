// Package oauth 提供 OAuth 协议通用构件（不含厂商/流程编排）。
//
// CallbackServer（本地回调监听）、BrowserOpener（跨平台开浏览器）、
// TokenStore / SavingTokenSource（会话落盘 + 刷新落盘）从 mcp 包抽取，mcp 包与订阅登录
// （tinyclue/subscription，厂商注册表 + Login 流程）共用。
package oauth

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/config"
)

// PersistedAuth 是落盘的 OAuth 会话：config 用于重建可刷新的 TokenSource，
// token 为当前 access/refresh。
type PersistedAuth struct {
	Config *oauth2.Config `json:"config"`
	Token  *oauth2.Token  `json:"token"`
}

// TokenStore 管理单个名称（MCP server / provider）的 OAuth 会话文件
// （<TinyClueDir()>/<subdir>/<SanitizeName(name)>.json）。目录 0700、文件 0600
// （含敏感 token）。TINYCLUE_CONFIG_DIR 生效，测试可 t.Setenv 隔离。
type TokenStore struct {
	subdir string
	name   string
}

// NewTokenStore 构造按 subdir+name 定位会话文件的存储。
// mcp 用 NewTokenStore("mcp-auth", serverName)，订阅登录用 NewTokenStore("oauth-auth", provider)。
func NewTokenStore(subdir, name string) TokenStore {
	return TokenStore{subdir: subdir, name: name}
}

func (ts TokenStore) dir() (string, error) {
	base, err := config.TinyClueDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, ts.subdir), nil
}

func (ts TokenStore) path() (string, error) {
	d, err := ts.dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, SanitizeName(ts.name)+".json"), nil
}

// Delete 删除该名称的持久化会话文件；文件不存在时幂等返回 nil。
func (ts TokenStore) Delete() error {
	p, err := ts.path()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Load 读取持久化会话；文件不存在时返回 (nil, error)。
func (ts TokenStore) Load() (*PersistedAuth, error) {
	p, err := ts.path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var pa PersistedAuth
	if err := json.Unmarshal(data, &pa); err != nil {
		return nil, err
	}
	return &pa, nil
}

// Save 把会话写盘；cfg/tok 任一为 nil 时跳过。
func (ts TokenStore) Save(cfg *oauth2.Config, tok *oauth2.Token) error {
	if cfg == nil || tok == nil {
		return nil
	}
	p, err := ts.path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(PersistedAuth{Config: cfg, Token: tok})
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// LoadPersistedSource 从盘上加载会话，构建可静默刷新的 saving source；无有效 token → nil。
func (ts TokenStore) LoadPersistedSource(ctx context.Context) oauth2.TokenSource {
	pa, err := ts.Load()
	if err != nil || pa == nil || pa.Config == nil || pa.Token == nil || pa.Token.AccessToken == "" {
		return nil
	}
	return NewSavingTokenSource(pa.Config.TokenSource(ctx, pa.Token), pa.Config, pa.Token, ts.Save)
}

// SanitizeName 把 server/provider 名转成安全文件名（保留字母数字 .-_，其余替换为 -）。
func SanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// ── 刷新落盘包装（复刻 go-sdk auth_example_test 模式，SDK 未导出）──

// savingTokenSource 包装 oauth2.TokenSource，每次 access token 变化时调 saver 写盘，
// 保证刷新后的 token 也持久化（重启后仍能静默复用）。
type savingTokenSource struct {
	mu          sync.Mutex
	src         oauth2.TokenSource
	saver       func(*oauth2.Config, *oauth2.Token) error
	config      *oauth2.Config
	accessToken string
}

func (s *savingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tok, err := s.src.Token()
	if err != nil {
		return nil, err
	}
	if s.accessToken != tok.AccessToken {
		s.accessToken = tok.AccessToken
		_ = s.saver(s.config, tok)
	}
	return tok, nil
}

// NewSavingTokenSource 包装 oauth2.TokenSource，access token 变化时经 saver 写盘。
func NewSavingTokenSource(wrapped oauth2.TokenSource, config *oauth2.Config, initial *oauth2.Token, saver func(*oauth2.Config, *oauth2.Token) error) oauth2.TokenSource {
	if wrapped == nil {
		return nil
	}
	if saver == nil {
		return wrapped
	}
	var accessToken string
	if initial != nil {
		accessToken = initial.AccessToken
	}
	return &savingTokenSource{src: wrapped, saver: saver, config: config, accessToken: accessToken}
}
