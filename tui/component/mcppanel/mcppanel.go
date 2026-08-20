// Package mcppanel provides a drill-down panel for inspecting MCP servers and
// triggering reconnect / authenticate / enable / disable actions.
//
// server 的 状态 + 传输类型（stdio/http）+ 是否已有已存 token 动态生成。
package mcppanel

import (
	"fmt"
	"sort"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/mcp"
	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// view 是面板的导航视图（ModelV2 只对容器直接子组件分发 DoUpdate，不递归 Children()，
// 故用单组件内部视图枚举，不用嵌套子组件）。
type view int

const (
	viewList view = iota
	viewMenu
	viewTools
	viewToolDetail
)

// menuItemKind 区分菜单项类型：动作（触发 fire）/ 下钻 View tools / 返回列表。
type menuItemKind int

const (
	menuAction menuItemKind = iota
	menuViewTools
	menuBack
)

type menuItem struct {
	label  string
	action mcp.Action
	kind   menuItemKind
}

// McpPanel is a scrollable list of MCP servers with per-server drill-down menus.
type McpPanel struct {
	items     []mcp.ServerSummary
	cursorSel int

	// 下钻导航状态（view 决定 Render 与按键分发）。
	v          view
	menuServer string
	menu       []menuItem
	menuSel    int
	tools      []core_types.AgentToolApi
	toolServer string
	toolsSel   int

	onAction     func(server string, action mcp.Action)
	onCancelAuth func(server string)
	onCancel     func()
	onRefreshed  func()
	// loadTools 加载指定 server 当前暴露的工具集（View tools 下钻，SetToolsLoader 注入）。
	loadTools func(server string) ([]core_types.AgentToolApi, error)
	// hasToken 报告 server 是否有已存 token（菜单构建时惰性查询一次，SetAuthState 注入）。
	hasToken func(server string) bool

	// busy 标记异步动作（reconnect/enable/disable/authenticate/clear-auth/reauth）进行中：锁定导航与动作键，
	// 仅放行 Esc/Enter 退出面板（动作在后台继续，退出不取消）与 [c] 中止进行中的交互授权。
	// 动作完成经 McpRefreshMsg 回到事件循环后清除。
	busy bool
	// busyName/busyAction 记录在途动作的目标与类型，用于渲染"in progress"提示与在途条目标记。
	busyName   string
	busyAction mcp.Action
	// reload 是面板刷新时重取数据的函数（MCP 摘要读取线程安全，可在 TUI goroutine 内安全调用）。
	reload func() []mcp.ServerSummary

	ready bool // prevents first-frame key echo

	*component.ComponentBase
}

// New creates an McpPanel from the given server summaries.
func New(summaries []mcp.ServerSummary) *McpPanel {
	return &McpPanel{
		items:         summaries,
		cursorSel:     0,
		ComponentBase: component.NewComponentBase(),
	}
}

// SetItems 刷新服务器列表（/mcp 面板动作后保持打开时调用），越界时夹紧 cursor。
func (p *McpPanel) SetItems(summaries []mcp.ServerSummary) {
	p.items = summaries
	if len(p.items) > 0 && p.cursorSel >= len(p.items) {
		p.cursorSel = len(p.items) - 1
	}
}

// SetAuthState 注入 token 存在性查询（/mcp 菜单构建时惰性检查磁盘，不在每帧 Render 调）。
func (p *McpPanel) SetAuthState(fn func(server string) bool) {
	p.hasToken = fn
}

// SetToolsLoader 注入工具集加载器（View tools 下钻时取该 server 当前暴露的工具）。
func (p *McpPanel) SetToolsLoader(fn func(server string) ([]core_types.AgentToolApi, error)) {
	p.loadTools = fn
}

// OnAction registers a callback fired when the user triggers an action
// (reconnect/enable/disable/authenticate/clear-auth/reauth) on a server. The panel stays
// open between actions; Esc/Enter at the list closes it.
func (p *McpPanel) OnAction(fn func(server string, action mcp.Action)) {
	p.onAction = fn
}

// OnCancel registers a callback fired when the user presses Enter/Esc.
func (p *McpPanel) OnCancel(fn func()) {
	p.onCancel = fn
}

// OnCancelAuth registers a callback fired when the user presses [c] to abort an in-progress
// interactive authorization (busy with authenticate/reauth). Success is reported by the aborted
// action itself; this callback may publish only on failure.
func (p *McpPanel) OnCancelAuth(fn func(server string)) {
	p.onCancelAuth = fn
}

// OnRefreshed registers a callback fired after an async action completes and the
// panel items are refreshed. Result text is published to the chat by the action
// producer (AutoCompleteDetail); this callback only refreshes footer/UI state.
func (p *McpPanel) OnRefreshed(fn func()) {
	p.onRefreshed = fn
}

// SetReload injects the function used to re-fetch panel data on refresh.
// MCP summaries are read thread-safely, so reload may be called on the TUI goroutine.
func (p *McpPanel) SetReload(fn func() []mcp.ServerSummary) {
	p.reload = fn
}

// DoBefore implements core.Component.
func (p *McpPanel) DoBefore(core.Data) error { return nil }

// DoUpdate handles keyboard navigation, actions, and async action completion.
func (p *McpPanel) DoUpdate(data core.Data) error {
	switch m := data.Msg.(type) {
	case core.KeyPressMsg:
		// 只吞首帧按键（打开面板的那次击键），其他消息类型不受 ready 影响。
		if !p.ready {
			p.ready = true
			return nil
		}
		p.handleInput(m)
	case core.McpRefreshMsg:
		p.handleRefresh()
	}
	return nil
}

func (p *McpPanel) handleInput(key core.KeyPressMsg) {
	// 操作进行中：锁定导航与动作键，仅放行 Esc 退出面板（动作在后台继续，退出不取消，
	// 结果由 OnAction 生产者发布到 chat，不依赖面板存活）与 [c] 中止进行中的交互授权。
	// Enter 在 busy 时忽略：它已被用于触发在途动作，再按不应关闭面板。
	if p.busy {
		if key.MatchString("c", "C") {
			// 交互授权（authenticate/reauth 会开浏览器等回调）可被中止：本地先把在途动作切为
			// "取消"态（渲染"正在取消授权"），真实结果由被中止的动作（返回 ErrAuthCancelled）
			// 经 McpRefreshMsg 回传后统一刷新面板。
			if p.cancelable() && p.onCancelAuth != nil {
				p.busyAction = mcp.ActionCancelAuth
				p.onCancelAuth(p.busyName)
			}
			return
		}
		if key.MatchString("esc") {
			if p.onCancel != nil {
				p.onCancel()
			}
		}
		return
	}

	// 非 busy：按当前视图分发导航/动作按键。
	switch p.v {
	case viewList:
		p.handleListKey(key)
	case viewMenu:
		p.handleMenuKey(key)
	case viewTools:
		p.handleToolsKey(key)
	case viewToolDetail:
		if key.MatchString("esc") {
			p.v = viewTools
		}
	}
}

func (p *McpPanel) handleListKey(key core.KeyPressMsg) {
	n := len(p.items)
	switch {
	case key.MatchString("up", "ctrl+p"):
		if n > 0 {
			p.cursorSel--
			if p.cursorSel < 0 {
				p.cursorSel = n - 1
			}
		}
	case key.MatchString("down", "ctrl+n"):
		if n > 0 {
			p.cursorSel++
			if p.cursorSel >= n {
				p.cursorSel = 0
			}
		}
	case key.MatchString("enter"):
		if n > 0 {
			p.openMenu(p.items[p.cursorSel])
		} else if p.onCancel != nil {
			p.onCancel()
		}
	case key.MatchString("esc"):
		if p.onCancel != nil {
			p.onCancel()
		}
	}
}

func (p *McpPanel) handleMenuKey(key core.KeyPressMsg) {
	n := len(p.menu)
	switch {
	case key.MatchString("up", "ctrl+p"):
		if n > 0 {
			p.menuSel--
			if p.menuSel < 0 {
				p.menuSel = n - 1
			}
		}
	case key.MatchString("down", "ctrl+n"):
		if n > 0 {
			p.menuSel++
			if p.menuSel >= n {
				p.menuSel = 0
			}
		}
	case key.MatchString("enter"):
		if n > 0 {
			p.activate(p.menu[p.menuSel])
		}
	case key.MatchString("esc"):
		p.v = viewList
	}
}

func (p *McpPanel) handleToolsKey(key core.KeyPressMsg) {
	n := len(p.tools)
	switch {
	case key.MatchString("up", "ctrl+p"):
		if n > 0 {
			p.toolsSel--
			if p.toolsSel < 0 {
				p.toolsSel = n - 1
			}
		}
	case key.MatchString("down", "ctrl+n"):
		if n > 0 {
			p.toolsSel++
			if p.toolsSel >= n {
				p.toolsSel = 0
			}
		}
	case key.MatchString("enter"):
		if n > 0 {
			p.v = viewToolDetail
		}
	case key.MatchString("esc"):
		p.v = viewMenu
		p.rebuildMenu()
	}
}

// openMenu 进入指定 server 的菜单视图（重新构建菜单选项）。
func (p *McpPanel) openMenu(sum mcp.ServerSummary) {
	p.menuServer = sum.Name
	p.menu = p.buildMenu(sum)
	p.menuSel = 0
	p.v = viewMenu
}

// activate 执行当前选中的菜单项（动作 / 下钻 View tools / 返回列表）。
func (p *McpPanel) activate(it menuItem) {
	switch it.kind {
	case menuAction:
		p.fire(it.action)
	case menuViewTools:
		p.loadToolsFor(p.menuServer)
	case menuBack:
		p.v = viewList
	}
}

// fire 发起一个后台动作（busy=true，结果经 McpRefreshMsg 回传刷新）。
func (p *McpPanel) fire(action mcp.Action) {
	if p.menuServer == "" {
		return
	}
	if p.onAction != nil {
		p.busy = true
		p.busyName = p.menuServer
		p.busyAction = action
		p.onAction(p.menuServer, action)
	}
}

// loadToolsFor 加载 View tools 数据：成功且有工具 → 进入 tools 视图；失败 / 空 → 留在菜单。
// 结果回显统一由动作生产者发布到 chat，这里不做面板内 notice 提示。
func (p *McpPanel) loadToolsFor(server string) {
	if p.loadTools == nil {
		return
	}
	tools, err := p.loadTools(server)
	if err != nil || len(tools) == 0 {
		return
	}
	p.toolServer = server
	p.tools = tools
	p.toolsSel = 0
	p.v = viewTools
}

// cancelable 报告当前在途动作是否可被 [c] 中止（会打开浏览器等回调的交互授权）。
// Reconnect 已是纯重连（不开浏览器），不可取消。
func (p *McpPanel) cancelable() bool {
	return p.busyAction == mcp.ActionAuthenticate || p.busyAction == mcp.ActionReauth
}

// handleRefresh 处理异步动作完成消息：清 busy、重取数据刷新当前视图，并通知调用方刷新 UI。
// 结果文案由动作生产者发布到 chat（AutoCompleteDetail），本方法只做面板状态刷新。
func (p *McpPanel) handleRefresh() {
	p.busy = false
	if p.reload != nil {
		p.SetItems(p.reload())
	}
	p.rebuildCurrentView()
	if p.onRefreshed != nil {
		p.onRefreshed()
	}
}

// rebuildCurrentView 刷新后按当前视图重建数据：菜单重建选项；tools/detail 重取工具
// （失败/空回菜单）。
func (p *McpPanel) rebuildCurrentView() {
	switch p.v {
	case viewMenu:
		p.rebuildMenu()
	case viewTools, viewToolDetail:
		p.reloadTools()
	}
}

// rebuildMenu 用最新 items 重建当前 server 的菜单（状态变化如 needs-auth→connected 时
// Authenticate↔Reconnect 选项随之切换）；server 已从列表消失则回列表。
func (p *McpPanel) rebuildMenu() {
	sum := p.summaryByName(p.menuServer)
	if sum == nil {
		p.v = viewList
		return
	}
	p.menu = p.buildMenu(*sum)
	if p.menuSel >= len(p.menu) {
		p.menuSel = len(p.menu) - 1
	}
	if p.menuSel < 0 {
		p.menuSel = 0
	}
}

// reloadTools 重取当前 tools 视图的 server 工具集；失败/空回菜单。
func (p *McpPanel) reloadTools() {
	if p.loadTools == nil {
		return
	}
	tools, err := p.loadTools(p.toolServer)
	if err != nil || len(tools) == 0 {
		p.v = viewMenu
		p.rebuildMenu()
		return
	}
	p.tools = tools
	if p.toolsSel >= len(p.tools) {
		p.toolsSel = len(p.tools) - 1
	}
	if p.toolsSel < 0 {
		p.toolsSel = 0
	}
}

// needs-auth 只给 Authenticate 不给 Reconnect；Reconnect 是纯重连）。hasToken 在此只查一次。
func (p *McpPanel) buildMenu(sum mcp.ServerSummary) []menuItem {
	var menu []menuItem
	add := func(label string, action mcp.Action, kind menuItemKind) {
		menu = append(menu, menuItem{label: label, action: action, kind: kind})
	}
	hasToken := p.hasToken != nil && p.hasToken(sum.Name)

	switch {
	case sum.Status == mcp.StatusDisabled:
		add("Enable", mcp.ActionEnable, menuAction)
	case !sum.HTTP:
		// stdio：无 OAuth，只给 View tools / Reconnect / Disable。
		if sum.Status == mcp.StatusConnected && sum.ToolCount > 0 {
			add("View tools", 0, menuViewTools)
		}
		add("Reconnect", mcp.ActionReconnect, menuAction)
		add("Disable", mcp.ActionDisable, menuAction)
	case sum.Status == mcp.StatusNeedsAuth:
		// needs-auth：只给 Authenticate（交互授权），不给 Reconnect。
		add("Authenticate", mcp.ActionAuthenticate, menuAction)
		if hasToken {
			add("Clear authentication", mcp.ActionClearAuth, menuAction)
		}
	default:
		// http connected / error / connecting。
		if sum.Status == mcp.StatusConnected && sum.ToolCount > 0 {
			add("View tools", 0, menuViewTools)
		}
		if hasToken {
			add("Re-authenticate", mcp.ActionReauth, menuAction)
			add("Clear authentication", mcp.ActionClearAuth, menuAction)
		}
		add("Reconnect", mcp.ActionReconnect, menuAction)
		add("Disable", mcp.ActionDisable, menuAction)
	}
	add("Back", 0, menuBack)
	return menu
}

// summaryByName 按名取最新摘要（nil 表示 server 已从列表消失）。
func (p *McpPanel) summaryByName(name string) *mcp.ServerSummary {
	for i := range p.items {
		if p.items[i].Name == name {
			return &p.items[i]
		}
	}
	return nil
}

// ── 渲染 ──

// Render implements core.Component.
func (p *McpPanel) Render(data core.Data) core.View {
	switch p.v {
	case viewMenu:
		return core.View{Lines: p.renderMenu()}
	case viewTools:
		return core.View{Lines: p.renderTools()}
	case viewToolDetail:
		return core.View{Lines: p.renderToolDetail()}
	default:
		return core.View{Lines: p.renderList()}
	}
}

// width 返回面板渲染宽度（TerminalContext 全局注入，测试可 SetWidth 覆盖）。
func (p *McpPanel) width() int {
	width := terminal.DefaultTerminalContext.GetWidth()
	if width < 10 {
		width = 10
	}
	return width
}

func (p *McpPanel) renderList() []string {
	width := p.width()
	var lines []string
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " MCP servers:")
	lines = append(lines, "")

	total := len(p.items)
	if total == 0 {
		lines = append(lines, "  (no MCP servers configured — add ~/.tinyclue/mcp.json or a project .mcp.json)")
		lines = append(lines, "")
		lines = append(lines, "  [Enter/Esc to exit]")
		return lines
	}

	start, end := scrollWindow(p.cursorSel, total, 10)
	for i := start; i < end; i++ {
		item := p.items[i]
		cursor := " "
		if i == p.cursorSel {
			cursor = "→"
		}
		label := fmt.Sprintf("%s %s  %s", cursor, item.Name, statusLine(item))
		if i == p.cursorSel {
			label = types.FgLightBlue + types.Bold + label + types.Reset
		}
		lines = append(lines, label)
	}

	lines = append(lines, fmt.Sprintf("  (%d/%d)", p.cursorSel+1, total))
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  ↑↓ navigate · Enter details · Esc exit"+types.Reset)
	return lines
}

