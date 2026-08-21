package utils

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxSanitizedLength = 200

// SanitizePath sanitizes a path component for use as a directory name.
// Mirrors TS sanitizePath() in sessionStoragePortable.ts.
//
// 去掉开头的 "-"（来自绝对路径的前导 /），让目录名更干净：
//   - /Users/foo → -Users-foo → Users-foo
//
// 但根目录 "/" sanitize 后就是 "-"，去掉就空了 → 保留，兜底不为空，
// 避免项目目录退化成 <home>/projects/ 本身（与所有项目撞名）。
func SanitizePath(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9]`)
	sanitized := re.ReplaceAllString(name, "-")
	if len(sanitized) > 1 {
		sanitized = strings.TrimPrefix(sanitized, "-")
	}
	if len(sanitized) <= maxSanitizedLength {
		return sanitized
	}
	hash := simpleHash(name)
	return sanitized[:maxSanitizedLength] + "-" + hash
}

func simpleHash(name string) string {
	h := md5.Sum([]byte(name))
	return hex.EncodeToString(h[:])[:8]
}

// --- Git root detection ---

// findGitRoot walks up from startPath looking for a .git directory/file.
// Mirrors TS findGitRoot().
func FindGitRoot(startPath string) string {
	current, err := filepath.Abs(startPath)
	if err != nil {
		return ""
	}

	volumeName := filepath.VolumeName(current)
	root := volumeName + string(filepath.Separator)

	for {
		gitPath := filepath.Join(current, ".git")
		if info, err := os.Stat(gitPath); err == nil {
			if info.IsDir() || info.Mode().IsRegular() {
				return filepath.Clean(current)
			}
		}

		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
		if current == root {
			break
		}
	}

	// Check root too
	gitPath := filepath.Join(root, ".git")
	if info, err := os.Stat(gitPath); err == nil {
		if info.IsDir() || info.Mode().IsRegular() {
			return filepath.Clean(root)
		}
	}

	return ""
}

// findCanonicalGitRoot resolves a git root through worktrees to the main repo root.
// Mirrors TS findCanonicalGitRoot().
func FindCanonicalGitRoot(startPath string) string {
	gitRoot := FindGitRoot(startPath)
	if gitRoot == "" {
		return ""
	}

	// In a worktree, .git is a file containing "gitdir: <path>"
	gitDotFile := filepath.Join(gitRoot, ".git")
	info, err := os.Stat(gitDotFile)
	if err != nil || !info.Mode().IsRegular() {
		return gitRoot
	}

	data, err := os.ReadFile(gitDotFile)
	if err != nil {
		return gitRoot
	}
	content := strings.TrimSpace(string(data))
	if !strings.HasPrefix(content, "gitdir:") {
		return gitRoot
	}

	worktreeGitDir := filepath.Clean(strings.TrimSpace(content[len("gitdir:"):]))

	// Read commondir
	commondirData, err := os.ReadFile(filepath.Join(worktreeGitDir, "commondir"))
	if err != nil {
		return gitRoot
	}
	commonDir := filepath.Clean(filepath.Join(worktreeGitDir, strings.TrimSpace(string(commondirData))))

	// SECURITY: validate the worktree structure (same as TS)
	if filepath.Dir(worktreeGitDir) != filepath.Join(commonDir, "worktrees") {
		return gitRoot
	}

	// Check backlink
	gitdirData, err := os.ReadFile(filepath.Join(worktreeGitDir, "gitdir"))
	if err != nil {
		return gitRoot
	}
	backlink := filepath.Clean(strings.TrimSpace(string(gitdirData)))
	expectedBacklink := filepath.Join(gitRoot, ".git")
	if backlink != expectedBacklink {
		return gitRoot
	}

	// Bare-repo worktrees: common dir isn't inside a working directory
	if filepath.Base(commonDir) != ".git" {
		return commonDir
	}
	return filepath.Dir(commonDir)
}

func GetSanitizedPath(cwd string) string {
	// Try canonical git root first (all worktrees share memory)
	var basePath string
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	if gitRoot := FindCanonicalGitRoot(cwd); gitRoot != "" {
		basePath = gitRoot
	} else {
		basePath = cwd
	}
	sanitized := SanitizePath(basePath)
	return sanitized
}

// Default path: <tinyclueHomeDir>/projects/<sanitized-project>
func GetHomeProjectsPath(tinyclueHomeDir, cwd string) string {
	projectsDir := filepath.Join(tinyclueHomeDir, "projects")
	sanitized := GetSanitizedPath(cwd)
	return filepath.Join(projectsDir, sanitized)
}
