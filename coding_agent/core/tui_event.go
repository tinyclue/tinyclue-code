package core

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/log"
	"strings"
	"time"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/statusline"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/tui"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/component/markdown"
	"github.com/tinyclue/tinyclue-code/tui/component/spacer"
	"github.com/tinyclue/tinyclue-code/tui/component/status_info"
	"github.com/tinyclue/tinyclue-code/tui/component/task_panel"
	textcomp "github.com/tinyclue/tinyclue-code/tui/component/text"
	"github.com/tinyclue/tinyclue-code/tui/component/tool"
	"github.com/tinyclue/tinyclue-code/tui/component/widget"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	tuitypes "github.com/tinyclue/tinyclue-code/tui/types"
)

// RenderStream 管理具有 Start-Update-End 生命周期的流式组件。
type RenderStream struct {
	Comp   core.Component // 主组件（thinking/text/tool）
	Detail core.Component // 详情子组件（thinkOutput/toolOutput，可为 nil）
}

const (
	keyThinking     = "thinking"
	keyText         = "text"
	keyAutoComplete = "auto_complete"
)

// AgentState 表示 Agent 当前的生命周期阶段，用于更新状态容器。
type AgentState int

const (
	AgentStateIdle         AgentState = iota // 空闲
	AgentStateProcessing                     // Agent 启动/处理中
	AgentStateThinking                       // 思考中
	AgentStateSpeaking                       // 回复中
	AgentStateExecuting                      // 执行工具
	AgentStateCompacting                     //上下文压缩
	AgentStateAutoRetrying                   //API调用重试
	AgentStateWaitJob                        //等待异步作业完成

	AgentStateFinished // 完成
)

// TuiEvent 监听 agent_event 队列，以命令模式按事件类型分派到对应 handler。
type TuiEvent struct {
	ctx context.Context
	tui *tui.ModelV2
	// Agent 状态机
	agentState AgentState

	// 状态行（业务对象：计时 + 流式 token 计数渲染，自带驱动时钟，TUI 只赋值渲染）
	statusLine *statusline.StatusLine

	// 状态容器（展示 agent 当前阶段）
	currentStatusComp     *status_info.StatusInfoComponent
	currentStatusCompStat *textcomp.TextComponent

	// 流式渲染跟踪（Start-Update-End 生命周期）
	streams map[string]*RenderStream

	//agent任务面板
	taskPanels map[string]core.Component

	// 工具执行权限确认回调（由 Interactive 注册）
	onPermissionHandler func(types.PermissionRequest)
}

func NewTuiEvent(ctx context.Context) *TuiEvent {
	te := &TuiEvent{
		ctx:        ctx,
		statusLine: statusline.New(),
		streams:    make(map[string]*RenderStream),
		taskPanels: make(map[string]core.Component),
	}
	// 状态行每次内容变化 → 赋值给状态容器并请求重绘
	te.statusLine.SetOnChange(func() {
		te.updateStatusContent()
		if te.tui != nil {
			te.tui.RequestRender()
		}
	})
	return te
}

func (te *TuiEvent) WithTui(tui *tui.ModelV2) *TuiEvent {
	te.tui = tui
	return te
}

// SetOnPermissionHandler 注册工具权限确认回调。
func (te *TuiEvent) SetOnPermissionHandler(handler func(types.PermissionRequest)) {
	te.onPermissionHandler = handler
}

// Run 启动队列监听，在后台 goroutine 中运行直到 ctx 取消。
func (te *TuiEvent) Run() {
	go func() {
		cancel := Subscribe(AgentEventQueue, func(msg types.EventMessage) {
			te.dispatch(msg)
		})
		defer cancel()
		<-te.ctx.Done()
	}()
}

