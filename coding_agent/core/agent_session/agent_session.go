package agent_session

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
	"hash/fnv"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type AgentSession struct {
	ctx         context.Context
	runtimeCtx  *core_types.AgentRuntimeContext
	sessionId   string
	sessionFile string       // jsonl file path
	file        *sessionFile // file handle for appending
	projectsDir string       // <base>/projects/，所有项目目录的根
	sessionDir  string       // <base>/projects/<sanitized-cwd>/{,sub/}，当前项目的会话目录
	cwd         string       // working dir（已 canonicalize）
	entrys      []core_types.SessionEntry
	mu          sync.Mutex // 保护 entrys slice
	fileMu      sync.Mutex // 保护 file 写入（可与 mu 分别获取）
}

func NewAgentSession(ctx context.Context, runtimeCtx *core_types.AgentRuntimeContext) *AgentSession {
	return &AgentSession{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
	}
}

func (as *AgentSession) buildMessageEntry(message types.Message) *core_types.MessageEntry {
	id := as.generateId()
	parentId := as.getCurrentEntryId()
	return &core_types.MessageEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type:      core_types.EntryTypeMessage,
			Id:        id,
			ParentId:  parentId,
			Timestamp: time.Now(),
		},
		Message: message,
	}
}

func (as *AgentSession) buildCompactionEntry(compactionContext *core_types.CompactionContext) *core_types.CompactionEntry {
	id := as.generateId()
	parentId := as.getCurrentEntryId()
	return &core_types.CompactionEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type:      core_types.EntryTypeCompaction,
			Id:        id,
			ParentId:  parentId,
			Timestamp: time.Now(),
		},
		Summary:          compactionContext.Summary,
		FirstKeptEntryId: compactionContext.FirstKeptEntryId,
		Tokens:           compactionContext.Tokens,
		ReadFiles:        compactionContext.FileLists.ReadFiles,
		ModifyFiles:      compactionContext.FileLists.ModifiedFiles,
		DiscoveredTools:  compactionContext.DiscoveredTools,
		Mode:             int(compactionContext.Mode),
	}
}

func (as *AgentSession) AppendMessage(message types.Message) *core_types.MessageEntry {
	entry := as.buildMessageEntry(message)
	as.entrys = append(as.entrys, entry)
	as.appendToFile(entry)
	return entry
}

func (as *AgentSession) AppendRespMessage(message types.Message, error types.Error) *core_types.MessageEntry {
	entry := as.buildMessageEntry(message)
	entry.Error = error
	as.entrys = append(as.entrys, entry)
	as.appendToFile(entry)
	return entry
}

func (as *AgentSession) getCurrentEntryId() string {
	parentId := ""
	if len(as.entrys) > 0 {
		parentId = as.entrys[len(as.entrys)-1].GetId()
	}
	return parentId
}

func (as *AgentSession) AppendCompaction(compactionContext *core_types.CompactionContext) {
	entry := as.buildCompactionEntry(compactionContext)
	as.entrys = append(as.entrys, entry)
	as.appendToFile(entry)
}

func (as *AgentSession) AppendSubAgentEntry(content core_types.SubAgentContent) {
	as.mu.Lock()
	id := as.generateId()
	parentId := as.getCurrentEntryId()
	entry := &core_types.SubAgentEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type:      core_types.EntryTypeSubAgent,
			Id:        id,
			ParentId:  parentId,
			Timestamp: time.Now(),
		},
		Content: content,
	}
	as.entrys = append(as.entrys, entry)
	as.mu.Unlock()

	as.appendToFile(entry)
}

// AppendPlanEntry 追加一条 plan mode 状态条目（进入/退出时写盘，供会话恢复）。
func (as *AgentSession) AppendPlanEntry(ps core_types.PlanState) {
	as.mu.Lock()
	id := as.generateId()
	parentId := as.getCurrentEntryId()
	entry := &core_types.PlanEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type:      core_types.EntryTypePlan,
			Id:        id,
			ParentId:  parentId,
			Timestamp: time.Now(),
		},
		PlanState: ps,
	}
	as.entrys = append(as.entrys, entry)
	as.mu.Unlock()

	as.appendToFile(entry)
}

// GetLastPlanEntry 返回最近一条 plan 状态条目（从末尾扫，resume 时重建 PlanState）。
func (as *AgentSession) GetLastPlanEntry() (*core_types.PlanEntry, bool) {
	as.mu.Lock()
	defer as.mu.Unlock()
	for i := len(as.entrys) - 1; i >= 0; i-- {
		if pe, ok := as.entrys[i].(*core_types.PlanEntry); ok {
			return pe, true
		}
	}
	return nil, false
}

