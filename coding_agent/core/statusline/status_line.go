// Package statusline 提供状态行业务对象：把 agent 轮次的进行状态
// （已耗时 + 流式输出 token 估算）组织为状态容器括号内的显示字符串。
//
// 这是业务逻辑层：计时的驱动时钟与 token 渲染全部在此，不依赖 TUI。
// 流式 token 计数由 session 级通用计数器（core.TokenCounter）负责，
// 它订阅 AgentEventQueue 独立累加，并通过回调把估算值喂给本包的
// TokenCountPart 做平滑收敛与样式渲染。TUI 层（StatusInfoComponent）
// 只负责赋值（SetContent）与渲染。
package statusline

import (
	"strings"
	"sync"
	"time"
)

// driveInterval 内部驱动时钟的触发间隔：token 显示值平滑收敛的驱动频率。
// 100ms（10fps）对齐参考项目逐帧平滑动画的观感；计时子段仍按秒显示，
// 不受加密影响。渲染开销为每秒 10 次 RequestRender，对终端 TUI 可接受。
const driveInterval = 100 * time.Millisecond

// tokensVisibleAfter 流式 token 计数在会话开始超过该时长后才显示
// （对齐 tinyclue 的 SHOW_TOKENS_AFTER_MS=30s 启发式，避免短回答显示无意义计数）。
const tokensVisibleAfter = 30 * time.Second

// StatusLine 状态行：组合计时与流式 token 计数两个子段，并自带驱动时钟。
//
// 生命周期：Start 开始新轮次（重置状态 + 启动内部时钟），Stop 结束轮次
// （停时钟 + 最后一次收敛）。时钟每次触发 Tick，并回调 SetOnChange 注册
// 的函数，供 TUI 刷新显示。对外暴露 Start/Stop/SetTokenEstimate/
// SetFinalTokens/Tokens/Render。
type StatusLine struct {
	mu       sync.Mutex
	timer    *TimerPart
	tokens   *TokenCountPart
	stopCh   chan struct{} // 关闭以停止内部驱动时钟
	wg       sync.WaitGroup
	onChange func() // 内容变化回调（TUI 注入：SetContent + RequestRender）
}

// New 创建一个状态行。
func New() *StatusLine {
	return &StatusLine{
		timer:  &TimerPart{},
		tokens: &TokenCountPart{},
	}
}

// SetOnChange 注册内容变化回调。内部驱动时钟每次 Tick 后调用。
// 在 Start 之前注册，首轮即生效。
func (sl *StatusLine) SetOnChange(fn func()) {
	sl.mu.Lock()
	sl.onChange = fn
	sl.mu.Unlock()
}

// Start 开始新轮次：计时起点设为 at、清空 token 计数，并启动内部驱动时钟。
func (sl *StatusLine) Start(at time.Time) {
	sl.mu.Lock()
	sl.timer.Start(at)
	sl.tokens.Reset()
	// 停掉并等待上一轮驱动时钟退出
	if sl.stopCh != nil {
		close(sl.stopCh)
		sl.wg.Wait()
	}
	stopCh := make(chan struct{})
	sl.stopCh = stopCh
	onChange := sl.onChange
	sl.wg.Add(1)
	sl.mu.Unlock()

	go func() {
		defer sl.wg.Done()
		sl.drive(stopCh, onChange)
	}()
}

// Stop 结束轮次：停止内部驱动时钟（等其退出，不再触发回调），
// 并以 at 为基准做最后一次收敛。
func (sl *StatusLine) Stop(at time.Time) {
	sl.mu.Lock()
	if sl.stopCh != nil {
		close(sl.stopCh)
		sl.stopCh = nil
		sl.wg.Wait()
	}
	sl.mu.Unlock()
	sl.Tick(at)
}

// drive 内部驱动时钟：每次触发推进各子段，并回调 onChange。
func (sl *StatusLine) drive(stopCh chan struct{}, onChange func()) {
	ticker := time.NewTicker(driveInterval)
	defer ticker.Stop()
	for {
		select {
		case now := <-ticker.C:
			sl.Tick(now)
			if onChange != nil {
				onChange()
			}
		case <-stopCh:
			return
		}
	}
}

// Tick 推进各子段（内部时钟与终态收敛共用）。
func (sl *StatusLine) Tick(now time.Time) {
	sl.timer.Tick(now)
	sl.tokens.Tick()
}

// SetTokenEstimate 设置估算 token 数目标（由 session 级 TokenCounter 回调推送）。
// 显示值由内部时钟逐 Tick 平滑收敛。
func (sl *StatusLine) SetTokenEstimate(n int) {
	sl.tokens.SetTarget(n)
}

// SetFinalTokens 终态校正 token 显示值。下行估算阶段可省略——
// Tick 已把显示值收敛到估算值附近；接入真实 usage 后用它校正。
func (sl *StatusLine) SetFinalTokens(n int) {
	sl.tokens.SetFinal(n)
}

// Tokens 返回当前显示的 token 数（供测试/终态使用）。
func (sl *StatusLine) Tokens() int {
	return sl.tokens.Display()
}

// Render 返回状态行显示字符串：非空子段以 " · " 拼接，整体加括号。
// 各段均为空时返回 ""（状态容器不显示括号内容）。
// token 计数段仅在会话超过 tokensVisibleAfter 后加入。
func (sl *StatusLine) Render() string {
	var parts []string
	if s := sl.timer.Render(); s != "" {
		parts = append(parts, s)
	}
	if sl.timer.Elapsed() > tokensVisibleAfter {
		if s := sl.tokens.Render(); s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " · ") + ")"
}
