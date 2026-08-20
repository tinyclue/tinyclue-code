package tui

import (
	"github.com/tinyclue/tinyclue-code/tui/core"
	"sync"
	"time"

	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// RenderTimer throttles rendering to at most once per 16ms (~60fps).
//
// Single select loop with timerRunning/pending state:
//
//	IDLE (timerRunning=false):
//	  RequestRender → send grant immediately, start 16ms timer
//
//	COOLDOWN (timerRunning=true):
//	  RequestRender → set pending=true (coalesced)
//	  timer fires   → if pending, send grant & restart timer
//	                → if !pending, back to IDLE
type RenderTimer struct {
	req      chan core.Data    // TUI → timer: request render
	done     chan struct{}     // signal goroutine to stop
	program  *terminal.Program // event loop, set via SetProgram
	stopOnce sync.Once
}

// NewRenderTimer creates a timer and starts its background goroutine.
// Use SetProgram before the event loop starts to wire up message delivery.
func NewRenderTimer() *RenderTimer {
	rt := &RenderTimer{
		req:  make(chan core.Data, 1),
		done: make(chan struct{}),
	}
	go rt.loop()
	return rt
}

// SetProgram sets the event loop program reference.
// Must be called before the event loop starts (Init, before Start).
func (rt *RenderTimer) SetProgram(p *terminal.Program) {
	rt.program = p
}

// RequestRender requests one render.  Non-blocking — coalesces extra
// requests within the 16ms cooldown window.
func (rt *RenderTimer) RequestRender(data core.Data) {
	select {
	case rt.req <- data:
	default:
	}
}

// Stop shuts down the background goroutine.  Safe to call multiple times.
func (rt *RenderTimer) Stop() {
	rt.stopOnce.Do(func() {
		close(rt.done)
	})
}

func (rt *RenderTimer) send(data core.Data) {
	if rt.program != nil {
		rt.program.Send(data.Msg)
	}
}

// loop is the single select loop.
func (rt *RenderTimer) loop() {
	timer := time.NewTimer(0)
	if !timer.Stop() {
		<-timer.C
	}
	timerRunning := false
	pending := false

	for {
		select {
		case <-rt.req:
			if !timerRunning {
				// IDLE — send grant immediately, start cooldown
				rt.send(core.Data{Msg: core.RenderGrantMsg{}})
				timer.Reset(16 * time.Millisecond)
				timerRunning = true
			} else {
				// COOLDOWN — coalesce, don't send
				pending = true
			}

		case <-timer.C:
			timerRunning = false
			if pending {
				rt.send(core.Data{Msg: core.RenderGrantMsg{}})
				timer.Reset(16 * time.Millisecond)
				timerRunning = true
				pending = false
			}
			// !pending → back to IDLE, nothing to do

		case <-rt.done:
			if timerRunning {
				timer.Stop()
			}
			return
		}
	}
}
