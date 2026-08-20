package editor

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	historyFileName        = "history.jsonl"
	maxHistoryItems        = 100
	maxPastedContentLength = 1024
	pasteCacheDirName      = "paste-cache"
)

// storedPaste 是 history.jsonl 中一条粘贴内容的落盘形态：小内容内联 content，
// 大内容只存 contentHash，正文写到 paste-cache/<hash>.txt
type storedPaste struct {
	ID          int    `json:"id"`
	Content     string `json:"content,omitempty"`
	ContentHash string `json:"contentHash,omitempty"`
}

// Display 保留缓冲原始文本（含粘贴折叠标记 [Pasted text #N ...]），PastedContents 为标记对应的粘贴内容
// （按 id 键控，显式保留序号；缺失的 id 在展开时保留字面量）。Pastes 为旧格式内联数组，读取兼容。逐条 JSON 落盘，追加序（旧→新），
// 读取时逆向得到新→旧。
type historyEntry struct {
	Display        string        `json:"display"`
	Pastes         []string      `json:"pastes,omitempty"` // 旧格式内联数组（读兼容）
	PastedContents []storedPaste `json:"pastedContents,omitempty"`
	Timestamp      int64         `json:"timestamp"`
	Project        string        `json:"project"`
}

// HistoryEntry 是导航恢复用条目：Display 含折叠标记，Pastes 按标记序号（id）键控，
type HistoryEntry struct {
	Display string
	Pastes  map[int]string
}

// clonePastes 拷贝粘贴 map，避免与调用方共享底层 map。
func clonePastes(pastes map[int]string) map[int]string {
	if pastes == nil {
		return nil
	}
	out := make(map[int]string, len(pastes))
	for k, v := range pastes {
		out[k] = v
	}
	return out
}

// HistoryStore 维护提交历史：内存缓存（最新在前）+ jsonl 持久化（~/.tinyclue/history.jsonl）。
// 大粘贴内容（>1024 字符）哈希外置到 ~/.tinyclue/paste-cache/<hash>.txt，避免 jsonl 膨胀。
// 编辑器不感知文件路径与 project：均由交互层注入（测试用 t.TempDir()）。
type HistoryStore struct {
	mu      sync.Mutex
	path    string
	project string         // realpath 归一化后的 cwd，读取时按此过滤，避免跨项目串扰
	cache   []historyEntry // 原始条目（含哈希引用），供写回
	entries []HistoryEntry // 已解析条目（哈希已还原），供导航读取
	loaded  bool
}

// NewHistoryStore 创建历史存储。path 为 history.jsonl 完整路径，project 为归一化 cwd。
func NewHistoryStore(path, project string) *HistoryStore {
	return &HistoryStore{
		path:    path,
		project: canonicalizeProject(project),
	}
}

// Add 提交时追加一条历史（去空白后为空则跳过）。display 保留折叠标记，pastes 按 id 键控对应内容。
// 小内容内联，大内容（>1024 字符）哈希外置到 paste-cache。插入最前，超过上限截断，原子写盘。
func (s *HistoryStore) Add(display string, pastes map[int]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.TrimSpace(display) == "" {
		return
	}
	s.ensureLoadedLocked()
	entry := historyEntry{
		Display:        display,
		PastedContents: s.storePastesLocked(pastes),
		Timestamp:      time.Now().UnixMilli(),
		Project:        s.project,
	}
	s.cache = append([]historyEntry{entry}, s.cache...)
	s.entries = append([]HistoryEntry{{
		Display: display,
		Pastes:  clonePastes(pastes),
	}}, s.entries...)
	if len(s.cache) > maxHistoryItems {
		s.cache = s.cache[:maxHistoryItems]
		s.entries = s.entries[:maxHistoryItems]
	}
	s.writeAllLocked()
}

// List 返回历史条目，最新在前。首次调用读盘并按 project 过滤、还原哈希引用。
func (s *HistoryStore) List() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLoadedLocked()
	out := make([]HistoryEntry, len(s.entries))
	copy(out, s.entries)
	return out
}

