// Package codingagent 提供 TUI 交互式会话的 Agent 初始化、事件订阅和用户输入处理。
package codingagent

import (
	"context"
	"fmt"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/mcp"
	"math"
	"sync"
	"time"
)

type AgentInteractive struct {
	ctx                   context.Context
	agent                 *agent_core.Agent
	tracker               *core_types.AgentTracker
	trackerMu             sync.RWMutex // 保护 tracker：写走 agent goroutine，读走 TUI/MCP 后台 goroutine
	interactiveOnProgress func(tracker core_types.AgentTracker)
}

func NewAgentInteractive(ctx context.Context) *AgentInteractive {
	job.JobInstance.EventSink = TuiEventSink
	// 内置工具；MCP 桥接工具由 Init() 异步连接后经 ServerConnected 事件追加（不阻塞启动）。
	tools := BuildTools()
	agentId := utils.CreateAgentId("")
	agent := agent_core.NewAgent(ctx, core_types.GENERAL, false, agentId).
		WithTools(tools).
		WithEventSink(TuiEventSink)
	instance := &AgentInteractive{
		ctx:   ctx,
		agent: agent,
	}
	agent_core.AMInstance.Register(agent)
	return instance
}

func (ai *AgentInteractive) Init() {
	ai.agent.Init()
	sessionUsage := ai.agent.GetSessionUsage()
	ai.tracker = &core_types.AgentTracker{
		ToolUseCount:   0,
		Input:          sessionUsage.Input,
		Output:         sessionUsage.Output,
		CacheRead:      sessionUsage.CacheRead,
		CacheWrite:     sessionUsage.CacheWrite,
		TotalTokens:    sessionUsage.TotalTokens,
		CostTotal:      sessionUsage.CostTotal,
		ContextUsage:   sessionUsage.ContextUsage,
		ContextWindow:  sessionUsage.ContextWindow,
		ContextPercent: sessionUsage.ContextPercent,
	}

	// MCP 后台连接：挂起 server 不阻塞 TUI 启动；连接结果经 onMcpEvent 按事件类型同步工具。
	// 事件回调必须放在 agent.Init()（InitWithTools）之后注册，否则快连接会先于初始化把工具加进
	// 注册表，随后被 InitWithTools 的整体覆盖丢回去。
	mcp.MInstance.SetEvents(ai.onMcpEvent)
	go mcp.MInstance.Init(ai.ctx)
}

func (ai *AgentInteractive) SetInteractiveOnProgress(fn func(tracker core_types.AgentTracker)) {
	ai.interactiveOnProgress = fn
}

// GetTracker 返回 tracker 快照（供启动时一次性渲染 footer，避免暴露内部状态给其他 goroutine）。
func (ai *AgentInteractive) GetTracker() core_types.AgentTracker {
	ai.trackerMu.RLock()
	defer ai.trackerMu.RUnlock()
	return *ai.tracker
}

// GetConversationPath 返回已加载会话的对话路径（resume 时把历史重放回 TUI）。
func (ai *AgentInteractive) GetConversationPath() []core_types.SessionEntry {
	return ai.agent.GetAgentContext().GetConversationPath()
}

// GetAgentId 返回主 agent 的 id（resume 时任务面板恢复用）。
func (ai *AgentInteractive) GetAgentId() string {
	return ai.agent.GetAgentId()
}

// IsPlanMode 报告主 agent 是否处于计划模式（footer 计划模式标记用）。
func (ai *AgentInteractive) IsPlanMode() bool {
	return ai.agent.IsPlanMode()
}

