package agent_session

import (
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"regexp"
	"strings"
	"time"
)

func (ac *AgentCompaction) PrepareCompactionV2(mode core_types.CompactMode, sessionFile string, entrys []core_types.SessionEntry) *core_types.PrepareCompactionContext {
	var path []core_types.SessionEntry
	if entrys == nil {
		path = ac.agentSession.GetConversationEntryPath()
	} else {
		path = entrys
	}
	boundaryStart, _ := ac.getBoundary(path) // v2 只用 boundaryStart，区间尾恒为 len(path)
	tokens := ac.EstimateEntryTokens(path)
	compaction, prevCompactionIndex := ac.agentSession.GetLastCompactionEntry(path)

	// 恢复性压缩需先补齐缺失的 tool_result，保证摘要区 tool_call/tool_result 配对完整。
	if mode == core_types.CompactModeRecover {
		path = ac.completeMissingToolResults(path, boundaryStart)
	}

	// 切分：产出保留区首条 id 与摘要区上界。
	firstKeptEntryId, summarizeEnd := ac.ResolveCut(mode, boundaryStart, path)
	summarize := ac.getMessageSummarizeV2(path, boundaryStart, summarizeEnd)

	fileOps := utils.NewFileOps()
	utils.ExtractCompactionOperations(path, prevCompactionIndex, fileOps)
	utils.ExtractFileOpsFromMessage(summarize, fileOps)
	//
	prevTools := utils.ExtractCompactionDiscoveredTool(path, prevCompactionIndex)
	discoveredTools := utils.ExtractDiscoveredToolsFromMessage(summarize, prevTools)

	return &core_types.PrepareCompactionContext{
		PrevTokens:       tokens,
		PrevCompaction:   compaction,
		FirstKeptEntryId: firstKeptEntryId,
		Summarize:        summarize,
		FileOps:          fileOps,
		DiscoveredTools:  discoveredTools,
		Mode:             mode,
		SessionFile:      sessionFile,
	}
}

// ResolveCut 计算压缩的两个输出：
//   - firstKeptEntryId：压缩后保留区首条 entry 的 id；保留区为空时用哨兵
//     firstKeptEntryNone。
//   - summarizeEnd：摘要区 [boundaryStart, summarizeEnd) 的排他上界。
//
// Default：findCutIndexV2 切出保留区起点；保留区为空（区间空）退化为全量压缩。
// Recover：不预留，整个区间全量摘要。
func (ac *AgentCompaction) ResolveCut(mode core_types.CompactMode, boundaryStart int, path []core_types.SessionEntry) (string, int) {
	if mode == core_types.CompactModeRecover {
		return firstKeptEntryNone, len(path)
	}

	cutIndex := ac.findCutIndexV2(boundaryStart, path)
	if cutIndex >= len(path) {
		// 保留区为空（区间空），退化为全量压缩。
		return firstKeptEntryNone, len(path)
	}
	return path[cutIndex].GetId(), cutIndex
}

func (ac *AgentCompaction) getMessageSummarizeV2(path []core_types.SessionEntry, boundaryStart int, boundaryEnd int) []core_types.SessionEntry {
	var result []core_types.SessionEntry
	for i := boundaryStart; i < boundaryEnd; i++ {
		//跳过types2.EntryTypeCompaction，之后请求大模型压缩时，会重新组装
		if path[i].Kind() == string(core_types.EntryTypeCompaction) {
			continue
		}
		result = append(result, path[i])
	}
	return result
}

