package core

import (
	"fmt"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/tui/component/markdown"
	textcomp "github.com/tinyclue/tinyclue-code/tui/component/text"
	"github.com/tinyclue/tinyclue-code/tui/component/widget"
	"github.com/tinyclue/tinyclue-code/tui/core"
	tuitypes "github.com/tinyclue/tinyclue-code/tui/types"
)

// ReplaySession 把会话条目重放为 ChatContainer 组件（resume 时把历史渲染回 TUI）。
// 只处理最终态：不经 dispatch（dispatch 依赖 streams 中间态，只适合 live）。
// 顺序与 live 渲染一致：user 块 → assistant markdown 块 → 工具组件（附结果）。
func (te *TuiEvent) ReplaySession(entries []core_types.SessionEntry) {
	if len(entries) == 0 {
		return
	}
	te.renderReplayNotice(fmt.Sprintf("Resumed session · %d messages", len(entries)))

	// 工具结果按 ToolCallId 回填：assistant 的 ToolCalls 先建流，后续 ToolResultMessage 填充。
	toolStreams := make(map[string]*RenderStream)
	for _, e := range entries {
		switch entry := e.(type) {
		case *core_types.CompactionEntry:
			te.renderReplayNotice(fmt.Sprintf("context compressed · %d tokens", entry.Tokens))
		case *core_types.MessageEntry:
			switch msg := entry.Message.(type) {
			case apitypes.UserMessage:
				// plan 提示等 meta 消息不是用户聊天，跳过。
				if msg.IsMeta {
					continue
				}
				te.renderReplayUser(msg.Text)
			case apitypes.AssistantMessage:
				te.renderReplayAssistant(entry, msg)
				for _, tc := range msg.ToolCalls {
					stream := te.buildToolStream(tc)
					toolStreams[tc.ID] = stream
					te.addChatChild(stream.Comp)
				}
			case apitypes.ToolResultMessage:
				stream := toolStreams[msg.ToolCallId]
				if stream == nil {
					continue
				}
				var err error
				if msg.IsError {
					err = errReplayToolFailed
				}
				renderTextContent(stream.Detail, core_types.TextContent{Text: toolResultText(msg)}, err)
				if msg.IsError {
					setToolWidgetState(stream.Comp, widget.StatusWidgetFail)
				} else {
					setToolWidgetState(stream.Comp, widget.StatusWidgetSuccess)
				}
				delete(toolStreams, msg.ToolCallId)
			}
		}
	}
}

// ReplayTasks 恢复任务面板：resume 时把已落盘的任务列表渲染到 TaskPanel 容器。
// 逻辑对齐 handleTaskUpdate（agent → taskManager.List → panel → 按数量显隐容器）。
func (te *TuiEvent) ReplayTasks(agentId string) {
	agent := AMInstance.GetAgent(agentId)
	if agent == nil {
		return
	}
	tasks, err := agent.GetTaskManager().List()
	if err != nil {
		return
	}
	panel := te.getOrCreateTaskPanel(agentId)
	panel.SetTasks(tasks)

	container := te.tui.GetContainer(core.TaskPanelContainerType)
	if container == nil {
		return
	}
	if len(tasks) == 0 {
		container.SetHidden(true)
	} else {
		container.SetHidden(false)
	}
	te.tui.RequestRender()
}

// errReplayToolFailed 仅用于把工具结果文本渲染成红色（UpdateText 只依据 err 非空着色，不展示文案）。
var errReplayToolFailed = fmt.Errorf("tool execution failed")

// renderReplayUser 渲染用户 prompt 块（与 handleUserMessage 一致）。
func (te *TuiEvent) renderReplayUser(text string) {
	md := textcomp.New("")
	md.SetWidget(&widget.PromptWidget{})
	md.SetBackground(tuitypes.BgGray)
	md.UpdateText(text, nil)
	te.addChatChild(md)
}

// renderReplayAssistant 渲染 assistant 回复的 markdown 块（与 handleTextStart/Update 一致）；
// 回复带错误时整块红字（镜像 handleTextUpdate 的 err 分支）。
func (te *TuiEvent) renderReplayAssistant(entry *core_types.MessageEntry, msg apitypes.AssistantMessage) {
	text := msg.TextContent.Text
	if text == "" {
		return
	}
	md := markdown.NewV2()
	md.SetWidget(widget.NewStatusWidget())
	if entry.Error.ErrorMessage != "" {
		text = tuitypes.FgLightRed + text + tuitypes.Reset
	}
	md.AddText(text)
	te.addChatChild(md)
}

// renderReplayNotice 渲染灰色分隔/提示行（resumed 提示、压缩标记）。
func (te *TuiEvent) renderReplayNotice(text string) {
	md := textcomp.New("")
	md.SetWidget(widget.NewStatusWidget())
	md.UpdateText(tuitypes.FgGray+"── "+text+" ──"+tuitypes.Reset, nil)
	te.addChatChild(md)
}

// toolResultText 拼接工具结果的文本块（镜像 live 对 TextContent 的渲染；image 块不回放）。
func toolResultText(tr apitypes.ToolResultMessage) string {
	var parts []string
	for _, c := range tr.Contents {
		if c.Type == "text" && c.Text != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n")
}
