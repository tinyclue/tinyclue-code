package core

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	_ "github.com/tinyclue/tinyclue-code/coding_agent/core/agent_def"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/plan"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/task"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
	"path/filepath"
	"sync"
	"time"
)

type Agent struct {
	ctx              context.Context
	agentId          string
	agentType        core_types.AgentType
	subAgent         bool
	agentLoop        *AgentLoop
	agentSignal      *AgentSignal
	running          bool
	runError         error
	agentTool        *AgentTool
	tools            []core_types.AgentToolApi
	agentDef         *core_types.BaseAgentDefinition
	agentContext     *AgentContext
	agentAttachment  *AgentAttachment
	eventSink        core_types.EventSink
	runtimeCtx       *core_types.AgentRuntimeContext
	planManager      *plan.PlanManager
	agentToolCommand *AgentToolCommand
	taskManager      *task.TaskManager
	taskCleaner      *task.AutoCleaner
	drainQueue       []apitypes.UserMessage
	drainMu          sync.Mutex
}

func NewAgent(ctx context.Context, agentType core_types.AgentType, subAgent bool, agentId string) *Agent {
	runtimeCtx := &core_types.AgentRuntimeContext{
		AgentId:    agentId,
		AgentType:  agentType,
		SubAgent:   subAgent,
		AgentState: core_types.NewAgentState(),
	}

	var agentDef *core_types.BaseAgentDefinition
	for _, def := range core_types.AgentDefinitions {
		if def.AgentType == agentType {
			agentDef = def
			break
		}
	}

	planManager := plan.NewPlanManager(ctx, runtimeCtx)
	agentTool := NewAgentTool(ctx, runtimeCtx)
	agentAttachment := NewAgentAttachment(ctx, runtimeCtx).WithPlanManager(planManager)
	agentContext := NewAgentContext(ctx, runtimeCtx).WithAgentTool(agentTool).WithAgentDef(agentDef)
	// plan mode 状态写盘：Enter/ExitPlan 变更后落一条 PlanEntry 到会话（resume 时恢复）。
	planManager.SetPersistFn(func(ps core_types.PlanState) {
		agentContext.AppendPlanEntry(ps)
	})
	// resume 恢复依赖：Init 时读取会话最近 plan 条目，恢复 plan mode + AppStage。
	planManager.WithRestoreSource(func() (*core_types.PlanEntry, bool) {
		return agentContext.GetLastPlanEntry()
	})
	agentToolCommand := NewAgentToolCommand(ctx, runtimeCtx).WithPlanManager(planManager)
	agentLoop := NewAgentLoop(ctx, runtimeCtx).
		WithAgentTool(agentTool).
		WithAgentAttachment(agentAttachment).
		WithAgentToolCommand(agentToolCommand).
		WithAgentContext(agentContext)
	taskManager := task.NewTaskManager(ctx, runtimeCtx)

	a := &Agent{
		ctx:              ctx,
		agentId:          agentId,
		agentType:        agentType,
		subAgent:         subAgent,
		agentLoop:        agentLoop,
		agentTool:        agentTool,
		agentContext:     agentContext,
		agentAttachment:  agentAttachment,
		runtimeCtx:       runtimeCtx,
		planManager:      planManager,
		agentToolCommand: agentToolCommand,
		agentDef:         agentDef,
		taskManager:      taskManager,
	}

	return a
}

func (agent *Agent) WithEventSink(sink core_types.EventSink) *Agent {
	agent.eventSink = sink
	agent.agentLoop.WithEventSink(sink)
	agent.agentContext.WithEventSink(sink)
	agent.agentTool.WithEventSink(sink)
	agent.taskManager.WithEventSink(sink)
	return agent
}

func (agent *Agent) WithTools(tools []core_types.AgentToolApi) *Agent {
	agent.tools = tools
	return agent
}

// GetAgentTool 返回 Agent 的工具注册表（/mcp 面板运行时增删工具用）。
func (agent *Agent) GetAgentTool() *AgentTool {
	return agent.agentTool
}

func (agent *Agent) Init() *Agent {
	agent.agentTool.InitWithTools(agent.tools, agent.agentDef.DisallowedTools)
	agent.agentContext.Init()
	agent.agentContext.RebuildMessage()
	agent.agentAttachment.Init()
	agent.agentLoop.Init()
	agent.runtimeCtx.SessionId = agent.agentContext.GetSessionId()

	// 会话恢复：若上次在 plan mode 中退出，恢复 PlanState + AppStage（见 PlanManager.Init）。
	agent.planManager.Init()

	tinyclueDir, _ := config.TinyClueDir()
	baseDir := filepath.Join(tinyclueDir, "tasks")
	agent.taskManager.Init(agent.agentId, baseDir, agent.agentContext.GetSessionId())
	agent.taskCleaner = task.NewAutoCleaner(agent.ctx, agent.taskManager, 2*time.Second, 10*time.Second)
	agent.taskCleaner.Start()

	return agent
}

