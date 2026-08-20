package agent_session

import (
	"context"
	"fmt"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"math"
	"strings"
	"time"
)

const (
	AUTOCOMPACT_BUFFER_TOKENS = 16000 //16K
	KEEP_RECENT_TOKENS        = 10000 // 10k

	TOOL_RESULT_MAX_CHARS = 2000
)

type AgentCompaction struct {
	ctx          context.Context
	runtimeCtx   *core_types.AgentRuntimeContext
	agentSession *AgentSession
}

func GetEffectiveContextWindowSize() int {
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	maxTokens := api_provider.GetClient().Adapter().MaxTokens()
	return contextWindow - min(maxTokens, 20000)
}

func GetAutoCompactThreshold() int {
	window := GetEffectiveContextWindowSize()
	return window - AUTOCOMPACT_BUFFER_TOKENS
}

// ValidateWindow 运行时校验当前模型的上下文窗口是否足以运行 agent。
// overhead 为一次请求中不可压缩的静态开销（系统提示 + 工具定义 + virtual/deferred）。
//
// 要求：effectiveWindow  > overhead + summary_max + KEEP_RECENT + AUTOCOMPACT_BUFFER。
// 推导：压缩触发时 overhead + summary + 会话 = threshold = effectiveWindow - BUFFER，
// 压缩非空要求会话 > KEEP_RECENT（保留区），代入得上式。summary_max 取
// min(maxTokens, 20000)。不满足则模型必然陷入"静态开销单独顶满阈值、压缩空摘要"
// 的死循环，直接拒绝。窗口未知（<=0）时放行，避免误杀未知模型。
func ValidateWindow(overhead int) error {
	client := api_provider.GetClient()
	if client == nil {
		return nil
	}
	window := GetEffectiveContextWindowSize()
	if window <= 0 {
		return nil
	}
	outputReserve := min(client.Adapter().MaxTokens(), 20000)                // 响应输出预留
	summaryTokens := outputReserve                                           //压缩上下文最大等于输出预留
	margin := summaryTokens + KEEP_RECENT_TOKENS + AUTOCOMPACT_BUFFER_TOKENS //输入预留
	if window <= margin+overhead {
		// window = contextWindow - summaryTokens，故换算回模型原生 context window：
		// contextWindow > margin + overhead + summaryTokens 才够用。
		requiredWindow := margin + overhead + outputReserve
		return fmt.Errorf("model context window must be at least %s to run the agent", utils.FormatTokens(requiredWindow))
	}
	return nil
}

