// Package loginpanel provides the login panel component for managing
// agent model authentication (API keys) through a multi-step UI.
package loginpanel

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"strings"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// LoginStep represents which step of the login flow is active.
type LoginStep int

const (
	StepAuthMethod LoginStep = iota
	StepSelectProvider
	StepEnterKey
	StepSubscribe // 订阅登录：浏览器授权进行中（忙碌态）
)

// auth method items
var authMethods = []string{"Use a subscription", "Use an API key"}

// LoginPanel is a multi-step component for configuring provider authentication.
type LoginPanel struct {
	step             LoginStep
	authMethodSel    int    // 0 = subscription, 1 = API key
	providerSel      int    // cursor index in provider list
	filterText       string // search text for filtering providers
	apiKeyText       []rune // characters typed in the API key input
	selectedProvider *config.ProviderInfo

	// subscribe 区分 provider 列表来源：true=订阅模式（oauthProviders() 子集），false=api-key 全量。
	subscribe           bool
	subscribingProvider string // StepSubscribe 在途授权的 provider
	// oauthProviders 订阅模式的候选 provider 列表（SetOAuthProviders 注入）。
	oauthProvidersFn func() []string

	onSubmit          func(provider string, apiKey string)
	onSubscribe       func(provider string)
	onCancelSubscribe func(provider string)
	onSubscribeDone   func(provider string)
	onCancel          func()

	ready bool // 是否已过首个事件循环，防止创建时被同一次 KeyPressMsg 误触

	*component.ComponentBase
}

// New creates a new LoginPanel at the auth method selection step.
func New() *LoginPanel {
	return &LoginPanel{
		step:          StepAuthMethod,
		authMethodSel: 1, // default to "Use an API key"
		ComponentBase: component.NewComponentBase(),
	}
}

// OnSubmit registers a callback for when credentials are submitted.
// The callback receives provider name and API key.
func (lp *LoginPanel) OnSubmit(fn func(provider string, apiKey string)) {
	lp.onSubmit = fn
}

// SetOAuthProviders 注入订阅模式下的候选 provider 列表（subscription.ProviderNames 等）。
func (lp *LoginPanel) SetOAuthProviders(fn func() []string) {
	lp.oauthProvidersFn = fn
}

// OnSubscribe registers a callback fired when the user selects a subscription
// provider (starts the browser authorization in the background).
func (lp *LoginPanel) OnSubscribe(fn func(provider string)) {
	lp.onSubscribe = fn
}

// OnCancelSubscribe registers a callback fired when the user presses [c]/Esc to
// abort an in-progress browser authorization.
func (lp *LoginPanel) OnCancelSubscribe(fn func(provider string)) {
	lp.onCancelSubscribe = fn
}

// OnSubscribeDone registers a callback fired when the subscription login finishes
// (success or cancel). The command side uses it to close the panel.
func (lp *LoginPanel) OnSubscribeDone(fn func(provider string)) {
	lp.onSubscribeDone = fn
}

// OnCancel registers a callback for when the panel is cancelled.
func (lp *LoginPanel) OnCancel(fn func()) {
	lp.onCancel = fn
}

// DoBefore implements core.Component.
func (lp *LoginPanel) DoBefore(core.Data) error { return nil }

// DoUpdate handles keyboard input for the current step.
func (lp *LoginPanel) DoUpdate(data core.Data) error {
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		// 只吞首帧按键（打开面板的那次击键），其他消息类型不受 ready 影响。
		if !lp.ready {
			lp.ready = true
			return nil
		}
		switch lp.step {
		case StepAuthMethod:
			lp.handleAuthMethodInput(m)
		case StepSelectProvider:
			lp.handleProviderSelectInput(m)
		case StepEnterKey:
			lp.handleApiKeyInput(m)
		case StepSubscribe:
			lp.handleSubscribeInput(m)
		}
	case core.PasteMsg:
		if lp.step == StepEnterKey {
			for _, r := range m.String() {
				if !unicode.IsControl(r) {
					lp.apiKeyText = append(lp.apiKeyText, r)
				}
			}
		}
	case core.OAuthDoneMsg:
		lp.handleOAuthDone(m)
	}
	return nil
}

