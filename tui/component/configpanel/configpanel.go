// Package configpanel provides a panel component for editing configuration
// settings (e.g. auto-memory). The panel builds its rows from
// config.Cnf.ListSettings(), so adding a new setting requires no panel changes.
package configpanel

import (
	"fmt"
	"slices"
	"strings"

	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// nameColumnWidth mirrors the Claude /config panel's fixed name column.
const nameColumnWidth = 44

// ConfigPanel is a scrollable list of configurable settings.
// ↑/↓ move the cursor, space/←/→ toggle the focused value,
// Enter submits changed settings, Esc cancels without writing.
type ConfigPanel struct {
	settings []config.SettingsSpec
	values   map[string]string // editable copies of current candidate values, keyed by setting key
	cursorSel int

	onSubmit func(changed map[string]any)
	onCancel func()

	ready bool // prevents first-frame key echo

	*component.ComponentBase
}

// New creates a ConfigPanel pre-loaded with settings from config.
func New() *ConfigPanel {
	views := config.Cnf.ListSettings()
	values := make(map[string]string, len(views))
	for _, v := range views {
		values[v.Key] = v.Value
	}
	return &ConfigPanel{
		settings:     views,
		values:       values,
		cursorSel:    0,
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit registers a callback fired when the user confirms changes.
// The callback receives the settings that were modified (key → new value).
func (cp *ConfigPanel) OnSubmit(fn func(changed map[string]any)) {
	cp.onSubmit = fn
}

// OnCancel registers a callback fired when the user presses Esc.
func (cp *ConfigPanel) OnCancel(fn func()) {
	cp.onCancel = fn
}

// DoBefore implements core.Component.
func (cp *ConfigPanel) DoBefore(core.Data) error { return nil }

// DoUpdate implements core.Component.
func (cp *ConfigPanel) DoUpdate(data core.Data) error {
	if !cp.ready {
		cp.ready = true
		return nil
	}
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		cp.handleInput(m)
	}
	return nil
}

func (cp *ConfigPanel) handleInput(key core.KeyPressMsg) {
	n := len(cp.settings)
	if n == 0 {
		return
	}
	switch {
	case key.MatchString("up", "ctrl+p"):
		cp.cursorSel--
		if cp.cursorSel < 0 {
			cp.cursorSel = n - 1
		}
	case key.MatchString("down", "ctrl+n"):
		cp.cursorSel++
		if cp.cursorSel >= n {
			cp.cursorSel = 0
		}
	case key.MatchString("space"),
		key.MatchString("left", "ctrl+b"),
		key.MatchString("right", "ctrl+f"):
		cp.toggle(cp.settings[cp.cursorSel])
	case key.MatchString("enter"):
		cp.submit()
	case key.MatchString("esc"):
		if cp.onCancel != nil {
			cp.onCancel()
		}
	}
}

// toggle cycles the focused setting's value to the next candidate.
func (cp *ConfigPanel) toggle(s config.SettingsSpec) {
	if len(s.Candidates) == 0 {
		return
	}
	idx := max(slices.Index(s.Candidates, cp.values[s.Key]), 0)
	cp.values[s.Key] = s.Candidates[(idx+1)%len(s.Candidates)]
}

// submit gathers settings changed since the panel opened and fires onSubmit.
func (cp *ConfigPanel) submit() {
	if cp.onSubmit == nil {
		return
	}
	changed := make(map[string]any)
	for _, s := range cp.settings {
		cur := cp.values[s.Key]
		if cur != s.Value {
			changed[s.Key] = cur
		}
	}
	cp.onSubmit(changed)
}

// Render implements core.Component.
func (cp *ConfigPanel) Render(data core.Data) core.View {
	width := max(terminal.DefaultTerminalContext.GetWidth(), 10)

	var lines []string

	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " Config:")
	lines = append(lines, "")

	total := len(cp.settings)
	if total == 0 {
		lines = append(lines, "  (no configurable settings)")
		lines = append(lines, "")
		lines = append(lines, "  [Esc to exit]")
		return core.View{Lines: lines}
	}

	// Scrollable viewport (max 10 items)，与 /model 面板一致。
	maxDisplay := 10
	start, end := 0, total
	if total > maxDisplay {
		half := maxDisplay / 2
		start = max(cp.cursorSel-half, 0)
		end = start + maxDisplay
		if end > total {
			end = total
			start = total - maxDisplay
		}
	}

	for i := start; i < end; i++ {
		s := cp.settings[i]
		cursor := "  "
		if i == cp.cursorSel {
			cursor = "→ "
		}

		line := cursor + padRight(s.Label, nameColumnWidth) + cp.valueText(s)
		if i == cp.cursorSel {
			line = types.FgLightBlue + types.Bold + line + types.Reset
		}
		lines = append(lines, line)
	}

	// 位置/页指示器，与 /model 面板一致。
	lines = append(lines, fmt.Sprintf("  (%d/%d)", cp.cursorSel+1, total))
	lines = append(lines, "")

	lines = append(lines, types.FgGray+"  ↑↓ navigate · space/←→ toggle · enter confirm · esc cancel"+types.Reset)

	return core.View{Lines: lines}
}

// valueText renders the current (editable) value of a setting as text.
func (cp *ConfigPanel) valueText(s config.SettingsSpec) string {
	return cp.values[s.Key]
}

// padRight left-aligns label into a fixed-width column, matching the Claude
// /config row layout (name on the left, value on the right).
func padRight(s string, n int) string {
	w := types.VisualWidth(s)
	return s + strings.Repeat(" ", max(n-w, 1))
}
