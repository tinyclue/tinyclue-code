// main_test/main.go 启动 TUI 交互式会话，并通过 TaskManager 模拟任务全生命周期，
// 以验证 TaskPanel 面板渲染的正确性。
package main

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"time"
)

func main() {
	ia := codingagent.New()
	ia.Init()

	//go simulateTaskLifecycle()
	//go simulateAskUserQuestion()

	ia.Start()
}

// simulateAskUserQuestion 模拟 AskUserQuestion 工具的提问交互流程，
// 验证 tabbed 问答面板的渲染、选择、提交和数据回传。
func simulateAskUserQuestion() {
	// 等待 TUI 初始化完成
	time.Sleep(1500 * time.Millisecond)

	// 构造 PermissionRequest，模拟 agent_loop 中 executeToolRound 的行为
	responseCh := make(chan core_types.PermissionResponse, 1)

	// 后台读取响应（用户提交/取消后会收到）
	go func() {
		resp := <-responseCh
		fmt.Printf("\nAskUserQuestion response: action=%s", resp.Action)
		if resp.ModifiedArgs != nil {
			if answers, ok := resp.ModifiedArgs["answers"].(map[string]any); ok {
				fmt.Printf(", answers=%v", answers)
			}
		}
	}()

	req := core_types.PermissionRequest{
		ToolName:   core_types.ASK_USER_QUESTION_TOOL_NAME,
		ToolCallID: "call-askuserquestion-1",
		Message:    "Please answer the following questions:",
		ResponseCh: responseCh,
		Args: map[string]any{
			"questions": []any{
				map[string]any{
					"question": "What programming language do you prefer?",
					"header":   "Language",
					"options": []any{
						map[string]any{"label": "Go", "description": "Golang - fast and simple"},
						map[string]any{"label": "Python", "description": "Python - versatile and popular"},
						map[string]any{"label": "Rust", "description": "Rust - safe and performant"},
						map[string]any{"label": "TypeScript", "description": "TS - typed JavaScript"},
					},
				},
				map[string]any{
					"question":    "Which features interest you? (select all that apply)",
					"header":      "Features",
					"multiSelect": true,
					"options": []any{
						map[string]any{"label": "Web Framework", "description": "Build web applications"},
						map[string]any{"label": "CLI Tools", "description": "Command-line interfaces"},
						map[string]any{"label": "Data Processing", "description": "Process large datasets"},
						map[string]any{"label": "Machine Learning", "description": "Train and deploy models"},
					},
				},
				map[string]any{
					"question": "How would you rate your experience?",
					"header":   "Experience",
					"options": []any{
						map[string]any{"label": "Beginner", "description": "Less than 1 year"},
						map[string]any{"label": "Intermediate", "description": "1-3 years"},
						map[string]any{"label": "Advanced", "description": "3-5 years"},
						map[string]any{"label": "Expert", "description": "5+ years"},
					},
				},
			},
			"annotations": map[string]any{},
		},
	}

	// 通过事件队列发布，触发 TuiEvent.handleToolPermissionRequired
	agent_core.Publish(agent_core.AgentEventQueue,
		core_types.NewEventMessage(core_types.ToolPermissionRequired, req, nil))
}

