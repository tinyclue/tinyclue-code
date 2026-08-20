package core

import (
	"context"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/plan"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type AgentToolCommand struct {
	ctx         context.Context
	runtimeCtx  *core_types.AgentRuntimeContext
	planManager *plan.PlanManager
}

func NewAgentToolCommand(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentToolCommand {
	return &AgentToolCommand{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (atc *AgentToolCommand) WithPlanManager(planManager *plan.PlanManager) *AgentToolCommand {
	atc.planManager = planManager
	return atc
}

func (atc *AgentToolCommand) AgentStart() {
	atc.planManager.AgentStart()
}

func (atc *AgentToolCommand) RunWithCommands(commands []core_types.ToolCommand) {
	if len(commands) == 0 {
		return
	}
	for _, command := range commands {
		switch command {
		case core_types.EnterPlanCommand:
			atc.runEnterPlan()
		case core_types.ExitPlanCommand:
			atc.runExitPlan()
		}
	}
}

func (atc *AgentToolCommand) runEnterPlan() {
	atc.planManager.EnterPlan()
}

func (atc *AgentToolCommand) runExitPlan() {
	atc.planManager.ExitPlan()
}
