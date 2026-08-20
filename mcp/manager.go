// manager.go：MCP Manager 编排门面。
//
// 职责范围：server 列表的总体编排——启动/退出生命周期、/mcp 面板用户动作（重连/授权/禁用/
// 启用）、事件转发（McpEventType + McpEventContext）、工具访问与调用路由。
// 连接的具体执行在 server.go（操作 SDK 的"连接执行者"），本文件只编排：
// 实例化 ServerState 并注入事件出口 notify 与工具调用路由 caller（见 instantiate），再驱动它们的 connect。
// 依赖方向单向 manager → server。
//
// 事件流（eventType + data，顺序即流程）：
//
//	启动 / 自动重连（后台，异步）：
//	  连接成功        → McpConnected(Name, Tools)            调用方同步真实工具 + 回显
//	  进入 needs-auth → McpNeedsAuth(Name)                  调用方注册授权伪工具 + 回显
//
//	交互授权（/mcp [a]/[r]，同步返回 + 事件）：
//	  拿到授权 URL → McpAuthStarted(Name, URL)              浏览器已打开提示
//	  成功后同样派发 McpConnected（与返回值同步幂等），调用方按返回值再同步一次
//
//	事件派发统一走 forwardEvent 转发上层回调。任何回调绝不在 st.mu / m.mu 下调用。
//	回调在 Init 之前一次性注册，之后只读（派发直接读 m.events）。
package mcp

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
	"github.com/tinyclue/tinyclue-code/oauth"
)

// Action 是对 MCP server 的管理动作（/mcp 面板触发，映射 Manager 的 Reconnect/Enable/Disable）。
type Action int

const (
	ActionReconnect Action = iota
	ActionDisable
	ActionEnable
	ActionAuthenticate // 对 needs-auth 的 http server 触发交互 OAuth 授权
	ActionCancelAuth   // 中止进行中的交互授权（/mcp busy 时 [c]）
	ActionClearAuth    // 删除已保存 OAuth token（不开浏览器）
	ActionReauth       // 清除 token 后重新交互授权（开浏览器）
)

func (a Action) String() string {
	switch a {
	case ActionReconnect:
		return "reconnect"
	case ActionDisable:
		return "disable"
	case ActionEnable:
		return "enable"
	case ActionAuthenticate:
		return "authenticate"
	case ActionCancelAuth:
		return "cancel"
	case ActionClearAuth:
		return "clear-auth"
	case ActionReauth:
		return "reauth"
	}
	return "unknown"
}

// initConnectBudget 启动时连接所有 server 的总预算；超时后未连上的降级为 Error。
const initConnectBudget = 15 * time.Second

// ServerSummary 供 /mcp 面板与 footer 展示。
type ServerSummary struct {
	Name      string
	Status    ServerStatus
	ToolCount int
	Err       string
	HTTP      bool // 传输类型（true=http/sse，false=stdio），菜单选项按状态+传输动态生成
}

// Manager 管理全部 MCP server 的生命周期与工具桥接。
type Manager struct {
	mu      sync.RWMutex
	servers []*ServerState
	ctx     context.Context
	cancel  context.CancelFunc
	// events 接收连接生命周期事件回调（SetEvents 注册，需在 Init 之前）。
	events func(McpEventType, McpEventContext)
}

// SetEvents 注册连接生命周期事件回调（func(McpEventType, McpEventContext)，nil 安全）。
// 须在 Init 之前调用（生产在 agent 工具表初始化之后、MCP 连接启动之前注册）。回调一经注册
// 不再变更：连接 goroutine / TUI 动作派发事件时直接读 m.events，无并发写（SetEvents 持锁仅为防御）。
func (m *Manager) SetEvents(fn func(McpEventType, McpEventContext)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = fn
}

// forwardEvent 把事件转发给上层回调（SetEvents 注册；nil 安全）。调用在锁外。
// server 只描述"发生了什么"，含义与副作用（注册工具/回显）由订阅方（上层回调）决定。
func (m *Manager) forwardEvent(et McpEventType, d McpEventContext) {
	fn := m.events // 一次性注册、之后只读（见 SetEvents）
	if fn != nil {
		fn(et, d)
	}
}