// dispatch 按 EventType 路由到对应 handler（命令模式）。
func (te *TuiEvent) dispatch(event types.EventMessage) {
	switch event.EventType {
	case types.AgentStart:
		te.handleAgentStart()
	case types.AgentEnd:
		te.handleAgentEnd(event.Err)
	case types.UserMessage:
		te.handleUserMessage(event.Message)
	case types.AutoCompleteMessage:
		te.handleAutoCompleteMessage(event.Message)
	case types.AutoCompleteDetail:
		te.handleAutoCompleteDetail(event.Message)
	case types.AssistantThinkingStart:
		//te.handleThinkingStart()
	case types.AssistantThinkingEnd:
		//te.handleThinkingEnd()

	case types.AssistantTextStart:
		te.handleTextStart()
	case types.AssistantTextUpdate:
		te.handleTextUpdate(event.Message, event.Err)
	case types.AssistantTextEnd:
		te.handleTextEnd()

	case types.ToolExecutionStart:
		te.handleToolStart(event.Message)
	case types.ToolExecutionUpdate:
		te.handleToolUpdate(event.Message)
	case types.ToolExecutionEnd:
		te.handleToolEnd(event.Message, event.Err)
	case types.CompactionStartEventType:
		te.handleCompactStart(event.Message, event.Err)
	case types.AutoRetryStartEventType:
		te.handleCompactStart(event.Message, event.Err)
	case types.WaitJob:
		te.handleWaitJob(event.Message, event.Err)
	case types.AgentStat:
		te.handleAgentStat(event.Message, event.Err)
	case types.JobUpdateEventType:
		te.handleJobUpdate(event.Message)
	case types.TaskUpdateEventType:
		te.handleTaskUpdate(event.Message)
	case types.ToolPermissionRequired:
		te.handleToolPermissionRequired(event.Message)
	}
}

// ── 状态容器更新 ──

// setAgentState 更新 agent 状态并同步到状态容器组件。
func (te *TuiEvent) setAgentState(state AgentState) {
	te.agentState = state

	// ── 状态行（计时 + token 计数） ──
	switch state {
	case AgentStateProcessing:
		// 新会话开始：重置状态行并启动内部驱动时钟
		te.statusLine.Start(time.Now())
	case AgentStateFinished:
		// 终态：停止时钟 + 最后一次收敛（显示最终耗时 + token 计数）
		te.statusLine.Stop(time.Now())
		te.updateStatusContent()
	case AgentStateIdle:
		te.statusLine.Stop(time.Now())
	}

	var title string
	switch state {
	case AgentStateProcessing:
		title = "Processing..."
	case AgentStateThinking:
		title = "Thinking..."
	case AgentStateSpeaking:
		title = "Responding..."
	case AgentStateExecuting:
		title = "ToolExecuting..."
	case AgentStateCompacting:
		title = "Conversation Compression..."
	case AgentStateAutoRetrying:
		title = "Retrying..."
	case AgentStateWaitJob:
		title = "Wait for async task completion..."
	case AgentStateFinished:
		title = "Finished"
	case AgentStateIdle:
		title = "Idle"
	}
	if te.currentStatusComp == nil {
		comp := status_info.NewStatusInfoComponent(title)
		comp.SetRequestRender(func() { te.tui.RequestRender() })
		comp.SetState(status_info.StatusRunning)
		te.currentStatusComp = comp
		outputMD := textcomp.New("")
		outputMD.SetLevel(1)
		comp.AddChild(outputMD)
		te.currentStatusCompStat = outputMD
		te.tui.GetContainer(core.StatusContainerType).AddChild(comp)
	} else {
		if state == AgentStateFinished {
			te.currentStatusCompStat.UpdateText("", nil)
			te.currentStatusComp.SetState(status_info.StatusDone)
		} else {
			te.currentStatusComp.SetState(status_info.StatusRunning)
		}
		te.currentStatusComp.SetTitle(title)
	}

	te.tui.RequestRender()
}

// updateStatusContent 把状态行业务对象渲染出的文本赋值给状态容器。
// TUI 层只做赋值与渲染，不关心"算什么"。
func (te *TuiEvent) updateStatusContent() {
	if te.currentStatusComp == nil {
		return
	}
	content := te.statusLine.Render()
	if content == "" {
		te.currentStatusComp.SetContent("")
		return
	}
	te.currentStatusComp.SetContent(tuitypes.FgGray + content + tuitypes.Reset)
}

// ── Status Container（Agent 生命周期） ──

func (te *TuiEvent) handleAgentStart() {
	te.setAgentState(AgentStateProcessing)
}

func (te *TuiEvent) handleAgentEnd(err error) {
	te.setAgentState(AgentStateFinished)
	if te.currentStatusComp == nil {
		return
	}
	//if err != nil {
	//	te.currentStatusComp.SetTitle("Error")
	//	te.currentStatusCompStat.UpdateText(err.Error(), err)
	//}
	te.handleThinkingEnd()
}