func (lp *LoginPanel) handleAuthMethodInput(key core.KeyPressMsg) {
	switch {
	case key.MatchString("up", "ctrl+p"):
		if lp.authMethodSel > 0 {
			lp.authMethodSel--
		}
	case key.MatchString("down", "ctrl+n"):
		if lp.authMethodSel < len(authMethods)-1 {
			lp.authMethodSel++
		}
	case key.MatchString("enter"):
		switch lp.authMethodSel {
		case 0:
			// Use a subscription → go to provider selection（列表限 oauth 厂商）
			lp.step = StepSelectProvider
			lp.providerSel = 0
			lp.subscribe = true
			lp.filterText = ""
		case 1:
			// Use an API key → go to provider selection
			lp.step = StepSelectProvider
			lp.providerSel = 0
			lp.subscribe = false
			lp.filterText = ""
		}
	case key.MatchString("esc"):
		lp.cancel()
	}
}

func (lp *LoginPanel) handleProviderSelectInput(key core.KeyPressMsg) {
	n := len(lp.filteredProviders())
	if n == 0 {
		return
	}

	switch {
	case key.MatchString("up", "ctrl+p"):
		lp.providerSel--
		if lp.providerSel < 0 {
			lp.providerSel = n - 1
		}
	case key.MatchString("down", "ctrl+n"):
		lp.providerSel++
		if lp.providerSel >= n {
			lp.providerSel = 0
		}
	case key.MatchString("enter"):
		providers := lp.filteredProviders()
		if lp.providerSel >= 0 && lp.providerSel < len(providers) {
			p := providers[lp.providerSel]
			if lp.subscribe {
				// 订阅模式：选中 → 进入忙碌态并触发浏览器授权。
				lp.subscribingProvider = p.Name
				lp.step = StepSubscribe
				if lp.onSubscribe != nil {
					lp.onSubscribe(p.Name)
				}
				return
			}
			lp.selectedProvider = &p
			lp.step = StepEnterKey
			lp.apiKeyText = nil
			lp.apiKeyText = make([]rune, 0)
		}
	case key.MatchString("esc"):
		lp.step = StepAuthMethod
		lp.filterText = ""
		lp.subscribe = false
	case key.MatchString("backspace"):
		if len(lp.filterText) > 0 {
			lp.filterText = lp.filterText[:len(lp.filterText)-1]
			lp.providerSel = 0
		}
	default:
		if key.Key().Text != "" {
			for _, r := range key.Key().Text {
				if !unicode.IsControl(r) {
					lp.filterText += string(r)
					lp.providerSel = 0
				}
			}
		}
	}
}

func (lp *LoginPanel) handleApiKeyInput(key core.KeyPressMsg) {
	switch {
	case key.MatchString("enter"):
		if lp.selectedProvider != nil && len(lp.apiKeyText) > 0 {
			apiKey := string(lp.apiKeyText)
			if lp.onSubmit != nil {
				lp.onSubmit(lp.selectedProvider.Name, apiKey)
			}
		}
	case key.MatchString("esc"):
		lp.cancel()
	case key.MatchString("backspace"):
		if len(lp.apiKeyText) > 0 {
			lp.apiKeyText = lp.apiKeyText[:len(lp.apiKeyText)-1]
		}
	default:
		if key.Key().Text != "" {
			for _, r := range key.Key().Text {
				if !unicode.IsControl(r) {
					lp.apiKeyText = append(lp.apiKeyText, r)
				}
			}
		}
	}
}

// handleSubscribeInput 处理订阅登录忙碌态的按键：[c]/Esc 中止在途浏览器授权。
func (lp *LoginPanel) handleSubscribeInput(key core.KeyPressMsg) {
	switch {
	case key.MatchString("c", "C"):
		if lp.onCancelSubscribe != nil {
			lp.onCancelSubscribe(lp.subscribingProvider)
		}
	case key.MatchString("esc"):
		if lp.onCancelSubscribe != nil {
			lp.onCancelSubscribe(lp.subscribingProvider)
		}
		lp.cancel()
	}
}