func (ac *AgentCompaction) CompactV2(prepare *core_types.PrepareCompactionContext) (*core_types.CompactionContext, error) {

	var (
		summarizeResult string
		hasSummarize    = len(prepare.Summarize) > 0
	)
	if !hasSummarize {
		return nil, core_types.ErrNoContextToCompact
	}
	runner := utils.NewTaskRunner()

	utils.Add(runner, &summarizeResult, func() (error, string) {
		return ac.GenerateSummaryV2(prepare)
	})

	if err := runner.Run(); err != nil {
		return nil, err
	}

	var summary string

	summary = summarizeResult

	fileLists := utils.ComputeFileLists(prepare.FileOps)
	if fileOpsStr := utils.FormatFileOperations(fileLists); fileOpsStr != "" {
		summary += fileOpsStr
	}

	return &core_types.CompactionContext{
		Prepare:          prepare,
		Summary:          summary,
		FirstKeptEntryId: prepare.FirstKeptEntryId,
		Tokens:           prepare.PrevTokens,
		FileLists:        fileLists,
		DiscoveredTools:  prepare.DiscoveredTools,
		Mode:             prepare.Mode,
	}, nil
}

// firstKeptEntryNone 是"保留区为空"的哨兵 id：它不是任何真实 entry 的 id，
// GetEntriesAfterCompaction 反查不到时回落 compactionIndex+1。
const firstKeptEntryNone = "[next]"

// findCutIndexV2 计算压缩切分点：保留区为 [cut, len(path))，摘要区为
// [boundaryStart, cut)。返回的 cut 恒为合法切分点（user/assistant 消息），
// 保证不会切散 tool_call/tool_result 配对。
//
// 算法分两步：
//  1. 找"保留目标"：从尾部向前累计消息 token，首个累计 >= KEEP_RECENT 的下标
//     即 target——它及之后是最近要原样保留的内容。整个区间不足 KEEP_RECENT 时
//     无可压缩的量，切在区间首个合法切分点（等价于保留整段）。
//  2. 吸附到合法切分点：target 本身可能是 tool_result 等不可切消息，取第一个
//     >= target 的合法切分点作为 cut。若不存在（target 落在尾部无 U/A 的消息上），
//     回落到最后一个合法切分点，让尾部消息整体保留、摘要非空。
func (ac *AgentCompaction) findCutIndexV2(boundaryStart int, path []core_types.SessionEntry) int {
	cutPoints := ac.findValidCutPoints(path, boundaryStart, len(path))
	if len(cutPoints) == 0 {
		// 区间内没有 U/A 可作切分点：无可压缩内容。返回 boundaryStart 使摘要区为空，
		// 由 CompactV2 报 ErrNoContextToCompact 兜底（与 findCutIndex 行为一致）。
		return boundaryStart
	}

	target, ok := accumulateRecentTail(path, boundaryStart)
	if !ok {
		// 区间不足 KEEP_RECENT：无可压缩量，保留整段，切在首个合法切分点。
		return cutPoints[0]
	}

	if cut, ok := firstCutPointAtOrAfter(cutPoints, target); ok {
		return cut
	}
	// 尾部没有 U/A 可切，回落最后一个合法切分点。
	return cutPoints[len(cutPoints)-1]
}

// accumulateRecentTail 从末尾向前累计消息 token，返回首个让累计值 >= KEEP_RECENT
// 的下标；整个区间不足 KEEP_RECENT 时返回 (0, false)。
func accumulateRecentTail(path []core_types.SessionEntry, boundaryStart int) (int, bool) {
	accumulated := 0
	for i := len(path) - 1; i >= boundaryStart; i-- {
		me, ok := path[i].(*core_types.MessageEntry)
		if !ok {
			continue
		}
		accumulated += utils.EstimateTokens(me.Message)
		if accumulated >= KEEP_RECENT_TOKENS {
			return i, true
		}
	}
	return 0, false
}

// firstCutPointAtOrAfter 返回 cutPoints 中第一个 >= index 的下标；不存在返回
// (0, false)。cutPoints 由 findValidCutPoints 升序构造。
func firstCutPointAtOrAfter(cutPoints []int, index int) (int, bool) {
	for _, c := range cutPoints {
		if c >= index {
			return c, true
		}
	}
	return 0, false
}

