package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListSettingsReturnsAutoMemory(t *testing.T) {
	c := &Config{dir: t.TempDir()}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}

	views := c.ListSettings()
	if len(views) == 0 {
		t.Fatal("ListSettings() returned no settings")
	}
	found := false
	for _, v := range views {
		if v.Key == "auto_memory" {
			found = true
			if v.Label == "" {
				t.Error("auto_memory setting has empty label")
			}
			if v.Value != "false" {
				t.Errorf("auto_memory value = %q, want false", v.Value)
			}
			if len(v.Candidates) != 2 {
				t.Errorf("auto_memory candidates = %v, want [true false]", v.Candidates)
			}
		}
	}
	if !found {
		t.Error("ListSettings() missing auto_memory setting")
	}
}

func TestSetSettingsPersists(t *testing.T) {
	dir := t.TempDir()
	c := &Config{dir: dir}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}

	if err := c.SetSettings(map[string]any{"auto_memory": "true"}); err != nil {
		t.Fatalf("SetSettings(auto_memory, true): %v", err)
	}
	if !c.AutoMemory() {
		t.Error("AutoMemory() = false after SetSettings(true)")
	}

	// Reload from disk to confirm it was persisted.
	if err := c.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if !c.AutoMemory() {
		t.Error("AutoMemory() = false after reload, want persisted true")
	}

	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		t.Fatalf("read settings.json: %v", err)
	}
	if !containsKey(string(data), "auto_memory") {
		t.Errorf("settings.json missing auto_memory key:\n%s", data)
	}
}

func TestSetSettingsInvalidValue(t *testing.T) {
	c := &Config{dir: t.TempDir()}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	// 非 string 值被拒。
	if err := c.SetSettings(map[string]any{"auto_memory": true}); err == nil {
		t.Error("SetSettings(auto_memory, bool) = nil, want type error")
	}
	// string 但不在候选值内（ParseBool 失败）被拒。
	if err := c.SetSettings(map[string]any{"auto_memory": "yes"}); err == nil {
		t.Error("SetSettings(auto_memory, \"yes\") = nil, want error")
	}
}

func TestSetSettingsUnknownKey(t *testing.T) {
	c := &Config{dir: t.TempDir()}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if err := c.SetSettings(map[string]any{"no_such_setting": true}); err == nil {
		t.Error("SetSettings(unknown key) = nil, want error")
	}
}

// TestSetSettingsBatchAtomic 验证批量写入的原子性：一个 key 非法 → 整体失败，
// 合法 key 也不落内存、不落盘（settings.json 不产生）。
func TestSetSettingsBatchAtomic(t *testing.T) {
	dir := t.TempDir()
	c := &Config{dir: dir}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}

	err := c.SetSettings(map[string]any{"auto_memory": "true", "no_such_setting": true})
	if err == nil {
		t.Fatal("SetSettings with unknown key = nil, want error")
	}
	if c.AutoMemory() {
		t.Error("AutoMemory() = true after failed batch, want unchanged false")
	}
	if data, ferr := os.ReadFile(filepath.Join(dir, "settings.json")); ferr == nil {
		if containsKey(string(data), "auto_memory") {
			t.Errorf("failed batch still wrote settings.json:\n%s", data)
		}
	}
}

// TestSetSettingsBatchAllValid 验证整批合法时一次写入成功。
func TestSetSettingsBatchAllValid(t *testing.T) {
	c := &Config{dir: t.TempDir()}
	if err := c.loadSettings(); err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if err := c.SetSettings(map[string]any{"auto_memory": "true"}); err != nil {
		t.Fatalf("SetSettings(auto_memory=true): %v", err)
	}
	if !c.AutoMemory() {
		t.Error("AutoMemory() = false after batch set true")
	}
}

func containsKey(s, key string) bool {
	for i := 0; i+len(key) <= len(s); i++ {
		if s[i:i+len(key)] == key {
			return true
		}
	}
	return false
}
