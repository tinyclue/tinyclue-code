package core

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/config"
	"strings"
	"time"
)

type AgentRunner struct {
}

func (r *AgentRunner) RunAgent(ctx context.Context, params *core_types.AgentRunnerContext) (core_types.SubAgentContent, error) {
	if params.AgentType == core_types.GENERAL {
		return core_types.SubAgentContent{}, fmt.Errorf("cannot launch sub-agent with GENERAL agent type")
	}
	var workTreeInfo *utils.WorktreeInfo
	agentId := utils.CreateAgentId("")
	if params.Isolation != "" {
		info, err := utils.CreateAgentWorktree(agentId[:8], config.CLI.Cwd)
		if err != nil {
			return core_types.SubAgentContent{}, err
		}
		workTreeInfo = info
	}
	agent := NewAgent(ctx, params.AgentType, true, agentId)
	agent.WithTools(params.Tools).WithEventSink(SubAgentEventSink).Init()
	AMInstance.Register(agent)

	async := params.Background || params.RunInBackground
	taskId := agent.GetAgentId()
	jobData := &core_types.LocalSubAgentJobState{
		AgentId:   agent.GetAgentId(),
		Prompt:    params.Prompt,
		AgentType: params.AgentType,
		ToolUseId: params.ToolUseId,
		IsAsync:   async,
	}
	job.JobInstance.Register(taskId, core_types.LOCAL_AGENT, params.Description, agent.Abort, params.SourceID, jobData)
	job.InitJobOutputAsSymlink(taskId, agent.GetSessionFilePath())

	if async {
		return r.runAsync(agent, taskId, params, workTreeInfo)
	}
	return r.runSync(agent, taskId, params, workTreeInfo)
}

func (r *AgentRunner) runSync(agent *Agent, taskId string, params *core_types.AgentRunnerContext, workTreeInfo *utils.WorktreeInfo) (core_types.SubAgentContent, error) {

	msg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      params.Prompt,
		CreatedAt: time.Now(),
	}
	startTime := time.Now()
	tracker := &core_types.AgentTracker{}
	job.JobInstance.Update(taskId, func(t *core_types.JobState) {
		t.Status = core_types.JobStatusRunning
	})
	runErr := agent.RunWithMessage(msg, subAgentOnProgress(taskId, tracker))
	workTreeResult := CleanupWorktreeIfNeeded(workTreeInfo)
	durationMs := time.Since(startTime).Milliseconds()
	result := r.buildAgentResult(agent, taskId, params, tracker, int(durationMs), false, workTreeResult)
	if params.OnSubAgentComplete != nil {
		params.OnSubAgentComplete(result)
	}
	result = completeAgentJob(taskId, result, tracker, agent, runErr)
	setJobNotified(taskId)
	if runErr != nil {
		return result, runErr
	}
	return result, nil
}

func (r *AgentRunner) runAsync(agent *Agent, taskId string, params *core_types.AgentRunnerContext, workTreeInfo *utils.WorktreeInfo) (core_types.SubAgentContent, error) {
	msg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      params.Prompt,
		CreatedAt: time.Now(),
	}

	go func() {
		tracker := &core_types.AgentTracker{}
		startTime := time.Now()
		job.JobInstance.Update(taskId, func(t *core_types.JobState) {
			t.Status = core_types.JobStatusRunning
		})
		runErr := agent.RunWithMessage(msg, subAgentOnProgress(taskId, tracker))
		workTreeResult := CleanupWorktreeIfNeeded(workTreeInfo)
		durationMs := time.Since(startTime).Milliseconds()
		result := r.buildAgentResult(agent, taskId, params, tracker, int(durationMs), true, workTreeResult)
		if params.OnSubAgentComplete != nil {
			params.OnSubAgentComplete(result)
		}
		result = completeAgentJob(taskId, result, tracker, agent, runErr)
		agentNotification(result, runErr)
		setJobNotified(taskId)
	}()

	return core_types.SubAgentContent{
		Status:         "async_launched",
		AgentId:        agent.GetAgentId(),
		AgentType:      params.AgentType,
		Description:    params.Description,
		Prompt:         params.Prompt,
		IsAsync:        true,
		ToolUseId:      params.ToolUseId,
		OutputFilePath: job.GetJobOutputPath(taskId),
	}, nil
}

func SubAgentEventSink(eventType core_types.AgentEventType, message interface{}, err error) {
	switch eventType {
	case core_types.ToolPermissionRequired:
		// 权限确认走主队列，交给 TuiEvent 弹窗
		Publish(AgentEventQueue, core_types.NewEventMessage(eventType, message, err))
	case core_types.AssistantTextUpdate, core_types.AssistantThinkingUpdate, core_types.ToolExecutionStart:
		// 原样转发子 agent 流式事件到子队列，不在这里预转换字节数——
		// token 计数统一由 TokenCounter 处理，避免计数逻辑分散。
		Publish(SubAgentEventQueue, core_types.NewEventMessage(eventType, message, err))
	}
}

