package usercontext

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/config"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// --- Types ---

// MemoryType indicates the category of a memory file, matching TS MemoryType.
type MemoryType string

const (
	MemoryTypeManaged MemoryType = "Managed"
	MemoryTypeUser    MemoryType = "User"
	MemoryTypeProject MemoryType = "Project"
	MemoryTypeLocal   MemoryType = "Local"
	MemoryTypeAutoMem MemoryType = "AutoMem"
	MemoryTypeTeamMem MemoryType = "TeamMem"
)

// MemoryFileInfo holds info about a discovered memory file.
// Extended version matching TS MemoryFileInfo.
type MemoryFileInfo struct {
	Path                   string
	Source                 FileSource // Deprecated: use Type
	Type                   MemoryType
	Dir                    string
	Content                string
	Globs                  []string // Frontmatter glob patterns for conditional rules
	Parent                 string   // Path of the file that @included this one
	ContentDiffersFromDisk bool
	RawContent             string
}

// sourceToType maps FileSource to MemoryType.
func sourceToType(src FileSource) MemoryType {
	switch src {
	case SourceManaged:
		return MemoryTypeManaged
	case SourceUser:
		return MemoryTypeUser
	case SourceProject:
		return MemoryTypeProject
	case SourceLocal:
		return MemoryTypeLocal
	case SourceAutoMem:
		return MemoryTypeAutoMem
	default:
		return MemoryTypeProject
	}
}

// typeToSource maps MemoryType to FileSource.
func typeToSource(t MemoryType) FileSource {
	switch t {
	case MemoryTypeManaged:
		return SourceManaged
	case MemoryTypeUser:
		return SourceUser
	case MemoryTypeProject:
		return SourceProject
	case MemoryTypeLocal:
		return SourceLocal
	case MemoryTypeAutoMem:
		return SourceAutoMem
	case MemoryTypeTeamMem:
		return SourceManaged // TeamMem maps to Managed source for priority
	default:
		return SourceProject
	}
}

// maxIncludeDepth matches TS MAX_INCLUDE_DEPTH.
const maxIncludeDepth = 5

// maxMemoryCharCount matches TS MAX_MEMORY_CHARACTER_COUNT.
const maxMemoryCharCount = 40000

// textFileExtensions matches TS TEXT_FILE_EXTENSIONS set.
var textFileExtensions = map[string]bool{
	".md": true, ".txt": true, ".text": true,
	".json": true, ".yaml": true, ".yml": true, ".toml": true, ".xml": true, ".csv": true,
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true, ".less": true,
	".js": true, ".ts": true, ".tsx": true, ".jsx": true, ".mjs": true, ".cjs": true,
	".mts": true, ".cts": true,
	".py": true, ".pyi": true, ".pyw": true,
	".rb": true, ".erb": true, ".rake": true,
	".go":   true,
	".rs":   true,
	".java": true, ".kt": true, ".kts": true, ".scala": true,
	".c": true, ".cpp": true, ".cc": true, ".cxx": true, ".h": true, ".hpp": true, ".hxx": true,
	".cs":    true,
	".swift": true,
	".sh":    true, ".bash": true, ".zsh": true, ".fish": true, ".ps1": true, ".bat": true, ".cmd": true,
	".env": true, ".ini": true, ".cfg": true, ".conf": true, ".config": true, ".properties": true,
	".sql": true, ".graphql": true, ".gql": true,
	".proto": true,
	".vue":   true, ".svelte": true, ".astro": true,
	".ejs": true, ".hbs": true, ".pug": true, ".jade": true,
	".php": true, ".pl": true, ".pm": true, ".lua": true, ".r": true, ".R": true, ".dart": true,
	".ex": true, ".exs": true, ".erl": true, ".hrl": true, ".clj": true, ".cljs": true, ".cljc": true, ".edn": true,
	".hs": true, ".lhs": true, ".elm": true, ".ml": true, ".mli": true,
	".f": true, ".f90": true, ".f95": true, ".for": true,
	".cmake": true, ".make": true, ".makefile": true, ".gradle": true, ".sbt": true,
	".rst": true, ".adoc": true, ".asciidoc": true, ".org": true, ".tex": true, ".latex": true,
	".lock": true,
	".log":  true, ".diff": true, ".patch": true,
}

