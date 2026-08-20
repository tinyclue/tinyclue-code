// Package codingagent 提供 TUI 交互式会话的初始化和启动逻辑。
package codingagent

import (
	"context"
	"fmt"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/mcp"
	"github.com/tinyclue/tinyclue-code/tui"
	"github.com/tinyclue/tinyclue-code/tui/component/autocomplete"
	"github.com/tinyclue/tinyclue-code/tui/component/editor"
	"github.com/tinyclue/tinyclue-code/tui/component/footer"
	"github.com/tinyclue/tinyclue-code/tui/component/spacer"
	"github.com/tinyclue/tinyclue-code/tui/component/welcome"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"path/filepath"
	"sync"
	"time"
)

// Interactive 管理 TUI 交互式会话的生命周期。
type Interactive struct {
	ctx    context.Context
	cancel context.CancelFunc
	tui    *tui.ModelV2

	tuiEvent *agent_core.TuiEvent

	tokenCounter *agent_core.TokenCounter

	defaultEditor       *editor.Editor             // 直接引用，用于路由键盘事件和光标定位
	defaultAutoComplete *autocomplete.AutoComplete // nil 时表示未打开
	agentInteractive    *AgentInteractive
	defaultFooter       *footer.FooterComponent

	// permissionQueue 权限请求队列，支持多个 agent 同时弹窗。
	// 入队来自 TuiEvent 订阅协程（handlePermissionDialog），出队来自 UI 事件循环协程
	// （closePermissionDialog），跨协程访问需持 permissionMu。
	permissionQueue []PermissionQueueItem
	permissionMu    sync.Mutex
}

// New 创建一个新的 Interactive 实例。
func New() *Interactive {
	return &Interactive{}
}

// Init 初始化 TUI 所需的全部资源：editor 队列、context、模型和事件循环程序。
func (ia *Interactive) Init() {
	ctx, cancel := context.WithCancel(context.Background())
	ia.ctx = ctx
	ia.cancel = cancel
	tuiInstance := tui.NewV2(ctx).WithCancel(cancel)
	tuiInstance.SetOnInterrupt(ia.OnInterrupt)

	// 预初始化 tiktoken，避免首次消息处理时的下载延迟。
	go utils.InitTiktoken()

	defaultEditor := editor.New()
	defaultEditor.SetOnSubmit(ia.OnSubmit)
	tinyclueDir, _ := config.TinyClueDir()
	defaultEditor.SetHistoryStore(editor.NewHistoryStore(filepath.Join(tinyclueDir, "history.jsonl"), config.CLI.Cwd))
	ia.defaultEditor = defaultEditor

	defaultAutoComplete := autocomplete.New().WithEditor(defaultEditor)
	ia.defaultAutoComplete = defaultAutoComplete

	headerContainer := core.NewContainer(core.HeaderContainerType)
	welcomeComp := welcome.New()
	welcomeComp.SetShortCwd(config.ShortCwd(config.CLI.Cwd))
	headerContainer.AddChild(welcomeComp)
	headerContainer.AddChild(spacer.New(2))

	chatContainer := core.NewContainer(core.ChatContainerType)

	statusContainer := core.NewContainer(core.StatusContainerType)

	thinkingContainer := core.NewContainer(core.ThinkingContainerType)

	editorContainer := core.NewContainer(core.EditorContainerType)
	editorContainer.AddChild(defaultEditor)
	editorContainer.AddChild(defaultAutoComplete)

	configPanelContainer := core.NewContainer(core.ConfigPanelContainerType)

	taskPanelContainer := core.NewContainer(core.TaskPanelContainerType)
	taskPanelContainer.SetHidden(true)

	permissionPanelContainer := core.NewContainer(core.PermissionPanelContainerType)
	permissionPanelContainer.SetHidden(true)

	footerComp := footer.New()
	footerComp.SetRequestRender(func() {
		tuiInstance.RequestRender()
	})
	ia.defaultFooter = footerComp
	footerContainer := core.NewContainer(core.FooterContainerType)
	footerContainer.AddChild(footerComp)

	tuiInstance.AddContainer(headerContainer)
	tuiInstance.AddContainer(chatContainer)
	tuiInstance.AddContainer(thinkingContainer)
	tuiInstance.AddContainer(taskPanelContainer)
	tuiInstance.AddContainer(permissionPanelContainer)
	tuiInstance.AddContainer(statusContainer)
	tuiInstance.AddContainer(editorContainer)
	tuiInstance.AddContainer(configPanelContainer)
	tuiInstance.AddContainer(footerContainer)
	tuiInstance.SetFocus(ia.defaultEditor)

	ia.tui = tuiInstance

	tuiEventInstance := agent_core.NewTuiEvent(ctx).WithTui(tuiInstance)
	ia.tuiEvent = tuiEventInstance
	ia.tuiEvent.SetOnPermissionHandler(func(req core_types.PermissionRequest) {
		ia.handlePermissionDialog(req)
	})

	// session 级流式 token 计数器：订阅 AgentEventQueue 独立做业务计数，
	// 通过回调把估算值喂给状态行渲染（token 计数与渲染解耦）。
	tokenCounter := agent_core.NewTokenCounter()
	ia.tokenCounter = tokenCounter
	tokenCounter.RegisterCallback(tuiEventInstance.OnTokenEstimate)

	agentInstance := NewAgentInteractive(ctx)
	agentInstance.Init()
	agentInstance.SetInteractiveOnProgress(ia.InteractiveOnProcess)
	ia.agentInteractive = agentInstance
	footerData := ia.buildFooterData(agentInstance.GetTracker())
	ia.defaultFooter.SetData(footerData)

	// 会话恢复：resume（-c）加载了历史时，把会话条目重放成 chat 组件 + 恢复任务面板。
	// 此刻容器已建好、agentInstance.Init() 已加载 session；tui.Run 在 Start()，重放先于首帧。
	if entries := ia.agentInteractive.GetConversationPath(); len(entries) > 0 {
		ia.tuiEvent.ReplaySession(entries)
		ia.tuiEvent.ReplayTasks(ia.agentInteractive.GetAgentId())
	}
}

