// server.go：单个 MCP server 的连接执行者。
//
// 职责范围：一条连接的完整生命周期——协商建会话（操作 SDK）、列工具、构建桥接工具、安装
// 会话、状态迁移、断线守护与自动重连。SDK 经 sdk.go 封装（唯一触碰官方 SDK 的文件是 sdk.go），
// 本文件的 connect / watchDisconnect / reconnectLoop 是"操作 SDK"的编排层。
//
// 对外通知采用事件模式（eventType + data，见 McpEventType / McpEventContext）：连接结果、授权等只作为事件通知出去
// （notify 由 manager 注入），server 不感知事件含义与副作用——订阅方（manager）自行决定
// 转发应用事件等。桥接工具的调用路由 caller 同样由 manager 注入（非事件）。
// server.go 不 import manager.go —— 依赖方向单向 manager → server。
package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
)

// ServerStatus 表示单个 MCP server 的连接状态。
type ServerStatus int

const (
	StatusConnecting ServerStatus = iota
	StatusConnected
	StatusError
	StatusNeedsAuth // http server 需要 OAuth 授权（启动连接遇 401，尚未授权）
	StatusDisabled
)

func (s ServerStatus) String() string {
	switch s {
	case StatusConnecting:
		return "connecting"
	case StatusConnected:
		return "connected"
	case StatusError:
		return "error"
	case StatusNeedsAuth:
		return "needs auth"
	case StatusDisabled:
		return "disabled"
	}
	return "unknown"
}

// errNeedsAuthState 是 StatusNeedsAuth 状态自带的说明错误（区别于 auth.go 的 ErrNeedsAuth 哨兵）。
var errNeedsAuthState = errors.New("requires authorization — run /mcp to authenticate")

// ServerState 单个 MCP server 的运行时状态。
// 注意：字段读写受 mu 保护（/mcp 面板在 TUI goroutine，Agent 在 agent goroutine）。
type ServerState struct {
	named config.NamedServer
	// notify 事件通知出口（manager 注入，见 McpEventType / McpEventContext）：连接结果、授权等
	// 只作为事件通知出去（eventType + data）。
	notify func(McpEventType, McpEventContext)
	// caller 桥接工具调用路由（manager 注入）：工具被调用时经它回到 Manager.CallTool。非事件。
	caller core_tools.MCPToolCaller
	sess   sdkSession
	tools  []*core_tools.BridgeTool
	status ServerStatus
	err    error
	// disabled 运行时禁用（/mcp 面板操作，仅内存，不持久化）。
	disabled bool
	// reconnecting 防并发自动重连（多个 watcher 竞态/重试循环期间只允许一个 reconnecting 循环）。
	reconnecting atomic.Bool
	// authing 防并发交互授权：模型工具 + /mcp 面板 [a]/[r] 同时触发时只开一个浏览器。
	// 与 reconnecting 并列（reconnecting 防并发自动重连循环，authing 防并发交互授权）。
	authing atomic.Bool
	// authCancel 中止进行中交互授权的取消函数（manager.reconnect 包一层 per-auth ctx 后注入，
	// /mcp 面板 busy 时按 [c] 调用 CancelAuth 触发）。受 mu 保护；无在途交互连接时为 nil。
	authCancel context.CancelFunc
	// gen 连接代际计数器：每次 begin() 自增。并发连接（手动 [r]/[a]、自动重连、Init）
	// 只有最新一代能安装会话/改状态，旧代发现 gen 已变则放弃自己的会话（防泄漏、防翻转）。
	gen uint64
	mu  sync.RWMutex
}

// NewServerState 创建单个 MCP server 的连接状态机（manager 实例化时注入事件出口与调用路由）。
// named 是配置与名称，notify 是事件出口（eventType + data，见 McpEventType / McpEventContext），
// caller 是工具调用路由（工具被调用时经它回到 Manager.CallTool）；二者均由 manager 注入。
// 依赖方向单向 manager → server。
func NewServerState(named config.NamedServer, notify func(McpEventType, McpEventContext), caller core_tools.MCPToolCaller) *ServerState {
	return &ServerState{named: named, notify: notify, caller: caller}
}

// ── 连接全流程 ──