// --- Core discovery logic ---

// getMemoryFiles discovers all memory files from all sources.
// Full implementation matching TS getMemoryFiles() in tinycluemd.ts.
func getMemoryFiles(cfg Config) ([]MemoryFileInfo, error) {
	var result []MemoryFileInfo
	processedPaths := make(map[string]bool)
	includeExternal := cfg.TinyclueMdExternalIncludes

	tinyclueHome := cfg.getTinyclueHomeDir()
	homeDir := cfg.getHomeDir()

	// Default paths for layers that support fallback
	defaultManagedTINYCLUE := filepath.Join(tinyclueHome, "managed", "TINYCLUE.md")
	defaultManagedRules := filepath.Join(tinyclueHome, "managed", "rules")
	defaultUserTINYCLUE := filepath.Join(tinyclueHome, "TINYCLUE.md")
	defaultUserRules := filepath.Join(tinyclueHome, "rules")

	// 1. Managed TINYCLUE.md (always loaded - policy settings)
	managedFile := layerPath(cfg.ManagedTINYCLUE, defaultManagedTINYCLUE, homeDir)
	files, err := processMemoryFile(managedFile, MemoryTypeManaged, processedPaths, includeExternal, 0, "")
	if err == nil {
		result = append(result, files...)
	}

	// 2. Managed .tinyclue/rules/*.md (always loaded)
	managedRules := layerPath(cfg.ManagedRulesDir, defaultManagedRules, homeDir)
	rules, err := processMdRules(managedRules, MemoryTypeManaged, processedPaths, includeExternal, false)
	if err == nil {
		result = append(result, rules...)
	}

	// 3. User ~/.tinyclue/TINYCLUE.md
	if cfg.UserTINYCLUE != "" || cfg.UserRulesDir != "" {
		userFile := layerPath(cfg.UserTINYCLUE, defaultUserTINYCLUE, homeDir)
		files, err := processMemoryFile(userFile, MemoryTypeUser, processedPaths, true, 0, "")
		if err == nil {
			result = append(result, files...)
		}

		// 4. User ~/.tinyclue/rules/*.md
		userRules := layerPath(cfg.UserRulesDir, defaultUserRules, homeDir)
		rules, err := processMdRules(userRules, MemoryTypeUser, processedPaths, true, false)
		if err == nil {
			result = append(result, rules...)
		}
	}

	// 5. Upward directory walk from CWD to root (Project + Local files)
	cwd := cfg.CWD
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("getwd: %w", err)
		}
	}
	cwd, _ = filepath.Abs(cwd)

	// Build path list from CWD up to root
	var dirs []string
	current := cwd
	for {
		dirs = append([]string{current}, dirs...) // prepend (root-first)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// Nested worktree detection (TS: findGitRoot vs findCanonicalGitRoot)
	gitRoot := utils.FindGitRoot(cwd)
	canonicalRoot := utils.FindCanonicalGitRoot(cwd)
	isNestedWorktree := gitRoot != "" && canonicalRoot != "" &&
		normalizePathForComparison(gitRoot) != normalizePathForComparison(canonicalRoot) &&
		pathInWorkingPath(gitRoot, canonicalRoot)

	// Process from root downward to CWD (matching TS dirs.reverse() iteration)
	for _, dir := range dirs {
		// In a nested worktree, skip checked-in files from main repo dirs
		// above the worktree root but within the canonical root.
		skipProject := isNestedWorktree &&
			pathInWorkingPath(dir, canonicalRoot) &&
			!pathInWorkingPath(dir, gitRoot)

		// Project: iterate configurable filenames
		if !skipProject {
			for _, fileName := range cfg.ProjectFiles {
				filePath := filepath.Join(dir, fileName)
				files, err := processMemoryFile(filePath, MemoryTypeProject, processedPaths, includeExternal, 0, "")
				if err == nil {
					result = append(result, files...)
				}
			}

			// Project: rules 目录
			if cfg.ProjectRulesDir != "" {
				rulesDir := filepath.Join(dir, cfg.ProjectRulesDir)
				rules, err := processMdRules(rulesDir, MemoryTypeProject, processedPaths, includeExternal, false)
				if err == nil {
					result = append(result, rules...)
				}
			}
		}

		// Local: iterate configurable filenames
		for _, fileName := range cfg.LocalFiles {
			localPath := filepath.Join(dir, fileName)
			files, err := processMemoryFile(localPath, MemoryTypeLocal, processedPaths, includeExternal, 0, "")
			if err == nil {
				result = append(result, files...)
			}
		}
	}

	// 6. Additional directories (--add-dir)
	if len(cfg.AddDirectories) > 0 {
		for _, addDir := range cfg.AddDirectories {
			addDir, _ = filepath.Abs(addDir)

			projectPath := filepath.Join(addDir, "TINYCLUE.md")
			files, err := processMemoryFile(projectPath, MemoryTypeProject, processedPaths, includeExternal, 0, "")
			if err == nil {
				result = append(result, files...)
			}

			dotTinycluePath := filepath.Join(addDir, ".tinyclue", "TINYCLUE.md")
			files, err = processMemoryFile(dotTinycluePath, MemoryTypeProject, processedPaths, includeExternal, 0, "")
			if err == nil {
				result = append(result, files...)
			}

			rulesDir := filepath.Join(addDir, ".tinyclue", "rules")
			rules, err := processMdRules(rulesDir, MemoryTypeProject, processedPaths, includeExternal, false)
			if err == nil {
				result = append(result, rules...)
			}
		}
	}

	// 7. AutoMem entrypoint (MEMORY.md)
	// 优先使用显式配置的 AutoMemEntrypoint，否则使用计算出的默认路径
	autoMemPath := cfg.AutoMemEntrypoint
	if autoMemPath == "" && cwd != "" {
		autoMemPath = GetAutoMemEntrypoint(cfg.getTinyclueHomeDir(), cwd)
	}
	if autoMemPath != "" {
		info, err := safelyReadMemoryFile(autoMemPath, MemoryTypeAutoMem, cfg, processedPaths, 0, "")
		if err == nil && info != nil {
			normPath := normalizePathForComparison(info.Path)
			if !processedPaths[normPath] {
				processedPaths[normPath] = true
				result = append(result, *info)
			}
		}
	}

	// 8. TeamMem entrypoint
	if cfg.TeamMemEntrypoint != "" {
		info, err := safelyReadMemoryFile(cfg.TeamMemEntrypoint, MemoryTypeTeamMem, cfg, processedPaths, 0, "")
		if err == nil && info != nil {
			normPath := normalizePathForComparison(info.Path)
			if !processedPaths[normPath] {
				processedPaths[normPath] = true
				result = append(result, *info)
			}
		}
	}

	return result, nil
}

