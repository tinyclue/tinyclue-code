package terminal

import "sync/atomic"

type TerminalContext struct {
	width  atomic.Int32
	height atomic.Int32
}

var DefaultTerminalContext = &TerminalContext{}

func (tc *TerminalContext) SetWidth(w int) {
	tc.width.Store(int32(w))
}

func (tc *TerminalContext) GetWidth() int {
	return int(tc.width.Load())
}

func (tc *TerminalContext) SetHeight(h int) {
	tc.height.Store(int32(h))
}

func (tc *TerminalContext) GetHeight() int {
	return int(tc.height.Load())
}