// connect 对 server 做一次连接尝试，返回是否被并发的新连接取代：
//   - true  = 本次连接已落地（Connected / Error / NeedsAuth 由 s.status 决定）；
//   - false = 被更新的连接（手动 [r]/[a] 或更新的自动重连）取代，未安装会话，调用方让位。
//
// interactive=false 为启动后台连接 / 自动重连（http 遇 401 标记 StatusNeedsAuth，不阻塞、
// 不开浏览器）；interactive=true 为 /mcp 面板用户触发（http 走完整授权流程）。
//
// 这是纯连接执行：不做任何不触网的策略捷径，永远真实探活。
//
// 流程（阶段 0-4；协商与列工具直接操作 SDK，结果作为事件 notify 出去，不感知含义）：
//
//	阶段 0  gen := begin() 领取代际（关旧会话、置 Connecting、gen++）；
//	阶段 1  connectSession 协商建会话（经 sdk.go；交互授权拿 URL 时 notify McpAuthStarted）；
//	阶段 2  buildTools 列工具并构建桥接工具（注入 caller 路由回 Manager.CallTool）；
//	阶段 3  connected(gen, sess, tools) / failed(gen, err) / needsAuth(gen)：状态迁移；
//	阶段 4  成功 → notify McpConnected(tools)；http server 挂断线守护（被动断线后自动重连）。
func (s *ServerState) connect(ctx context.Context, interactive bool) bool {
	// 阶段 0：领取代际。写新会话前取走并关闭旧会话（防手动/自动重连竞态下的会话泄漏）。
	gen := s.begin()

	// 阶段 1：协商连接（操作 SDK）。交互授权流程在拿到授权 URL 时通知出去（订阅方提示"授权中"）。
	sess, err := connectSession(ctx, s.name(), s.named.Server, interactive, func(_, url string) {
		s.notify(McpAuthStarted, McpEventContext{Name: s.name(), URL: url})
	})
	if err != nil {
		if errors.Is(err, ErrNeedsAuth) {
			// 真实 401 进入 needs-auth（仅当本连接仍是最新代）：置状态并通知。
			return s.enterNeedsAuth(gen)
		}
		// 用户中止进行中交互授权（/mcp [c] 取消 authCtx）：回到 needs-auth 而非 Error——
		// 语义是"尚未授权，用户选择不授权"，而非连接失败。判定用本连接自己的 ctx（authCtx）
		// 是否被取消，不依赖 SDK 对错误的包装；超时（DeadlineExceeded）不命中，仍走 Error。
		if interactive && ctx.Err() == context.Canceled {
			return s.enterNeedsAuth(gen)
		}
		return s.failed(gen, err)
	}

	// 阶段 2：列工具并构建桥接工具（注入 caller，工具被调用时路由回 Manager.CallTool）。
	tools, err := s.buildTools(ctx, sess)
	if err != nil {
		sess.Close()
		return s.failed(gen, err)
	}

	// 阶段 3：仅当仍是最新代且未禁用时安装会话；否则关闭本会话并让位
	//（不泄漏、不装到已禁用的 server、不覆盖更新的连接）。connected 在锁内返回本次安装的
	// 工具快照（s.tools 可能被并发连接改写，勿在锁外再读）。
	installed, ok := s.connected(gen, sess, tools)
	if !ok {
		return false
	}

	// 阶段 4：连接成功 → 通知（订阅方注册真实工具）；http 挂断线守护（被动断线后自动重连）。
	s.notify(McpConnected, McpEventContext{Name: s.name(), Tools: installed})
	if s.isHTTP() {
		go s.watchDisconnect(ctx, sess)
	}
	return true
}

// buildTools 列工具并构建桥接工具，注入 caller（工具调用路由回 Manager.CallTool）。
func (s *ServerState) buildTools(ctx context.Context, sess sdkSession) ([]*core_tools.BridgeTool, error) {
	infos, err := listTools(ctx, sess)
	if err != nil {
		return nil, err
	}
	tools := make([]*core_tools.BridgeTool, 0, len(infos))
	for _, ti := range infos {
		bt := core_tools.NewMCPBridgeTool(s.name(), ti.Name, ti.Description, toParameters(ti.InputSchema))
		bt.SetCaller(s.caller)
		tools = append(tools, bt)
	}
	return tools, nil
}

