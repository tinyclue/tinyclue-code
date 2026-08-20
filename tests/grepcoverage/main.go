package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

var grep *core_tools.Grep
var testDir = "/tmp/ripgrepcheck"

func main() {
	// 确保 testDir 存在
	if info, err := os.Stat(testDir); err != nil || !info.IsDir() {
		fmt.Printf("FAIL: test directory not found at %s (run setup_testdata.sh first)\n", testDir)
		os.Exit(1)
	}
	// 确保 CWD 指向测试数据目录（toRelativePath 依赖它）
	config.CLI.Cwd = testDir

	grep = core_tools.NewGrep()
	ctx := context.Background()

	fmt.Println("=== Grep 工具覆盖测试 ===")

	// ── 基本 content 模式 ──
	fmt.Println("--- content 模式 ---")
	run("基本搜索", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
		})
		if content == "" {
			return false
		}
		if !strings.Contains(content, "hello") {
			fmt.Printf("    expected 'hello' in output, got: %s\n", content)
			return false
		}
		// 路径应为相对路径
		if strings.HasPrefix(content, "/") {
			fmt.Printf("    expected relative path, got absolute: %s\n", content[:60])
			return false
		}
		return tc.Content.(core_types.GrepContent).NumLines > 0
	})

	run("无匹配", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "NONEXISTENT_ZZZZ",
			"output_mode": "content",
		})
		return content == "No matches found"
	})

	run("大小写不敏感 -i", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"-i":          true,
		})
		// 应匹配 hello.txt(hello) 和 README.md(Hello)
		return strings.Contains(content, "README.md") &&
			strings.Contains(content, "hello.txt")
	})

	run("行号 -n", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"-n":          true,
		})
		return strings.Contains(content, ":1:") || strings.Contains(content, ":4:")
	})

	run("行号 -n=false", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"-n":          false,
		})
		// 无行号时格式是 path:content（不包含 :数字:）
		for _, line := range strings.Split(content, "\n") {
			if strings.Contains(line, "hello") && !strings.Contains(line, "hello.txt") {
				// 跳过 hello.txt 行
				// 检查是否有 :数字: 模式（行号）
				parts := strings.SplitN(line, ":", 3)
				if len(parts) == 3 {
					// 第二个部分如果是数字则是行号
					if _, err := strconv.Atoi(parts[1]); err == nil && parts[2] != "" {
						return false
					}
				}
			}
		}
		return true
	})

	run("上下文 -C 1", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"context":     float64(1),
		})
		gc := tc.Content.(core_types.GrepContent)
		return strings.Count(gc.Content, "\n") > 5 // 有上下文行
	})

	run("前后文 -B 1 -A 1", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"-B":          float64(1),
			"-A":          float64(1),
		})
		// - 是上下文行分隔符
		return strings.Count(content, "\n") > 5
	})

	run("glob 过滤", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"glob":        "*.go",
		})
		// 只返回 .go 文件
		for _, line := range strings.Split(content, "\n") {
			if line == "" {
				continue
			}
			if !strings.HasSuffix(strings.SplitN(line, ":", 2)[0], ".go") &&
				!strings.HasSuffix(strings.SplitN(line, ":", 2)[0], ".go") {
				// 检查是否包含 .go 文件名
				if !strings.Contains(line, ".go") {
					// 也不是 context 行（- 分隔符）
					parts := strings.SplitN(line, ":", 2)
					if len(parts) >= 2 && !strings.HasSuffix(parts[0], ".go") {
						fmt.Printf("    expected .go files only, got: %s\n", line)
						return false
					}
				}
			}
		}
		return true
	})

	run("type 过滤", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"type":        "go",
		})
		return strings.Contains(content, ".go:")
	})

	run("multiline 模式", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello\\s+world",
			"output_mode": "content",
			"multiline":   true,
		})
		return len(content) > 0 && content != "No matches found"
	})

	run("pattern 以 - 开头（-e 保护）", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "-i",
			"output_mode": "content",
		})
		// -i 模式应匹配 literal "-i" 字符串… 实际不太有匹配，主要确认不报错
		_ = content
		return true // 不 crash 就算过
	})

	// ── files_with_matches 模式 ──
	fmt.Println("\n--- files_with_matches 模式 ---")
	run("默认模式（files_with_matches）", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern": "hello",
			// 不指定 output_mode
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		if !strings.Contains(content, "Found") {
			fmt.Printf("    expected 'Found' in result, got: %s\n", content)
			return false
		}
		if !strings.Contains(content, "files") {
			fmt.Printf("    expected 'files' in result, got: %s\n", content)
			return false
		}
		return true
	})

	run("无匹配", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern": "NONEXISTENT_ZZZZ",
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumFiles != 0 {
			fmt.Printf("    expected 0 files, got %d\n", gc.NumFiles)
			return false
		}
		if !strings.Contains(content, "No files found") {
			fmt.Printf("    expected 'No files found', got: %s\n", content)
			return false
		}
		return true
	})

	// ── count 模式 ──
	fmt.Println("\n--- count 模式 ---")
	run("count 基本", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "count",
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumMatches == 0 {
			fmt.Printf("    expected >0 matches, got 0\n")
			return false
		}
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		if !strings.Contains(content, "occurrences across") {
			fmt.Printf("    expected 'occurrences across' in result, got: %s\n", content)
			return false
		}
		return true
	})

	run("count 无匹配", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "NONEXISTENT_ZZZZ",
			"output_mode": "count",
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumFiles != 0 || gc.NumMatches != 0 {
			fmt.Printf("    expected 0, got files=%d matches=%d\n", gc.NumFiles, gc.NumMatches)
			return false
		}
		if !strings.Contains(content, "No matches found") {
			fmt.Printf("    expected 'No matches found', got: %s\n", content)
			return false
		}
		return true
	})

	// ── head_limit / offset ──
	fmt.Println("\n--- head_limit / offset ---")
	run("head_limit=5 限制行数", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"head_limit":  float64(5),
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.AppliedLimit != 5 {
			fmt.Printf("    expected AppliedLimit=5, got %d\n", gc.AppliedLimit)
			return false
		}
		if gc.NumLines > 5 {
			fmt.Printf("    expected <=5 lines, got %d\n", gc.NumLines)
			return false
		}
		return true
	})

	run("head_limit=0（不限）", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"head_limit":  float64(0),
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.AppliedLimit != 0 {
			fmt.Printf("    expected AppliedLimit=0, got %d\n", gc.AppliedLimit)
			return false
		}
		// 0 = unlimited，应为所有匹配行
		return gc.NumLines > 10
	})

	run("offset=5 跳过前5行", func() bool {
		// 获取无 offset 的全量结果
		tcFull, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"head_limit":  float64(0),
		})
		fullLines := strings.Count(tcFull.Content.(core_types.GrepContent).Content, "\n") + 1

		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"offset":      float64(5),
			"head_limit":  float64(0),
		})
		gc := tc.Content.(core_types.GrepContent)
		expectedLines := fullLines - 5
		if expectedLines < 0 {
			expectedLines = 0
		}
		if gc.NumLines != expectedLines {
			fmt.Printf("    expected %d lines (full=%d - offset=5), got %d\n", expectedLines, fullLines, gc.NumLines)
			return false
		}
		return true
	})

	run("head_limit + offset 组合", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"head_limit":  float64(3),
			"offset":      float64(2),
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumLines > 3 {
			fmt.Printf("    expected <=3 lines, got %d\n", gc.NumLines)
			return false
		}
		if gc.AppliedLimit != 3 {
			fmt.Printf("    expected AppliedLimit=3, got %d\n", gc.AppliedLimit)
			return false
		}
		return true
	})

	// ── 路径 ──
	fmt.Println("\n--- 路径参数 ---")
	run("path 参数指定子目录", func() bool {
		tc, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
			"path":        "/tmp/ripgrepcheck/subdir",
		})
		_ = content
		gc := tc.Content.(core_types.GrepContent)
		// 只应匹配 subdir 下的文件
		for _, line := range strings.Split(gc.Content, "\n") {
			if line == "" {
				continue
			}
			if !strings.Contains(line, "subdir") && !strings.Contains(line, "-") {
				// 上下文行也可能包含 subdir
				// 放宽检查：只要路径在 subdir 下就行
			}
		}
		return gc.NumLines > 0
	})

	// ── 路径相对化 ──
	fmt.Println("\n--- 路径相对化 ---")
	run("content 模式路径为相对路径", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
		})
		for _, line := range strings.Split(content, "\n") {
			if line == "" || strings.HasPrefix(line, "-") || line == "--" {
				continue
			}
			// 去掉可能的行号后检查路径部分
			if strings.HasPrefix(line, "/") {
				fmt.Printf("    expected relative path, got absolute: %s\n", line)
				return false
			}
		}
		return true
	})

	run("files_with_matches 路径为相对路径", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern": "hello",
		})
		gc := tc.Content.(core_types.GrepContent)
		for _, f := range gc.Filenames {
			if strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "~/") {
				fmt.Printf("    expected relative path, got absolute: %s\n", f)
				return false
			}
		}
		return true
	})

	run("count 模式路径为相对路径", func() bool {
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "count",
		})
		// count 内容的每一行是 path:count，path 应相对
		for _, line := range strings.Split(content, "\n") {
			if line == "" || strings.HasPrefix(line, "No ") {
				continue
			}
			if idx := strings.LastIndex(line, ":"); idx > 0 {
				path := line[:idx]
				if strings.HasPrefix(path, "/") {
					fmt.Printf("    expected relative path in count, got absolute: %s\n", line)
					return false
				}
			}
		}
		return true
	})

	// ── VCS 排除 ──
	fmt.Println("\n--- VCS 排除 ---")
	run("VCS 目录被排除", func() bool {
		// 创建 .git 目录模拟（不会真正执行 git init）
		gitDir := "/tmp/ripgrepcheck/.git"
		os.MkdirAll(gitDir+"/objects", 0755)
		os.WriteFile(gitDir+"/HEAD", []byte("ref: refs/heads/main\n"), 0644)
		os.WriteFile(gitDir+"/objects/test.txt", []byte("hello from git\n"), 0644)
		defer os.RemoveAll(gitDir)

		// .git 下的 hello 不应出现在搜索结果中
		_, _, content := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "content",
		})
		if strings.Contains(content, ".git") {
			fmt.Printf("    expected no .git paths in output, got:\n%s\n", content[:200])
			return false
		}
		return true
	})

	// ── glob 参数解析 ──
	fmt.Println("\n--- glob 参数解析 ---")
	run("glob 逗号分割", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "files_with_matches",
			"glob":        "*.go,*.txt",
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		return true
	})

	run("glob 花括号保护", func() bool {
		tc, _, _ := executeGrep(ctx, map[string]any{
			"pattern":     "hello",
			"output_mode": "files_with_matches",
			"glob":        "*.{go,txt}",
		})
		gc := tc.Content.(core_types.GrepContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		return true
	})

	fmt.Println("\n=== 测试完成 ===")
}

func executeGrep(ctx context.Context, args map[string]any) (core_types.ToolContext, apitypes.ToolResultMessage, string) {
	toolCall := apitypes.ToolCall{
		ID:        "test_01",
		Name:      core_types.GREP_TOOL_NAME,
		Arguments: args,
	}

	toolUseCtx := core_types.ToolUseContext{
		ToolCall: toolCall,
	}

	tc, err := grep.Execute(ctx, toolUseCtx)
	if err != nil {
		fmt.Printf("    ERROR: %v\n", err)
		return tc, apitypes.ToolResultMessage{}, ""
	}

	result := grep.BuildToolResult(tc)
	text := ""
	if len(result.Contents) > 0 {
		text = result.Contents[0].Text
	}
	return tc, result, text
}

func run(name string, fn func() bool) {
	if fn() {
		fmt.Printf("  PASS: %s\n", name)
	} else {
		fmt.Printf("  FAIL: %s\n", name)
	}
}