// completeMissingToolResults 为最后一条带 tool_call 的 assistant 补齐缺失的
// tool_result 占位，返回补齐后的路径。占位插回其 tool_result 回合末尾，避免被
// 其后其他类型的消息隔断。无缺失时原样返回 path。
func (ac *AgentCompaction) completeMissingToolResults(path []core_types.SessionEntry, boundaryStart int) []core_types.SessionEntry {
	missing, insertIndex := ac.findMissingToolResults(path, boundaryStart)
	if insertIndex < 0 {
		return path
	}
	inserts := make([]core_types.SessionEntry, 0, len(missing))
	for _, item := range missing {
		inserts = append(inserts, &core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{
				Type:      core_types.EntryTypeMessage,
				Timestamp: time.Now(),
			},
			Message: item,
		})
	}
	path = append(path, inserts...)
	copy(path[insertIndex+len(inserts):], path[insertIndex:])
	copy(path[insertIndex:], inserts)
	return path
}

// findMissingToolResults 在 [startIndex, len(path)) 内找出最后一条带 tool_call 的
// assistant 消息及其紧邻的连续 tool_result 回合。若该 assistant 的 ToolCalls 数量
// 多于其后实际出现的 ToolResultMessage（例如工具执行被中断），为缺失的 call 生成
// 占位 ToolResultMessage，保证 tool_call/tool_result 配对完整，避免 API 报
// tool_call_id 不匹配。
//
// startIndex 传 boundaryStart（压缩边界，恒在 prevCompactionIndex 之后），保证：
//   - 只在会序列化进总结器的 summarize 区间内补占位；
//   - 插回点 insertIndex >= boundaryStart > prevCompactionIndex，splice 不会
//     位移压缩条目，prevCompactionIndex 相关下标保持有效。
//
// 返回值：
//   - missing：需要补插的占位结果；完整配对或无带 tool_call 的 assistant 时为 nil。
//   - insertIndex：占位结果应插入的下标（该 assistant 的 tool_result 回合末尾）；
//     无需补插时返回 -1。
func (ac *AgentCompaction) findMissingToolResults(path []core_types.SessionEntry, startIndex int) ([]apitypes.ToolResultMessage, int) {
	lastAssistantIndex := -1
	var lastAssistant apitypes.AssistantMessage
	for i := startIndex; i < len(path); i++ {
		me, ok := path[i].(*core_types.MessageEntry)
		if !ok {
			continue
		}
		msg, ok := me.Message.(apitypes.AssistantMessage)
		if !ok {
			continue
		}
		if len(msg.ToolCalls) == 0 {
			continue
		}
		lastAssistantIndex = i
		lastAssistant = msg
	}
	if lastAssistantIndex < 0 {
		return nil, -1
	}

	covered := make(map[string]struct{})
	insertIndex := lastAssistantIndex + 1
	for i := lastAssistantIndex + 1; i < len(path); i++ {
		me, ok := path[i].(*core_types.MessageEntry)
		if !ok {
			break
		}
		tr, ok := me.Message.(apitypes.ToolResultMessage)
		if !ok {
			break
		}
		covered[tr.ToolCallId] = struct{}{}
		insertIndex = i + 1
	}

	var missing []apitypes.ToolResultMessage
	for _, tc := range lastAssistant.ToolCalls {
		if _, ok := covered[tc.ID]; ok {
			continue
		}
		missing = append(missing, apitypes.ToolResultMessage{
			Role:       apitypes.ToolRole,
			ToolCallId: tc.ID,
			ToolName:   tc.Name,
			IsError:    true,
			Contents:   []apitypes.ContentBlock{{Type: "text", Text: "[Tool result missing due to internal error]"}},
		})
	}
	if len(missing) == 0 {
		return nil, -1
	}
	return missing, insertIndex
}

