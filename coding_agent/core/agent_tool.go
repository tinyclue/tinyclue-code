package core

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"sync"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"

	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// AgentTool 管理所有工具的初始化、注册和调用分发。
type AgentTool struct {
	ctx                  context.Context
	runtimeCtx           *types.AgentRuntimeContext
	allAgentTools        []types.AgentToolApi
	disallowedToolsNames []string
	discoveredToolNames  []string
	deferredToolNames    []string

	eventSink types.EventSink

	// runtimeDisabled 记录运行时被禁用的工具（/mcp 面板操作），与 disallowedToolsNames 互补。
	runtimeDisabled map[string]bool
	// serverTools 记录每个 MCP server 最近同步过的工具名，供重连/禁用时按 server 清理陈旧桥接。
	serverTools map[string][]string
	// mu 保护 allAgentTools / runtimeDisabled / deferredToolNames。
	// /mcp 面板运行在 TUI goroutine，GetEnableTools/FindToolByName 运行在 agent goroutine，两者并发。
	mu sync.RWMutex
}

func NewAgentTool(ctx context.Context, runtimeCtx *types.AgentRuntimeContext) *AgentTool {
	return &AgentTool{
		ctx:             ctx,
		runtimeCtx:      runtimeCtx,
		runtimeDisabled: map[string]bool{},
		serverTools:     map[string][]string{},
	}
}

func (op *AgentTool) WithEventSink(sink types.EventSink) *AgentTool {
	op.eventSink = sink
	return op
}

// InitWithTools 用外部传入的工具列表和禁用工具名单初始化，注册所有工具并按名称建立映射。
func (op *AgentTool) InitWithTools(tools []types.AgentToolApi, disallowedTools []string) {
	op.mu.Lock()
	defer op.mu.Unlock()
	op.allAgentTools = tools
	op.disallowedToolsNames = disallowedTools
	op.runtimeDisabled = map[string]bool{}
	op.serverTools = map[string][]string{}
	op.deferredToolNames = nil
	for _, t := range tools {
		if t.IsDeferredTool() {
			op.deferredToolNames = append(op.deferredToolNames, t.Name())
		}
	}
}

// SyncServerTools 把指定 MCP server 的工具集同步为 target（该 server 当前应暴露的工具），
// 注册表按 server 收敛为唯一入口（mcp 层不返回增删 diff）：
//   - target 中未注册的工具追加注册；已注册的清除运行时禁用标记（重连后恢复可见）；
//   - 该 server 此前同步过、但不在 target 中的工具标记为运行时禁用（陈旧桥接清理）。
func (op *AgentTool) SyncServerTools(server string, target []types.AgentToolApi) {
	op.mu.Lock()
	defer op.mu.Unlock()

	targetNames := make(map[string]bool, len(target))
	var fresh []types.AgentToolApi
	for _, t := range target {
		name := t.Name()
		targetNames[name] = true
		if !op.hasNameLocked(name) {
			fresh = append(fresh, t)
		}
		delete(op.runtimeDisabled, name)
	}
	op.addToolsLocked(fresh)
	for _, n := range op.serverTools[server] {
		if !targetNames[n] {
			op.runtimeDisabled[n] = true
		}
	}
	names := make([]string, 0, len(targetNames))
	for n := range targetNames {
		names = append(names, n)
	}
	op.serverTools[server] = names
}

// addToolsLocked 追加未注册的新工具（调用方持锁）。先过滤同名幂等跳过，
// 再一次性 copy-on-write 追加，避免循环内反复复制整个切片。
func (op *AgentTool) addToolsLocked(fresh []types.AgentToolApi) {
	if len(fresh) == 0 {
		return
	}
	all := make([]types.AgentToolApi, len(op.allAgentTools)+len(fresh))
	copy(all, op.allAgentTools)
	copy(all[len(op.allAgentTools):], fresh)
	op.allAgentTools = all
	for _, t := range fresh {
		if t.IsDeferredTool() {
			op.deferredToolNames = append(op.deferredToolNames, t.Name())
		}
	}
}

func (op *AgentTool) hasNameLocked(name string) bool {
	for _, t := range op.allAgentTools {
		if t.Name() == name {
			return true
		}
	}
	return false
}

func (op *AgentTool) AddDiscoveredToolNames(names []string) {
	discoveredToolNames := op.discoveredToolNames
	discoveredToolNames = append(op.discoveredToolNames, names...)
	op.discoveredToolNames = utils.DeduplicateStrings(discoveredToolNames)
}

func (op *AgentTool) UpdateDiscoveredTools(discoveredToolNames []string) {
	op.discoveredToolNames = discoveredToolNames
}

