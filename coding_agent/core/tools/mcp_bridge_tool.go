package tools

import (
	"context"
	"fmt"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
)

const toolPrefix = "mcp__"

// MCPToolCaller 执行一次 MCP 工具调用，由 mcp 组装层注入（默认包装 MInstance），
// 使 tools 层不必反向依赖 mcp 包。返回文本结果与 isError 标记。
type MCPToolCaller func(ctx context.Context, server, tool string, args map[string]any) (content string, isError bool, err error)

// BridgeTool 将单个 MCP server 工具桥接为 AgentToolApi，使模型通过标准函数调用
// 直接使用 MCP 工具。权限在 BeforeToolCall 返回 Ask，复用现有通用权限面板。
type BridgeTool struct {
	*ToolBase
	server string // MCP server 名（配置中的 mcpServers key）
	tool   string // MCP 侧的原始工具名
	name   string // 桥接名：mcp__<server>__<tool>
	desc   string
	schema apitypes.Parameters
	caller MCPToolCaller // 由组装层 SetCaller 注入；nil 时 Execute 直接报错
}

// NewMCPBridgeTool 构造桥接工具。schema 由 mcp 组装层完成 MCP→apitypes 转换后传入。
// 不受名字归一化影响。
func NewMCPBridgeTool(server, tool, desc string, schema apitypes.Parameters) *BridgeTool {
	return &BridgeTool{
		ToolBase: NewToolBase(),
		server:   server,
		tool:     tool,
		name:     toolPrefix + NormalizeNameForMCP(server) + "__" + NormalizeNameForMCP(tool),
		desc:     desc,
		schema:   schema,
	}
}

// SetCaller 注入 MCP 调用器（mcp 组装层在 connect 时调用，绑定所属 Manager）。
func (bt *BridgeTool) SetCaller(caller MCPToolCaller) {
	bt.caller = caller
}

func (bt *BridgeTool) Name() string { return bt.name }

func (bt *BridgeTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        bt.name,
		Description: bt.desc,
		Parameters:  bt.schema,
		Strict:      false, // 任意 MCP schema 无法满足 OpenAI strict（additionalProperties:false 全 required）约束
	}
}

// BeforeToolCall 命中项目 allow 规则（.tinyclue/config/settings.local.json）则免确认；
// 否则每次调用都要求用户确认。
func (bt *BridgeTool) BeforeToolCall(_ context.Context, tuc core_types.ToolUseContext) core_types.BeforeToolCallResult {
	if config.AllowMatch(bt.name, config.RenderArgs(bt.name, tuc.ToolCall.Arguments)) {
		return core_types.BeforeToolCallResult{Action: core_types.BeforeToolCallAllow}
	}
	return core_types.BeforeToolCallResult{
		Action:  core_types.BeforeToolCallAsk,
		Message: fmt.Sprintf("允许调用 MCP 工具 %s（server: %s）？", bt.tool, bt.server),
	}
}

func (bt *BridgeTool) Execute(ctx context.Context, tuc core_types.ToolUseContext) (core_types.ToolContext, error) {
	if bt.caller == nil {
		return bt.ErrorReturn(tuc.ToolCall, fmt.Errorf("MCP 工具 %s 未注入调用器", bt.name))
	}
	content, isError, err := bt.caller(ctx, bt.server, bt.tool, tuc.ToolCall.Arguments)
	if err != nil {
		return bt.ErrorReturn(tuc.ToolCall, fmt.Errorf("MCP 工具 %s 调用失败: %w", bt.name, err))
	}
	toolContext := core_types.ToolContext{
		ToolCall: tuc.ToolCall,
		Content:  core_types.TextContent{Text: content},
	}
	if isError {
		// 映射为 tool_use_error（agent_loop 走 newErrorToolResult，模型可见并可自我修正）。
		return toolContext, fmt.Errorf("MCP 工具 %s 返回错误: %s", bt.name, content)
	}
	return toolContext, nil
}

func (bt *BridgeTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	text := ""
	if t, ok := toolContext.Content.(core_types.TextContent); ok {
		text = t.Text
	}
	return bt.BuildToolResultByText(text, toolContext)
}