func subAgentOnProgress(taskId string, tracker *core_types.AgentTracker) func(core_types.ProgressEvent) {
	return func(event core_types.ProgressEvent) {
		switch event.Type {
		case core_types.ProgressTypeAssistantUsage:
			if msg, ok := event.Data.(apitypes.AssistantMessage); ok {
				tracker.ToolUseCount += len(msg.ToolCalls)
				tracker.Input += msg.Usage.Input
				tracker.Output += msg.Usage.Output
				tracker.CacheRead += msg.Usage.CacheRead
				tracker.CacheWrite += msg.Usage.CacheWrite
				tracker.TotalTokens += msg.Usage.TotalTokens

				if taskId != "" {
					job.JobInstance.Update(taskId, func(ts *core_types.JobState) {
						if stateData, ok := ts.Data.(*core_types.LocalSubAgentJobState); ok {
							stateData.Progress.ToolUseCount = tracker.ToolUseCount
							stateData.Progress.TokenCount = tracker.TotalTokens
							stateData.Progress.RecentTools = append(stateData.Progress.RecentTools, msg.ToolCalls...)
							if len(stateData.Progress.RecentTools) > 5 {
								stateData.Progress.RecentTools = stateData.Progress.RecentTools[len(stateData.Progress.RecentTools)-5:]
							}
						}
					})
				}
			}

		case core_types.ProgressTypeSummary:
			if m, ok := event.Data.(core_types.ProgressSummaryMessage); ok && taskId != "" {
				job.JobInstance.Update(taskId, func(ts *core_types.JobState) {
					if data, ok := ts.Data.(*core_types.LocalSubAgentJobState); ok {
						if m.Summary != "" {
							data.Progress.LastActivesSummary = m.Summary
						}
					}
				})
			}
		}
	}
}

// buildAgentResult 提取 agent 运行结果并更新作业状态。
func (r *AgentRunner) buildAgentResult(agent *Agent, taskId string, params *core_types.AgentRunnerContext, tracker *core_types.AgentTracker, durationMs int, async bool, workTreeResult []string) core_types.SubAgentContent {
	result := core_types.SubAgentContent{
		Status:            "completed",
		AgentId:           agent.GetAgentId(),
		AgentType:         params.AgentType,
		Description:       params.Description,
		Prompt:            params.Prompt,
		TotalToolUseCount: tracker.ToolUseCount,
		TotalDurationMs:   durationMs,
		TotalTokens:       tracker.TotalTokens,
		SessionUsage:      agent.GetSessionUsage(),
		IsAsync:           async,
		Context:           buildContent(taskId, agent),
		OutputFilePath:    job.GetJobOutputPath(taskId),
		ToolUseId:         params.ToolUseId,
	}
	if len(workTreeResult) == 2 {
		result.WorkTreePath = workTreeResult[0]
		result.WorkTreeBranch = workTreeResult[1]
	}
	return result
}

// buildSyncContent 提取 agent 最终输出文本。
func buildContent(taskId string, agent *Agent) []apitypes.ContentBlock {
	messages := agent.GetMessages()
	var lastMsg apitypes.AssistantMessage
	for i := len(messages) - 1; i >= 0; i-- {
		var ok bool
		lastMsg, ok = messages[i].(apitypes.AssistantMessage)
		if ok {
			break
		}
	}
	var blocks []apitypes.ContentBlock
	if lastMsg.TextContent.Text != "" {
		blocks = append(blocks, apitypes.ContentBlock{Type: "text", Text: lastMsg.TextContent.Text})
	}
	return blocks
}

// completeAgentJob 根据 runErr 和 Abort 状态确定作业最终 status 并写入结果。
func completeAgentJob(taskId string, result core_types.SubAgentContent, tracker *core_types.AgentTracker, agent *Agent, runErr error) core_types.SubAgentContent {
	if runErr != nil {
		result.Status = string(core_types.JobStatusFailed)
		result.Error = runErr.Error()
		completeJob(taskId, result, tracker, core_types.JobStatusFailed, runErr.Error())
		return result
	}
	if agent.agentSignal != nil && agent.agentSignal.IsAborted() {
		result.Status = string(core_types.JobStatusKilled)
		result.Error = "[Request interrupted by user for tool use]"
		completeJob(taskId, result, tracker, core_types.JobStatusKilled, "")
		return result
	}
	result.Status = string(core_types.JobStatusCompleted)
	completeJob(taskId, result, tracker, core_types.JobStatusCompleted, "")
	return result
}

