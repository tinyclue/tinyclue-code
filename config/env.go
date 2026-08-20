package config

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Env 包含运行环境信息。
type Env struct {
	Platform string // 操作系统，如 "darwin" / "linux"
	Shell    string // 当前 shell 路径，如 "/bin/zsh"
	IsGit    bool   // cwd 是否在 git 仓库内
	UnameSR  string // `uname -s -r` 输出，如 "Darwin 22.1.0"
}

var (
	envOnce sync.Once
	env     Env
)

// GetEnv 返回运行环境信息（惰性初始化，首次调用时探测）。
func GetEnv() *Env {
	envOnce.Do(initEnv)
	return &env
}

func initEnv() {
	env.Platform = runtime.GOOS
	env.Shell = os.Getenv("SHELL")
	env.detectGit()
	env.detectUname()
}

// detectGit 通过 git rev-parse 探测仓库信息。
func (e *Env) detectGit() {
	// --is-inside-work-tree 返回 true/false 且 exit code 指示结果
	if err := exec.Command("git", "rev-parse", "--is-inside-work-tree").Run(); err != nil {
		return
	}
	e.IsGit = true
}

// detectUname 运行 uname -s -r 获取内核名称和版本。
func (e *Env) detectUname() {
	out, err := exec.Command("uname", "-s", "-r").Output()
	if err != nil {
		return
	}
	e.UnameSR = strings.TrimSpace(string(out))
}
