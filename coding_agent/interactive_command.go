package codingagent

import (
	"context"
	"errors"
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/mcp"
	"github.com/tinyclue/tinyclue-code/subscription"
	"github.com/tinyclue/tinyclue-code/tui/component/loginpanel"
	"github.com/tinyclue/tinyclue-code/tui/component/mcppanel"
	"github.com/tinyclue/tinyclue-code/tui/component/modelpanel"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"time"
)

// OnSubmit 处理用户提交的文本输入，识别命令或转发给 agent 处理。
func (ia *Interactive) OnSubmit(text string) {
	if text == "/login" {
		ia.handleLogin()
		return
	}
	if text == "/model" {
		ia.handleModel()
		return
	}
	if text == "/mcp" {
		ia.handleMcp()
		return
	}
	if text == "/exit" {
		ia.tui.Send(core.QuitMsg{})
		return
	}
	ia.agentInteractive.ProcessUserInput(text)
}

// ── Panel Framework ──

// showPanel 打开一个面板组件，隐藏 editor/footer，发送命令事件。
// 所有面板组件都应通过此方法打开，以确保行为一致。
func (ia *Interactive) showPanel(cmd string, comp core.Component) {
	userMsg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      cmd,
		CreatedAt: time.Now(),
	}
	agent_core.Publish(agent_core.AgentEventQueue,
		core_types.NewEventMessage(core_types.AutoCompleteMessage, userMsg, nil))

	ia.tui.GetContainer(core.EditorContainerType).SetHidden(true)
	ia.tui.GetContainer(core.FooterContainerType).SetHidden(true)

	configPanelContainer := ia.tui.GetContainer(core.ConfigPanelContainerType)
	configPanelContainer.ClearChildren()
	configPanelContainer.AddChild(comp)

	ia.tui.SetFocus(comp)
	ia.tui.RequestRender()
}

// closePanel 移除当前面板，恢复 editor/footer，并向事件队列发送 detail 消息。
// reason 为取消/成功/失败提示文字，传入空字符串则不发送。
func (ia *Interactive) closePanel(reason string) {
	ia.tui.GetContainer(core.ConfigPanelContainerType).ClearChildren()

	ia.tui.GetContainer(core.EditorContainerType).SetHidden(false)
	ia.defaultEditor.SetText("")

	ia.tui.GetContainer(core.FooterContainerType).SetHidden(false)

	ia.tui.SetFocus(ia.defaultEditor)
	ia.tui.RequestRender()

	if reason != "" {
		agent_core.Publish(agent_core.AgentEventQueue,
			core_types.NewEventMessage(core_types.AutoCompleteDetail, reason, nil))
	}
}

// ── Login Panel ──

func (ia *Interactive) handleLogin() {
	loginComp := loginpanel.New()
	// subCancel 在 OnSubscribe/OnCancelSubscribe 闭包间共享：记录在途浏览器授权，[c]/Esc 取消。
	var subCancel context.CancelFunc
	loginComp.SetOAuthProviders(subscription.ProviderNames)
	loginComp.OnSubmit(func(provider, apiKey string) {
		entry := &config.AuthEntry{
			Type: "api-key",
			Key:  apiKey,
		}
		if err := config.Cnf.SetAuth(provider, entry); err != nil {
			ia.closePanel("Failed to save API key: " + err.Error())
			return
		}
		// 保存后热更新客户端（默认 Client 继续用旧凭据直到重建）。
		if err := api_provider.UpdateFromConfig(); err != nil {
			ia.closePanel("Saved API key for " + provider + " but reload failed: " + err.Error())
			return
		}
		ia.closePanel("Saved API key for " + provider + ". Credentials saved to ~/.tinyclue/config/auth.json")
	})
	loginComp.OnSubscribe(func(provider string) {
		ctx, cancel := context.WithCancel(context.Background())
		subCancel = cancel
		go ia.subscribe(ctx, provider)
	})
	loginComp.OnCancelSubscribe(func(string) {
		if subCancel != nil {
			subCancel()
		}
	})
	loginComp.OnSubscribeDone(func(string) {
		ia.closePanel("")
	})
	loginComp.OnCancel(func() {
		ia.closePanel("Login cancelled")
	})
	ia.showPanel("/login", loginComp)
}

// subscribe 在后台 goroutine 执行订阅登录：登录并落盘 → 写 oauth 标记 + 热更新 →
// 结果文案发布到 chat → 经 OAuthDoneMsg 回传面板（成功/取消关面板，失败停留可选其他厂商）。
// ctx 可取消（[c]/Esc 中止在途浏览器授权）。
func (ia *Interactive) subscribe(ctx context.Context, provider string) {
	err := subscription.LoginAndSave(ctx, provider)
	if err == nil {
		_ = config.Cnf.SetAuth(provider, &config.AuthEntry{Type: "oauth"})
		_ = api_provider.UpdateFromConfig()
	}
	reason := oauthResult(provider, err)
	agent_core.Publish(agent_core.AgentEventQueue,
		core_types.NewEventMessage(core_types.AutoCompleteDetail, reason, nil))
	ia.tui.Send(core.OAuthDoneMsg{Provider: provider, Err: err})
}