// ── 状态迁移 ──
//
//	              begin() 领取代际（关旧会话、置 Connecting、gen++）
//	                   │
//	                   ▼
//	            StatusConnecting
//	                   │
//	        connect 阶段 2-3（协商 + 列工具）
//	                   │
//	    ┌──────────────┼───────────────┐
//	    ▼              ▼               ▼
//	 connected(gen)  failed(gen)   needsAuth(gen)
//	    │              │               │
//	    ▼              ▼               ▼
//	 StatusConnected StatusError  StatusNeedsAuth
//
//	Disabled 由 manager 的 Disable 直接置位（从任一状态进入）。
//	connect 只在 begin()/connected()/failed()/needsAuth() 返回 true（本连接仍是最新一代
//	且未禁用）时才执行副作用（装会话、派发事件）；返回 false = 被更新的连接取代，
//	调用方让位，不得执行副作用。

// begin 领取代际：取走并关闭旧会话、置 Connecting、gen++。返回本次连接持有的代际，
// 后续 connected/failed/needsAuth 用它判断本连接是否仍是当前代。
func (s *ServerState) begin() uint64 {
	s.close()
	s.mu.Lock()
	s.status = StatusConnecting
	s.gen++
	gen := s.gen
	s.mu.Unlock()
	return gen
}

// isCurrentLocked 报告本次连接是否仍是最新一代且未禁用（须在持 s.mu 时调用）。
// 代际防翻转（s.gen != gen）是连接状态机的并发不变量：begin() 领取代际后，期间任何新连接
// （手动 [r]/[a]、自动重连、Init）再 begin() 都会 gen++；旧代在 connected/failed/needsAuth
// 各自完成时用本方法判断自己是否已被取代——被取代则让位，不安装会话/不改状态（防泄漏、防翻转）。
// 检查必须与状态写持同一把锁：先检查后写会留 TOCTOU 窗口，新连接可趁隙插入。
func (s *ServerState) isCurrentLocked(gen uint64) bool {
	return s.gen == gen && !s.disabled
}

// connected 仅当仍是最新一代且未禁用时安装会话并置 Connected；否则关闭会话返回 false
// （调用方让位，不得覆盖新连接或装到已禁用的 server）。返回的 tools 快照在锁内构建，
// 供调用方锁外 notify（s.tools 可能被并发连接改写）。
func (s *ServerState) connected(gen uint64, sess sdkSession, tools []*core_tools.BridgeTool) ([]core_types.AgentToolApi, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isCurrentLocked(gen) {
		sess.Close()
		return nil, false
	}
	s.sess = sess
	s.tools = tools
	s.status = StatusConnected
	s.err = nil
	return s.toolsAPI(), true
}

// failed 标记连接失败（置 Error，清会话与工具）；仅当仍是最新一代且未禁用时生效。
// 生效后于锁外派发 McpFailed（供订阅方回显连接失败文案；自动重连的每次失败尝试都会触发，
// 订阅方按需节流）。回调绝不在 st.mu 下调用。
func (s *ServerState) failed(gen uint64, err error) bool {
	s.mu.Lock()
	applied := s.isCurrentLocked(gen)
	if applied {
		s.status = StatusError
		s.err = err
		s.sess = nil
		s.tools = nil
	}
	s.mu.Unlock()
	if applied {
		s.notify(McpFailed, McpEventContext{Name: s.name(), Err: err})
	}
	return applied
}

// 置 StatusNeedsAuth（清会话与工具）；仅当仍是最新一代且未禁用时生效。
func (s *ServerState) needsAuth(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isCurrentLocked(gen) {
		return false
	}
	s.setNeedsAuthStateLocked()
	return true
}

// enterNeedsAuth 本连接仍是最新代时置 needs-auth 并派发 McpNeedsAuth（订阅方注册授权伪工具）；
// 被并发连接取代返回 false（调用方让位）。回调在锁外执行。
func (s *ServerState) enterNeedsAuth(gen uint64) bool {
	if !s.needsAuth(gen) {
		return false
	}
	s.notify(McpNeedsAuth, McpEventContext{Name: s.name()})
	return true
}

// setNeedsAuthStateLocked 把状态置为 needs-auth（须在持 s.mu 时调用）。
func (s *ServerState) setNeedsAuthStateLocked() {
	s.status = StatusNeedsAuth
	s.err = errNeedsAuthState
	s.sess = nil
	s.tools = nil
}

