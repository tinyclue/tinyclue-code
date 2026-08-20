package codingagent

import (
	"github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/tui/component/askuserquestion"
	"github.com/tinyclue/tinyclue-code/tui/component/exitplanmode"
	"github.com/tinyclue/tinyclue-code/tui/component/permission"
	"github.com/tinyclue/tinyclue-code/tui/core"
)

// PermissionQueueItem 权限请求队列项，包含请求信息和对应的面板组件。
type PermissionQueueItem struct {
	req       core_types.PermissionRequest
	panelComp core.Component
}

// handlePermissionDialog 收到新的权限请求：创建面板 → 入队 → 尝试展示。
func (ia *Interactive) handlePermissionDialog(req core_types.PermissionRequest) {
	panelComp := ia.createPermissionPanelByTool(req)
	if panelComp == nil {
		return
	}
	ia.permissionQueue = append(ia.permissionQueue, PermissionQueueItem{req, panelComp})
	ia.tryShowNextPanel()
}

// closePermissionDialog 关闭当前权限面板（由按钮回调调用）：出队 → 尝试展示下一个。
func (ia *Interactive) closePermissionDialog() {
	if len(ia.permissionQueue) > 0 {
		ia.permissionQueue = ia.permissionQueue[1:]
	}
	ia.tryShowNextPanel()
}

// tryShowNextPanel 队列有剩余就展示队首，队列为空就隐藏面板容器恢复编辑区。
func (ia *Interactive) tryShowNextPanel() {
	if len(ia.permissionQueue) > 0 {
		ia.activatePanel(ia.permissionQueue[0].panelComp)
	} else {
		ia.hidePanelContainer()
	}
}

// activatePanel 隐藏编辑区，在权限容器中渲染指定面板。
func (ia *Interactive) activatePanel(comp core.Component) {
	ia.tui.GetContainer(core.StatusContainerType).SetHidden(true)
	ia.tui.GetContainer(core.EditorContainerType).SetHidden(true)
	ia.tui.GetContainer(core.FooterContainerType).SetHidden(true)

	pc := ia.tui.GetContainer(core.PermissionPanelContainerType)
	pc.ClearChildren()
	pc.AddChild(comp)
	pc.SetHidden(false)
	ia.tui.SetFocus(comp)
	ia.tui.RequestRender()
}

// hidePanelContainer 隐藏权限容器，恢复编辑区和状态栏。
func (ia *Interactive) hidePanelContainer() {
	pc := ia.tui.GetContainer(core.PermissionPanelContainerType)
	pc.ClearChildren()
	pc.SetHidden(true)

	ia.tui.GetContainer(core.StatusContainerType).SetHidden(false)
	ia.tui.GetContainer(core.EditorContainerType).SetHidden(false)
	ia.tui.GetContainer(core.FooterContainerType).SetHidden(false)
	ia.tui.SetFocus(ia.defaultEditor)
	ia.tui.RequestRender()
}

// createPermissionPanelByTool 根据 toolName 创建对应的权限确认面板组件，并完成回调绑定。
func (ia *Interactive) createPermissionPanelByTool(req core_types.PermissionRequest) core.Component {
	switch req.ToolName {
	case core_types.ASK_USER_QUESTION_TOOL_NAME:
		return ia.createAskUserQuestionPanel(req)
	case core_types.EXIT_PLAN_MODE_TOOL_NAME:
		return ia.createExitPlanModePanel(req)
	default:
		return ia.createDefaultPermissionPanel(req)
	}
}

// createExitPlanModePanel 创建退出计划模式确认面板：展示计划 + 批准/拒绝（可带原因）。
// 拒绝时携带 Feedback（可选），agent_loop 据此拼 REJECT_MESSAGE_WITH_REASON_PREFIX。
func (ia *Interactive) createExitPlanModePanel(req core_types.PermissionRequest) core.Component {
	plan, _ := req.Data["plan"].(string)
	planFilePath, _ := req.Data["planFilePath"].(string)

	panelComp := exitplanmode.New(plan, planFilePath)
	panelComp.OnSubmit(func(action exitplanmode.Action, reason string) {
		resp := core_types.PermissionResponse{}
		switch action {
		case exitplanmode.ActionDeny:
			resp.Action = core_types.BeforeToolCallDeny
			resp.Feedback = reason
		default: // ActionApprove：把内容带回来，供 ExitPlanMode.Execute 读取；reason 作为 acceptFeedback 追加到 tool_result
			resp.Action = core_types.BeforeToolCallAllow
			resp.ModifiedArgs = map[string]any{
				"plan":         plan,
				"planFilePath": planFilePath,
			}
			if reason != "" {
				resp.Feedback = reason
			}
		}
		ia.closePermissionDialog()
		req.ResponseCh <- resp
	})
	panelComp.OnCancel(func() {
		ia.closePermissionDialog()
		req.ResponseCh <- core_types.PermissionResponse{
			Action: core_types.BeforeToolCallDeny,
		}
	})
	return panelComp
}