// oauthResult 组装订阅登录结果文案（"Signed in to X (subscription)" /
// "Sign in to X canceled" / "Sign in to X failed: ..."）。用户取消渲染为 canceled 而非 failed。
func oauthResult(provider string, err error) string {
	if errors.Is(err, context.Canceled) {
		return fmt.Sprintf("Sign in to %s canceled", provider)
	}
	if err != nil {
		return fmt.Sprintf("Sign in to %s failed: %v", provider, err)
	}
	return fmt.Sprintf("Signed in to %s (subscription)", provider)
}

// ── MCP Panel ──

// handleMcp 打开 MCP server 状态面板，支持重试/启用/禁用。
// 动作在后台 goroutine 执行（connect 不阻塞 TUI 事件循环），完成后经 McpRefreshMsg
// 回到事件循环刷新面板（保持打开，支持连续操作）；结果文案发聊天 detail，Enter/Esc 关闭。
func (ia *Interactive) handleMcp() {
	panelComp := mcppanel.New(ia.agentInteractive.McpSummary())
	// MCP 摘要读取线程安全，可在 TUI 事件循环内安全重取。
	panelComp.SetReload(ia.agentInteractive.McpSummary)
	// 菜单选项按 状态+传输+是否已有 token 动态生成：hasToken 在菜单构建时惰性查询（磁盘检查），
	// tools 加载用于 View tools 下钻。
	panelComp.SetAuthState(ia.agentInteractive.McpHasToken)
	panelComp.SetToolsLoader(ia.agentInteractive.McpServerTools)
	panelComp.OnAction(func(server string, action mcp.Action) {
		go func() {
			err := ia.agentInteractive.ApplyMcpAction(server, action)
			reason := mcpActionResult(server, action, err)
			// 结果文案发布到 chat（不依赖面板存活：操作中退出面板结果也不丢）；
			// McpRefreshMsg 仅作刷新信号（无载荷），面板据此重取数据重绘。
			agent_core.Publish(agent_core.AgentEventQueue,
				core_types.NewEventMessage(core_types.AutoCompleteDetail, reason, nil))
			ia.tui.Send(core.McpRefreshMsg{})
		}()
	})
	// 刷新后走统一进度回调链刷新 footer 的 MCP 状态（结果文案已由 OnAction 发布，不重复）。
	panelComp.OnRefreshed(func() {
		ia.agentInteractive.NotifyMcpUpdated()
	})
	// [c] 取消进行中授权：成功时不发布结果（被中止的 [a]/[r] 会自行报 "canceled"），
	// 仅失败（无在途授权等）才发错误文案，避免一次取消出两条消息。
	panelComp.OnCancelAuth(func(server string) {
		go func() {
			if err := ia.agentInteractive.ApplyMcpAction(server, mcp.ActionCancelAuth); err != nil {
				reason := mcpActionResult(server, mcp.ActionCancelAuth, err)
				agent_core.Publish(agent_core.AgentEventQueue,
					core_types.NewEventMessage(core_types.AutoCompleteDetail, reason, nil))
				ia.tui.Send(core.McpRefreshMsg{})
			}
		}()
	})
	panelComp.OnCancel(func() {
		ia.closePanel("")
	})
	ia.showPanel("/mcp", panelComp)
}

// mcpActionResult 组装 MCP 动作结果文案（"MCP reconnect toy ok" / "MCP reconnect toy failed: ..."）。
// 被用户取消的授权（ErrAuthCancelled）渲染为 "canceled" 而非 "failed"。
func mcpActionResult(server string, action mcp.Action, err error) string {
	if errors.Is(err, mcp.ErrAuthCancelled) {
		return fmt.Sprintf("MCP %s %s canceled", action, server)
	}
	if err != nil {
		return fmt.Sprintf("MCP %s %s failed: %v", action, server, err)
	}
	return fmt.Sprintf("MCP %s %s ok", action, server)
}

// ── Model Panel ──

func (ia *Interactive) handleModel() {
	modelComp := modelpanel.New()
	modelComp.OnSubmit(func(provider, modelID, reasoningEffort string) {
		_ = config.Cnf.SetReasoningEffort(reasoningEffort)
		if err := config.Cnf.SetDefaults(provider, modelID); err != nil {
			ia.closePanel("Failed to save model: " + err.Error())
		} else {
			if err := config.Cnf.Reload(); err != nil {
				ia.closePanel("Model saved but reload failed: " + err.Error())
			} else {
				ia.closePanel("Model set to " + modelID + " (" + provider + ")")
			}
		}
	})
	modelComp.OnCancel(func() {
		ia.closePanel("Model selection cancelled")
	})
	ia.showPanel("/model", modelComp)
}
