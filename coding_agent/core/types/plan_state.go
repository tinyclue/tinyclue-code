package types

// PlanState 是 plan mode 的相关状态，通过共享指针在各组件间传递。
type PlanState struct {
	FileDir          string
	Active           bool
	JustActivated    bool
	TurnCount        int // 用户输入计数，>=5 时放行注入
	AttachmentCount  int // 累计注入次数，决定 full / sparse
	NeedPlanModeExit bool
	FilePath         string
	Slug             string // plan 文件 slug（跨进程稳定路径：恢复时用 SeedPlanFileCache 固定）
}