// onAgentProgress 是主 agent 的进度事件处理器：按事件类型更新 tracker 并回传 UI。
// ai.tracker 仅在 Init 初始化一次，主 agent 恒用它（与旧实现闭包捕获等价）。
// 注意：AssistantUsage/SubAgentComplete 走 agent goroutine，McpUpdated 走 TUI/MCP 后台 goroutine，
// 写操作全部在 trackerMu 下进行，UI 回调只收快照（见 pushProgress）。
func (ai *AgentInteractive) onAgentProgress(event core_types.ProgressEvent) {
	switch event.Type {
	case core_types.ProgressTypeAssistantUsage:
		if msg, ok := event.Data.(apitypes.AssistantMessage); ok {
			ai.trackerMu.Lock()
			ai.tracker.ToolUseCount += len(msg.ToolCalls)
			ai.tracker.Input += msg.Usage.Input
			ai.tracker.Output += msg.Usage.Output
			ai.tracker.CacheRead += msg.Usage.CacheRead
			ai.tracker.CacheWrite += msg.Usage.CacheWrite
			ai.tracker.TotalTokens += msg.Usage.TotalTokens
			ai.tracker.CostTotal += msg.Usage.Cost.Total

			ai.tracker.ContextUsage = msg.Usage.TotalTokens
			pct := float64(ai.tracker.ContextUsage) / float64(ai.tracker.ContextWindow) * 100
			ai.tracker.ContextPercent = math.Round(pct*100) / 100
			ai.trackerMu.Unlock()
			ai.pushProgress()
		}

	case core_types.ProgressTypeSubAgentComplete:
		if content, ok := event.Data.(core_types.SubAgentContent); ok {
			usage := content.SessionUsage
			ai.trackerMu.Lock()
			ai.tracker.ToolUseCount += content.TotalToolUseCount
			ai.tracker.Input += usage.Input
			ai.tracker.Output += usage.Output
			ai.tracker.CacheRead += usage.CacheRead
			ai.tracker.CacheWrite += usage.CacheWrite
			ai.tracker.TotalTokens += usage.TotalTokens
			ai.tracker.CostTotal += usage.CostTotal
			ai.trackerMu.Unlock()
			ai.pushProgress()
		}

	case core_types.ProgressTypeMcpUpdated:
		// MCP 状态变化：仅刷新 UI（footer 的 MCP 信息由 buildFooterData 现读 Summary()），不改用量。
		ai.pushProgress()

	case core_types.ProgressTypePlanModeChanged:
		// EnterPlan/ExitPlan 工具执行完成：仅刷新 UI（footer 的 plan mode 标记由 buildFooterData 现读 IsPlanMode()）。
		ai.pushProgress()

	case core_types.ProgressTypeSummary:
		if m, ok := event.Data.(core_types.ProgressSummaryMessage); ok {
			TuiEventSink(core_types.AgentStat, m, nil)
		}
	}
}

// pushProgress 把 tracker 快照交给 UI 回调。可能在 agent / TUI / MCP 后台三种 goroutine 触发，
// nil 判空兼容异步 MCP init 早于 SetInteractiveOnProgress 注册（interactive.Init 的时间窗）。
func (ai *AgentInteractive) pushProgress() {
	if ai.interactiveOnProgress == nil {
		return
	}
	ai.trackerMu.RLock()
	snap := *ai.tracker
	ai.trackerMu.RUnlock()
	ai.interactiveOnProgress(snap)
}

// NotifyMcpUpdated 供 /mcp 面板动作后触发 footer 刷新，走统一进度回调链。
func (ai *AgentInteractive) NotifyMcpUpdated() {
	ai.onAgentProgress(core_types.ProgressEvent{Type: core_types.ProgressTypeMcpUpdated})
}

