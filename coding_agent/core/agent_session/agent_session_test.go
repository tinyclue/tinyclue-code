package agent_session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
)

// newTestSession 构造 AgentSession：HOME 指向临时目录、清空 TINYCLUE_CONFIG_DIR，
// 使 config.TinyClueDir() 落到 tmp/.tinyclue，initDir 建好项目目录（as.sessionDir）。
func newTestSession(t *testing.T) *AgentSession {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("TINYCLUE_CONFIG_DIR", "")
	ctx := context.Background()
	as := NewAgentSession(ctx, &core_types.AgentRuntimeContext{SessionId: "test"})
	as.initDir()
	return as
}

// writeSessionFile 在 dir 写一个仅含 header 的 session 文件，返回路径。
func writeSessionFile(t *testing.T, dir string, sessionId string, seq int) string {
	t.Helper()
	date := time.Now().Format("2006-01-02")
	name := fmt.Sprintf("%s_%04d_%s.jsonl", date, seq, sessionId)
	path := filepath.Join(dir, name)
	header := &core_types.HeaderEntry{
		SessionEntryBase: core_types.SessionEntryBase{
			Type: core_types.EntryTypeSession,
			Id:   sessionId,
		},
		SessionId: sessionId,
		Cwd:       dir,
	}
	data, err := core_types.MarshalSessionEntry(header)
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
	return path
}

// TestConcurrentAppendReadNoRace 并发 AppendMessage/AppendRespMessage 与读取会话条目，
// 验证 entrys slice 的写写、读写无数据竞争（配合 go test -race 生效）。
// file==nil 时 appendToFile 为空操作，无需真实会话文件。
func TestConcurrentAppendReadNoRace(t *testing.T) {
	as := &AgentSession{}
	const n = 50
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			as.AppendMessage(types.UserMessage{Role: types.UserRole, Text: "hi", CreatedAt: time.Now()})
		}()
		go func() {
			defer wg.Done()
			as.AppendRespMessage(types.UserMessage{Role: types.UserRole, Text: "resp", CreatedAt: time.Now()}, types.Error{})
		}()
		wg.Add(2)
		go func() {
			defer wg.Done()
			as.GetConversationEntryPath()
		}()
		go func() {
			defer wg.Done()
			as.GetEntrysPath()
			as.GetLastPlanEntry()
		}()
	}
	wg.Wait()
	if len(as.entrys) != 2*n {
		t.Fatalf("expected %d entries, got %d", 2*n, len(as.entrys))
	}
}

