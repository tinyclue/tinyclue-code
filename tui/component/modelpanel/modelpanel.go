// Package modelpanel provides a panel component for selecting the active
// agent model from providers that have API keys configured.
package modelpanel

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"sort"
	"strconv"
	"strings"
)

// modelItem holds display info for a single model entry.
type modelItem struct {
	Provider      string
	ModelID       string
	ModelName     string
	ContextWindow int
}

// thinkingLevel 表示一个推理级别选项。
type thinkingLevel struct {
	Display string // 显示名，如 "High effort"
	Value   string // 配置值，如 "high"
}

var thinkingLevels = []thinkingLevel{
	{Display: "Low effort", Value: "low"},
	{Display: "Medium effort", Value: "medium"},
	{Display: "High effort", Value: "high"},
	{Display: "Max effort", Value: "max"},
}

// ModelPanel is a scrollable list of models from configured providers.
// The user navigates with ↑/↓ and confirms with Enter.
type ModelPanel struct {
	items     []modelItem
	cursorSel int

	thinkingSel int // 当前选中的推理级别索引

	onSubmit func(provider, modelID, reasoningEffort string)
	onCancel func()

	ready bool // prevents first-frame key echo

	*component.ComponentBase
}

// New creates a ModelPanel pre-loaded with models from configured providers.
func New() *ModelPanel {
	items := buildModelList()
	idx := defaultIndex(items)
	if idx < 0 {
		idx = 0
	}
	return &ModelPanel{
		items:         items,
		cursorSel:     idx,
		thinkingSel:   defaultThinkingIndex(config.Cnf.ReasoningEffort()),
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit registers a callback fired when the user confirms a selection.
// The callback receives provider name, model ID and reasoning effort value.
func (mp *ModelPanel) OnSubmit(fn func(provider, modelID, reasoningEffort string)) {
	mp.onSubmit = fn
}

// OnCancel registers a callback fired when the user presses Esc.
func (mp *ModelPanel) OnCancel(fn func()) {
	mp.onCancel = fn
}

// DoBefore implements core.Component.
func (mp *ModelPanel) DoBefore(core.Data) error { return nil }

// DoUpdate handles keyboard navigation and confirmation.
func (mp *ModelPanel) DoUpdate(data core.Data) error {
	if !mp.ready {
		mp.ready = true
		return nil
	}
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		mp.handleInput(m)
	}
	return nil
}

func (mp *ModelPanel) handleInput(key core.KeyPressMsg) {
	n := len(mp.items)
	if n == 0 {
		return
	}
	switch {
	case key.MatchString("up", "ctrl+p"):
		mp.cursorSel--
		if mp.cursorSel < 0 {
			mp.cursorSel = n - 1
		}
	case key.MatchString("down", "ctrl+n"):
		mp.cursorSel++
		if mp.cursorSel >= n {
			mp.cursorSel = 0
		}
	case key.MatchString("left", "ctrl+b"):
		mp.thinkingSel--
		if mp.thinkingSel < 0 {
			mp.thinkingSel = len(thinkingLevels) - 1
		}
	case key.MatchString("right", "ctrl+f"):
		mp.thinkingSel++
		if mp.thinkingSel >= len(thinkingLevels) {
			mp.thinkingSel = 0
		}
	case key.MatchString("enter"):
		if mp.cursorSel >= 0 && mp.cursorSel < n {
			item := mp.items[mp.cursorSel]
			if mp.onSubmit != nil {
				mp.onSubmit(item.Provider, item.ModelID, thinkingLevels[mp.thinkingSel].Value)
			}
		}
	case key.MatchString("esc"):
		if mp.onCancel != nil {
			mp.onCancel()
		}
	}
}

// Render implements core.Component.
func (mp *ModelPanel) Render(data core.Data) core.View {
	width := terminal.DefaultTerminalContext.GetWidth()
	if width < 10 {
		width = 10
	}

	var lines []string

	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " Select model:")
	lines = append(lines, "")

	total := len(mp.items)
	if total == 0 {
		lines = append(lines, "  (no configured models — use /login first)")
		lines = append(lines, "")
		lines = append(lines, "  [Esc to exit]")
		return core.View{Lines: lines}
	}

	defaultProvider := config.Cnf.DefaultProvider()
	defaultModel := config.Cnf.DefaultModel()

	// Scrollable viewport (max 10 items)
	maxDisplay := 10
	start, end := 0, total
	if total > maxDisplay {
		half := maxDisplay / 2
		start = mp.cursorSel - half
		if start < 0 {
			start = 0
		}
		end = start + maxDisplay
		if end > total {
			end = total
			start = total - maxDisplay
		}
	}

	for i := start; i < end; i++ {
		item := mp.items[i]
		cursor := " "
		if i == mp.cursorSel {
			cursor = "→"
		}

		// Checkmark for the currently active model
		check := ""
		if item.Provider == defaultProvider && item.ModelID == defaultModel {
			check = " ✔"
		}

		// Context-window hint
		contextInfo := ""
		if item.ContextWindow > 0 {
			contextInfo = " (" + formatContextWindow(item.ContextWindow) + " context)"
		}

		label := fmt.Sprintf("%s %s%s%s", cursor, item.ModelName, contextInfo, check)
		if i == mp.cursorSel {
			label = types.FgLightBlue + types.Bold + label + types.Reset
		}
		lines = append(lines, label)
	}

	// Page indicator
	lines = append(lines, fmt.Sprintf("  (%d/%d)", mp.cursorSel+1, total))
	lines = append(lines, "")

	// Current-model summary
	currentLabel := mp.currentModelLabel()
	if currentLabel != "" {
		lines = append(lines, fmt.Sprintf("  ● Current model %s", currentLabel))
	}

	// Thinking level
	think := thinkingLevels[mp.thinkingSel]
	defaultSuffix := ""
	if think.Value == "high" {
		defaultSuffix = " (default)"
	}
	lines = append(lines, types.FgLightRed+fmt.Sprintf("  ⏺ %s%s    ← → to adjust", think.Display, defaultSuffix)+types.Reset)
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  Enter to confirm · Esc to exit"+types.Reset)

	return core.View{Lines: lines}
}

