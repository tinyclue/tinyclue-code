//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/ripgrep"
)

var testDir = "/tmp/ripgrepcheck"

type testCase struct {
	name string
	args []string
	// skipCompare: true 表示只跑不比较（用于已知差异或信息性输出）
	skipCompare bool
	// note: 备注说明
	note string
}

func main() {
	fmt.Printf("测试目录: %s\n", testDir)
	fmt.Printf("文件总数: %d\n\n", countFiles(testDir))

	// ──────────────── 1. --files 模式测试 ────────────────
	filesCases := []testCase{
		// 基础
		{"--files 全部", []string{"--files"}, false, ""},
		{"--files --hidden 含隐藏", []string{"--files", "--hidden"}, false, ""},
		{"--files --no-ignore", []string{"--files", "--no-ignore"}, false, ""},

		// --glob 过滤
		{"--glob *.go", []string{"--files", "--glob", "*.go"}, false, ""},
		{"--glob !*.go (排除)", []string{"--files", "--glob", "!*.go"}, false, ""},
		{"--glob **/*.go", []string{"--files", "--glob", "**/*.go"}, false, ""},
		{"--glob *.{go,txt} (花括号)", []string{"--files", "--glob", "*.{go,txt}"}, false, ""},
		{"--glob *.txt", []string{"--files", "--glob", "*.txt"}, false, ""},
		{"--glob *.*", []string{"--files", "--glob", "*.*"}, true, "rg 会将隐藏文件纳入匹配"},

		// 目录 glob（rg 需要 **/ 前缀，引擎不要求）
		{"--glob subdir/*", []string{"--files", "--glob", "subdir/*"}, true, "rg 无 **/ 前缀时不匹配目录"},
		{"--glob subdir/*.go", []string{"--files", "--glob", "subdir/*.go"}, true, "rg 需要 **/subdir/*.go"},
		{"--glob subdir/**/*.go", []string{"--files", "--glob", "subdir/**/*.go"}, true, "rg 要求显式 **/subdir/**/*.go"},
		{"--glob .hidden/* (隐藏)", []string{"--files", "--hidden", "--glob", ".hidden/*"}, true, "rg 要求 **/.hidden/*"},

		// 字符类 glob
		{"--glob [md]*.go (字符类)", []string{"--files", "--glob", "[md]*.go"}, false, "main.go, deep.go 等"},
		{"--glob [!m]*.go (否定字符类)", []string{"--files", "--glob", "[!m]*.go"}, false, "非 m 开头的 .go"},

		// ? 通配符
		{"--glob ???.* (三个字符)", []string{"--files", "--glob", "???.*"}, false, "如 app.js, data.xml"},

		// --type 过滤
		{"--type go", []string{"--files", "--type", "go"}, false, ""},
		{"--type txt", []string{"--files", "--type", "txt"}, false, ""},
		{"--type md", []string{"--files", "--type", "md"}, false, ""},
		{"--type json", []string{"--files", "--type", "json"}, false, ""},

		// --max-depth
		{"--max-depth 1", []string{"--files", "--max-depth", "1"}, false, ""},
		{"--max-depth 2", []string{"--files", "--max-depth", "2"}, false, ""},
		{"--max-depth 0 (不限)", []string{"--files", "--max-depth", "0"}, false, ""},
	}

	// ──────────────── 2. 内容搜索参数测试 ────────────────
	searchCases := []testCase{
		// 基础搜索
		{"基本文本 hello", []string{"hello"}, false, ""},
		{"大小写不敏感 -i HELLO", []string{"-i", "HELLO"}, false, ""},
		{"大小写敏感 -s hello", []string{"-s", "hello"}, false, ""},
		{"行号 -n", []string{"-n", "hello"}, false, ""},
		{"无行号 -N", []string{"-N", "hello"}, false, ""},

		// 文件名显示
		{"-H 强制文件名", []string{"-H", "hello"}, false, "单文件也显示文件名"},
		{"-I 隐藏文件名", []string{"-I", "hello"}, false, "多文件也隐藏文件名"},

		// 输出控制
		{"-l 仅文件名", []string{"-l", "hello"}, false, ""},
		{"-c 统计", []string{"-c", "hello"}, false, ""},
		{"-c -n 统计+行号", []string{"-c", "-n", "hello"}, false, "rg 忽略 -n，引擎保持"},

		// 上下文
		{"-C 1 上下文", []string{"-C", "1", "hello"}, false, ""},
		{"-C 2 上下文", []string{"-C", "2", "hello"}, false, ""},
		{"-B 1 前文", []string{"-B", "1", "hello"}, false, ""},
		{"-A 1 后文", []string{"-A", "1", "hello"}, false, ""},
		{"-B 1 -A 1 前后文", []string{"-B", "1", "-A", "1", "hello"}, false, ""},

		// 多行
		{"-U 多行 hello\\s+world", []string{"-U", "hello\\s+world"}, false, ""},

		// --max-count
		{"-m 1 最大匹配数", []string{"-m", "1", "hello"}, false, ""},
		{"-m 2 最大匹配数", []string{"-m", "2", "hello"}, false, ""},

		// --max-depth
		{"搜索 --max-depth 1", []string{"--max-depth", "1", "hello"}, false, ""},

		// -0 / --null
		{"-l -0 空分隔", []string{"-l", "-0", "hello"}, false, "\\0 分隔"},
		{"-c -0 空分隔", []string{"-c", "-0", "hello"}, true, "\\0 + count 格式无法直接对比"},

		// -e 显式 pattern
		{"-e 显式 pattern", []string{"-e", "hello"}, false, ""},
		{"-e 配合 -i", []string{"-e", "HELLO", "-i"}, false, ""},

		// --max-columns
		{"--max-columns 10", []string{"--max-columns", "10", "hello"}, false, ""},
	}

	// ──────────────── 3. Glob 语法测试 ────────────────
	// 使用 --files --glob 测试 glob 语法
	globCases := []testCase{
		// 基础通配符
		{"glob: *.go", []string{"--files", "--glob", "*.go"}, false, "匹配所有 .go"},
		{"glob: *.txt", []string{"--files", "--glob", "*.txt"}, false, "匹配所有 .txt"},
		{"glob: *.*", []string{"--files", "--glob", "*.*"}, true, "rg 会将隐藏文件纳入匹配"},

		// ? 通配符
		{"glob: ???.*", []string{"--files", "--glob", "???.*"}, false, "3字符名"},
		{"glob: ???.go", []string{"--files", "--glob", "???.go"}, false, "3字符 .go"},
		{"glob: ????????.txt", []string{"--files", "--glob", "????????.txt"}, false, "8字符 .txt => numbers.txt 不匹配"},

		// 字符类
		{"glob: [md]*.go", []string{"--files", "--glob", "[md]*.go"}, false, "m/d 开头 .go"},
		{"glob: [!m]*.go", []string{"--files", "--glob", "[!m]*.go"}, false, "非 m 开头 .go"},
		{"glob: [a-z]*.go", []string{"--files", "--glob", "[a-z]*.go"}, false, "a-z 开头 .go"},
		{"glob: [!a-z]*.go", []string{"--files", "--glob", "[!a-z]*.go"}, false, "非字母开头 .go（无）"},

		// 花括号展开
		{"glob: *.{go,txt}", []string{"--files", "--glob", "*.{go,txt}"}, false, ".go 或 .txt"},
		{"glob: {main,utils}.go", []string{"--files", "--glob", "{main,utils}.go"}, false, "显式文件名"},
		{"glob: *.{md,json,css}", []string{"--files", "--glob", "*.{md,json,css}"}, false, "多扩展名"},
		{"glob: {subdir,mixed}/*", []string{"--files", "--glob", "{subdir,mixed}/*"}, true, "rg 需 **/ 前缀的目录 glob"},

		// ** 双星号
		{"glob: **/*.go", []string{"--files", "--glob", "**/*.go"}, false, "任意深度 .go"},
		{"glob: **/deep.go", []string{"--files", "--glob", "**/deep.go"}, false, "任意深度 deep.go"},
		{"glob: subdir/**/*.go", []string{"--files", "--glob", "subdir/**/*.go"}, true, "rg 要求 **/subdir/**/*.go"},
		{"glob: **/level3/*", []string{"--files", "--glob", "**/level3/*"}, false, "level3 目录下所有"},

		// 否定
		{"glob: !*.go (排除 .go)", []string{"--files", "--glob", "!*.go"}, false, "排除所有 .go"},

		// 特殊字符文件名
		{"glob: *[[]* (含括号)", []string{"--files", "--glob", "*[[]*"}, false, "文件名含 [ 的文件"},

		// 隐藏文件
		{"glob: .hidden/*", []string{"--files", "--hidden", "--glob", ".hidden/*"}, true, "rg 要求 **/.hidden/*"},
		{"glob: **/.hidden/*", []string{"--files", "--hidden", "--glob", "**/.hidden/*"}, false, "任意 .hidden"},

		// 空结果
		{"glob: *.xyz (无匹配)", []string{"--files", "--glob", "*.xyz"}, false, "期望空结果"},
	}

	// ──────────────── 4. 正则语法测试 ────────────────
	regexCases := []testCase{
		// 字面量
		{"regex: hello", []string{"hello"}, false, ""},
		{"regex: foo", []string{"foo"}, false, ""},

		// . 点号
		{"regex: h.llo", []string{"h.llo"}, false, "点号匹配任意字符"},
		{"regex: foo.bar", []string{"foo.bar"}, false, ""},
		{"regex: foox.bar", []string{"foox.bar"}, false, ""},

		// 锚点
		{"regex: ^start", []string{"^start"}, false, "行首"},
		{"regex: ^123", []string{"^123"}, false, ""},
		{"regex: end$", []string{"end$"}, false, "行尾"},
		{"regex: world$", []string{"world$"}, false, ""},

		// 字符类
		{"regex: [0-9]+", []string{"[0-9]+"}, false, "数字"},
		{"regex: [A-Z]+", []string{"[A-Z]+"}, false, "大写字母"},
		{"regex: [a-z]+", []string{"[a-z]+"}, false, "小写字母"},
		{"regex: [0-9]{3}", []string{"[0-9]{3}"}, false, "3位数字"},
		{"regex: [0-9]{3}-[0-9]{3}", []string{"[0-9]{3}-[0-9]{3}"}, false, "电话号码"},
		{"regex: [^0-9]+", []string{"[^0-9]+"}, false, "非数字"},

		// 重复
		{"regex: foo*", []string{"foo*"}, false, "* 重复"},
		{"regex: foo+", []string{"foo+"}, false, "+ 重复"},
		{"regex: foo.*bar", []string{"foo.*bar"}, false, "dot-star"},
		{"regex: hello.*world", []string{"hello.*world"}, false, ""},
		{"regex: foox?bar", []string{"foox?bar"}, false, "可选"},

		// 转义
		{"regex: \\.go", []string{"\\.go"}, false, "转义点号 -> 字面 .go"},
		{"regex: \\.txt", []string{"\\.txt"}, false, ""},
		{"regex: \\*", []string{"\\*"}, false, "转义星号 -> 字面 *"},

		// 分组和分支
		{"regex: foo|bar", []string{"foo|bar"}, false, "分支"},
		{"regex: (foo|bar)", []string{"(foo|bar)"}, false, "捕获分组"},
		{"regex: (foo|bar)baz", []string{"(foo|bar)baz"}, false, ""},
		{"regex: gr[ea]y", []string{"gr[ea]y"}, false, ""},

		// 简写类
		{"regex: \\s+ (空白)", []string{"\\s+"}, false, ""},
		{"regex: \\d+ (数字)", []string{"\\d+"}, false, ""},
		{"regex: \\w+ (单词)", []string{"\\w+"}, false, ""},

		// Unicode
		{"regex: 你好", []string{"你好"}, false, "unicode 中文"},
		{"regex: こんにちは", []string{"こんにちは"}, false, "unicode 日文"},
		{"regex: [\\p{Han}]+", []string{"[\\p{Han}]+"}, false, "汉字 unicode class"},

		// 复杂组合
		{"regex: ^[a-z]+\\s+[a-z]+", []string{"^[a-z]+\\s+[a-z]+"}, false, "行首单词+空格+单词"},
		{"regex: test@example\\.com", []string{"test@example\\.com"}, false, "email 格式"},
		{"regex: \\d{3}-\\d{4}-\\d{4}", []string{"\\d{3}-\\d{4}-\\d{4}"}, false, "电话格式"},
	}

	// ──────────────── 运行 ────────────────
	allPass := true

	fmt.Println("──── 1. --files 模式参数测试 ────")
	for _, c := range filesCases {
		if !runCompare(c) {
			allPass = false
		}
	}

	fmt.Println("\n──── 2. 内容搜索参数测试 ────")
	for _, c := range searchCases {
		if !runCompare(c) {
			allPass = false
		}
	}

	fmt.Println("\n──── 3. Glob 语法测试 ────")
	for _, c := range globCases {
		if !runCompare(c) {
			allPass = false
		}
	}

	fmt.Println("\n──── 4. 正则语法测试 ────")
	for _, c := range regexCases {
		if !runCompare(c) {
			allPass = false
		}
	}

	if allPass {
		fmt.Println("\n★★★★ 全部一致 ★★★★")
	} else {
		fmt.Printf("\n★★ 有不一致 (%d 个失败) ★★\n", countFails)
		os.Exit(1)
	}
}

