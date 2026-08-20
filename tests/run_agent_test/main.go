// run_agent_test 真实启动同步和异步子 agent，测试 RunAgent 工具全链路：Execute → BuildToolResult。
package main

import (
	"context"
	"fmt"
	"time"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	core_tools "github.com/tinyclue/tinyclue-code/coding_agent/core/tools"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

func dumpContent(label string, c core_types.SubAgentContent) {
	fmt.Printf("--- %s ---\n", label)
	fmt.Printf("  Status:            %q\n", c.Status)
	fmt.Printf("  Prompt:            %q\n", c.Prompt)
	fmt.Printf("  AgentId:           %q\n", c.AgentId)
	fmt.Printf("  AgentType:         %q\n", c.AgentType)
	fmt.Printf("  Description:       %q\n", c.Description)
	fmt.Printf("  ToolUseId:         %q\n", c.ToolUseId)
	fmt.Printf("  TotalTokens:       %d\n", c.TotalTokens)
	fmt.Printf("  TotalToolUseCount: %d\n", c.TotalToolUseCount)
	fmt.Printf("  TotalDurationMs:   %d\n", c.TotalDurationMs)
	fmt.Printf("  SessionUsage:      {Input:%d Output:%d CacheRead:%d CacheWrite:%d Total:%d}\n",
		c.SessionUsage.Input, c.SessionUsage.Output, c.SessionUsage.CacheRead, c.SessionUsage.CacheWrite, c.SessionUsage.TotalTokens)
	fmt.Printf("  WorkTreePath:      %q\n", c.WorkTreePath)
	fmt.Printf("  WorkTreeBranch:    %q\n", c.WorkTreeBranch)
	fmt.Printf("  IsAsync:           %v\n", c.IsAsync)
	fmt.Printf("  Context:           %d blocks\n", len(c.Context))
	for i, block := range c.Context {
		fmt.Printf("    [%d] Type=%q Text=%q\n", i, block.Type, block.Text)
	}
	fmt.Printf("  OutputFilePath:    %q\n", c.OutputFilePath)
	fmt.Printf("  Error:             %q\n", c.Error)
}

func main() {
	// ── 1. 创建所有工具（同 agent_interactive.go 模式） ──
	runner := &agent_core.AgentRunner{}
	allTools := []core_types.AgentToolApi{
		core_tools.NewBashTool(),
		core_tools.NewEditTool(),
		core_tools.NewReadTool(),
		core_tools.NewWriteTool(),
		core_tools.NewToolSearch(),
		core_tools.NewEnterPlanMode(),
		core_tools.NewExitPlanMode(),
		core_tools.NewWebSearch(),
		core_tools.NewRunAgentTool(runner),
	}

	// ── 2. 初始化 AgentTool 注册中心（主 agent 无禁用工具） ──
	agentTool := agent_core.NewAgentTool(context.Background(), nil)
	agentTool.InitWithTools(allTools, nil)

	agentUseCtx := &core_types.AgentUseContext{
		AgentId:   "parent-agent-001",
		SessionId: "session-001",
	}

	// ── 3. 同步测试：Explore agent ──
	fmt.Println("=== Test 1: Sync Explore agent ===")
	syncCall := apitypes.ToolCall{
		ID:   "call-sync-1",
		Name: "RunAgent",
		Arguments: map[string]any{
			"subagent_type": "Explore",
			"prompt":        "Just output the word 'ok' and nothing else.",
			"description":   "Sync explore test",
		},
	}

	start := time.Now()
	result, err := agentTool.ExecuteTool(context.Background(), syncCall, agentUseCtx)
	if err != nil {
		fmt.Printf("Execute error: %v\n", err)
	} else {
		content := result.Content.(core_types.SubAgentContent)
		dumpContent("Sync SubAgentContent", content)

		toolResult := agentTool.BuildToolResult(syncCall, result)
		fmt.Printf("ToolResult isError: %v\n", toolResult.IsError)
		if len(toolResult.Contents) > 0 {
			fmt.Printf("ToolResult text: %s\n", toolResult.Contents[0].Text)
		}
	}
	fmt.Printf("Sync elapsed: %v\n\n", time.Since(start))

	// ── 4. 异步测试：Plan agent（Background=true） ──
	fmt.Println("=== Test 2: Async Plan agent ===")
	asyncCall := apitypes.ToolCall{
		ID:   "call-async-1",
		Name: "RunAgent",
		Arguments: map[string]any{
			"subagent_type": "Plan",
			"prompt":        "Just output the word 'ok' and nothing else.",
			"description":   "Async plan test",
		},
	}

	asyncResult, asyncErr := agentTool.ExecuteTool(context.Background(), asyncCall, agentUseCtx)
	if asyncErr != nil {
		fmt.Printf("Execute error: %v\n", asyncErr)
	} else {
		asyncContent := asyncResult.Content.(core_types.SubAgentContent)
		dumpContent("Async SubAgentContent (launch)", asyncContent)

		asyncToolResult := agentTool.BuildToolResult(asyncCall, asyncResult)
		fmt.Printf("ToolResult isError: %v\n", asyncToolResult.IsError)
		if len(asyncToolResult.Contents) > 0 {
			fmt.Printf("Async result text:\n%s\n", asyncToolResult.Contents[0].Text)
		}
	}

	// ── 5. 等待异步 agent 完成通知 ──
	fmt.Println("\n=== Waiting for async agent notification... ===")
	notified := make(chan string, 1)
	cancel := agent_core.Subscribe(agent_core.UserInputQueue, func(msg core_types.EventMessage) {
		if msg.EventType == core_types.TaskNotification {
			if userMsg, ok := msg.Message.(apitypes.UserMessage); ok {
				notified <- userMsg.Text
			}
		}
	})

	select {
	case text := <-notified:
		fmt.Println("\n=== Async agent notification received ===")
		fmt.Println(text)
		cancel()
	case <-time.After(180 * time.Second):
		fmt.Println("\nTimeout waiting for async agent notification")
		cancel()
	}

	fmt.Println("\n=== All tests done ===")
}