func (ac *AgentCompaction) GenerateSummaryV2(prepare *core_types.PrepareCompactionContext) (error, string) {

	// 首次压缩时 PrevCompaction 为 nil，需守卫后再取上一次摘要。
	var prevSummary string
	if prepare.PrevCompaction != nil {
		prevSummary = prepare.PrevCompaction.Summary
	}
	budget := ac.summarizeInputBudget(prevSummary)

	conversations, err := ac.SerializeConversationV2(prepare.Summarize, budget, prepare.FileOps)
	if err != nil {
		return err, ""
	}
	maxTokens := api_provider.GetClient().Adapter().MaxTokens()
	maxTokens = min(maxTokens, 20000)

	err, summary := ac.doSummaryV2(prevSummary, conversations, maxTokens)
	if err != nil {
		return err, summary
	}
	recentMessagesPreserved := true
	if prepare.Mode == core_types.CompactModeRecover {
		recentMessagesPreserved = false
	}
	// 仅恢复性压缩追加"直接继续、不要提问"的续接指令；默认压缩后模型应回答保留区最新消息。
	suppressFollowUpQuestions := prepare.Mode == core_types.CompactModeRecover
	summaryResult := getCompactUserSummaryMessage(summary, prepare.SessionFile, recentMessagesPreserved, suppressFollowUpQuestions)
	return nil, summaryResult
}

// SerializeConversationV2 把会话条目序列化为发送给总结器的消息列表。
//
// 先估算全部消息的 token 总量；若未超预算，原样返回（预算内零截断、全文保留）。
// 若超预算，复用 utils.OffloadToolResults：从后往前按"一次 assistant tool_call
// 回合"分组、组内按 token 从大到小，逐个把 tool_result 的文本块外置到
// ~/.tinyclue/tool-results/<tool_use_id>.txt，原位替换为 persisted-output 指针
// （含体积、完整路径与 2KB preview），并把临时路径写入 fileOps.Read，由 CompactV2
// 现有逻辑拼进 summary 的 <read-files>，供续接模型用 Read 工具读取全文。
// 全部外置完仍超预算，返回 error，由调用方一路向上透传。
func (ac *AgentCompaction) SerializeConversationV2(path []core_types.SessionEntry, budget int, fileOps *core_types.FileOperations) ([]apitypes.Message, error) {
	parts := make([]apitypes.Message, 0, len(path))
	for _, entry := range path {
		me, ok := entry.(*core_types.MessageEntry)
		if !ok {
			continue
		}
		// 拷贝 tool_result 的 Contents：外置替换会改写 Contents 里的文本，
		// 若不拷贝则共享的底层数组会把会话原始条目一并改掉。
		// ContentBlock 全为标量字段，一次切片拷贝即可彻底解耦。
		if tr, ok := me.Message.(apitypes.ToolResultMessage); ok {
			copied := make([]apitypes.ContentBlock, len(tr.Contents))
			copy(copied, tr.Contents)
			tr.Contents = copied
			parts = append(parts, tr)
			continue
		}
		parts = append(parts, me.Message)
	}

	total := utils.EstimateMessagesTokens(parts)
	if budget <= 0 || total <= budget {
		return parts, nil
	}

	result, err := utils.OffloadToolResults(parts, total, utils.EstimateTextTokens, budget)
	if err != nil {
		return nil, err
	}
	fileOps.Read = append(fileOps.Read, result.Files...)
	if result.Overflow {
		return nil, fmt.Errorf("Conversation too large for summarizer: %d tokens exceed budget %d after offloading all tool results.", result.Total, budget)
	}
	return parts, nil
}

// 计算总结器请求的输入 token 预算。
func (ac *AgentCompaction) summarizeInputBudget(prepareSummary string) int {

	window := GetEffectiveContextWindowSize()
	budget := window - utils.EstimateTextTokens(prompt.GetCompactPrompt()) - utils.EstimateTextTokens(prepareSummary)
	return budget
}

