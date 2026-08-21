package config

import (
	"fmt"
	"strconv"
)

// SettingsSpec 描述一个 /config 可配置项。
// Key/Label 为注册信息；Candidates 是该 key 的候选值（string），面板据此循环切换；
// Value 是当前值快照（候选值之一），由 ListSettings 填充。
type SettingsSpec struct {
	Key        string
	Label      string
	Value      string   // 当前值（string 形式）
	Candidates []string // 候选值
}

// settingsSpecs 全部可配置项注册表。新增配置项只需在此加一条。
var settingsSpecs = []SettingsSpec{
	{Key: "auto_memory", Label: "Auto memory", Candidates: []string{"true", "false"}},
}

// ListSettings 返回全部配置项及其当前值，供 /config 面板构建列表。
func (c *Config) ListSettings() []SettingsSpec {
	c.mu.RLock()
	defer c.mu.RUnlock()
	specs := make([]SettingsSpec, 0, len(settingsSpecs))
	for _, s := range settingsSpecs {
		specs = append(specs, SettingsSpec{
			Key:        s.Key,
			Label:      s.Label,
			Value:      c.settingValue(s.Key),
			Candidates: s.Candidates,
		})
	}
	return specs
}

// settingValue 读取某个配置项的当前值（string 形式）。key 均来自注册表，不会落到 default。
func (c *Config) settingValue(key string) string {
	switch key {
	case "auto_memory":
		return strconv.FormatBool(c.settings.AutoMemory)
	case "reasoning_effort":
		return c.settings.ReasoningEffort
	case "default_provider":
		return c.settings.DefaultProvider
	case "default_model":
		return c.settings.DefaultModel
	}
	return ""
}

// SetSettings 批量校验并写入多个配置项，然后一次性保存到 settings.json。
// 任一 key 非法或值类型不符时整体失败：不落盘、不修改内存（原子性）。
func (c *Config) SetSettings(changed map[string]any) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 先全部预检（用副本），通过后再应用到真实 settings，最后只写一次文件。
	probe := c.settings
	for key, val := range changed {
		if err := c.applySetting(&probe, key, val); err != nil {
			return err
		}
	}
	for key, val := range changed {
		if err := c.applySetting(&c.settings, key, val); err != nil {
			return err
		}
	}
	return c.saveSettings()
}

// applySetting 显式判定 key，校验并转换 value（string 候选值）后写入 Settings。
func (c *Config) applySetting(s *Settings, key string, value any) error {
	str, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s: expected string, got %T", key, value)
	}
	switch key {
	case "auto_memory":
		b, err := strconv.ParseBool(str)
		if err != nil {
			return fmt.Errorf("auto_memory: invalid value %q (want true/false)", str)
		}
		s.AutoMemory = b
		return nil
	case "reasoning_effort":
		s.ReasoningEffort = str
		return nil
	case "default_provider":
		s.DefaultProvider = str
		return nil
	case "default_model":
		s.DefaultModel = str
		return nil
	}
	return fmt.Errorf("unknown setting: %s", key)
}
