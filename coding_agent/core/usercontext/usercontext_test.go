package usercontext

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Frontmatter parsing ---

func TestParseFrontmatter_NoFrontmatter(t *testing.T) {
	input := "# Title\n\nSome content"
	result := parseFrontmatter(input)
	if result.Content != input {
		t.Fatalf("expected content unchanged, got %q", result.Content)
	}
}

func TestParseFrontmatter_WithFrontmatter(t *testing.T) {
	input := "---\npaths: src/*.ts\n---\n# Title\n\nContent"
	result := parseFrontmatter(input)
	if strings.Contains(result.Content, "---") {
		t.Fatal("frontmatter delimiter should be stripped")
	}
	if !strings.Contains(result.Content, "# Title") {
		t.Fatal("content after frontmatter should remain")
	}
}

func TestParseFrontmatter_BlockListPaths(t *testing.T) {
	input := "---\npaths:\n  - src/*.ts\n  - src/*.tsx\n---\n# Title"
	result := parseFrontmatter(input)
	if result.Frontmatter.Paths == nil {
		t.Fatal("expected paths to be parsed")
	}
	paths, ok := result.Frontmatter.Paths.([]string)
	if !ok {
		t.Fatalf("expected []string, got %T", result.Frontmatter.Paths)
	}
	if len(paths) != 2 || paths[0] != "src/*.ts" || paths[1] != "src/*.tsx" {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestParseFrontmatter_InlineListPaths(t *testing.T) {
	input := "---\npaths: [src/*.ts, src/*.tsx]\n---\n# Title"
	result := parseFrontmatter(input)
	paths, ok := result.Frontmatter.Paths.([]string)
	if !ok || len(paths) != 2 {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestParseFrontmatter_SinglePath(t *testing.T) {
	input := "---\npaths: src/*.ts\n---\n# Title"
	result := parseFrontmatter(input)
	path, ok := result.Frontmatter.Paths.(string)
	if !ok || path != "src/*.ts" {
		t.Fatalf("unexpected path: %v", result.Frontmatter.Paths)
	}
}

// --- splitPathInFrontmatter ---

func TestSplitPathInFrontmatter_String(t *testing.T) {
	result := splitPathInFrontmatter("a, b, c")
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestSplitPathInFrontmatter_StringSlice(t *testing.T) {
	result := splitPathInFrontmatter([]string{"a", "b"})
	if len(result) != 2 || result[0] != "a" || result[1] != "b" {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestSplitPathInFrontmatter_Nil(t *testing.T) {
	result := splitPathInFrontmatter(nil)
	if result != nil {
		t.Fatal("expected nil for non-string/non-slice")
	}
}

func TestSplitPathInFrontmatter_BraceExpansion(t *testing.T) {
	result := splitPathInFrontmatter("src/*.{ts,tsx}")
	if len(result) != 2 || result[0] != "src/*.ts" || result[1] != "src/*.tsx" {
		t.Fatalf("unexpected: %v", result)
	}
}

// --- expandBraces ---

func TestExpandBraces_NoBraces(t *testing.T) {
	result := expandBraces("simple")
	if len(result) != 1 || result[0] != "simple" {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestExpandBraces_Single(t *testing.T) {
	result := expandBraces("{a,b}")
	if len(result) != 2 || result[0] != "a" || result[1] != "b" {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestExpandBraces_Nested(t *testing.T) {
	result := expandBraces("{a,b}/{c,d}")
	if len(result) != 4 {
		t.Fatalf("expected 4 items, got %v", result)
	}
}

// --- splitCommaSeparated ---

func TestSplitCommaSeparated_Simple(t *testing.T) {
	result := splitCommaSeparated("a, b, c")
	if len(result) != 3 {
		t.Fatalf("unexpected: %v", result)
	}
}

func TestSplitCommaSeparated_Braces(t *testing.T) {
	result := splitCommaSeparated("src/*.{ts,tsx}")
	if len(result) != 2 {
		t.Fatalf("braces should expand: %v", result)
	}
}

// --- HTML comment stripping ---

func TestStripHtmlComments_NoComment(t *testing.T) {
	input := "Hello world"
	result, stripped := stripHtmlComments(input)
	if stripped || result != input {
		t.Fatal("should not strip anything")
	}
}

func TestStripHtmlComments_SingleLine(t *testing.T) {
	input := "Before\n<!-- comment -->\nAfter"
	result, stripped := stripHtmlComments(input)
	if !stripped {
		t.Fatal("should have stripped")
	}
	// Comment line removed; surrounding newline structure depends on
	// the line-based approach (simpler than the marked-lexer TS version).
	if strings.Contains(result, "<!--") {
		t.Fatal("comment should be removed")
	}
	if !strings.Contains(result, "Before") || !strings.Contains(result, "After") {
		t.Fatal("content around comment should remain")
	}
}

func TestStripHtmlComments_MultiLine(t *testing.T) {
	input := "Before\n<!--\nmulti\nline\n-->\nAfter"
	result, stripped := stripHtmlComments(input)
	if !stripped {
		t.Fatal("should have stripped")
	}
	if strings.Contains(result, "<!--") || strings.Contains(result, "multi") {
		t.Fatal("multi-line comment should be stripped")
	}
	if !strings.Contains(result, "Before") || !strings.Contains(result, "After") {
		t.Fatal("content around comment should remain")
	}
}

func TestStripHtmlComments_Residue(t *testing.T) {
	input := "<!-- note -->Keep me"
	result, stripped := stripHtmlComments(input)
	if !stripped {
		t.Fatal("should have stripped")
	}
	if strings.TrimSpace(result) != "Keep me" {
		t.Fatalf("unexpected: %q", result)
	}
}

func TestStripHtmlComments_Unclosed(t *testing.T) {
	input := "Before\n<!-- unclosed\nStill here"
	result, stripped := stripHtmlComments(input)
	if stripped {
		t.Fatal("unclosed comment should not strip")
	}
	if result != input {
		t.Fatalf("unclosed comments should keep original: %q", result)
	}
}

// --- @include extraction ---

func TestExtractIncludePaths_Basic(t *testing.T) {
	content := "Some text @./test.md here"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) == 0 {
		t.Fatal("expected include paths")
	}
	if !strings.HasSuffix(paths[0], "/base/test.md") {
		t.Fatalf("unexpected path: %s", paths[0])
	}
}

func TestExtractIncludePaths_SkipCodeBlock(t *testing.T) {
	content := "Text\n```\n@./secret.md\n```\n@./public.md"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path (code-block should be skipped), got %d: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "/base/public.md") {
		t.Fatalf("unexpected path: %s", paths[0])
	}
}

func TestExtractIncludePaths_SkipInlineCode(t *testing.T) {
	content := "Text `@./secret.md` and @./public.md"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) != 1 {
		t.Fatalf("expected 1 path (inline code should be skipped), got %d: %v", len(paths), paths)
	}
	if !strings.HasSuffix(paths[0], "/base/public.md") {
		t.Fatalf("unexpected path: %s", paths[0])
	}
}

func TestExtractIncludePaths_Absolute(t *testing.T) {
	content := "See @/etc/config.yaml"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) == 0 {
		t.Fatal("expected paths")
	}
	if paths[0] != "/etc/config.yaml" {
		t.Fatalf("expected /etc/config.yaml, got %s", paths[0])
	}
}

func TestExtractIncludePaths_Tilde(t *testing.T) {
	home, _ := os.UserHomeDir()
	content := "See @~/notes.md"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) == 0 {
		t.Fatal("expected paths")
	}
	if !strings.HasPrefix(paths[0], home) {
		t.Fatalf("expected home dir prefix, got %s", paths[0])
	}
}

func TestExtractIncludePaths_StripFragment(t *testing.T) {
	content := "See @./file.md#section"
	baseDir := "/base"
	paths := extractIncludePaths(content, baseDir)
	if len(paths) == 0 {
		t.Fatal("expected paths")
	}
	if strings.Contains(paths[0], "#") {
		t.Fatalf("fragment should be stripped: %s", paths[0])
	}
}

func TestRemoveFencedCodeBlocks(t *testing.T) {
	input := "before\n```\ninside\n```\nafter"
	result := removeFencedCodeBlocks(input)
	if strings.Contains(result, "inside") {
		t.Fatal("code block content should be removed")
	}
	if !strings.Contains(result, "before") || !strings.Contains(result, "after") {
		t.Fatal("text outside code block should remain")
	}
}

func TestRemoveInlineCodeSpans(t *testing.T) {
	input := "before `code` after"
	result := removeInlineCodeSpans(input)
	if result != "before  after" {
		t.Fatalf("unexpected: %q", result)
	}
}

// --- Path utilities ---

func TestSanitizePath_Simple(t *testing.T) {
	result := utils.SanitizePath("hello")
	if result != "hello" {
		t.Fatalf("unexpected: %s", result)
	}
}

func TestSanitizePath_ReplaceNonAlpha(t *testing.T) {
	result := utils.SanitizePath("hello/world/test")
	if result != "hello-world-test" {
		t.Fatalf("unexpected: %s", result)
	}
}

func TestSanitizePath_Long(t *testing.T) {
	long := strings.Repeat("a", 300)
	result := utils.SanitizePath(long)
	// SanitizePath 截断到 200 字符 + "-" + 8 位 hash（约 209 字符）。
	if len(result) > 210 {
		t.Fatalf("too long: %d", len(result))
	}
	if !strings.HasPrefix(result, long[:200]) {
		t.Fatal("should start with prefix")
	}
}

func TestNormalizePathForComparison(t *testing.T) {
	result := normalizePathForComparison("/Users/Test/File.go")
	if result != strings.ToLower("/users/test/file.go") &&
		result != strings.ToLower(filepath.Clean("/Users/Test/File.go")) {
		t.Fatalf("unexpected: %s", result)
	}
}

func TestPathInWorkingPath_Same(t *testing.T) {
	if !pathInWorkingPath("/a/b", "/a/b") {
		t.Fatal("same path should be inside")
	}
}

func TestPathInWorkingPath_Inside(t *testing.T) {
	if !pathInWorkingPath("/a/b/c", "/a/b") {
		t.Fatal("/a/b/c should be inside /a/b")
	}
}

func TestPathInWorkingPath_Outside(t *testing.T) {
	if pathInWorkingPath("/a/c", "/a/b") {
		t.Fatal("/a/c should NOT be inside /a/b")
	}
}

// --- expandPath ---

func TestExpandPath_Absolute(t *testing.T) {
	result := expandPath("/a/b/c", "/base")
	if result != "/a/b/c" {
		t.Fatalf("unexpected: %s", result)
	}
}

func TestExpandPath_Relative(t *testing.T) {
	result := expandPath("./foo.md", "/base/dir")
	if result != "/base/dir/foo.md" {
		t.Fatalf("unexpected: %s", result)
	}
}

func TestExpandPath_BareRelative(t *testing.T) {
	result := expandPath("foo.md", "/base/dir")
	if result != "/base/dir/foo.md" {
		t.Fatalf("unexpected: %s", result)
	}
}

// --- validateMemoryPath ---

func TestValidateMemoryPath_Valid(t *testing.T) {
	result := validateMemoryPath("/a/b/c", false)
	if result == "" {
		t.Fatal("expected valid")
	}
}

func TestValidateMemoryPath_Relative(t *testing.T) {
	result := validateMemoryPath("a/b", false)
	if result != "" {
		t.Fatal("relative path should be rejected")
	}
}

func TestValidateMemoryPath_Empty(t *testing.T) {
	result := validateMemoryPath("", false)
	if result != "" {
		t.Fatal("empty should be rejected")
	}
}

func TestValidateMemoryPath_Root(t *testing.T) {
	result := validateMemoryPath("/", false)
	if result != "" {
		t.Fatal("root should be rejected (len < 3)")
	}
}

// --- isMemoryFilePath ---

func TestIsMemoryFilePath(t *testing.T) {
	if !isMemoryFilePath("/some/dir/TINYCLUE.md") {
		t.Fatal("TINYCLUE.md should be detected")
	}
	if !isMemoryFilePath("/some/dir/TINYCLUE.local.md") {
		t.Fatal("TINYCLUE.local.md should be detected")
	}
	if !isMemoryFilePath("/some/.tinyclue/rules/foo.md") {
		t.Fatal(".tinyclue/rules/*.md should be detected")
	}
	if isMemoryFilePath("/some/other/file.md") {
		t.Fatal("other .md files should NOT be detected")
	}
	if isMemoryFilePath("/some/other/file.txt") {
		t.Fatal(".txt files should NOT be detected")
	}
}

// --- getAllMemoryFilePaths ---

func TestGetAllMemoryFilePaths(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/a", Content: "hello"},
		{Path: "/b", Content: ""},
		{Path: "/c", Content: "world"},
	}
	paths := getAllMemoryFilePaths(files)
	if len(paths) != 2 {
		t.Fatalf("expected 2 (skip empty content), got %d: %v", len(paths), paths)
	}
}

// --- getLargeMemoryFiles ---

func TestGetLargeMemoryFiles(t *testing.T) {
	small := MemoryFileInfo{Path: "/small", Content: strings.Repeat("a", 100)}
	large := MemoryFileInfo{Path: "/large", Content: strings.Repeat("a", 50000)}
	result := getLargeMemoryFiles([]MemoryFileInfo{small, large})
	if len(result) != 1 || result[0].Path != "/large" {
		t.Fatalf("expected only large file, got %d files", len(result))
	}
}

// --- typePriority ---

func TestTypePriority(t *testing.T) {
	if typePriority(MemoryTypeManaged) >= typePriority(MemoryTypeUser) {
		t.Fatal("Managed should have lower priority than User")
	}
	if typePriority(MemoryTypeUser) >= typePriority(MemoryTypeProject) {
		t.Fatal("User should have lower priority than Project")
	}
	if typePriority(MemoryTypeProject) >= typePriority(MemoryTypeLocal) {
		t.Fatal("Project should have lower priority than Local")
	}
}

// --- getTinyclueMds ---

func TestGetTinyclueMds_Empty(t *testing.T) {
	result := getTinyclueMds(nil)
	if result != "" {
		t.Fatal("nil input should produce empty string")
	}
}

func TestGetTinyclueMds_Basic(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/test/TINYCLUE.md", Type: MemoryTypeProject, Content: "Do X"},
	}
	result := getTinyclueMds(files)
	if !strings.Contains(result, "/test/TINYCLUE.md") {
		t.Fatal("should contain path")
	}
	if !strings.Contains(result, "Do X") {
		t.Fatal("should contain content")
	}
	if !strings.Contains(result, memoryInstructionPrompt) {
		t.Fatal("should contain instruction prompt")
	}
}

func TestGetTinyclueMds_AllTypesIncluded(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/p1", Type: MemoryTypeProject, Content: "only-project-content"},
		{Path: "/p2", Type: MemoryTypeLocal, Content: "only-local-content"},
		{Path: "/p3", Type: MemoryTypeUser, Content: "only-user-content"},
	}
	result := getTinyclueMds(files)
	if !strings.Contains(result, "only-project-content") {
		t.Fatal("Project content should be included")
	}
	if !strings.Contains(result, "only-local-content") {
		t.Fatal("Local content should be included")
	}
	if !strings.Contains(result, "only-user-content") {
		t.Fatal("User content should be included")
	}
}

func TestGetTinyclueMds_EmptyContent(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/p", Type: MemoryTypeProject, Content: "  "},
	}
	result := getTinyclueMds(files)
	if result != "" {
		t.Fatal("files with only whitespace content should produce empty output")
	}
}

func TestGetTinyclueMds_TeamMem(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/team", Type: MemoryTypeTeamMem, Content: "team rules"},
	}
	result := getTinyclueMds(files)
	if !strings.Contains(result, "<team-memory-content") {
		t.Fatal("TeamMem should use team-memory-content tags")
	}
	if !strings.Contains(result, "source=\"shared\"") {
		t.Fatal("TeamMem should have source=\"shared\"")
	}
}

// --- PrependUserContext ---

func TestPrependUserContext_Basic(t *testing.T) {
	ctx := &UserContext{
		TinyclueMd:  "Some instructions",
		CurrentDate: "Today's date is 2026/01/01.",
	}
	result := PrependUserContext(ctx)
	if !strings.Contains(result, "<system-reminder>") {
		t.Fatal("should wrap in system-reminder tags")
	}
	if !strings.Contains(result, "</system-reminder>") {
		t.Fatal("should close system-reminder tags")
	}
	if !strings.Contains(result, "# tinyclueMd") {
		t.Fatal("should include tinyclueMd header")
	}
	if !strings.Contains(result, "# currentDate") {
		t.Fatal("should include currentDate header")
	}
	if !strings.Contains(result, "Some instructions") {
		t.Fatal("should include tinyclueMd content")
	}
}

func TestPrependUserContext_NoTinyclueMd(t *testing.T) {
	ctx := &UserContext{
		TinyclueMd:  "",
		CurrentDate: "Today's date is 2026/01/01.",
	}
	result := PrependUserContext(ctx)
	if !strings.Contains(result, "# currentDate") {
		t.Fatal("should still include currentDate")
	}
	if strings.Contains(result, "# tinyclueMd") {
		t.Fatal("should NOT include tinyclueMd when empty")
	}
}

// --- truncateEntrypointContent ---

func TestTruncateEntrypointContent_Short(t *testing.T) {
	content := "line1\nline2\nline3"
	result := truncateEntrypointContent(content)
	if result.wasLineTruncated || result.wasByteTruncated {
		t.Fatal("short content should not be truncated")
	}
	if result.content != content {
		t.Fatalf("content changed: %q", result.content)
	}
}

func TestTruncateEntrypointContent_LineTruncated(t *testing.T) {
	lines := make([]string, maxEntrypointLines+10)
	for i := range lines {
		lines[i] = "line"
	}
	content := strings.Join(lines, "\n")
	result := truncateEntrypointContent(content)
	if !result.wasLineTruncated {
		t.Fatal("should be line truncated")
	}
	if !strings.Contains(result.content, "WARNING:") {
		t.Fatal("should contain WARNING message when truncated")
	}
}

// --- sourceToType / typeToSource ---

func TestSourceToType(t *testing.T) {
	if sourceToType(SourceManaged) != MemoryTypeManaged {
		t.Fatal("sourceToType Managed -> MemoryTypeManaged")
	}
	if sourceToType(SourceUser) != MemoryTypeUser {
		t.Fatal("sourceToType User -> MemoryTypeUser")
	}
}

func TestTypeToSource(t *testing.T) {
	if typeToSource(MemoryTypeManaged) != SourceManaged {
		t.Fatal("typeToSource MemoryTypeManaged -> SourceManaged")
	}
	if typeToSource(MemoryTypeUser) != SourceUser {
		t.Fatal("typeToSource MemoryTypeUser -> SourceUser")
	}
}

// --- matchGlob ---

func TestMatchGlob_Exact(t *testing.T) {
	if !matchGlob("/base/src/file.ts", "/base", []string{"src/file.ts"}) {
		t.Fatal("exact glob should match")
	}
}

func TestMatchGlob_Wildcard(t *testing.T) {
	if !matchGlob("/base/src/file.ts", "/base", []string{"src/*.ts"}) {
		t.Fatal("wildcard glob should match")
	}
}

func TestMatchGlob_DirGlob(t *testing.T) {
	if !matchGlob("/base/src/sub/file.ts", "/base", []string{"src/**"}) {
		t.Fatal("dir glob should match")
	}
}

func TestMatchGlob_NoMatch(t *testing.T) {
	if matchGlob("/base/other/file.py", "/base", []string{"src/*.ts"}) {
		t.Fatal("non-matching glob should not match")
	}
}

func TestMatchGlob_OutsideBase(t *testing.T) {
	if matchGlob("/outside/file.ts", "/base", []string{"*.ts"}) {
		t.Fatal("path outside base should not match")
	}
}

// --- getExternalTinyclueMdIncludes ---

func TestGetExternalTinyclueMdIncludes_UserType(t *testing.T) {
	files := []MemoryFileInfo{
		{Path: "/external/file.md", Type: MemoryTypeUser, Parent: "/somewhere/TINYCLUE.md"},
	}
	result := getExternalTinyclueMdIncludes(files)
	if len(result) != 0 {
		t.Fatal("User type should not be considered external")
	}
}

// --- hasExternalTinyclueMdIncludes ---

func TestHasExternalTinyclueMdIncludes_Empty(t *testing.T) {
	if hasExternalTinyclueMdIncludes(nil) {
		t.Fatal("empty should not have external includes")
	}
}

// --- parseSimpleYAMLField ---

func TestParseSimpleYAMLField_SingleValue(t *testing.T) {
	result := parseSimpleYAMLField("paths: src/*.ts", "paths")
	v, ok := result.(string)
	if !ok || v != "src/*.ts" {
		t.Fatalf("expected 'src/*.ts', got %v", result)
	}
}

func TestParseSimpleYAMLField_InlineList(t *testing.T) {
	result := parseSimpleYAMLField("paths: [a, b, c]", "paths")
	v, ok := result.([]string)
	if !ok || len(v) != 3 {
		t.Fatalf("expected [a b c], got %v", result)
	}
}

func TestParseSimpleYAMLField_BlockList(t *testing.T) {
	yaml := "paths:\n  - a\n  - b\n  - c"
	result := parseSimpleYAMLField(yaml, "paths")
	v, ok := result.([]string)
	if !ok || len(v) != 3 {
		t.Fatalf("expected [a b c], got %v", result)
	}
}

func TestParseSimpleYAMLField_KeyNotFound(t *testing.T) {
	result := parseSimpleYAMLField("other: value", "paths")
	if result != nil {
		t.Fatal("should return nil for missing key")
	}
}

// --- parseMemoryFileContent ---

func TestParseMemoryFileContent_TextFile(t *testing.T) {
	result := parseMemoryFileContent("hello", "/path/file.md", MemoryTypeProject, "")
	if result == nil {
		t.Fatal("should parse .md files")
	}
	if result.Content != "hello" {
		t.Fatalf("unexpected content: %q", result.Content)
	}
}

func TestParseMemoryFileContent_NonTextFile(t *testing.T) {
	result := parseMemoryFileContent("binary", "/path/image.png", MemoryTypeProject, "")
	if result != nil {
		t.Fatal("non-text files should return nil")
	}
}

func TestParseMemoryFileContent_FrontmatterStripped(t *testing.T) {
	input := "---\npaths: test\n---\nreal content"
	result := parseMemoryFileContent(input, "/p/TINYCLUE.md", MemoryTypeProject, "")
	if result == nil {
		t.Fatal("should parse")
	}
	if strings.Contains(result.Content, "---") {
		t.Fatal("frontmatter should be stripped from content")
	}
	if !strings.Contains(result.Content, "real content") {
		t.Fatal("content should remain")
	}
}

func TestParseMemoryFileContent_WithGlobs(t *testing.T) {
	input := "---\npaths: src/*.ts\n---\ncontent"
	result := parseMemoryFileContent(input, "/p/TINYCLUE.md", MemoryTypeProject, "")
	if result == nil || len(result.Globs) != 1 || result.Globs[0] != "src/*.ts" {
		t.Fatalf("unexpected globs: %v", result.Globs)
	}
}

func TestParseMemoryFileContent_AutoMemTruncated(t *testing.T) {
	lines := make([]string, maxEntrypointLines+10)
	for i := range lines {
		lines[i] = "line content here"
	}
	content := strings.Join(lines, "\n")
	result := parseMemoryFileContent(content, "/p/MEMORY.md", MemoryTypeAutoMem, "")
	if result == nil {
		t.Fatal("should parse")
	}
	if !result.ContentDiffersFromDisk {
		t.Fatal("AutoMem content should differ from disk when truncated")
	}
	// Verify warning is present
	if !strings.Contains(result.Content, "WARNING:") {
		t.Fatal("truncated content should include WARNING")
	}
}

func TestParseMemoryFileContent_HtmlCommentStripped(t *testing.T) {
	input := "keep\n<!-- comment -->\nthis"
	result := parseMemoryFileContent(input, "/p/TINYCLUE.md", MemoryTypeProject, "")
	if result == nil || strings.Contains(result.Content, "comment") {
		t.Fatalf("HTML comments should be stripped, got: %q", result.Content)
	}
}

// --- resolveSymlink ---

func TestResolveSymlink_NoSymlink(t *testing.T) {
	// Create temp file
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	result := resolveSymlink(file)
	if result != filepath.Clean(file) {
		t.Fatalf("expected %s, got %s", filepath.Clean(file), result)
	}
}

func TestResolveSymlink_Symlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "link.txt")
	if err := os.WriteFile(target, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.txt", link); err != nil {
		t.Fatal(err)
	}
	result := resolveSymlink(link)
	if result != filepath.Clean(target) {
		t.Fatalf("expected %s, got %s", filepath.Clean(target), result)
	}
}

