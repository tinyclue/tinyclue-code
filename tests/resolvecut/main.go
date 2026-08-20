// cmd/resolvecut 单独模拟 ResolveCut 的各个分支场景，验证切分决策。
//
// 用法（模型清单读 ~/.tinyclue/caches，可用 TINYCLUE_CACHE_DIR 覆盖，与 CWD 无关）：
//
//	go run ./cmd/resolvecut
//
// 每个 case 打印：路径构成（索引 / 消息类型 / id / token 数）、regionTotal、
// ResolveCut 实际输出、按算法规范手写的参考输出，并给出 PASS/FAIL。
// PASS/FAIL 依据参考实现（ReferenceCut，完全按 ResolveCut 的文档语义实现），
// 与 tiktoken 对具体文本的精确计数无关，因此对大小文本都鲁棒。
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

// sentinel 与 agent_session 包内 firstKeptEntryNone（"[next]"）一致。
const sentinel = "[next]"

// ---------------------------------------------------------------------------
// 消息构造
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

// ---------------------------------------------------------------------------
// 参考实现：完全按 ResolveCut 的文档语义手写，作为断言基准。
// ---------------------------------------------------------------------------

// ReferenceCut 复刻 ResolveCut 的决策逻辑（与 agent_compaction_v2.go 同构）：
// Recover → 全量；Default → 无切分点返回 boundaryStart，否则反向累计 token 找
// 保留目标并吸附到合法切分点，区间不足 KEEP_RECENT 时切在首个切分点。
func ReferenceCut(mode core_types.CompactMode, boundaryStart int, path []core_types.SessionEntry) (string, int) {
	if mode == core_types.CompactModeRecover {
		return sentinel, len(path)
	}

	// 1. 合法切分点：仅 U/A，不含 tool_result。
	var cutPoints []int
	for i := boundaryStart; i < len(path); i++ {
		if me, ok := path[i].(*core_types.MessageEntry); ok {
			switch me.Message.(type) {
			case apitypes.UserMessage, apitypes.AssistantMessage:
				cutPoints = append(cutPoints, i)
			}
		}
	}

	cut := boundaryStart
	if len(cutPoints) > 0 {
		// 2. 从尾部向前累计 token，找首个累计 >= KEEP_RECENT 的"保留目标"。
		acc := 0
		target, found := 0, false
		for i := len(path) - 1; i >= boundaryStart; i-- {
			if me, ok := path[i].(*core_types.MessageEntry); ok {
				acc += utils.EstimateTokens(me.Message)
				if acc >= agent_session.KEEP_RECENT_TOKENS {
					target, found = i, true
					break
				}
			}
		}

		switch {
		case !found: // 整个区间不足 KEEP_RECENT：保留整段，切在首个合法切分点。
			cut = cutPoints[0]
		default: // 吸附到首个 >= target 的切分点；没有则回落最后一个切分点。
			cut = -1
			for _, c := range cutPoints {
				if c >= target {
					cut = c
					break
				}
			}
			if cut < 0 {
				cut = cutPoints[len(cutPoints)-1]
			}
		}
	}

	if cut >= len(path) {
		return sentinel, len(path)
	}
	return path[cut].GetId(), cut
}

// ---------------------------------------------------------------------------
// 输出辅助
// ---------------------------------------------------------------------------