// MInstance 是全局 MCP 管理器单例（应用启动时 Init，退出时 Close）。
var MInstance = &Manager{}

// NewManager 创建独立 Manager（供测试使用）。
func NewManager() *Manager { return &Manager{} }

// Init 解析配置（用户级 + 项目级 .mcp.json）并并行连接所有启用的 server。
// 生产经 go 异步调用（agentInteractive.Init），测试同步等待；initConnectBudget 仅是同步路径
// 兜底（超时仅记日志，后台连接 goroutine 继续跑，连上后仍会 Connected）。坏 server 记日志 +
// 标记 Error，绝不阻塞/中断启动。
func (m *Manager) Init(ctx context.Context) {
	// 1. 重入保护：清理上一次初始化的会话与上下文，避免重复 Init 泄漏。
	m.reset(ctx)
	// 2. 解析配置（用户级 + 项目级 .mcp.json）；坏配置记日志，绝不中断启动。
	cfg := m.loadConfig()
	// 3. 实例化 server 状态并注入 notify/caller；无启用 server 直接返回。
	if m.instantiate(cfg) == 0 {
		return
	}
	log.Infof(ctx, "mcp: connecting %d servers", len(m.serversSnapshot()))
	// 4. 并行后台连接所有 server（互不阻塞，坏 server 降级为 Error）；全部结束后通知 needs-auth。
	done := m.connectAll(ctx)
	// 5. 等待全部连接结束（或预算超时/ctx 取消）。
	m.waitInit(ctx, done)
}

// reset 重入保护：清理上一次初始化的会话与取消函数（close 不持 m.mu，锁序安全）。
func (m *Manager) reset(ctx context.Context) {
	for _, s := range m.serversSnapshot() {
		s.close()
	}
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.servers = nil
	m.mu.Unlock()
}

// loadConfig 解析 MCP 配置（用户级 + 项目级 .mcp.json）；解析失败仅记日志，返回可用部分。
func (m *Manager) loadConfig() *config.MCPConfig {
	cwd, err := os.Getwd()
	if err != nil {
		log.Warnf(m.ctx, "mcp: get cwd: %v", err)
		cwd = "."
	}
	cfg, cfgErr := config.LoadMCPConfig(cwd)
	if cfgErr != nil {
		log.Warnf(m.ctx, "mcp: %v", cfgErr)
	}
	return cfg
}

// 按配置生成 ServerState 列表并注入事件出口 notify / 调用路由 caller，返回启用的 server 数量。
func (m *Manager) instantiate(cfg *config.MCPConfig) int {
	m.mu.Lock()
	for _, ns := range cfg.EnabledServers() {
		// NewServerState 注入事件出口 notify（直连 Manager.forwardEvent，eventType + data）
		// 与工具调用路由 caller。
		st := NewServerState(ns, m.forwardEvent, m.mcpCaller())
		m.servers = append(m.servers, st)
	}
	count := len(m.servers)
	m.mu.Unlock()
	return count
}

