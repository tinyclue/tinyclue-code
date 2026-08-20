package loginpanel

import (
	"context"
	"errors"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

func renderAll(p *LoginPanel) string {
	return strings.Join(p.Render(core.Data{}).Lines, "\n")
}

// press 发送合成按键（ready 前置置位，确保每次按键都被处理）。
func press(p *LoginPanel, code rune) {
	p.ready = true
	p.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: code}))})
}

// send 发送非按键消息（OAuthDoneMsg 等）。
func send(p *LoginPanel, msg core.Msg) {
	p.DoUpdate(core.Data{Msg: msg})
}

// oauthTestProvider 返回一个确定存在于 config.Providers 的厂商（Name 用于回调断言，Label 用于渲染断言）。
func oauthTestProvider(t *testing.T) (name, label string) {
	t.Helper()
	p := oauthTestProviderInfo(t)
	return p.Name, p.Label
}

// oauthTestProviderName 返回一个确定存在于 config.Providers 的厂商名。
func oauthTestProviderName(t *testing.T) string {
	t.Helper()
	return oauthTestProviderInfo(t).Name
}

func oauthTestProviderInfo(t *testing.T) config.ProviderInfo {
	t.Helper()
	if len(config.Providers) == 0 {
		t.Skip("no providers in model cache")
	}
	return config.Providers[0]
}

// TestRenderSubscriptionFlow 验证订阅流程：选 "Use a subscription" → StepSelectProvider
// 只显示 oauth 厂商（Subscribe 模式列表 = oauth 集合 ∩ config.Providers）。
func TestRenderSubscriptionFlow(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New()
	_, label := oauthTestProvider(t)
	p.SetOAuthProviders(func() []string { return []string{oauthTestProviderName(t)} })

	// 默认 authMethodSel=1（API key）→ 上移到 0（subscription）→ Enter。
	press(p, uv.KeyUp)
	press(p, uv.KeyEnter)

	out := renderAll(p)
	if !strings.Contains(out, " Select provider to configure:") {
		t.Fatalf("subscription flow should reach provider select:\n%s", out)
	}
	if !strings.Contains(out, label) {
		t.Fatalf("subscribe list should include %q:\n%s", label, out)
	}
	if !strings.Contains(out, "  (1/1)") {
		t.Fatalf("subscribe list should have exactly 1 entry:\n%s", out)
	}
	if !strings.Contains(out, "• unconfigured") {
		t.Fatalf("subscribe list should show status:\n%s", out)
	}
}

// TestSubscribeBusy 验证选中订阅厂商 → OnSubscribe 触发、进入忙碌态渲染
// "opening browser for authorization" + "[c] cancel authorization"。
func TestSubscribeBusy(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New()
	provider, label := oauthTestProvider(t)
	p.SetOAuthProviders(func() []string { return []string{provider} })

	var subscribed string
	p.OnSubscribe(func(prov string) { subscribed = prov })

	press(p, uv.KeyUp)
	press(p, uv.KeyEnter) // → StepSelectProvider
	press(p, uv.KeyEnter) // → StepSubscribe + OnSubscribe

	if subscribed != provider {
		t.Fatalf("OnSubscribe provider = %q, want %q", subscribed, provider)
	}
	out := renderAll(p)
	for _, want := range []string{
		" Signing in to " + label,
		"opening browser for authorization",
		"[c] cancel authorization",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("subscribe busy render missing %q:\n%s", want, out)
		}
	}
}

// TestSubscribeCancel 验证忙碌中按 [c] → OnCancelSubscribe 触发（在途授权被中止）。
func TestSubscribeCancel(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New()
	provider := oauthTestProviderName(t)
	p.SetOAuthProviders(func() []string { return []string{provider} })

	var canceled string
	p.OnSubscribe(func(prov string) { _ = prov })
	p.OnCancelSubscribe(func(prov string) { canceled = prov })

	press(p, uv.KeyUp)
	press(p, uv.KeyEnter)
	press(p, uv.KeyEnter) // → StepSubscribe
	press(p, 'c')

	if canceled != provider {
		t.Fatalf("OnCancelSubscribe provider = %q, want %q", canceled, provider)
	}
}