func NewAgentCompaction(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentCompaction {
	return &AgentCompaction{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (ac *AgentCompaction) Init() {
}

func (ac *AgentCompaction) WithSession(agentSession *AgentSession) *AgentCompaction {
	ac.agentSession = agentSession
	return ac
}

func (ac *AgentCompaction) CheckUserLimit(userMessage apitypes.UserMessage) bool {
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	userToken := utils.EstimateTokens(userMessage)
	usetTokenLimit := ac.ShouldCompact(userToken, contextWindow)
	return usetTokenLimit
}

func (ac *AgentCompaction) CheckCompactByUser(userMessage apitypes.UserMessage) bool {
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	userToken := utils.EstimateTokens(userMessage)
	path := ac.agentSession.GetConversationEntryPath()

	// 如果最近一次 compact 发生在所有 assistant 回复之后，说明上下文已清理，无需压缩
	if ac.isCompactionNewerThanAllAssistants(path) {
		return false
	}

	entryTokens := ac.EstimateEntryTokens(path)
	return ac.ShouldCompact(userToken+entryTokens, contextWindow)
}

func (ac *AgentCompaction) NeedCompact() bool {
	contextWindow := api_provider.GetClient().Adapter().ContextWindow()
	path := ac.agentSession.GetConversationEntryPath()
	// 如果最近一次 compact 发生在所有 assistant 回复之后，说明上下文已清理，无需压缩
	if ac.isCompactionNewerThanAllAssistants(path) {
		return false
	}
	entryTokens := ac.EstimateEntryTokens(path)
	return ac.ShouldCompact(entryTokens, contextWindow)
}

// isCompactionNewerThanAllAssistants 检查最近一条 compaction 的时间戳是否
// 晚于所有 assistant 消息的时间戳。如果是，说明 compaction 之后没有产生新的
// 对话内容，当前上下文应远低于压缩阈值。
func (ac *AgentCompaction) isCompactionNewerThanAllAssistants(path []core_types.SessionEntry) bool {
	var lastCompactTime time.Time
	var lastAssistantTime time.Time

	for _, entry := range path {
		switch e := entry.(type) {
		case *core_types.CompactionEntry:
			if e.Timestamp.After(lastCompactTime) {
				lastCompactTime = e.Timestamp
			}
		case *core_types.MessageEntry:
			if e.Message.RoleName() == apitypes.AssistantRole {
				if e.Timestamp.After(lastAssistantTime) {
					lastAssistantTime = e.Timestamp
				}
			}
		}
	}

	return !lastCompactTime.IsZero() && lastCompactTime.After(lastAssistantTime)
}

func (ac *AgentCompaction) ShouldCompact(tokens int, contextWindow int) bool {
	return tokens >= GetAutoCompactThreshold()
}

// EstimateCurrentTokens estimates total context tokens for the session.
// Uses the last valid API usage as the baseline, then estimates tokens for any
// entries that came after (trailing messages not reflected in that usage).
func (ac *AgentCompaction) EstimateEntryTokens(entrys []core_types.SessionEntry) int {
	usage, index := ac.getLastCorrectUsage(entrys)
	if index < 0 {
		return ac.EstimateSessionTokens(entrys)
	}
	//寻找到了最后一条正常的usage数据，usage后边的消息，全部采用估算形式
	trailing := ac.getTrailingTokens(index, entrys)
	return usage.TotalTokens + trailing
}

// EstimateSessionTokens estimates the total token count from session entries.
//
// - types2.MessageEntry: delegates to the char/4 heuristic via utils.EstimateTokens.
// - types2.CompactionEntry: uses the stored token count from actual API usage.
// - Other entry types (types2.HeaderEntry, etc.) are skipped.
func (ac *AgentCompaction) EstimateSessionTokens(entrys []core_types.SessionEntry) int {
	total := 0
	for _, entry := range entrys {
		switch e := entry.(type) {
		case *core_types.MessageEntry:
			total += utils.EstimateTokens(e.Message)
		case *core_types.CompactionEntry:
			total += utils.EstimateTextTokens(e.Summary)
		}
	}
	return total
}

// getTrailingTokens estimates token count for entries after lastUsageIndex.
func (ac *AgentCompaction) getTrailingTokens(lastUsageIndex int, entries []core_types.SessionEntry) int {
	return ac.EstimateSessionTokens(entries[lastUsageIndex+1:])
}

// getLastUsage walks backwards through session entries to find the most recent
// assistant message with valid usage data (non-aborted, non-error).
// Returns the usage and true if found.
func (ac *AgentCompaction) getLastCorrectUsage(entries []core_types.SessionEntry) (apitypes.Usage, int) {
	for i := len(entries) - 1; i >= 0; i-- {
		e, ok := entries[i].(*core_types.MessageEntry)
		if !ok {
			continue
		}
		if e.Message.RoleName() != apitypes.AssistantRole {
			continue
		}
		msg, ok := e.Message.(apitypes.AssistantMessage)
		if !ok {
			continue
		}
		if msg.Usage.TotalTokens > 0 {
			return msg.Usage, i
		}
	}
	return apitypes.Usage{}, -1
}

func (ac *AgentCompaction) PrepareCompaction() *core_types.PrepareCompactionContext {
	path := ac.agentSession.GetConversationEntryPath()
	boundaryStart, boundaryEnd := ac.getBoundary(path)
	tokens := ac.EstimateEntryTokens(path)
	compaction, prevCompactionIndex := ac.agentSession.GetLastCompactionEntry(path)

	firstKeptEntryIndex := ac.findCutIndex(boundaryStart, boundaryEnd, path)
	turnStartIndex := ac.findTurnStartIndex(path, firstKeptEntryIndex, boundaryStart)

	firstKeptEntry := path[firstKeptEntryIndex]

	summarize := ac.getMessageSummarize(path, firstKeptEntryIndex, turnStartIndex, boundaryStart)
	turnPrefixSummarize := ac.getMessageTurnPrefixSummarize(path, firstKeptEntryIndex, turnStartIndex)
	fileOps := utils.NewFileOps()
	utils.ExtractCompactionOperations(path, prevCompactionIndex, fileOps)
	utils.ExtractFileOpsFromMessage(summarize, fileOps)
	utils.ExtractFileOpsFromMessage(turnPrefixSummarize, fileOps)
	//
	prevTools := utils.ExtractCompactionDiscoveredTool(path, prevCompactionIndex)
	discoveredTools := utils.ExtractDiscoveredToolsFromMessage(summarize, prevTools)
	discoveredTools = utils.ExtractDiscoveredToolsFromMessage(turnPrefixSummarize, discoveredTools)

	return &core_types.PrepareCompactionContext{
		PrevTokens:          tokens,
		PrevCompaction:      compaction,
		FirstKeptEntryId:    firstKeptEntry.GetId(),
		Summarize:           summarize,
		TurnPrefixSummarize: turnPrefixSummarize,
		FileOps:             fileOps,
		DiscoveredTools:     discoveredTools,
	}
}

func (ac *AgentCompaction) Compact(prepare *core_types.PrepareCompactionContext) (*core_types.CompactionContext, error) {

	var (
		summarizeResult string
		prefixResult    string
		hasSummarize    = len(prepare.Summarize) > 0
		hasTurnPrefix   = len(prepare.TurnPrefixSummarize) > 0
	)
	if !hasSummarize && !hasTurnPrefix {
		return nil, core_types.ErrNoContextToCompact
	}
	runner := utils.NewTaskRunner()

	if hasSummarize {
		utils.Add(runner, &summarizeResult, func() (error, string) {
			return ac.GenerateSummary(prepare)
		})
	}
	if hasTurnPrefix {
		utils.Add(runner, &prefixResult, func() (error, string) {
			return ac.GenerateTurnPrefixSummary(prepare)
		})
	}

	if err := runner.Run(); err != nil {
		return nil, err
	}

	var summary string
	if hasTurnPrefix {
		if !hasSummarize {
			summarizeResult = "No prior history."
		}
		summary = summarizeResult + "\n\n---\n\n**Turn Context (split turn):**\n\n" + prefixResult
	} else {
		summary = summarizeResult
	}

	fileLists := utils.ComputeFileLists(prepare.FileOps)
	if fileOpsStr := utils.FormatFileOperations(fileLists); fileOpsStr != "" {
		summary += fileOpsStr
	}

	return &core_types.CompactionContext{
		Prepare:          prepare,
		Summary:          summary,
		FirstKeptEntryId: prepare.FirstKeptEntryId,
		FileLists:        fileLists,
		DiscoveredTools:  prepare.DiscoveredTools,
		Mode:             core_types.CompactModeDefault,
	}, nil
}

func (ac *AgentCompaction) GenerateTurnPrefixSummary(prepare *core_types.PrepareCompactionContext) (error, string) {

	conversationText := ac.SerializeConversation(prepare.TurnPrefixSummarize)

	promptText := "<conversation>"
	promptText += "\\n"
	promptText += conversationText
	promptText += "\\n"
	promptText += "</conversation>"
	promptText += "\\n\\n"
	promptText += prompt.TURN_PREFIX_SUMMARIZATION_PROMPT

	maxToken := int(math.Min(math.Floor(0.5*float64(AUTOCOMPACT_BUFFER_TOKENS)), float64(api_provider.GetClient().Adapter().MaxTokens())))

	return ac.doSummary(promptText, maxToken)
}

func (ac *AgentCompaction) GenerateSummary(prepare *core_types.PrepareCompactionContext) (error, string) {

	basePrompt := prompt.SUMMARIZATION_PROMPT
	if prepare.PrevCompaction != nil {
		basePrompt = prompt.UPDATE_SUMMARIZATION_PROMPT
	}

	conversationText := ac.SerializeConversation(prepare.Summarize)
	promptText := "<conversation>"
	promptText += "\n"
	promptText += conversationText
	promptText += "\n"
	promptText += "</conversation>"
	promptText += "\n\n"

	if prepare.PrevCompaction != nil {
		promptText += "<previous-summary>"
		promptText += "\n"
		promptText += prepare.PrevCompaction.Summary
		promptText += "\n"
		promptText += "</previous-summary>"
		promptText += "\n\n"
	}
	promptText = promptText + basePrompt

	maxToken := int(math.Min(math.Floor(0.8*float64(AUTOCOMPACT_BUFFER_TOKENS)), float64(api_provider.GetClient().Adapter().MaxTokens())))

	return ac.doSummary(promptText, maxToken)
}

func (ac *AgentCompaction) doSummary(promptText string, maxToken int) (error, string) {
	userMessage := apitypes.NewUserMessage(promptText)
	systemMessage := apitypes.NewSystemMessage(prompt.SUMMARIZATION_SYSTEM_PROMPT)

	var messages []apitypes.Message
	messages = append(messages, systemMessage)
	messages = append(messages, userMessage)
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

func (ac *AgentCompaction) truncateForSummary(text string, maxChars int) string {
	if len(text) <= maxChars {
		return text
	}
	truncatedChars := len(text) - maxChars
	return text[:maxChars] + fmt.Sprintf("\n\n[... %d more characters truncated]", truncatedChars)
}

// SerializeConversation serializes session entries to plain text for summarization.
// This prevents the model from treating it as a conversation to continue.
func (ac *AgentCompaction) SerializeConversation(path []core_types.SessionEntry) string {
	var parts []string

	for _, entry := range path {
		me, ok := entry.(*core_types.MessageEntry)
		if !ok {
			continue
		}

		switch msg := me.Message.(type) {
		case apitypes.UserMessage:
			if msg.Text != "" {
				parts = append(parts, "[User]: "+msg.Text)
			}

		case apitypes.AssistantMessage:
			if msg.ThinkingContent.Thinking != "" {
				parts = append(parts, "[Assistant thinking]: "+msg.ThinkingContent.Thinking)
			}
			if msg.TextContent.Text != "" {
				parts = append(parts, "[Assistant]: "+msg.TextContent.Text)
			}
			if len(msg.ToolCalls) > 0 {
				var toolParts []string
				for _, tc := range msg.ToolCalls {
					var args []string
					for k, v := range tc.Arguments {
						args = append(args, fmt.Sprintf("%s=%v", k, v))
					}
					toolParts = append(toolParts, tc.Name+"("+strings.Join(args, ", ")+")")
				}
				parts = append(parts, "[Assistant tool calls]: "+strings.Join(toolParts, "; "))
			}

		case apitypes.ToolResultMessage:
			for _, c := range msg.Contents {
				if c.Type == "text" && c.Text != "" {
					parts = append(parts, "[Tool result]: "+ac.truncateForSummary(c.Text, TOOL_RESULT_MAX_CHARS))
				}
			}
		}
	}

	return strings.Join(parts, "\n\n")
}

func (ac *AgentCompaction) getBoundary(path []core_types.SessionEntry) (int, int) {
	boundaryStart := 0
	boundaryEnd := len(path)
	compaction, compactionIndex := ac.agentSession.GetLastCompactionEntry(path)
	if compaction != nil {
		firstKeptEntryIndex := -1
		for i := len(path) - 1; i >= 0; i-- {
			if path[i].GetId() == compaction.FirstKeptEntryId {
				firstKeptEntryIndex = i
				break
			}
		}
		if firstKeptEntryIndex >= 0 {
			boundaryStart = firstKeptEntryIndex
		} else {
			boundaryStart = compactionIndex + 1
		}
	}
	return boundaryStart, boundaryEnd
}

// findValidCutPoints returns indices of entries that are valid cut points for
// compaction. A valid cut point is a user or assistant message entry.
// Entries between startIndex (inclusive) and endIndex (exclusive) are considered.
func (ac *AgentCompaction) findValidCutPoints(entries []core_types.SessionEntry, startIndex, endIndex int) []int {
	var cutPoints []int
	for i := startIndex; i < endIndex; i++ {
		me, ok := entries[i].(*core_types.MessageEntry)
		if !ok {
			continue
		}
		switch me.Message.(type) {
		//有效切分点，不要toolResult消息，预防toolCall和toolResult被切分，导致消息无法对齐
		case apitypes.UserMessage, apitypes.AssistantMessage:
			cutPoints = append(cutPoints, i)
		}
	}
	return cutPoints
}

// 举例子：
//
// cutPoints = [4(user), 5(asst), 7(asst)]     ← toolResult(6) 不在里面
// |        |         |
// E4(user) ← E5(asst) ← E6(tool) ← E7(asst)
// ↑
// 如果我们从后往前累积 token:
// E7=300, E6=800, E5=200, E4=100
// keepRecentTokens=1500
//
// E7=300 < 1500, 继续
// E7+E6=1100 < 1500, 继续
// E7+E6+E5=1300 < 1500, 继续
// E7+E6+E5+E4=1400 还不到 1500
//
// 假设换个场景:
// E7=200, E6=2000 (超大文件输出)
// E7=200, accumulatedTokens=200 < 1500
// E6=2000, accumulatedTokens=2200 >= 1500 ← i=6
//
// 此时 i=6(E6) 是 toolResult，不是合法切分点。直接 cutIndex = i 会切在 toolResult 上，但 toolResult 不能当切分点（否则会和前面的 tool call 断开）。所以需要找到最近的合法切分点：
//
// for (let c = 0; c < cutPoints.length; c++) {
// if (cutPoints[c] >= i) {  // 找到第一个 >= 6 的合法切分点
// cutIndex = cutPoints[c]; // cutPoints = [4,5,7] → cutIndex = 7
// break;
// }
// }
//
// 结果 cutIndex = 7（E7 assistant），所有 toolResult 保持在保留区内，不会和前面的 call 拆散。
func (ac *AgentCompaction) findCutIndex(boundaryStart int, boundaryEnd int, path []core_types.SessionEntry) int {
	cutPoints := ac.findValidCutPoints(path, boundaryStart, boundaryEnd)
	// 默认从 boundaryStart 起保留；若 boundaryStart 不是合法切分点，则退到其后第一个
	// 合法切分点（user/assistant），避免切在 tool_result 上。
	// 之前默认 0 会把 path[0]（最老一条）当保留起点，重建时把整段旧历史带回来。
	cutIndex := boundaryStart
	if len(cutPoints) > 0 {
		cutIndex = cutPoints[0]
	}
	accumulatedTokens := 0
	for i := boundaryEnd - 1; i >= boundaryStart; i-- {
		//只计算对话，不算压缩摘要
		e, ok := path[i].(*core_types.MessageEntry)
		if !ok {
			continue
		}
		token := utils.EstimateTokens(e.Message)
		accumulatedTokens += token
		if accumulatedTokens >= KEEP_RECENT_TOKENS {
			// 找到第一个 ≥ i 的合法切分点，保证 i 处的超长单条消息（如超大 tool_result）
			// 落在保留区内，避免与保留区后的内容拆散。
			for j := 0; j < len(cutPoints); j++ {
				if cutPoints[j] >= i {
					return cutPoints[j]
				}
			}
			// 无 ≥ i 的切分点：i 落在最后一条非 user/assistant 消息上（如超大 tool_result），
			// 回落到最后一个合法切分点，保证 summarize 非空且尾部消息整体保留。
			if len(cutPoints) > 0 {
				return cutPoints[len(cutPoints)-1]
			}
			return boundaryStart
		}
	}
	return cutIndex
}

func (ac *AgentCompaction) findTurnStartIndex(path []core_types.SessionEntry, cutIndex int, boundaryStart int) int {
	if ac.isUserRoleMessage(path, cutIndex) {
		return -1
	}
	for i := cutIndex; i >= boundaryStart; i-- {
		if ac.isUserRoleMessage(path, i) {
			return i
		}
	}
	return -1
}

func (ac *AgentCompaction) isUserRoleMessage(path []core_types.SessionEntry, index int) bool {
	entry, ok := path[index].(*core_types.MessageEntry)
	if !ok {
		return false
	}
	message, ok2 := entry.Message.(apitypes.UserMessage)
	if !ok2 {
		return false
	}
	if message.Role == apitypes.UserRole {
		return true
	}
	return false
}

func (ac *AgentCompaction) isSplitTurn(path []core_types.SessionEntry, cutIndex int, turnStartIndex int) bool {
	if !ac.isUserRoleMessage(path, cutIndex) && turnStartIndex != -1 {
		return true
	}
	return false
}

func (ac *AgentCompaction) getMessageSummarize(path []core_types.SessionEntry, firstKeptEntryIndex int, turnStartIndex int, boundaryStart int) []core_types.SessionEntry {
	boundaryEnd := firstKeptEntryIndex
	if ac.isSplitTurn(path, firstKeptEntryIndex, turnStartIndex) {
		boundaryEnd = turnStartIndex
	}
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

func (ac *AgentCompaction) getMessageTurnPrefixSummarize(path []core_types.SessionEntry, firstKeptEntryIndex int, turnStartIndex int) []core_types.SessionEntry {
	var result []core_types.SessionEntry
	if ac.isSplitTurn(path, firstKeptEntryIndex, turnStartIndex) {
		for i := turnStartIndex; i < firstKeptEntryIndex; i++ {
			//跳过types2.EntryTypeCompaction，之后请求大模型压缩时，会重新组装
			if path[i].Kind() == string(core_types.EntryTypeCompaction) {
				continue
			}
			result = append(result, path[i])
		}
	}
	return result
}