// Start 启动事件循环并进入主消息循环，阻塞直到上下文取消（如 Ctrl+C）。
func (ia *Interactive) Start() {
	// TUI event loop 跑在独立协程
	ia.tui.Run(ia.ctx, ia.cancel)
	// Agent 事件循环（订阅 UserInputQueue）、TUI 事件监听均在内部启动 goroutine
	ia.agentInteractive.Run()
	ia.tuiEvent.Run()
	ia.tokenCounter.Start()

	for {
		select {
		case <-ia.ctx.Done():
			// 关闭 MCP server 子进程，避免 Ctrl+C 退出后残留。
			ia.agentInteractive.Close()
			// 等 event loop 完全退出，避免终端来不及恢复
			time.Sleep(200 * time.Millisecond)
			return
		}
	}
}

func (ia *Interactive) OnInterrupt() {
	ia.agentInteractive.Abort()
}

func (ia *Interactive) InteractiveOnProcess(tracker core_types.AgentTracker) {
	footerData := ia.buildFooterData(tracker)
	ia.defaultFooter.SetData(footerData)
	ia.tui.RequestRender()
}

// mcpFooterInfo 汇总 MCP server 状态为 footer 文案，如 "MCP 2/3"；无 server 时返回空串。
func (ia *Interactive) mcpFooterInfo() string {
	sums := ia.agentInteractive.McpSummary()
	if len(sums) == 0 {
		return ""
	}
	connected := 0
	for _, s := range sums {
		if s.Status == mcp.StatusConnected {
			connected++
		}
	}
	return fmt.Sprintf("MCP %d/%d", connected, len(sums))
}

func (ia *Interactive) buildFooterData(tracker core_types.AgentTracker) footer.FooterData {
	footerData := footer.FooterData{
		DefaultModel:    config.Cnf.DefaultModel(),
		ReasoningEffort: config.Cnf.ReasoningEffort(),
		McpInfo:         ia.mcpFooterInfo(),
		PlanMode:        ia.agentInteractive.IsPlanMode(),
		Input:           tracker.Input,
		Output:          tracker.Output,
		CacheRead:       tracker.CacheRead,
		CacheWrite:      tracker.CacheWrite,
		TotalTokens:     tracker.TotalTokens,
		CostTotal:       tracker.CostTotal,
		ContextUsage:    tracker.ContextUsage,
		ContextWindow:   tracker.ContextWindow,
		ContextPercent:  tracker.ContextPercent,
	}
	return footerData
}
