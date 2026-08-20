package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHistoryAddAndListOrder 验证 Add/List：最新在前，旧条目按序排列。
func TestHistoryAddAndListOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := NewHistoryStore(path, "proj")
	s.Add("first", nil)
	s.Add("second", nil)
	s.Add("third", nil)

	got := s.List()
	want := []string{"third", "second", "first"}
	if len(got) != len(want) {
		t.Fatalf("List() len=%d want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Display != w {
			t.Errorf("List()[%d]=%q want %q", i, got[i].Display, w)
		}
	}
}

// TestHistoryPersistenceAcrossReload 验证持久化：同一文件重建 store（模拟重启）能读到原条目。
func TestHistoryPersistenceAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	NewHistoryStore(path, "proj").Add("one", nil)
	NewHistoryStore(path, "proj").Add("two", nil)

	s2 := NewHistoryStore(path, "proj")
	got := s2.List()
	if len(got) != 2 || got[0].Display != "two" || got[1].Display != "one" {
		t.Fatalf("reload should read persisted entries newest-first: %v", got)
	}
}

// TestHistoryPastesPersist 验证粘贴折叠标记的保留：Display 原文 + Pastes 内容一起持久化，
// 重建 store 后两者均能还原（导航恢复时仍显示折叠标记）。
func TestHistoryPastesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	NewHistoryStore(path, "proj").Add("raw [Pasted text #1 +3 lines]", map[int]string{1: "a\nb\nc"})

	s2 := NewHistoryStore(path, "proj")
	got := s2.List()
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %v", got)
	}
	if got[0].Display != "raw [Pasted text #1 +3 lines]" {
		t.Errorf("Display should keep collapsed marker, got %q", got[0].Display)
	}
	if len(got[0].Pastes) != 1 || got[0].Pastes[1] != "a\nb\nc" {
		t.Errorf("Pastes should roundtrip by id, got %v", got[0].Pastes)
	}
}

// TestHistoryProjectFilter 验证按 project 过滤：其他 project 的条目不被读取。
func TestHistoryProjectFilter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	NewHistoryStore(path, "projA").Add("a1", nil)
	NewHistoryStore(path, "projA").Add("a2", nil)

	if got := NewHistoryStore(path, "projB").List(); len(got) != 0 {
		t.Fatalf("other project entries should be filtered out: %v", got)
	}
	if got := NewHistoryStore(path, "projA").List(); len(got) != 2 {
		t.Fatalf("own project entries should load: %v", got)
	}
}

// TestHistoryTruncate 验证超过上限截断：只保留最新 maxHistoryItems 条。
func TestHistoryTruncate(t *testing.T) {
	s := NewHistoryStore(filepath.Join(t.TempDir(), "history.jsonl"), "proj")
	for i := 0; i < maxHistoryItems+5; i++ {
		s.Add(fmt.Sprintf("msg-%d", i), nil)
	}
	got := s.List()
	if len(got) != maxHistoryItems {
		t.Fatalf("history should be capped at %d, got %d", maxHistoryItems, len(got))
	}
	if got[0].Display != fmt.Sprintf("msg-%d", maxHistoryItems+4) {
		t.Errorf("newest entry should be kept first: %v", got[:3])
	}
}

// TestHistoryBlankSkipped 验证空白/纯空格提交不入历史。
func TestHistoryBlankSkipped(t *testing.T) {
	s := NewHistoryStore(filepath.Join(t.TempDir(), "history.jsonl"), "proj")
	s.Add("   ", nil)
	s.Add("", nil)
	s.Add("hello", nil)
	if got := s.List(); len(got) != 1 || got[0].Display != "hello" {
		t.Fatalf("blank entries should be skipped: %v", got)
	}
}

// TestHistorySkipsCorruptedLine 验证损坏行与其他 project 行被跳过（尽力而为读取）。
func TestHistorySkipsCorruptedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := "{\"display\":\"good\",\"timestamp\":1,\"project\":\"proj\"}\n" +
		"not-json\n" +
		"{\"display\":\"bad-proj\",\"timestamp\":2,\"project\":\"other\"}\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	s := NewHistoryStore(path, "proj")
	got := s.List()
	if len(got) != 1 || got[0].Display != "good" {
		t.Fatalf("corrupted/other-project lines should be skipped: %v", got)
	}
}