// createDefaultPermissionPanel 创建通用 Allow/AllowAlways/Deny 权限面板。
func (ia *Interactive) createDefaultPermissionPanel(req core_types.PermissionRequest) core.Component {
	argDisplay := req.Message
	switch req.ToolName {
	case core_types.BASH_TOOL_NAME:
		cmd, _ := req.Args["command"].(string)
		argDisplay = cmd
	case core_types.READ_TOOL_NAME, core_types.WRITE_TOOL_NAME, core_types.EDIT_TOOL_NAME:
		path, _ := req.Args["path"].(string)
		argDisplay = path
	}

	// "不再二次询问"选项：Bash 显示命令，其余工具级只显示工具名。
	argsStr := config.RenderArgs(req.ToolName, req.Args)
	rememberLabel := truncateLabel(config.RememberLabel(req.ToolName, argsStr), 80)

	panelComp := permission.New(req.ToolName, argDisplay, req.Message, rememberLabel)
	panelComp.OnSubmit(func(action permission.Action) {
		resp := core_types.PermissionResponse{}
		switch action {
		case permission.ActionDeny:
			resp.Action = core_types.BeforeToolCallDeny
		case permission.ActionAllowAlways:
			// 落盘规则（Bash → command *，其余 → 工具级）；失败仅记日志，不影响授权。
			ruleArgs := argsStr
			if req.ToolName == core_types.BASH_TOOL_NAME {
				ruleArgs = tools.BashRuleContent(argsStr)
			}
			if err := config.AddAllowRule(req.ToolName, ruleArgs); err != nil {
				log.Errorf(ia.ctx, "add allow rule %s: %v", req.ToolName, err)
			}
			resp.Action = core_types.BeforeToolCallAllow
		default: // ActionAllow
			resp.Action = core_types.BeforeToolCallAllow
		}
		ia.closePermissionDialog()
		req.ResponseCh <- resp
	})
	panelComp.OnCancel(func() {
		req.ResponseCh <- core_types.PermissionResponse{
			Action: core_types.BeforeToolCallDeny,
		}
		ia.closePermissionDialog()
	})
	return panelComp
}

// createAskUserQuestionPanel 创建 AskUserQuestion tabbed 问答面板。
func (ia *Interactive) createAskUserQuestionPanel(req core_types.PermissionRequest) core.Component {
	questions := parseQuestions(req.Args["questions"])
	annotations, _ := req.Args["annotations"].(map[string]any)
	if annotations == nil {
		annotations = make(map[string]any)
	}

	panelComp := askuserquestion.New(questions)
	panelComp.OnSubmit(func(answers askuserquestion.AnswersMap) {
		modifiedArgs := make(map[string]any)
		modifiedArgs["questions"] = req.Args["questions"]

		// Merge "Other" text into annotations[questionText]["notes"]
		for qi, q := range questions {
			if otherText := panelComp.GetOtherText(qi); otherText != "" {
				qText := q.Question
				existing, ok := annotations[qText].(map[string]any)
				if !ok {
					existing = make(map[string]any)
					annotations[qText] = existing
				}
				existing["notes"] = otherText
			}
		}

		modifiedArgs["answers"] = stringMapToAnyMap(answers)
		modifiedArgs["annotations"] = annotations

		ia.closePermissionDialog()
		req.ResponseCh <- core_types.PermissionResponse{
			Action:       core_types.BeforeToolCallAllow,
			ModifiedArgs: modifiedArgs,
		}
	})
	panelComp.OnCancel(func() {
		ia.closePermissionDialog()
		req.ResponseCh <- core_types.PermissionResponse{
			Action: core_types.BeforeToolCallDeny,
		}
	})
	return panelComp
}

// ── AskUserQuestion helpers ──

// parseQuestions converts tool call args questions ([]any) to []askuserquestion.QuestionData.
func parseQuestions(raw any) []askuserquestion.QuestionData {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	questions := make([]askuserquestion.QuestionData, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		q := askuserquestion.QuestionData{
			Question:    toString(m["question"]),
			Header:      toString(m["header"]),
			MultiSelect: toBool(m["multiSelect"]),
		}
		if opts, ok := m["options"].([]any); ok {
			for _, opt := range opts {
				if om, ok := opt.(map[string]any); ok {
					q.Options = append(q.Options, askuserquestion.QuestionOptionData{
						Label:       toString(om["label"]),
						Description: toString(om["description"]),
					})
				}
			}
		}
		questions = append(questions, q)
	}
	return questions
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

// truncateLabel 截断面板选项文案到 maxRunes 个字符（按 rune 计），避免超长参数撑爆面板。
func truncateLabel(label string, maxRunes int) string {
	if maxRunes <= 0 {
		return label
	}
	r := []rune(label)
	if len(r) <= maxRunes {
		return label
	}
	return string(r[:maxRunes]) + "…"
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// stringMapToAnyMap converts map[string]string to map[string]any.
func stringMapToAnyMap(m map[string]string) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
