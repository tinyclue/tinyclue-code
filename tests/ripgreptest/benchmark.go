//go:build ignore
// +build ignore

package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/ripgrep"
)

var sizes = []struct {
	name  string
	files int
	lines int
}{
	{"小规模", 50, 30},
	{"中规模", 200, 100},
	{"大规模", 1000, 50},
}

func main() {
	rgPath := os.Getenv("HOME") + "/bin/rg"

	fmt.Println("══════════════════════════════════════════")
	fmt.Println("性能基准测试 (Go RipGrep Engine vs ripgrep)")
	fmt.Println("══════════════════════════════════════════")

	for _, s := range sizes {
		dir, err := generateTestData(s.files, s.lines)
		if err != nil {
			fmt.Printf("生成测试数据失败: %v\n", err)
			continue
		}

		fmt.Printf("\n── %s: %d 文件, 每文件 ~%d 行 ──\n", s.name, s.files, s.lines)

		// ── 测试项目 ──
		type benchCase struct {
			name string
			args []string
		}

		cases := []benchCase{
			{"--files 列出全部", []string{"--files"}},
			{"搜索 'hello' (全文)", []string{"hello"}},
			{"搜索 -i 'HELLO' (忽略大小写)", []string{"-i", "HELLO"}},
			{"-l 仅文件名", []string{"-l", "hello"}},
			{"-c 统计", []string{"-c", "hello"}},
			{"-C 2 上下文", []string{"-C", "2", "hello"}},
			{"--glob *.go 过滤", []string{"--files", "--glob", "*.go"}},
			{"--type go 过滤", []string{"--files", "--type", "go"}},
		}

		// 预热
		for _, c := range cases {
			ripgrep.RipGrep(context.Background(), c.args, dir)
			exec.Command(rgPath, append(c.args, "-n", "--no-ignore", dir)...).CombinedOutput()
		}

		iterations := 5
		fmt.Printf("\n(每次测试运行 %d 次取平均)\n", iterations)
		fmt.Printf("%-30s %-20s %-20s %s\n", "测试项", "Go 引擎", "rg", "加速比")
		fmt.Println(strings.Repeat("─", 90))

		for _, c := range cases {
			goDur := measureGo(c.args, dir, iterations)
			rgDur := measureRg(rgPath, c.args, dir, iterations)

			ratio := float64(rgDur) / float64(goDur)
			arrow := ""
			if ratio > 1 {
				arrow = fmt.Sprintf("Go %.2fx 快", ratio)
			} else {
				arrow = fmt.Sprintf("rg %.2fx 快", 1/ratio)
			}
			fmt.Printf("%-30s %-20s %-20s %s\n", c.name, goDur.Round(time.Microsecond), rgDur.Round(time.Microsecond), arrow)
		}
	}
}

// generateTestData 在临时目录生成测试文件。
func generateTestData(numFiles, linesPerFile int) (string, error) {
	dir, err := os.MkdirTemp("", "ripgrepbench")
	if err != nil {
		return "", err
	}

	// 分散到多个子目录
	dirs := []string{""}
	for i := 0; i < 3; i++ {
		dirs = append(dirs, fmt.Sprintf("dir%d", i))
		for j := 0; j < 2; j++ {
			dirs = append(dirs, fmt.Sprintf("dir%d/sub%d", i, j))
		}
	}

	for _, d := range dirs {
		os.MkdirAll(filepath.Join(dir, d), 0755)
	}

	exts := []string{".go", ".txt", ".js", ".json", ".yaml", ".md", ".css", ".html"}

	for i := 0; i < numFiles; i++ {
		subDir := dirs[i%len(dirs)]
		ext := exts[i%len(exts)]
		filename := fmt.Sprintf("file_%d%s", i, ext)
		path := filepath.Join(dir, subDir, filename)

		f, err := os.Create(path)
		if err != nil {
			os.RemoveAll(dir)
			return "", err
		}

		// 写入内容，部分包含 "hello" 关键词
		for j := 0; j < linesPerFile; j++ {
			if rand.Intn(5) == 0 {
				fmt.Fprintf(f, "line %d: hello world from file %d\n", j, i)
			} else {
				fmt.Fprintf(f, "line %d: some random content %d for testing %x\n", j, i, rand.Intn(1000))
			}
		}
		f.Close()
	}

	return dir, nil
}

func measureGo(args []string, dir string, n int) time.Duration {
	var total time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()
		r, err := ripgrep.RipGrep(context.Background(), args, dir)
		_ = r
		if err != nil {
			return total / time.Duration(i+1)
		}
		total += time.Since(start)
	}
	return total / time.Duration(n)
}

func measureRg(rgPath string, args []string, dir string, n int) time.Duration {
	fullArgs := make([]string, 0, len(args)+4)
	fullArgs = append(fullArgs, args...)

	needN := true
	hasNoIgnore := false
	for _, a := range args {
		if a == "--files" {
			needN = false
		}
		if a == "--no-ignore" {
			hasNoIgnore = true
		}
	}
	if needN {
		fullArgs = append(fullArgs, "-n")
	}
	if !hasNoIgnore {
		fullArgs = append(fullArgs, "--no-ignore")
	}
	fullArgs = append(fullArgs, dir)

	var total time.Duration
	for i := 0; i < n; i++ {
		start := time.Now()
		out, err := exec.Command(rgPath, fullArgs...).CombinedOutput()
		_ = out
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() != 1 {
					return total / time.Duration(i+1)
				}
			}
		}
		total += time.Since(start)
	}
	return total / time.Duration(n)
}
