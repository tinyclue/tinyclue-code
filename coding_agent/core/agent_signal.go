package core

import "context"

type AgentSignal struct {
	abort  bool
	ctx    context.Context
	cancel context.CancelFunc
}

func NewAgentSignal(ctx context.Context) *AgentSignal {
	signalCtx, signalCancel := context.WithCancel(ctx)
	return &AgentSignal{
		abort:  false,
		ctx:    signalCtx,
		cancel: signalCancel,
	}
}

func (as *AgentSignal) Abort() {
	as.abort = true
	as.cancel()
}

func (as *AgentSignal) Ctx() context.Context {
	return as.ctx
}

func (as *AgentSignal) IsAborted() bool {
	return as.abort
}
