package core

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/agent_session"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/log"
	"time"
)

type AgentLoop struct {
	ctx             context.Context
	runtimeCtx      *core_types.AgentRuntimeContext
	agentTool       *AgentTool
	agentAttachment *AgentAttachment
	eventSink       core_types.EventSink
	toolCommand     *AgentToolCommand
	agentContext    *AgentContext
	agentUseContext *core_types.AgentUseContext
}

func NewAgentLoop(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentLoop {
	return &AgentLoop{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (al *AgentLoop) WithAgentTool(agentTool *AgentTool) *AgentLoop {
	al.agentTool = agentTool
	return al
}

func (al *AgentLoop) WithAgentContext(agentContext *AgentContext) *AgentLoop {
	al.agentContext = agentContext
	return al
}

func (al *AgentLoop) WithAgentAttachment(agentAttachment *AgentAttachment) *AgentLoop {
	al.agentAttachment = agentAttachment
	return al
}

func (al *AgentLoop) WithAgentToolCommand(command *AgentToolCommand) *AgentLoop {
	al.toolCommand = command
	return al
}

func (al *AgentLoop) WithEventSink(sink core_types.EventSink) *AgentLoop {
	al.eventSink = sink
	return al
}

func (al *AgentLoop) Init() {

}

// rejectSmallWindow 运行时窗口拦截：当前模型上下文窗口不足以容纳静态开销 +
// 压缩摘要 + 保留区 + 缓冲时，直接拒绝本轮运行。模型可被 "/" 在运行中切换，
// 故每回合判定；静态开销与模型无关，仅计算一次后缓存。
func (al *AgentLoop) rejectSmallWindow(staticOverhead int) error {

	return agent_session.ValidateWindow(staticOverhead)
}

func (al *AgentLoop) RunAgentLoop(signal *AgentSignal, agentUseContext *core_types.AgentUseContext) error {
	al.agentUseContext = agentUseContext
	err := al.runLoop(signal, agentUseContext)
	return err
}

func (al *AgentLoop) runLoop(signal *AgentSignal, agentUseContext *core_types.AgentUseContext) error {
	for {
		// 运行时窗口拦截：模型可被 "/" 在运行中切换，每回合判定当前模型是否够用。
		if err := al.rejectSmallWindow(agentUseContext.StaticOverhead); err != nil {
			return err
		}
		al.Stat("Starting new turn...")
		al.SendEventMessage(core_types.TurnStart, nil, nil)
		//上下文摘要
		_, err := al.autoCompact(agentUseContext)
		if err != nil {
			return err
		}
		//请求api附带重试
		finalResp, err := al.withRetryRequest(signal, agentUseContext)
		if err != nil {
			return err
		}
		agentUseContext.CurrentContext.AddRespMessage(finalResp)

		// 上下文溢出恢复：压缩后重试
		compacted, err := al.tryRecoverCompact(signal, agentUseContext, finalResp)
		if err != nil {
			return err
		}
		if compacted {
			continue
		}

		if !finalResp.Error.IsSuccess {
			al.Stat("API call failed...")
			al.SendEventMessage(core_types.TurnEnd, nil, nil)
			return fmt.Errorf("%s", finalResp.Error.ErrorMessage)
		}

		hasTools := al.runTools(signal, agentUseContext, finalResp)
		if !hasTools {
			al.SendEventMessage(core_types.TurnEnd, nil, nil)
			return nil
		}
		al.processAttachments(signal, agentUseContext)
		al.Stat("Turn completed...")
	}
}

func (al *AgentLoop) buildChatRequest(agentUseContext *core_types.AgentUseContext) *apitypes.ChatRequest {
	currentMessages := agentUseContext.CurrentContext.BuildMessages()
	currentTools := al.agentTool.GetEnableTools()
	req := &apitypes.ChatRequest{
		Messages: currentMessages,
		Tools:    currentTools,
	}
	return req
}

func (al *AgentLoop) withRetryRequest(signal *AgentSignal, agentUseContext *core_types.AgentUseContext) (*apitypes.ChatResponse, error) {
	maxRetries := 10
	client := api_provider.GetClient()
	if client == nil {
		log.Errorf(signal.Ctx(), "agent_loop: GetClient() returned nil")
		return nil, fmt.Errorf("api provider not initialized")
	}
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	var finalResp *apitypes.ChatResponse
	for attempt := 1; attempt < maxRetries+1; attempt++ {
		if attempt > 1 {
			al.eventSink.Emit(core_types.AutoRetryStartEventType, nil, nil)
			al.Stat(fmt.Sprintf("API request failed, retrying in %ds (%d/10)...", 2*attempt, attempt))
		} else {
			al.Stat(fmt.Sprintf("API request (%d/10)...", attempt))
		}
		params := al.buildChatRequest(agentUseContext)
		requestStart := time.Now()
		finalResp = al.streamResponse(signal, client, params, attempt, agentUseContext, requestStart)
		if attempt > 1 {
			al.eventSink.Emit(core_types.AutoRetryEndEventType, nil, nil)
		}
		if utils.IsRetryableError(finalResp, contextWindow) {
			agentUseContext.CurrentContext.RemoveLastAssistantEntry()
			time.Sleep(time.Duration(2*attempt) * time.Second)
			continue
		}
		break
	}
	return finalResp, nil

}
func (al *AgentLoop) streamResponse(signal *AgentSignal, client *api_provider.Client, params *apitypes.ChatRequest, attempt int, agentUseContext *core_types.AgentUseContext, requestStart time.Time) *apitypes.ChatResponse {
	stream := client.ChatStream(signal.Ctx(), params)
	var finalResp *apitypes.ChatResponse

	for event := range stream.Chan() {
		switch event.EventType {
		case apitypes.EventTypeStart:
			al.Stat("Stream reading started...")

		case apitypes.EventTypeThinkingStart:
			elapsed := int(time.Since(requestStart).Seconds())
			al.Stat(fmt.Sprintf("Model thinking started...(%ds)", elapsed))
			al.SendEventMessage(core_types.AssistantThinkingStart, nil, nil)

		case apitypes.EventTypeThinkingDelta:
			al.SendEventMessage(core_types.AssistantThinkingUpdate, event.Delta, nil)
			al.Stat("Model thinking...")
		case apitypes.EventTypeThinkingEnd:
			elapsed := int(time.Since(requestStart).Seconds())
			al.Stat(fmt.Sprintf("Model thinking completed...(%ds)", elapsed))
			al.SendEventMessage(core_types.AssistantThinkingEnd, nil, nil)

		case apitypes.EventTypeTextStart:
			elapsed := int(time.Since(requestStart).Seconds())
			al.Stat(fmt.Sprintf("Model responding...(%ds)", elapsed))
			al.SendEventMessage(core_types.AssistantTextStart, nil, nil)

		case apitypes.EventTypeTextDelta:
			al.SendEventMessage(core_types.AssistantTextUpdate, event.Delta, nil)
			al.Stat("Model responding...")
		case apitypes.EventTypeTextEnd:
			elapsed := int(time.Since(requestStart).Seconds())
			al.Stat(fmt.Sprintf("Model response completed...(%ds)", elapsed))
			al.SendEventMessage(core_types.AssistantTextEnd, nil, nil)

		case apitypes.EventTypeToolCallStart:
			elapsed := int(time.Since(requestStart).Seconds())
			al.Stat(fmt.Sprintf("Model needs to call tools...(%ds)", elapsed))
		case apitypes.EventTypeToolCallDelta:
		case apitypes.EventTypeToolCallEnd:

		case apitypes.EventTypeDone, apitypes.EventTypeError:
			finalResp = event.Response
			al.Stat("Stream reading completed...")
		}
	}
	return finalResp
}

func (al *AgentLoop) autoCompact(agentUseContext *core_types.AgentUseContext) (bool, error) {
	var err error
	maxRetries := 3
	for attempt := 1; attempt < maxRetries+1; attempt++ {
		al.Stat("Checking context compaction need...")
		if !al.agentContext.NeedCompact() {
			return false, nil
		}
		if attempt > 1 {
			al.Stat(fmt.Sprintf("Compaction retry (%d/3)...", attempt))
		} else {
			al.Stat("Compacting context...")
		}
		// 压缩 session
		err = al.agentContext.DoCompactV2(core_types.CompactModeDefault)
		if err == nil {
			al.Stat("Context compaction successful...")
			al.rebuildCurrentContext(agentUseContext)
			return true, nil
		}
		if errors.Is(err, core_types.ErrNoContextToCompact) {
			// 无新增内容可压（估算超线但可压区为空，见场景1/2）：跳过本轮压缩，
			// 不视为失败。哨兵是 Default 路径的主防线——窗口拦截已把场景2的模型
			// 挡在启动外，此处兜住场景1（单条超大消息占满保留区）等残余情形。
			al.Stat("No context to compact, skipping...")
			return false, nil
		}
		time.Sleep(time.Duration(2*attempt) * time.Second)
	}
	al.Stat("Context compaction failed...")
	return false, err
}

func (al *AgentLoop) Stat(summary string) {
	if al.agentUseContext != nil {
		al.agentUseContext.CurrentContext.Stat(summary)
	}
}

// tryRecoverCompact 检查 finalResp 是否为上下文溢出错误，如果是则执行压缩。
// 成功：重建上下文并移除错误回复。失败：仅移除错误回复让外层重试。
// 返回 true 表示需要重试，外层应 continue。
func (al *AgentLoop) tryRecoverCompact(signal *AgentSignal, agentUseContext *core_types.AgentUseContext, finalResp *apitypes.ChatResponse) (bool, error) {
	if finalResp.Error.IsSuccess {
		return false, nil
	}
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	if !utils.IsContextOverflow(finalResp, contextWindow) {
		return false, nil
	}
	al.Stat("Context overflow, recovering via compaction...")
	var err error
	maxRetries := 3
	for attempt := 1; attempt < maxRetries+1; attempt++ {
		if attempt > 1 {
			al.Stat(fmt.Sprintf("Compaction recovery retry (%d/3)...", attempt))
		}
		err = al.agentContext.DoCompactV2(core_types.CompactModeRecover)
		if err == nil {
			al.Stat("Context overflow recovery successful...")
			al.rebuildCurrentContext(agentUseContext)
			return true, nil
		}
		if errors.Is(err, core_types.ErrNoContextToCompact) {
			// 溢出但无新增内容可压（如上次 Recover 后 summary 本身过大）：压缩
			// 无法改善，交还 runLoop 按真实溢出错误处理，避免重试 3 次后中止 run。
			al.Stat("Context overflow but no new content to compact, propagating original error...")
			return false, nil
		}
		time.Sleep(time.Duration(2*attempt) * time.Second)
	}
	al.Stat("Context overflow recovery failed...")
	return false, err
}

func (al *AgentLoop) rebuildCurrentContext(agentUseContext *core_types.AgentUseContext) {
	// 从 session 重建规范消息列表（包含压缩后的内容）
	al.agentContext.RebuildMessage()
	// 原地重建 CurrentContext 的消息列表，OnAppend 回调自动保留
	agentUseContext.CurrentContext.RebuildFromMessages(al.agentContext.GetMessages())
}

func (al *AgentLoop) validateTool(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) bool {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	result := al.agentTool.ValidateTool(signal.Ctx(), tool, runtimeContext.AgentUseContext)
	runtimeContext.ToolsDetail[toolIdx].ValidateContent = result
	if !result.Result {
		failToolResult(toolIdx, runtimeContext, result.Message)
		return false
	}
	return true
}

func failToolResult(toolIdx int, runtimeContext *core_types.ToolRuntimeContext, errMsg string) {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	toolResult := newErrorToolResult(tool.ID, tool.Name, errMsg)
	runtimeContext.ToolsDetail[toolIdx].ToolContext = core_types.ToolContext{
		ToolCall: tool,
		Content: core_types.TextContent{
			Text: errMsg,
		},
	}
	runtimeContext.ToolsDetail[toolIdx].Err = errors.New(errMsg)
	runtimeContext.ToolsDetail[toolIdx].ToolResult = toolResult
}

func (al *AgentLoop) BeforeToolCall(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) bool {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	al.Stat(fmt.Sprintf("Checking tool permission: %s", tool.Name))
	result := al.agentTool.BeforeToolCall(signal.Ctx(), tool, runtimeContext.AgentUseContext)
	runtimeContext.ToolsDetail[toolIdx].BeforeToolCallResult = result
	switch result.Action {
	case core_types.BeforeToolCallDeny:
		msg := result.Message
		if msg == "" {
			msg = core_types.RejectMessage
		}
		failToolResult(toolIdx, runtimeContext, msg)
		return false
	case core_types.BeforeToolCallAsk:
		response := al.handlePermissionAsk(signal, toolIdx, runtimeContext)
		if response.Action == core_types.BeforeToolCallDeny {
			failToolResult(toolIdx, runtimeContext, rejectMessage(response.Feedback))
			return false
		}
		runtimeContext.ToolsDetail[toolIdx].PermissionResponse = &response
		return true
	default:
		return true
	}
}

func (al *AgentLoop) runTools(signal *AgentSignal, agentUseContext *core_types.AgentUseContext, finalResp *apitypes.ChatResponse) bool {
	toolCalls := al.extractToolCalls(finalResp)
	if len(toolCalls) == 0 {
		return false
	}
	agentUseContext.OnSubAgentComplete = func(content core_types.SubAgentContent) {
		al.agentContext.AppendSubAgentEntry(content)
		// 发送子 agent 用量到 onProgress 链路，实时更新 tracker
		if agentUseContext.CurrentContext != nil {
			agentUseContext.CurrentContext.EmitProgress(core_types.ProgressEvent{Type: core_types.ProgressTypeSubAgentComplete, Data: content})
		}
	}

	// 预分配固定长度 slice：多协程并行时各写自己的 index，互不竞争。
	toolsDetail := make([]*core_types.ToolRuntimeDetail, len(toolCalls))
	for i, tool := range toolCalls {
		toolsDetail[i] = &core_types.ToolRuntimeDetail{
			ID:   tool.ID,
			Tool: tool,
		}
	}

	toolRuntimeContext := &core_types.ToolRuntimeContext{
		Ctx:             al.ctx,
		AgentUseContext: agentUseContext,
		FinalResp:       finalResp,
		ToolsDetail:     toolsDetail,
	}

	for i, tool := range toolCalls {
		if signal.Ctx().Err() != nil { // 中止预检：必须在 Start 之前
			break
		}
		al.Stat(fmt.Sprintf("Preparing to execute tool: %s", tool.Name))
		al.SendEventMessage(core_types.ToolExecutionStart, tool, nil)

		done := al.runToolItem(signal, i, toolRuntimeContext)
		toolContext := toolRuntimeContext.ToolsDetail[i].ToolContext
		err := toolRuntimeContext.ToolsDetail[i].Err
		if done {
			al.SendEventMessage(core_types.ToolExecutionEnd, toolContext, nil)
		} else {
			al.SendEventMessage(core_types.ToolExecutionEnd, toolContext, err)
		}
	}
	//获取工具执行结果
	toolsResults := toolRuntimeContext.GetToolResults()
	feedbacks := toolRuntimeContext.GetFeedbacks()
	//处理延迟工具发现
	al.agentTool.ProcessToolRef(toolsResults)
	//注入上下文
	agentUseContext.CurrentContext.AddToolResultMessages(toolsResults)
	agentUseContext.CurrentContext.AddUserMessages(feedbacks)
	return true
}

func (al *AgentLoop) runToolItem(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) bool {
	var done bool
	// 1. 工具参数验证
	done = al.validateTool(signal, toolIdx, runtimeContext)
	if !done {
		return false
	}
	// 2. 权限/确认检查（拒绝 → 统一拒绝错误结果）
	done = al.BeforeToolCall(signal, toolIdx, runtimeContext)
	if !done {
		return false
	}
	// 3. 执行工具
	done = al.executeTool(signal, toolIdx, runtimeContext)
	if !done {
		return false
	}
	// 4. 结果构建
	al.executeToolResult(signal, toolIdx, runtimeContext)

	// 5.用户反馈
	al.executeFeedback(signal, toolIdx, runtimeContext)

	// 6.命令回调
	al.executeToolCommands(signal, toolIdx, runtimeContext)

	return true
}

func (al *AgentLoop) handlePermissionAsk(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) core_types.PermissionResponse {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	data := runtimeContext.ToolsDetail[toolIdx].BeforeToolCallResult.Data
	responseCh := make(chan core_types.PermissionResponse)
	al.SendEventMessage(core_types.ToolPermissionRequired, core_types.PermissionRequest{
		ToolName:   tool.Name,
		ToolCallID: tool.ID,
		Args:       tool.Arguments,
		Data:       data,
		ResponseCh: responseCh,
	}, nil)

	var response core_types.PermissionResponse
	select {
	case response = <-responseCh:
	case <-signal.Ctx().Done():
		response = core_types.PermissionResponse{Action: core_types.BeforeToolCallDeny}
	}
	return response
}

func (al *AgentLoop) executeTool(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) bool {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	response := runtimeContext.ToolsDetail[toolIdx].PermissionResponse
	al.Stat(fmt.Sprintf("Executing tool: %s", tool.Name))
	// 批准时面板回传的修改后参数（如 ExitPlanMode 的 plan、AskUserQuestion 的 answers）：
	if response != nil && response.ModifiedArgs != nil {
		tool.Arguments = response.ModifiedArgs
	}
	toolContext, err := al.agentTool.ExecuteTool(signal.Ctx(), tool, runtimeContext.AgentUseContext)
	runtimeContext.ToolsDetail[toolIdx].ToolContext = toolContext
	if err != nil {
		errMsg := err.Error()
		if tc, ok := toolContext.Content.(core_types.TextContent); ok && tc.Text != "" {
			errMsg = tc.Text
		}
		failToolResult(toolIdx, runtimeContext, errMsg)
		return false
	}
	return true
}

func (al *AgentLoop) executeToolResult(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	toolContext := runtimeContext.ToolsDetail[toolIdx].ToolContext
	result := al.agentTool.BuildToolResult(tool, toolContext)
	runtimeContext.ToolsDetail[toolIdx].ToolResult = result
}

func (al *AgentLoop) executeFeedback(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) {
	response := runtimeContext.ToolsDetail[toolIdx].PermissionResponse
	if response != nil && response.Feedback != "" {
		fb := apitypes.NewUserMessage(response.Feedback)
		runtimeContext.ToolsDetail[toolIdx].Feedback = &fb
	}
}

func (al *AgentLoop) executeToolCommands(signal *AgentSignal, toolIdx int, runtimeContext *core_types.ToolRuntimeContext) {
	tool := runtimeContext.ToolsDetail[toolIdx].Tool
	toolContext := runtimeContext.ToolsDetail[toolIdx].ToolContext
	if len(toolContext.Commands) > 0 {
		al.Stat(fmt.Sprintf("Running tool commands: %s", tool.Name))
		al.toolCommand.RunWithCommands(toolContext.Commands)
	}
}

func rejectMessage(feedback string) string {
	if feedback != "" {
		return core_types.RejectMessageWithReasonPrefix + feedback
	}
	return core_types.RejectMessage
}

// newErrorToolResult 创建 IsError=true 的工具结果消息。
func newErrorToolResult(toolCallID, toolName, text string) apitypes.ToolResultMessage {
	return apitypes.ToolResultMessage{
		Role:       apitypes.ToolRole,
		ToolCallId: toolCallID,
		ToolName:   toolName,
		IsError:    true,
		Contents:   []apitypes.ContentBlock{{Type: "text", Text: text}},
	}
}

func (al *AgentLoop) SendEventMessage(eventType core_types.AgentEventType, message interface{}, err error) {
	if al.eventSink != nil {
		al.eventSink.Emit(eventType, message, err)
	}
}

func (al *AgentLoop) extractToolCalls(resp *apitypes.ChatResponse) []apitypes.ToolCall {
	if resp == nil {
		return nil
	}
	return resp.Content.ToolCalls
}

// toolCallback 统一的工具执行回调，处理中间状态和执行结果。
// 由工具在 Execute 过程中调用，用于上报进度和最终结果。
func (al *AgentLoop) toolCallback(toolContext core_types.ToolContext) {
	al.SendEventMessage(core_types.ToolExecutionUpdate, toolContext, nil)
}

func (al *AgentLoop) processAttachments(signal *AgentSignal, agentUseContext *core_types.AgentUseContext) {
	al.Stat("Processing attachment messages...")
	//process attachments
	attachmentMessages := al.agentAttachment.GetAttachmentMessages(agentUseContext)
	userMetaMessages := al.agentAttachment.BuildUserMetaMessages(attachmentMessages)
	agentUseContext.CurrentContext.AddUserMetaMessages(userMetaMessages)
}
