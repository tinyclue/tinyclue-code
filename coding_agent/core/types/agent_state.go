package types

// AppStage 表示 agent 的运行阶段。
type AppStage string

const (
	Default AppStage = "default"
	Plan    AppStage = "plan"
)

// AgentState 是 agent 的运行时状态，通过共享指针在各组件间传递。
// 内部字段不直接暴露：读写一律经方法，SetAppStage 内统一做变化检测并触发
// onStageChanged 回调，避免各组件无差别地直接操作内部值。
type AgentState struct {
	appStage       AppStage
	onStageChanged func(AppStage) // 阶段变化回调（footer 刷新用），可为 nil
}

// NewAgentState 创建默认阶段（Default）的 AgentState。
func NewAgentState() *AgentState {
	return &AgentState{appStage: Default}
}

// AppStage 返回当前阶段。
func (s *AgentState) AppStage() AppStage {
	return s.appStage
}

// IsPlanMode 报告当前是否处于计划模式。
func (s *AgentState) IsPlanMode() bool {
	return s.appStage == Plan
}

// SetAppStage 设置阶段：仅在实际变化时写入并触发 onStageChanged 回调。
func (s *AgentState) SetAppStage(stage AppStage) {
	if s.appStage == stage {
		return
	}
	s.appStage = stage
	if s.onStageChanged != nil {
		s.onStageChanged(stage)
	}
}

// SetOnStageChanged 注册阶段变化回调（footer 刷新用，NewAgent 接线到当轮 CurrentContext）。
func (s *AgentState) SetOnStageChanged(fn func(AppStage)) {
	s.onStageChanged = fn
}
