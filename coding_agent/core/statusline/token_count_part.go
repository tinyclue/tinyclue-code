package statusline

import (
	"sync"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
)

// TokenCountPart 流式 token 计数渲染子段：display 平滑向 target 收敛
// （每 Tick 一次追上一半差距）。target 由 session 级 TokenCounter
// （core 包）回调推送，本类只负责平滑收敛与样式渲染，不负责业务计数。
type TokenCountPart struct {
	mu      sync.Mutex
	target  int // 估算 token 数（由 session 级 TokenCounter 推送）
	display int // 平滑显示值
}

// SetTarget 设置估算目标 token 数（来自 session 级 TokenCounter 回调）。
func (t *TokenCountPart) SetTarget(n int) {
	t.mu.Lock()
	t.target = n
	t.mu.Unlock()
}

// Tick 让显示值向目标收敛。
func (t *TokenCountPart) Tick() {
	t.mu.Lock()
	defer t.mu.Unlock()
	gap := t.target - t.display
	if gap <= 0 {
		return
	}
	inc := max(1, (gap+1)/2)
	t.display = min(t.display+inc, t.target)
}

// SetFinal 直接设定显示值（终态/真实 usage 校正）。
func (t *TokenCountPart) SetFinal(n int) {
	t.mu.Lock()
	t.target = n
	t.display = n
	t.mu.Unlock()
}

// Display 返回当前显示值。
func (t *TokenCountPart) Display() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.display
}

// Reset 清空计数（新轮次）。
func (t *TokenCountPart) Reset() {
	t.mu.Lock()
	t.target = 0
	t.display = 0
	t.mu.Unlock()
}

// Render 返回 token 计数文本，如 "↓ 1.2K tokens"。显示值为 0 返回 ""。
func (t *TokenCountPart) Render() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.display <= 0 {
		return ""
	}
	return "↓ " + utils.FormatTokens(t.display) + " tokens"
}