// onMcpEvent 处理 MCP 事件（eventType + data），按事件类型对齐 agent 工具注册表 / 发提示：
//   - McpConnected → 注册真实桥接工具（启动/自动重连后替换授权伪工具）+ 回显 "connected"；
//   - McpNeedsAuth → 替换为授权伪工具（模型可感知该 server 需要授权并主动发起）+ 回显
//     "requires authorization — run /mcp to authenticate"；
//   - McpAuthStarted → 交互授权拿到授权 URL（浏览器即将打开），提示 "authorizing"；
//   - McpFailed → 回显 connect failed。
//
// Error/Disabled 不发事件（无工具，由调用方动作显式同步）。
func (ai *AgentInteractive) onMcpEvent(et mcp.McpEventType, d mcp.McpEventContext) {
	switch et {
	case mcp.McpConnected:
		ai.agent.GetAgentTool().SyncServerTools(d.Name, d.Tools)
		ai.NotifyMcpUpdated()
		TuiEventSink(core_types.AutoCompleteDetail, fmt.Sprintf("MCP server %q connected (%d tools)", d.Name, len(d.Tools)), nil)
	case mcp.McpNeedsAuth:
		ai.agent.GetAgentTool().SyncServerTools(d.Name, []core_types.AgentToolApi{core_tools.NewMcpAuthTool(d.Name, ai.authenticateAsync)})
		ai.NotifyMcpUpdated()
		TuiEventSink(core_types.AutoCompleteDetail, fmt.Sprintf("MCP server %q requires authorization — run /mcp to authenticate", d.Name), nil)
	case mcp.McpAuthStarted:
		TuiEventSink(core_types.AutoCompleteDetail, fmt.Sprintf("MCP server %q authorizing — browser opened: %s", d.Name, d.URL), nil)
	case mcp.McpFailed:
		ai.NotifyMcpUpdated()
		TuiEventSink(core_types.AutoCompleteDetail, fmt.Sprintf("MCP server %q connect failed: %v", d.Name, d.Err), nil)
	}
}

// authenticateAsync 异步发起 OAuth 授权并替换工具：立即返回 nil（浏览器即将自动打开），
// 授权结果经 TUI 事件提示 + 工具注册表更新（成功则真实工具替换伪工具）。
func (ai *AgentInteractive) authenticateAsync(server string) error {
	go func() {
		tools, err := mcp.MInstance.Authenticate(server)
		if err != nil {
			TuiEventSink(core_types.AutoCompleteDetail, fmt.Sprintf("MCP server %q authorization failed: %v", server, err), nil)
			ai.NotifyMcpUpdated()
			return
		}
		ai.agent.GetAgentTool().SyncServerTools(server, tools)
		ai.NotifyMcpUpdated()
	}()
	return nil
}

// ApplyMcpAction 执行 MCP server 管理动作（重试/启用/禁用）并同步 agent 工具注册表，返回执行错误。
// MInstance 的操作与工具注册都在这里完成，结果文案由调用方组装。
func (ai *AgentInteractive) ApplyMcpAction(server string, action mcp.Action) error {
	at := ai.agent.GetAgentTool()
	switch action {
	case mcp.ActionReconnect:
		tools, err := mcp.MInstance.Reconnect(server)
		if err != nil {
			return err
		}
		at.SyncServerTools(server, tools)
		return nil
	case mcp.ActionEnable:
		tools, err := mcp.MInstance.Enable(server)
		if err != nil {
			return err
		}
		at.SyncServerTools(server, tools)
		return nil
	case mcp.ActionDisable:
		mcp.MInstance.Disable(server)
		at.SyncServerTools(server, nil)
		return nil
	case mcp.ActionAuthenticate:
		// 交互式 OAuth 授权（等价交互式重连）：成功后按 server 同步工具注册表。
		tools, err := mcp.MInstance.Authenticate(server)
		if err != nil {
			return err
		}
		at.SyncServerTools(server, tools)
		return nil
	case mcp.ActionCancelAuth:
		// 中止进行中交互授权：取消后连接回 needs-auth，McpNeedsAuth 事件会注册授权伪工具，
		// 这里不同步注册表（也不清空——被中止的授权仍处于 needs-auth，伪工具应保留）。
		return mcp.MInstance.CancelAuth(server)
	case mcp.ActionClearAuth:
		// 清 token 后连接落回 needs-auth，McpNeedsAuth 事件会替换注册表为授权伪工具，
		// 这里不同步（避免与事件竞态覆盖）。
		return mcp.MInstance.ClearAuthentication(server)
	case mcp.ActionReauth:
		tools, err := mcp.MInstance.Reauthenticate(server)
		if err != nil {
			return err
		}
		at.SyncServerTools(server, tools)
		return nil
	}
	return fmt.Errorf("unknown MCP action: %v", action)
}

