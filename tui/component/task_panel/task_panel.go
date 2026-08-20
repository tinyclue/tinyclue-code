// Package task_panel 提供任务面板组件，以紧凑格式展示任务列表。
package task_panel

import (
	"fmt"
	types2 "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"sort"
	"strings"
	"time"
)

const (
	recentCompletedTTL = 30 * time.Second
	dim                = "\033[2m"
	strikethrough      = "\033[9m"
)

// TaskPanel 展示任务列表，含状态图标、owner 标签和阻塞信息。
type TaskPanel struct {
	tasks      []*types2.Task
	agentId    string
	maxDisplay int

	// 最近完成追踪（用于 30s 内优先显示）
	completionTimestamps map[string]time.Time
	previousCompletedIDs map[string]bool

	*component.ComponentBase
}

// New 创建 TaskPanel 实例。
func New(agentId string) *TaskPanel {
	return &TaskPanel{
		agentId:              agentId,
		maxDisplay:           10,
		completionTimestamps: make(map[string]time.Time),
		previousCompletedIDs: make(map[string]bool),
		ComponentBase:        component.NewComponentBase(),
	}
}

// SetTasks 替换面板中的任务数据并触发重新排序。
// 同时检测新完成的任务以记录时间戳，用于最近完成优先排序。
func (p *TaskPanel) SetTasks(tasks []*types2.Task) {
	now := time.Now()
	currentCompleted := make(map[string]bool)
	for _, t := range tasks {
		if t.Status == types2.TaskStatusCompleted {
			currentCompleted[t.ID] = true
			if !p.previousCompletedIDs[t.ID] {
				p.completionTimestamps[t.ID] = now
			}
		}
	}
	// 清理已不再 completed 的时间戳
	for id := range p.completionTimestamps {
		if !currentCompleted[id] {
			delete(p.completionTimestamps, id)
		}
	}
	p.previousCompletedIDs = currentCompleted
	p.tasks = tasks
}

// DoBefore 实现 core.Component 接口。
func (p *TaskPanel) DoBefore(_ core.Data) error { return nil }

// DoUpdate 实现 core.Component 接口。
func (p *TaskPanel) DoUpdate(_ core.Data) error { return nil }

// Children 实现 core.Component 接口。
func (p *TaskPanel) Children() []core.Component { return nil }

// Render 实现 core.Component 接口，渲染任务面板视图。
func (p *TaskPanel) Render(_ core.Data) core.View {
	if len(p.tasks) == 0 {
		return core.View{}
	}

	// 构建未完成任务集合，用于阻塞检测。
	nonCompleted := make(map[string]bool)
	for _, t := range p.tasks {
		if t.Status != types2.TaskStatusCompleted {
			nonCompleted[t.ID] = true
		}
	}

	// 统计。
	var completed, inProgress, pending int
	for _, t := range p.tasks {
		switch t.Status {
		case types2.TaskStatusCompleted:
			completed++
		case types2.TaskStatusInProgress:
			inProgress++
		case types2.TaskStatusPending:
			pending++
		}
	}
	total := len(p.tasks)

	// 构建最近完成集合。
	now := time.Now()
	recentCompleted := make(map[string]bool)
	for id, ts := range p.completionTimestamps {
		if now.Sub(ts) < recentCompletedTTL {
			recentCompleted[id] = true
		}
	}

	// 排序：recent completed → in_progress → pending（未阻塞优先） → older completed。
	sorted := sortTasks(p.tasks, nonCompleted, recentCompleted)

	// 计算显示数量。
	displayCount := min(p.maxDisplay, len(sorted))

	// 终端宽度，用于 owner 标签有条件显示。
	columns := terminal.DefaultTerminalContext.GetWidth()

	var lines []string

	// ── 标题行：dim 底色 + 数字 bold ──
	header := fmt.Sprintf("%s%d%s tasks (%s%d%s done, %s%d%s in progress, %s%d%s open)%s",
		dim, total, types.Reset+dim,
		types.Bold, completed, types.Reset+dim,
		types.Bold, inProgress, types.Reset+dim,
		types.Bold, pending, types.Reset+dim, types.Reset)
	lines = append(lines, header)

	// ── 任务行 ──
	for i := 0; i < displayCount; i++ {
		line := renderTaskLine(sorted[i], nonCompleted, columns)
		lines = append(lines, line)
	}

	// ── 截断提示 ──
	if displayCount < len(sorted) {
		remaining := sorted[displayCount:]
		var remainingInProg, remainingPending, remainingCompleted int
		for _, t := range remaining {
			switch t.Status {
			case types2.TaskStatusInProgress:
				remainingInProg++
			case types2.TaskStatusPending:
				remainingPending++
			case types2.TaskStatusCompleted:
				remainingCompleted++
			}
		}
		var hintParts []string
		if remainingInProg > 0 {
			hintParts = append(hintParts, fmt.Sprintf("%d in progress", remainingInProg))
		}
		if remainingPending > 0 {
			hintParts = append(hintParts, fmt.Sprintf("%d pending", remainingPending))
		}
		if remainingCompleted > 0 {
			hintParts = append(hintParts, fmt.Sprintf("%d completed", remainingCompleted))
		}
		if len(hintParts) > 0 {
			hint := dim + "… +" + strings.Join(hintParts, ", ") + types.Reset
			lines = append(lines, hint)
		}
	}

	return core.View{Lines: lines}
}

