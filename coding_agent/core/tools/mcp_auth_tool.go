package tools

import (
	"context"
	"fmt"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// McpAuthTool 是"伪工具"：某 http server 进入 needs-auth 状态时，它的桥接工具被整体替换成
// 这个伪工具，让模型能感知该 server 存在、需要授权，并可通过调用它显式发起授权
// 非 deferred 工具恒可见，模型才知道未授权 server 的存在。
type McpAuthTool struct {
	*ToolBase
	server string                    // MCP server 名（配置中的 mcpServers key）
	name   string                    // 桥接名：mcp__<Normalize(server)>__authenticate
	authFn func(server string) error // 由组装层注入：异步发起授权（打开浏览器后立即返回）
}

// NewMcpAuthTool 构造授权伪工具。authFn 由组装层注入（见 agent_interactive.authenticateAsync）。
func NewMcpAuthTool(server string, authFn func(server string) error) *McpAuthTool {
	return &McpAuthTool{
		ToolBase: NewToolBase(),
		server:   server,
		name:     toolPrefix + NormalizeNameForMCP(server) + "__authenticate",
		authFn:   authFn,
	}
}

func (at *McpAuthTool) Name() string { return at.name }

func (at *McpAuthTool) GetTool() apitypes.Tool {
	return apitypes.Tool{
		Name:        at.name,
		Description: fmt.Sprintf("The `%s` MCP server is installed but requires authentication. Call this tool to start the OAuth flow — a browser will open automatically to complete authorization. Once the user completes authorization in their browser, the server's real tools will become available automatically.", at.server),
		// 无参数工具也必须给出合法空 schema：零值 Parameters 会序列化为
		// {"type":"","properties":null,"required":null}，OpenAI 兼容端点拒绝
		// required:null（"null is not of type array" 400）。
		Parameters: apitypes.Parameters{
			Type:       "object",
			Properties: map[string]apitypes.SchemaItem{},
			Required:   []string{},
		},
	}
}

// IsDeferredTool 恒返回 false：伪工具始终在模型工具列表中，模型才知道未授权 server 的存在。
func (at *McpAuthTool) IsDeferredTool() bool { return false }

// BeforeToolCall 用 ToolBase 默认 Allow：发起授权不是危险操作，不弹确认面板
// （与 BridgeTool 的权限确认不同）。
func (at *McpAuthTool) Execute(ctx context.Context, tuc core_types.ToolUseContext) (core_types.ToolContext, error) {
	if at.authFn == nil {
		return at.ErrorReturn(tuc.ToolCall, fmt.Errorf("mcp auth tool %s has no authFn injected", at.name))
	}
	if err := at.authFn(at.server); err != nil {
		return at.ErrorReturn(tuc.ToolCall, fmt.Errorf("Failed to start OAuth flow for %s: %w. Ask the user to run /mcp and authenticate manually.", at.server, err))
	}
	toolContext := core_types.ToolContext{
		ToolCall: tuc.ToolCall,
		Content: core_types.TextContent{
			Text: fmt.Sprintf("Started the OAuth flow for the `%s` MCP server — a browser will open automatically to complete authorization. Once the user completes the flow, the server's tools will become available automatically.", at.server),
		},
	}
	return toolContext, nil
}

func (at *McpAuthTool) BuildToolResult(toolContext core_types.ToolContext) apitypes.ToolResultMessage {
	text := ""
	if t, ok := toolContext.Content.(core_types.TextContent); ok {
		text = t.Text
	}
	return at.BuildToolResultByText(text, toolContext)
}