// connectAll 并行对每个 server 发起启动后台连接（interactive=false：http 遇 401 进 needs-auth，
// 绝不阻塞/开浏览器），返回在所有连接结束后关闭的 channel。每个连接结果由 server.go 的
// connect 经 notify 事件派发（McpConnected/McpNeedsAuth/McpFailed 时发，禁用不发）。
func (m *Manager) connectAll(ctx context.Context) <-chan struct{} {
	var wg sync.WaitGroup
	for _, st := range m.serversSnapshot() {
		wg.Add(1)
		go func(st *ServerState) {
			defer wg.Done()
			st.connect(ctx, false)
		}(st)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	return done
}

// waitInit 阻塞到全部启动连接结束（或预算超时/ctx 取消）。initConnectBudget 仅是同步路径兜底。
func (m *Manager) waitInit(ctx context.Context, done <-chan struct{}) {
	select {
	case <-done:
	case <-time.After(initConnectBudget):
		log.Warnf(ctx, "mcp: some servers not connected within budget %s; run /mcp to inspect and retry", initConnectBudget)
	case <-ctx.Done():
	}
}

// ── 工具访问与调用 ──

// Tools 返回所有已连接且未禁用的 server 的桥接工具。
func (m *Manager) Tools() []core_types.AgentToolApi {
	var out []core_types.AgentToolApi
	for _, s := range m.serversSnapshot() {
		out = append(out, s.connectedTools()...)
	}
	return out
}

// CallTool 调用指定 server 的 MCP 工具。manager 只做按名路由（serverByName），
// SDK 调用与连接状态检查归 ServerState.callTool（见 server.go）。
func (m *Manager) CallTool(ctx context.Context, server, tool string, args map[string]any) (toolResult, error) {
	st := m.serverByName(server)
	if st == nil {
		return toolResult{}, fmt.Errorf("MCP server not found: %s", server)
	}
	return st.callTool(ctx, tool, args)
}

// mcpCaller 返回绑定本 Manager 的桥接调用器，供 buildTools 构建的 BridgeTool 注入。
// 绑定实例而非全局 MInstance，使 NewManager 的测试路径可独立验证。
func (m *Manager) mcpCaller() core_tools.MCPToolCaller {
	return func(ctx context.Context, server, tool string, args map[string]any) (string, bool, error) {
		tr, err := m.CallTool(ctx, server, tool, args)
		return tr.Content, tr.IsError, err
	}
}

// Summary 返回所有 server 的展示摘要（按名称排序）。
func (m *Manager) Summary() []ServerSummary {
	servers := m.serversSnapshot()
	out := make([]ServerSummary, 0, len(servers))
	for _, s := range servers {
		out = append(out, s.summary())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ── /mcp 面板用户动作 ──

// reconnectInteractive 对 server 做一次交互式连接尝试并返回其当前暴露的工具集（调用方据此按
// server 同步注册表，增删/禁用归并由 AgentTool 负责，mcp 层不做 diff）。needs-auth 的 http
// server 触发完整 OAuth 授权流程（打开浏览器等回调）。连接结果的事件由 server.go 的 connect 经
// notify 事件派发（与返回值同步幂等），失败返回错误。仅供 Authenticate / Reauth 使用。
//
// 每次交互连接包一层 authCtx（From m.ctx）并把取消函数注入 ServerState：/mcp 面板 busy 时
// 按 [c] 调 CancelAuth 即可中止在途浏览器授权（connect 把取消映射回 needs-auth），本函数
// 返回 ErrAuthCancelled。中止后 server 回 needs-auth，可重新 [a] 授权。
func (m *Manager) reconnectInteractive(name string) ([]core_types.AgentToolApi, error) {
	st := m.serverByName(name)
	if st == nil {
		return nil, fmt.Errorf("MCP server not found: %s", name)
	}
	// 并发交互授权防护：模型工具 + /mcp 面板 [a]/[r] 同时触发只开一个浏览器。CAS 在
	// begin() 之前——被拒的并发连接不 bump gen，不扰动在途授权；非交互自动重连不经此路径。
	if !st.authing.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("MCP server %s authorization already in progress", name)
	}
	defer st.authing.Store(false)

	authCtx, cancel := context.WithCancel(m.ctx)
	st.setAuthCancel(cancel)
	defer st.clearAuthCancel()

	st.connect(authCtx, true)
	// 已连上（用户完成授权）→ 返回工具；被取消 → ErrAuthCancelled；否则交 toolSet 报状态错误。
	if st.isConnected() {
		return st.toolSet()
	}
	if authCtx.Err() == context.Canceled {
		return nil, ErrAuthCancelled
	}
	return st.toolSet()
}

// reconnectNonInteractive 对 server 做一次纯重连（interactive=false，绝不开浏览器）并返回其
// 当前暴露的工具集。手动 Reconnect/Enable 用：直接调 st.connect 真实探活，不注入 authCancel、
// 不做 authing CAS。
func (m *Manager) reconnectNonInteractive(name string) ([]core_types.AgentToolApi, error) {
	st := m.serverByName(name)
	if st == nil {
		return nil, fmt.Errorf("MCP server not found: %s", name)
	}
	st.connect(m.ctx, false)
	return st.toolSet()
}

// CancelAuth 中止 server 进行中的交互授权（/mcp busy 时 [c]）：取消 authCtx，浏览器等待被
// ctx 取消，连接回到 needs-auth。无在途授权时返回错误。
func (m *Manager) CancelAuth(name string) error {
	st := m.serverByName(name)
	if st == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	if !st.cancelAuth() {
		return fmt.Errorf("MCP server %s has no authorization in progress", name)
	}
	return nil
}

// Reconnect 纯重连一个 server（interactive=false，绝不开浏览器），返回其当前暴露的工具集
// （供调用方按 server 同步注册表）。needs-auth 的 http server 请用 Authenticate。
func (m *Manager) Reconnect(name string) ([]core_types.AgentToolApi, error) {
	return m.reconnectNonInteractive(name)
}

// Authenticate 对 needs-auth 的 http server 触发交互 OAuth 授权（交互式重连，授权成功后
// 返回该 server 的工具集供调用方注册）。
func (m *Manager) Authenticate(name string) ([]core_types.AgentToolApi, error) {
	return m.reconnectInteractive(name)
}

// Disable 运行时停用一个 server（仅内存），关闭会话。工具注册表由调用方按 server 清空
// （SyncServerTools(server, nil)），mcp 层不返回工具名。
func (m *Manager) Disable(name string) {
	st := m.serverByName(name)
	if st == nil {
		return
	}
	st.disable()
}

// Enable 运行时重新启用一个 server；若此前未连接则纯重连（非交互，绝不开浏览器），
// 返回该 server 当前暴露的工具集。
func (m *Manager) Enable(name string) ([]core_types.AgentToolApi, error) {
	st := m.serverByName(name)
	if st == nil {
		return nil, fmt.Errorf("MCP server not found: %s", name)
	}
	if !st.enable() {
		return nil, nil
	}
	return m.reconnectNonInteractive(name)
}

// HasSavedToken 报告 server 是否已有可复用 token（/mcp 菜单惰性查询，磁盘检查）。
func (m *Manager) HasSavedToken(name string) bool {
	pa, err := oauth.NewTokenStore("mcp-auth", name).Load()
	return err == nil && pa != nil && pa.Token != nil && pa.Token.AccessToken != ""
}

// ServerTools 返回指定 server 当前暴露的工具集（仅 Connected 时有，否则错误）。
func (m *Manager) ServerTools(name string) ([]core_types.AgentToolApi, error) {
	st := m.serverByName(name)
	if st == nil {
		return nil, fmt.Errorf("MCP server not found: %s", name)
	}
	return st.toolSet()
}

// ClearAuthentication 清 token + 非交互重连落到 needs-auth（不开浏览器）。
// 返回 nil 即成功（落点可能是 needs-auth 或 Error，菜单按真实状态重建展示）。
func (m *Manager) ClearAuthentication(name string) error {
	st := m.serverByName(name)
	if st == nil {
		return fmt.Errorf("MCP server not found: %s", name)
	}
	if err := (oauth.NewTokenStore("mcp-auth", name)).Delete(); err != nil {
		return err
	}
	if st.isConnected() {
		st.connect(m.ctx, false) // 关会话重探活；401 → needsAuth → McpNeedsAuth 事件
	}
	return nil
}

// Reauthenticate 清 token + 交互重连（开浏览器），返回该 server 的工具集供调用方注册。
func (m *Manager) Reauthenticate(name string) ([]core_types.AgentToolApi, error) {
	st := m.serverByName(name)
	if st == nil {
		return nil, fmt.Errorf("MCP server not found: %s", name)
	}
	if err := (oauth.NewTokenStore("mcp-auth", name)).Delete(); err != nil {
		return nil, err
	}
	return m.reconnectInteractive(name)
}

// ── 生命周期与查询 ──

// Close 关闭所有 server 会话并取消管理器上下文（应用退出时调用）。
func (m *Manager) Close() {
	for _, s := range m.serversSnapshot() {
		s.close()
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Manager) serverByName(name string) *ServerState {
	for _, s := range m.serversSnapshot() {
		if s.name() == name {
			return s
		}
	}
	return nil
}

func (m *Manager) serversSnapshot() []*ServerState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*ServerState, len(m.servers))
	copy(out, m.servers)
	return out
}
