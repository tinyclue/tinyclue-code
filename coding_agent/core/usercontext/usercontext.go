// Package usercontext implements TINYCLUE.md discovery and merging logic,
// mirroring the TypeScript implementation in src/context.ts, src/utils/tinycluemd.ts,
// src/memdir/paths.ts, and src/utils/api.ts.
//
// It walks directories upward from CWD to root, discovers TINYCLUE.md files at
// each level, resolves @include directives, processes frontmatter, strips HTML
// comments, and merges them into a "virtual user message" prepended to the
// conversation -- providing project-specific instructions to the AI.
package usercontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FileSource indicates where a TINYCLUE.md file comes from.
type FileSource string

const (
	SourceManaged FileSource = "Managed" // Policy-managed (global config)
	SourceUser    FileSource = "User"    // ~/.tinyclue/TINYCLUE.md
	SourceProject FileSource = "Project" // Project-level TINYCLUE.md (from dir walk)
	SourceLocal   FileSource = "Local"   // TINYCLUE.local.md (gitignored, local-only)
	SourceAutoMem FileSource = "AutoMem" // Auto-memory entrypoint (~/.tinyclue/projects/.../memory/MEMORY.md)
)

// UserContext is the result of collecting TINYCLUE.md files and current date.
// It mirrors the TS return type: `{ [k: string]: string }`.
type UserContext struct {
	TinyclueMd  string `json:"tinyclueMd,omitempty"`
	CurrentDate string `json:"currentDate"`
}

// Config 控制 TINYCLUE.md 的发现和加载行为。
// 对应 TS 中 getUserContext() 的选项参数，见 src/context.ts。
//
// 各层级的路径字段为非空时即启用该层级。
// 使用 GetDefaultConfig() 获取所有层级默认启用的配置。
type Config struct {

	// Disabled 为 true 时完全禁用所有 TINYCLUE.md 发现（逃生舱）。
	// 对应 TS 的 TINYCLUE_CODE_DISABLE_TINYCLUE_MDS 环境变量。
	Disabled bool

	// HomeDir 覆盖用户家目录（为空则使用 os.UserHomeDir）。
	HomeDir string

	// TinyclueHomeDir overrides TINYCLUE_CONFIG_DIR and default $HOME/.tinyclue.
	TinyclueHomeDir string

	// CWD 覆盖当前工作目录（为空则使用 os.Getwd）。
	CWD string

	// TinyclueMdExternalIncludes 允许 @include 引用 CWD 外部的文件。
	// 对应 TS 的 includeExternal 标志。
	TinyclueMdExternalIncludes bool

	// --- 7 层级路径配置（优先级从低到高）---
	// 各字段为空时使用硬编码默认路径。

	// 1. Managed 层：托管策略文件路径
	// 默认: $HOME/.tinyclue/managed/TINYCLUE.md
	ManagedTINYCLUE string

	// 1. Managed 层：托管策略 rules 目录
	// 默认: $HOME/.tinyclue/managed/rules
	ManagedRulesDir string

	// 2. User 层：用户级 TINYCLUE.md 路径
	// 默认: $HOME/.tinyclue/TINYCLUE.md
	UserTINYCLUE string

	// 2. User 层：用户级 rules 目录
	// 默认: $HOME/.tinyclue/rules
	UserRulesDir string

	// 3. Project 层：在目录向上遍历中查找的文件名列表
	// 默认: ["TINYCLUE.md", ".tinyclue/TINYCLUE.md"]
	ProjectFiles []string

	// 3. Project 层：项目级 rules 目录名（相对路径）
	// 默认: ".tinyclue/rules"
	ProjectRulesDir string

	// 4. Local 层：本地私有文件名列表（相对路径）
	// 默认: ["TINYCLUE.local.md"]
	LocalFiles []string

	// 5. AddDirectories（额外搜索目录，已有上面的 AddDirectories 字段）
	AddDirectories []string

	// 6. AutoMem 层：自动记忆 MEMORY.md 的完整路径。
	// 设此字段会跳过环境变量覆盖和默认路径计算逻辑，直接使用此路径。
	// 为空时使用默认计算路径（参见 GetUserContext 文档）。
	AutoMemEntrypoint string

	// 7. TeamMem 层：团队共享记忆 MEMORY.md 的完整路径。
	// 默认: 空（需显式配置）。
	TeamMemEntrypoint string
}

// GetDefaultConfig 返回默认配置，所有层级路径使用标准默认值。
// 用户可按需修改各层路径后传入 GetUserContext。
func GetDefaultConfig() Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	cwd, err2 := os.Getwd()
	if err2 != nil {
		cwd = ""
	}

	tinyclueHome := GetTinyclueConfigHomeDir()

	cfg := Config{
		Disabled:                   false,
		HomeDir:                    homeDir,
		TinyclueHomeDir:            GetTinyclueConfigHomeDir(),
		CWD:                        cwd,
		TinyclueMdExternalIncludes: false,

		// 1. Managed
		ManagedTINYCLUE: filepath.Join(tinyclueHome, "managed", "TINYCLUE.md"),
		ManagedRulesDir: filepath.Join(tinyclueHome, "managed", "rules"),

		// 2. User
		UserTINYCLUE: filepath.Join(tinyclueHome, "TINYCLUE.md"),
		UserRulesDir: filepath.Join(tinyclueHome, "rules"),

		// 3. Project
		ProjectFiles:    []string{"TINYCLUE.md", ".tinyclue/TINYCLUE.md"},
		ProjectRulesDir: ".tinyclue/rules",

		// 4. Local
		LocalFiles: []string{"TINYCLUE.local.md"},

		// 5.AddDirectories（额外搜索目录)
		AddDirectories: []string{},

		// 6. AutoMem
		AutoMemEntrypoint: GetAutoMemEntrypoint(tinyclueHome, cwd),

		// 7. TeamMem
		TeamMemEntrypoint: getTeamMemEntrypoint(tinyclueHome, cwd),
	}
	return cfg
}

