package utils

import (
	"fmt"
	"github.com/aymanbagabas/go-udiff"
	"path/filepath"
	"strings"
)

// computeDiff 使用 go-udiff 计算原始与变更文件之间的差异。
// 返回两个值：numbered（带行号，用于界面渲染），unified（标准 unified diff，存档备用）。
// numbered 格式：首行统计，后跟 ```diff <lang> 代码块，每行格式为 "行号 标记符(+/-/空格) 内容"
func ComputeDiff(original, result, path string) (numbered, unified string) {
	if original == result {
		return "", ""
	}

	edits := udiff.Strings(original, result)
	if len(edits) == 0 {
		return "", ""
	}

	d, err := udiff.ToUnifiedDiff("", "", original, edits, 5)
	if err != nil {
		return "", ""
	}

	// 统计增删行数
	var added, removed int
	for _, hunk := range d.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Insert:
				added++
			case udiff.Delete:
				removed++
			}
		}
	}

	// 计算最大行号确定数字宽度
	maxNum := 0
	for _, hunk := range d.Hunks {
		fromEnd := hunk.FromLine
		toEnd := hunk.ToLine
		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				fromEnd++
			case udiff.Insert:
				toEnd++
			case udiff.Equal:
				fromEnd++
				toEnd++
			}
		}
		if fromEnd > 1 && fromEnd-1 > maxNum {
			maxNum = fromEnd - 1
		}
		if toEnd > 1 && toEnd-1 > maxNum {
			maxNum = toEnd - 1
		}
	}
	width := 1
	for n := maxNum; n >= 10; n /= 10 {
		width++
	}

	// 从文件扩展名推断语言
	fence := "```diff\n"
	if ext := filepath.Ext(path); ext != "" {
		fence = "```diff " + ext[1:] + "\n"
	}

	// === 构建 numbered diff（带行号） ===
	var nb strings.Builder
	nb.WriteString(fence)

	for _, hunk := range d.Hunks {
		fromNum := hunk.FromLine
		toNum := hunk.ToLine

		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				fmt.Fprintf(&nb, "%*d -%s", width, fromNum, l.Content)
				fromNum++
			case udiff.Insert:
				fmt.Fprintf(&nb, "%*d +%s", width, toNum, l.Content)
				toNum++
			case udiff.Equal:
				fmt.Fprintf(&nb, "%*d  %s", width, toNum, l.Content)
				fromNum++
				toNum++
			}
		}
	}
	nb.WriteString("```")

	// === 构建 unified diff（标准格式） ===
	var ub strings.Builder
	ub.WriteString(fence)

	for _, hunk := range d.Hunks {
		fromCount, toCount := 0, 0
		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				fromCount++
			case udiff.Insert:
				toCount++
			case udiff.Equal:
				fromCount++
				toCount++
			}
		}
		fmt.Fprintf(&ub, "@@ -%d", hunk.FromLine)
		if fromCount > 1 {
			fmt.Fprintf(&ub, ",%d", fromCount)
		} else if hunk.FromLine == 1 && fromCount == 0 {
			fmt.Fprintf(&ub, ",0")
		}
		fmt.Fprintf(&ub, " +%d", hunk.ToLine)
		if toCount > 1 {
			fmt.Fprintf(&ub, ",%d", toCount)
		} else if hunk.ToLine == 1 && toCount == 0 {
			fmt.Fprintf(&ub, ",0")
		}
		ub.WriteString(" @@\n")

		for _, l := range hunk.Lines {
			switch l.Kind {
			case udiff.Delete:
				ub.WriteString("-" + l.Content)
			case udiff.Insert:
				ub.WriteString("+" + l.Content)
			case udiff.Equal:
				ub.WriteString(" " + l.Content)
			}
		}
	}
	ub.WriteString("```")

	return nb.String(), ub.String()
}

// lineChanges 统计行数增减。
type LineCounts struct {
	Additions, Deletions int
}

func CountLineChanges(orig, result []string) LineCounts {
	edits := udiff.Strings(strings.Join(orig, "\n"), strings.Join(result, "\n"))
	if len(edits) == 0 {
		return LineCounts{}
	}
	d, err := udiff.ToUnifiedDiff("", "", strings.Join(orig, "\n"), edits, 5)
	if err != nil {
		return LineCounts{}
	}

	var c LineCounts
	for _, hunk := range d.Hunks {
		for _, line := range hunk.Lines {
			switch line.Kind {
			case udiff.Insert:
				c.Additions++
			case udiff.Delete:
				c.Deletions++
			}
		}
	}
	return c
}