// --- GetUserContext integration ---

func TestGetUserContext_EmptyConfig(t *testing.T) {
	// Empty Config{} has no User/Project/Local paths, only Managed + AutoMem.
	// In a test environment with no Managed files, TinyclueMd should be empty.
	cfg := Config{}
	ctx, err := GetUserContext(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ctx.CurrentDate, "Today's date is ") {
		t.Fatal("CurrentDate should be formatted")
	}
}

func TestGetUserContext_WithTinyclueMd(t *testing.T) {
	dir := t.TempDir()
	tinyclueMd := filepath.Join(dir, "TINYCLUE.md")
	if err := os.WriteFile(tinyclueMd, []byte("project instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GetDefaultConfig()
	cfg.CWD = dir
	cfg.HomeDir = dir // avoid loading real ~/.tinyclue
	cfg.UserTINYCLUE = ""
	cfg.UserRulesDir = ""
	cfg.LocalFiles = nil
	ctx, err := GetUserContext(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if ctx.TinyclueMd == "" {
		t.Fatal("TinyclueMd should not be empty")
	}
	if !strings.Contains(ctx.TinyclueMd, "project instructions") {
		t.Fatal("should contain file content")
	}
}

func TestGetUserContext_WithProjectFiles(t *testing.T) {
	dir := t.TempDir()
	tinyclueMd := filepath.Join(dir, "TINYCLUE.md")
	if err := os.WriteFile(tinyclueMd, []byte("project instructions"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := GetDefaultConfig()
	cfg.CWD = dir
	cfg.HomeDir = dir
	cfg.UserTINYCLUE = ""
	cfg.UserRulesDir = ""
	cfg.LocalFiles = nil
	ctx, err := GetUserContext(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx.TinyclueMd, "project instructions") {
		t.Fatal("Project files should be included")
	}
}

// --- parseMemoryFileContent include paths ---

func TestParseMemoryFileContent_IncludePaths(t *testing.T) {
	input := "Check @./other.md for details"
	result := parseMemoryFileContent(input, "/base/TINYCLUE.md", MemoryTypeProject, "/base")
	if result == nil {
		t.Fatal("should parse")
	}
	if len(result.IncludePaths) == 0 {
		t.Fatal("should have include paths")
	}
	found := false
	for _, p := range result.IncludePaths {
		if strings.HasSuffix(p, "/base/other.md") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("include paths don't contain other.md: %v", result.IncludePaths)
	}
}

func TestParseMemoryFileContent_NoIncludeBase(t *testing.T) {
	input := "Check @./other.md"
	result := parseMemoryFileContent(input, "/base/TINYCLUE.md", MemoryTypeProject, "")
	if result == nil {
		t.Fatal("should parse")
	}
	if len(result.IncludePaths) > 0 {
		t.Fatal("should not extract includes when includeBasePath is empty")
	}
}

// --- processMdRules with non-existent dir ---

func TestProcessMdRules_NonExistentDir(t *testing.T) {
	result, err := processMdRules("/nonexistent/dir", MemoryTypeProject, make(map[string]bool), false, false)
	if err == nil {
		t.Fatal("should return error for non-existent dir")
	}
	if result != nil {
		t.Fatal("result should be nil on error")
	}
}

// --- getMemoryFile processing with empty content ---

func TestProcessMemoryFile_NonExistent(t *testing.T) {
	result, err := processMemoryFile("/nonexistent/file.md", MemoryTypeProject, make(map[string]bool), false, 0, "")
	if err != nil {
		t.Fatal("non-existent files should not error (they don't exist)")
	}
	if result != nil {
		t.Fatal("result should be nil")
	}
}

// --- extractPaths @path validation ---

func TestExtractPathsFromText_Invalid(t *testing.T) {
	paths := make(map[string]bool)
	extractPathsFromText("use @#invalid", "/base", paths)
	if len(paths) > 0 {
		t.Fatal("@# should not be valid")
	}
}

func TestExtractPathsFromText_ValidBare(t *testing.T) {
	paths := make(map[string]bool)
	extractPathsFromText("use @file.md", "/base/dir", paths)
	if len(paths) == 0 {
		t.Fatal("@file.md should be valid as bare path")
	}
}

// --- expandPath ~ expansion ---

func TestExpandPath_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	result := expandPath("~/docs", "/base")
	if !strings.HasPrefix(result, home) {
		t.Fatalf("expected home dir prefix, got %s", result)
	}
}

// --- getAutoMemPath defaults ---

func TestGetAutoMemPath_Default(t *testing.T) {
	homeDir, _ := os.UserHomeDir()
	path := GetAutoMemPath(homeDir, "/tmp")
	if path == "" {
		t.Fatal("should produce a path")
	}
	if !strings.HasSuffix(path, "memory/") && !strings.HasSuffix(path, "memory"+string(filepath.Separator)) {
		t.Fatalf("path should end with memory/ : %s", path)
	}
}

// --- getTinyclueMds description strings ---

func TestGetTinyclueMds_Descriptions(t *testing.T) {
	tests := []struct {
		typ          MemoryType
		descContains string
	}{
		{MemoryTypeProject, "project instructions"},
		{MemoryTypeLocal, "not checked in"},
		{MemoryTypeAutoMem, "auto-memory"},
		{MemoryTypeTeamMem, "shared team memory"},
		{MemoryTypeUser, "private global instructions"},
		{MemoryTypeManaged, "private global instructions"},
	}
	for _, tt := range tests {
		files := []MemoryFileInfo{
			{Path: "/p", Type: tt.typ, Content: "content"},
		}
		result := getTinyclueMds(files)
		if !strings.Contains(result, tt.descContains) {
			t.Errorf("type %s should have description containing %q", tt.typ, tt.descContains)
		}
	}
}

// --- Config getHomeDir ---

func TestConfigGetHomeDir_Override(t *testing.T) {
	cfg := Config{HomeDir: "/custom/home"}
	if cfg.getHomeDir() != "/custom/home" {
		t.Fatal("should use overridden home dir")
	}
}

func TestConfigGetHomeDir_Default(t *testing.T) {
	cfg := Config{}
	home := cfg.getHomeDir()
	if home == "" {
		t.Fatal("should resolve home dir")
	}
}
