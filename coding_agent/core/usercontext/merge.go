package usercontext

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MEMORY_INSTRUCTION_PROMPT matches the TS constant in tinycluemd.ts.
const memoryInstructionPrompt = "Codebase and user instructions are shown below. Be sure to adhere to these instructions. IMPORTANT: These instructions OVERRIDE any default behavior and you MUST follow them exactly as written."

// getTinyclueMds formats memory files into a single merged string.
// Mirrors TS getTinyclueMds() in tinycluemd.ts.
func getTinyclueMds(files []MemoryFileInfo) string {
	var memories []string

	for _, file := range files {
		content := strings.TrimSpace(file.Content)
		if content == "" {
			continue
		}

		var description string
		switch file.Type {
		case MemoryTypeProject:
			description = " (project instructions, checked into the codebase)"
		case MemoryTypeLocal:
			description = " (user's private project instructions, not checked in)"
		case MemoryTypeTeamMem:
			description = " (shared team memory, synced across the organization)"
		case MemoryTypeAutoMem:
			description = " (user's auto-memory, persists across conversations)"
		default:
			description = " (user's private global instructions for all projects)"
		}

		var entry string
		if file.Type == MemoryTypeTeamMem {
			entry = fmt.Sprintf("Contents of %s%s:\n\n<team-memory-content source=\"shared\">\n%s\n</team-memory-content>",
				file.Path, description, content)
		} else {
			entry = fmt.Sprintf("Contents of %s%s:\n\n%s", file.Path, description, content)
		}
		memories = append(memories, entry)
	}

	if len(memories) == 0 {
		return ""
	}

	return memoryInstructionPrompt + "\n\n" + strings.Join(memories, "\n\n")
}

//
//// mergeTinyclueMds combines all discovered TINYCLUE.md files into a single string,
//// matching the TS format from getTinyclueMds() in src/utils/tinycluemd.ts.
//// This is the legacy format used by the original Go implementation.
//// Callers should prefer getTinyclueMds() for full TS compatibility.
//func mergeTinyclueMds(files []MemoryFileInfo, _ Config) string {
//	if len(files) == 0 {
//		return ""
//	}
//
//	// Group by type, preserving order
//	type entry struct {
//		typ  MemoryType
//		file MemoryFileInfo
//	}
//
//	var entries []entry
//	for _, f := range files {
//		entries = append(entries, entry{typ: f.Type, file: f})
//	}
//
//	// Sort by priority (same as TS priority order)
//	sort.Slice(entries, func(i, j int) bool {
//		return typePriority(entries[i].typ) < typePriority(entries[j].typ)
//	})
//
//	var parts []string
//	for _, e := range entries {
//		data, err := os.ReadFile(e.file.Path)
//		if err != nil {
//			continue
//		}
//		content := strings.TrimSpace(string(data))
//		if content == "" {
//			continue
//		}
//
//		var description string
//		switch e.typ {
//		case MemoryTypeProject:
//			description = " (project instructions, checked into the codebase)"
//		case MemoryTypeLocal:
//			description = " (user's private project instructions, not checked in)"
//		case MemoryTypeAutoMem:
//			description = " (user's auto-memory, persists across conversations)"
//		default:
//			description = " (user's private global instructions for all projects)"
//		}
//
//		// TS output format:
//		// Contents of <path><description>:\n\n<content>
//		entry := fmt.Sprintf("Contents of %s%s:\n\n%s", e.file.Path, description, content)
//		parts = append(parts, entry)
//	}
//
//	if len(parts) == 0 {
//		return ""
//	}
//
//	return memoryInstructionPrompt + "\n\n" + strings.Join(parts, "\n\n")
//}

// typePriority returns the priority order for memory types (lower = loaded first = lower priority).
func typePriority(t MemoryType) int {
	switch t {
	case MemoryTypeManaged:
		return 0
	case MemoryTypeUser:
		return 1
	case MemoryTypeProject:
		return 2
	case MemoryTypeLocal:
		return 3
	case MemoryTypeAutoMem:
		return 4
	case MemoryTypeTeamMem:
		return 5
	default:
		return 99
	}
}

// priority is the legacy priority function for FileSource.
func priority(src FileSource) int {
	switch src {
	case SourceManaged:
		return 0
	case SourceUser:
		return 1
	case SourceProject:
		return 2
	case SourceLocal:
		return 3
	case SourceAutoMem:
		return 4
	default:
		return 99
	}
}

// processFile is a legacy helper that reads a single TINYCLUE.md file.
// Returns nil if the file doesn't exist.
func processFile(path string, source FileSource, processed map[string]bool) (*MemoryFileInfo, error) {
	clean, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if processed[clean] {
		return nil, nil
	}

	info, err := os.Stat(clean)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("not found: %s", clean)
	}

	processed[clean] = true
	return &MemoryFileInfo{
		Path:   clean,
		Source: source,
		Type:   sourceToType(source),
		Dir:    filepath.Dir(clean),
	}, nil
}

// processRulesDir is a legacy helper that reads .md files from a rules directory.
func processRulesDir(rulesDir string, source FileSource, processed map[string]bool) ([]MemoryFileInfo, error) {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil, err
	}

	var result []MemoryFileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		fullPath := filepath.Join(rulesDir, entry.Name())
		if info, err := processFile(fullPath, source, processed); err == nil && info != nil {
			result = append(result, *info)
		}
	}
	return result, nil
}
