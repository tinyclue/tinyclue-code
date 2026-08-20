package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	maxWorktreeSlugLength = 64
)

var validSlugSegment = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// WorktreeInfo holds information about a created agent worktree.
type WorktreeInfo struct {
	WorktreePath   string // Absolute path to the worktree directory
	WorktreeBranch string // Branch name created for the worktree
	HeadCommit     string // SHA of the HEAD commit at creation time
	GitRoot        string // Absolute path to the git repository root
}

// validateWorktreeSlug validates the worktree name/slug.
// Slugs may contain letters, digits, dots, underscores, and dashes.
// Multiple segments can be joined with "/" (e.g. "user/feature").
func validateWorktreeSlug(slug string) error {
	if len(slug) > maxWorktreeSlugLength {
		return fmt.Errorf("invalid worktree name: must be %d characters or fewer (got %d)",
			maxWorktreeSlugLength, len(slug))
	}

	for _, segment := range strings.Split(slug, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid worktree name %q: must not contain \".\" or \"..\" path segments", slug)
		}
		if !validSlugSegment.MatchString(segment) {
			return fmt.Errorf("invalid worktree name %q: segment %q contains invalid characters "+
				"(only letters, digits, dots, underscores, and dashes are allowed)", slug, segment)
		}
	}
	return nil
}

// flattenSlug replaces "/" with "+" for safe use in branch names and directory paths.
func flattenSlug(slug string) string {
	return strings.ReplaceAll(slug, "/", "+")
}

// worktreeBranchName generates a branch name for the given slug.
func worktreeBranchName(slug string) string {
	return "worktree-" + flattenSlug(slug)
}

// findCanonicalGitRoot finds the main repository root, resolving through
// git worktrees. Uses git rev-parse --git-common-dir to find the shared
// .git directory, then returns its parent as the canonical root.
// Unlike findGitRoot (which returns the current worktree's root), this
// returns the same value regardless of which worktree you're in.
func findCanonicalGitRoot(startPath string) (string, error) {
	// --git-common-dir returns the shared .git directory.
	// In a normal repo: ".git"
	// In a worktree:    "/abs/path/to/main/.git"
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = startPath
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("not in a git repository: %w", err)
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		// Relative output (e.g. ".git") — resolve against startPath
		commonDir = filepath.Join(startPath, commonDir)
	}
	// commonDir = /path/to/main/.git
	commonDir = filepath.Clean(commonDir)

	if filepath.Base(commonDir) == ".git" {
		return filepath.Dir(commonDir), nil
	}
	// Bare repo: common dir isn't inside a working directory
	return commonDir, nil
}

// getHeadSha returns the SHA of HEAD in the given git repo.
func getHeadSha(gitRoot string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = gitRoot
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get HEAD SHA: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// CreateAgentWorktree creates a lightweight git worktree for a subagent.
// It creates the worktree at <gitRoot>/.tinyclue/worktrees/<slug>/.
// If the worktree already exists, it resumes it (fast path).
// cwd is the directory to use as the starting point for git root discovery.
// Hook-based creation is not supported — this uses native git worktrees only.
func CreateAgentWorktree(slug string, cwd string) (*WorktreeInfo, error) {
	// 1. Validate slug
	if err := validateWorktreeSlug(slug); err != nil {
		return nil, err
	}

	// 2. Find canonical git root (resolves through worktree .git pointers)
	gitRoot, err := findCanonicalGitRoot(cwd)
	if err != nil {
		return nil, err
	}
	fmt.Println(gitRoot)
	worktreePath := filepath.Join(gitRoot, ".tinyclue", "worktrees", flattenSlug(slug))
	fmt.Println(worktreePath)

	branchName := worktreeBranchName(slug)

	// 3. Check if worktree already exists by reading .git HEAD
	headFile := filepath.Join(worktreePath, ".git")
	if _, err := os.Stat(headFile); err == nil {
		// Worktree exists — fast resume path: get the current HEAD
		headSha, err := getHeadSha(worktreePath)
		if err != nil {
			return nil, fmt.Errorf("failed to resume existing worktree: %w", err)
		}
		return &WorktreeInfo{
			WorktreePath:   worktreePath,
			WorktreeBranch: branchName,
			HeadCommit:     headSha,
			GitRoot:        gitRoot,
		}, nil
	}

	// 4. Ensure the worktrees parent directory exists
	worktreesDir := filepath.Join(gitRoot, ".tinyclue", "worktrees")
	if err := os.MkdirAll(worktreesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worktrees directory: %w", err)
	}

	// 5. Get base SHA before creating worktree
	headSha, err := getHeadSha(gitRoot)
	if err != nil {
		return nil, err
	}

	// 6. Create the worktree
	// -B: create branch or reset if it already exists (handles orphan branches)
	cmd := exec.Command("git", "worktree", "add", "-B", branchName, worktreePath, "HEAD")
	cmd.Dir = gitRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("failed to create worktree: %s: %w", string(output), err)
	}

	return &WorktreeInfo{
		WorktreePath:   worktreePath,
		WorktreeBranch: branchName,
		HeadCommit:     headSha,
		GitRoot:        gitRoot,
	}, nil
}

// RemoveAgentWorktree removes a worktree created by CreateAgentWorktree.
// For git-based worktrees, removes the worktree directory, cleans up,
// and deletes the temporary worktree branch.
// Falls back to manual removal + git worktree prune for git versions < 2.17.
func RemoveAgentWorktree(info *WorktreeInfo) error {
	if info == nil || info.GitRoot == "" || info.WorktreePath == "" {
		return fmt.Errorf("invalid worktree info")
	}

	// Run from the main repo root, not the worktree (which we're about to delete)
	cmd := exec.Command("git", "worktree", "remove", "--force", info.WorktreePath)
	cmd.Dir = info.GitRoot
	err := cmd.Run()
	if err != nil {
		// Fallback for git < 2.17: manual directory removal + git worktree prune
		if err := os.RemoveAll(info.WorktreePath); err != nil {
			return fmt.Errorf("failed to remove worktree directory: %w", err)
		}
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = info.GitRoot
		_ = pruneCmd.Run() // best-effort
	}

	// Delete the temporary worktree branch
	if info.WorktreeBranch != "" {
		branchCmd := exec.Command("git", "branch", "-D", info.WorktreeBranch)
		branchCmd.Dir = info.GitRoot
		_ = branchCmd.Run() // best-effort; branch may already be gone
	}

	return nil
}

// HasWorktreeChanges checks whether a worktree has uncommitted changes or
// commits ahead of the given headCommit. Returns true if changes exist,
// or if the check itself fails (fail-closed — assumes changes on uncertainty).
func HasWorktreeChanges(worktreePath string, headCommit string) bool {
	// 1. Check for uncommitted changes (modified, staged, untracked)
	statusCmd := exec.Command("git", "status", "--porcelain")
	statusCmd.Dir = worktreePath
	statusOut, err := statusCmd.Output()
	if err != nil {
		return true
	}
	if len(strings.TrimSpace(string(statusOut))) > 0 {
		return true
	}

	// 2. Check for new commits ahead of the base
	revCmd := exec.Command("git", "rev-list", "--count", headCommit+"..HEAD")
	revCmd.Dir = worktreePath
	revOut, err := revCmd.Output()
	if err != nil {
		return true
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(revOut)))
	if err != nil {
		return true
	}
	return count > 0
}
