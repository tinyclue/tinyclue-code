package prompt

import (
	"fmt"

	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func GetGrepPrompt() string {
	return fmt.Sprintf(`A powerful search tool built on ripgrep

Usage:
- ALWAYS use %[1]s for search tasks. NEVER invoke `+"`grep`"+` or `+"`rg`"+` as a %[2]s command. The %[1]s tool has been optimized for correct permissions and access.
- Supports full regex syntax (e.g., "log.*Error", "function\s+\w+")
- Filter files with glob parameter (e.g., "*.js", "**/*.tsx") or type parameter (e.g., "js", "py", "rust")
- Output modes: "content" shows matching lines, "files_with_matches" shows only file paths (default), "count" shows match counts
- Use %[3]s tool for open-ended searches requiring multiple rounds
- Pattern syntax: Uses ripgrep (not grep) - literal braces need escaping (use `+"`interface\\{\\}`"+` to find `+"`interface{}`"+` in Go code)
- Multiline matching: By default patterns match within single lines only. For cross-line patterns like `+"`struct \\{[\\s\\S]*?field`"+`, use `+"`multiline: true`"+``,
		core_types.GREP_TOOL_NAME, core_types.BASH_TOOL_NAME, core_types.RUN_AGENT_TOOL_NAME)
}
