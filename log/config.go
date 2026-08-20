package log

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type fileLogConfig struct {
	Enable   *bool  `json:"enable"`
	LogPath  string `json:"log_path"`
	FileName string `json:"file_name"`
	MaxSize  int64  `json:"max_size"`
	Backups  int    `json:"backups"`
	Level    string `json:"level"`
}

// slogConfigPath 返回日志配置文件路径：$TINYCLUE_CONFIG_DIR/config/slog.json，
// 否则 ~/.tinyclue/config/slog.json。
// TINYCLUE_CONFIG_DIR 语义与 usercontext/paths.go、config/config.go 一致（配置根目录）；
// 内联解析，避免 config→log 循环依赖。
func slogConfigPath() string {
	if dir := os.Getenv("TINYCLUE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "config", "slog.json")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".tinyclue", "config", "slog.json")
	}
	return ""
}

// loadFileConfig 尝试读取 ~/.tinyclue/config/slog.json，如果存在且合法则自动配置日志。
func loadFileConfig() {
	path := slogConfigPath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return // 没有配置文件，静默跳过
	}

	var cfg fileLogConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return
	}

	// 日志级别
	if cfg.Level != "" {
		if lvl, err := ParseLevel(cfg.Level); err == nil {
			SetLevel(lvl)
		}
	}

	// enable 显式为 false 时全局禁用文件日志
	if cfg.Enable != nil && !*cfg.Enable {
		fileLogDisabled = true
		return
	}
	if cfg.LogPath == "" || cfg.FileName == "" {
		return
	}
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 100 * 1024 * 1024
	}
	if cfg.Backups <= 0 {
		cfg.Backups = 7
	}

	_ = SetFileLog(cfg.LogPath, cfg.FileName, cfg.MaxSize, cfg.Backups)
}
