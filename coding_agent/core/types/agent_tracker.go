package types

// AgentTracker 跟踪 agent 运行过程中的统计数据。
type AgentTracker struct {
	ToolUseCount int
	Input        int
	Output       int
	CacheRead    int
	CacheWrite   int
	TotalTokens  int

	CostTotal float64

	ContextUsage   int     // 当前上下文已用 token（估算）
	ContextWindow  int     // 模型上下文窗口上限
	ContextPercent float64 // 上下文占用百分比，保留 2 位小数
}
