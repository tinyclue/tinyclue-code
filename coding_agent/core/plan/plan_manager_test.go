package plan

import (
	"context"
	"testing"

	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func newTestPlanManager(t *testing.T) *PlanManager {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	runtimeCtx := &core_types.AgentRuntimeContext{
		AgentId:    "a1",
		SessionId:  "s1",
		AgentState: core_types.NewAgentState(),
	}
	return NewPlanManager(context.Background(), runtimeCtx)
}

// TestRestoreFrom 验证 RestoreFrom 从注入的会话最近 plan 条目恢复：按存的值原样保留
// （含 JustActivated——EnterPlan 后未注入即中断的场景，persist 已把 true 落盘，
// 恢复后需保留以补注提示），并复位 AppStage 为 Plan。
func TestRestoreFrom(t *testing.T) {
	pm := newTestPlanManager(t)
	pm.WithRestoreSource(func() (*core_types.PlanEntry, bool) {
		return &core_types.PlanEntry{PlanState: core_types.PlanState{
			FileDir:          "/tmp/plans",
			Active:           true,
			JustActivated:    true,
			TurnCount:        3,
			AttachmentCount:  2,
			NeedPlanModeExit: true,
			FilePath:         "/tmp/plans/a.md",
			Slug:             "restored-slug",
		}}, true
	})

	pm.RestoreFrom()

	ps := pm.GetPlanState()
	if !ps.Active {
		t.Error("expected Active=true after restore")
	}
	if ps.FileDir != "/tmp/plans" || ps.FilePath != "/tmp/plans/a.md" || ps.Slug != "restored-slug" {
		t.Errorf("persisted fields lost: %+v", ps)
	}
	if !ps.JustActivated {
		t.Error("expected JustActivated preserved as true after restore")
	}
	if ps.TurnCount != 3 || ps.AttachmentCount != 2 || !ps.NeedPlanModeExit {
		t.Errorf("fields should restore as saved: %+v", ps)
	}
	if pm.runtimeCtx.AgentState.AppStage() != core_types.Plan {
		t.Errorf("expected AppStage=Plan after restore, got %q", pm.runtimeCtx.AgentState.AppStage())
	}
}

// TestInitRestoresPlanMode 验证 Init 从会话最近 plan 条目恢复 plan mode：
// Active 时经 RestoreFrom 恢复 PlanState + AppStage（JustActivated 按存的值保留，
// 该条目未注入过 → false，首轮不重复注入）。
func TestInitRestoresPlanMode(t *testing.T) {
	pm := newTestPlanManager(t)
	state := pm.runtimeCtx.AgentState
	looked := false
	pm.WithRestoreSource(func() (*core_types.PlanEntry, bool) {
		looked = true
		return &core_types.PlanEntry{PlanState: core_types.PlanState{
			Active: true, Slug: "restored-slug", FilePath: "/tmp/plans/a.md",
		}}, true
	})

	pm.Init()

	if !looked {
		t.Fatal("expected last plan entry lookup invoked on Init")
	}
	if state.AppStage() != core_types.Plan {
		t.Errorf("expected AppStage=Plan, got %q", state.AppStage())
	}
	ps := pm.GetPlanState()
	if !ps.Active || ps.Slug != "restored-slug" || ps.FilePath != "/tmp/plans/a.md" {
		t.Errorf("expected plan restored as saved: %+v", ps)
	}
	if ps.JustActivated {
		t.Error("expected JustActivated=false after restore")
	}
}

// TestInitSkipsInactivePlan 验证非 Active 或无可恢复条目时 Init 不触发恢复。
func TestInitSkipsInactivePlan(t *testing.T) {
	pm := newTestPlanManager(t)
	pm.WithRestoreSource(func() (*core_types.PlanEntry, bool) {
		return &core_types.PlanEntry{PlanState: core_types.PlanState{Active: false}}, true
	})
	pm.Init()
	if pm.GetPlanState().Active {
		t.Error("expected plan inactive when last entry not active")
	}
}

// TestInitSkipsWithoutSource 验证未注入恢复依赖时 Init 为空操作（独立使用场景）。
func TestInitSkipsWithoutSource(t *testing.T) {
	pm := newTestPlanManager(t)
	pm.Init() // 不 panic
	if pm.GetPlanState().Active {
		t.Error("expected plan inactive without restore source")
	}
}

// TestEnterPlanPersists 验证 EnterPlan 写盘回调触发且状态生效。
func TestEnterPlanPersists(t *testing.T) {
	pm := newTestPlanManager(t)
	var persisted core_types.PlanState
	called := false
	pm.SetPersistFn(func(ps core_types.PlanState) { persisted = ps; called = true })

	pm.EnterPlan()

	if !called {
		t.Fatal("expected persist callback invoked on EnterPlan")
	}
	if !persisted.Active {
		t.Error("expected persisted plan state active")
	}
	if !pm.GetPlanState().Active {
		t.Error("expected plan active after EnterPlan")
	}
	if pm.runtimeCtx.AgentState.AppStage() != core_types.Plan {
		t.Errorf("expected AppStage=Plan, got %q", pm.runtimeCtx.AgentState.AppStage())
	}
	if pm.GetPlanState().Slug == "" {
		t.Error("expected plan slug assigned on EnterPlan")
	}
}

// TestExitPlanPersists 验证 ExitPlan 写盘回调触发且状态复位。
func TestExitPlanPersists(t *testing.T) {
	pm := newTestPlanManager(t)
	var persisted core_types.PlanState
	called := false
	pm.SetPersistFn(func(ps core_types.PlanState) { persisted = ps; called = true })

	pm.EnterPlan()
	called = false
	pm.ExitPlan()

	if !called {
		t.Fatal("expected persist callback invoked on ExitPlan")
	}
	if persisted.Active {
		t.Error("expected persisted plan state inactive after ExitPlan")
	}
	if pm.GetPlanState().Active {
		t.Error("expected plan inactive after ExitPlan")
	}
	if pm.runtimeCtx.AgentState.AppStage() != core_types.Default {
		t.Errorf("expected AppStage=Default, got %q", pm.runtimeCtx.AgentState.AppStage())
	}
}

// TestInitRestoresPendingInject 验证恢复时 JustActivated 原样保留：
// EnterPlan 后未注入即中断，persist 落盘 JustActivated=true，恢复后应保留，
// 让下一轮补注 full plan 提示（而非被强制清零后永不注入）。
func TestInitRestoresPendingInject(t *testing.T) {
	pm := newTestPlanManager(t)
	pm.WithRestoreSource(func() (*core_types.PlanEntry, bool) {
		return &core_types.PlanEntry{PlanState: core_types.PlanState{
			Active: true, Slug: "restored-slug", FilePath: "/tmp/plans/a.md",
			JustActivated: true,
		}}, true
	})

	pm.Init()

	ps := pm.GetPlanState()
	if !ps.Active {
		t.Fatal("expected plan active after restore")
	}
	if !ps.JustActivated {
		t.Error("expected JustActivated preserved as true (pending first inject)")
	}
}

// TestEnterPlanAttachmentPersistsFirstInject 验证首次注入后 persist 把
// JustActivated=false 落盘：已注入的会话中断后恢复，不会把 full 提示再注一遍。
func TestEnterPlanAttachmentPersistsFirstInject(t *testing.T) {
	pm := newTestPlanManager(t)
	var persisted core_types.PlanState
	called := false
	pm.SetPersistFn(func(ps core_types.PlanState) { persisted = ps; called = true })

	pm.EnterPlan()

	attachs := pm.EnterPlanAttachment("s1")
	if len(attachs) != 1 {
		t.Fatalf("expected 1 attachment on first inject, got %d", len(attachs))
	}
	if remindType, _ := attachs[0].Data["remindType"].(string); remindType != "full" {
		t.Errorf("expected first inject remindType=full, got %q", remindType)
	}
	if !called {
		t.Fatal("expected persist callback invoked after first inject")
	}
	if persisted.JustActivated {
		t.Error("expected persisted JustActivated=false after first inject")
	}
	if pm.GetPlanState().JustActivated {
		t.Error("expected in-memory JustActivated=false after first inject")
	}
	if pm.GetPlanState().AttachmentCount != 1 {
		t.Errorf("expected AttachmentCount=1 after first inject, got %d", pm.GetPlanState().AttachmentCount)
	}
}
