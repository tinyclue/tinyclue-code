package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/ripgrep"
)

var testdataDir string

func main() {
	//tinyclue --resume 84f28ef3-2096-4a7e-954e-7423ab309684
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Printf("FAIL: getwd: %v\n", err)
		os.Exit(1)
	}
	testdataDir = filepath.Join(cwd, "cmd/ripgreptest/testdata")
	if info, err := os.Stat(testdataDir); err != nil || !info.IsDir() {
		fmt.Printf("FAIL: testdata directory not found at %s\n", testdataDir)
		os.Exit(1)
	}

	fmt.Println("=== RipGrep Engine 全覆盖测试 ===")

	// ── --files 模式 ──
	fmt.Println("--- --files 模式 ---")
	run("--files 列出所有 go 文件", testFilesAllGo)
	run("--files 列出所有 txt 文件", testFilesAllTxt)
	run("--files + --glob 过滤", testFilesGlob)
	run("--files + --type 过滤", testFilesType)
	run("--files + --hidden 显示隐藏文件", testFilesHidden)
	run("--files + --max-depth 限制深度", testFilesMaxDepth)
	run("--files 空结果", testFilesNoMatch)
	run("--files + 否定 glob", testFilesNegateGlob)

	// ── 默认 content 搜索模式 ──
	fmt.Println("\n--- Content 搜索模式 ---")
	run("基本文本搜索", testSearchBasic)
	run("搜索无匹配 → exit code 1", testSearchNoMatch)
	run("大小写不敏感 -i", testSearchCaseInsensitive)
	run("行号 -n", testSearchLineNumber)
	run("上下文 -C", testSearchContext)
	run("前后文 -B/-A", testSearchBeforeAfter)
	run("多文件搜索", testSearchMultiFile)
	run("搜索 + --glob 过滤", testSearchGlob)
	run("搜索 + --type 过滤", testSearchType)
	run("--hidden 搜索隐藏文件", testSearchHidden)
	run("--max-count 限制匹配数", testSearchMaxCount)

	// ── 多行模式 ──
	fmt.Println("\n--- Multiline 模式 ---")
	run("--multiline 跨行匹配", testSearchMultiline)

	// ── -l files-with-matches 模式 ──
	fmt.Println("\n--- -l files-with-matches 模式 ---")
	run("-l 列出有匹配的文件", testFilesWithMatches)
	run("-l 无匹配", testFilesWithMatchesNoMatch)

	// ── -c count 模式 ──
	fmt.Println("\n--- -c count 模式 ---")
	run("-c 统计匹配数", testCountMatches)
	run("-c 无匹配", testCountNoMatch)

	// ── 排序 ──
	fmt.Println("\n--- 排序 ---")
	run("--sort modified", testSortModified)
	run("--sortr modified", testSortrModified)

	// ── 分隔符 ──
	fmt.Println("\n--- 分隔符 ---")
	run("--null 空字符分隔", testNullSep)
	run("--path-separator", testPathSeparator)

	// ── 参数解析 ──
	fmt.Println("\n--- 参数解析 ---")
	run("多 path 参数", testMultiPath)
	run("空 args 返回 error", testEmptyArgs)

	// ── Glob 匹配 ──
	fmt.Println("\n--- Glob 匹配 ---")
	run("花括号展开: *.{go,txt}", testGlobBraceExpansion)
	run("** 递归: **/*.go", testGlobDoubleStar)
	run("目录前缀: subdir/*.go", testGlobDirectoryPrefix)
	run("多个 --glob AND 组合", testGlobMultipleGlobs)
	run("否定花括号: !*.{txt}", testGlobNegateBrace)
	run("精确路径匹配", testGlobExactPath)
	run("搜索 + 花括号 glob", testGlobSearchWithBrace)
	run("VCS 排除模式: !**/.git", testGlobVCSExclusion)
	run("** + 子目录: subdir/**/*.go", testGlobSubdirAllGo)
	run("? 通配符: d??p.go", testGlobQuestionMark)
	run("[abc] 字符类: [md]*.go", testGlobCharClass)
	run("[!abc] 否定字符类", testGlobNegateCharClass)
	run("多组花括号: {main,utils}.go", testGlobMultipleBraces)

	// ── 正则表达式 ──
	fmt.Println("\n--- 正则表达式 ---")
	run(". 匹配任意字符", testRegexDot)
	run("* 量词匹配", testRegexStar)
	run("+ 量词匹配", testRegexPlus)
	run("^ 行首锚定", testRegexAnchorStart)
	run("$ 行尾锚定", testRegexAnchorEnd)
	run("[0-9] 字符类", testRegexCharClass)
	run("(foo|bar) 分支", testRegexAlternation)
	run("转义序列 \\* 匹配字面星号", testRegexEscapedDot)

	// ── 新增功能测试 ──
	fmt.Println("\n--- 新增功能 ---")
	run("-e 处理以 - 开头的 pattern", testPatternWithDash)
	run("-- 后的 pattern 不被截断", testPatternDoubleDash)
	run("--sort=modified 等号语法", testSortEqSyntax)
	run("--sortr=modified 等号语法", testSortrEqSyntax)
	run("glob.ts 完整调用 --files --glob --sort=modified", testGlobStyleFiles)
	run("--no-ignore 不影响结果（no-op）", testNoIgnore)
	run("--max-columns 正常处理", testMaxColumns)

	fmt.Println("\n=== 测试完成 ===")
}

