package configpanel

import (
	"reflect"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// newTestPanel 构造一个仅含 auto_memory（候选值 true/false，初始 "false"）的面板。
func newTestPanel() *ConfigPanel {
	return &ConfigPanel{
		settings: []config.SettingsSpec{
			{Key: "auto_memory", Label: "Auto memory", Value: "false", Candidates: []string{"true", "false"}},
		},
		values:       map[string]string{"auto_memory": "false"},
		cursorSel:    0,
		ComponentBase: component.NewComponentBase(),
	}
}

// press 发送合成按键（绕过首帧 ready 守卫）。
func press(p *ConfigPanel, code rune) {
	p.ready = true
	p.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: code}))})
}

func TestToggleBool(t *testing.T) {
	p := newTestPanel()
	press(p, uv.KeySpace)
	if v := p.values["auto_memory"]; v != "true" {
		t.Errorf("after space, auto_memory = %v, want true", v)
	}
	press(p, uv.KeyLeft)
	if v := p.values["auto_memory"]; v != "false" {
		t.Errorf("after left, auto_memory = %v, want false", v)
	}
	press(p, uv.KeyRight)
	if v := p.values["auto_memory"]; v != "true" {
		t.Errorf("after right, auto_memory = %v, want true", v)
	}
}

func TestSubmitChanged(t *testing.T) {
	p := newTestPanel()
	var got map[string]any
	p.OnSubmit(func(changed map[string]any) { got = changed })

	// 未改动 → 空 map，不落盘任何东西。
	press(p, uv.KeyEnter)
	if len(got) != 0 {
		t.Errorf("unchanged submit = %v, want empty", got)
	}

	// 改动后 → 只含被改动的 auto_memory。
	press(p, uv.KeySpace)
	press(p, uv.KeyEnter)
	want := map[string]any{"auto_memory": "true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("changed submit = %v, want %v", got, want)
	}
}

func TestCancel(t *testing.T) {
	p := newTestPanel()
	cancelled := false
	p.OnCancel(func() { cancelled = true })

	press(p, uv.KeySpace)
	press(p, uv.KeyEsc)
	if !cancelled {
		t.Error("OnCancel not fired on esc")
	}
}

func TestRender(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := newTestPanel()
	out := strings.Join(p.Render(core.Data{}).Lines, "\n")
	for _, want := range []string{
		"Config:",
		"Auto memory",
		"false",
		"space/←→ toggle",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
	for _, want := range []string{"→", "(1/1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}