func (p *McpPanel) renderMenu() []string {
	width := p.width()
	var lines []string
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")

	if sum := p.summaryByName(p.menuServer); sum != nil {
		lines = append(lines, " "+p.menuServer+"  "+statusLine(*sum))
	} else {
		lines = append(lines, " "+p.menuServer)
	}
	lines = append(lines, "")

	for i, it := range p.menu {
		cursor := " "
		if i == p.menuSel {
			cursor = "→"
		}
		label := fmt.Sprintf("%s %s", cursor, it.label)
		switch {
		case p.busy && it.kind == menuAction && it.action == p.busyAction:
			// 在途动作：目标项尾部追加进行中提示（"  … reconnecting" 等）。
			label += "  … " + p.busyVerb()
			// 交互授权进行中：追加 [c] 中止提示（按下后 busyAction 切为 CancelAuth，随即消失）。
			if p.cancelable() {
				label += types.FgYellow + "  [c] cancel authorization" + types.Reset
			}
		case p.busy:
			// 在途动作：其余项变暗，提示当前不可操作。
			label = types.FgGray + label + types.Reset
		}
		if i == p.menuSel {
			label = types.FgLightBlue + types.Bold + label + types.Reset
		}
		lines = append(lines, label)
	}

	lines = append(lines, fmt.Sprintf("  (%d/%d)", p.menuSel+1, len(p.menu)))
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  ↑↓ navigate · Enter select · Esc back"+types.Reset)
	return lines
}

