package plan

import (
	"context"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	prompt2 "github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
)

type PlanManager struct {
	ctx        context.Context
	planState  *core_types.PlanState // 内部持指针：GetPlanState 直接返回共享；存 entry 与恢复走值传递
	runtimeCtx *core_types.AgentRuntimeContext
	persistFn  func(core_types.PlanState) // 状态变更后写盘回调（会话恢复用），可为 nil

	// 恢复依赖（resume 用，独立使用场景可留空）：getLastPlanEntry 返回会话最近一条
	// plan 条目，Init 据此恢复 plan mode；AppStage 复位走 runtimeCtx.AgentState。
	getLastPlanEntry func() (*core_types.PlanEntry, bool)
}

// SetPersistFn 注册 plan 状态变更回调（NewAgent 接线到会话落盘）。
func (pm *PlanManager) SetPersistFn(fn func(core_types.PlanState)) {
	pm.persistFn = fn
}

// WithRestoreSource 注入恢复依赖：会话最近 plan 条目读取回调
// （AppStage 复位经 runtimeCtx.AgentState，无需单独注入）。
func (pm *PlanManager) WithRestoreSource(getLastPlanEntry func() (*core_types.PlanEntry, bool)) *PlanManager {
	pm.getLastPlanEntry = getLastPlanEntry
	return pm
}

// persist 在 EnterPlan/ExitPlan 变更状态后把最新 PlanState 写盘（值拷贝）。
func (pm *PlanManager) persist() {
	if pm.persistFn != nil {
		pm.persistFn(*pm.planState)
	}
}

