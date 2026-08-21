package config

import (
	"os"
	"path/filepath"
	"strings"
)

// CLI stores parsed command-line arguments as key-value pairs.
// Package init() parses os.Args automatically; tests can call CLI.Parse() to
// override for specific scenarios.
var CLI CLIFlags

func init() {
	cwd, _ := os.Getwd()
	CLI.Cwd = canonicalizeDir(cwd)
	CLI.Parse(os.Args[1:])
}

// ── Flag key constants ──────────────────────────────────────────────────────
// Define all supported flags here. Convention: single-letter for common flags,
// descriptive names for less common ones. Consumers reference these constants
// rather than raw strings.

const (
	FlagSession = "c" // -c [sessionId] — resume an existing session
)

// ── Struct ──────────────────────────────────────────────────────────────────

// CLIFlags represents all parsed command-line flags as a key-value map.
// After Parse(), call Has(key) to check presence and Get(key) to read the value.
type CLIFlags struct {
	Cwd   string // 程序启动时的当前工作目录
	flags map[string]string
}

// ShortCwd 将完整路径中的 home 前缀替换为 ~，适合显示。
func ShortCwd(full string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return full
	}
	if after, ok := strings.CutPrefix(full, home); ok {
		return "~" + after
	}
	return full
}

// ── Parse ───────────────────────────────────────────────────────────────────

// Parse parses CLI arguments in -key value / -key=value format.
// Safe to call multiple times — resets state on each call.
func (f *CLIFlags) Parse(args []string) {
	f.flags = make(map[string]string)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}

		key := strings.TrimLeft(arg, "-")
		if key == "" {
			continue
		}

		if k, v, ok := strings.Cut(key, "="); ok {
			// -key=value
			f.flags[k] = v
		} else if i+1 < len(args) && isValue(args[i+1]) {
			// -key value
			f.flags[key] = args[i+1]
			i++
		} else {
			// -key (present, no value)
			f.flags[key] = ""
		}
	}
}

// isValue reports whether s looks like a value rather than another flag
// (i.e. doesn't start with "-").
func isValue(s string) bool {
	return !strings.HasPrefix(s, "-")
}

// ── Accessors ───────────────────────────────────────────────────────────────

// Has reports whether the flag key was set.
func (f *CLIFlags) Has(key string) bool {
	if f.flags == nil {
		return false
	}
	_, ok := f.flags[key]
	return ok
}

// Get returns the value for the given flag key and whether it was set.
// When the flag was provided without a value (e.g. -c alone), value is ""
// and ok is true.
func (f *CLIFlags) Get(key string) (value string, ok bool) {
	if f.flags == nil {
		return "", false
	}
	v, ok := f.flags[key]
	return v, ok
}

// canonicalizeDir realpath 归一化（EvalSymlinks），失败回退原值。
// 避免 symlink 进入同一目录却产生两个项目目录（如 macOS /tmp 与 /private/tmp）。
func canonicalizeDir(dir string) string {
	dir = filepath.Clean(dir) // 先消除 . ..
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}
