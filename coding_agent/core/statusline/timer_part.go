package statusline

import (
	"sync"
	"time"
)

// TimerPart 计时子段：显示轮次已耗时。
type TimerPart struct {
	mu      sync.Mutex
	start   time.Time
	elapsed time.Duration
}

// Start 记录计时起点并清空耗时。
func (t *TimerPart) Start(at time.Time) {
	t.mu.Lock()
	t.start = at
	t.elapsed = 0
	t.mu.Unlock()
}

// Tick 按当前时间更新耗时（精确到秒）。
func (t *TimerPart) Tick(now time.Time) {
	t.mu.Lock()
	if !t.start.IsZero() {
		t.elapsed = now.Sub(t.start).Round(time.Second)
	}
	t.mu.Unlock()
}

// Render 返回已耗时文本，如 "12s"、"1m30s"。未启动返回 ""。
func (t *TimerPart) Render() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.start.IsZero() && t.elapsed == 0 {
		return ""
	}
	return t.elapsed.String()
}

// Elapsed 返回当前已耗时（未启动返回 0）。
func (t *TimerPart) Elapsed() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.elapsed
}