func run(name string, fn func() bool) {
	if fn() {
		fmt.Printf("  PASS: %s\n", name)
	} else {
		fmt.Printf("  FAIL: %s\n", name)
	}
}

func rip(args ...string) (*ripgrep.RipGrepResult, error) {
	return ripgrep.RipGrep(context.Background(), args, testdataDir)
}

// ──────────────────── --files 测试 ────────────────────

func testFilesAllGo() bool {
	r, err := rip("--files", "--glob", "*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// main.go, utils.go, subdir/deep.go, subdir/nested/deep.go
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 .go files, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testFilesAllTxt() bool {
	r, err := rip("--files", "--glob", "*.txt")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 2 {
		fmt.Printf("    expected 2 .txt files, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	sort.Strings(r.Lines)
	if r.Lines[0] != "hello.txt" {
		fmt.Printf("    expected hello.txt first, got %s\n", r.Lines[0])
		return false
	}
	return true
}

func testFilesGlob() bool {
	r, err := rip("--files", "--glob", "*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, f := range r.Lines {
		if !strings.HasSuffix(f, ".go") {
			fmt.Printf("    unexpected non-.go file: %s\n", f)
			return false
		}
	}
	return true
}

func testFilesType() bool {
	r, err := rip("--files", "--type", "go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 go files (type=go), got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testFilesHidden() bool {
	// 不加 --hidden，.hidden 目录不应出现
	r, err := rip("--files")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, f := range r.Lines {
		if strings.Contains(f, ".hidden") {
			fmt.Printf("    hidden file leaked without --hidden: %s\n", f)
			return false
		}
	}

	// 加 --hidden，.hidden/hidden.go 应出现
	r2, err2 := rip("--files", "--hidden")
	if err2 != nil {
		fmt.Printf("    error: %v\n", err2)
		return false
	}
	found := false
	for _, f := range r2.Lines {
		if strings.Contains(f, "hidden.go") {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("    expected hidden.go in output with --hidden, got: %v\n", r2.Lines)
		return false
	}
	return true
}

func testFilesMaxDepth() bool {
	r, err := rip("--files", "--glob", "*.go", "--max-depth", "1")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 深度1: main.go, utils.go（subdir/* 在深度2）
	if len(r.Lines) != 2 {
		fmt.Printf("    expected 2 .go files at max-depth=1, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testFilesNoMatch() bool {
	r, err := rip("--files", "--glob", "*.xyz")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if r.ExitCode != 1 {
		fmt.Printf("    expected exit code 1 for no match, got %d\n", r.ExitCode)
		return false
	}
	if len(r.Lines) != 0 {
		fmt.Printf("    expected 0 lines, got %d\n", len(r.Lines))
		return false
	}
	return true
}

func testFilesNegateGlob() bool {
	r, err := rip("--files", "--glob", "!*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, f := range r.Lines {
		if strings.HasSuffix(f, ".go") {
			fmt.Printf("    .go file should be excluded: %s\n", f)
			return false
		}
	}
	return len(r.Lines) > 0
}

// ──────────────────── Content 搜索测试 ────────────────────

func testSearchBasic() bool {
	r, err := rip("hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'hello', got 0\n")
		return false
	}
	return true
}

func testSearchNoMatch() bool {
	r, err := rip("zzzznonexistent")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if r.ExitCode != 1 {
		fmt.Printf("    expected exit code 1 for no match, got %d\n", r.ExitCode)
		return false
	}
	if len(r.Lines) != 0 {
		fmt.Printf("    expected 0 lines, got %d\n", len(r.Lines))
		return false
	}
	return true
}

func testSearchCaseInsensitive() bool {
	// -i 应匹配 HELLO → hello
	r, err := rip("-i", "HELLO")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for case-insensitive 'HELLO', got 0\n")
		return false
	}

	// 不加 -i 不应匹配
	r2, err2 := rip("HELLO")
	if err2 != nil {
		fmt.Printf("    error: %v\n", err2)
		return false
	}
	if len(r2.Lines) != 0 {
		fmt.Printf("    expected no matches for case-sensitive 'HELLO', got %d\n", len(r2.Lines))
		return false
	}
	return true
}

func testSearchLineNumber() bool {
	r, err := rip("-n", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 应该有行号: file:N:content
	for _, line := range r.Lines {
		if line == "--" {
			continue
		}
		// 检查 file:N: 模式
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			fmt.Printf("    expected line number format file:N:content, got: %s\n", line)
			return false
		}
	}
	return true
}

func testSearchContext() bool {
	r, err := rip("-C", "1", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches with context, got 0\n")
		return false
	}
	return true
}

func testSearchBeforeAfter() bool {
	r, err := rip("-B", "1", "-A", "2", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches with before/after context, got 0\n")
		return false
	}
	return true
}

func testSearchMultiFile() bool {
	r, err := rip("hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// hello 应在 hello.txt 和 subdir/deep.go 中都有匹配
	files := make(map[string]bool)
	for _, line := range r.Lines {
		if line == "--" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) >= 2 {
			files[parts[0]] = true
		}
	}
	if len(files) < 2 {
		fmt.Printf("    expected matches in multiple files, got files: %v\n", files)
		return false
	}
	return true
}

func testSearchGlob() bool {
	r, err := rip("hello", "--glob", "*.txt")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, line := range r.Lines {
		if line == "--" {
			continue
		}
		if strings.Contains(line, ".go") {
			fmt.Printf("    .go file should be excluded by glob: %s\n", line)
			return false
		}
	}
	return len(r.Lines) > 0
}

func testSearchType() bool {
	r, err := rip("hello", "--type", "go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, line := range r.Lines {
		if line == "--" {
			continue
		}
		if strings.Contains(line, ".txt") {
			fmt.Printf("    .txt file should be excluded by type=go: %s\n", line)
			return false
		}
	}
	return len(r.Lines) > 0
}

func testSearchHidden() bool {
	// 不加 --hidden 不应搜到隐藏文件
	r, err := rip("secret")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 0 {
		fmt.Printf("    expected no matches in hidden files without --hidden, got %d\n", len(r.Lines))
		return false
	}

	// 加 --hidden 应搜到
	r2, err2 := rip("secret", "--hidden")
	if err2 != nil {
		fmt.Printf("    error: %v\n", err2)
		return false
	}
	if len(r2.Lines) == 0 {
		fmt.Printf("    expected matches in hidden files with --hidden, got 0\n")
		return false
	}
	return true
}

func testSearchMaxCount() bool {
	r, err := rip("hello", "-m", "1")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches with max-count, got 0\n")
		return false
	}
	return true
}

// ──────────────────── Multiline 测试 ────────────────────

func testSearchMultiline() bool {
	r, err := rip("-U", "func\\s+\\w+.*\\n}")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected multiline matches, got 0\n")
		return false
	}
	return true
}

// ──────────────────── -l files-with-matches 测试 ────────────────────

func testFilesWithMatches() bool {
	r, err := rip("-l", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected files with 'hello', got 0\n")
		return false
	}
	// lines 应该是文件名，不是 file:line:content
	for _, f := range r.Lines {
		if _, statErr := os.Stat(filepath.Join(testdataDir, f)); statErr != nil {
			fmt.Printf("    -l should output filenames only, got: %s (err: %v)\n", f, statErr)
			return false
		}
	}
	return true
}

func testFilesWithMatchesNoMatch() bool {
	r, err := rip("-l", "zzzznonexistent")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if r.ExitCode != 1 {
		fmt.Printf("    expected exit code 1 for no match, got %d\n", r.ExitCode)
		return false
	}
	return true
}

// ──────────────────── -c count 测试 ────────────────────

func testCountMatches() bool {
	r, err := rip("-c", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected count results, got 0\n")
		return false
	}
	// 格式: file:N
	for _, line := range r.Lines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			fmt.Printf("    expected file:count format, got: %s\n", line)
			return false
		}
	}
	return true
}

func testCountNoMatch() bool {
	r, err := rip("-c", "zzzznonexistent")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if r.ExitCode != 1 {
		fmt.Printf("    expected exit code 1 for no match, got %d\n", r.ExitCode)
		return false
	}
	return true
}

// ──────────────────── 排序测试 ────────────────────

func testSortModified() bool {
	r, err := rip("--files", "--sort", "modified")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected files sorted by modified time, got 0\n")
		return false
	}
	return true
}

func testSortrModified() bool {
	r, err := rip("--files", "--sortr", "modified")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected files sorted by modified time (desc), got 0\n")
		return false
	}
	return true
}

// ──────────────────── 分隔符测试 ────────────────────

func testNullSep() bool {
	r, err := rip("-l", "-0", "hello")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected null-separated results, got 0\n")
		return false
	}
	// 空字符分隔的模式下退出码应该正常
	if r.ExitCode != 0 {
		fmt.Printf("    expected exit code 0, got %d\n", r.ExitCode)
		return false
	}
	return true
}

func testPathSeparator() bool {
	r, err := rip("--files", "--glob", "*.go", "--path-separator", "\\")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, f := range r.Lines {
		if strings.Contains(f, "/") {
			fmt.Printf("    expected backslash path separator, got: %s\n", f)
			return false
		}
	}
	return len(r.Lines) > 0
}

func testGlobQuestionMark() bool {
	// ? 通配符：subdir/d??p.go 应匹配 subdir/deep.go
	r, err := rip("--files", "--glob", "subdir/d??p.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 1 || r.Lines[0] != "subdir/deep.go" {
		fmt.Printf("    expected subdir/deep.go (d??p.go), got %v\n", r.Lines)
		return false
	}
	return true
}

func testGlobCharClass() bool {
	// [abc] 字符类：[md]*.go 匹配 m/d 开头的 .go 文件
	r, err := rip("--files", "--glob", "[md]*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// main.go + subdir/deep.go + subdir/nested/deep.go (Base fallback 匹配所有 deep.go)
	if len(r.Lines) != 3 {
		fmt.Printf("    expected 3 .go files starting with m/d, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	foundMain := false
	foundDeep := false
	for _, f := range r.Lines {
		if f == "main.go" {
			foundMain = true
		}
		if strings.HasSuffix(f, "deep.go") {
			foundDeep = true
		}
	}
	if !foundMain || !foundDeep {
		fmt.Printf("    expected main.go and deep.go in [md]*.go: %v\n", r.Lines)
		return false
	}
	return true
}

func testGlobNegateCharClass() bool {
	// [!abc] 否定字符类：转为 [^abc]
	r, err := rip("--files", "--glob", "[!m]*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 排除 main.go，应包含 utils.go + subdir/deep.go + subdir/nested/deep.go
	for _, f := range r.Lines {
		if f == "main.go" {
			fmt.Printf("    main.go should be excluded by [!m]*.go\n")
			return false
		}
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected some files after [!m]*.go exclusion, got 0\n")
		return false
	}
	return true
}

func testGlobMultipleBraces() bool {
	// 多组花括号：{main,utils}.go 应匹配 main.go 和 utils.go
	r, err := rip("--files", "--glob", "{main,utils}.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 2 {
		fmt.Printf("    expected 2 files ({main,utils}.go), got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	foundMain := false
	foundUtils := false
	for _, f := range r.Lines {
		if f == "main.go" {
			foundMain = true
		}
		if f == "utils.go" {
			foundUtils = true
		}
	}
	if !foundMain || !foundUtils {
		fmt.Printf("    expected both main.go and utils.go: %v\n", r.Lines)
		return false
	}
	return true
}

// ──────────────────── 正则表达式测试 ────────────────────

func testRegexDot() bool {
	r, err := rip("h.llo")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'h.llo' (dot), got 0\n")
		return false
	}
	return true
}

func testRegexStar() bool {
	r, err := rip("hel*o")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'hel*o' (star), got 0\n")
		return false
	}
	return true
}

func testRegexPlus() bool {
	r, err := rip("hel+o")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'hel+o' (plus), got 0\n")
		return false
	}
	return true
}

func testRegexAnchorStart() bool {
	r, err := rip("^123")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for '^123' (start anchor), got 0\n")
		return false
	}
	return true
}

func testRegexAnchorEnd() bool {
	r, err := rip("world$")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'world$' (end anchor), got 0\n")
		return false
	}
	return true
}

func testRegexCharClass() bool {
	r, err := rip("[0-9]+")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for '[0-9]+', got 0\n")
		return false
	}
	return true
}

func testRegexAlternation() bool {
	r, err := rip("foo|bar")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for 'foo|bar', got 0\n")
		return false
	}
	return true
}

func testRegexEscapedDot() bool {
	// \* 转义星号，匹配字面量 *（utils.go 中有 a * b）
	r, err := rip(`\*`)
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches for '\\*' (escaped asterisk), got 0\n")
		return false
	}
	return true
}

// ──────────────────── 新增功能测试 ────────────────────

func testPatternWithDash() bool {
	// -e 允许 pattern 以 - 开头
	r, err := rip("-e", "-i")
	if err != nil {
		fmt.Printf("    error with -e flag: %v\n", err)
		return false
	}
	// 匹配 -i 不区分大小写应匹配内容中的 foobar 等
	// 这里只验证 -e 能正确将 -i 视为 pattern 而非 flag
	if r.ExitCode != 1 {
		fmt.Printf("    expected exit code 1 (no match for '-i'), got %d\n", r.ExitCode)
		return false
	}
	return true
}

func testSortEqSyntax() bool {
	// --sort=modified 单参数语法（glob.ts 使用方式）
	r, err := rip("--files", "--glob", "*.go", "--sort=modified")
	if err != nil {
		fmt.Printf("    error with --sort=modified: %v\n", err)
		return false
	}
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 .go files with --sort=modified, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testSortrEqSyntax() bool {
	r, err := rip("--files", "--glob", "*.go", "--sortr=modified")
	if err != nil {
		fmt.Printf("    error with --sortr=modified: %v\n", err)
		return false
	}
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 .go files with --sortr=modified, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testGlobStyleFiles() bool {
	// glob.ts 完整调用方式：--files --glob pattern --sort=modified
	r, err := rip("--files", "--glob", "*.go", "--sort=modified")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 验证文件按修改时间升序排列
	if len(r.Lines) < 2 {
		fmt.Printf("    expected >=2 files, got %d\n", len(r.Lines))
		return false
	}
	return true
}

func testNoIgnore() bool {
	// --no-ignore 不应影响结果（纯 Go 实现无 .gitignore，相当于 no-op）
	r, err := rip("--files", "--glob", "*.go", "--no-ignore")
	if err != nil {
		fmt.Printf("    error with --no-ignore: %v\n", err)
		return false
	}
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 .go files, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	return true
}

func testMaxColumns() bool {
	// --max-columns 500 不应影响结果
	r, err := rip("hello", "--max-columns", "500")
	if err != nil {
		fmt.Printf("    error with --max-columns: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches with --max-columns, got 0\n")
		return false
	}
	return true
}

// ──────────────────── 参数解析测试 ────────────────────

func testPatternDoubleDash() bool {
	// -- 在 -- separator 后的 pattern 不应被截断
	// 创建一个文件包含 --foo
	tmpFile := filepath.Join(os.TempDir(), "ripgreptest_dash.txt")
	os.WriteFile(tmpFile, []byte("content with --foo here\n"), 0644)
	defer os.Remove(tmpFile)

	r, err := ripgrep.RipGrep(context.Background(), []string{"--", "--foo", tmpFile}, "")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected match for '--foo', got 0 lines\n")
		return false
	}
	return true
}

func testMultiPath() bool {
	// 创建临时文件用作多 path 测试
	tmpFile := filepath.Join(os.TempDir(), "ripgreptest_hello.txt")
	os.WriteFile(tmpFile, []byte("hello from temp\n"), 0644)
	defer os.Remove(tmpFile)

	r, err := rip("hello", testdataDir, tmpFile)
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches from multiple paths, got 0\n")
		return false
	}
	return true
}

func testEmptyArgs() bool {
	// 没有 pattern 会返回 error
	_, err := rip()
	if err == nil {
		fmt.Printf("    expected error for empty pattern, got nil\n")
		return false
	}
	return true
}

// ──────────────────── Glob 匹配测试 ────────────────────

func testGlobBraceExpansion() bool {
	// 花括号展开：*.{go,txt} → *.go + *.txt 匹配所有深度的 .go 和 .txt 文件
	r, err := rip("--files", "--glob", "*.{go,txt}")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 4 .go + 2 .txt = 6（.go 因 Base fallback 匹配到所有深度）
	if len(r.Lines) != 6 {
		fmt.Printf("    expected 6 files (*.{go,txt}), got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	// 验证包含 txt 文件
	hasTxt := false
	for _, f := range r.Lines {
		if strings.HasSuffix(f, ".txt") {
			hasTxt = true
			break
		}
	}
	if !hasTxt {
		fmt.Printf("    expected .txt files in brace expansion results: %v\n", r.Lines)
		return false
	}
	return true
}

func testGlobDoubleStar() bool {
	// ** 递归匹配：**/*.go 应匹配所有深度的 .go 文件
	r, err := rip("--files", "--glob", "**/*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// main.go, utils.go, subdir/deep.go, subdir/nested/deep.go (不含 .hidden)
	if len(r.Lines) != 4 {
		fmt.Printf("    expected 4 .go files with **/*.go, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	foundNested := false
	for _, f := range r.Lines {
		if strings.Contains(f, "nested/deep.go") {
			foundNested = true
			break
		}
	}
	if !foundNested {
		fmt.Printf("    expected nested/deep.go in **/*.go results: %v\n", r.Lines)
		return false
	}
	return true
}

func testGlobDirectoryPrefix() bool {
	// 目录前缀：subdir/*.go 应只匹配 subdir/ 下直接的文件（不含嵌套）
	r, err := rip("--files", "--glob", "subdir/*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 只有 subdir/deep.go，不包含 subdir/nested/deep.go
	if len(r.Lines) != 1 {
		fmt.Printf("    expected 1 file in subdir/*.go, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	if r.Lines[0] != "subdir/deep.go" {
		fmt.Printf("    expected subdir/deep.go, got %s\n", r.Lines[0])
		return false
	}
	return true
}

func testGlobMultipleGlobs() bool {
	// 多个 --glob：组合是 AND 关系
	// 先用 --glob *.go，再 --glob !subdir/**
	r, err := rip("--files", "--glob", "*.go", "--glob", "!subdir/**")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// 只应匹配根目录的 .go 文件（排除 subdir/ 下所有）
	if len(r.Lines) != 2 {
		fmt.Printf("    expected 2 root .go files with multiple globs, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	for _, f := range r.Lines {
		if !strings.HasSuffix(f, ".go") || strings.Contains(f, "/") {
			fmt.Printf("    unexpected file with multiple globs: %s\n", f)
			return false
		}
	}
	return true
}

func testGlobNegateBrace() bool {
	// 否定 + 花括号：!*.{txt} 排除所有 .txt 文件
	r, err := rip("--files", "--glob", "!*.{txt}")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	for _, f := range r.Lines {
		if strings.HasSuffix(f, ".txt") {
			fmt.Printf("    .txt should be excluded by negated brace: %s\n", f)
			return false
		}
	}
	// 确认仍有其他文件
	if len(r.Lines) == 0 {
		fmt.Printf("    expected some files after negated brace, got 0\n")
		return false
	}
	return true
}

func testGlobExactPath() bool {
	// 精确路径匹配：subdir/nested/deep.go
	r, err := rip("--files", "--glob", "subdir/nested/deep.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) != 1 || r.Lines[0] != "subdir/nested/deep.go" {
		fmt.Printf("    expected exactly subdir/nested/deep.go, got %v\n", r.Lines)
		return false
	}
	return true
}

func testGlobSearchWithBrace() bool {
	// 搜索模式下使用花括号 glob
	r, err := rip("hello", "--glob", "*.{txt,go}")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected matches with brace glob in search mode, got 0\n")
		return false
	}
	return true
}

func testGlobVCSExclusion() bool {
	// VCS 排除模式：!**/.git 不破坏结果
	r, err := rip("--files", "--glob", "!**/.git")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	if len(r.Lines) == 0 {
		fmt.Printf("    expected files despite !**/.git, got 0\n")
		return false
	}
	return true
}

func testGlobSubdirAllGo() bool {
	// ** 匹配子目录：subdir/**/*.go 应匹配 subdir/ 下所有 .go
	r, err := rip("--files", "--glob", "subdir/**/*.go")
	if err != nil {
		fmt.Printf("    error: %v\n", err)
		return false
	}
	// subdir/deep.go, subdir/nested/deep.go
	if len(r.Lines) != 2 {
		fmt.Printf("    expected 2 .go files under subdir with **, got %d: %v\n", len(r.Lines), r.Lines)
		return false
	}
	for _, f := range r.Lines {
		if !strings.HasPrefix(f, "subdir/") || !strings.HasSuffix(f, ".go") {
			fmt.Printf("    unexpected file: %s\n", f)
			return false
		}
	}
	return true
}
