// Package tools 提供工具函数的截断算法。
package utils

import (
	"fmt"
	"strings"
)

// TruncationKind 表示截断产生的原因。
type TruncationKind int

const (
	TruncationNone             TruncationKind = iota // 未截断
	TruncationFirstLineExceeds                       // 首行超字节限制（head），返回开头部分
	TruncationLastLinePartial                        // 末行超字节限制（tail），返回末尾部分
	TruncationLines                                  // 行数超限
	TruncationBytes                                  // 字节超限
)

// TruncationResult 保存截断操作的完整元信息。
type TruncationResult struct {
	Kind        TruncationKind
	Content     string // 截断后的内容
	OutputLines int    // 截断后实际行数
	OutputBytes int    // 截断后实际字节数
	TotalLines  int    // 原始总行数
	TotalBytes  int    // 原始总字节数
	MaxLines    int    // 应用的行限制
	MaxBytes    int    // 应用的字节限制
}

// splitLinesForCounting 将内容按行分割，尾部换行符不产生空行。
func splitLinesForCounting(content string) []string {
	if len(content) == 0 {
		return nil
	}
	lines := strings.Split(content, "\n")
	if content[len(content)-1] == '\n' {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// formatSize 将字节数转为可读字符串（50B, 12.3KB, 1.5MB）。
func FormatSize(bytes int) string {
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	}
}

// truncateHead 保留内容的前 N 行（在 maxBytes 限制内）。
// 适用于文件读取，可以看到开头。
func TruncateHead(content string, maxLines, maxBytes int) TruncationResult {
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	// 无需截断
	if totalBytes <= maxBytes && totalLines <= maxLines {
		return TruncationResult{
			Kind:        TruncationNone,
			Content:     content,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	// 首行单独超字节限制：返回开头部分
	if totalLines > 0 && len(lines[0]) > maxBytes {
		truncated := truncateStringToBytesFromHead(lines[0], maxBytes)
		return TruncationResult{
			Kind:        TruncationFirstLineExceeds,
			Content:     truncated,
			OutputLines: 1,
			OutputBytes: len(truncated),
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	// 收集能完整放下的行
	var outputLines, outputBytes int
	kind := TruncationLines
	for i := 0; i < len(lines) && i < maxLines; i++ {
		lineBytes := len(lines[i])
		if i > 0 {
			lineBytes++ // \n
		}
		if outputBytes+lineBytes > maxBytes {
			kind = TruncationBytes
			break
		}
		outputLines++
		outputBytes += lineBytes
	}

	return TruncationResult{
		Kind:        kind,
		Content:     strings.Join(lines[:outputLines], "\n"),
		OutputLines: outputLines,
		OutputBytes: outputBytes,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

// truncateTail 保留内容的最后 N 行（在 maxBytes 限制内）。
// 适用于 bash 输出，可以看到结尾。
func TruncateTail(content string, maxLines, maxBytes int) TruncationResult {
	totalBytes := len(content)
	lines := splitLinesForCounting(content)
	totalLines := len(lines)

	// 无需截断
	if totalBytes <= maxBytes && totalLines <= maxLines {
		return TruncationResult{
			Kind:        TruncationNone,
			Content:     content,
			OutputLines: totalLines,
			OutputBytes: totalBytes,
			TotalLines:  totalLines,
			TotalBytes:  totalBytes,
			MaxLines:    maxLines,
			MaxBytes:    maxBytes,
		}
	}

	var outputLines, outputBytes int
	kind := TruncationLines

	for i := len(lines) - 1; i >= 0 && outputLines < maxLines; i-- {
		line := lines[i]
		lineBytes := len(line)
		if outputLines > 0 {
			lineBytes++ // \n
		}

		if outputBytes+lineBytes > maxBytes {
			if outputLines == 0 {
				truncated := truncateStringToBytesFromEnd(line, maxBytes)
				return TruncationResult{
					Kind:        TruncationLastLinePartial,
					Content:     truncated,
					OutputLines: 1,
					OutputBytes: len(truncated),
					TotalLines:  totalLines,
					TotalBytes:  totalBytes,
					MaxLines:    maxLines,
					MaxBytes:    maxBytes,
				}
			}
			kind = TruncationBytes
			break
		}

		outputLines++
		outputBytes += lineBytes
	}

	return TruncationResult{
		Kind:        kind,
		Content:     strings.Join(lines[len(lines)-outputLines:], "\n"),
		OutputLines: outputLines,
		OutputBytes: outputBytes,
		TotalLines:  totalLines,
		TotalBytes:  totalBytes,
		MaxLines:    maxLines,
		MaxBytes:    maxBytes,
	}
}

// truncateStringToBytesFromEnd 从字符串末尾截取不超过 maxBytes 字节的内容。
func truncateStringToBytesFromEnd(str string, maxBytes int) string {
	b := []byte(str)
	if len(b) <= maxBytes {
		return str
	}
	start := len(b) - maxBytes
	for start < len(b) && (b[start]&0xc0) == 0x80 {
		start++
	}
	return string(b[start:])
}

// truncateStringToBytesFromHead 从字符串开头截取不超过 maxBytes 字节的内容。
func truncateStringToBytesFromHead(str string, maxBytes int) string {
	b := []byte(str)
	if len(b) <= maxBytes {
		return str
	}
	end := maxBytes
	for end < len(b) && (b[end]&0xc0) == 0x80 {
		end++
	}
	return string(b[:end])
}