func NewPlanManager(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *PlanManager {
	return &PlanManager{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
		planState: &core_types.PlanState{
			FileDir:          "",
			Active:           false,
			JustActivated:    false,
			TurnCount:        0, // 用户输入计数，>=5 时放行注入
			AttachmentCount:  0, // 累计注入次数，决定 full / sparse
			NeedPlanModeExit: false,
			FilePath:         "",
		},
	}
}

// Init 恢复上次会话的 plan mode：最近 plan 条目若为 Active（上次在 plan mode 中退出），
// 恢复 PlanState + AppStage，并固定 plan 文件路径（跨进程 slug 一致，避免已写 plan 文件
// 孤儿化）。RestoreFrom 复位瞬态字段：不立刻重注入提示（plan 提示已在恢复的上下文里）。
func (pm *PlanManager) Init() {
	pm.RestoreFrom()
}

// GetPlanState 返回内部状态指针：AgentUseContext.PlanState 借此共享同一份状态，
// 供 ExitPlanMode 等工具实时读取（handler 变更后无需重新下发）。
func (pm *PlanManager) GetPlanState() *core_types.PlanState {
	return pm.planState
}

func (pm *PlanManager) AgentStart() {
	if pm.planState.Active {
		pm.planState.TurnCount++
	}
}

// setAppStage 是 AppStage 的唯一写入入口：仅在实际变化时写入并触发通知
// （EnterPlan/ExitPlan 均经此切换，通知与状态变更同源，避免外部轮询/对比推断）。
// setAppStage 是 PlanManager 内 AppStage 的写入入口：委托给 AgentState.SetAppStage，
// 变化检测与通知统一在 AgentState 内完成（字段已私有化，无法直接赋值）。
func (pm *PlanManager) setAppStage(stage core_types.AppStage) {
	if pm.runtimeCtx.AgentState != nil {
		pm.runtimeCtx.AgentState.SetAppStage(stage)
	}
}

func (pm *PlanManager) EnterPlan() {
	if pm.planState.Active {
		return
	}
	pm.planState.FileDir = utils.GetPlanDir()
	pm.planState.Active = true
	pm.planState.Slug = utils.GetPlanSlug(pm.runtimeCtx.SessionId)
	pm.planState.FilePath = utils.GetPlanFilePath(pm.runtimeCtx.SessionId)
	pm.planState.JustActivated = true
	pm.planState.TurnCount = 0
	pm.planState.AttachmentCount = 0
	pm.planState.NeedPlanModeExit = false
	pm.setAppStage(core_types.Plan)
	pm.persist()
}

func (pm *PlanManager) ExitPlan() {
	if !pm.planState.Active {
		return
	}
	pm.planState.Active = false
	pm.planState.NeedPlanModeExit = true
	pm.planState.TurnCount = 0
	pm.planState.AttachmentCount = 0
	pm.setAppStage(core_types.Default)
	pm.persist()
}

// RestoreFrom 用会话中持久化的 PlanState 恢复 plan mode（resume），直接赋值，
// JustActivated 按存的值原样保留：EnterPlan 后若在首次注入前中断，persist 已把
// JustActivated=true 落盘，恢复后保持 true 才能让下一轮补注 full plan 提示；
// 首次注入后 EnterPlanAttachment 会 persist() 一次（JustActivated=false），
// 故已注入的会话恢复后不会重复注入。
func (pm *PlanManager) RestoreFrom() {
	if pm.getLastPlanEntry == nil {
		return
	}
	pe, ok := pm.getLastPlanEntry()
	if !ok || !pe.PlanState.Active {
		return
	}
	*pm.planState = pe.PlanState
	// 恢复也走 setAppStage 统一写入入口；此时 onStageChanged 尚未接线（Init 早于首轮运行），
	// 即使触发回调也是空操作，footer 在启动时按恢复后的状态重建。
	pm.setAppStage(core_types.Plan)
	utils.SeedPlanFileCache(pm.runtimeCtx.SessionId, pe.PlanState.Slug)
}

func (pm *PlanManager) EnterPlanAttachment(sessionId string) []*core_types.AttachmentMessage {
	var result []*core_types.AttachmentMessage
	if !pm.planState.Active {
		return result
	}
	planFilePath := utils.GetPlanFilePath(sessionId)

	// 首次激活：直接放行，注入 full。注入后立即 persist，把 JustActivated=false 落盘，
	// 防止中断后恢复把已注入的 full 提示再注一遍（配合 RestoreFrom 原样保留）。
	if pm.planState.JustActivated {
		pm.planState.JustActivated = false
		pm.planState.AttachmentCount = 1
		pm.persist()
		result = append(result, &core_types.AttachmentMessage{
			Type: "plan_mode",
			Data: map[string]any{
				"remindType":   "full",
				"planFilePath": planFilePath,
			},
		})
		return result
	}

	// 闸门：满 5 次用户输入才放行
	if pm.planState.TurnCount < 5 {
		return nil
	}

	pm.planState.TurnCount = 0
	pm.planState.AttachmentCount++

	remindType := "sparse"
	if pm.planState.AttachmentCount%5 == 1 {
		remindType = "full"
	}
	result = append(result, &core_types.AttachmentMessage{
		Type: core_types.PLAN_MODE,
		Data: map[string]any{
			"remindType":   remindType,
			"planFilePath": planFilePath,
		},
	})
	return result
}

func (pm *PlanManager) EnterPlanMetaMessage(attachment *core_types.AttachmentMessage) *apitypes.UserMessage {
	remindType, _ := attachment.Data["remindType"].(string)
	planFilePath, _ := attachment.Data["planFilePath"].(string)
	exists, _ := utils.PathExists(planFilePath)
	prompt := ""
	if remindType == "sparse" {
		prompt = prompt2.GetPlanModeSparsePrompt(planFilePath)
	} else {
		prompt = prompt2.GetPlanModePrompt(planFilePath, exists)
	}
	//
	message := apitypes.NewUserMetaMessage(prompt)
	return &message
}

func (pm *PlanManager) ExitPlanAttachment(sessionId string) []*core_types.AttachmentMessage {
	var result []*core_types.AttachmentMessage
	if !pm.planState.NeedPlanModeExit {
		return result
	}
	pm.planState.NeedPlanModeExit = false
	filePath := utils.GetPlanFilePath(sessionId)
	result = append(result, &core_types.AttachmentMessage{
		Type: core_types.PLAN_MODE_EXIT,
		Data: map[string]any{
			"planFilePath": filePath,
		},
	})
	return result
}

func (pm *PlanManager) ExitPlanMetaMessage(attachment *core_types.AttachmentMessage) *apitypes.UserMessage {
	planFilePath, _ := attachment.Data["planFilePath"].(string)
	exists, _ := utils.PathExists(planFilePath)

	prompt := prompt2.GetPlanModeExitPrompt(planFilePath, exists)
	message := apitypes.NewUserMetaMessage(prompt)
	return &message
}