// renderTaskLine 渲染单行任务。
func renderTaskLine(t *types2.Task, nonCompleted map[string]bool, columns int) string {
	// 阻塞检测。
	blocked := false
	for _, b := range t.BlockedBy {
		if nonCompleted[b] {
			blocked = true
			break
		}
	}

	// 状态图标。
	var icon string
	switch t.Status {
	case types2.TaskStatusCompleted:
		icon = types.FgGreen + "✔" + types.Reset
	case types2.TaskStatusInProgress:
		icon = types.FgLightRed + "■" + types.Reset
	case types2.TaskStatusPending:
		icon = types.FgGray + "□" + types.Reset
	}

	// 主题文字样式。
	var subjectStyled string
	switch t.Status {
	case types2.TaskStatusCompleted:
		// 仅 dim + strikethrough（不用 FgGray，避免双重变暗）
		subjectStyled = dim + strikethrough + t.Subject + types.Reset
	case types2.TaskStatusInProgress:
		if blocked {
			subjectStyled = types.Bold + dim + t.Subject + types.Reset
		} else {
			subjectStyled = types.Bold + t.Subject + types.Reset
		}
	default:
		if blocked {
			// 仅 dim（不用 FgGray）
			subjectStyled = dim + t.Subject + types.Reset
		} else {
			subjectStyled = t.Subject
		}
	}

	// Owner 标签：columns >= 60 且 owner 非空时显示。
	var ownerTag string
	if columns >= 60 && t.Owner != "" {
		ownerTag = dim + "(@" + t.Owner + ")" + types.Reset
	}

	// 阻塞信息：▸ blocked by #1, #2
	var blockedInfo string
	if blocked {
		ids := strings.Join(t.BlockedBy, ", #")
		blockedInfo = dim + " ▸ blocked by #" + ids + types.Reset
	}

	var parts []string
	parts = append(parts, icon+" "+subjectStyled)
	if ownerTag != "" {
		parts = append(parts, ownerTag)
	}
	if blockedInfo != "" {
		parts = append(parts, blockedInfo)
	}
	return strings.Join(parts, " ")
}

// sortTasks 按优先级排序：recent completed → in_progress → pending（未阻塞优先） → older completed。
func sortTasks(tasks []*types2.Task, nonCompleted map[string]bool, recentCompleted map[string]bool) []*types2.Task {
	sorted := make([]*types2.Task, len(tasks))
	copy(sorted, tasks)

	sort.SliceStable(sorted, func(i, j int) bool {
		ti, tj := sorted[i], sorted[j]
		pi := priority(ti, nonCompleted, recentCompleted)
		pj := priority(tj, nonCompleted, recentCompleted)
		return pi < pj
	})
	return sorted
}

// priority 返回任务排序优先级（数值越小越靠前）。
func priority(t *types2.Task, nonCompleted map[string]bool, recentCompleted map[string]bool) int {
	switch t.Status {
	case types2.TaskStatusInProgress:
		return 1
	case types2.TaskStatusPending:
		// 被阻塞的任务排在未阻塞后面。
		for _, b := range t.BlockedBy {
			if nonCompleted[b] {
				return 3
			}
		}
		return 2
	case types2.TaskStatusCompleted:
		if recentCompleted[t.ID] {
			return 0
		}
		return 4
	default:
		return 5
	}
}