// --- Memory file processing with @include ---

// processMemoryFile reads a memory file, parses its content, and recursively
// resolves @include directives. Mirrors TS processMemoryFile().
func processMemoryFile(filePath string, memType MemoryType, processedPaths map[string]bool, includeExternal bool, depth int, parent string) ([]MemoryFileInfo, error) {
	if depth >= maxIncludeDepth {
		return nil, nil
	}

	normPath := normalizePathForComparison(filePath)
	if processedPaths[normPath] {
		return nil, nil
	}

	// Resolve symlinks
	resolvedPath := resolveSymlink(filePath)

	processedPaths[normPath] = true
	resolvedNorm := normalizePathForComparison(resolvedPath)
	if resolvedNorm != normPath {
		processedPaths[resolvedNorm] = true
	}

	info, err := safelyReadMemoryFile(resolvedPath, memType, Config{}, processedPaths, depth, parent)
	if err != nil || info == nil {
		return nil, nil
	}
	if strings.TrimSpace(info.Content) == "" {
		return nil, nil
	}

	// Extract @include paths from content.
	// safelyReadMemoryFile already handled frontmatter/globs/comments/truncation
	// via parseMemoryFileContent — we only need @include resolution here.
	var includePaths []string
	if info.RawContent != "" {
		includePaths = extractIncludePaths(info.RawContent, info.Path)
	} else {
		includePaths = extractIncludePaths(info.Content, info.Path)
	}

	if parent != "" {
		info.Parent = parent
	}

	result := []MemoryFileInfo{*info}

	// Resolve @include directives
	for _, includePath := range includePaths {
		isExternal := !pathInWorkingPath(includePath, filepath.Dir(filePath))
		if isExternal && !includeExternal {
			continue
		}

		// Determine type for included files
		includeType := memType
		if isExternal && memType == MemoryTypeUser {
			// User memory includes always allowed
			includeType = memType
		}

		includedFiles, err := processMemoryFile(includePath, includeType, processedPaths, includeExternal, depth+1, filePath)
		if err == nil {
			result = append(result, includedFiles...)
		}
	}

	return result, nil
}