var countFails int

func runCompare(c testCase) bool {
	if c.skipCompare {
		fmt.Printf("  SKIP: %s\n", c.name)
		if c.note != "" {
			fmt.Printf("        (%s)\n", c.note)
		}
		return true
	}

	goResult := runGo(c.args)
	rgResult := runRg(c.args)
	ok := compare(c.name, goResult, rgResult)
	if !ok {
		countFails++
		if c.note != "" {
			fmt.Printf("        note: %s\n", c.note)
		}
	}
	return ok
}

func runGo(args []string) string {
	r, err := ripgrep.RipGrep(context.Background(), args, testDir)
	if err != nil {
		return fmt.Sprintf("ERROR: %v", err)
	}
	sort.Strings(r.Lines)
	return strings.Join(r.Lines, "\n")
}

func runRg(args []string) string {
	rgPath := os.Getenv("HOME") + "/bin/rg"

	// 解析 args 判断是否需要补充 flag
	hasN := false
	hasFiles := false
	hasNoIgnore := false
	hasHidden := false
	hasNull := false
	for _, a := range args {
		switch a {
		case "--files":
			hasFiles = true
		case "-n", "--line-number":
			hasN = true
		case "-N", "--no-line-number":
			hasN = true // don't add our own -n
		case "--no-ignore", "--no-ignore-parent", "--no-ignore-vcs":
			hasNoIgnore = true
		case "--hidden", "-.":
			hasHidden = true
		case "-0", "--null":
			hasNull = true
		}
	}

	fullArgs := make([]string, 0, len(args)+4)
	fullArgs = append(fullArgs, args...)

	if !hasN && !hasFiles {
		fullArgs = append(fullArgs, "-n")
	}
	if !hasNoIgnore {
		fullArgs = append(fullArgs, "--no-ignore")
	}
	if hasHidden {
		fullArgs = append(fullArgs, "--hidden")
	}
	fullArgs = append(fullArgs, testDir)

	out, err := exec.Command(rgPath, fullArgs...).CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() != 1 {
				return fmt.Sprintf("RG_ERROR(code=%d): %s", exitErr.ExitCode(), strings.TrimSpace(string(out)))
			}
		}
	}

	if hasNull {
		// NUL 分隔模式：整块输出，统一替换 NUL 为 \n 后排序比较
		cleaned := strings.ReplaceAll(string(out), "\x00", "\n")
		lines := strings.Split(strings.TrimSpace(cleaned), "\n")
		var filtered []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l == "" {
				continue
			}
			filtered = append(filtered, l)
		}
		sort.Strings(filtered)
		return strings.Join(filtered, "\n")
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var filtered []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		filtered = append(filtered, l)
	}
	sort.Strings(filtered)
	return strings.Join(filtered, "\n")
}

