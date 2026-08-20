package config

import (
	"os"
	"path/filepath"
)

// CacheDir 返回 tinyclue 缓存目录：$TINYCLUE_CACHE_DIR，否则配置根目录下的 caches 子目录
// （$TINYCLUE_CONFIG_DIR/caches 或 ~/.tinyclue/caches），与配置目录同源、随 TINYCLUE_CONFIG_DIR 一起迁移。
func CacheDir() (string, error) {
	if dir := os.Getenv("TINYCLUE_CACHE_DIR"); dir != "" {
		return dir, nil
	}
	home, err := TinyClueDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "caches"), nil
}

// ModelCachePath 返回 model-cache.json 的缓存路径（默认 ~/.tinyclue/caches/model-cache.json）。
func ModelCachePath() (string, error) {
	dir, err := CacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "model-cache.json"), nil
}