// ── Chat Container（User Message） ──

func (te *TuiEvent) handleUserMessage(msg interface{}) {
	userMsg, ok := msg.(apitypes.UserMessage)
	if !ok {
		return
	}
	te.setAgentState(AgentStateProcessing)
	md := textcomp.New("")
	md.SetWidget(&widget.PromptWidget{})
	md.SetBackground(tuitypes.BgGray)
	md.UpdateText(userMsg.Text, nil)

	te.addChatChild(md)
}

func (te *TuiEvent) handleAutoCompleteMessage(msg interface{}) {
	userMsg, ok := msg.(apitypes.UserMessage)
	if !ok {
		return
	}
	md := textcomp.New("")
	md.SetWidget(&widget.PromptWidget{})
	md.SetBackground(tuitypes.BgGray)
	md.UpdateText(userMsg.Text, nil)

	outputMD := textcomp.New("")
	outputMD.SetLevel(1)
	md.AddChild(outputMD)

	te.streams[keyAutoComplete] = &RenderStream{Comp: md, Detail: outputMD}

	te.addChatChild(md)
}

func (te *TuiEvent) handleAutoCompleteDetail(msg interface{}) {
	delta, ok := msg.(string)
	if !ok {
		return
	}
	if delta == "" {
		return
	}
	s := te.streams[keyAutoComplete]
	if s == nil || s.Detail == nil {
		// 无自动完成上下文（启动阶段的 MCP 连接/授权提示等，尚未打开过面板）：自建独立
		// 聊天文本块承载本条 detail，不伪造 /mcp 命令回显，也不注册进 streams（后续 detail
		// 各自成块）。面板打开后此分支不再命中，仍走下方的块内追加。
		//md := textcomp.New("")
		//md.SetWidget(widget.NewStatusWidget())
		//md.UpdateText(delta, nil)
		//te.addChatChild(md)
		return
	}
	// 每条 detail 独立成行：异步 MCP 动作的多个结果可能连续到达，纯拼接会挤成一行。
	// 首条不加前导换行，避免 Detail 块顶部空行。队列单 goroutine 串行消费，读-改-写安全。
	detail := s.Detail.(*textcomp.TextComponent)
	if detail.Text() != "" {
		delta = "\n" + delta
	}
	detail.AddText(delta)
	te.tui.RequestRender()
}

// ── Chat Container（Assistant Thinking 流式） ──

func (te *TuiEvent) handleThinkingStart() {
	te.setAgentState(AgentStateThinking)

	// 防御：若上一个 thinking 块因流异常未收到 End 仍可见，先隐藏它，
	// 保证任意时刻至多一个可见 thinking 块，避免孤儿块堆叠。
	if old := te.streams[keyThinking]; old != nil {
		old.Comp.(interface{ SetHidden(bool) }).SetHidden(true)
	}

	md := textcomp.New("")
	widgetInstance := widget.NewStatusWidget()
	widgetInstance.SetState(widget.StatusWidgetRunning)
	md.SetWidget(widgetInstance)
	md.UpdateText("Thinking...", nil)

	// 挂载一个 text 子组件，用于展示 think 信息
	outputMD := textcomp.New("")
	outputMD.SetLevel(1)
	md.AddChild(outputMD)
	md.AddChild(spacer.New(1))

	te.streams[keyThinking] = &RenderStream{Comp: md, Detail: outputMD}

	te.addThinkingChild(md)
}

func (te *TuiEvent) handleThinkingEnd() {
	s := te.streams[keyThinking]
	if s == nil {
		return
	}
	// 隐藏 thinking 组件，保留在容器中以供全量重绘
	s.Comp.(interface{ SetHidden(bool) }).SetHidden(true)
	delete(te.streams, keyThinking)
	te.tui.RequestRender()
}

// ── Chat Container（Assistant Text 流式） ──

func (te *TuiEvent) handleTextStart() {
	te.setAgentState(AgentStateSpeaking)
	md := markdown.NewV2()
	md.SetWidget(widget.NewStatusWidget())
	te.streams[keyText] = &RenderStream{Comp: md}
	te.addChatChild(md)
}

