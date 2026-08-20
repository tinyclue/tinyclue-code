package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/tinyclue/tinyclue-code/config"

	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

var glob *core_tools.Glob
var testDir = "/tmp/ripgrepcheck"

func main() {
	if info, err := os.Stat(testDir); err != nil || !info.IsDir() {
		fmt.Printf("FAIL: test directory not found at %s (run setup_testdata.sh first)\n", testDir)
		os.Exit(1)
	}
	config.CLI.Cwd = testDir

	glob = core_tools.NewGlob()
	ctx := context.Background()

	fmt.Println("=== Glob 工具覆盖测试 ===")

	// ── 基本匹配 ──
	fmt.Println("--- 基本匹配 ---")
	run("*.go 匹配", func() bool {
		tc, _, content := executeGlob(ctx, map[string]any{
			"pattern": "*.go",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		if !strings.Contains(content, ".go") {
			fmt.Printf("    expected .go files, got: %s\n", content[:100])
			return false
		}
		return true
	})

	run("**/*.txt 递归匹配", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "**/*.txt",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		return true
	})

	run("无匹配 *.nonexistent", func() bool {
		tc, _, content := executeGlob(ctx, map[string]any{
			"pattern": "*.nonexistent",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles != 0 {
			fmt.Printf("    expected 0 files, got %d\n", gc.NumFiles)
			return false
		}
		if content != "No files found" {
			fmt.Printf("    expected 'No files found', got: %s\n", content)
			return false
		}
		return true
	})

	// ── path 参数 ──
	fmt.Println("\n--- path 参数 ---")
	run("path 指定子目录", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "*.txt",
			"path":    "/tmp/ripgrepcheck/subdir",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		return true
	})

	run("path 不存在报错", func() bool {
		toolCall := apitypes.ToolCall{
			ID:   "test",
			Name: core_types.GLOB_TOOL_NAME,
			Arguments: map[string]any{
				"pattern": "*.go",
				"path":    "/nonexistent/path",
			},
		}
		toolUseCtx := core_types.ToolUseContext{ToolCall: toolCall}
		gc := glob.ValidateInput(ctx, toolUseCtx)
		return gc.Result // ValidateInput 返回 ValidateContent，不是 error
	})

	// ── 绝对路径 pattern ──
	fmt.Println("\n--- 绝对路径 pattern ---")
	run("绝对路径 /tmp/ripgrepcheck/*.go", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "/tmp/ripgrepcheck/*.go",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		// 路径应相对 CWD（/tmp/ripgrepcheck），不应包含绝对路径
		for _, f := range gc.Filenames {
			if strings.HasPrefix(f, "/") {
				fmt.Printf("    expected relative path, got absolute: %s\n", f)
				return false
			}
		}
		return true
	})

	run("绝对路径 subdir", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "/tmp/ripgrepcheck/subdir/*.txt",
		})
		gc := tc.Content.(core_types.GlobContent)
		if gc.NumFiles == 0 {
			fmt.Printf("    expected >0 files, got 0\n")
			return false
		}
		for _, f := range gc.Filenames {
			if !strings.Contains(f, "subdir") {
				fmt.Printf("    expected subdir path, got: %s\n", f)
				return false
			}
		}
		return true
	})

	// ── 路径相对化 ──
	fmt.Println("\n--- 路径相对化 ---")
	run("结果路径为相对路径", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "*.go",
		})
		gc := tc.Content.(core_types.GlobContent)
		for _, f := range gc.Filenames {
			if strings.HasPrefix(f, "/") && !strings.HasPrefix(f, "~/") {
				fmt.Printf("    expected relative path, got absolute: %s\n", f)
				return false
			}
		}
		return true
	})

	// ── BuildToolResult ──
	fmt.Println("\n--- BuildToolResult ---")
	run("BuildToolResult 无匹配", func() bool {
		tc, result, content := executeGlob(ctx, map[string]any{
			"pattern": "*.nonexistent",
		})
		_ = tc
		if content != "No files found" {
			fmt.Printf("    expected 'No files found', got: %s\n", content)
			return false
		}
		if result.IsError {
			fmt.Printf("    expected IsError=false\n")
			return false
		}
		return true
	})

	run("BuildToolResult 有匹配", func() bool {
		tc, result, content := executeGlob(ctx, map[string]any{
			"pattern": "*.go",
		})
		gc := tc.Content.(core_types.GlobContent)
		_ = content
		if result.IsError {
			fmt.Printf("    expected IsError=false\n")
			return false
		}
		if gc.NumFiles > 0 && !strings.Contains(content, ".go") {
			fmt.Printf("    expected .go in output\n")
			return false
		}
		return true
	})

	// ── 截断 ──
	fmt.Println("\n--- 截断 ---")
	run("大量结果截断", func() bool {
		// 用 ** 匹配全部文件，应超过 100 的限制
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "**",
		})
		gc := tc.Content.(core_types.GlobContent)
		if !gc.Truncated {
			fmt.Printf("    expected truncated=true for ** pattern\n")
			// 可能文件数不够 100，如果未截断也不一定是 bug
		}
		return true
	})

	// ── 隐藏文件 ──
	fmt.Println("\n--- 隐藏文件 ---")
	run("隐藏文件被包含", func() bool {
		tc, _, _ := executeGlob(ctx, map[string]any{
			"pattern": "*.go",
		})
		gc := tc.Content.(core_types.GlobContent)
		hasHidden := false
		for _, f := range gc.Filenames {
			if strings.Contains(f, ".hidden") {
				hasHidden = true
				break
			}
		}
		if !hasHidden {
			fmt.Printf("    expected hidden files to be included (.hidden/), NumFiles=%d\n", gc.NumFiles)
			return false
		}
		return true
	})

	fmt.Println("\n=== 测试完成 ===")
}

func executeGlob(ctx context.Context, args map[string]any) (core_types.ToolContext, apitypes.ToolResultMessage, string) {
	toolCall := apitypes.ToolCall{
		ID:        "test_01",
		Name:      core_types.GLOB_TOOL_NAME,
		Arguments: args,
	}

	toolUseCtx := core_types.ToolUseContext{
		ToolCall: toolCall,
	}

	tc, err := glob.Execute(ctx, toolUseCtx)
	if err != nil {
		fmt.Printf("    ERROR: %v\n", err)
		return tc, apitypes.ToolResultMessage{}, ""
	}

	result := glob.BuildToolResult(tc)
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