func (as *AgentSession) GetEntrysPath() []core_types.SessionEntry {
	as.mu.Lock()
	defer as.mu.Unlock()
	return as.buildEntryPath(as.entrys)
}

func (as *AgentSession) createSessionId() string {
	id, _ := uuid.NewV7()
	return id.String()
}

func (as *AgentSession) generateId() string {
	return uuid.New().String()[:8]
}

// GetConversationEntryPath 正向遍历全部会话条目，返回 MessageEntry + CompactionEntry 列表。
func (as *AgentSession) GetConversationEntryPath() []core_types.SessionEntry {
	path := as.buildEntryPath(as.entrys)
	var result []core_types.SessionEntry
	for _, e := range path {
		switch e.(type) {
		case *core_types.MessageEntry, *core_types.CompactionEntry:
		default:
			continue
		}
		if me, ok := e.(*core_types.MessageEntry); ok {
			if !as.ShouldIncludeMessageEntry(me) {
				continue
			}
		}
		result = append(result, e)
	}
	return result
}

func (as *AgentSession) GetPrevDiscoveredToolNames() []string {
	path := as.GetConversationEntryPath()
	compaction, _ := as.GetLastCompactionEntry(path)
	if compaction != nil {
		return compaction.DiscoveredTools
	} else {
		return make([]string, 0)
	}
}

func (as *AgentSession) BuildMessageContext() []types.Message {
	path := as.GetConversationEntryPath()
	compaction, compactionIndex := as.GetLastCompactionEntry(path)

	var result []types.Message

	if compaction != nil {
		// ---- v1 ----
		// Wrap compaction summary as a user message at the front
		//summaryText := prompt.COMPACTION_SUMMARY_PREFIX + compaction.Summary + prompt.COMPACTION_SUMMARY_SUFFIX
		//result = append(result, types.NewUserMessage(summaryText))
		// ---- v2 ----
		result = append(result, types.NewUserMessage(compaction.Summary))
		// Append messages kept after compaction point
		for _, entry := range as.GetEntriesAfterCompaction(path, compaction, compactionIndex) {
			if me, ok := entry.(*core_types.MessageEntry); ok {
				result = append(result, me.Message)
			}
		}
	} else {
		// No compaction: extract all messages from path
		for _, entry := range path {
			if me, ok := entry.(*core_types.MessageEntry); ok {
				result = append(result, me.Message)
			}
		}
	}

	return result
}

// ShouldIncludeMessageEntry 判断 MessageEntry 是否应加入模型上下文。
// 只对 assistant 角色的消息做过滤：aborted 和可重试 error 不加入上下文。
func (as *AgentSession) ShouldIncludeMessageEntry(entry *core_types.MessageEntry) bool {
	// 只过滤 assistant 消息
	if entry.Message.RoleName() != types.AssistantRole {
		return true
	}
	msg, ok := entry.Message.(types.AssistantMessage)
	if !ok {
		return true
	}
	finishReason := msg.StopReason.FinishReason
	if finishReason == types.FinishReasonError || finishReason == types.FinishReasonAborted {
		return false
	}
	return true // stop / length / tool_use
}

// GetLastCompactionEntry extracts the most recent CompactionEntry from a
// GetConversationEntryPath result (chronological order, root → leaf).
func (as *AgentSession) GetLastCompactionEntry(path []core_types.SessionEntry) (*core_types.CompactionEntry, int) {
	for i := len(path) - 1; i >= 0; i-- {
		if e, ok := path[i].(*core_types.CompactionEntry); ok {
			return e, i
		}
	}
	return nil, -1
}

// buildEntryPath rebuilds the entry path from the last entry in the given slice
// back to the root by following parentId chain. The result is in chronological
// order (root first, leaf last).
func (as *AgentSession) buildEntryPath(entries []core_types.SessionEntry) []core_types.SessionEntry {
	if len(entries) == 0 {
		return nil
	}

	byID := make(map[string]core_types.SessionEntry, len(entries))
	for _, e := range entries {
		byID[e.GetId()] = e
	}

	// Walk backward from leaf to root (build in reverse order)
	var reversed []core_types.SessionEntry
	current := entries[len(entries)-1]
	for {
		reversed = append(reversed, current)
		parentID := current.GetParentId()
		if parentID == "" {
			break
		}
		next, ok := byID[parentID]
		if !ok {
			break
		}
		current = next
	}

	// Reverse to get chronological order (root → leaf)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	return reversed
}