func (p *McpPanel) renderTools() []string {
	width := p.width()
	var lines []string
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")
	lines = append(lines, " "+p.toolServer+" tools:")
	lines = append(lines, "")

	total := len(p.tools)
	start, end := scrollWindow(p.toolsSel, total, 10)
	for i := start; i < end; i++ {
		tool := p.tools[i].GetTool()
		cursor := " "
		if i == p.toolsSel {
			cursor = "→"
		}
		name := p.displayToolName(tool.Name)
		label := fmt.Sprintf("%s %s", cursor, name)
		if tool.Description != "" {
			label += " — " + tool.Description
		}
		if i == p.toolsSel {
			label = types.FgLightBlue + types.Bold + label + types.Reset
		}
		lines = append(lines, label)
	}

	lines = append(lines, fmt.Sprintf("  (%d/%d)", p.toolsSel+1, total))
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  ↑↓ navigate · Enter details · Esc back"+types.Reset)
	return lines
}

func (p *McpPanel) renderToolDetail() []string {
	width := p.width()
	var lines []string
	lines = append(lines, strings.Repeat("─", width))
	lines = append(lines, "")

	if len(p.tools) == 0 || p.toolsSel >= len(p.tools) {
		return lines
	}
	tool := p.tools[p.toolsSel].GetTool()
	lines = append(lines, fmt.Sprintf(" %s / %s", p.toolServer, p.displayToolName(tool.Name)))
	lines = append(lines, "")

	if tool.Description != "" {
		for _, l := range types.WrapTextWithAnsi(tool.Description, width-2) {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, "")
	}

	lines = p.renderInputSchema(lines, tool.Parameters, width)
	lines = append(lines, "")
	lines = append(lines, types.FgGray+"  Esc back"+types.Reset)
	return lines
}

