package core

import (
	"context"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/usercontext"
	"github.com/tinyclue/tinyclue-code/config"
	"os"
)

type AgentMemory struct {
	ctx        context.Context
	runtimeCtx *core_types.AgentRuntimeContext
}

func NewAgentMemory(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentMemory {
	return &AgentMemory{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (am *AgentMemory) Init() {
	am.initDir()
}

func (am *AgentMemory) initDir() {
	dir, _ := config.TinyClueDir()
	cwd := config.CLI.Cwd
	memoryDir := usercontext.GetAutoMemPath(dir, cwd)
	os.MkdirAll(memoryDir, 0755)
}