func (te *TuiEvent) handleTextUpdate(msg interface{}, err error) {
	s := te.streams[keyText]
	if s == nil || s.Comp == nil {
		return
	}
	delta, ok := msg.(string)
	if !ok {
		return
	}
	if err != nil {
		delta = tuitypes.FgLightRed + delta + tuitypes.Reset
	}
	s.Comp.(*markdown.MarkdownNewComponent).AddText(delta)
	te.tui.RequestRender()
}

func (te *TuiEvent) handleTextEnd() {
	delete(te.streams, keyText)
	te.tui.RequestRender()
}

// OnTokenEstimate 是 session 级 TokenCounter 的 token 估算变化回调：
// 把最新估算值喂给状态行 token 子段（token 计数与渲染解耦）。
// 显示值由状态行内部驱动时钟逐 Tick 平滑收敛并回调 onChange 重绘，
// 无需在此触发渲染。
func (te *TuiEvent) OnTokenEstimate(estimate int) {
	te.statusLine.SetTokenEstimate(estimate)
}

// ── Chat Container（Tool Execution） ──

func (te *TuiEvent) handleToolStart(msg interface{}) {
	tc, ok := msg.(apitypes.ToolCall)
	if !ok {
		return
	}
	log.Debugf(te.ctx, "handleToolStart: tool=%s id=%s command=%v", tc.Name, tc.ID, tc.Arguments["command"])
	te.setAgentState(AgentStateExecuting)

	toolStreamKey := "tool:" + tc.ID
	stream := te.buildToolStream(tc)
	te.streams[toolStreamKey] = stream
	log.Debugf(te.ctx, "handleToolStart: toolComp=%p toolKey=%s", stream.Comp, toolStreamKey)
	te.addChatChild(stream.Comp)
}

// buildToolStream 构建工具执行组件（状态圆点 + 输出子块 + 按工具类型折叠）。
// live（handleToolStart）与 resume 回放（ReplaySession）共用同一构建路径，避免两处漂移。
func (te *TuiEvent) buildToolStream(tc apitypes.ToolCall) *RenderStream {
	toolName, subName := resolveToolArg(tc)
	toolComp := tool.NewToolComponent(toolName, subName)
	// 默认挂载状态小圆点，设为进行中
	dot := widget.NewStatusWidget()
	dot.SetState(widget.StatusWidgetDefault)
	toolComp.SetWidget(dot)

	// 挂载一个 text 子组件，用于展示工具执行输出
	outputMD := textcomp.New("")
	outputMD.SetLevel(1)

	// 按工具类型配置折叠
	switch toolName {
	case "Bash":
		// 后台 bash 展开更多尾部（最近 5 行）；同步保持 1 行
		tail := 1
		if b, _ := tc.Arguments["run_in_background"].(bool); b {
			tail = 5
		}
		outputMD.SetCollapsible(component.ShowTail(tail))
	case "Read":
		outputMD.SetCollapsible(component.ShowHead(1))
	case "Glob":
		outputMD.SetCollapsible(component.ShowHead(1))
	case "Grep":
		outputMD.SetCollapsible(component.ShowHead(1))
	case "Edit":
		outputMD.SetCollapsible(component.ShowHead(1))
	case "Write":
		outputMD.SetCollapsible(component.ShowHead(1))
	default:
		// MCP 工具（mcp__<server>__<tool>）：输出通常很大（页面快照/可访问性树等），
		// 默认折叠只保留前 5 行，避免巨量内容拖慢渲染并占满聊天区。
		if strings.HasPrefix(toolName, "mcp") {
			outputMD.SetCollapsible(component.ShowHead(1))
		}
	}

	toolComp.AddChild(outputMD)
	return &RenderStream{Comp: toolComp, Detail: outputMD}
}

