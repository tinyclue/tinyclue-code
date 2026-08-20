package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

// PersistedPreviewBytes 是 persisted-output 替换文本里 Preview 保留的字节数。
const PersistedPreviewBytes = 2000

// OffloadSizeFunc 度量一段文本占用的"大小"，token 数或字节数。
// 各调用方自选口径：压缩总结器用 token（EstimateTextTokens），
// loop 开场检查用字节（func(s string) int { return len(s) }）。
type OffloadSizeFunc func(text string) int

// blockCandidate 描述一个待外置的 tool_result 文本块。
type blockCandidate struct {
	msgIndex  int    // messages 中的下标
	blockIdx  int    // Contents 中的下标
	toolUseID string // ToolCallId，用作临时文件名
	text      string // 原始全文
	size      int    // 按 measure 度量的大小
}

// OffloadResult 是一次外置处理的结果。
type OffloadResult struct {
	Files    []string // 外置文件完整路径，按写入顺序
	Total    int      // 处理后的总大小（与传入 initialSize 同一口径）
	Overflow bool     // 全部外置后仍超 maxSize
}

// OffloadToolResults 把 messages 中过大的 tool_result 文本块依次外置到
// ~/.tinyclue/tool-results/<tool_use_id>.txt，原位替换为 persisted-output 指针，
// 直到总大小 ≤ maxSize。
//
// initialSize 是 messages 的初始总大小（按调用方的口径，如全部消息的 token
// 估算）；measure 度量单个文本块，用于排序与替换后的大小增量。
// 从后往前按"一次 assistant tool_call 回合"分组，组内按 size 降序，先外置
// 最大的；每外置一块重查是否已 ≤ maxSize。全部外置完仍超时 Overflow=true，
// 由调用方决定成败。
//
// 注意：会原地改写传入 messages 中 ToolResultMessage.Contents 的文本。调用方
// 若不希望污染共享的会话条目（如压缩总结器），需先深拷贝 Contents。
func OffloadToolResults(messages []apitypes.Message, initialSize int, measure OffloadSizeFunc, maxSize int) (*OffloadResult, error) {
	result := &OffloadResult{Files: make([]string, 0), Total: initialSize}
	if maxSize <= 0 || initialSize <= maxSize {
		return result, nil
	}

	// 收集候选：连续相邻的 ToolResultMessage 归为一组（一次 assistant tool_call 回合）。
	var groups [][]blockCandidate
	i := 0
	for i < len(messages) {
		if _, ok := messages[i].(apitypes.ToolResultMessage); !ok {
			i++
			continue
		}
		var blocks []blockCandidate
		for i < len(messages) {
			tr, ok := messages[i].(apitypes.ToolResultMessage)
			if !ok {
				break
			}
			for j, c := range tr.Contents {
				if c.Type != "text" || c.Text == "" {
					continue
				}
				blocks = append(blocks, blockCandidate{
					msgIndex:  i,
					blockIdx:  j,
					toolUseID: tr.ToolCallId,
					text:      c.Text,
					size:      measure(c.Text),
				})
			}
			i++
		}
		if len(blocks) > 0 {
			// 组内按 size 降序，先外置最大的。
			sort.Slice(blocks, func(a, b int) bool { return blocks[a].size > blocks[b].size })
			groups = append(groups, blocks)
		}
	}

	// 组从新到旧（后往前），组内从大到小；每外置一块后重查大小。
	for g := len(groups) - 1; g >= 0; g-- {
		for _, cand := range groups[g] {
			if result.Total <= maxSize {
				return result, nil
			}
			path, err := PersistToolResult(cand.toolUseID, cand.blockIdx, cand.text)
			if err != nil {
				return nil, err
			}
			repl := BuildPersistedOutputText(cand.text, path)
			tr := messages[cand.msgIndex].(apitypes.ToolResultMessage)
			tr.Contents[cand.blockIdx].Text = repl
			messages[cand.msgIndex] = tr
			result.Files = append(result.Files, path)
			result.Total += measure(repl) - cand.size
		}
	}
	if result.Total > maxSize {
		result.Overflow = true
	}
	return result, nil
}

// PersistToolResult 把 tool_result 文本块全文写入 ~/.tinyclue/tool-results/ 下
// 以 <tool_use_id>.txt 命名的文件，返回完整路径。多 text 块时附加块序号防撞。
func PersistToolResult(toolUseID string, blockIdx int, text string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(homeDir, ".tinyclue", "tool-results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	name := toolUseID + ".txt"
	if blockIdx > 0 {
		name = fmt.Sprintf("%s_%d.txt", toolUseID, blockIdx+1)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(text), 0644); err != nil {
		return "", err
	}
	return path, nil
}

// BuildPersistedOutputText 生成 tool_result 文本块被外置后的占位文本：
//
//	<persisted-output>
//	  Output too large (X). Full output saved to: <path>
//
//	  Preview (first 2.0 KB):
//	  <原文前 2KB（按 rune 边界截断，保证合法 UTF-8）>
//	  ...
//	  </persisted-output>
func BuildPersistedOutputText(original string, path string) string {
	preview := original
	if len(original) > PersistedPreviewBytes {
		end := PersistedPreviewBytes
		for end > 0 && !utf8.RuneStart(original[end]) {
			end--
		}
		preview = original[:end]
	}
	return fmt.Sprintf("<persisted-output>\n"+
		"  Output too large (%s). Full output saved to: %s\n"+
		"\n"+
		"  Preview (first %s):\n"+
		"  %s\n"+
		"  ...\n"+
		"  </persisted-output>",
		FormatBytes(len(original)), path, FormatBytes(PersistedPreviewBytes), preview)
}

// FormatBytes 把字节数格式化为人类可读体积。
func FormatBytes(n int) string {
	switch {
	case n >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	case n >= 1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%d bytes", n)
	}
}

// EstimateMessagesTokens 估算消息列表的总 token 数。
func EstimateMessagesTokens(messages []apitypes.Message) int {
	total := 0
	for _, m := range messages {
		total += EstimateTokens(m)
	}
	return total
}
