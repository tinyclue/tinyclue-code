// Package terminal 提供 Elm Architecture 风格的事件循环，底层基于 ultraviolet。
// 只做事件分发，不做渲染。你可以接入任何 UI 后端。
package terminal

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/types"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/term"
)

// =============================================================
// 消息类型 — 自定义命名类型，非 uv 别名，和 tea 一致
// =============================================================

// =============================================================
// 核心类型
// =============================================================

type Model interface {
	Update(data core.Data) error
}

// Program 管理事件循环生命周期。
type Program struct {
	model Model

	msgs chan core.Msg
	errs chan error

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	sigHandler    bool
	lastInterrupt time.Time // 上次 Ctrl+C 时间，用于双击退出检测
}

type Option func(*Program)

func WithSignalHandler() Option {
	return func(p *Program) {
		p.sigHandler = true
	}
}

func New(model Model, opts ...Option) *Program {
	ctx, cancel := context.WithCancel(context.Background())
	return &Program{
		model:  model,
		msgs:   make(chan core.Msg, 128),
		errs:   make(chan error, 1),
		ctx:    ctx,
		cancel: cancel,
	}
}

func (p *Program) Send(msg core.Msg) {
	select {
	case p.msgs <- msg:
	case <-p.ctx.Done():
	}
}

// Run 启动事件循环，阻塞直到退出。
// 底层使用 uv.TerminalReader.StreamEvents()，与 Bubble Tea 架构一致。
func (p *Program) Run() error {
	stdin := os.Stdin
	termType := os.Getenv("TERM")

	// 1. 设置 raw mode
	var prevState *term.State
	if term.IsTerminal(stdin.Fd()) {
		var err error
		prevState, err = term.MakeRaw(stdin.Fd())
		if err != nil {
			return fmt.Errorf("loop: make raw: %w", err)
		}
	}

	// 2. 创建 cancel reader（优雅退出需要）
	cancelReader, err := uv.NewCancelReader(stdin)
	if err != nil {
		if prevState != nil {
			_ = term.Restore(stdin.Fd(), prevState)
		}
		return fmt.Errorf("loop: cancel reader: %w", err)
	}

	// 3. 创建 TerminalReader（与 Bubble Tea 完全相同的架构）
	drv := uv.NewTerminalReader(cancelReader, termType)

	// 4. 启用 bracketed paste 和 focus events
	fmt.Fprint(os.Stdout, "[?2004h[?1004h")

	// 5. 事件直接流到 p.msgs
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		if err := drv.StreamEvents(p.ctx, p.msgs); err != nil {
			select {
			case <-p.ctx.Done():
			case p.errs <- err:
			}
		}
	}()

	if p.sigHandler {
		p.wg.Add(1)
		go p.signalLoop()
	}

	// 6. 窗口大小变化监听
	resizeDone := make(chan struct{})
	go p.resizeLoop(resizeDone)

	// 7. 发送初始窗口大小
	p.sendWindowSize()

	err2 := p.eventLoop()

	// 8. Cleanup
	p.cancel()
	cancelReader.Cancel()
	<-resizeDone
	fmt.Fprint(os.Stdout, "[?2004l[?1004l")
	if prevState != nil {
		_ = term.Restore(stdin.Fd(), prevState)
	}
	p.wg.Wait()
	return err2
}

// resizeLoop 监听 SIGWINCH，在窗口大小变化时发送 WindowSizeEvent。
func (p *Program) resizeLoop(done chan<- struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	defer func() {
		signal.Stop(sig)
		close(done)
	}()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-sig:
		}
		p.sendWindowSize()
	}
}

// sendWindowSize 查询终端大小并发送 WindowSizeEvent 到事件队列。
func (p *Program) sendWindowSize() {
	w, h, err := term.GetSize(os.Stdout.Fd())
	if err != nil {
		return
	}
	DefaultTerminalContext.SetWidth(w)
	DefaultTerminalContext.SetHeight(h)
	p.Send(uv.WindowSizeEvent{Width: w, Height: h})
}

// =============================================================
// translateInputEvent — 和 tea.input.go 完全一致的模式
// 将 uv 原生事件转换为 terminal 包消息类型
// =============================================================

func (p *Program) translateInputEvent(e uv.Event) core.Msg {
	switch e := e.(type) {
	case uv.KeyPressEvent:
		return core.KeyPressMsg(e)
	case uv.KeyReleaseEvent:
		return core.KeyReleaseMsg(e)
	case uv.WindowSizeEvent:
		return core.WindowSizeMsg(e)
	case uv.MouseClickEvent:
		return core.MouseClickMsg(e)
	case uv.MouseReleaseEvent:
		return core.MouseReleaseMsg(e)
	case uv.MouseWheelEvent:
		return core.MouseWheelMsg(e)
	case uv.MouseMotionEvent:
		return core.MouseMotionMsg(e)
	case uv.FocusEvent:
		return core.FocusMsg(e)
	case uv.BlurEvent:
		return core.BlurMsg(e)
	case uv.PasteEvent:
		return core.PasteMsg(e)
	case uv.PasteStartEvent:
		return nil
	case uv.PasteEndEvent:
		return nil
	case uv.ClipboardEvent:
		return core.ClipboardMsg(e)
	case uv.CursorPositionEvent:
		return core.CursorPositionMsg(e)
	case uv.ForegroundColorEvent:
		return core.ForegroundColorMsg(e)
	case uv.BackgroundColorEvent:
		return core.BackgroundColorMsg(e)
	case uv.CursorColorEvent:
		return core.CursorColorMsg(e)
	case uv.CapabilityEvent:
		return core.CapabilityMsg(e)
	case uv.ModeReportEvent:
		return core.ModeReportMsg(e)
	case uv.KeyboardEnhancementsEvent:
		return core.KeyboardEnhancementsMsg(e)
	case uv.TerminalVersionEvent:
		return core.TerminalVersionMsg(e)
	}
	return e
}

// handleInterrupt 处理中断信号，首次 InterruptMsg，500ms 内二次 QuitMsg。
func (p *Program) handleInterrupt() core.Msg {
	if time.Since(p.lastInterrupt) < types.RevertTimeout {
		return core.QuitMsg{}
	}
	p.lastInterrupt = time.Now()
	return core.InterruptMsg{}
}

// =============================================================
// eventLoop
// =============================================================

func (p *Program) eventLoop() error {

	for {
		select {
		case <-p.ctx.Done():
			return nil

		case err := <-p.errs:
			return err

		case msg := <-p.msgs:
			msg = p.translateInputEvent(msg)
			if msg == nil {
				continue
			}
			// Ctrl+C 走两级处理，Ctrl+Q 直接退出
			if keyMsg, ok := msg.(core.KeyPressMsg); ok {
				if keyMsg.MatchString("ctrl+c") {
					msg = p.handleInterrupt()
				} else if keyMsg.MatchString("ctrl+q") {
					msg = core.QuitMsg{}
				}
			}
			data := core.Data{Msg: msg}
			err := p.model.Update(data)

			// QuitMsg 处理后退出
			if _, ok := msg.(core.QuitMsg); ok {
				return nil
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nexit: %v\n", err)
			}
		}
	}
}

// =============================================================
// signalLoop
// =============================================================

func (p *Program) signalLoop() {
	defer p.wg.Done()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sig)

	select {
	case <-p.ctx.Done():
	case s := <-sig:
		switch s {
		case syscall.SIGINT:
			// macOS raw mode 下 Ctrl+C 走信号路径，发 InterruptMsg（非直接退出）
			p.Send(p.handleInterrupt())
		default:
			p.Send(core.QuitMsg{})
		}
	}
}
