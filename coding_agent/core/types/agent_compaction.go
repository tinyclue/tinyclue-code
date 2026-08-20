package types

import "errors"

// ErrNoContextToCompact 表示压缩区间为空、没有内容可压。调用方（autoCompact、
// tryRecoverCompact）应将其视为"无需压缩"，而非致命错误：总量接近窗口但
// 无可压新内容时，应交由溢出恢复（Recover 合并 prevSummary）或按真实 API
// 错误处理，而不是中止整个 run。
var ErrNoContextToCompact = errors.New("No context need to compact.")

// CompactMode 控制压缩切分策略。
type CompactMode int

const (
	CompactModeDefault CompactMode = iota // 保留最近 KEEP_RECENT_TOKENS token 的消息（现状）
	CompactModeRecover                    // 恢复性：不预留，切到最后一条合法切分点
)

// FileOperations tracks file paths referenced by tool calls during compaction.
type FileOperations struct {
	Read    []string
	Written []string
	Edited  []string
}

// FileLists groups computed read-only and modified file paths.
type FileLists struct {
	ReadFiles     []string
	ModifiedFiles []string
}

// PrepareCompactionContext holds the prepared state for a compaction operation.
type PrepareCompactionContext struct {
	PrevTokens          int
	PrevCompaction      *CompactionEntry
	FirstKeptEntryId    string
	Summarize           []SessionEntry
	TurnPrefixSummarize []SessionEntry
	FileOps             *FileOperations
	DiscoveredTools     []string
	Mode                CompactMode
	SessionFile         string
}

type CompactionContext struct {
	Prepare          *PrepareCompactionContext
	Summary          string
	FirstKeptEntryId string
	Tokens           int
	FileLists        FileLists
	DiscoveredTools  []string
	Mode             CompactMode
}
