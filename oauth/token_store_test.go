package oauth

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"

	"github.com/tinyclue/tinyclue-code/config"
)

// ── token 持久化 ──

func TestTokenStoreSaveLoadDelete(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	st := NewTokenStore("oauth-auth", "my provider")
	cfg := &oauth2.Config{
		ClientID: "cid",
		Endpoint: oauth2.Endpoint{AuthURL: "https://as.example/auth", TokenURL: "https://as.example/token"},
		Scopes:   []string{"offline_access"},
	}
	tok := &oauth2.Token{AccessToken: "at", RefreshToken: "rt", TokenType: "Bearer"}

	if err := st.Save(cfg, tok); err != nil {
		t.Fatalf("save: %v", err)
	}
	pa, err := st.Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if pa.Config.ClientID != "cid" || pa.Token.AccessToken != "at" || pa.Token.RefreshToken != "rt" {
		t.Fatalf("roundtrip mismatch: %+v", pa)
	}

	// 目录 0700 / 文件 0600。
	dir, _ := config.TinyClueDir()
	p := filepath.Join(dir, "oauth-auth", SanitizeName("my provider")+".json")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, want 0600", fi.Mode().Perm())
	}
	if fi, err := os.Stat(filepath.Dir(p)); err == nil && fi.Mode().Perm() != 0o700 {
		t.Fatalf("token dir mode = %v, want 0700", fi.Mode().Perm())
	}

	// Delete 幂等。
	if err := st.Delete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.Load(); err == nil {
		t.Fatal("load after delete should error")
	}
	if err := st.Delete(); err != nil {
		t.Fatalf("second delete: %v", err)
	}
}

// ── 刷新落盘 ──

func TestSavingTokenSourceRepersists(t *testing.T) {
	t.Setenv("TINYCLUE_CONFIG_DIR", t.TempDir())
	st := NewTokenStore("oauth-auth", "repersist")
	cfg := &oauth2.Config{ClientID: "cid", Endpoint: oauth2.Endpoint{AuthURL: "https://as/auth", TokenURL: "https://as/token"}}

	var saves atomic.Int32
	saver := func(c *oauth2.Config, tok *oauth2.Token) error {
		saves.Add(1)
		return st.Save(c, tok)
	}
	src := NewSavingTokenSource(&fakeTS{tokens: []string{"at1", "at2", "at2"}}, cfg, &oauth2.Token{AccessToken: "at1"}, saver)

	tok, err := src.Token()
	if err != nil || tok.AccessToken != "at1" {
		t.Fatalf("first token = %v, %v", tok, err)
	}
	if saves.Load() != 0 {
		t.Fatalf("initial token should not re-save, saves=%d", saves.Load())
	}

	tok, _ = src.Token()
	if tok.AccessToken != "at2" || saves.Load() != 1 {
		t.Fatalf("changed token should save once: tok=%v saves=%d", tok.AccessToken, saves.Load())
	}

	tok, _ = src.Token()
	if tok.AccessToken != "at2" || saves.Load() != 1 {
		t.Fatalf("unchanged token should not re-save: saves=%d", saves.Load())
	}

	// 盘上已更新为新 access token。
	pa, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if pa.Token.AccessToken != "at2" {
		t.Fatalf("persisted access token = %q, want at2", pa.Token.AccessToken)
	}
}

// fakeTS 依次返回固定 access token 序列（末尾重复 → 模拟"刷新后 token 不变"场景）。
type fakeTS struct {
	tokens []string
	idx    int
}

func (f *fakeTS) Token() (*oauth2.Token, error) {
	tok := &oauth2.Token{AccessToken: f.tokens[f.idx]}
	if f.idx < len(f.tokens)-1 {
		f.idx++
	}
	return tok, nil
}