// handleOAuthDone 处理订阅登录异步完成消息：成功（Err==nil）→ 通知命令侧关面板（onSubscribeDone）；
// 失败或被用户取消（[c]）→ 回 provider 选择列表停留（取消/错误文案已由命令侧发布到 chat，
// 面板不关，可换厂商重试或 [Esc] 退出）。
func (lp *LoginPanel) handleOAuthDone(m core.OAuthDoneMsg) {
	if m.Err != nil {
		// 取消/失败：停留 provider 选择，保持 subscribe=true（仍是订阅模式的 oauth 厂商列表，
		// 否则列表会跳回全量 api-key 厂商）。
		lp.step = StepSelectProvider
		lp.filterText = ""
		return
	}
	if lp.onSubscribeDone != nil {
		lp.onSubscribeDone(m.Provider)
	}
}

// cancel fires the onCancel callback.
func (lp *LoginPanel) cancel() {
	if lp.onCancel != nil {
		lp.onCancel()
	}
}

// subscribeProviders 返回订阅模式的候选 provider 列表：config.Providers 中 Name 属于
// oauthProviders() 集合的子集（保留标签/默认勾选等展示信息）。
func (lp *LoginPanel) subscribeProviders() []config.ProviderInfo {
	if lp.oauthProvidersFn == nil {
		return nil
	}
	set := make(map[string]struct{})
	for _, name := range lp.oauthProvidersFn() {
		set[name] = struct{}{}
	}
	var result []config.ProviderInfo
	for _, p := range config.Providers {
		if _, ok := set[p.Name]; ok {
			result = append(result, p)
		}
	}
	return result
}

// filteredProviders returns the provider list filtered by filterText.
func (lp *LoginPanel) filteredProviders() []config.ProviderInfo {
	base := config.Providers
	if lp.subscribe {
		base = lp.subscribeProviders()
	}
	if lp.filterText == "" {
		return base
	}
	lower := strings.ToLower(lp.filterText)
	var result []config.ProviderInfo
	for _, p := range base {
		if strings.Contains(strings.ToLower(p.Label), lower) {
			result = append(result, p)
		}
	}
	return result
}

// Render implements core.Component.
func (lp *LoginPanel) Render(data core.Data) core.View {
	width := terminal.DefaultTerminalContext.GetWidth()
	if width < 10 {
		width = 10
	}

	var lines []string
	switch lp.step {
	case StepAuthMethod:
		lines = lp.renderAuthMethod(width)
	case StepSelectProvider:
		lines = lp.renderProviderSelect(width)
	case StepEnterKey:
		lines = lp.renderApiKeyEntry(width)
	case StepSubscribe:
		lines = lp.renderSubscribe(width)
	}
	return core.View{Lines: lines}
}

// ── Step 4: Subscription (browser authorization busy) ──

func (lp *LoginPanel) renderSubscribe(width int) []string {
	providerName := lp.subscribingProvider
	if p := providerByName(lp.subscribingProvider); p != nil {
		providerName = p.Label
	}
	var lines []string
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf(" Signing in to %s (subscription)", providerName))
	lines = append(lines, "")
	lines = append(lines, types.FgYellow+"  … opening browser for authorization — complete it there"+types.Reset)
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  [c] cancel authorization · [Esc] exit"+types.Reset)
	return lines
}

// providerByName 在 config.Providers 中按 Name 查找（展示用，找不到返回 nil）。
func providerByName(name string) *config.ProviderInfo {
	for i := range config.Providers {
		if config.Providers[i].Name == name {
			return &config.Providers[i]
		}
	}
	return nil
}

// ── Step 1: Auth Method Selection ──

func (lp *LoginPanel) renderAuthMethod(width int) []string {
	var lines []string
	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " Select authentication method:")
	lines = append(lines, "")
	for i, method := range authMethods {
		cursor := " "
		if i == lp.authMethodSel {
			cursor = "→"
		}
		line := fmt.Sprintf(" %s %s", cursor, method)
		if i == lp.authMethodSel {
			line = types.FgLightBlue + types.Bold + line + types.Reset
		}
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, types.FgGray+" ↑↓ navigate  enter select  escape/ctrl+c cancel"+types.Reset)
	return lines
}

