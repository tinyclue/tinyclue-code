package core

import (
	"context"
	"encoding/json"
	"github.com/tinyclue/tinyclue-code/api_provider"
	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/agent_session"
	prompt2 "github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/usercontext"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"github.com/tinyclue/tinyclue-code/config"
	"math"
	"sort"
	"strings"
	"time"
)

type AgentContext struct {
	ctx        context.Context
	runtimeCtx *core_types.AgentRuntimeContext

	agentSession    *agent_session.AgentSession
	agentCompaction *agent_session.AgentCompaction
	agentTool       *AgentTool

	systemMessage apitypes.Message
	messages      []apitypes.Message

	discoveredToolNames []string
	eventSink           core_types.EventSink

	systemPromptFn  func() string
	disallowedTools []string
	staticOverhead  int
}

func NewAgentContext(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentContext {
	agentSession := agent_session.NewAgentSession(ctx, runtimeCtx)
	agentCompaction := agent_session.NewAgentCompaction(ctx, runtimeCtx).WithSession(agentSession)
	return &AgentContext{
		ctx:             ctx,
		runtimeCtx:      runtimeCtx,
		agentSession:    agentSession,
		agentCompaction: agentCompaction,
	}
}

func (ac *AgentContext) WithAgentTool(agentTool *AgentTool) *AgentContext {
	ac.agentTool = agentTool
	return ac
}

func (ac *AgentContext) WithAgentDef(def *core_types.BaseAgentDefinition) *AgentContext {
	ac.systemPromptFn = def.SystemPrompt
	ac.disallowedTools = def.DisallowedTools
	return ac
}

func (ac *AgentContext) WithEventSink(sink core_types.EventSink) *AgentContext {
	ac.eventSink = sink
	return ac
}

func (ac *AgentContext) Init() {
	ac.systemMessage = ac.buildSystemMessage()
	ac.agentSession.Init()
	ac.agentCompaction.Init()
	ac.staticOverhead = ac.ComputeStaticOverhead()
}

func (ac *AgentContext) AppendAgentMessage(entry core_types.AgentMessage) {
	if _, ok := entry.Message.(apitypes.AssistantMessage); ok {
		ac.agentSession.AppendRespMessage(entry.Message, entry.Response.Error)
	} else {
		ac.agentSession.AppendMessage(entry.Message)
	}
}

func (ac *AgentContext) RebuildMessage() {
	var result []apitypes.Message
	ac.systemMessage = ac.buildSystemMessage()
	result = append(result, ac.systemMessage)

	if !ac.runtimeCtx.SubAgent {
		userVirualMessage := ac.buildUserVirualMessage()
		if userVirualMessage != nil {
			result = append(result, userVirualMessage)
		}
	}

	deferredMessage := ac.buildDeferredToolsMessage()
	result = append(result, deferredMessage)

	messages := ac.agentSession.BuildMessageContext()
	if len(messages) > 0 {
		result = append(result, messages...)
	}

	ac.messages = result

	prevDiscoveredToolNames := ac.agentSession.GetPrevDiscoveredToolNames()
	discoveredToolNames := utils.ExtractDiscoveredToolNames(messages)
	discoveredToolNames = append(discoveredToolNames, prevDiscoveredToolNames...)
	ac.discoveredToolNames = utils.DeduplicateStrings(discoveredToolNames)
	ac.agentTool.UpdateDiscoveredTools(ac.discoveredToolNames)
}

// computeStaticOverhead 估算一次请求中不可压缩的静态开销：系统提示 + virtual user +
// deferred tools + 工具 schema。
func (ac *AgentContext) ComputeStaticOverhead() int {
	overhead := utils.EstimateTokens(ac.buildSystemMessage())
	if !ac.runtimeCtx.SubAgent {
		if vm := ac.buildUserVirualMessage(); vm != nil {
			overhead += utils.EstimateTokens(vm)
		}
	}
	overhead += utils.EstimateTokens(ac.buildDeferredToolsMessage())
	if js, err := json.Marshal(ac.agentTool.GetEnableTools()); err == nil {
		overhead += utils.EstimateTextTokens(string(js))
	}
	return overhead
}

func (ac *AgentContext) GetStaticOverhead() int {
	return ac.staticOverhead
}

func (ac *AgentContext) GetMessages() []apitypes.Message {
	return ac.messages
}

func (ac *AgentContext) GetDiscoveredToolNames() []string {
	return ac.discoveredToolNames
}

func (ac *AgentContext) buildSystemMessage() apitypes.Message {
	prompt := prompt2.GetSystemPrompt()
	if ac.systemPromptFn != nil {
		prompt = ac.systemPromptFn()
	}
	return apitypes.SystemMessage{
		Role:      apitypes.SystemRole,
		Text:      prompt,
		CreatedAt: time.Now(),
	}
}

func (ac *AgentContext) buildUserVirualMessage() apitypes.Message {
	cfg := usercontext.GetDefaultConfig()
	cfg.CWD = config.CLI.Cwd
	uc, err := usercontext.GetUserContext(cfg)
	if err != nil {
		return nil
	}
	text := usercontext.PrependUserContext(uc)
	return apitypes.NewUserMessage(text)
}

func (ac *AgentContext) buildDeferredToolsMessage() apitypes.Message {
	disallowed := make(map[string]struct{}, len(ac.disallowedTools))
	for _, name := range ac.disallowedTools {
		disallowed[name] = struct{}{}
	}
	deferredToolNames := ac.agentTool.GetDeferredToolNames()
	sort.Strings(deferredToolNames)
	var filtered []string
	for _, name := range deferredToolNames {
		if _, ok := disallowed[name]; !ok {
			filtered = append(filtered, name)
		}
	}
	text := "<available-deferred-tools>\n" +
		strings.Join(filtered, "\n") +
		"\n</available-deferred-tools>"
	return apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      text,
		CreatedAt: time.Now(),
	}
}