// close 关闭会话并杀掉子进程。锁内只取走 sess，Close（可能阻塞）在锁外执行，避免短暂挡其他读者。
func (s *ServerState) close() {
	s.mu.Lock()
	sess := s.sess
	s.sess = nil
	s.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// disable 置为运行时禁用（/mcp 面板 Disable）并关闭会话。工具注册表由调用方按 server 清空。
func (s *ServerState) disable() {
	s.mu.Lock()
	s.disabled = true
	s.status = StatusDisabled
	s.mu.Unlock()
	s.close()
}

// enable 解除运行时禁用（/mcp 面板 Enable）；返回是否需要重连（此前 Error/Disabled/needs-auth
// 才重连，Connected 直接可用）。工具注册表由调用方按返回值同步。
func (s *ServerState) enable() bool {
	s.mu.Lock()
	s.disabled = false
	needConnect := s.status == StatusError || s.status == StatusDisabled || s.status == StatusNeedsAuth
	s.mu.Unlock()
	return needConnect
}

// ── 中止进行中交互授权 ──

// setAuthCancel 注入本次交互连接的取消函数（manager.reconnect 在 connect 前调用），
// 供 /mcp 面板 [c] 取消在途浏览器授权。
func (s *ServerState) setAuthCancel(cancel context.CancelFunc) {
	s.mu.Lock()
	s.authCancel = cancel
	s.mu.Unlock()
}

// clearAuthCancel 清除取消函数（reconnect 的 defer 调用；connect 返回后无在途授权）。
func (s *ServerState) clearAuthCancel() {
	s.mu.Lock()
	s.authCancel = nil
	s.mu.Unlock()
}

// cancelAuth 中止进行中的交互授权（取消 reconnect 注入的 authCtx → 浏览器等待被 ctx 取消）。
// 返回是否有可取消的在途授权。
func (s *ServerState) cancelAuth() bool {
	s.mu.RLock()
	cancel := s.authCancel
	s.mu.RUnlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// isConnected 是否处于 Connected 且未禁用（reconnect 用它区分"已连上"与"被取消/失败"）。
func (s *ServerState) isConnected() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status == StatusConnected && !s.disabled
}

// ── 断线守护与自动重连（仅 http）──

var (
	maxReconnectAttempts = 5
	initialBackoffMs     = 1 * time.Second
	maxBackoffMs         = 30 * time.Second
)

// watchDisconnect 阻塞在被 watch 的会话上，连接关闭（服务器退出/网络中断）后触发自动重连。
// watched.Wait() 返回后做身份+状态双重检查：会话可能已被 Reconnect/Disable 关闭或替换，
// 旧 watcher 不得对"新会话的死亡"重复重连。
func (s *ServerState) watchDisconnect(ctx context.Context, watched sdkSession) {
	if err := watched.Wait(); err != nil && !errors.Is(err, mcp.ErrConnectionClosed) {
		log.Warnf(ctx, "mcp %s: session closed: %v", s.name(), err)
	}
	s.mu.RLock()
	stillConnected := !s.disabled && s.status == StatusConnected && s.sess == watched
	s.mu.RUnlock()
	if !stillConnected {
		return
	}
	s.reconnectLoop(ctx, watched)
}

// reconnecting CAS 防并发（多个 watcher 竞态）；成功/进入 needs-auth/禁用/ctx 取消即停止。
// watched 是触发本循环的已死会话：退避期间若服务器已被其他连接恢复（s.sess 是不同于它的
// 存活会话），立即停止，不得拆掉别人刚建好的连接。connect 返回 false（被更新的连接取代）
// 时同样让位。连接结果的事件经 connect 内部的 notify 派发，这里不再重复。
func (s *ServerState) reconnectLoop(ctx context.Context, watched sdkSession) {
	if !s.reconnecting.CompareAndSwap(false, true) {
		return
	}
	defer s.reconnecting.Store(false)
	for attempt := 1; attempt <= maxReconnectAttempts; attempt++ {
		if ctx.Err() != nil {
			return
		}
		s.mu.RLock()
		disabled := s.disabled
		sess := s.sess
		s.mu.RUnlock()
		if disabled {
			return
		}
		// 断线期间已被其他连接（手动 [r]/[a] 等）恢复：停止自动重连，不拆存活会话。
		if sess != nil && sess != watched {
			return
		}
		// non-interactive：复用持久化 token + 静默刷新，失败不开浏览器。
		if !s.connect(ctx, false) {
			return // 被更新的连接取代，让位
		}
		s.mu.RLock()
		status := s.status
		s.mu.RUnlock()
		switch status {
		case StatusConnected, StatusNeedsAuth:
			return // connect 已派发 StateChanged，重连结束
		}
		if attempt == maxReconnectAttempts {
			break
		}
		backoff := initialBackoffMs << (attempt - 1)
		if backoff > maxBackoffMs {
			backoff = maxBackoffMs
		}
		t := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}

// ── 只读查询 ──

// toolSet 返回 Connected 状态下该 server 当前暴露的工具快照；否则返回错误。
func (s *ServerState) toolSet() ([]core_types.AgentToolApi, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status != StatusConnected {
		if s.err != nil {
			return nil, s.err
		}
		return nil, fmt.Errorf("MCP server %s connect failed", s.name())
	}
	return s.toolsAPI(), nil
}

// toolsAPI 把桥接工具转成 AgentToolApi 切片（须在持 s.mu 读锁时调用）。
func (s *ServerState) toolsAPI() []core_types.AgentToolApi {
	tools := make([]core_types.AgentToolApi, 0, len(s.tools))
	for _, bt := range s.tools {
		tools = append(tools, bt)
	}
	return tools
}

// connectedTools 返回 Connected 且未禁用时该 server 暴露的工具快照；否则返回 nil（供 Tools 聚合）。
func (s *ServerState) connectedTools() []core_types.AgentToolApi {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.status != StatusConnected || s.disabled {
		return nil
	}
	return s.toolsAPI()
}

// summary 返回展示摘要（/mcp 面板与 footer）。
func (s *ServerState) summary() ServerSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sum := ServerSummary{Name: s.name(), Status: s.status, ToolCount: len(s.tools), HTTP: s.isHTTP()}
	if s.disabled {
		sum.Status = StatusDisabled
	}
	if s.err != nil {
		sum.Err = s.err.Error()
	}
	return sum
}

// ── 工具调用（操作 SDK）──

// callTool 调用本 server 的 MCP 工具。连接状态检查（禁用 / needs-auth / 未连接）归本状态机所有，
// SDK 调用经 sdk.go 的 callTool 执行——这是 ServerState 除 connect 外的第二个 SDK 触点，
// manager 只做"按名路由到 ServerState"的编排，不触碰 SDK。
func (s *ServerState) callTool(ctx context.Context, tool string, args map[string]any) (toolResult, error) {
	s.mu.RLock()
	sess := s.sess
	disabled := s.disabled
	status := s.status
	s.mu.RUnlock()
	if disabled {
		return toolResult{}, fmt.Errorf("MCP server %s disabled", s.name())
	}
	if sess == nil {
		if status == StatusNeedsAuth {
			return toolResult{}, fmt.Errorf("MCP server %s requires authorization — run /mcp to authenticate", s.name())
		}
		return toolResult{}, fmt.Errorf("MCP server %s not connected", s.name())
	}
	res, err := callTool(ctx, sess, s.named.Server, tool, args)
	if err != nil && errors.Is(err, ErrNeedsAuth) {
		// 已连接会话中途收到 401（token 过期/吊销）：翻回 needs-auth 并派发 McpNeedsAuth，
		// 让 /mcp 面板与授权伪工具接管。否则面板仍显示 connected、工具调用持续失败。
		s.needsAuthMidSession(sess)
	}
	return res, err
}

// needsAuthMidSession 已连接会话中途收到 401（token 过期/吊销，非交互连接由 startupHandler
// 返回 ErrNeedsAuth）：把 server 翻回 needs-auth——关闭旧会话、清工具、置状态，并派发
// McpNeedsAuth（订阅方替换为授权伪工具 + 回显）。仅当该会话仍是当前会话时迁移：若已被并发
// 连接替换（s.sess != sess），本连接是旧会话，其 401 不得覆盖更新的连接状态。
// 会话关闭后 watchDisconnect 会唤醒但发现 sess 已换（nil），不会自动重连（token 已死，重连
// 只会再 401 翻回 needs-auth，徒增 churn；应由用户 /mcp 重授权）。
func (s *ServerState) needsAuthMidSession(sess sdkSession) {
	s.mu.Lock()
	if s.sess != sess {
		s.mu.Unlock()
		return
	}
	s.sess = nil
	s.setNeedsAuthStateLocked()
	s.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
	s.notify(McpNeedsAuth, McpEventContext{Name: s.name()})
}

func (s *ServerState) name() string { return s.named.Name }

func (s *ServerState) isHTTP() bool { return s.named.Server.Type == "http" }
