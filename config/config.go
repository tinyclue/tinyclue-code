// Package config 管理 ~/.tinyclue/config/ 下的配置文件。
// 负责加载 settings.json（当前生效的 provider/model）和 auth.json（各厂商授权信息）。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// =============================================================
// 配置结构
// =============================================================

// Settings 对应 settings.json。
type Settings struct {
	DefaultProvider string `json:"defaultProvider"`            // 当前生效的厂商，如 "deepseek"
	DefaultModel    string `json:"defaultModel"`               // 当前生效的模型名
	ReasoningEffort string `json:"reasoning_effort,omitempty"` // 推理级别: low/medium/high/max
	Language        string //@TODO Neo 语言配置
	AutoMemory      bool
}

// AuthEntry 某个厂商的授权信息。
type AuthEntry struct {
	Type string `json:"type"` // 如 "api-key"
	Key  string `json:"key"`  // API Key
}

// Auth 对应 auth.json。使用 map 支持动态厂商列表，JSON 格式为 {"provider": {...}}。
type Auth struct {
	Entries map[string]*AuthEntry
}

// UnmarshalJSON 将 {"provider": {...}} 格式的反序列化到 Entries map 中。
func (a *Auth) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &a.Entries)
}

// MarshalJSON 将 Entries map 序列化为 {"provider": {...}} 格式。
func (a *Auth) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.Entries)
}

// Config 聚合所有配置。
type Config struct {
	settings Settings
	auth     Auth
	dir      string // 配置目录路径
}

// Cnf 全局配置实例，init 时自动初始化。
var Cnf = &Config{}

func init() {
	dir, err := ConfigDir()
	if err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
	Cnf.dir = dir

	if err := Cnf.loadSettings(); err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
	if err := Cnf.loadAuth(); err != nil {
		panic(fmt.Sprintf("config: %v", err))
	}
}

// Reload 从磁盘重新加载 settings 和 auth（用于测试或热更新）。
func (c *Config) Reload() error {
	if err := c.loadSettings(); err != nil {
		return err
	}
	return c.loadAuth()
}

// =============================================================
// 加载与访问
// =============================================================

// TinyClueDir 返回 tinyclue 配置根目录：$TINYCLUE_CONFIG_DIR 或 ~/.tinyclue。
func TinyClueDir() (string, error) {
	if dir := os.Getenv("TINYCLUE_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: get home dir: %w", err)
	}
	return filepath.Join(home, ".tinyclue"), nil
}

// ConfigDir 返回配置目录路径：$TINYCLUE_CONFIG_DIR/config 或 ~/.tinyclue/config。
func ConfigDir() (string, error) {
	dir, err := TinyClueDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config"), nil
}

// DefaultProvider 返回 settings.json 中配置的默认厂商名称。
func (c *Config) DefaultProvider() string {
	return c.settings.DefaultProvider
}

// DefaultModel 返回 settings.json 中配置的默认模型名称。
func (c *Config) DefaultModel() string {
	return c.settings.DefaultModel
}

func (c *Config) Language() string {
	return c.settings.Language
}

func (c *Config) AutoMemory() bool {
	return c.settings.AutoMemory
}

// ReasoningEffort 返回 settings.json 中配置的推理级别。
// 未配置时默认返回 "high"。
func (c *Config) ReasoningEffort() string {
	if c.settings.ReasoningEffort == "" {
		return "high"
	}
	return c.settings.ReasoningEffort
}

// SetReasoningEffort 设置推理级别并保存到 settings.json。
func (c *Config) SetReasoningEffort(effort string) error {
	c.settings.ReasoningEffort = effort
	return c.saveSettings()
}

// saveSettings 将当前 settings 写入 settings.json。
func (c *Config) saveSettings() error {
	path := filepath.Join(c.dir, "settings.json")
	data, err := json.MarshalIndent(&c.settings, "", "\t")
	if err != nil {
		return fmt.Errorf("config: marshal settings: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// AuthFor 返回指定厂商的 API Key，如未配置则返回空。
// oauth 标记（Type=="oauth"、空 Key）也算已配置，此时返回 ("", true)。
func (c *Config) AuthFor(provider string) (key string, ok bool) {
	if c.auth.Entries == nil {
		return "", false
	}
	entry, exists := c.auth.Entries[provider]
	if !exists || entry == nil {
		return "", false
	}
	if entry.Key != "" || entry.Type == "oauth" {
		return entry.Key, true
	}
	return "", false
}

// AuthTypeFor 返回指定厂商的鉴权方式（entry.Type："api-key"/"oauth"），未配置返回 ""。
func (c *Config) AuthTypeFor(provider string) string {
	if c.auth.Entries == nil {
		return ""
	}
	entry, exists := c.auth.Entries[provider]
	if !exists || entry == nil {
		return ""
	}
	return entry.Type
}

// ProviderNames 返回所有已配置了 auth 的厂商列表（api-key 或 oauth 标记均算已配置）。
func (c *Config) ProviderNames() []string {
	var names []string
	for name, entry := range c.auth.Entries {
		if entry != nil && (entry.Key != "" || entry.Type == "oauth") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// SetDefaults 设置默认厂商和模型并保存到 settings.json。
func (c *Config) SetDefaults(provider, model string) error {
	c.settings.DefaultProvider = provider
	c.settings.DefaultModel = model
	return c.saveSettings()
}

// SetAuth 设置指定厂商的授权信息并保存到 auth.json。
// 厂商已存在则更新，不存在则追加。同时更新内存缓存。
func (c *Config) SetAuth(provider string, entry *AuthEntry) error {
	if c.auth.Entries == nil {
		c.auth.Entries = make(map[string]*AuthEntry)
	}
	c.auth.Entries[provider] = entry

	path := filepath.Join(c.dir, "auth.json")
	data, err := json.MarshalIndent(&c.auth, "", "\t")
	if err != nil {
		return fmt.Errorf("config: marshal auth: %w", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("config: write auth: %w", err)
	}
	return nil
}

// =============================================================
// 内部：文件加载
// =============================================================

func (c *Config) loadSettings() error {
	path := filepath.Join(c.dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 文件不存在使用默认值
			c.settings = Settings{DefaultProvider: "deepseek", DefaultModel: "deepseek-chat"}
			return nil
		}
		return fmt.Errorf("config: read settings: %w", err)
	}

	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("config: parse settings: %w", err)
	}
	// 安装时 settings.json 初始化为空对象 {}，空值回落默认厂商/模型，
	// 保持首次使用的 deepseek 默认行为。
	if s.DefaultProvider == "" {
		s.DefaultProvider = "deepseek"
	}
	if s.DefaultModel == "" {
		s.DefaultModel = "deepseek-chat"
	}
	c.settings = s
	return nil
}

func (c *Config) loadAuth() error {
	path := filepath.Join(c.dir, "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // auth 可选
		}
		return fmt.Errorf("config: read auth: %w", err)
	}

	var a Auth
	if err := json.Unmarshal(data, &a); err != nil {
		return fmt.Errorf("config: parse auth: %w", err)
	}
	c.auth = a
	return nil
}

// =============================================================
// 工具：初始化默认配置文件（首次使用）
// =============================================================