func (ac *AgentCompaction) doSummaryV2(prepareSummary string, conversations []apitypes.Message, maxToken int) (error, string) {
	systemMessage := apitypes.NewSystemMessage("You are a helpful AI assistant tasked with summarizing conversations.")
	promptMessage := apitypes.NewUserMessage(prompt.GetCompactPrompt())

	var messages []apitypes.Message
	messages = append(messages, systemMessage)
	// 首次压缩无上一次摘要，跳过空的 prepareSummary 消息。
	if prepareSummary != "" {
		messages = append(messages, apitypes.NewUserMessage(prepareSummary))
	}
	messages = append(messages, conversations...)
	messages = append(messages, promptMessage)
	req := &apitypes.ChatRequest{
		Messages:  messages,
		MaxTokens: &maxToken,
	}
	stream := api_provider.GetClient().Chat(ac.ctx, req)
	if stream.HasError() {
		errMsg := stream.Response().Error.ErrorMessage
		if errMsg == "" {
			errMsg = "Unknown error"
		}
		return fmt.Errorf("Summary generation failed: %s", errMsg), ""
	}

	return nil, stream.Response().Content.TextContent.Text
}

var (
	analysisSectionPattern = regexp.MustCompile(`(?s)<analysis>.*?</analysis>`)
	summarySectionPattern  = regexp.MustCompile(`(?s)<summary>(.*?)</summary>`)
	extraBlankLinesPattern = regexp.MustCompile(`\n\n+`)
)

// replaceFirst 仅替换第一个匹配（对应 JS String.replace 无 /g 标志时只处理首次匹配）。
func replaceFirst(pat *regexp.Regexp, s, repl string) string {
	loc := pat.FindStringIndex(s)
	if loc == nil {
		return s
	}
	return s[:loc[0]] + repl + s[loc[1]:]
}

// formatCompactSummary 格式化压缩摘要：
//  1. 去掉 <analysis>...</analysis> 草稿区（无信息价值）；
//  2. 把 <summary>...</summary> 内容替换为 "Summary:\n<content>"；
//  3. 合并多余空行为一个空行；
//  4. 去除首尾空白。
func formatCompactSummary(summary string) string {
	formatted := replaceFirst(analysisSectionPattern, summary, "")
	if m := summarySectionPattern.FindStringSubmatch(formatted); m != nil {
		content := strings.TrimSpace(m[1])
		formatted = replaceFirst(summarySectionPattern, formatted, "Summary:\n"+content)
	}
	formatted = extraBlankLinesPattern.ReplaceAllString(formatted, "\n\n")
	return strings.TrimSpace(formatted)
}

// getCompactUserSummaryMessage 构造压缩后的用户续接消息。
//   - suppressFollowUpQuestions 为 true 时追加"直接继续、不要提问"的续接指令；
//   - transcriptPath 非空时附上完整 transcript 路径提示；
//   - recentMessagesPreserved 为 true 时声明最近消息原样保留。
//
// 注：TS 原版里 feature('PROACTIVE')/feature('KAIROS')/proactiveModule 分支对应
// Tinyclue 内部的自主模式开关，Go 代码库没有对应机制，故省略该附加段落。
func getCompactUserSummaryMessage(summary string, transcriptPath string, recentMessagesPreserved bool, suppressFollowUpQuestions bool) string {
	formattedSummary := formatCompactSummary(summary)

	baseSummary := "This session is being continued from a previous conversation that ran out of context. The summary below covers the earlier portion of the conversation.\n\n" + formattedSummary

	if transcriptPath != "" {
		baseSummary += "\n\nIf you need specific details from before compaction (like exact code snippets, error messages, or content you generated), read the full transcript at: " + transcriptPath
	}

	if recentMessagesPreserved {
		baseSummary += "\n\nRecent messages are preserved verbatim."
	}

	if suppressFollowUpQuestions {
		return baseSummary + "\nContinue the conversation from where it left off without asking the user any further questions. Resume directly — do not acknowledge the summary, do not recap what was happening, do not preface with \"I'll continue\" or similar. Pick up the last task as if the break never happened."
	}

	return baseSummary
}
