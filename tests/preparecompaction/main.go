// cmd/preparecompaction 单独模拟 PrepareCompactionV2 的完整流程，覆盖各类 case。
//
// PrepareCompactionV2 只做"准备"：边界定位（getBoundary）、切分（ResolveCut）、
// 摘要区抽取、文件操作与已发现工具的提取；真正的摘要请求在 CompactV2（走 API）。
// 因此可以离线注入 entrys 直接驱动，无需真实会话。
//
// 用法（模型清单读 ~/.tinyclue/caches，可用 TINYCLUE_CACHE_DIR 覆盖，与 CWD 无关）：
//
//	go run ./cmd/preparecompaction
//
// 每个 case 打印输入路径、实际 ctx 输出，并与硬编码的期望值逐字段比对。
package main

import (
	"context"
	"fmt"
	"strings"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/agent_session"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
)

const sentinel = "[next]" // 与 agent_session 包内 firstKeptEntryNone 一致

// ---------------------------------------------------------------------------
// 消息 / 压缩条目构造
// ---------------------------------------------------------------------------

func u(id, text string) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message:          apitypes.UserMessage{Role: apitypes.UserRole, Text: text},
	}
}

func a(id, text string) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message:          apitypes.AssistantMessage{Role: apitypes.AssistantRole, TextContent: apitypes.TextContent{Text: text}},
	}
}

// aT 构造带 tool_call 的 assistant（TextContent 留空，仅工具调用）。
func aT(id string, calls ...apitypes.ToolCall) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message: apitypes.AssistantMessage{
			Role:      apitypes.AssistantRole,
			ToolCalls: calls,
		},
	}
}

// aUsage 构造带真实 usage（TotalTokens>0）的 assistant，用于触发
// EstimateEntryTokens 的 usage+trailing 分支。
func aUsage(id, text string, totalTokens int) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message: apitypes.AssistantMessage{
			Role:        apitypes.AssistantRole,
			TextContent: apitypes.TextContent{Text: text},
			Usage:       apitypes.Usage{TotalTokens: totalTokens},
		},
	}
}

func t(id, callID, text string) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message: apitypes.ToolResultMessage{
			Role:       apitypes.ToolRole,
			ToolCallId: callID,
			ToolName:   "bash",
			Contents:   []apitypes.ContentBlock{{Type: "text", Text: text}},
		},
	}
}

// tRef 构造带 tool_reference（用于已发现工具提取）的 tool_result。
func tRef(id, callID string, refs []map[string]any, text string) *core_types.MessageEntry {
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: id},
		Message: apitypes.ToolResultMessage{
			Role:           apitypes.ToolRole,
			ToolCallId:     callID,
			ToolName:       "bash",
			ToolReferences: refs,
			Contents:       []apitypes.ContentBlock{{Type: "text", Text: text}},
		},
	}
}

func call(id, name string, args map[string]any) apitypes.ToolCall {
	return apitypes.ToolCall{ID: id, Name: name, Arguments: args}
}

func comp(id, firstKept, summary string, readFiles, modifyFiles, tools []string) *core_types.CompactionEntry {
	return &core_types.CompactionEntry{
		SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeCompaction, Id: id},
		Summary:          summary,
		FirstKeptEntryId: firstKept,
		ReadFiles:        readFiles,
		ModifyFiles:      modifyFiles,
		DiscoveredTools:  tools,
	}
}

// ---------------------------------------------------------------------------
// 参考 / 输出辅助
// ---------------------------------------------------------------------------

// lastCompaction 找到最后一条压缩条目（与 AgentSession.GetLastCompactionEntry 同构）。
func lastCompaction(path []core_types.SessionEntry) (*core_types.CompactionEntry, int) {
	for i := len(path) - 1; i >= 0; i-- {
		if e, ok := path[i].(*core_types.CompactionEntry); ok {
			return e, i
		}
	}
	return nil, -1
}