// safelyReadMemoryFile reads and parses a memory file.
// NOTE: does NOT check processedPaths — that's the caller's responsibility
// (processMemoryFile pre-marks paths to prevent @include cycles).
func safelyReadMemoryFile(filePath string, memType MemoryType, _ Config, _ map[string]bool, _ int, parent string) (*MemoryFileInfo, error) {
	clean, err := filepath.Abs(filePath)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(clean)
	if err != nil || info.IsDir() {
		return nil, fmt.Errorf("not found: %s", clean)
	}

	data, err := os.ReadFile(clean)
	if err != nil {
		return nil, err
	}

	rawContent := string(data)

	// Parse content
	parsed := parseMemoryFileContent(rawContent, clean, memType, "")
	if parsed == nil {
		return nil, nil
	}

	mfi := &MemoryFileInfo{
		Path:                   clean,
		Type:                   memType,
		Content:                parsed.Content,
		Globs:                  parsed.Globs,
		ContentDiffersFromDisk: parsed.ContentDiffersFromDisk,
		RawContent:             parsed.RawContent,
		Dir:                    filepath.Dir(clean),
	}

	if parent != "" {
		mfi.Parent = parent
	}

	return mfi, nil
}

// parsedMemoryFile holds the result of parsing a memory file's content.
type parsedMemoryFile struct {
	Content                string
	Globs                  []string
	IncludePaths           []string
	ContentDiffersFromDisk bool
	RawContent             string
}

