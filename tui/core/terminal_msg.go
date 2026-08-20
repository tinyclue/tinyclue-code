package core

import uv "github.com/charmbracelet/ultraviolet"

type Msg = uv.Event

// 按键
type KeyPressMsg uv.KeyPressEvent
type KeyReleaseMsg uv.KeyReleaseEvent

func (k KeyPressMsg) String() string               { return uv.KeyPressEvent(k).String() }
func (k KeyPressMsg) Keystroke() string            { return uv.KeyPressEvent(k).Keystroke() }
func (k KeyPressMsg) MatchString(s ...string) bool { return uv.KeyPressEvent(k).MatchString(s...) }
func (k KeyPressMsg) Key() uv.Key                  { return uv.KeyPressEvent(k).Key() }

func (k KeyReleaseMsg) String() string               { return uv.KeyReleaseEvent(k).String() }
func (k KeyReleaseMsg) Keystroke() string            { return uv.KeyReleaseEvent(k).Keystroke() }
func (k KeyReleaseMsg) MatchString(s ...string) bool { return uv.KeyReleaseEvent(k).MatchString(s...) }
func (k KeyReleaseMsg) Key() uv.Key                  { return uv.KeyReleaseEvent(k).Key() }

// 鼠标
type MouseClickMsg uv.MouseClickEvent
type MouseReleaseMsg uv.MouseReleaseEvent
type MouseWheelMsg uv.MouseWheelEvent
type MouseMotionMsg uv.MouseMotionEvent

func (m MouseClickMsg) Mouse() uv.Mouse   { return uv.MouseClickEvent(m).Mouse() }
func (m MouseClickMsg) String() string    { return uv.MouseClickEvent(m).String() }
func (m MouseReleaseMsg) Mouse() uv.Mouse { return uv.MouseReleaseEvent(m).Mouse() }
func (m MouseReleaseMsg) String() string  { return uv.MouseReleaseEvent(m).String() }
func (m MouseWheelMsg) Mouse() uv.Mouse   { return uv.MouseWheelEvent(m).Mouse() }
func (m MouseWheelMsg) String() string    { return uv.MouseWheelEvent(m).String() }
func (m MouseMotionMsg) Mouse() uv.Mouse  { return uv.MouseMotionEvent(m).Mouse() }
func (m MouseMotionMsg) String() string   { return uv.MouseMotionEvent(m).String() }

// 窗口 / 焦点 / 粘贴
type WindowSizeMsg uv.WindowSizeEvent
type FocusMsg uv.FocusEvent
type BlurMsg uv.BlurEvent
type PasteMsg uv.PasteEvent

func (p PasteMsg) String() string { return uv.PasteEvent(p).String() }

// 剪贴板 / 颜色 / 光标
type ClipboardMsg uv.ClipboardEvent
type ForegroundColorMsg uv.ForegroundColorEvent
type BackgroundColorMsg uv.BackgroundColorEvent
type CursorColorMsg uv.CursorColorEvent
type CursorPositionMsg uv.CursorPositionEvent

// 终端能力
type CapabilityMsg uv.CapabilityEvent
type ModeReportMsg uv.ModeReportEvent
type KeyboardEnhancementsMsg uv.KeyboardEnhancementsEvent
type TerminalVersionMsg uv.TerminalVersionEvent

// 内置消息
type QuitMsg struct{}
type InterruptMsg struct{}
type RenderGrantMsg struct{}
type ReqRenderMsg struct{}

// McpRefreshMsg 是 /mcp 面板动作（reconnect/enable/disable/authenticate/clear-auth/reauth）异步完成后的回传消息：
// 动作在后台 goroutine 执行（避免 connect 阻塞 TUI 事件循环），完成后经 tui.Send
// 投递回事件循环，由 dispatchMsg 路由到组件刷新面板。结果文案由动作生产者发布到 chat
// （AutoCompleteDetail），本消息只作刷新信号，不携带载荷。
type McpRefreshMsg struct{}

// OAuthDoneMsg 是 /login 订阅登录（浏览器授权）异步完成后的回传消息：动作在后台 goroutine
// 执行，完成后经 tui.Send 回传，面板据此结束忙碌态。结果文案由动作生产者发布到 chat
// （AutoCompleteDetail），本消息只携带 provider 与错误状态。
type OAuthDoneMsg struct {
	Provider string
	Err      error
}