// simulateTaskLifecycle 通过 Agent.agentTask（TaskManager）模拟任务的完整生命周期，
// TaskManager 的每次操作会自动通过 eventSink 发出 TaskUpdateEventType 事件，
// TuiEvent.handleTaskUpdate 接收到事件后更新 TaskPanel 的显示。
func simulateTaskLifecycle() {
	// 等待 TUI 初始化完成
	time.Sleep(1500 * time.Millisecond)

	// 通过 AMInstance 获取已注册的 GENERAL agent
	agents := agent_core.AMInstance.ListByType(core_types.GENERAL)
	if len(agents) == 0 {
		fmt.Println("no agent found")
		return
	}
	agent := agent_core.AMInstance.GetAgent(agents[0])
	if agent == nil {
		fmt.Println("agent is nil")
		return
	}
	// GetAgentTaskManager 返回 TaskManagerApi 接口，涵盖 Create/Update/List/Delete
	tm := agent.GetTaskManager()

	inProgress := core_types.TaskStatusInProgress
	completed := core_types.TaskStatusCompleted

	// ──────────────────────────────────────────────
	// 1. 创建 6 个任务
	// ──────────────────────────────────────────────
	//fmt.Println("=== Creating tasks ===")

	t1, _ := tm.Create("Implement login page", "Design and implement the login page UI and logic")
	t2, _ := tm.Create("Design database schema", "Create the database schema for user data")
	t3, _ := tm.Create("Setup CI/CD pipeline", "Configure GitHub Actions for CI/CD")
	t4, _ := tm.Create("Write unit tests", "Write unit tests for the API layer")
	t5, _ := tm.Create("Create API endpoints", "Implement REST API endpoints")
	t6, _ := tm.Create("Update documentation", "Update the README and API docs")

	time.Sleep(800 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 2. 设置状态：in_progress / completed
	//    面板应显示：
	//      ■ Design database schema (@alice)
	//      ■ Create API endpoints (@bob)
	//      ✔ Setup CI/CD pipeline  (strikethrough)
	//      □ Implement login page (@alice) > blocked by #2
	//      □ Write unit tests (@bob) > blocked by #1
	//      □ Update documentation (@charlie)
	// ──────────────────────────────────────────────
	//fmt.Println("=== Setting statuses ===")

	alice, bob, charlie := "alice", "bob", "charlie"
	tm.Update(t2.ID, core_types.TaskUpdateRequest{Status: &inProgress, Owner: &alice})
	time.Sleep(300 * time.Millisecond)
	tm.Update(t5.ID, core_types.TaskUpdateRequest{Status: &inProgress, Owner: &bob})
	time.Sleep(300 * time.Millisecond)
	tm.Update(t3.ID, core_types.TaskUpdateRequest{Status: &completed})
	time.Sleep(300 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 3. 设置阻塞关系
	//    t2 (schema) → blocks → t1 (login)
	//    t1 (login)  → blocks → t4 (tests)
	//    效果：t1 被 t2 阻塞，t4 被 t1 阻塞
	// ──────────────────────────────────────────────
	//fmt.Println("=== Setting blocking relationships ===")

	tm.Update(t1.ID, core_types.TaskUpdateRequest{AddBlockedBy: []string{t2.ID}})
	tm.Update(t2.ID, core_types.TaskUpdateRequest{AddBlocks: []string{t1.ID}})
	time.Sleep(300 * time.Millisecond)

	tm.Update(t4.ID, core_types.TaskUpdateRequest{AddBlockedBy: []string{t1.ID}})
	tm.Update(t1.ID, core_types.TaskUpdateRequest{AddBlocks: []string{t4.ID}})
	time.Sleep(300 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 4. 设置未完成任务 owner
	// ──────────────────────────────────────────────
	//fmt.Println("=== Setting owners ===")

	tm.Update(t1.ID, core_types.TaskUpdateRequest{Owner: &alice})
	tm.Update(t4.ID, core_types.TaskUpdateRequest{Owner: &bob})
	tm.Update(t6.ID, core_types.TaskUpdateRequest{Owner: &charlie})

	time.Sleep(800 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 5. 创建并立即完成一个任务（验证动态增减）
	// ──────────────────────────────────────────────
	//fmt.Println("=== Creating and completing a task ===")

	t7, _ := tm.Create("Code review", "Review PR #42")
	time.Sleep(300 * time.Millisecond)
	tm.Update(t7.ID, core_types.TaskUpdateRequest{Status: &completed})
	time.Sleep(800 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 6. 删除一个任务（验证面板更新）
	// ──────────────────────────────────────────────
	//fmt.Println("=== Deleting a task ===")
	tm.Delete(t7.ID)

	//fmt.Println("Task lifecycle simulation complete. Panel should show 6 tasks.")
	//fmt.Println("Press Ctrl+C to exit.")

	time.Sleep(1000 * time.Millisecond)

	// ──────────────────────────────────────────────
	// 7. 全部完成，测试面板能否自动隐藏
	// ──────────────────────────────────────────────
	//fmt.Println("=== Completing all tasks ===")
	//for _, t := range []*agent_task_types.Task{t1, t2, t3, t4, t5, t6} {
	//	tm.Update(t.ID, agent_task_types.UpdateRequest{Status: &completed})
	//	time.Sleep(200 * time.Millisecond)
	//}
	//fmt.Println("All tasks completed. Panel should be hidden.")
}

// ---------------------------------------------------------------------------
// 以下为保留的历史模拟代码（用于测试 TUI 各组件的渲染效果）
// ---------------------------------------------------------------------------

// const test = `只显示目录和可执行文件。以下是当前项目目录下的内容汇总：
//
// **根目录文件：**
// - ` + "`README.md`"
//
// func simulateEvents(ia *codingagent.Interactive) {
// 	// 等 TUI 初始化完成
// 	time.Sleep(1500 * time.Millisecond)
//
// 	// ── 1. Agent 启动 ──
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AgentStart, nil, nil))
// 	time.Sleep(500 * time.Millisecond)
//
// 	// ── 2. 用户消息 ──
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.UserMessage,
// 		apitypes.UserMessage{
// 			Text: "帮我查看当前目录文件列表，并告诉我各文件的作用。",
// 		}, nil))
// 	time.Sleep(800 * time.Millisecond)
//
// 	// ── 3. 思考过程（流式） ──
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantThinkingStart, nil, nil))
// 	time.Sleep(300 * time.Millisecond)
//
// 	updates := []string{
// 		"用户想查看当前目录...\n",
// 		"需要执行 ls 命令...\n",
// 		"同时分析各文件用途...\n",
// 	}
// 	for _, u := range updates {
// 		core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantThinkingUpdate, u, nil))
// 		time.Sleep(1000 * time.Millisecond)
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantThinkingEnd, nil, nil))
// 	time.Sleep(500 * time.Millisecond)
//
// 	// ── 4. 助理文本回复（流式 markdown） ──
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantTextStart, nil, nil))
// 	time.Sleep(200 * time.Millisecond)
//
// 	textParts := []string{
// 		test,
// 		"好的，我来帮你查看当前目录的文件列表。\n",
// 		"首先执行 `ls -la` 命令...\n",
// 		"执行结果\n我将使用 bash 工具来查看目录内容。\n",
// 		"## 2026年7月24日 A股大盘行情\n\n今日A股三大指数 **集体低开低走**，全市场超4900只个股下跌，情绪较为低迷。\n\n### 主要指数表现\n\n| 指数 | 开盘/点位 | 跌幅 |\n|:-----|:---------:|:----:|\n| **上证指数** | 3853.63 → 3830.19 | **-1.20%** |\n| **深证成指** | 13915.04 → 13873.50 | **-1.77%** |\n| **创业板指** | 3515.50 → 3511.75 | **-1.78%** |\n| **科创综指** | 1935.87 | **-1.32%** |\n\n### 板块分化\n\n- **涨幅居前**：军工装备、石油天然气、银行、煤炭、保险\n- **跌幅居前**：贵金属（-4.42%）、算力租赁、AI应用、半导体、有色金属、电力\n\n### 资金面\n\n- 两市成交额约 **12,253亿元**，较前日缩量约 **2,306亿元**\n- 央行净回笼 **3,615亿元**\n- 融资余额减少 **98.21亿元**\n\n### 总结\n\n今天市场整体呈现 **普跌、缩量** 的弱势格局。仅军工、石油、银行等少数权重板块护盘，前期热门的贵金属、半导体、算力方向集体回调。投资者情绪偏谨慎，等待进一步的政策信号。\n\n---\n\nSources:\n- [A股午评：创业板指半日跌1.78%，全市场超4900只个股飘绿](https://m.jiemian.com/article/14821533.html)\n- [四大股指低开，沪指跌0.6%，深成指跌1.47%，创业板跌1.68%](https://m.hexun.com/stock/2026-07-24/224699999.html)\n- [A股半导体强势拉升，电力大牛股7连板](https://fund.eastmoney.com/a/202607243820218996.html)\n- [四大指数集体低开 沪指跌0.6%](https://www.cnstock.com/commonDetail/750248)\n- [开评：三大指数低开 油气、能源设备等板块涨幅居前](https://www.egsea.com/news/detail?id=2319336)",
// 	}
// 	for _, p := range textParts {
// 		core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantTextUpdate, p, nil))
// 		time.Sleep(600 * time.Millisecond)
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AssistantTextEnd, nil, nil))
// 	time.Sleep(400 * time.Millisecond)
//
// 	// ── 5. 工具调用：Bash（覆盖正常、截断、超时、错误等场景） ──
// 	bashTests := []struct {
// 		id      string
// 		command string
// 		timeout int
// 	}{
// 		{"call-bash-1", "ls -la", 0},
// 		//{"call-bash-2", "for i in $(seq 1 2500); do echo \"line $i\"; done", 0},
// 		//{"call-bash-3", "yes \"01234567890123456789012345678901234567890123456789\" | head -n 1200", 0},
// 		//{"call-bash-4", "echo \"starting...\" && sleep 10 && echo \"done\"", 2},
// 		//{"call-bash-5", "echo \"stdout: normal output\" && echo \"stderr: error message\" >&2", 0},
// 		//{"call-bash-6", "echo \"before exit\" && exit 42", 0},
// 		//{"call-bash-7", "nonexistent_cmd_xyz_12345", 0},
// 	}
// 	for _, bt := range bashTests {
// 		args := map[string]any{"command": bt.command}
// 		if bt.timeout > 0 {
// 			args["timeout"] = float64(bt.timeout)
// 		}
// 		tc := apitypes.ToolCall{
// 			ID:        bt.id,
// 			Name:      "bash",
// 			Arguments: args,
// 		}
// 		core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, tc, nil))
// 		time.Sleep(300 * time.Millisecond)
//
// 		bashTool := tools.NewBashTool()
// 		bashCtx, bashErr := bashTool.Execute(context.Background(), types.ToolUseContext{
// 			ToolCall: tc,
// 			OnProgress: func(ctx types.ToolContext) {
// 				core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 			},
// 		})
// 		if bashErr != nil {
// 			bashCtx = types.ToolContext{Content: types.TextContent{Text: "Error: " + bashErr.Error()}}
// 		}
// 		time.Sleep(300 * time.Millisecond)
// 		core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, bashCtx, bashErr))
// 		time.Sleep(1200 * time.Millisecond)
// 	}
//
// 	// ── 6. 工具调用：Read（真实 read.Execute） ──
// 	readTool := tools.NewReadTool()
// 	readTC := apitypes.ToolCall{
// 		ID:   "call-read-1",
// 		Name: "read",
// 		Arguments: map[string]any{
// 			"path": "tests/main_test/test_edit_input.go",
// 		},
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, readTC, nil))
// 	time.Sleep(500 * time.Millisecond)
// 	readCtx, readErr := readTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: readTC,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	if readErr != nil {
// 		readCtx = types.ToolContext{Content: types.TextContent{Text: "Error: " + readErr.Error()}}
// 	}
// 	time.Sleep(500 * time.Millisecond)
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, readCtx, readErr))
// 	time.Sleep(800 * time.Millisecond)
//
// 	// 6b. 读取图片（nil client → 模拟不支持 vision，仅追加提示）
// 	readImgTC := apitypes.ToolCall{
// 		ID:   "call-read-img",
// 		Name: "read",
// 		Arguments: map[string]any{
// 			"path": "tests/main_test/testdata_large.jpg",
// 		},
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, readImgTC, nil))
// 	time.Sleep(500 * time.Millisecond)
// 	readImgCtx, readImgErr := readTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: readImgTC,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	if readImgErr != nil {
// 		readImgCtx = types.ToolContext{Content: types.TextContent{Text: "Error: " + readImgErr.Error()}}
// 	}
// 	time.Sleep(500 * time.Millisecond)
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, readImgCtx, readImgErr))
// 	time.Sleep(800 * time.Millisecond)
//
// 	// ── 7. 工具调用：Write（真实 write.Execute，新建 + 覆盖） ──
// 	writeTool := tools.NewWriteTool()
//
// 	// 7a. 新建文件（无 diff）
// 	writeTC1 := apitypes.ToolCall{
// 		ID:   "call-write-1",
// 		Name: "write",
// 		Arguments: map[string]any{
// 			"path":    "tests/main_test/test_write_output.txt",
// 			"content": "version 1\nline 2\nline 3",
// 		},
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, writeTC1, nil))
// 	time.Sleep(500 * time.Millisecond)
// 	writeCtx1, writeErr1 := writeTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: writeTC1,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	if writeErr1 != nil {
// 		writeCtx1 = types.ToolContext{Content: types.TextContent{Text: "Error: " + writeErr1.Error()}}
// 	}
// 	time.Sleep(500 * time.Millisecond)
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, writeCtx1, writeErr1))
// 	time.Sleep(800 * time.Millisecond)
//
// 	// 7b. 覆盖已有文件（展示 diff）
// 	writeTC2 := apitypes.ToolCall{
// 		ID:   "call-write-2",
// 		Name: "write",
// 		Arguments: map[string]any{
// 			"path":    "tests/main_test/test_write_output.txt",
// 			"content": "version 2\nline 2\nline 3\nline 4\nline 5",
// 		},
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, writeTC2, nil))
// 	time.Sleep(500 * time.Millisecond)
// 	writeCtx2, writeErr2 := writeTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: writeTC2,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	if writeErr2 != nil {
// 		writeCtx2 = types.ToolContext{Content: types.TextContent{Text: "Error: " + writeErr2.Error()}}
// 	}
// 	time.Sleep(500 * time.Millisecond)
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, writeCtx2, writeErr2))
// 	time.Sleep(800 * time.Millisecond)
//
// 	// ── 8. 工具调用：Edit（真实 edit.Execute） ──
// 	editTool := tools.NewEditTool()
//
// 	tc := apitypes.ToolCall{
// 		ID:   "call-edit-1",
// 		Name: "edit",
// 		Arguments: map[string]any{
// 			"path": "tests/main_test/test_edit_input.go",
// 			"edits": []any{
// 				map[string]any{
// 					"oldText": `import "fmt"`,
// 					"newText": `import (
// 		"fmt"
// 		"time"
// 			)`,
// 				},
// 				map[string]any{
// 					"oldText": `	fmt.Println("Hello")
// 	fmt.Println("World")`,
// 					"newText": `	fmt.Println("Hello, World!")
// 	fmt.Println("Current time:", time.Now())//你`,
// 				},
// 			},
// 		},
// 	}
//
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, tc, nil))
// 	time.Sleep(500 * time.Millisecond)
//
// 	resultCtx, execErr := editTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: tc,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	if execErr != nil {
// 		resultCtx = types.ToolContext{Content: types.TextContent{Text: execErr.Error()}}
// 	}
// 	time.Sleep(500 * time.Millisecond)
//
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, resultCtx, execErr))
// 	time.Sleep(800 * time.Millisecond)
//
// 	gittc := apitypes.ToolCall{
// 		ID:   "git-checkout",
// 		Name: "bash",
// 		Arguments: map[string]any{
// 			"command": "git checkout tests/main_test/test_edit_input.go",
// 		},
// 	}
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionStart, gittc, nil))
// 	// 恢复被 edit 修改的测试文件
// 	gitTool := tools.NewBashTool()
// 	resultCtx2, execErr2 := gitTool.Execute(context.Background(), types.ToolUseContext{
// 		ToolCall: gittc,
// 		OnProgress: func(ctx types.ToolContext) {
// 			core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionUpdate, ctx, nil))
// 		},
// 	})
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.ToolExecutionEnd, resultCtx2, execErr2))
//
// 	// ── 9. Agent 结束 ──
// 	core.Publish(core.AgentEventQueue, types.NewEventMessage(types.AgentEnd, nil, nil))
// 	time.Sleep(300 * time.Millisecond)
//
// }