// parseMemoryFileContent parses raw content into content, globs, and @include paths.
// Mirrors TS parseMemoryFileContent() in tinycluemd.ts.
func parseMemoryFileContent(rawContent, filePath string, memType MemoryType, includeBasePath string) *parsedMemoryFile {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext != "" && !textFileExtensions[ext] {
		return nil
	}

	// Parse frontmatter
	parsed := parseFrontmatter(rawContent)
	content := parsed.Content

	// Parse paths from frontmatter
	var globs []string
	if parsed.Frontmatter.Paths != nil {
		paths := splitPathInFrontmatter(parsed.Frontmatter.Paths)
		// Filter out empty/** patterns
		var filtered []string
		for _, p := range paths {
			if p == "**" {
				continue
			}
			if p != "" {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) > 0 {
			globs = filtered
		}
	}

	// Process @include paths
	var includePaths []string
	if includeBasePath != "" {
		includePaths = extractIncludePaths(content, includeBasePath)
	}

	// Strip HTML comments
	strippedContent, _ := stripHtmlComments(content)
	finalContent := strippedContent

	// Truncate AutoMem/TeamMem entrypoints
	if memType == MemoryTypeAutoMem || memType == MemoryTypeTeamMem {
		truncated := truncateEntrypointContent(finalContent)
		finalContent = truncated.content
	}

	contentDiffersFromDisk := finalContent != rawContent

	var raw string
	if contentDiffersFromDisk {
		raw = rawContent
	}

	return &parsedMemoryFile{
		Content:                finalContent,
		Globs:                  globs,
		IncludePaths:           includePaths,
		ContentDiffersFromDisk: contentDiffersFromDisk,
		RawContent:             raw,
	}
}

// --- Rules directory processing ---

// processMdRules processes all .md files in a rules directory and subdirectories.
// Mirrors TS processMdRules() with conditionalRule support and visitedDirs cycle detection.
func processMdRules(rulesDir string, memType MemoryType, processedPaths map[string]bool, includeExternal bool, conditionalRule bool) ([]MemoryFileInfo, error) {
	// Use visitedDirs for symlink cycle detection (TS behavior)
	visitedDirs := make(map[string]bool)
	return processMdRulesWithVisited(rulesDir, memType, processedPaths, includeExternal, conditionalRule, visitedDirs)
}

// processMdRulesWithVisited is the recursive implementation with cycle detection.
func processMdRulesWithVisited(rulesDir string, memType MemoryType, processedPaths map[string]bool, includeExternal bool, conditionalRule bool, visitedDirs map[string]bool) ([]MemoryFileInfo, error) {
	// Resolve symlinks for the rules directory (TS safeResolvePath)
	resolvedDir := resolveSymlink(rulesDir)
	if visitedDirs[resolvedDir] {
		return nil, nil
	}
	visitedDirs[resolvedDir] = true

	entries, err := os.ReadDir(resolvedDir)
	if err != nil {
		return nil, err
	}

	var result []MemoryFileInfo
	for _, entry := range entries {
		entryPath := filepath.Join(resolvedDir, entry.Name())

		if entry.IsDir() {
			// Recursive subdirectory processing (TS behavior)
			subRules, err := processMdRulesWithVisited(entryPath, memType, processedPaths, includeExternal, conditionalRule, visitedDirs)
			if err == nil {
				result = append(result, subRules...)
			}
			continue
		}

		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		files, err := processMemoryFile(entryPath, memType, processedPaths, includeExternal, 0, "")
		if err != nil {
			continue
		}

		// Filter by conditionalRule: conditionalRule=true means only files with globs,
		// conditionalRule=false means only files without globs.
		for _, f := range files {
			if conditionalRule && len(f.Globs) > 0 {
				result = append(result, f)
			} else if !conditionalRule && len(f.Globs) == 0 {
				result = append(result, f)
			}
		}
	}

	return result, nil
}

// processConditionedMdRules processes rules files matching a target path via globs.
// Mirrors TS processConditionedMdRules().
func processConditionedMdRules(targetPath, rulesDir string, memType MemoryType, processedPaths map[string]bool, includeExternal bool) ([]MemoryFileInfo, error) {
	rules, err := processMdRules(rulesDir, memType, processedPaths, includeExternal, true)
	if err != nil {
		return nil, err
	}

	var result []MemoryFileInfo
	for _, rule := range rules {
		if len(rule.Globs) == 0 {
			continue
		}

		// For Project rules: glob patterns are relative to the directory containing .tinyclue
		// For Managed/User rules: glob patterns are relative to original CWD
		var baseDir string
		if memType == MemoryTypeProject {
			// Parent of .tinyclue (rulesDir is .../.tinyclue/rules)
			baseDir = filepath.Dir(filepath.Dir(rulesDir))
		} else {
			// Use current directory
			baseDir = config.CLI.Cwd
		}

		if matchGlob(targetPath, baseDir, rule.Globs) {
			result = append(result, rule)
		}
	}

	return result, nil
}

// --- Conditional rules accessors ---

// getManagedAndUserConditionalRules returns managed and user conditional rules
// matching a target path. Mirrors TS getManagedAndUserConditionalRules().
func getManagedAndUserConditionalRules(targetPath string, cfg Config) ([]MemoryFileInfo, error) {
	var result []MemoryFileInfo
	processedPaths := make(map[string]bool)

	tinyclueHome := cfg.getTinyclueHomeDir()
	homeDir := cfg.getHomeDir()

	// Managed conditional rules
	managedRules := filepath.Join(tinyclueHome, "managed", "rules")
	rules, err := processConditionedMdRules(targetPath, managedRules, MemoryTypeManaged, processedPaths, false)
	if err == nil {
		result = append(result, rules...)
	}

	// User conditional rules
	if cfg.UserRulesDir != "" {
		userRules := expandHomeDir(cfg.UserRulesDir, homeDir)
		rules, err := processConditionedMdRules(targetPath, userRules, MemoryTypeUser, processedPaths, true)
		if err == nil {
			result = append(result, rules...)
		}
	}

	return result, nil
}

// getMemoryFilesForNestedDirectory loads memory files for a single nested directory.
// Mirrors TS getMemoryFilesForNestedDirectory().
func getMemoryFilesForNestedDirectory(dir, targetPath string, cfg Config) ([]MemoryFileInfo, error) {
	var result []MemoryFileInfo
	processedPaths := make(map[string]bool)

	for _, fileName := range cfg.ProjectFiles {
		filePath := filepath.Join(dir, fileName)
		files, err := processMemoryFile(filePath, MemoryTypeProject, processedPaths, false, 0, "")
		if err == nil {
			result = append(result, files...)
		}
	}

	for _, fileName := range cfg.LocalFiles {
		localPath := filepath.Join(dir, fileName)
		files, err := processMemoryFile(localPath, MemoryTypeLocal, processedPaths, false, 0, "")
		if err == nil {
			result = append(result, files...)
		}
	}

	rulesDir := filepath.Join(dir, ".tinyclue", "rules")

	// Unconditional rules
	unconditionalProcessed := make(map[string]bool)
	uncondRules, err := processMdRules(rulesDir, MemoryTypeProject, unconditionalProcessed, false, false)
	if err == nil {
		result = append(result, uncondRules...)
	}

	// Conditional rules
	condRules, err := processConditionedMdRules(targetPath, rulesDir, MemoryTypeProject, processedPaths, false)
	if err == nil {
		result = append(result, condRules...)
	}

	// Seed processedPaths with unconditional paths
	for p := range unconditionalProcessed {
		processedPaths[p] = true
	}

	return result, nil
}

// getConditionalRulesForCwdLevelDirectory gets conditional rules for a CWD-level directory.
// Mirrors TS getConditionalRulesForCwdLevelDirectory().
func getConditionalRulesForCwdLevelDirectory(dir, targetPath string) ([]MemoryFileInfo, error) {
	rulesDir := filepath.Join(dir, ".tinyclue", "rules")
	processedPaths := make(map[string]bool)
	return processConditionedMdRules(targetPath, rulesDir, MemoryTypeProject, processedPaths, false)
}

// --- Glob matching for conditional rules ---

// matchGlob checks if targetPath matches any of the glob patterns relative to baseDir.
// Uses gitignore-style matching via filepath.Match with path prefix checks.
func matchGlob(targetPath, baseDir string, globs []string) bool {
	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return false
	}

	rel = strings.ReplaceAll(rel, "\\", "/")
	for _, g := range globs {
		g = strings.ReplaceAll(g, "\\", "/")
		// Try exact match
		if matched, _ := filepath.Match(g, rel); matched {
			return true
		}
		// Try prefix match for directory globs
		if strings.HasSuffix(g, "/**") {
			prefix, _ := strings.CutSuffix(g, "/**")
			if strings.HasPrefix(rel, prefix) {
				return true
			}
		}
		// Try matching base name
		if matched, _ := filepath.Match(g, filepath.Base(rel)); matched {
			return true
		}
	}
	return false
}