// currentModelLabel returns the display name of the currently configured model,
// or the raw model ID if the name is not available.
func (mp *ModelPanel) currentModelLabel() string {
	dp := config.Cnf.DefaultProvider()
	dm := config.Cnf.DefaultModel()
	if dp == "" || dm == "" {
		return ""
	}
	if models, ok := config.Data[dp]; ok {
		if m, ok := models[dm]; ok {
			return m.Name
		}
	}
	return dm
}

// ── helpers ──

// buildModelList collects all models from providers that have auth configured.
func buildModelList() []modelItem {
	providers := config.Cnf.ProviderNames()
	var items []modelItem
	for _, p := range providers {
		models, ok := config.Data[p]
		if !ok {
			continue
		}
		var ids []string
		for id := range models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			m := models[id]
			items = append(items, modelItem{
				Provider:      p,
				ModelID:       id,
				ModelName:     m.Name,
				ContextWindow: m.ContextWindow,
			})
		}
	}
	return items
}

// defaultIndex finds the item that matches the current DefaultProvider/DefaultModel.
func defaultIndex(items []modelItem) int {
	dp := config.Cnf.DefaultProvider()
	dm := config.Cnf.DefaultModel()
	for i, it := range items {
		if it.Provider == dp && it.ModelID == dm {
			return i
		}
	}
	return -1
}

// defaultThinkingIndex 返回配置值对应的 thinkingLevel 索引，未找到则返回 high (2)。
func defaultThinkingIndex(val string) int {
	for i, l := range thinkingLevels {
		if l.Value == val {
			return i
		}
	}
	return 2 // high
}

// formatContextWindow renders a context-window value in human-readable form.
// e.g. 1000000 → "1M", 200000 → "200K"
func formatContextWindow(w int) string {
	if w >= 1000000 {
		n := w / 1000000
		return strconv.Itoa(n) + "M"
	}
	if w >= 1000 {
		n := w / 1000
		return strconv.Itoa(n) + "K"
	}
	return strconv.Itoa(w)
}