// displayToolName 去掉桥接前缀 mcp__<server>__，显示原始工具名。
func (p *McpPanel) displayToolName(full string) string {
	prefix := "mcp__" + core_tools.NormalizeNameForMCP(p.toolServer) + "__"
	if strings.HasPrefix(full, prefix) {
		return strings.TrimPrefix(full, prefix)
	}
	return full
}

// renderInputSchema 渲染工具参数 schema：type 行、required 行、按 key 排序的属性
// "<name> (<type>) [required]" + 描述 + 嵌套细节。
func (p *McpPanel) renderInputSchema(lines []string, params apitypes.Parameters, width int) []string {
	lines = append(lines, "  input schema:")
	lines = append(lines, "    type: "+params.Type)
	if len(params.Required) > 0 {
		lines = append(lines, "    required: "+strings.Join(params.Required, ", "))
	}
	keys := sortedKeys(params.Properties)
	if len(keys) == 0 {
		lines = append(lines, "    (no properties)")
		return lines
	}
	for _, k := range keys {
		prop := params.Properties[k]
		req := ""
		for _, r := range params.Required {
			if r == k {
				req = " [required]"
				break
			}
		}
		lines = append(lines, fmt.Sprintf("    %s (%s)%s", k, prop.Type, req))
		lines = p.renderSchemaItem(lines, prop, 6, 1, width)
	}
	return lines
}