// storePastesLocked 把粘贴内容转为落盘形态：≤1024 内联，>1024 哈希外置。
// pastes 按 id 键控，落盘保留显式 id，保证哈希文件缺失时其余内容不错位。
// 哈希文件先于 history.jsonl 写入，避免 jsonl 引用不存在的文件。
func (s *HistoryStore) storePastesLocked(pastes map[int]string) []storedPaste {
	if len(pastes) == 0 {
		return nil
	}
	ids := make([]int, 0, len(pastes))
	for id := range pastes {
		ids = append(ids, id)
	}
	sort.Ints(ids) // 稳定落盘顺序，便于测试与 diff
	out := make([]storedPaste, 0, len(ids))
	for _, id := range ids {
		p := pastes[id]
		sp := storedPaste{ID: id}
		if len(p) <= maxPastedContentLength {
			sp.Content = p
		} else {
			hash := hashPaste(p)
			sp.ContentHash = hash
			s.writePasteFileLocked(hash, p)
		}
		out = append(out, sp)
	}
	return out
}

// resolvePastesLocked 还原一条条目的粘贴内容（按 id 键控）：
// resolveStoredPastedContent 取不到时返回 null 跳过，展开时保留字面量）。
func (s *HistoryStore) resolvePastesLocked(e historyEntry) map[int]string {
	if len(e.PastedContents) == 0 {
		// 旧格式：位置序内联数组 → 按 id（i+1）建 map
		out := make(map[int]string, len(e.Pastes))
		for i, p := range e.Pastes {
			out[i+1] = p
		}
		return out
	}
	out := make(map[int]string, len(e.PastedContents))
	for _, sp := range e.PastedContents {
		if sp.Content != "" {
			out[sp.ID] = sp.Content
			continue
		}
		if sp.ContentHash != "" {
			if c, err := os.ReadFile(s.pasteCachePath(sp.ContentHash)); err == nil {
				out[sp.ID] = string(c)
			}
		}
	}
	return out
}

// ensureLoadedLocked 首次访问时读盘：逐行解析，只保留本 project 条目，逆向为新→旧。
// 文件超上限时裁剪并重写（盘上已积累超过上限的旧条目）。
func (s *HistoryStore) ensureLoadedLocked() {
	if s.loaded {
		return
	}
	s.loaded = true

	f, err := os.Open(s.path)
	if err != nil {
		return // 文件不存在/不可读：空历史
	}
	defer f.Close()

	var entries []historyEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e historyEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // 跳过损坏行
		}
		if e.Project != s.project {
			continue
		}
		entries = append(entries, e)
	}
	_ = scanner.Err() // 文件损坏/截断时用已解析到的条目，历史读取为尽力而为

	// 逆向：文件为旧→新，缓存需新→旧。
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	if len(entries) > maxHistoryItems {
		entries = entries[:maxHistoryItems]
	}
	s.cache = entries
	s.entries = make([]HistoryEntry, len(entries))
	for i, e := range entries {
		s.entries[i] = HistoryEntry{Display: e.Display, Pastes: s.resolvePastesLocked(e)}
	}
}

// writeAllLocked 把缓存整体写回文件：先写同目录临时文件再原子 rename，避免崩溃截断历史文件。
func (s *HistoryStore) writeAllLocked() {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return
	}
	var b strings.Builder
	for i := len(s.cache) - 1; i >= 0; i-- {
		line, err := json.Marshal(s.cache[i])
		if err != nil {
			continue
		}
		b.Write(line)
		b.WriteByte('\n')
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, s.path)
}

// pasteCacheDir 返回粘贴缓存目录：与 history.jsonl 同目录下的 paste-cache。
func (s *HistoryStore) pasteCacheDir() string {
	return filepath.Join(filepath.Dir(s.path), pasteCacheDirName)
}

func (s *HistoryStore) pasteCachePath(hash string) string {
	return filepath.Join(s.pasteCacheDir(), hash+".txt")
}

// writePasteFileLocked 把大粘贴内容写到 paste-cache/<hash>.txt（内容寻址，同内容同文件）。
func (s *HistoryStore) writePasteFileLocked(hash, content string) {
	if err := os.MkdirAll(s.pasteCacheDir(), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(s.pasteCachePath(hash), []byte(content), 0o600)
}

func hashPaste(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])[:16]
}

// canonicalizeProject realpath 归一化 project 键（EvalSymlinks 失败回退原值），
// 与 agent_session.canonicalizeDir 思路一致：保证跨进程/跨重启 project 键稳定可比。
func canonicalizeProject(dir string) string {
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		return real
	}
	return dir
}