func kindOf(e core_types.SessionEntry) string {
	me, ok := e.(*core_types.MessageEntry)
	if !ok {
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

func regionTotal(path []core_types.SessionEntry, boundaryStart int) int {
	total := 0
	for i := boundaryStart; i < len(path); i++ {
		if me, ok := path[i].(*core_types.MessageEntry); ok {
			total += utils.EstimateTokens(me.Message)
		}
	}
	return total
}

// diagram 打印路径构成，并在切分点处插一个 "|"：左侧为摘要区，右侧为保留区。
func diagram(path []core_types.SessionEntry, boundaryStart, cut int) {
	fmt.Printf("  [boundaryStart=%d]\n", boundaryStart)
	line := "  "
	for i := 0; i < len(path); i++ {
		if i == cut {
			line += "| "
		}
		tok := 0
		if me, ok := path[i].(*core_types.MessageEntry); ok {
			tok = utils.EstimateTokens(me.Message)
		}
		line += fmt.Sprintf("[%d]%s %s %dt  ", i, kindOf(path[i]), path[i].GetId(), tok)
	}
	if cut == len(path) {
		line += "| "
	}
	fmt.Println(line)
	if cut < len(path) {
		fmt.Printf("  摘要区 [%d, %d)  保留区 [%d, %d)  KEEP_RECENT=%d\n",
			boundaryStart, cut, cut, len(path), agent_session.KEEP_RECENT_TOKENS)
	} else {
		fmt.Printf("  保留区为空 → 全量压缩（摘要区 [%d, %d)）\n", boundaryStart, len(path))
	}
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

type tc struct {
	name          string
	mode          core_types.CompactMode
	boundaryStart int
	path          []core_types.SessionEntry
	want          string // 期望行为说明（人类可读）
}

func main() {
	fmt.Printf("KEEP_RECENT_TOKENS = %d\n", agent_session.KEEP_RECENT_TOKENS)

	// 文本尺寸：huge 远超阈值；med 中量（4 条合计可越过阈值）；small 接近零。
	huge := strings.Repeat("0123456789abcdef", 10000) // ~160k chars，约 4 万 token
	med := strings.Repeat("word ", 3000)              // ~15k chars，约 3~5 千 token
	small := "hello"

	cases := []tc{
		{
			name:          "Case 1 空路径",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          nil,
			want:          "区间空 → 无合法切分点 → cutIndex(0) >= len(0) → 退化为全量压缩",
		},
		{
			name:          "Case 2 区间空（boundaryStart == len(path)）",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 2,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small)},
			want:          "上次压缩为 [next] 时最后一条是压缩条目，区间 [2,2) 空 → 全量压缩",
		},
		{
			name:          "Case 3 区间内全是 tool_result（无合法切分点）",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{t("tr0", "c0", huge), t("tr1", "c1", huge)},
			want:          "无 U/A 可切 → 摘要区空，保留区含全部 → 实际由 CompactV2 报 ErrNoContextToCompact",
		},
		{
			name:          "Case 4 区间不足 KEEP_RECENT",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", small), u("u3", small), a("a4", small)},
			want:          "无可压缩量 → 保留整段，切在首个合法切分点（摘要区空）",
		},
		{
			name:          "Case 5 尾部超阈值，target 直接落在 U/A 上",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), u("u2", small), a("a3", huge)},
			want:          "反向累计首个越界的是 a3（本身即 U/A）→ 切点=3，超长消息整体保留",
		},
		{
			name:          "Case 6 target 落在 tool_result，吸附到下一个 U/A",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", huge), a("a3", small), u("u4", small), a("a5", small)},
			want:          "tr2 不可切 → 向上取首个 >= 2 的 U/A（a3），不拆散 tool_call/tool_result",
		},
		{
			name:          "Case 7 target 在尾部但无 U/A 可切，回落最后切分点",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", huge)},
			want:          "尾部是 tool_result（不可切），无切分点 >= target → 回落最后一个切分点 a1",
		},
		{
			name:          "Case 8 尾部多段中量消息累计越过阈值",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), u("u2", med), a("a3", med), u("u4", med), a("a5", med)},
			want:          "多段中量在尾部累计越过 KEEP_RECENT → 保留目标落在较早的 u2",
		},
		{
			name:          "Case 9 Default + boundaryStart 偏移（区间前段已被上次压缩）",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 2,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", small), u("u3", small), a("a4", small), u("u5", small), a("a6", small)},
			want:          "区间 [2,7)，总量不足 KEEP_RECENT → 切在区间首个切分点 u3；u0/a1 不进摘要区",
		},
		{
			name:          "Case 10 Recover 全量（boundaryStart=0）",
			mode:          core_types.CompactModeRecover,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", small), u("u3", small), a("a4", small)},
			want:          "不预留最近消息 → 无条件 [next] + 全量摘要",
		},
		{
			name:          "Case 11 Recover + boundaryStart 非零",
			mode:          core_types.CompactModeRecover,
			boundaryStart: 1,
			path:          []core_types.SessionEntry{u("u0", small), a("a1", small), t("tr2", "c2", small), u("u3", small), a("a4", small)},
			want:          "Recover 不预留最近消息，无条件 [next]；摘要上界恒为 len(path)，摘要区为 [boundaryStart, len)",
		},
		{
			name:          "Case 12 区间不足 KEEP_RECENT 且 boundaryStart 落在 tool_result（cutPoints[0] != boundaryStart）",
			mode:          core_types.CompactModeDefault,
			boundaryStart: 0,
			path:          []core_types.SessionEntry{t("tr0", "c0", small), t("tr1", "c1", small), u("u2", small), a("a3", small)},
			want:          "反例：cutPoints=[2,3] 仅 u2/a3 可切；区间不足 KEEP_RECENT → return cutPoints[0]=2 ≠ boundaryStart=0 → 保留 [u2,a3]，摘要区 [0,2)=[tr0,tr1]",
		},
	}

	ac := agent_session.NewAgentCompaction(context.Background(), nil)

	pass := 0
	for _, c := range cases {
		fmt.Printf("\n===================== %s =====================\n", c.name)
		fmt.Printf("mode=%s boundaryStart=%d\n", modeName(c.mode), c.boundaryStart)

		gotId, gotEnd := ac.ResolveCut(c.mode, c.boundaryStart, c.path)
		wantId, wantEnd := ReferenceCut(c.mode, c.boundaryStart, c.path)
		total := regionTotal(c.path, c.boundaryStart)

		if len(c.path) == 0 {
			fmt.Println("  path: (empty)")
		}
		diagram(c.path, c.boundaryStart, gotEnd)

		fmt.Printf("  regionTotal=%d token\n", total)
		fmt.Printf("  ResolveCut -> firstKeptEntryId=%q summarizeEnd=%d\n", gotId, gotEnd)
		fmt.Printf("  Reference  -> firstKeptEntryId=%q summarizeEnd=%d\n", wantId, wantEnd)
		fmt.Printf("  期望行为: %s\n", c.want)

		ok := gotId == wantId && gotEnd == wantEnd
		if ok {
			pass++
		}
		fmt.Printf("  => %s\n", passFail(ok))
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