func (ac *AgentContext) CheckUserTokens(userInputMessage apitypes.UserMessage) bool {
	return ac.agentCompaction.CheckUserLimit(userInputMessage)
}

func (ac *AgentContext) CompactIfNeedBefore(userInputMessage apitypes.UserMessage) {
	if !ac.agentCompaction.CheckCompactByUser(userInputMessage) {
		return
	}
	ac.DoCompactV2(core_types.CompactModeDefault)
}

func (ac *AgentContext) NeedCompact() bool {
	return ac.agentCompaction.NeedCompact()
}

func (ac *AgentContext) ShouldCompact(tokens int, contextWindow int) bool {
	return ac.agentCompaction.ShouldCompact(tokens, contextWindow)
}

func (ac *AgentContext) DoCompact() error {
	ac.eventSink.Emit(core_types.CompactionStartEventType, nil, nil)
	prepare := ac.agentCompaction.PrepareCompaction()
	compactContext, err := ac.agentCompaction.Compact(prepare)
	if err == nil {
		ac.agentSession.AppendCompaction(compactContext)
	}
	ac.eventSink.Emit(core_types.CompactionEndEventType, nil, nil)
	return err
}

func (ac *AgentContext) DoCompactV2(mode core_types.CompactMode) error {
	ac.eventSink.Emit(core_types.CompactionStartEventType, nil, nil)
	prepare := ac.agentCompaction.PrepareCompactionV2(mode, ac.GetSessionFilePath(), nil)
	compactContext, err := ac.agentCompaction.CompactV2(prepare)
	if err == nil {
		ac.agentSession.AppendCompaction(compactContext)
	}
	ac.eventSink.Emit(core_types.CompactionEndEventType, nil, nil)
	return err
}

// 遍历 agentSession 中 assistant 消息和子 agent 结果，累加 token 用量和费用。
func (ac *AgentContext) GetSessionUsage() core_types.SessionUsage {
	path := ac.agentSession.GetConversationEntryPath()
	var u core_types.SessionUsage
	for _, entry := range path {
		me, ok := entry.(*core_types.MessageEntry)
		if !ok {
			continue
		}
		if me.Message.RoleName() != apitypes.AssistantRole {
			continue
		}
		msg, ok := me.Message.(apitypes.AssistantMessage)
		if !ok {
			continue
		}
		u.Input += msg.Usage.Input
		u.Output += msg.Usage.Output
		u.CacheRead += msg.Usage.CacheRead
		u.CacheWrite += msg.Usage.CacheWrite
		u.TotalTokens += msg.Usage.TotalTokens
		u.CostTotal += msg.Usage.Cost.Total
	}

	// 累加子 agent 用量（不在 GetConversationEntryPath 中，需单独遍历）
	for _, entry := range ac.agentSession.GetEntrysPath() {
		se, ok := entry.(*core_types.SubAgentEntry)
		if !ok {
			continue
		}
		u.Input += se.Content.SessionUsage.Input
		u.Output += se.Content.SessionUsage.Output
		u.CacheRead += se.Content.SessionUsage.CacheRead
		u.CacheWrite += se.Content.SessionUsage.CacheWrite
		u.TotalTokens += se.Content.SessionUsage.TotalTokens
		u.CostTotal += se.Content.SessionUsage.CostTotal
	}

	u.ContextUsage = ac.agentCompaction.EstimateEntryTokens(path)
	if client := api_provider.GetClient(); client != nil {
		u.ContextWindow = client.Adapter().ContextWindow()
	}
	if u.ContextWindow > 0 {
		pct := float64(u.ContextUsage) / float64(u.ContextWindow) * 100
		u.ContextPercent = math.Round(pct*100) / 100
	}
	return u
}

func (ac *AgentContext) AppendSubAgentEntry(content core_types.SubAgentContent) {
	ac.agentSession.AppendSubAgentEntry(content)
}

func (ac *AgentContext) GetSessionId() string {
	return ac.agentSession.GetSessionId()
}

func (ac *AgentContext) GetSessionFilePath() string {
	return ac.agentSession.GetSessionFile()
}

// GetConversationPath 返回会话的对话路径（MessageEntry + CompactionEntry，chronological）。
// resume 时供 TUI 渲染回放历史。
func (ac *AgentContext) GetConversationPath() []core_types.SessionEntry {
	return ac.agentSession.GetConversationEntryPath()
}

// AppendPlanEntry 把 plan mode 状态写盘（Enter/ExitPlan 时由 PlanManager persistFn 触发）。
func (ac *AgentContext) AppendPlanEntry(ps core_types.PlanState) {
	ac.agentSession.AppendPlanEntry(ps)
}

// GetLastPlanEntry 返回最近一条 plan 状态条目（resume 时重建 PlanState）。
func (ac *AgentContext) GetLastPlanEntry() (*core_types.PlanEntry, bool) {
	return ac.agentSession.GetLastPlanEntry()
}
