package usercontext

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"strings"
)

// --- Path utilities ---

// normalizePathForComparison normalizes a path for comparison.
// Mirrors TS normalizePathForComparison() in file.ts.
func normalizePathForComparison(filePath string) string {
	cleaned := filepath.Clean(filePath)
	// On case-insensitive platforms (darwin, windows), lowercase
	// We lowercase always since Go can run on any platform
	return strings.ToLower(cleaned)
}

// pathInWorkingPath checks if a path is inside a working path.
// Mirrors TS pathInWorkingPath() in permissions/filesystem.ts.
func pathInWorkingPath(path, workingPath string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	absWorkingPath, err := filepath.Abs(workingPath)
	if err != nil {
		return false
	}

	// On macOS, handle /var -> /private/var and /tmp -> /private/tmp symlinks
	absPath = normalizeMacOSSymlinks(absPath)
	absWorkingPath = normalizeMacOSSymlinks(absWorkingPath)

	normPath := strings.ToLower(filepath.Clean(absPath))
	normWorking := strings.ToLower(filepath.Clean(absWorkingPath))

	rel, err := filepath.Rel(normWorking, normPath)
	if err != nil {
		return false
	}

	// Same path or inside
	return rel == "." || !strings.HasPrefix(rel, "..")
}

func normalizeMacOSSymlinks(p string) string {
	p = strings.Replace(p, "/private/var/", "/var/", 1)
	if strings.HasPrefix(p, "/private/tmp") {
		p = "/tmp" + p[len("/private/tmp"):]
	}
	return p
}

// --- AutoMem path computation ---

// autoMemConstants mirrors the TS constants.
const (
	autoMemDirname        = "memory"
	autoMemEntrypointName = "MEMORY.md"

	maxEntrypointLines = 200
	maxEntrypointBytes = 25000
)

// getAutoMemPath returns the auto-memory directory path.
// Mirrors TS getAutoMemPath() in memdir/paths.ts.
//
// Computed default path:
//
//	<tinyclueHomeDir>/projects/<sanitized-git-or-cwd>/memory/
func GetAutoMemPath(tinyclueHomeDir, cwd string) string {
	projectsPath := utils.GetHomeProjectsPath(tinyclueHomeDir, cwd)
	autoMemDir := filepath.Join(projectsPath, autoMemDirname)
	// Add trailing separator for compatibility with path prefix checks
	return autoMemDir + string(filepath.Separator)
}

// getAutoMemEntrypoint returns the MEMORY.md path inside the auto-memory dir.
// Mirrors TS getAutoMemEntrypoint().
func GetAutoMemEntrypoint(tinyclueHomeDir, cwd string) string {
	return filepath.Join(GetAutoMemPath(tinyclueHomeDir, cwd), autoMemEntrypointName)
}

// getTeamMemEntrypoint returns the team memory MEMORY.md path.
// Mirrors TS getTeamMemEntrypoint() in memdir/teamMemPaths.ts.
//
// Default path: <tinyclueHomeDir>/projects/<sanitized-project>/memory/team/MEMORY.md
func getTeamMemEntrypoint(tinyclueHomeDir, cwd string) string {
	return filepath.Join(GetAutoMemPath(tinyclueHomeDir, cwd), "team", autoMemEntrypointName)
}

// validateMemoryPath checks a candidate auto-memory path for safety.
// Returns empty string for invalid paths.
// Mirrors TS validateMemoryPath() in memdir/paths.ts.
func validateMemoryPath(raw string, expandTilde bool) string {
	if raw == "" {
		return ""
	}

	candidate := raw

	if expandTilde && (strings.HasPrefix(candidate, "~/") || strings.HasPrefix(candidate, "~\\")) {
		rest := candidate[2:]
		cleaned := filepath.Clean(rest)
		if cleaned == "." || cleaned == ".." {
			return ""
		}
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		candidate = filepath.Join(homeDir, rest)
	}

	cleaned := filepath.Clean(candidate)

	// Reject relative paths, root-level paths, UNC paths, null bytes
	if !filepath.IsAbs(cleaned) {
		return ""
	}
	if len(cleaned) < 3 {
		return ""
	}
	if strings.HasPrefix(cleaned, "\\\\") || strings.HasPrefix(cleaned, "//") {
		return ""
	}
	if strings.ContainsRune(cleaned, 0) {
		return ""
	}

	return cleaned + string(filepath.Separator)
}

// expandPath resolves @include paths.
// Supports: ~/path, ./path, /absolute/path, and bare relative paths.
func expandPath(path, baseDir string) string {
	switch {
	case strings.HasPrefix(path, "~/"):
		homeDir, err := os.UserHomeDir()
		if err != nil {
			// Fall through to filepath.Join with ~ as literal
			return filepath.Join(baseDir, path)
		}
		return filepath.Join(homeDir, path[2:])
	case strings.HasPrefix(path, "/"):
		return filepath.Clean(path)
	case strings.HasPrefix(path, "./"):
		return filepath.Clean(filepath.Join(baseDir, path))
	default:
		// Bare relative path — treat same as ./path (TS behavior)
		return filepath.Clean(filepath.Join(baseDir, path))
	}
}

// entrypointTruncation holds truncation results for MEMORY.md.
type entrypointTruncation struct {
	content          string
	lineCount        int
	byteCount        int
	wasLineTruncated bool
	wasByteTruncated bool
}

// truncateEntrypointContent truncates MEMORY.md content to line AND byte caps.
// Mirrors TS truncateEntrypointContent() in memdir/memdir.ts.
func truncateEntrypointContent(raw string) entrypointTruncation {
	trimmed := strings.TrimSpace(raw)
	contentLines := strings.Split(trimmed, "\n")
	lineCount := len(contentLines)
	byteCount := len(trimmed)

	wasLineTruncated := lineCount > maxEntrypointLines
	wasByteTruncated := byteCount > maxEntrypointBytes

	if !wasLineTruncated && !wasByteTruncated {
		return entrypointTruncation{
			content:   trimmed,
			lineCount: lineCount, byteCount: byteCount,
			wasLineTruncated: false, wasByteTruncated: false,
		}
	}

	truncated := trimmed
	if wasLineTruncated {
		truncated = strings.Join(contentLines[:maxEntrypointLines], "\n")
	}

	if len(truncated) > maxEntrypointBytes {
		cutAt := strings.LastIndex(truncated[:maxEntrypointBytes], "\n")
		if cutAt > 0 {
			truncated = truncated[:cutAt]
		} else {
			truncated = truncated[:maxEntrypointBytes]
		}
	}

	var reason string
	switch {
	case wasByteTruncated && !wasLineTruncated:
		reason = fmt.Sprintf("%d bytes (limit: %d) — index entries are too long", byteCount, maxEntrypointBytes)
	case wasLineTruncated && !wasByteTruncated:
		reason = fmt.Sprintf("%d lines (limit: %d)", lineCount, maxEntrypointLines)
	default:
		reason = fmt.Sprintf("%d lines and %d bytes", lineCount, byteCount)
	}

	return entrypointTruncation{
		content: truncated + "\n\n> WARNING: " + autoMemEntrypointName +
			" is " + reason + ". Only part of it was loaded. Keep index entries to one line under ~200 chars; move detail into topic files.\n",
		lineCount: lineCount, byteCount: byteCount,
		wasLineTruncated: wasLineTruncated, wasByteTruncated: wasByteTruncated,
	}
}