// refBoundary 复刻 getBoundary：找 firstKeptEntryId 的最后一次出现，找不到回落
// compactionIndex+1。仅用于展示参考值。
func refBoundary(path []core_types.SessionEntry) int {
	comp, ci := lastCompaction(path)
	if comp == nil {
		return 0
	}
	for i := len(path) - 1; i >= 0; i-- {
		if path[i].GetId() == comp.FirstKeptEntryId {
			return i
		}
	}
	return ci + 1
}

// sumTokens 复刻 EstimateSessionTokens（assistant 无 Usage 时走此路径）。
func sumTokens(path []core_types.SessionEntry) int {
	total := 0
	for _, e := range path {
		switch v := e.(type) {
		case *core_types.MessageEntry:
			total += utils.EstimateTokens(v.Message)
		case *core_types.CompactionEntry:
			total += utils.EstimateTextTokens(v.Summary)
		}
	}
	return total
}

func entryIds(entries []core_types.SessionEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.GetId()
	}
	return ids
}

func kindOf(e core_types.SessionEntry) string {
	me, ok := e.(*core_types.MessageEntry)
	if !ok {
		if _, ok := e.(*core_types.CompactionEntry); ok {
			return "C"
		}
		return "?"
	}
	switch me.Message.(type) {
	case apitypes.UserMessage:
		return "U"
	case apitypes.AssistantMessage:
		return "A"
	case apitypes.ToolResultMessage:
		return "TR"
	}
	return "?"
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func modeName(m core_types.CompactMode) string {
	if m == core_types.CompactModeRecover {
		return "Recover"
	}
	return "Default"
}

// ---------------------------------------------------------------------------
// 用例表
// ---------------------------------------------------------------------------

type expect struct {
	boundaryStart int      // 期望边界（内部参考值，用于展示）
	hasPrev       bool     // 期望存在上次压缩
	firstKept     string   // 期望 FirstKeptEntryId
	summarize     []string // 期望 Summarize 的 entry id 序列（占位条目 id 为空串）
	read          []string
	written       []string
	edited        []string
	tools         []string
	phCallId      string // 非空时校验 Summarize 末条为缺失 tool_result 的占位结果
}

type tc struct {
	name          string
	mode          core_types.CompactMode
	sessionFile   string
	entrys        []core_types.SessionEntry
	ac            *agent_session.AgentCompaction // 非 nil 时用此实例（覆盖共享空会话，供会话路径 case）
	tokenOverride *int                           // 非 nil 时覆盖 PrevTokens 期望（默认 sumTokens(entrys)）
	want          expect
}

func intPtr(v int) *int { return &v }

func main() {
	small := "hi"
	huge := strings.Repeat("0123456789abcdef", 10000) // ~50000 token

	// Case 15 用真实会话填充，验证 entrys==nil 时走 GetConversationEntryPath。
	// AppendMessage 会链式建 parentId，as.file==nil 时不落盘，可离线使用。
	session15 := &agent_session.AgentSession{}
	first15 := session15.AppendMessage(apitypes.UserMessage{Role: apitypes.UserRole, Text: small})
	session15.AppendMessage(apitypes.AssistantMessage{Role: apitypes.AssistantRole, TextContent: apitypes.TextContent{Text: small}})
	firstId15 := first15.GetId()
	ac15 := agent_session.NewAgentCompaction(context.Background(), nil).WithSession(session15)

	cases := []tc{
		{
			name:        "Case 1 首次压缩 Default，区间不足 KEEP_RECENT",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s1.json",
			entrys: []core_types.SessionEntry{
				u("u0", small), a("a1", small), t("tr2", "c2", small), u("u3", small), a("a4", small),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "u0",
				summarize:     []string{},
			},
		},
		{
			name:        "Case 2 首次压缩 Default，尾部超阈值 target 命中 U/A",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s2.json",
			entrys: []core_types.SessionEntry{
				u("u0", small), a("a1", small), u("u2", small), a("a3", huge),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "a3",
				summarize:     []string{"u0", "a1", "u2"},
			},
		},
		{
			name:        "Case 3 首次压缩 Default，target 落在 tool_result 吸附下一个 U/A",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s3.json",
			entrys: []core_types.SessionEntry{
				u("u0", small), a("a1", small), t("tr2", "c2", huge), a("a3", small), u("u4", small), a("a5", small),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "a3",
				summarize:     []string{"u0", "a1", "tr2"},
			},
		},
		{
			name:        "Case 4 连续压缩 Default，边界定位到 firstKeptEntryId",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s4.json",
			entrys: []core_types.SessionEntry{
				comp("comp0", "u3", "prev summary", []string{"old.go"}, []string{"old_edit.go"}, []string{"Grep"}),
				u("u3", small), a("a4", small), t("tr5", "c5", small), u("u6", small), a("a7", small),
			},
			want: expect{
				boundaryStart: 1,
				hasPrev:       true,
				firstKept:     "u3",
				summarize:     []string{},
				read:          []string{"old.go"},
				edited:        []string{"old_edit.go"},
				tools:         []string{"Grep"},
			},
		},
		{
			name:        "Case 5 连续压缩 Default，firstKeptEntryId 为哨兵 [next]",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s5.json",
			entrys: []core_types.SessionEntry{
				comp("comp0", "[next]", "prev summary", nil, nil, nil),
				u("u1", small), a("a2", small), t("tr3", "c3", small), u("u4", small), a("a5", small),
			},
			want: expect{
				boundaryStart: 1,
				hasPrev:       true,
				firstKept:     "u1",
				summarize:     []string{},
			},
		},
		{
			name:        "Case 6 Recover，补齐缺失 tool_result（含 Read 文件提取）",
			mode:        core_types.CompactModeRecover,
			sessionFile: "/tmp/s6.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				aT("a1", call("c1", "Bash", map[string]any{"command": "ls"})),
				t("tr2", "c1", small),
				u("u3", small),
				aT("a4", call("c2", "Read", map[string]any{"path": "new.go"})),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "[next]",
				summarize:     []string{"u0", "a1", "tr2", "u3", "a4", ""}, // 末条为占位，id 为空
				read:          []string{"new.go"},
				phCallId:      "c2",
			},
		},
		{
			name:        "Case 7 Recover，多 tool_call 部分缺结果",
			mode:        core_types.CompactModeRecover,
			sessionFile: "/tmp/s7.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				aT("a1", call("c1", "Bash", map[string]any{"command": "ls"}), call("c2", "Read", map[string]any{"path": "x.go"})),
				t("tr2", "c1", small),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "[next]",
				summarize:     []string{"u0", "a1", "tr2", ""}, // 占位 c2
				read:          []string{"x.go"},                // a1 的 Read 调用在摘要区内
				phCallId:      "c2",
			},
		},
		{
			name:        "Case 8 首次压缩 Default，read/write/edit 文件操作提取",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s8.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				aT("a1", call("c1", "Read", map[string]any{"path": "a.txt"})),
				t("tr2", "c1", small),
				aT("a3", call("c2", "Write", map[string]any{"path": "b.txt"})),
				t("tr4", "c2", small),
				aT("a5", call("c3", "Edit", map[string]any{"path": "c.txt"})),
				t("tr6", "c3", small),
				u("u7", small),
				a("a8", huge),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "a8",
				summarize:     []string{"u0", "a1", "tr2", "a3", "tr4", "a5", "tr6", "u7"},
				read:          []string{"a.txt"},
				written:       []string{"b.txt"},
				edited:        []string{"c.txt"},
			},
		},
		{
			name:        "Case 9 连续压缩，工具发现 + 上次压缩 carry-over",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s9.json",
			entrys: []core_types.SessionEntry{
				comp("comp0", "u4", "prev summary", []string{"old.go"}, []string{"old_edit.go"}, []string{"Grep"}),
				u("u4", small), a("a5", small),
				tRef("tr6", "c6", []map[string]any{{"tool_name": "Bash"}, {"tool_name": "Read"}}, small),
				u("u7", small), a("a8", huge),
			},
			want: expect{
				boundaryStart: 1,
				hasPrev:       true,
				firstKept:     "a8",
				summarize:     []string{"u4", "a5", "tr6", "u7"},
				read:          []string{"old.go"},
				edited:        []string{"old_edit.go"},
				tools:         []string{"Bash", "Read", "Grep"},
			},
		},
		{
			name:        "Case 10 空路径",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s10.json",
			entrys:      []core_types.SessionEntry{},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "[next]",
				summarize:     []string{},
			},
		},
		{
			name:        "Case 11 Recover，占位插回 tool_result 回合中间（非尾部）",
			mode:        core_types.CompactModeRecover,
			sessionFile: "/tmp/s11.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				aT("a1", call("c1", "Bash", map[string]any{"command": "ls"}), call("c2", "Read", map[string]any{"path": "x.go"})),
				t("tr2", "c1", small),
				u("u3", small),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "[next]",
				summarize:     []string{"u0", "a1", "tr2", "", "u3"}, // 占位 c2 插回 tr2 之后、u3 之前
				read:          []string{"x.go"},                      // a1 的 Read 调用在摘要区内
				phCallId:      "c2",
			},
		},
		{
			name:        "Case 12 Recover，tool_call/tool_result 配对完整（不补占位）",
			mode:        core_types.CompactModeRecover,
			sessionFile: "/tmp/s12.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				aT("a1", call("c1", "Bash", map[string]any{"command": "ls"})),
				t("tr2", "c1", small),
				u("u3", small),
			},
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "[next]",
				summarize:     []string{"u0", "a1", "tr2", "u3"},
			},
		},
		{
			name:          "Case 13 EstimateEntryTokens 走 usage 分支",
			mode:          core_types.CompactModeDefault,
			sessionFile:   "/tmp/s13.json",
			entrys:        []core_types.SessionEntry{u("u0", small), aUsage("a1", small, 100)},
			tokenOverride: intPtr(100), // usage.TotalTokens + 其后无消息
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     "u0",
				summarize:     []string{},
			},
		},
		{
			name:        "Case 14 压缩条目在 path 尾部（区间空退化）",
			mode:        core_types.CompactModeDefault,
			sessionFile: "/tmp/s14.json",
			entrys: []core_types.SessionEntry{
				u("u0", small),
				a("a1", small),
				comp("comp1", "nonexistent", "prev summary", nil, nil, nil),
			},
			want: expect{
				boundaryStart: 3,
				hasPrev:       true,
				firstKept:     "[next]",
				summarize:     []string{},
			},
		},
		{
			name:          "Case 15 entrys==nil 走会话路径",
			mode:          core_types.CompactModeDefault,
			sessionFile:   "/tmp/s15.json",
			entrys:        nil,
			ac:            ac15,
			tokenOverride: intPtr(2),
			want: expect{
				boundaryStart: 0,
				hasPrev:       false,
				firstKept:     firstId15,
				summarize:     []string{},
			},
		},
	}

	ac := agent_session.NewAgentCompaction(context.Background(), nil).
		WithSession(&agent_session.AgentSession{})

	pass := 0
	for _, c := range cases {
		fmt.Printf("\n===================== %s =====================\n", c.name)
		fmt.Printf("mode=%s sessionFile=%q\n", modeName(c.mode), c.sessionFile)

		// 输入路径
		bs := refBoundary(c.entrys)
		for i, e := range c.entrys {
			tok := ""
			if me, ok := e.(*core_types.MessageEntry); ok {
				tok = fmt.Sprintf(" %dt", utils.EstimateTokens(me.Message))
			}
			fmt.Printf("  [%d]%s %s%s\n", i, kindOf(e), e.GetId(), tok)
		}

		useAC := ac
		if c.ac != nil {
			useAC = c.ac
		}
		ctx := useAC.PrepareCompactionV2(c.mode, c.sessionFile, c.entrys)
		got := expect{
			hasPrev:   ctx.PrevCompaction != nil,
			firstKept: ctx.FirstKeptEntryId,
			summarize: entryIds(ctx.Summarize),
			read:      ctx.FileOps.Read,
			written:   ctx.FileOps.Written,
			edited:    ctx.FileOps.Edited,
			tools:     ctx.DiscoveredTools,
		}

		wantTokens := sumTokens(c.entrys)
		if c.tokenOverride != nil {
			wantTokens = *c.tokenOverride
		}

		// 占位结果校验（Recover 补的缺失 tool_result）。占位可能插在中间（Case 11），
		// 故扫描整个摘要区；位置由 want.summarize 的 id 序列（含空串占位）精确校验。
		phOK := true
		if c.want.phCallId != "" {
			phOK = false
			for _, e := range ctx.Summarize {
				me, ok := e.(*core_types.MessageEntry)
				if !ok {
					continue
				}
				tr, ok := me.Message.(apitypes.ToolResultMessage)
				if !ok {
					continue
				}
				if tr.ToolCallId == c.want.phCallId && tr.IsError &&
					len(tr.Contents) == 1 && tr.Contents[0].Text == "[Tool result missing due to internal error]" {
					phOK = true
					break
				}
			}
		}

		fmt.Printf("  boundaryStart(ref)=%d  PrevTokens=%d (期望 %d)\n", bs, ctx.PrevTokens, wantTokens)
		if ctx.PrevCompaction != nil {
			fmt.Printf("  PrevCompaction=%s firstKeptEntryId=%q\n", ctx.PrevCompaction.GetId(), ctx.PrevCompaction.FirstKeptEntryId)
		} else {
			fmt.Println("  PrevCompaction=nil")
		}
		fmt.Printf("  FirstKeptEntryId=%q  Summarize(%d)=%v\n", ctx.FirstKeptEntryId, len(ctx.Summarize), entryIds(ctx.Summarize))
		fmt.Printf("  FileOps Read=%v Written=%v Edited=%v\n", ctx.FileOps.Read, ctx.FileOps.Written, ctx.FileOps.Edited)
		fmt.Printf("  DiscoveredTools=%v\n", ctx.DiscoveredTools)

		w := c.want
		checks := []struct {
			name string
			ok   bool
		}{
			{"boundaryStart(ref) 匹配", bs == w.boundaryStart},
			{"hasPrev", got.hasPrev == w.hasPrev},
			{"FirstKeptEntryId", got.firstKept == w.firstKept},
			{"Summarize ids", same(got.summarize, w.summarize)},
			{"FileOps.Read", same(got.read, w.read)},
			{"FileOps.Written", same(got.written, w.written)},
			{"FileOps.Edited", same(got.edited, w.edited)},
			{"DiscoveredTools", same(got.tools, w.tools)},
			{"PrevTokens", ctx.PrevTokens == wantTokens},
			{"Mode", ctx.Mode == c.mode},
			{"SessionFile", ctx.SessionFile == c.sessionFile},
			{"占位 tool_result", phOK},
		}

		allOK := true
		for _, ch := range checks {
			status := "PASS"
			if !ch.ok {
				status = "FAIL"
				allOK = false
			}
			fmt.Printf("  [%s] %s\n", status, ch.name)
		}
		if allOK {
			pass++
		}
		fmt.Printf("  => %s\n", passFail(allOK))
	}

	fmt.Printf("\n===== 结果: %d/%d PASS =====\n", pass, len(cases))
	if pass != len(cases) {
		fmt.Println("FAIL")
	} else {
		fmt.Println("ALL PASS")
	}
}

func passFail(ok bool) string {
	if ok {
		return "[PASS]"
	}
	return "[FAIL]"
}