func TuiEventSink(eventType core_types.AgentEventType, message interface{}, err error) {
	agent_core.Publish(agent_core.AgentEventQueue, core_types.NewEventMessage(eventType, message, err))
}

func BuildTools() []core_types.AgentToolApi {
	runner := &agent_core.AgentRunner{}
	tools := []core_types.AgentToolApi{
		core_tools.NewBashTool(),
		core_tools.NewEditTool(),
		core_tools.NewReadTool(),
		core_tools.NewWriteTool(),
		core_tools.NewGlob(),
		core_tools.NewGrep(),
		core_tools.NewToolSearch(),
		core_tools.NewEnterPlanMode(),
		core_tools.NewExitPlanMode(),
		core_tools.NewWebSearch(),
		core_tools.NewAskUserQuestionTool(),
		core_tools.NewTaskCreate(),
		core_tools.NewTaskGet(),
		core_tools.NewTaskList(),
		core_tools.NewTaskUpdate(),
		core_tools.NewRunAgentTool(runner),
	}
	// MCP 桥接工具不在此处追加：MInstance 此刻尚未 Init（Tools() 恒为空），
	// 全部由 Init() 异步连接后的 ServerConnected 事件 AddTools 注册。
	return tools
}

func (ai *AgentInteractive) Run() {
	go func() {
		cancel := agent_core.Subscribe(agent_core.UserInputQueue, func(msg core_types.EventMessage) {
			userInputMessage, ok := msg.Message.(apitypes.UserMessage)
			if !ok {
				return
			}
			switch msg.EventType {
			case core_types.UserInput:
				ai.agent.RunWithMessage(userInputMessage, ai.onAgentProgress)
			}
		})
		defer cancel()
		<-ai.ctx.Done()
	}()

	go func() {
		cancel := agent_core.Subscribe(agent_core.AgentNotificationQueue, func(msg core_types.EventMessage) {
			if userMsg, ok := msg.Message.(apitypes.UserMessage); ok {
				ai.agent.PushDrainQueue(userMsg)
			}
		})
		defer cancel()
		<-ai.ctx.Done()
	}()
}

func (ai *AgentInteractive) Abort() {
	agent_core.AMInstance.AbortAll()
}

// Close 关闭 MCP server 子进程（应用退出时调用）。
// MInstance 的生命周期（Init/Tools/Close）统一收在 agentInteractive。
func (ai *AgentInteractive) Close() {
	mcp.MInstance.Close()
}

// McpSummary 返回 MCP server 状态摘要（供 footer / /mcp 面板展示）。
func (ai *AgentInteractive) McpSummary() []mcp.ServerSummary {
	return mcp.MInstance.Summary()
}

// McpHasToken 报告 server 是否已有可复用 token（/mcp 菜单惰性查询，磁盘检查）。
func (ai *AgentInteractive) McpHasToken(server string) bool {
	return mcp.MInstance.HasSavedToken(server)
}

// McpServerTools 返回 server 当前暴露的工具集（/mcp 菜单 View tools 下钻数据）。
func (ai *AgentInteractive) McpServerTools(server string) ([]core_types.AgentToolApi, error) {
	return mcp.MInstance.ServerTools(server)
}

func (ai *AgentInteractive) ProcessUserInput(text string) {
	userPromptMessage := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      text,
		CreatedAt: time.Now(),
	}
	agent_core.Publish(agent_core.UserInputQueue, core_types.NewEventMessage(core_types.UserInput, userPromptMessage, nil))
}