// getHomeDir resolves the home directory for config file lookups.
func (cfg Config) getHomeDir() string {
	if cfg.HomeDir != "" {
		return cfg.HomeDir
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// getTinyclueHomeDir returns the Tinyclue config home directory.
// Uses TinyclueHomeDir override, then TINYCLUE_CONFIG_DIR env var, then $HOME/.tinyclue.
func (cfg Config) getTinyclueHomeDir() string {
	if cfg.TinyclueHomeDir != "" {
		return cfg.TinyclueHomeDir
	}
	return GetTinyclueConfigHomeDir()
}

// GetUserContext 收集所有 TINYCLUE.md 及记忆文件，返回合并后的用户上下文。
// 对应 TS 的 getUserContext()，见 src/context.ts。
//
// 扫描目录和文件如下（按优先级从低到高）：
//
//  1. Managed（托管规则）
//     - cfg.ManagedTINYCLUE（默认 $HOME/.tinyclue/managed/TINYCLUE.md）
//     - cfg.ManagedRulesDir/*.md（默认 $HOME/.tinyclue/managed/rules/*.md）
//     由企业策略自动注入，始终加载。
//
//  2. User（用户级全局配置）
//     - cfg.UserTINYCLUE（默认 $HOME/.tinyclue/TINYCLUE.md）
//     - cfg.UserRulesDir/*.md（默认 $HOME/.tinyclue/rules/*.md）
//
//  3. Project（项目级，逐层向上扫描）
//     从 CWD 开始逐层向根目录遍历，每层检查：
//     - cfg.ProjectFiles（默认 ["TINYCLUE.md", ".tinyclue/TINYCLUE.md"]）
//     - cfg.ProjectRulesDir/*.md（默认 ".tinyclue/rules"）
//     遇到 worktree 时通过 findCanonicalGitRoot 去重：
//     如果 worktree 的 TINYCLUE.md 与主仓库的相同则跳过。
//
//  4. Local（本地私有项目配置）
//     与 Project 同路径扫描：
//     - cfg.LocalFiles（默认 ["TINYCLUE.local.md"]）
//
//  5. AddDirectories（额外目录）
//     - cfg.AddDirectories 中指定的目录下的 TINYCLUE.md/.tinyclue/TINYCLUE.md
//
//  6. AutoMem（跨会话自动记忆，cfg.AutoMemEntrypoint）
//     当 cfg.AutoMemEntrypoint 非空时直接使用该路径；
//     否则使用计算出的默认路径：
//     <homeDir>/.tinyclue/projects/<sanitized-path>/memory/MEMORY.md
//     其中 sanitized-path = 规范化 git 根路径（worktree 感知）或 CWD，经 sanitize 处理
//     最终指向 MEMORY.md（入口索引文件）+ *.md（具体记忆文件）
//
//  7. TeamMem（团队共享记忆，cfg.TeamMemEntrypoint）
//     - cfg.TeamMemEntrypoint 指向的 MEMORY.md（需显式配置路径）
//
// 每个文件的处理管线（见 processMemoryFile → parseMemoryFileContent）：
//   - 提取 YAML frontmatter（paths 字段用于条件规则匹配）
//   - 递归解析 @include 指令（最大深度 5，只引入文本文件白名单扩展名）
//   - 剥离 HTML 注释（<!-- ... -->）
//   - MEMORY.md 按 200 行 / 25000 字节截断
//   - .tinyclue/rules/*.md 中 paths glob 与目标文件路径不匹配时跳过
//
// 最终依次通过 getTinyclueMds 格式化和 PrependUserContext 包装为
// <system-reminder> 块注入到对话的上下文中。
//
// 使用 cfg.Disabled = true 可完全禁用所有 TINYCLUE.md 发现。
func GetUserContext(cfg Config) (*UserContext, error) {
	var tinyclueMd string

	// 全局禁用
	if !cfg.Disabled {
		files, err := getMemoryFiles(cfg)
		if err != nil {
			return nil, fmt.Errorf("get memory files: %w", err)
		}

		tinyclueMd = getTinyclueMds(files)
	}

	// Returns local date in YYYY/MM/DD format matching TS getLocalISODate()
	now := time.Now()
	localDate := now.Format("2006/01/02")

	ctx := &UserContext{
		TinyclueMd:  tinyclueMd,
		CurrentDate: fmt.Sprintf("Today's date is %s.", localDate),
	}
	return ctx, nil
}

// PrependUserContext formats the user context as a virtual user message
// to be prepended to the conversation. Mirrors prependUserContext() in api.ts.
// Wraps the content in <system-reminder> tags matching TS behavior.
func PrependUserContext(ctx *UserContext) string {
	var entries []string
	if ctx.TinyclueMd != "" {
		entries = append(entries, "# tinyclueMd\n"+ctx.TinyclueMd)
	}
	entries = append(entries, "# currentDate\n"+ctx.CurrentDate)

	if len(entries) == 0 {
		return ""
	}

	return "<system-reminder>\nAs you answer the user's questions, you can use the following context:\n" +
		strings.Join(entries, "\n") +
		"\n\nIMPORTANT: this context may or may not be relevant to your tasks. You should not respond to this context unless it is highly relevant to your task.\n</system-reminder>"
}
