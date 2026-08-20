// Package selectlist provides a reusable filterable single-select list component.
package selectlist

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"strings"
)

// Item is a single selectable option.
type Item struct {
	Value string // value submitted when selected
	Label string // displayed label, matched against when filtering
	Desc  string // description text shown next to the label
}

// SelectList is a filterable single-select list with cursor navigation.
// It implements core.Component and can be used standalone or embedded.
type SelectList struct {
	items         []Item
	filteredItems []Item
	search        string
	cursorIdx     int
	active        bool

	onSelectChange func(item *Item)  // cursor moved
	onSelect       func(item *Item)  // enter / Confirm()
	onCancel       func()            // esc / Cancel()
	onFilterChange func(text string) // search text changed

	*component.ComponentBase
}

// New creates an empty SelectList.
func New() *SelectList {
	return &SelectList{
		ComponentBase: component.NewComponentBase(),
	}
}

// --- Items ---

// AddItem appends an option.
func (sl *SelectList) AddItem(value, label, desc string) {
	sl.items = append(sl.items, Item{Value: value, Label: label, Desc: desc})
	sl.refilter()
}

// Items returns all items.
func (sl *SelectList) Items() []Item {
	return sl.items
}

// FilteredItems returns the currently filtered items.
func (sl *SelectList) FilteredItems() []Item {
	return sl.filteredItems
}

// --- Active state ---

// SetActive controls whether the list is visible and processes input.
func (sl *SelectList) SetActive(active bool) {
	sl.active = active
}

// IsActive returns whether the list is active.
func (sl *SelectList) IsActive() bool {
	return sl.active
}

// --- Callbacks ---

func (sl *SelectList) OnSelectChange(fn func(item *Item))  { sl.onSelectChange = fn }
func (sl *SelectList) OnSelect(fn func(item *Item))        { sl.onSelect = fn }
func (sl *SelectList) OnCancel(fn func())                  { sl.onCancel = fn }
func (sl *SelectList) OnFilterChange(fn func(text string)) { sl.onFilterChange = fn }

// --- Cursor ---

// CursorUp moves the selection cursor up, wrapping to the last item.
func (sl *SelectList) CursorUp() {
	n := len(sl.filteredItems)
	if n == 0 {
		return
	}
	sl.cursorIdx--
	if sl.cursorIdx < 0 {
		sl.cursorIdx = n - 1
	}
	if sl.onSelectChange != nil {
		sl.onSelectChange(sl.SelectedItem())
	}
}

// CursorDown moves the selection cursor down, wrapping to the first item.
func (sl *SelectList) CursorDown() {
	n := len(sl.filteredItems)
	if n == 0 {
		return
	}
	sl.cursorIdx++
	if sl.cursorIdx >= n {
		sl.cursorIdx = 0
	}
	if sl.onSelectChange != nil {
		sl.onSelectChange(sl.SelectedItem())
	}
}

// SelectedItem returns the currently selected item, or nil if none.
func (sl *SelectList) SelectedItem() *Item {
	if sl.cursorIdx < 0 || sl.cursorIdx >= len(sl.filteredItems) {
		return nil
	}
	return &sl.filteredItems[sl.cursorIdx]
}

// CursorIndex returns the current cursor index within filtered items.
func (sl *SelectList) CursorIndex() int {
	return sl.cursorIdx
}

// --- Search ---

// Search returns the current search term.
func (sl *SelectList) Search() string {
	return sl.search
}

// FilterAppend appends a character to the search and re-filters.
// Fires onFilterChange callback.
func (sl *SelectList) FilterAppend(ch string) {
	sl.search += ch
	sl.refilter()
	if sl.onFilterChange != nil {
		sl.onFilterChange(sl.search)
	}
}