// --- Utilities ---

// resolveSymlink resolves a file path through symlinks.
func resolveSymlink(filePath string) string {
	info, err := os.Lstat(filePath)
	if err != nil {
		return filePath
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := os.Readlink(filePath)
		if err != nil {
			return filePath
		}
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(filepath.Dir(filePath), resolved)
		}
		return filepath.Clean(resolved)
	}
	return filepath.Clean(filePath)
}

// --- Home directory expansion ---

// expandHomeDir 展开路径中的 ~/ 前缀为 homeDir。
// 如果路径不以 ~/ 开头，原样返回。
func expandHomeDir(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// layerPath 返回层级路径：如果 cfg 字段非空则展开并返回，否则使用默认值。
func layerPath(cfgVal, defaultVal, homeDir string) string {
	if cfgVal != "" {
		return expandHomeDir(cfgVal, homeDir)
	}
	return defaultVal
}

// --- External includes tracking ---

// ExternalTinyclueMdInclude holds info about an external @include.
// Mirrors TS ExternalTinyclueMdInclude.
type ExternalTinyclueMdInclude struct {
	Path   string
	Parent string
}

// getExternalTinyclueMdIncludes returns files that are external includes.
// Mirrors TS getExternalTinyclueMdIncludes() — checks if included file is outside the original CWD.
func getExternalTinyclueMdIncludes(files []MemoryFileInfo) []ExternalTinyclueMdInclude {
	cwd := config.CLI.Cwd
	var result []ExternalTinyclueMdInclude
	for _, f := range files {
		if f.Type != MemoryTypeUser && f.Parent != "" && !pathInWorkingPath(f.Path, cwd) {
			result = append(result, ExternalTinyclueMdInclude{
				Path:   f.Path,
				Parent: f.Parent,
			})
		}
	}
	return result
}

// hasExternalTinyclueMdIncludes checks if any external includes exist.
// Mirrors TS hasExternalTinyclueMdIncludes().
func hasExternalTinyclueMdIncludes(files []MemoryFileInfo) bool {
	return len(getExternalTinyclueMdIncludes(files)) > 0
}

// isMemoryFilePath checks if a path is a memory file (TINYCLUE.md, TINYCLUE.local.md, or .tinyclue/rules/*.md).
// Mirrors TS isMemoryFilePath().
func isMemoryFilePath(filePath string) bool {
	name := filepath.Base(filePath)
	if name == "TINYCLUE.md" || name == "TINYCLUE.local.md" {
		return true
	}
	if strings.HasSuffix(name, ".md") &&
		strings.Contains(filePath, string(filepath.Separator)+".tinyclue"+string(filepath.Separator)+"rules"+string(filepath.Separator)) {
		return true
	}
	return false
}

// getAllMemoryFilePaths collects all memory file paths from discovered files and a file state cache.
// Mirrors TS getAllMemoryFilePaths(). Simplified — doesn't take fileStateCache.
func getAllMemoryFilePaths(files []MemoryFileInfo) []string {
	paths := make(map[string]bool)
	for _, f := range files {
		if strings.TrimSpace(f.Content) != "" {
			paths[f.Path] = true
		}
	}
	var result []string
	for p := range paths {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// getLargeMemoryFiles returns files exceeding MAX_MEMORY_CHARACTER_COUNT.
// Mirrors TS getLargeMemoryFiles().
func getLargeMemoryFiles(files []MemoryFileInfo) []MemoryFileInfo {
	var result []MemoryFileInfo
	for _, f := range files {
		if len(f.Content) > maxMemoryCharCount {
			result = append(result, f)
		}
	}
	return result
}