// TestOAuthDoneClosesOnSuccess 验证成功/取消 → OnSubscribeDone 触发（命令侧据此关面板）。
func TestOAuthDoneClosesOnSuccess(t *testing.T) {
	p := New()
	provider := oauthTestProviderName(t)
	p.SetOAuthProviders(func() []string { return []string{provider} })
	press(p, uv.KeyUp)
	press(p, uv.KeyEnter)
	press(p, uv.KeyEnter) // → StepSubscribe

	var done string
	p.OnSubscribeDone(func(prov string) { done = prov })

	// 成功。
	send(p, core.OAuthDoneMsg{Provider: provider, Err: nil})
	if done != provider {
		t.Fatalf("OnSubscribeDone (success) = %q, want %q", done, provider)
	}
}

// TestOAuthDoneStaysOnFailure 验证失败 → 停留 provider 选择（仍是订阅模式 oauth 厂商列表），
// 错误已回显到 chat，面板不关闭。
func TestOAuthDoneStaysOnFailure(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New()
	provider, label := oauthTestProvider(t)
	p.SetOAuthProviders(func() []string { return []string{provider} })
	press(p, uv.KeyUp)
	press(p, uv.KeyEnter)
	press(p, uv.KeyEnter) // → StepSubscribe

	var done string
	p.OnSubscribeDone(func(prov string) { done = prov })

	send(p, core.OAuthDoneMsg{Provider: provider, Err: errors.New("access_denied")})
	if done != "" {
		t.Fatalf("OnSubscribeDone should NOT fire on failure, got %q", done)
	}
	out := renderAll(p)
	if !strings.Contains(out, " Select provider to configure:") {
		t.Fatalf("failure should return to provider select:\n%s", out)
	}
	// 列表仍是订阅模式：只含 oauth 厂商（本例 1 家），而非全量 api-key 列表。
	if !strings.Contains(out, label) {
		t.Fatalf("failure should stay on subscribe (oauth) list with %q:\n%s", label, out)
	}
	if !strings.Contains(out, "  (1/1)") {
		t.Fatalf("failure should keep the oauth subset list (not full list):\n%s", out)
	}
}

// TestOAuthDoneStaysOnCancel 验证用户取消（[c] → context.Canceled）→ 面板保持打开、回到
// provider 选择（仍是订阅模式 oauth 厂商列表），OnSubscribeDone 不触发（只有登录成功才关面板）。
func TestOAuthDoneStaysOnCancel(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	p := New()
	provider, label := oauthTestProvider(t)
	p.SetOAuthProviders(func() []string { return []string{provider} })
	press(p, uv.KeyUp)
	press(p, uv.KeyEnter)
	press(p, uv.KeyEnter) // → StepSubscribe

	var done string
	p.OnSubscribeDone(func(prov string) { done = prov })

	send(p, core.OAuthDoneMsg{Provider: provider, Err: context.Canceled})
	if done != "" {
		t.Fatalf("OnSubscribeDone should NOT fire on cancel, got %q", done)
	}
	out := renderAll(p)
	if !strings.Contains(out, " Select provider to configure:") {
		t.Fatalf("cancel should return to provider select, panel stays open:\n%s", out)
	}
	// 列表仍是订阅模式：只含 oauth 厂商（本例 1 家），而非全量 api-key 列表。
	if !strings.Contains(out, label) {
		t.Fatalf("cancel should stay on subscribe (oauth) list with %q:\n%s", label, out)
	}
	if !strings.Contains(out, "  (1/1)") {
		t.Fatalf("cancel should keep the oauth subset list (not full list):\n%s", out)
	}
}
