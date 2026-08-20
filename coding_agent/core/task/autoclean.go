package task

import (
	"context"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"time"
)

// AutoCleaner monitors the task list and automatically resets it when all
// tasks have been completed for a grace period. This mirrors the TS
// TasksV2Store behaviour: when every task reaches status "completed", a
// timer starts; if no new incomplete task appears before the timer fires,
// Reset() is called to delete all task files.
//
// Use NewAutoCleaner to create one, then call Stop() when done.
type AutoCleaner struct {
	ctx      context.Context
	manager  *TaskManager
	interval time.Duration
	grace    time.Duration
}

// NewAutoCleaner creates an AutoCleaner that watches the given manager's task
// list. pollingInterval controls how often the disk is re-scanned; gracePeriod
// is the quiet time that must pass with every task completed before Reset is
// called. Defaults (2s poll, 5s grace) are used when a zero value is passed.
func NewAutoCleaner(ctx context.Context, m *TaskManager, pollingInterval, gracePeriod time.Duration) *AutoCleaner {
	if pollingInterval <= 0 {
		pollingInterval = 2 * time.Second
	}
	if gracePeriod <= 0 {
		gracePeriod = 5 * time.Second
	}
	ac := &AutoCleaner{
		ctx:      ctx,
		manager:  m,
		interval: pollingInterval,
		grace:    gracePeriod,
	}
	return ac
}

func (ac *AutoCleaner) Start() {
	go ac.loop()
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// allCompleted returns true when every visible task has status Completed.
func (ac *AutoCleaner) allCompleted() bool {
	tasks, err := ac.manager.List()
	if err != nil {
		return false
	}
	if len(tasks) == 0 {
		return false
	}
	for _, t := range tasks {
		if t.Status != types.TaskStatusCompleted {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// loop
// ---------------------------------------------------------------------------
//
// Two states:
//
//	idle ──(all tasks completed)──→ waiting
//	waiting ──(grace expired + still all completed)──→ calls Reset, back to idle
//	waiting ──(incomplete task appears)──→ back to idle

func (ac *AutoCleaner) loop() {
	ticker := time.NewTicker(ac.interval)
	defer ticker.Stop()

	var graceTimer *time.Timer
	var graceCh <-chan time.Time

	cancelGrace := func() {
		if graceTimer != nil {
			graceTimer.Stop()
			graceTimer = nil
			graceCh = nil
		}
	}

	for {
		select {
		case <-ac.ctx.Done():
			cancelGrace()
			return

		case <-ticker.C:
			if ac.allCompleted() {
				// 全部已完成 → 进入 waiting 状态（启动宽限计时器）
				if graceTimer == nil {
					graceTimer = time.NewTimer(ac.grace)
					graceCh = graceTimer.C
				}
			} else {
				// 有未完成的任务 → 回到 idle 状态（取消宽限计时器）
				cancelGrace()
			}

		case <-graceCh:
			// 宽限计时器到时间了，再次确认
			if ac.allCompleted() {
				_ = ac.manager.Reset()
			}
			cancelGrace()
		}
	}
}
