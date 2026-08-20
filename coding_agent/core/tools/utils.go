package tools

import (
	"github.com/tinyclue/tinyclue-code/config"
	"os"
	"path/filepath"
	"strings"
)

// expandPath 展开 ~ 为用户目录（与 TS expandPath 一致）。
func expandPath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[1:])
}

// toRelativePath 将绝对路径转为相对当前工作目录的路径。
func toRelativePath(absPath string) string {
	rel, err := filepath.Rel(config.CLI.Cwd, absPath)
	if err != nil {
		return absPath
	}
	return rel
}