// TestSanitizePath 验证路径→目录名映射、超长截断+hash、确定性。
func TestSanitizePath(t *testing.T) {
	cases := []struct{ in, want string }{
		{"/a b/c", "-a-b-c"},
		{"/Users/foo/my-project", "-Users-foo-my-project"},
		{"plain", "plain"},
		{"/", "-"},
		{"~/.tinyclue/projects", "---tinyclue-projects"},
	}
	for _, c := range cases {
		if got := sanitizePath(c.in); got != c.want {
			t.Errorf("sanitizePath(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// 超长路径：截断 + hash 尾巴，且确定性。
	long := "/" + strings.Repeat("abc", 100) // 301 字符
	got := sanitizePath(long)
	if len(got) > maxSanitizedLength+40 {
		t.Errorf("sanitizePath long result too long: %d", len(got))
	}
	if sanitizePath(long) != got {
		t.Error("sanitizePath long result not deterministic")
	}
	if !strings.HasPrefix(got, "-") || !strings.Contains(got, "-") {
		t.Errorf("sanitizePath long result unexpected: %q", got)
	}
}

// TestInitLatestProjectScoped 验证 InitLatest 优先当前项目目录内最近会话，
// 不选别的项目目录里更新但项目不匹配的文件。
func TestInitLatestProjectScoped(t *testing.T) {
	as := newTestSession(t)
	now := time.Now()

	matching := writeSessionFile(t, as.sessionDir, "sess-local", 1)
	os.Chtimes(matching, now.Add(-2*time.Hour), now.Add(-2*time.Hour))

	otherDir := filepath.Join(filepath.Dir(as.sessionDir), "-some-other-project")
	os.MkdirAll(otherDir, 0755)
	other := writeSessionFile(t, otherDir, "sess-other", 1)
	os.Chtimes(other, now, now)

	as.InitLatest()
	if as.GetSessionId() != "sess-local" {
		t.Fatalf("expected project-scoped session sess-local, got %q", as.GetSessionId())
	}
}

// TestInitLatestEmptyProjectCreates 验证当前项目无会话时直接新建，
// 不回退到其他项目（-c 无参语义就是"续本项目"）。
func TestInitLatestEmptyProjectCreates(t *testing.T) {
	as := newTestSession(t)

	// 别的项目有会话，当前项目没有。
	otherDir := filepath.Join(filepath.Dir(as.sessionDir), "-some-other-project")
	os.MkdirAll(otherDir, 0755)
	writeSessionFile(t, otherDir, "sess-other", 1)

	as.InitLatest()
	if as.GetSessionId() == "" {
		t.Fatal("expected new session when current project has none")
	}
	if as.GetSessionId() == "sess-other" {
		t.Fatal("must not restore another project's session")
	}
}

// TestInitLatestEmptyCreates 验证无任何会话时回退新建会话。
func TestInitLatestEmptyCreates(t *testing.T) {
	as := newTestSession(t)
	as.InitLatest()
	if as.GetSessionId() == "" {
		t.Fatal("expected new session to be created")
	}
	if _, err := os.Stat(as.GetSessionFile()); err != nil {
		t.Fatalf("expected session file to exist: %v", err)
	}
}

// TestLoadSessionBadIdFallsBack 验证 -c <badId> 无匹配时回退新建会话。
func TestLoadSessionBadIdFallsBack(t *testing.T) {
	as := newTestSession(t)
	as.loadSession("no-such-session-id")
	if as.GetSessionId() == "" {
		t.Fatal("expected fallback to new session")
	}
}

// TestLoadSessionAcrossProjects 验证 -c <id> 能跨项目目录按 id 找到会话。
func TestLoadSessionAcrossProjects(t *testing.T) {
	as := newTestSession(t)
	otherDir := filepath.Join(filepath.Dir(as.sessionDir), "-other-project")
	os.MkdirAll(otherDir, 0755)
	writeSessionFile(t, otherDir, "sess-target", 1)

	as.loadSession("sess-target")
	if as.GetSessionId() != "sess-target" {
		t.Fatalf("expected load by id across projects, got %q", as.GetSessionId())
	}
}

// TestSequencePerProjectDir 验证会话编号按项目目录独立，不跨项目继承。
func TestSequencePerProjectDir(t *testing.T) {
	as := newTestSession(t)

	otherDir := filepath.Join(filepath.Dir(as.sessionDir), "-other-project")
	os.MkdirAll(otherDir, 0755)
	writeSessionFile(t, otherDir, "sess-a", 5) // 别的项目已有 seq=5

	as.createSession() // 当前项目首个会话 seq=1
	if seq := as.nextSequence(); seq != 2 {
		t.Fatalf("expected per-project next seq=2, got %d", seq)
	}
}

// TestPlanEntryPersistRestore 验证 AppendPlanEntry 后重读文件能恢复 Active 的 PlanState。
func TestPlanEntryPersistRestore(t *testing.T) {
	as := newTestSession(t)
	as.createSession()

	ps := core_types.PlanState{
		FileDir:          "/tmp",
		Active:           true,
		JustActivated:    true,
		TurnCount:        3,
		AttachmentCount:  1,
		NeedPlanModeExit: false,
		FilePath:         "/tmp/plan.md",
		Slug:             "my-slug",
	}
	as.AppendPlanEntry(ps)

	// 重读文件，验证条目类型与内容完整落盘。
	reloaded := NewAgentSession(context.Background(), &core_types.AgentRuntimeContext{SessionId: "test"})
	reloaded.sessionDir = as.sessionDir
	reloaded.cwd = as.cwd
	reloaded.loadSessionFile(as.GetSessionFile())

	pe, ok := reloaded.GetLastPlanEntry()
	if !ok {
		t.Fatal("expected last plan entry after reload")
	}
	if !pe.PlanState.Active {
		t.Error("expected plan state Active=true")
	}
	if pe.PlanState.Slug != "my-slug" || pe.PlanState.FilePath != "/tmp/plan.md" {
		t.Errorf("unexpected restored plan state: %+v", pe.PlanState)
	}
}

// TestAppendPlanEntryWritesFile 验证 plan 条目 JSON 真实写进 session 文件（项目目录内）。
func TestAppendPlanEntryWritesFile(t *testing.T) {
	as := newTestSession(t)
	as.createSession()

	ps := core_types.PlanState{Active: true, Slug: "written-slug", FilePath: "/p/written.md"}
	as.AppendPlanEntry(ps)

	// 会话文件必须落在当前项目目录（sessionDir）内。
	if dir := filepath.Dir(as.GetSessionFile()); dir != as.sessionDir {
		t.Fatalf("session file not in project dir: got %q, want %q", dir, as.sessionDir)
	}

	data, err := os.ReadFile(as.GetSessionFile())
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, `"type":"plan"`) {
		t.Errorf("session file missing plan entry:\n%s", content)
	}
	if !strings.Contains(content, "written-slug") {
		t.Errorf("session file missing plan slug:\n%s", content)
	}
}