// completeJob 将作业标记为指定 status 并写入结果、进度和错误信息。
func completeJob(taskId string, result core_types.SubAgentContent, tracker *core_types.AgentTracker, status core_types.JobStatus, errMsg string) {
	job.JobInstance.Update(taskId, func(t *core_types.JobState) {
		t.Status = status
		t.Error = errMsg
		if data, ok := t.Data.(*core_types.LocalSubAgentJobState); ok {
			data.Result = result
			data.Progress.ToolUseCount = tracker.ToolUseCount
			data.Progress.TokenCount = tracker.TotalTokens
		}
	})
}

func setJobNotified(taskId string) {
	job.JobInstance.Update(taskId, func(t *core_types.JobState) {
		t.Notified = true
	})
}

func agentNotification(subAgentCtx core_types.SubAgentContent, runErr error) {
	taskNotificationXml := BuildTaskNotificationXml(subAgentCtx, runErr)
	userMsg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      taskNotificationXml,
		CreatedAt: time.Now(),
	}
	Publish(AgentNotificationQueue, core_types.NewEventMessage(core_types.TaskNotification, userMsg, nil))
}

func BuildTaskNotificationXml(subAgentCtx core_types.SubAgentContent, runErr error) string {
	summary := BuildSummary(subAgentCtx.Status, subAgentCtx.Description, runErr)
	outputFilePath := subAgentCtx.OutputFilePath
	toolUseIdLine := "\n" + "<tool-use-id>" + subAgentCtx.ToolUseId + "</tool-use-id>"
	resultSection := BuildResultSection(subAgentCtx.Context)
	usageSection := BuildUsageSection(subAgentCtx.TotalTokens, subAgentCtx.TotalToolUseCount, subAgentCtx.TotalDurationMs)
	worktreeSection := BuildWorktreeSection(subAgentCtx.WorkTreePath, subAgentCtx.WorkTreeBranch)
	message := "<task-notification>"
	message += "\n<task-id>" + subAgentCtx.AgentId + "</task-id>"
	message += toolUseIdLine
	message += "\n<output-file>" + outputFilePath + "</output-file>"
	message += "\n<status>" + subAgentCtx.Status + "</status>"
	message += "\n<summary>" + summary + "</summary>"
	message += resultSection
	message += usageSection
	message += worktreeSection
	message += "\n</task-notification>"
	return message
}

func BuildSummary(status string, description string, runErr error) string {
	if status == string(core_types.JobStatusCompleted) {
		return "Agent " + description + " completed"
	}
	if status == string(core_types.JobStatusFailed) {
		return "Agent " + description + " failed: " + runErr.Error()
	}
	return "Agent " + description + " was stopped"
}

// BuildResultSection 将 contentBlocks 中的 text 字段用 "\n" 拼接返回。
func BuildResultSection(contentBlocks []apitypes.ContentBlock) string {
	if len(contentBlocks) == 0 {
		return ""
	}
	var texts []string
	for _, block := range contentBlocks {
		if block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return "\n<result>" + strings.Join(texts, "\n") + "</result>"
}

func BuildUsageSection(totalTokens int, toolUses int, durationMs int) string {
	if totalTokens == 0 {
		return ""
	}
	text := "<usage>"
	text += "<total_tokens>" + fmt.Sprintf("%d", totalTokens) + "</total_tokens>"
	text += "<tool_uses>" + fmt.Sprintf("%d", toolUses) + "</tool_uses>"
	text += "<duration_ms>" + fmt.Sprintf("%d", durationMs) + "</duration_ms>"
	text += "</usage>"
	return "\n" + text
}

func BuildWorktreeSection(worktreePath string, worktreeBranch string) string {
	text := ""
	if worktreePath != "" {
		text += "<worktree>"
		text += "<worktreePath>"
		text += worktreePath
		text += "</worktreePath>"
		if worktreeBranch != "" {
			text += "<worktreeBranch>"
			text += worktreeBranch
			text += "</worktreeBranch>"
		}
		text += "</worktree>"
		text = "\n" + text
	}
	return text

}

func CleanupWorktreeIfNeeded(workTreeInfo *utils.WorktreeInfo) []string {
	if workTreeInfo == nil {
		return nil
	}
	changed := utils.HasWorktreeChanges(workTreeInfo.WorktreePath, workTreeInfo.HeadCommit)
	if !changed {
		utils.RemoveAgentWorktree(workTreeInfo)
	}
	return []string{workTreeInfo.WorktreePath, workTreeInfo.WorktreeBranch}
}