func (agent *Agent) Abort() {
	if agent.agentSignal != nil {
		agent.agentSignal.Abort()
	}
}

// PushDrainQueue 向 drain 队列推送一条用户消息，供 drainAndContinue 消费。
func (agent *Agent) PushDrainQueue(msg apitypes.UserMessage) {
	agent.drainMu.Lock()
	defer agent.drainMu.Unlock()
	agent.drainQueue = append(agent.drainQueue, msg)
}

func (agent *Agent) popDrainQueue() *apitypes.UserMessage {
	agent.drainMu.Lock()
	defer agent.drainMu.Unlock()
	if len(agent.drainQueue) == 0 {
		return nil
	}
	msg := agent.drainQueue[0]
	agent.drainQueue = agent.drainQueue[1:]
	return &msg
}

func (agent *Agent) drainQueueLen() int {
	agent.drainMu.Lock()
	defer agent.drainMu.Unlock()
	return len(agent.drainQueue)
}

func (agent *Agent) hasRuningTasks() bool {
	hasUnfinished := false
	agentId := agent.agentId
	job.JobInstance.ReadAll(func(tasks map[string]*core_types.JobState) {
		for _, t := range tasks {
			if t.SourceID != agentId {
				continue
			}
			if t.Status != core_types.JobStatusCompleted && t.Status != core_types.JobStatusFailed && t.Status != core_types.JobStatusKilled {
				hasUnfinished = true
				return
			}
			if !t.Notified {
				hasUnfinished = true
				return
			}
		}
	})
	return hasUnfinished
}

func (agent *Agent) GetAgentId() string {
	return agent.agentId
}

func (agent *Agent) GetTaskManager() core_types.TaskManagerApi {
	return agent.taskManager
}

func (agent *Agent) GetSession() string {
	return agent.agentContext.GetSessionId()
}
func (agent *Agent) GetAgentContext() *AgentContext {
	return agent.agentContext
}

func (agent *Agent) GetSessionFilePath() string {
	return agent.agentContext.GetSessionFilePath()
}

func (agent *Agent) GetSessionUsage() core_types.SessionUsage {
	return agent.agentContext.GetSessionUsage()
}

// IsPlanMode 报告 agent 是否处于计划模式（AppStage==Plan，EnterPlan 置位、ExitPlan 复位）。
func (agent *Agent) IsPlanMode() bool {
	return agent.runtimeCtx.AgentState.IsPlanMode()
}

func (agent *Agent) GetMessages() []apitypes.Message {
	return agent.agentContext.GetMessages()
}

func (agent *Agent) RunWithMessage(userInputMessage apitypes.UserMessage, onProgress func(core_types.ProgressEvent)) error {
	return agent.runWithLifecycle(userInputMessage, onProgress)
}

