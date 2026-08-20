package usercontext

import (
	"regexp"
	"strings"
)

// --- YAML frontmatter parsing ---

var frontmatterRegex = regexp.MustCompile(`^---\s*\n([\s\S]*?)---\s*\n?`)

// FrontmatterData holds parsed YAML frontmatter fields relevant to memory files.
type FrontmatterData struct {
	Paths any `yaml:"paths"`
}

// ParsedMarkdown holds the result of parseFrontmatter.
type ParsedMarkdown struct {
	Frontmatter FrontmatterData
	Content     string
}

// parseFrontmatter extracts YAML frontmatter (--- delimited) from markdown content.
// Returns parsed frontmatter and content minus the frontmatter block.
func parseFrontmatter(markdown string) ParsedMarkdown {
	match := frontmatterRegex.FindStringSubmatch(markdown)
	if match == nil {
		return ParsedMarkdown{
			Frontmatter: FrontmatterData{},
			Content:     markdown,
		}
	}

	frontmatterText := match[1]
	content := markdown[len(match[0]):]

	fm := FrontmatterData{}
	// Simple YAML parsing for 'paths:' field
	// TS uses a full YAML parser; we do a simple line-by-line parse
	// since Go doesn't ship a YAML parser in stdlib.
	pathsValue := parseSimpleYAMLField(frontmatterText, "paths")
	if pathsValue != nil {
		fm.Paths = pathsValue
	}

	return ParsedMarkdown{
		Frontmatter: fm,
		Content:     content,
	}
}

// parseSimpleYAMLField does a minimal YAML kv-pair parse for a top-level key.
// Returns nil if the key is absent.
// Supports:
//
//	key: value            (single string)
//	key: [a, b, c]        (inline list)
//	key:\n  - a\n  - b    (block list)
func parseSimpleYAMLField(yamlText string, targetKey string) any {
	lines := strings.Split(yamlText, "\n")
	inBlockList := false
	var blockList []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Check for key: value on a single line
		if strings.HasPrefix(trimmed, targetKey+":") {
			rest := strings.TrimSpace(trimmed[len(targetKey)+1:])

			if rest == "" || rest == "|" {
				// Block list follows
				inBlockList = true
				blockList = nil
				continue
			}
			if strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]") {
				// Inline list: [a, b, c]
				inner := strings.TrimSpace(rest[1 : len(rest)-1])
				if inner == "" {
					return []string{}
				}
				var items []string
				for _, item := range strings.Split(inner, ",") {
					items = append(items, strings.TrimSpace(item))
				}
				return items
			}
			// Single value
			return rest
		}

		if inBlockList {
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-") {
				item := strings.TrimSpace(trimmed[1:])
				blockList = append(blockList, item)
			} else if trimmed == "" {
				continue
			} else {
				// End of block list
				inBlockList = false
			}
		}
	}

	if len(blockList) > 0 {
		return blockList
	}
	return nil
}

// splitPathInFrontmatter splits comma-separated paths and expands brace patterns,
// matching the TS splitPathInFrontmatter() in frontmatterParser.ts.
func splitPathInFrontmatter(input any) []string {
	switch v := input.(type) {
	case []string:
		var result []string
		for _, s := range v {
			result = append(result, splitPathInFrontmatter(s)...)
		}
		return result
	case string:
		return splitCommaSeparated(v)
	default:
		return nil
	}
}

