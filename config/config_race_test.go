package config

import (
	"sync"
	"testing"
)

// TestConfigConcurrentAccessNoRace 并发读（DefaultProvider/AuthFor/...）与写
// （Reload/SetAuth/SetDefaults/...）访问同一 Config，验证热更新路径无数据竞争
// （配合 go test -race 生效）。
func TestConfigConcurrentAccessNoRace(t *testing.T) {
	c := &Config{dir: t.TempDir()}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}

	var wg sync.WaitGroup
	const n = 50
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.DefaultProvider()
			_ = c.DefaultModel()
			_ = c.Language()
			_ = c.AutoMemory()
			_ = c.ReasoningEffort()
			_, _ = c.AuthFor("anthropic")
			_ = c.AuthTypeFor("anthropic")
			_ = c.ProviderNames()
		}()
		go func() {
			defer wg.Done()
			if err := c.Reload(); err != nil {
				t.Errorf("Reload: %v", err)
			}
			if err := c.SetAuth("anthropic", &AuthEntry{Type: "oauth"}); err != nil {
				t.Errorf("SetAuth: %v", err)
			}
			if err := c.SetReasoningEffort("low"); err != nil {
				t.Errorf("SetReasoningEffort: %v", err)
			}
			if err := c.SetDefaults("deepseek", "deepseek-chat"); err != nil {
				t.Errorf("SetDefaults: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := c.ReasoningEffort(); got != "low" {
		t.Errorf("ReasoningEffort = %q, want low", got)
	}
	if _, ok := c.AuthFor("anthropic"); !ok {
		t.Error("expected anthropic auth to be set")
	}
}