// GetEntriesAfterCompaction returns all entries from the FirstKeptEntryId position
// to the end of the path (inclusive). Returns nil if the compaction entry or its
// FirstKeptEntryId is not found in the path.
func (as *AgentSession) GetEntriesAfterCompaction(path []core_types.SessionEntry, compaction *core_types.CompactionEntry, compactionIndex int) []core_types.SessionEntry {
	if compaction == nil || compaction.FirstKeptEntryId == "" {
		return nil
	}
	boundaryStart := 0
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
	return path[boundaryStart:]
}

func (as *AgentSession) GetSessionId() string {
	return as.sessionId
}

// sessionFile wraps a JSONL file handle for append operations.
type sessionFile struct {
	path string
	f    *os.File
}

func newSessionFile(path string) (*sessionFile, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return &sessionFile{path: path, f: f}, nil
}

func (sf *sessionFile) appendEntry(entry core_types.SessionEntry) error {
	data, err := core_types.MarshalSessionEntry(entry)
	if err != nil {
		return err
	}
	if _, err := sf.f.Write(data); err != nil {
		return err
	}
	_, err = sf.f.WriteString("\n")
	return err
}

func (sf *sessionFile) close() {
	if sf.f != nil {
		sf.f.Close()
	}
}

// ---------------------------------------------------------------------------
// Init — entry point
// ---------------------------------------------------------------------------

// Init 初始化会话，根据命令行 -c 参数决定行为：
//  1. 无 -c 参数 → 创建新会话
//  2. [-c] 无 sessionId → 恢复最近一条会话
//  3. [-c sessionId] → 按 sessionId 恢复
func (as *AgentSession) Init() {
	if !as.runtimeCtx.SubAgent {
		as.initDir()
		sessionID, hasSession := config.CLI.Get(config.FlagSession)
		switch {
		case !hasSession:
			as.createSession()
		case sessionID == "":
			as.InitLatest()
		default:
			as.loadSession(sessionID)
		}
	} else {
		as.initSubDir()
		as.createSession()
	}
}

// initDir 解析会话存储目录：<base>/projects/<sanitized-cwd>
// cwd 先 canonicalize 再 sanitize，保证 symlink 路径（macOS /tmp→/private/tmp）映射到同一目录。
func (as *AgentSession) initDir() {
	// config.TinyClueDir 已兜底：$TINYCLUE_CONFIG_DIR 或 ~/.tinyclue。
	base, _ := config.TinyClueDir()
	as.projectsDir = filepath.Join(base, "projects")
	// 用启动时捕获的 config.CLI.Cwd（与欢迎页一致），不重复调用 os.Getwd。
	as.cwd = canonicalizeDir(config.CLI.Cwd)
	as.sessionDir = filepath.Join(as.projectsDir, sanitizePath(as.cwd))
	os.MkdirAll(as.sessionDir, 0755)
}

// initSubDir 子 agent 会话归档目录：当前项目目录下加 sub/ 子目录（只归档，不参与恢复）。
func (as *AgentSession) initSubDir() {
	as.initDir()
	as.sessionDir = filepath.Join(as.sessionDir, "sub")
	os.MkdirAll(as.sessionDir, 0755)
}

// （如 /a b/c → -a-b-c），得到确定性的项目目录名。
var pathSanitizer = regexp.MustCompile(`[^a-zA-Z0-9]`)

// maxSanitizedLength 超长路径截断阈值
const maxSanitizedLength = 200

// sanitizePath 把任意路径映射成项目目录名：非字母数字逐字符替换为 '-'；
// 结果超长（>maxSanitizedLength）截断并拼 fnv64a hash 尾巴，保证确定性。
func sanitizePath(s string) string {
	out := pathSanitizer.ReplaceAllString(s, "-")
	if len(out) <= maxSanitizedLength {
		return out
	}
	h := fnv.New64a()
	h.Write([]byte(s))
	return out[:maxSanitizedLength] + "-" + strconv.FormatUint(h.Sum64(), 16)
}

// canonicalizeDir realpath 归一化（EvalSymlinks），失败回退原值。
// 避免 symlink 进入同一目录却产生两个项目目录（如 macOS /tmp 与 /private/tmp）。
func canonicalizeDir(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved
	}
	return dir
}

// InitLatest 恢复会话：恢复当前项目目录（projects/<sanitized-cwd>）内最近会话；
// 本项目没有会话则直接创建新会话（不跨项目回退，-c 无参语义就是"续本项目"）。
func (as *AgentSession) InitLatest() {
	newestFile := as.findLatestSessionFile()
	if newestFile == "" {
		as.createSession()
		return
	}
	as.loadSessionFile(newestFile)
}