// splitCommaSeparated splits by comma (respecting braces) and expands {...} patterns.
func splitCommaSeparated(input string) []string {
	var parts []string
	var current strings.Builder
	braceDepth := 0

	for _, ch := range input {
		switch ch {
		case '{':
			braceDepth++
			current.WriteRune(ch)
		case '}':
			braceDepth--
			current.WriteRune(ch)
		case ',':
			if braceDepth == 0 {
				trimmed := strings.TrimSpace(current.String())
				if trimmed != "" {
					parts = append(parts, trimmed)
				}
				current.Reset()
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	trimmed := strings.TrimSpace(current.String())
	if trimmed != "" {
		parts = append(parts, trimmed)
	}

	// Expand brace patterns
	var result []string
	for _, p := range parts {
		if p != "" {
			result = append(result, expandBraces(p)...)
		}
	}
	return result
}

// expandBraces expands {a,b} patterns in a glob string.
// e.g. "src/*.{ts,tsx}" -> ["src/*.ts", "src/*.tsx"]
func expandBraces(pattern string) []string {
	re := regexp.MustCompile(`^([^{]*)\{([^}]+)\}(.*)$`)
	match := re.FindStringSubmatch(pattern)
	if match == nil {
		return []string{pattern}
	}

	prefix := match[1]
	alternatives := match[2]
	suffix := match[3]

	var result []string
	for _, alt := range strings.Split(alternatives, ",") {
		alt = strings.TrimSpace(alt)
		combined := prefix + alt + suffix
		// Recurse for additional brace groups
		result = append(result, expandBraces(combined)...)
	}
	return result
}

// --- HTML comment stripping ---

// stripHtmlComments removes block-level HTML comments (<!-- ... -->) from markdown.
// Only strips comments that appear as block-level HTML tokens (own lines).
// Unclosed comments are left in place.
func stripHtmlComments(content string) (string, bool) {
	if !strings.Contains(content, "<!--") {
		return content, false
	}

	// Simple approach: split into lines and process.
	// A block-level HTML comment in CommonMark starts on its own line.
	lines := strings.Split(content, "\n")
	var result []string
	stripped := false
	inComment := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if inComment {
			// Check if this line ends the comment
			if strings.Contains(trimmed, "-->") {
				inComment = false
				// Keep anything after -->
				idx := strings.Index(trimmed, "-->")
				residue := strings.TrimSpace(trimmed[idx+3:])
				if residue != "" {
					// Pre-comment whitespace from original line
					lineBefore := line[:len(line)-len(trimmed)]
					result = append(result, lineBefore+residue)
				}
				stripped = true
			}
			// Still in comment, skip line
			continue
		}

		// Check if line starts an HTML comment block
		if strings.HasPrefix(trimmed, "<!--") && strings.Contains(trimmed, "-->") {
			// Single-line comment: <!-- ... -->
			stripped = true
			// Keep residue after -->
			idx := strings.Index(trimmed, "-->")
			residue := strings.TrimSpace(trimmed[idx+3:])
			if residue != "" {
				lineBefore := line[:len(line)-len(trimmed)]
				result = append(result, lineBefore+residue)
			}
			continue
		}

		if strings.HasPrefix(trimmed, "<!--") {
			// Multi-line comment starts here
			inComment = true
			stripped = true
			continue
		}

		result = append(result, line)
	}

	// If unclosed comment, keep the line (matching TS behavior)
	if inComment {
		// We consumed lines that were part of the comment.
		// TS leaves unclosed comments in place, so we need to undo.
		// Since this is the unclosed case, we just return original content.
		return content, false
	}

	return strings.Join(result, "\n"), stripped
}

// --- @include directive extraction ---

// extractIncludePaths extracts @path references from markdown text content.
// Skips code blocks and code spans. Only extracts from regular text nodes.
// Mirrors TS extractIncludePathsFromTokens().
func extractIncludePaths(content string, basePath string) []string {
	// We don't have a full markdown parser, so we use a simpler approach:
	// split into blocks (fenced code blocks, inline code, and regular text),
	// and only extract @paths from non-code text.
	absolutePaths := make(map[string]bool)

	// Remove fenced code blocks first
	cleaned := removeFencedCodeBlocks(content)

	// Then remove inline code spans
	cleaned = removeInlineCodeSpans(cleaned)

	// Extract @paths from remaining text
	extractPathsFromText(cleaned, basePath, absolutePaths)

	var result []string
	for p := range absolutePaths {
		result = append(result, p)
	}
	return result
}

// removeFencedCodeBlocks removes ``` fences and their content.
func removeFencedCodeBlocks(content string) string {
	var result strings.Builder
	inBlock := false
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inBlock = !inBlock
			continue
		}
		if !inBlock {
			result.WriteString(line)
			result.WriteString("\n")
		}
	}
	return strings.TrimRight(result.String(), "\n")
}

// removeInlineCodeSpans removes `inline code` spans.
func removeInlineCodeSpans(content string) string {
	// Simple backtick removal - replaces `...` with spaces
	var result strings.Builder
	inCode := false
	for i := 0; i < len(content); i++ {
		if content[i] == '`' {
			inCode = !inCode
			continue
		}
		if !inCode {
			result.WriteByte(content[i])
		}
	}
	return result.String()
}

// extractPathsFromText finds @path patterns in text content.
// Supports @path, @./path, @~/path, @/absolute/path.
func extractPathsFromText(text string, basePath string, paths map[string]bool) {
	re := regexp.MustCompile(`(?:^|\s)@((?:[^\s\\]|\\ )+)`)
	matches := re.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		path := match[1]

		// Strip fragment identifiers (#heading)
		if idx := strings.Index(path, "#"); idx != -1 {
			path = path[:idx]
		}
		if path == "" {
			continue
		}
		// Unescape spaces
		path = strings.ReplaceAll(path, "\\ ", " ")

		// Validate path format matching TS:
		// @path, @./path, @~/path, @/path
		isValid := false
		switch {
		case strings.HasPrefix(path, "./"):
			isValid = true
		case strings.HasPrefix(path, "~/"):
			isValid = true
		case strings.HasPrefix(path, "/") && path != "/":
			isValid = true
		case !strings.HasPrefix(path, "@") &&
			!regexp.MustCompile(`^[#%^&*()]+`).MatchString(path) &&
			regexp.MustCompile(`^[a-zA-Z0-9._-]`).MatchString(path):
			isValid = true
		}

		if isValid {
			resolvedPath := expandPath(path, basePath)
			paths[resolvedPath] = true
		}
	}
}