func (op *AgentTool) hasDiscoveredTool(name string) bool {
	for _, tname := range op.discoveredToolNames {
		if tname == name {
			return true
		}
	}
	return false
}

// isDisallowedTool 判断工具本轮是否不可用：静态禁用名单或运行时禁用（/mcp 面板）都算。
func (op *AgentTool) isDisallowedTool(name string) bool {
	if op.runtimeDisabled[name] {
		return true
	}
	for _, tname := range op.disallowedToolsNames {
		if tname == name {
			return true
		}
	}
	return false
}

func (op *AgentTool) GetEnableTools() []apitypes.Tool {
	op.mu.RLock()
	defer op.mu.RUnlock()
	var tools []apitypes.Tool
	for _, t := range op.allAgentTools {
		if op.isDisallowedTool(t.Name()) {
			continue
		}
		if !t.IsDeferredTool() || (t.IsDeferredTool() && op.hasDiscoveredTool(t.Name())) {
			tools = append(tools, t.GetTool())
		}
	}
	return tools
}

func (op *AgentTool) getToolByName(name string) types.AgentToolApi {
	var tool types.AgentToolApi
	find := false
	for _, t := range op.allAgentTools {
		if t.Name() == name {
			find = true
			tool = t
			break
		}
	}
	if !find {
		return nil
	}
	return tool
}

func (op *AgentTool) FindToolByName(name string) (types.AgentToolApi, bool) {
	op.mu.RLock()
	defer op.mu.RUnlock()
	var tool types.AgentToolApi
	find := false
	for _, t := range op.allAgentTools {
		if t.Name() == name {
			find = true
			tool = t
			break
		}
	}
	if !find {
		return nil, false
	}
	if op.isDisallowedTool(tool.Name()) {
		return nil, false
	}
	if !tool.IsDeferredTool() || (tool.IsDeferredTool() && op.hasDiscoveredTool(tool.Name())) {
		return tool, true
	}
	return nil, false
}

func (op *AgentTool) ValidateTool(ctx context.Context, call apitypes.ToolCall, agentUseContext *types.AgentUseContext) types.ValidateContent {
	t, ok := op.FindToolByName(call.Name)
	if !ok {
		toolContext := types.ValidateContent{
			Result:  false,
			Message: `<tool_use_error>Error: No such tool available: ` + call.Name + `</tool_use_error>`,
		}
		return toolContext
	}
	toolUseContext := types.NewToolUseContext(op.allAgentTools, call, agentUseContext)
	return t.ValidateInput(ctx, toolUseContext)
}

// BeforeToolCall 查找工具并调用其 BeforeToolCall 方法。
func (op *AgentTool) BeforeToolCall(ctx context.Context, call apitypes.ToolCall, agentUseContext *types.AgentUseContext) types.BeforeToolCallResult {
	t, ok := op.FindToolByName(call.Name)
	if !ok {
		return types.BeforeToolCallResult{Action: types.BeforeToolCallDeny, Message: fmt.Sprintf("unknown tool: %s", call.Name)}
	}
	toolUseContext := types.NewToolUseContext(op.allAgentTools, call, agentUseContext)
	return t.BeforeToolCall(ctx, toolUseContext)
}

// 根据 ToolCall.Name 查找对应的工具并执行。
func (op *AgentTool) ExecuteTool(ctx context.Context, call apitypes.ToolCall, agentUseContext *types.AgentUseContext) (types.ToolContext, error) {
	t, ok := op.FindToolByName(call.Name)
	if !ok {
		return types.ToolContext{}, fmt.Errorf("unknown tool: %s", call.Name)
	}
	toolUseContext := types.NewToolUseContext(op.allAgentTools, call, agentUseContext)
	toolUseContext = toolUseContext.WithOnProgress(func(toolContext types.ToolContext) {
		op.eventSink.Emit(types.ToolExecutionUpdate, toolContext, nil)
	})
	return t.Execute(ctx, toolUseContext)
}

func (op *AgentTool) BuildToolResult(call apitypes.ToolCall, toolContext types.ToolContext) apitypes.ToolResultMessage {
	t, _ := op.FindToolByName(call.Name)
	return t.BuildToolResult(toolContext)
}

func (op *AgentTool) GetDeferredToolNames() []string {
	op.mu.RLock()
	defer op.mu.RUnlock()
	return append([]string{}, op.deferredToolNames...)
}

func (op *AgentTool) ProcessToolRef(toolResults []apitypes.ToolResultMessage) {
	messages := make([]apitypes.Message, len(toolResults))
	for i, tr := range toolResults {
		messages[i] = tr
	}
	discoveredToolNames := utils.ExtractDiscoveredToolNames(messages)
	op.AddDiscoveredToolNames(discoveredToolNames)
}
