package statusline

import (
	"sync"
	"testing"
	"time"
)

// ── TimerPart ──

func TestTimerPartUnstarted(t *testing.T) {
	tp := &TimerPart{}
	if got := tp.Render(); got != "" {
		t.Fatalf("unstarted timer Render() = %q, want %q", got, "")
	}
}

func TestTimerPartElapsed(t *testing.T) {
	start := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	tp := &TimerPart{}
	tp.Start(start)

	tp.Tick(start.Add(12 * time.Second))
	if got := tp.Render(); got != "12s" {
		t.Fatalf("Render() = %q, want %q", got, "12s")
	}

	tp.Tick(start.Add(90 * time.Second))
	if got := tp.Render(); got != "1m30s" {
		t.Fatalf("Render() = %q, want %q", got, "1m30s")
	}
}

func TestTimerPartRestart(t *testing.T) {
	start := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)

	tp := &TimerPart{}
	tp.Start(start)
	tp.Tick(start.Add(2 * time.Minute))

	tp.Start(start.Add(5 * time.Minute)) // 新轮次
	tp.Tick(start.Add(5*time.Minute + 3*time.Second))
	if got := tp.Render(); got != "3s" {
		t.Fatalf("Render() = %q, want %q", got, "3s")
	}
}

// ── StatusLine ──

func TestStatusLineEmpty(t *testing.T) {
	sl := New()
	if got := sl.Render(); got != "" {
		t.Fatalf("empty Render() = %q, want %q", got, "")
	}
}

// TestStatusLineCombined 直接驱动子段（不启动内部时钟，保证确定性）。
// 计时恒显示；token 计数在会话超过 30s 后加入。
func TestStatusLineCombined(t *testing.T) {
	sl := New()
	start := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	sl.timer.Start(start)
	sl.tokens.Reset()

	sl.Tick(start.Add(40 * time.Second))
	if got := sl.Render(); got != "(40s)" {
		t.Fatalf("timer-only Render() = %q, want %q", got, "(40s)")
	}

	// session TokenCounter 推送估算 50 token；一次 Tick 追上一半差距 = 25
	sl.SetTokenEstimate(50)
	sl.Tick(start.Add(41 * time.Second))
	if got := sl.Render(); got != "(41s · ↓ 25 tokens)" {
		t.Fatalf("combined Render() = %q, want %q", got, "(41s · ↓ 25 tokens)")
	}
}

// TestStatusLineTokenGate 验证 token 计数仅在会话超过 tokensVisibleAfter 后显示。
func TestStatusLineTokenGate(t *testing.T) {
	sl := New()
	start := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	sl.timer.Start(start)
	sl.tokens.Reset()
	sl.SetFinalTokens(100) // 固定显示值，聚焦时间门控

	// 30s 内隐藏
	sl.Tick(start.Add(29 * time.Second))
	if got := sl.Render(); got != "(29s)" {
		t.Fatalf("before threshold Render() = %q, want %q", got, "(29s)")
	}
	// 恰好 30s 仍隐藏（"大于 30 秒才显示"）
	sl.Tick(start.Add(30 * time.Second))
	if got := sl.Render(); got != "(30s)" {
		t.Fatalf("at threshold Render() = %q, want %q", got, "(30s)")
	}
	// 超过 30s 显示
	sl.Tick(start.Add(31 * time.Second))
	if got := sl.Render(); got != "(31s · ↓ 100 tokens)" {
		t.Fatalf("after threshold Render() = %q, want %q", got, "(31s · ↓ 100 tokens)")
	}
}

// TestStatusLineStopSettles 验证 Stop 以 at 为基准做终态收敛。
func TestStatusLineStopSettles(t *testing.T) {
	sl := New()
	start := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	sl.timer.Start(start)
	sl.tokens.Reset()

	// session TokenCounter 推送估算 100 token；一次 Tick 显示追到 50
	sl.tokens.SetTarget(100)
	sl.Tick(start.Add(60 * time.Second))
	// Stop 终态收敛：50 → 追上一半差距 → 75
	sl.Stop(start.Add(60 * time.Second))

	if got := sl.Render(); got != "(1m0s · ↓ 75 tokens)" {
		t.Fatalf("after Stop Render() = %q, want %q", got, "(1m0s · ↓ 75 tokens)")
	}
	if got := sl.Tokens(); got != 75 {
		t.Fatalf("after Stop Tokens() = %d, want 75", got)
	}
}

// TestStatusLineStartStopClock 验证内部驱动时钟生命周期：
// Start 后 onChange 被周期性调用，Stop 后不再调用。
func TestStatusLineStartStopClock(t *testing.T) {
	sl := New()
	var mu sync.Mutex
	calls := 0
	sl.SetOnChange(func() {
		mu.Lock()
		calls++
		mu.Unlock()
	})

	sl.Start(time.Now())
	time.Sleep(1300 * time.Millisecond)
	mu.Lock()
	c1 := calls
	mu.Unlock()
	if c1 < 1 {
		t.Fatalf("onChange not called while running: got %d, want ≥ 1", c1)
	}

	sl.Stop(time.Now())
	time.Sleep(1300 * time.Millisecond)
	mu.Lock()
	c2 := calls
	mu.Unlock()
	if c2 != c1 {
		t.Fatalf("onChange called after Stop: %d → %d", c1, c2)
	}
}