// findLatestSessionFile 返回当前项目目录内最近修改的 .jsonl（mtime 降序）。
// 目录结构已按项目隔离，无需再读 header 过滤。
func (as *AgentSession) findLatestSessionFile() string {
	entries, err := os.ReadDir(as.sessionDir)
	if err != nil {
		return ""
	}

	var newestFile string
	var newestMod time.Time
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mod := info.ModTime()
		if newestFile == "" || mod.After(newestMod) {
			newestFile = filepath.Join(as.sessionDir, e.Name())
			newestMod = mod
		}
	}
	return newestFile
}

// ---------------------------------------------------------------------------
// Create / Load / Append
// ---------------------------------------------------------------------------

func (as *AgentSession) createSession() {
	as.sessionId = as.createSessionId()
	date := time.Now().Format("2006-01-02")
	seq := as.nextSequence()
	as.sessionFile = filepath.Join(as.sessionDir, fmt.Sprintf("%s_%04d_%s.jsonl", date, seq, as.sessionId))

	f, _ := os.Create(as.sessionFile)
	defer f.Close()

	id := as.generateId()
	headerEntry := &core_types.HeaderEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type:      core_types.EntryTypeSession,
			Id:        id,
			ParentId:  "",
			Timestamp: time.Now(),
		},
		SessionId: as.sessionId,
		Cwd:       as.cwd,
	}
	as.entrys = append(as.entrys, headerEntry)

	data, _ := core_types.MarshalSessionEntry(headerEntry)
	f.Write(data)
	f.WriteString("\n")

	as.file, _ = newSessionFile(as.sessionFile)
}

// nextSequence scans today's session files and returns the next available
// sequence number. The file format is {date}_{seq:04d}_{sessionId}.jsonl.
func (as *AgentSession) nextSequence() int {
	date := time.Now().Format("2006-01-02")
	pattern := filepath.Join(as.sessionDir, date+"_*.jsonl")
	matches, _ := filepath.Glob(pattern)
	maxSeq := 0
	prefix := date + "_"
	for _, m := range matches {
		base := filepath.Base(m)
		if !strings.HasPrefix(base, prefix) {
			continue
		}
		rest := strings.TrimPrefix(base, prefix)
		parts := strings.SplitN(rest, "_", 2)
		if len(parts) != 2 {
			continue
		}
		seq, err := strconv.Atoi(parts[0])
		if err != nil {
			continue
		}
		if seq > maxSeq {
			maxSeq = seq
		}
	}
	return maxSeq + 1
}

// loadSession 按 sessionId 恢复会话：sessionId 全局唯一，跨所有项目目录查找
// （projects/*/*_{id}.jsonl，glob 不穿透 sub/ 子目录，子 agent 会话不参与）。
// 无匹配（-c <badId>）时回退创建新会话，避免留下空 session（无 header/无文件句柄）。
func (as *AgentSession) loadSession(sessionId string) {
	pattern := filepath.Join(as.projectsDir, "*", fmt.Sprintf("*_%s.jsonl", sessionId))
	matches, _ := filepath.Glob(pattern)
	if len(matches) == 0 {
		as.createSession()
		return
	}
	as.loadSessionFile(matches[0])
}

// loadSessionFile reads a session file by path, parses all entries, and
// opens the file handle for future appends.
func (as *AgentSession) loadSessionFile(filePath string) {
	as.sessionFile = filePath

	data, _ := os.ReadFile(filePath)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		entry, err := core_types.UnmarshalSessionEntry([]byte(line))
		if err == nil {
			as.entrys = append(as.entrys, entry)
		}
	}

	if len(as.entrys) > 0 {
		if h, ok := as.entrys[0].(*core_types.HeaderEntry); ok {
			as.sessionId = h.SessionId
		}
	}

	as.file, _ = newSessionFile(filePath)
}

// appendToFile appends a single entry to the JSONL file.
// Thread-safe: serialized via fileMu.
func (as *AgentSession) appendToFile(entry core_types.SessionEntry) {
	as.fileMu.Lock()
	defer as.fileMu.Unlock()
	if as.file == nil {
		return
	}
	as.file.appendEntry(entry)
}

// Close closes the session file handle. Call when the session is no longer needed.
func (as *AgentSession) Close() {
	if as.file != nil {
		as.file.close()
		as.file = nil
	}
}

func (as *AgentSession) GetSessionFile() string {
	return as.sessionFile
}
