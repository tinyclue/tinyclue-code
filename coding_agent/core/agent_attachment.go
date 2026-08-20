package core

import (
	"context"
	"fmt"
	types2 "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/plan"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

type AgentAttachment struct {
	ctx         context.Context
	runtimeCtx  *core_types.AgentRuntimeContext
	planManager *plan.PlanManager
}

func NewAgentAttachment(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentAttachment {
	return &AgentAttachment{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (ac *AgentAttachment) Init() {

}

func (ac *AgentAttachment) WithPlanManager(planManager *plan.PlanManager) *AgentAttachment {
	ac.planManager = planManager
	return ac
}

func (ac *AgentAttachment) GetAttachmentMessages(agentUseContext *core_types.AgentUseContext) []*core_types.AttachmentMessage {
	var result []*core_types.AttachmentMessage
	var message []*core_types.AttachmentMessage
	message = ac.planManager.EnterPlanAttachment(agentUseContext.SessionId)
	result = append(result, message...)

	message = ac.planManager.ExitPlanAttachment(agentUseContext.SessionId)
	result = append(result, message...)

	message = ac.getCriticalSystemReminderAttachment(agentUseContext)
	result = append(result, message...)

	return result
}

func (ac *AgentAttachment) getCriticalSystemReminderAttachment(agentUseContext *core_types.AgentUseContext) []*core_types.AttachmentMessage {
	var result []*core_types.AttachmentMessage
	if agentUseContext.AgentDef.CriticalSystemReminder != "" {
		result = append(result, &core_types.AttachmentMessage{
			Type: core_types.CRITICAL_SYSTEM_REMINDER,
			Data: map[string]any{
				"content": agentUseContext.AgentDef.CriticalSystemReminder,
			},
		})
	}
	return result
}

func (ac *AgentAttachment) BuildUserMetaMessages(attachmentMessages []*core_types.AttachmentMessage) []types2.UserMessage {
	var messages []*types2.UserMessage
	for _, att := range attachmentMessages {
		switch att.Type {
		case core_types.PLAN_MODE:
			messages = append(messages, ac.planManager.EnterPlanMetaMessage(att))
		case core_types.PLAN_MODE_EXIT:
			messages = append(messages, ac.planManager.ExitPlanMetaMessage(att))
		case core_types.CRITICAL_SYSTEM_REMINDER:
			text, _ := att.Data["content"].(string)
			message := types2.NewUserMetaMessage(text)
			messages = append(messages, &message)
		}
	}
	var result []types2.UserMessage
	for _, message := range messages {
		u := ac.wrapInSystemReminder(message)
		result = append(result, u)
	}
	return result
}

func (ac *AgentAttachment) wrapInSystemReminder(message *types2.UserMessage) types2.UserMessage {
	message.Text = fmt.Sprintf("<system-reminder>\n%s\n</system-reminder>", message.Text)
	return *message
}