// TestHistorySmallPasteInline 验证 ≤1024 字符的粘贴内容内联在 history.jsonl（含 content 字段）。
func TestHistorySmallPasteInline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	s := NewHistoryStore(path, "proj")
	s.Add("raw [Pasted text #1 +2 lines]", map[int]string{1: "small\npaste"})

	if got := s.List(); len(got) != 1 || got[0].Display != "raw [Pasted text #1 +2 lines]" {
		t.Fatalf("entry should be recorded: %v", got)
	} else if len(got[0].Pastes) != 1 || got[0].Pastes[1] != "small\npaste" {
		t.Fatalf("small paste should be restored inline: %v", got[0].Pastes)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "\"content\":\"small\\npaste\"") {
		t.Errorf("small paste should be persisted inline with content field: %s", data)
	}
}

// TestHistoryLargePasteHashed 验证 >1024 字符的粘贴内容哈希外置：
// history.jsonl 里只存 contentHash，paste-cache/<hash>.txt 生成，重建 store 后仍能还原。
func TestHistoryLargePasteHashed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	big := strings.Repeat("x", maxPastedContentLength+100)
	s := NewHistoryStore(path, "proj")
	s.Add("raw [Pasted text #1 +1 lines]", map[int]string{1: big})

	// history.jsonl 不内联大内容
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), big) {
		t.Errorf("large paste should not be inlined in history.jsonl")
	}
	if !strings.Contains(string(data), "\"contentHash\"") {
		t.Errorf("large paste should persist as contentHash: %s", data)
	}

	// paste-cache 文件存在
	hash := hashPaste(big)
	cacheFile := filepath.Join(filepath.Dir(path), pasteCacheDirName, hash+".txt")
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("paste cache file should exist: %v", err)
	}

	// 重建 store 后还原
	s2 := NewHistoryStore(path, "proj")
	got := s2.List()
	if len(got) != 1 || len(got[0].Pastes) != 1 || got[0].Pastes[1] != big {
		t.Fatalf("large paste should be restored from cache: %v", got)
	}
}

// TestHistoryMissingHashKeepsIDAlignment 验证哈希外置文件缺失时按 id 还原不错位：
// 中间 id（2）缺失，map 仍保留 {1:c1, 3:c3}，而不是把 c3 挤到位置 2（旧 bug）。
func TestHistoryMissingHashKeepsIDAlignment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	big2 := strings.Repeat("b", maxPastedContentLength+100)
	s := NewHistoryStore(path, "proj")
	s.Add(
		"[Pasted text #1 +1 lines][Pasted text #2 +1 lines][Pasted text #3 +1 lines]",
		map[int]string{1: "c1", 2: big2, 3: "c3"},
	)

	// 删除 big2 的哈希缓存文件，模拟缓存丢失
	if err := os.Remove(filepath.Join(dir, pasteCacheDirName, hashPaste(big2)+".txt")); err != nil {
		t.Fatal(err)
	}

	got := NewHistoryStore(path, "proj").List()
	if len(got) != 1 {
		t.Fatalf("want 1 entry, got %d", len(got))
	}
	p := got[0].Pastes
	if p[1] != "c1" || p[3] != "c3" {
		t.Fatalf("ids 1 and 3 should resolve to their own content, got %v", p)
	}
	if _, ok := p[2]; ok {
		t.Errorf("missing hash id 2 should stay absent, got %v", p)
	}
}

// TestHistoryHashDeterministic 验证同内容同哈希：两次 Add 相同大粘贴只生成一个缓存文件。
func TestHistoryHashDeterministic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	big := strings.Repeat("y", maxPastedContentLength+50)
	s := NewHistoryStore(path, "proj")
	s.Add("a", map[int]string{1: big})
	s.Add("b", map[int]string{1: big})

	hash := hashPaste(big)
	cacheFile := filepath.Join(filepath.Dir(path), pasteCacheDirName, hash+".txt")
	if _, err := os.Stat(cacheFile); err != nil {
		t.Fatalf("paste cache file should exist: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(filepath.Dir(path), pasteCacheDirName))
	if len(entries) != 1 {
		t.Errorf("identical pastes should share one cache file, got %d files", len(entries))
	}
}