func compare(name string, goResult, rgResult string) bool {
	if goResult == rgResult {
		fmt.Printf("  PASS: %s\n", name)
		return true
	}

	fmt.Printf("  FAIL: %s\n", name)

	goLines := strings.Split(goResult, "\n")
	rgLines := strings.Split(rgResult, "\n")
	goSet := make(map[string]bool)
	for _, l := range goLines {
		goSet[l] = true
	}
	rgSet := make(map[string]bool)
	for _, l := range rgLines {
		rgSet[l] = true
	}

	var onlyInGo []string
	var onlyInRg []string
	for _, l := range goLines {
		if !rgSet[l] && l != "" {
			onlyInGo = append(onlyInGo, l)
		}
	}
	for _, l := range rgLines {
		if !goSet[l] && l != "" {
			onlyInRg = append(onlyInRg, l)
		}
	}

	if len(onlyInGo) > 0 {
		fmt.Printf("    only in Go:  %v\n", onlyInGo)
	}
	if len(onlyInRg) > 0 {
		fmt.Printf("    only in rg:  %v\n", onlyInRg)
	}
	if len(onlyInGo) == 0 && len(onlyInRg) == 0 {
		fmt.Printf("    (仅格式差异)\n")
		return true
	}
	return false
}

func countFiles(dir string) int {
	var n int
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}
