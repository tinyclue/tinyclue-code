package exitplanmode

import (
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// renderAll 渲染面板并拼成单字符串（断言用）。
func renderAll(p *Panel) string {
	return strings.Join(p.Render(core.Data{}).Lines, "\n")
}

// press 发送合成按键。
func press(p *Panel, code rune) {
	p.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: code}))})
}

// typeText 发送带文本的按键序列（键入当前选项的输入框）。
func typeText(p *Panel, text string) {
	for _, r := range text {
		p.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: r, Text: string(r)}))})
	}
}

func TestRenderShowsPlanAndOptions(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New("Step 1: explore\nStep 2: implement", "/tmp/plans/a.md")

	out := renderAll(p)
	for _, want := range []string{
		"Ready to code?",
		"Plan file: /tmp/plans/a.md",
		"Step 1: explore",
		"Step 2: implement",
		"Yes, start coding",
		"No, keep planning",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q:\n%s", want, out)
		}
	}
}

func TestRenderEmptyPlanPlaceholder(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New("", "")

	out := renderAll(p)
	if !strings.Contains(out, "(no plan yet)") {
		t.Errorf("empty plan should show placeholder:\n%s", out)
	}
}

// TestRenderInputFollowsSelection 验证单输入框跟随当前选项：默认 Approve 显示 Feedback，
// 切到 Deny 显示 Reason；空内容显示对应占位符。
func TestRenderInputFollowsSelection(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New("some plan", "")

	out := renderAll(p)
	if !strings.Contains(out, "Feedback:") {
		t.Errorf("default selection should show Feedback input:\n%s", out)
	}
	if !strings.Contains(out, "Add optional feedback") {
		t.Errorf("approve empty input should show feedback placeholder:\n%s", out)
	}

	press(p, uv.KeyDown)
	out = renderAll(p)
	if !strings.Contains(out, "Reason:") {
		t.Errorf("deny selection should show Reason input:\n%s", out)
	}
	if !strings.Contains(out, "Tell Tinyclue what to change") {
		t.Errorf("deny empty input should show reason placeholder:\n%s", out)
	}
}

func TestApprove(t *testing.T) {
	p := New("some plan", "")
	var gotAction Action
	var gotReason string
	submitted := false
	p.OnSubmit(func(action Action, reason string) {
		gotAction = action
		gotReason = reason
		submitted = true
	})

	// 默认光标在 Approve → Enter
	press(p, uv.KeyEnter)

	if !submitted {
		t.Fatal("expected onSubmit on approve enter")
	}
	if gotAction != ActionApprove {
		t.Errorf("expected ActionApprove, got %v", gotAction)
	}
	if gotReason != "" {
		t.Errorf("expected empty feedback on approve, got %q", gotReason)
	}
}

// TestApproveCarriesFeedback 验证批准时输入的内容作为 Feedback（acceptFeedback）带回。
func TestApproveCarriesFeedback(t *testing.T) {
	p := New("some plan", "")
	var gotAction Action
	var gotReason string
	submitted := false
	p.OnSubmit(func(action Action, reason string) {
		gotAction = action
		gotReason = reason
		submitted = true
	})

	typeText(p, "also update the README")
	press(p, uv.KeyEnter)

	if !submitted {
		t.Fatal("expected onSubmit on approve enter")
	}
	if gotAction != ActionApprove {
		t.Errorf("expected ActionApprove, got %v", gotAction)
	}
	if gotReason != "also update the README" {
		t.Errorf("expected approve to carry feedback, got %q", gotReason)
	}
}

func TestDenyWithReason(t *testing.T) {
	p := New("some plan", "")
	var gotAction Action
	var gotReason string
	p.OnSubmit(func(action Action, reason string) {
		gotAction = action
		gotReason = reason
	})

	// ↓ 到 Deny → 输入原因 → Enter 确认
	press(p, uv.KeyDown)
	typeText(p, "refine the plan")
	press(p, uv.KeyEnter)

	if gotAction != ActionDeny {
		t.Errorf("expected ActionDeny, got %v", gotAction)
	}
	if gotReason != "refine the plan" {
		t.Errorf("expected typed reason, got %q", gotReason)
	}
}

func TestDenyEmptyReason(t *testing.T) {
	p := New("some plan", "")
	var gotReason string
	denied := false
	p.OnSubmit(func(action Action, reason string) {
		gotReason = reason
		denied = action == ActionDeny
	})

	// ↓ 到 Deny → 直接 Enter（空原因）确认拒绝
	press(p, uv.KeyDown)
	press(p, uv.KeyEnter)

	if !denied {
		t.Error("expected deny submitted")
	}
	if gotReason != "" {
		t.Errorf("expected empty reason, got %q", gotReason)
	}
}

func TestEscCancelsPanel(t *testing.T) {
	p := New("some plan", "")
	cancelled := false
	p.OnCancel(func() { cancelled = true })

	press(p, uv.KeyEsc)

	if !cancelled {
		t.Error("expected onCancel on top-level esc")
	}
}

func TestReasonBackspace(t *testing.T) {
	p := New("some plan", "")
	var gotReason string
	p.OnSubmit(func(action Action, reason string) { gotReason = reason })

	press(p, uv.KeyDown)
	typeText(p, "abcd")
	press(p, uv.KeyBackspace) // → "abc"
	press(p, uv.KeyEnter)

	if gotReason != "abc" {
		t.Errorf("expected reason after backspace %q, got %q", "abc", gotReason)
	}
}

func TestFeedbackBackspace(t *testing.T) {
	p := New("some plan", "")
	var gotFeedback string
	p.OnSubmit(func(action Action, reason string) { gotFeedback = reason })

	typeText(p, "ab")
	press(p, uv.KeyBackspace) // → "a"
	press(p, uv.KeyEnter)

	if gotFeedback != "a" {
		t.Errorf("expected feedback after backspace %q, got %q", "a", gotFeedback)
	}
}

// TestSwitchPreservesPerOptionContent 验证切换选项时各自的输入内容独立保留。
func TestSwitchPreservesPerOptionContent(t *testing.T) {
	p := New("some plan", "")

	// Approve 输入 feedback
	typeText(p, "note for approve")
	// 切到 Deny 输入 reason
	press(p, uv.KeyDown)
	typeText(p, "rework")
	// 切回 Approve：feedback 保留，批准带回 feedback
	press(p, uv.KeyUp)

	var gotAction Action
	var gotReason string
	p.OnSubmit(func(action Action, reason string) {
		gotAction = action
		gotReason = reason
	})
	press(p, uv.KeyEnter)

	if gotAction != ActionApprove {
		t.Errorf("expected ActionApprove, got %v", gotAction)
	}
	if gotReason != "note for approve" {
		t.Errorf("expected preserved feedback, got %q", gotReason)
	}
	if p.reason != "rework" {
		t.Errorf("expected deny reason preserved, got %q", p.reason)
	}
}