// renderSchemaItem 渲染单个 schema 项的嵌套细节（描述 / Items / Properties / Enum / Default），
// 深度上限 2 层防超高。调用方已输出 "<name> (<type>)" 行。
func (p *McpPanel) renderSchemaItem(lines []string, item apitypes.SchemaItem, indent, depth, width int) []string {
	pad := strings.Repeat(" ", indent)
	if item.Description != "" {
		for _, l := range types.WrapTextWithAnsi(item.Description, width-indent-2) {
			lines = append(lines, pad+l)
		}
	}
	if item.Items != nil {
		lines = append(lines, pad+"items:")
		lines = append(lines, pad+"  type: "+item.Items.Type)
		if depth < 2 {
			lines = p.renderSchemaItem(lines, *item.Items, indent+4, depth+1, width)
		}
	}
	if len(item.Properties) > 0 {
		lines = append(lines, pad+"properties:")
		for _, k := range sortedKeys(item.Properties) {
			sub := item.Properties[k]
			lines = append(lines, fmt.Sprintf("%s  %s (%s)", pad, k, sub.Type))
			if depth < 2 {
				lines = p.renderSchemaItem(lines, sub, indent+4, depth+1, width)
			}
		}
	}
	if len(item.Enum) > 0 {
		vals := make([]string, 0, len(item.Enum))
		for _, e := range item.Enum {
			vals = append(vals, fmt.Sprintf("%v", e))
		}
		lines = append(lines, pad+"enum: "+strings.Join(vals, ", "))
	}
	if item.Default != nil {
		lines = append(lines, pad+"default: "+fmt.Sprintf("%v", item.Default))
	}
	return lines
}