// resolveToolArg 从 ToolCall 中提取用于展示的 toolName 和 subName。
func resolveToolArg(tc apitypes.ToolCall) (toolName, subName string) {
	getArg := func(key string) string {
		if v, ok := tc.Arguments[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	switch tc.Name {
	case "Bash":
		return "Bash", getArg("command")
	case "Read":
		return "Read", getArg("path")
	case "Write":
		return "Write", getArg("path")
	case "Edit":
		return "Edit", getArg("path")
	case "Glob":
		return "Glob", "pattern:" + getArg("pattern")
	case "Grep":
		return "Grep", "pattern:" + getArg("pattern")
	case "WebSearch":
		return "WebSearch", getArg("query")
	case "ToolSearch":
		return "ToolSearch", getArg("query")
	case "RunAgent":
		sub := getArg("subagent_type")
		if sub == "" {
			sub = tc.Name
		}
		return sub, getArg("description")
	default:
		// Glob / Grep / EnterPlanMode / ExitPlanMode / AskUserQuestion / SendMessage
		sub := getArg("command")
		if sub == "" {
			sub = getArg("path")
		}
		if sub == "" {
			sub = getArg("query")
		}
		if sub == "" {
			sub = tc.Name
		}
		return tc.Name, sub
	}
}

func (te *TuiEvent) handleToolUpdate(msg interface{}) {
	tc, ok := msg.(types.ToolContext)
	if !ok {
		return
	}
	stream := te.getStreamByToolID(tc.ToolCall.ID)
	if stream == nil {
		return
	}
	var text string
	switch content := tc.Content.(type) {
	case types.TextContent:
		text = content.Delta
	case types.ImageContent:
		text = content.Text
	}
	if text != "" {
		stream.Detail.(*textcomp.TextComponent).AddText(text)
		te.tui.RequestRender()
	}
}

func (te *TuiEvent) handleCompactStart(msg interface{}, err error) {
	te.setAgentState(AgentStateCompacting)
}

func (te *TuiEvent) handleAutoRetryStart(msg interface{}, err error) {
	te.setAgentState(AgentStateAutoRetrying)
}

func (te *TuiEvent) handleWaitJob(msg interface{}, err error) {
	te.setAgentState(AgentStateWaitJob)
}

func (te *TuiEvent) handleAgentStat(msg interface{}, err error) {
	if m, ok := msg.(types.ProgressSummaryMessage); ok {
		te.currentStatusCompStat.UpdateText(m.Summary, err)
	}
}

func (te *TuiEvent) getStreamByToolID(toolID string) *RenderStream {
	streamKey := "tool:" + toolID
	stream := te.streams[streamKey]
	if stream == nil || stream.Detail == nil {
		return nil
	}
	return stream
}

func (te *TuiEvent) deleteStreamByToolID(toolID string) {
	toolKey := "tool:" + toolID
	delete(te.streams, toolKey)
}

func (te *TuiEvent) handleToolEnd(msg interface{}, err error) {
	tc, ok := msg.(types.ToolContext)
	if !ok {
		return
	}
	stream := te.getStreamByToolID(tc.ToolCall.ID)
	if stream == nil {
		return
	}

	switch content := tc.Content.(type) {
	case types.SubAgentContent:
		// RunAgent 异步：保留流，等待后续 JobUpdate 更新摘要并收尾。
		if content.IsAsync {
			te.tui.RequestRender()
			return
		}
		te.renderJob(content.AgentId)
	case types.BashTaskContent:
		// 后台 bash：保留流，等待 JobUpdate 更新状态并收尾（与异步 agent 一致）。
		if content.IsAsync {
			te.tui.RequestRender()
			return
		}
		// 同步 bash：渲染完整输出文本（含截断提示），与 TextContent 渲染一致。
		renderTextContent(stream.Detail, types.TextContent{Text: content.Text, Details: content.Details}, err)
	case types.TextContent:
		renderTextContent(stream.Detail, content, err)
	case types.ImageContent:
		if stream.Detail != nil && content.Text != "" {
			stream.Detail.(*textcomp.TextComponent).UpdateText(content.Text, err)
		}
	case types.GrepContent:
		renderGrepContent(stream.Detail, content, err)
	case types.GlobContent:
		renderGlobContent(stream.Detail, content, err)
	case types.ExitPlanModeContent:
		// ExitPlanMode：批准后回显三段——确认 / 计划保存路径 / 计划正文。
		var parts []string
		parts = append(parts, "User approved Tinyclue plan.")
		if content.FilePath != "" {
			parts = append(parts, "Plan saved to :"+content.FilePath)
		}
		if content.Plan != "" {
			parts = append(parts, content.Plan)
		}
		renderTextContent(stream.Detail, types.TextContent{Text: strings.Join(parts, "\n")}, err)
	}

	// 状态小圆点：成功/失败
	if err != nil {
		setToolWidgetState(stream.Comp, widget.StatusWidgetFail)
	} else {
		setToolWidgetState(stream.Comp, widget.StatusWidgetSuccess)
	}
	te.deleteStreamByToolID(tc.ToolCall.ID)
	te.tui.RequestRender()
}

// renderTextContent 渲染文本工具最终输出：Text + diff 详情。
func renderTextContent(detail core.Component, content types.TextContent, err error) {
	if detail == nil {
		return
	}
	var parts []string
	if content.Text != "" {
		parts = append(parts, content.Text)
	}
	if d, ok := content.Details["diff"]; ok {
		if ds, ok := d.(string); ok && ds != "" {
			parts = append(parts, ds)
		}
	}
	if len(parts) > 0 {
		detail.(*textcomp.TextComponent).UpdateText(strings.Join(parts, "\n"), err)
	}
}

// renderGrepContent 渲染 Grep 输出：content / files_with_matches / count 三种模式。
func renderGrepContent(detail core.Component, content types.GrepContent, err error) {
	if detail == nil {
		return
	}
	text := content.Content
	switch {
	case text == "":
		switch content.Mode {
		case "count":
			text = fmt.Sprintf("Found %d matches across %d files", content.NumMatches, content.NumFiles)
		case "files_with_matches":
			text = fmt.Sprintf("Found %d files", content.NumFiles)
		default:
			text = "No matches found"
		}
	case content.Mode != "content":
		text = fmt.Sprintf("Found %d files\n%s", content.NumFiles, text)
	}
	if text != "" {
		detail.(*textcomp.TextComponent).UpdateText(text, err)
	}
}

// renderGlobContent 渲染 Glob 输出：文件数 + 文件名列表 + 截断提示。
func renderGlobContent(detail core.Component, content types.GlobContent, err error) {
	if detail == nil {
		return
	}
	text := fmt.Sprintf("Found %d files", content.NumFiles)
	if len(content.Filenames) > 0 {
		text += "\n" + strings.Join(content.Filenames, "\n")
	}
	if content.Truncated {
		text += "\n(Results are truncated. Consider using a more specific path or pattern.)"
	}
	detail.(*textcomp.TextComponent).UpdateText(text, err)
}

// setToolWidgetState 设置工具组件状态小圆点的颜色。
func setToolWidgetState(comp core.Component, state widget.StatusWidgetState) {
	tc, ok := comp.(*tool.ToolComponent)
	if !ok {
		return
	}
	if dot, ok := tc.Widget().(*widget.StatusWidget); ok {
		dot.SetState(state)
	}
}

// ── 公共渲染 ──

// addChatChild 向 chat 容器添加子组件。
func (te *TuiEvent) addChatChild(child core.Component) {
	container := te.tui.GetContainer(core.ChatContainerType)
	container.AddChild(child)
	container.AddChild(spacer.New(1))
	log.Debugf(te.ctx, "addChatChild: container children=%d childType=%T", container.Len(), child)
	te.tui.RequestRender()
}

func (te *TuiEvent) addThinkingChild(child core.Component) {
	te.tui.GetContainer(core.ThinkingContainerType).AddChild(child)
	te.tui.RequestRender()
}

func (te *TuiEvent) handleJobUpdate(msg interface{}) {
	taskId, ok := msg.(string)
	if !ok {
		return
	}

	te.renderJob(taskId)
}

func (te *TuiEvent) renderJob(taskId string) {
	job.JobInstance.Read(taskId, func(ts *types.JobState) {
		switch data := ts.Data.(type) {
		case *types.LocalSubAgentJobState:
			te.renderSubAgentJob(ts, data)
		case *types.LocalBashJobState:
			te.renderBashJob(ts, data)
		}
	})
}

// renderSubAgentJob 渲染子 agent 异步作业：摘要 + 终态收尾。
func (te *TuiEvent) renderSubAgentJob(ts *types.JobState, data *types.LocalSubAgentJobState) {
	stream := te.getStreamByToolID(data.ToolUseId)
	if stream == nil {
		return
	}
	renderSubAgentJobSummary(stream.Detail, ts)

	if data.IsAsync {
		// 终态：更新状态圆点并清理流（后续不再有 JobUpdate）。
		if ts.Status == types.JobStatusCompleted || ts.Status == types.JobStatusFailed || ts.Status == types.JobStatusKilled {
			if ts.Status == types.JobStatusCompleted {
				setToolWidgetState(stream.Comp, widget.StatusWidgetSuccess)
			} else {
				setToolWidgetState(stream.Comp, widget.StatusWidgetFail)
			}
			te.deleteStreamByToolID(data.ToolUseId)
		}
	}
}

// renderBashJob 渲染后台 bash 作业：状态 + 作业ID + 输出文件路径 + 最近输出行（对齐 renderSubAgentJob）。
func (te *TuiEvent) renderBashJob(ts *types.JobState, data *types.LocalBashJobState) {
	stream := te.getStreamByToolID(data.ToolUseId)
	if stream == nil {
		return
	}

	var parts []string
	// ▶ ${作业状态描述}
	parts = append(parts, tuitypes.FgLightRed+"▶ "+bashJobStatusLine(ts, data)+tuitypes.Reset)
	// ${作业ID描述}
	parts = append(parts, fmt.Sprintf("%sJob ID: %s%s", tuitypes.FgGray, ts.ID, tuitypes.Reset))
	// ${输出文件路径}
	if ts.OutputFilePath != "" {
		parts = append(parts, fmt.Sprintf("%s%s%s", tuitypes.FgGray, ts.OutputFilePath, tuitypes.Reset))
	}
	// 最近 5 行输出
	lines := data.Progress.RecentLines
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	parts = append(parts, lines...)

	if stream.Detail != nil {
		stream.Detail.(*textcomp.TextComponent).UpdateText(strings.Join(parts, "\n"), nil)
	}
	if ts.Status == types.JobStatusCompleted || ts.Status == types.JobStatusFailed || ts.Status == types.JobStatusKilled {
		if ts.Status == types.JobStatusCompleted {
			setToolWidgetState(stream.Comp, widget.StatusWidgetSuccess)
		} else {
			setToolWidgetState(stream.Comp, widget.StatusWidgetFail)
		}
		te.deleteStreamByToolID(data.ToolUseId)
	}
	te.tui.RequestRender()
}

// bashJobStatusLine 生成后台 bash 作业的状态描述行。
func bashJobStatusLine(ts *types.JobState, data *types.LocalBashJobState) string {
	switch ts.Status {
	case types.JobStatusPending, types.JobStatusRunning:
		elapsed := time.Since(ts.StartTime).Round(time.Second)
		return fmt.Sprintf("running, %s", elapsed.String())
	case types.JobStatusCompleted:
		return fmt.Sprintf("completed (exit %d)", data.ExitCode)
	case types.JobStatusKilled:
		return "killed"
	default: // JobStatusFailed
		return fmt.Sprintf("failed (exit %d)", data.ExitCode)
	}
}

// ── RunAgent 工具摘要渲染 ──

// 将子 agent 作业的 LastActivesSummary、RecentTools 和汇总统计渲染到 detail 组件中。
func renderSubAgentJobSummary(detail core.Component, ts *types.JobState) {
	if detail == nil {
		return
	}
	data, ok := ts.Data.(*types.LocalSubAgentJobState)
	if !ok {
		return
	}
	var parts []string
	if data.Progress.LastActivesSummary != "" {
		parts = append(parts, tuitypes.FgLightRed+"▶ "+data.Progress.LastActivesSummary+tuitypes.Reset)
	}
	for _, tc := range data.Progress.RecentTools {
		parts = append(parts, formatToolLine(tc))
	}
	elapsed := time.Since(ts.StartTime).Round(time.Second)
	summary := fmt.Sprintf("%s%d tool uses · %s tokens · %s%s",
		tuitypes.FgGray, data.Progress.ToolUseCount,
		utils.FormatTokens(data.Progress.TokenCount), elapsed.String(), tuitypes.Reset)
	parts = append(parts, summary)
	detail.(*textcomp.TextComponent).UpdateText(strings.Join(parts, "\n"), nil)
}

// formatToolLine 将 ToolCall 格式化为紧凑单行，超过终端宽度则截断末尾显示"..."。
func formatToolLine(tc apitypes.ToolCall) string {
	arg := formatToolArg(tc)
	if idx := strings.IndexAny(arg, "\n\r"); idx >= 0 {
		arg = arg[:idx] + "..."
	}

	termWidth := terminal.DefaultTerminalContext.GetWidth()
	maxLineWidth := termWidth - 20 // 缩进+右侧留白
	if maxLineWidth < 20 {
		maxLineWidth = 20
	}

	var line string
	if arg != "" {
		line = fmt.Sprintf("  %s: %s", tc.Name, arg)
	} else {
		line = fmt.Sprintf("  %s", tc.Name)
	}

	if len([]rune(line)) > maxLineWidth {
		truncated := string([]rune(line)[:maxLineWidth-3])
		line = truncated + "..."
	}
	return line
}

// formatToolArg 根据工具名称提取关键参数文本。
func formatToolArg(tc apitypes.ToolCall) string {
	if tc.Arguments == nil {
		return ""
	}
	getStr := func(key string) string {
		v, ok := tc.Arguments[key]
		if !ok {
			return ""
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return ""
		}
		return s
	}

	switch tc.Name {
	case "Bash":
		return getStr("command")
	case "Read", "Write", "Edit":
		return getStr("path")
	case "Glob":
		if p := getStr("pattern"); p != "" {
			return "pattern:" + p
		}
		return getStr("path")
	case "Grep":
		if p := getStr("pattern"); p != "" {
			return "pattern:" + p
		}
		return getStr("path")
	case "WebSearch", "ToolSearch":
		return getStr("query")
	case "AskUserQuestion":
		if qs, ok := tc.Arguments["questions"].([]interface{}); ok && len(qs) > 0 {
			if q, ok := qs[0].(map[string]interface{}); ok {
				if s, ok := q["question"].(string); ok && s != "" {
					return s
				}
			}
		}
		return ""
	case "TaskCreate":
		return getStr("subject")
	case "TaskUpdate":
		if s := getStr("subject"); s != "" {
			return "subject:" + s
		}
		return getStr("taskId")
	case "TaskGet":
		return getStr("taskId")
	case "RunAgent":
		if sub := getStr("subagent_type"); sub != "" {
			if desc := getStr("description"); desc != "" {
				return sub + ": " + desc
			}
			return sub
		}
		return getStr("description")
	case "EnterPlanMode", "ExitPlanMode", "TaskList":
		return ""
	default:
		// 未知工具：尝试常见参数
		for _, key := range []string{"command", "path", "query", "pattern", "description", "subject", "taskId"} {
			if s := getStr(key); s != "" {
				return s
			}
		}
	}
	return ""
}

// handleToolPermissionRequired 处理工具权限确认事件，调用注册的 onPermissionHandler 回调。
func (te *TuiEvent) handleToolPermissionRequired(msg interface{}) {
	req, ok := msg.(types.PermissionRequest)
	if !ok {
		return
	}
	if te.onPermissionHandler != nil {
		te.onPermissionHandler(req)
	}
}

func (te *TuiEvent) handleTaskUpdate(msg interface{}) {
	event, ok := msg.(types.TaskEvent)
	if !ok {
		return
	}
	agentId := event.AgentId
	agentInstance := AMInstance.GetAgent(agentId)
	if agentInstance == nil {
		return
	}
	taskManager := agentInstance.GetTaskManager()
	tasks, err := taskManager.List()
	if err != nil {
		return
	}

	// 获取或创建该 agent 的任务面板组件。
	panel := te.getOrCreateTaskPanel(agentId)
	panel.SetTasks(tasks)

	// 控制容器显隐。
	container := te.tui.GetContainer(core.TaskPanelContainerType)
	if container == nil {
		return
	}

	if event.Type == types.TaskEventListReset || len(tasks) == 0 {
		container.SetHidden(true)
	} else {
		container.SetHidden(false)
	}

	te.tui.RequestRender()
}

// getOrCreateTaskPanel 返回 agentId 对应的任务面板，不存在则创建并挂载到容器。
func (te *TuiEvent) getOrCreateTaskPanel(agentId string) *task_panel.TaskPanel {
	if existing, ok := te.taskPanels[agentId]; ok {
		return existing.(*task_panel.TaskPanel)
	}

	panel := task_panel.New(agentId)
	panel.SetWidget(widget.NewStatusWidget())
	te.taskPanels[agentId] = panel

	container := te.tui.GetContainer(core.TaskPanelContainerType)
	if container != nil {
		// 替换已有面板（同一容器，只保留当前 agent 的面板）。
		container.ClearChildren()
		container.AddChild(panel)
		container.AddChild(spacer.New(1))

	}

	return panel
}