func (agent *Agent) runWithLifecycle(userInputMessage apitypes.UserMessage, onProgress func(core_types.ProgressEvent)) (err error) {
	if agent.running {
		return fmt.Errorf("agent is alreday running.")
	}
	agent.running = true

	defer func() {
		if r := recover(); r != nil {
			log.Errorf(agent.ctx, "agent: panic recovered: %v", r)
			err = fmt.Errorf("agent panic: %v", r)
			agent.Abort()
			agent.SendErrorText(err.Error())
			agent.agentLoop.SendEventMessage(core_types.AgentEnd, nil, err)
		}
		agent.running = false
	}()

	if agent.agentContext.CheckUserTokens(userInputMessage) {
		agent.agentLoop.SendEventMessage(core_types.AgentStart, nil, nil)
		agent.SendErrorText("Input message is too long, please modify it.")
		agent.agentLoop.SendEventMessage(core_types.AgentEnd, nil, nil)
		return fmt.Errorf("Input message is too long.")
	}

	agent.agentSignal = NewAgentSignal(agent.ctx)

	agent.agentContext.CompactIfNeedBefore(userInputMessage)

	currentContext := core_types.NewCurrentContext(agent.agentContext.GetMessages()).WithOnAppend(func(message core_types.AgentMessage) {
		agent.agentContext.AppendAgentMessage(message)
	}).WithOnProgress(func(event core_types.ProgressEvent) {
		onProgress(event)
	})

	// AppStage 变化通知接到当轮 CurrentContext：EnterPlan/ExitPlan 在工具执行中经
	// AgentState.SetAppStage 切换阶段，此时本轮的 progress 链路（onProgress）已就绪，
	// 事件直达 footer 刷新。
	agent.runtimeCtx.AgentState.SetOnStageChanged(func(_ core_types.AppStage) {
		currentContext.EmitProgress(core_types.ProgressEvent{Type: core_types.ProgressTypePlanModeChanged})
	})

	currentContext.AddMessage(userInputMessage)
	agent.agentLoop.SendEventMessage(core_types.UserMessage, userInputMessage, nil)

	agentUseContext := &core_types.AgentUseContext{
		AgentId:        agent.agentId,
		SessionId:      agent.agentContext.GetSessionId(),
		SubAgent:       agent.subAgent,
		PlanState:      agent.planManager.GetPlanState(),
		TaskManagerApi: agent.taskManager,
		AgentDef:       agent.agentDef,
		CurrentContext: currentContext,
		StaticOverhead: agent.agentContext.GetStaticOverhead(),
	}

	agent.agentLoop.SendEventMessage(core_types.AgentStart, nil, nil)
	agent.agentToolCommand.AgentStart()

	// 新一轮开始前先排空队列：上一轮中断期间入队的任务终态通知在此随用户消息一起带给模型。
	agent.FlushDrainQueueAtStart(agentUseContext)

	var loopErr error
	for {
		agentUseContext.CurrentContext.Stat("Starting dialog loop...")
		loopErr = agent.agentLoop.RunAgentLoop(agent.agentSignal, agentUseContext)
		if loopErr != nil {
			break
		}
		if !agentUseContext.CurrentContext.LastEntryIsAssistant() {
			break
		}
		finalResp := currentContext.LastAssistantEntry()
		if !finalResp.Error.IsSuccess {
			break
		}
		if !agentUseContext.SubAgent {
			agentUseContext.CurrentContext.Stat("Waiting for subtask completion...")
		}
		ret := agent.HandleDrainQueue(agentUseContext)
		if !ret {
			break
		}
	}
	agentUseContext.CurrentContext.Stat("Ending dialog loop...")
	agent.showErrorUI(agentUseContext.CurrentContext)
	agent.agentLoop.SendEventMessage(core_types.AgentEnd, nil, loopErr)
	agent.agentContext.RebuildMessage()
	return
}

// FlushDrainQueueAtStart 对话轮开始前把积压的队列消息排空进当前上下文。
// 对齐参考项目"每轮开头先排空队列"的语义：上一轮中断（循环已退出、HandleDrainQueue
// 不再被调用）期间入队的任务终态通知，不因中断丢失，随新一轮用户消息一起带给模型。
func (agent *Agent) FlushDrainQueueAtStart(agentUseContext *core_types.AgentUseContext) {
	if agentUseContext.SubAgent {
		return
	}
	for {
		msg := agent.popDrainQueue()
		if msg == nil {
			return
		}
		agentUseContext.CurrentContext.AddMessage(*msg)
	}
}

func (agent *Agent) HandleDrainQueue(agentUseContext *core_types.AgentUseContext) bool {
	if agentUseContext.SubAgent {
		return false
	}
	for {
		if agent.agentSignal != nil && agent.agentSignal.Ctx().Err() != nil {
			return false
		}
		msg := agent.popDrainQueue()
		if msg != nil {
			agentUseContext.CurrentContext.AddMessage(*msg)
			return true
		}
		if agent.hasRuningTasks() || agent.drainQueueLen() > 0 {
			agent.agentLoop.SendEventMessage(core_types.WaitJob, nil, nil)
			time.Sleep(1000 * time.Millisecond)
			continue
		} else {
			return false
		}
	}
}

func (agent *Agent) showErrorUI(currentContext *core_types.CurrentContext) {
	if !currentContext.LastEntryIsAssistant() {
		return
	}
	finalResp := currentContext.LastAssistantEntry()
	if finalResp.Error.IsSuccess {
		return
	}

	text := ""
	if finalResp.Content.StopReason.FinishReason == apitypes.FinishReasonAborted {
		text = "The operation was cancelled."
	} else if finalResp.Content.StopReason.FinishReason == apitypes.FinishReasonError {
		text = fmt.Sprintf("Something went wrong: %s (code %d). Please try again later.",
			finalResp.Error.ErrorMessage, finalResp.Error.Code)
	}
	agent.SendErrorText(text)
}

func (agent *Agent) SendErrorText(text string) {
	if text != "" {
		agent.eventSink.Emit(core_types.AssistantTextStart, nil, nil)
		agent.eventSink.Emit(core_types.AssistantTextUpdate, text, fmt.Errorf("%s", text))
		agent.eventSink.Emit(core_types.AssistantTextEnd, nil, nil)
	}
}