// ── Step 2: Provider Selection ──

func (lp *LoginPanel) renderProviderSelect(width int) []string {
	var lines []string
	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " Select provider to configure:")
	lines = append(lines, "")

	providers := lp.filteredProviders()
	total := len(providers)

	if total == 0 {
		lines = append(lines, "  (no matches)")
		lines = append(lines, "")
		lines = append(lines, "  [Type to search] [Esc to cancel]")
		return lines
	}

	defaultProvider := config.Cnf.DefaultProvider()
	maxDisplay := 10
	start, end := 0, total
	if total > maxDisplay {
		half := maxDisplay / 2
		start = lp.providerSel - half
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
		p := providers[i]
		cursor := " "
		if i == lp.providerSel {
			cursor = "→"
		}

		// Checkmark for default provider
		check := " "
		if p.Name == defaultProvider {
			check = "✓"
		}

		// configured/unconfigured status
		_, hasAuth := config.Cnf.AuthFor(p.Name)
		status := "• unconfigured"
		if hasAuth {
			status = "• configured"
		}

		// Build label with padding for alignment
		label := fmt.Sprintf("%s %-30s %s %s", cursor, p.Label, check, status)
		if i == lp.providerSel {
			label = types.FgLightBlue + types.Bold + label + types.Reset
		}
		lines = append(lines, label)
	}

	// Page indicator
	lines = append(lines, fmt.Sprintf("  (%d/%d)", lp.providerSel+1, total))

	return lines
}

// ── Step 3: API Key Entry ──

func (lp *LoginPanel) renderApiKeyEntry(width int) []string {
	providerName := ""
	if lp.selectedProvider != nil {
		providerName = lp.selectedProvider.Label
	}

	var lines []string
	// Top divider
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf(" Login to %s", providerName))
	lines = append(lines, "")
	lines = append(lines, " Enter API key:")

	// Input box with rounded corners (40% of terminal width)
	inputWidth := int(float64(width) * 0.4)
	if inputWidth < 20 {
		inputWidth = 20
	}

	// Top border: ╭───╮
	topBorder := "  " + "╭" + strings.Repeat("─", inputWidth) + "╮"
	lines = append(lines, topBorder)

	// API key text line with cursor
	keyText := string(lp.apiKeyText)
	cursorCol := runewidth.StringWidth(keyText)

	// Truncate text to fit in the input box
	displayText := keyText
	if cursorCol > inputWidth {
		// Scroll: show the end of the text
		runes := []rune(keyText)
		visibleEnd := len(runes)
		visibleStart := 0
		for i := len(runes) - 1; i >= 0; i-- {
			w := runewidth.StringWidth(string(runes[i:visibleEnd]))
			if w > inputWidth {
				visibleStart = i + 1
				break
			}
		}
		displayText = string(runes[visibleStart:visibleEnd])
		cursorCol = inputWidth // cursor at the right edge
	}

	padding := inputWidth - runewidth.StringWidth(displayText)
	inputLine := "  " + "│" + displayText + strings.Repeat(" ", padding) + "│"

	// Insert cursor marker (use []rune to safely handle multi-byte characters)
	if lp.IsFocused() {
		runes := []rune(inputLine)
		markerIdx := 3 + cursorCol // 3 for "  │" prefix
		if markerIdx >= len(runes) {
			// Append at end
			before := string(runes[:len(runes)-1]) // drop the right │
			inputLine = before + core.CURSOR_MARKER + types.CursorStyle + " " + types.Reset + "│"
		} else {
			before := string(runes[:markerIdx])
			ch := string(runes[markerIdx])
			after := string(runes[markerIdx+1:])
			cursor := types.CursorStyle + ch + types.Reset
			inputLine = before + core.CURSOR_MARKER + cursor + after
		}
	}

	lines = append(lines, inputLine)

	// Bottom border: ╰───╯
	bottomBorder := "  " + "╰" + strings.Repeat("─", inputWidth) + "╯"
	lines = append(lines, bottomBorder)
	lines = append(lines, "")
	lines = append(lines, types.FgGray+" (esc to cancel, enter to submit)"+types.Reset)

	return lines
}