// FilterBackspace removes the last character from the search and re-filters.
// Fires onFilterChange callback (even when search was already empty).
func (sl *SelectList) FilterBackspace() {
	if len(sl.search) > 0 {
		sl.search = sl.search[:len(sl.search)-1]
		sl.refilter()
	} else {
		sl.filteredItems = make([]Item, len(sl.items))
		copy(sl.filteredItems, sl.items)
	}
	if sl.onFilterChange != nil {
		sl.onFilterChange(sl.search)
	}
}

// SetSearch sets the search term and re-filters.
// Fires onFilterChange callback.
func (sl *SelectList) SetSearch(term string) {
	sl.search = term
	sl.refilter()
	if sl.onFilterChange != nil {
		sl.onFilterChange(sl.search)
	}
}

// --- Actions ---

// Confirm confirms the current selection, firing onSelect.
func (sl *SelectList) Confirm() {
	item := sl.SelectedItem()
	if item != nil && sl.onSelect != nil {
		sl.onSelect(item)
	}
	sl.Reset()
}

// Cancel cancels, firing onCancel and resetting state.
func (sl *SelectList) Cancel() {
	if sl.onCancel != nil {
		sl.onCancel()
	}
	sl.Reset()
}

// Reset clears search and cursor, showing all items.
func (sl *SelectList) Reset() {
	sl.search = ""
	sl.cursorIdx = 0
	sl.filteredItems = make([]Item, len(sl.items))
	copy(sl.filteredItems, sl.items)
}

// --- core.Component ---

func (sl *SelectList) DoBefore(core.Data) error { return nil }

func (sl *SelectList) DoUpdate(data core.Data) error {
	if !sl.active {
		return nil
	}

	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		switch {
		case m.MatchString("up", "ctrl+p"):
			sl.CursorUp()
		case m.MatchString("down", "ctrl+n"):
			sl.CursorDown()
		case m.MatchString("enter"):
			sl.Confirm()
		case m.MatchString("esc"):
			sl.Cancel()
		case m.MatchString("backspace"):
			sl.FilterBackspace()
		default:
			sl.FilterAppend(m.Text)
		}
	}
	return nil
}

func (sl *SelectList) Render(data core.Data) core.View {
	var lines []string
	if !sl.active {
		return core.View{Lines: lines}
	}

	items := sl.filteredItems
	total := len(items)
	if total == 0 {
		lines = append(lines, "  (no matches)")
		lines = append(lines, "")
		lines = append(lines, "  [Type to search] [Enter to change] [Esc to cancel]")
		return core.View{Lines: lines}
	}

	// Scroll window: show up to 6 items with cursor always visible
	maxDisplay := 6
	start, end := 0, total
	if total > maxDisplay {
		half := maxDisplay / 2
		start = sl.cursorIdx - half
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
		item := items[i]
		cursor := " "
		if i == sl.cursorIdx {
			cursor = ">"
		}
		line := fmt.Sprintf("%s %-14s %s", cursor, item.Label, item.Desc)
		if i == sl.cursorIdx {
			line = types.FgLightBlue + types.Bold + line + types.Reset
		}
		lines = append(lines, line)
	}

	// Position indicator
	lines = append(lines, fmt.Sprintf("  (%d/%d)", sl.cursorIdx+1, total))

	return core.View{Lines: lines}
}

// --- internal ---

// refilter re-runs the left-match filter and resets the cursor.
// Leading "/" is treated as an autocomplete trigger prefix and skipped
// during matching; "/" alone shows all items.
func (sl *SelectList) refilter() {
	if sl.search == "" || sl.search == "/" {
		sl.filteredItems = make([]Item, len(sl.items))
		copy(sl.filteredItems, sl.items)
	} else {
		sl.filteredItems = nil
		search := sl.search
		if search[0] == '/' {
			search = search[1:]
		}
		lower := strings.ToLower(search)
		for _, item := range sl.items {
			if strings.HasPrefix(strings.ToLower(item.Label), lower) {
				sl.filteredItems = append(sl.filteredItems, item)
			}
		}
	}
	if sl.cursorIdx >= len(sl.filteredItems) {
		sl.cursorIdx = 0
	}
}
