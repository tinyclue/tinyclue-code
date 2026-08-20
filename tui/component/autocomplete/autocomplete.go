// Package autocomplete provides a command autocomplete component
// that embeds a reusable SelectList and integrates with the Editor.
package autocomplete

import (
	"github.com/tinyclue/tinyclue-code/tui/component/editor"
	"github.com/tinyclue/tinyclue-code/tui/component/selectlist"
	"github.com/tinyclue/tinyclue-code/tui/core"
)

// defaultItems are the built-in command suggestions.
var defaultItems = []selectlist.Item{
	{Value: "/login", Label: "login", Desc: "Configure provider authentication"},
	{Value: "/model", Label: "model", Desc: "Select model"},
	{Value: "/mcp", Label: "mcp", Desc: "Manage MCP servers"},
	{Value: "/exit", Label: "exit", Desc: "Exit the application"},
}

// AutoComplete is a command autocomplete component.
// It implements core.Component by embedding selectlist.SelectList
// and adding Editor integration on top.
type AutoComplete struct {
	*selectlist.SelectList
	defaultEditor *editor.Editor
}

// New creates an AutoComplete with default command items.
func New() *AutoComplete {
	ac := &AutoComplete{
		SelectList: selectlist.New(),
	}
	for _, item := range defaultItems {
		ac.AddItem(item.Value, item.Label, item.Desc)
	}
	return ac
}

// WithEditor binds an Editor and registers callbacks for Editor integration.
func (ac *AutoComplete) WithEditor(e *editor.Editor) *AutoComplete {
	ac.defaultEditor = e

	ac.OnFilterChange(func(text string) {
		if text == "" {
			ac.defaultEditor.SetText("")
			ac.defaultEditor.ActiveAutoComplete(false)
			ac.SetActive(false)
		} else {
			ac.defaultEditor.SetText(text)
			ac.defaultEditor.MoveToEnd()
		}
	})

	ac.OnSelect(func(item *selectlist.Item) {
		ac.defaultEditor.DoOnSubmit(item.Value)
		ac.defaultEditor.ActiveAutoComplete(false)
		ac.SetActive(false)
	})

	ac.OnCancel(func() {
		ac.defaultEditor.SetText("")
		ac.defaultEditor.ActiveAutoComplete(false)
		ac.SetActive(false)
	})

	return ac
}

// DoBefore triggers the autocomplete when "/" is typed on an empty editor.
// This runs before DoUpdate so the Editor skips processing the same "/" key.
func (ac *AutoComplete) DoBefore(data core.Data) error {
	keyMsg, ok := data.Msg.(core.KeyPressMsg)
	if !ok {
		return nil
	}
	if !ac.IsActive() && keyMsg.Key().Text == "/" && ac.defaultEditor.Text() == "" {
		ac.SetActive(true)
		ac.defaultEditor.ActiveAutoComplete(true)
	}
	return nil
}

// DoUpdate delegates fully to the embedded SelectList when active.
func (ac *AutoComplete) DoUpdate(data core.Data) error {
	if !ac.IsActive() {
		return nil
	}
	return ac.SelectList.DoUpdate(data)
}

// Render delegates rendering to the embedded SelectList.
func (ac *AutoComplete) Render(data core.Data) core.View {
	return ac.SelectList.Render(data)
}