// sortedKeys 返回 map 的排序 key 列表（nil-safe）。
func sortedKeys(m map[string]apitypes.SchemaItem) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// busyVerb 渲染在途操作的进行中提示（"reconnecting" 等），追加在目标条目尾部（条目本身已含 server 名）。
func (p *McpPanel) busyVerb() string {
	switch p.busyAction {
	case mcp.ActionReconnect:
		return "reconnecting"
	case mcp.ActionEnable:
		return "enabling"
	case mcp.ActionDisable:
		return "disabling"
	case mcp.ActionAuthenticate:
		return "opening browser for authorization — complete it there"
	case mcp.ActionClearAuth:
		return "clearing authentication"
	case mcp.ActionReauth:
		return "re-authenticating"
	case mcp.ActionCancelAuth:
		return "canceling authorization"
	}
	return "working"
}

// scrollWindow 计算 maxDisplay 滚动窗口 [start, end)（选中项居中，边缘贴边）。
func scrollWindow(sel, total, maxDisplay int) (int, int) {
	start, end := 0, total
	if total > maxDisplay {
		half := maxDisplay / 2
		start = sel - half
		if start < 0 {
			start = 0
		}
		end = start + maxDisplay
		if end > total {
			end = total
			start = total - maxDisplay
		}
	}
	return start, end
}

func statusLine(s mcp.ServerSummary) string {
	switch s.Status {
	case mcp.StatusConnected:
		return types.FgGreen + fmt.Sprintf("● connected (%d tools)", s.ToolCount) + types.Reset
	case mcp.StatusConnecting:
		return "… connecting"
	case mcp.StatusError:
		err := s.Err
		if len(err) > 80 {
			err = err[:80] + "…"
		}
		return types.FgRed + "✗ error: " + err + types.Reset
	case mcp.StatusNeedsAuth:
		return types.FgYellow + "⚠ needs auth" + types.Reset
	case mcp.StatusDisabled:
		return types.FgGray + "— disabled" + types.Reset
	}
	return ""
}
