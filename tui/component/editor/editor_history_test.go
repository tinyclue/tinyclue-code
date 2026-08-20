package editor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"

	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
)

// newTestEditor 构造带临时历史存储的 editor（history 参数为按时间先后添加的条目）。
func newTestEditor(t *testing.T, history []string) *Editor {
	t.Helper()
	e := New()
	store := NewHistoryStore(filepath.Join(t.TempDir(), "history.jsonl"), "proj")
	for _, h := range history {
		store.Add(h, nil)
	}
	e.SetHistoryStore(store)
	return e
}

// press 发送合成按键（参照 mcppanel_test 的写法）。
func press(e *Editor, code rune) {
	e.DoUpdate(core.Data{Msg: core.KeyPressMsg(uv.KeyPressEvent(uv.Key{Code: code}))})
}

// TestHistoryNavigationSingleLine 验证单行输入上下键翻历史：
// 上键：光标在行首（草稿态）直接翻历史，逐条到最旧、到头不动；
// 下键：光标先在行尾才翻历史，逐条回新、回到草稿、到草稿不动。
func TestHistoryNavigationSingleLine(t *testing.T) {
	e := newTestEditor(t, []string{"msg-old", "msg-new"})
	e.SetText("draft") // 光标在行首 (0,0)

	press(e, uv.KeyUp) // 行首上键：直接翻历史，显示最新
	if e.Text() != "msg-new" {
		t.Errorf("after 1st up want %q got %q", "msg-new", e.Text())
	}
	if e.historyIndex != 1 {
		t.Errorf("historyIndex want 1 got %d", e.historyIndex)
	}

	press(e, uv.KeyUp) // 再上：显示更旧
	if e.Text() != "msg-old" {
		t.Errorf("after 2nd up want %q got %q", "msg-old", e.Text())
	}
	if e.historyIndex != 2 {
		t.Errorf("historyIndex want 2 got %d", e.historyIndex)
	}

	press(e, uv.KeyUp) // 已到最旧：不动
	if e.Text() != "msg-old" {
		t.Errorf("at oldest, text should stay %q got %q", "msg-old", e.Text())
	}
	if e.historyIndex != 2 {
		t.Errorf("historyIndex should stay 2 got %d", e.historyIndex)
	}

	press(e, uv.KeyDown) // 条目行首下键：先到行尾
	if e.Text() != "msg-old" {
		t.Errorf("down at entry start should not navigate, got %q", e.Text())
	}
	if e.historyIndex != 2 {
		t.Errorf("historyIndex should stay 2 got %d", e.historyIndex)
	}

	press(e, uv.KeyDown) // 行尾下键：回新
	if e.Text() != "msg-new" {
		t.Errorf("after down want %q got %q", "msg-new", e.Text())
	}
	if e.historyIndex != 1 {
		t.Errorf("historyIndex want 1 got %d", e.historyIndex)
	}

	press(e, uv.KeyDown) // 新条目行尾：回到草稿态，恢复草稿
	if e.Text() != "draft" {
		t.Errorf("draft should be restored want %q got %q", "draft", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("historyIndex want 0 got %d", e.historyIndex)
	}

	press(e, uv.KeyDown) // 草稿行尾：已在草稿态，不动
	if e.Text() != "draft" {
		t.Errorf("at draft, text should stay %q got %q", "draft", e.Text())
	}
}

// TestHistoryUpWithEmptyHistoryNoop 验证无历史时上键 no-op（草稿不动、index 归零）。
func TestHistoryUpWithEmptyHistoryNoop(t *testing.T) {
	e := newTestEditor(t, nil)
	e.SetText("draft")

	press(e, uv.KeyUp)
	if e.Text() != "draft" {
		t.Errorf("empty history: text should stay %q got %q", "draft", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("empty history: historyIndex should stay 0 got %d", e.historyIndex)
	}
}

// TestHistoryMultilineCursorFirst 验证多行输入"先动光标再翻历史"：
// 光标在末行/中间行时上键只移动光标；光标到首行后再上键先归位到行首，再上键才翻历史。
func TestHistoryMultilineCursorFirst(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	e := newTestEditor(t, []string{"msg-old", "msg-new"})
	e.DoBefore(core.Data{}) // 刷新 e.width
	e.SetText("line1\nline2\nline3")
	e.TextBuffer.MoveToEnd() // 光标在最后一行

	// 末行→中间行：只移动光标，不翻历史
	press(e, uv.KeyUp)
	if e.Text() != "line1\nline2\nline3" {
		t.Errorf("up on last line should move cursor not history, got %q", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("historyIndex should stay 0 got %d", e.historyIndex)
	}

	// 中间行→首行：仍只移动光标
	press(e, uv.KeyUp)
	if e.Text() != "line1\nline2\nline3" {
		t.Errorf("up on middle line should move cursor not history, got %q", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("historyIndex should stay 0 got %d", e.historyIndex)
	}

	// 首行（行尾）：上键先归位到行首，不翻历史
	press(e, uv.KeyUp)
	if e.Text() != "line1\nline2\nline3" {
		t.Errorf("up at first line end should move to line start, got %q", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("historyIndex should stay 0 got %d", e.historyIndex)
	}

	// 首行行首再上：光标动不了 → 翻历史
	press(e, uv.KeyUp)
	if e.Text() != "msg-new" {
		t.Errorf("up at first line start should navigate history, got %q", e.Text())
	}
	if e.historyIndex != 1 {
		t.Errorf("historyIndex want 1 got %d", e.historyIndex)
	}
}

// TestDoOnSubmitRecordsHistory 验证提交路径写入历史并复位输入与导航态。
func TestDoOnSubmitRecordsHistory(t *testing.T) {
	e := newTestEditor(t, nil)
	submitted := ""
	e.SetOnSubmit(func(text string) { submitted = text })

	e.DoOnSubmit("hello")

	if submitted != "hello" {
		t.Errorf("submitted want %q got %q", "hello", submitted)
	}
	if got := e.history.List(); len(got) != 1 || got[0].Display != "hello" {
		t.Fatalf("history should record submitted text: %v", got)
	}
	if e.Text() != "" {
		t.Errorf("input should be cleared after submit, got %q", e.Text())
	}
	if e.historyIndex != 0 {
		t.Errorf("historyIndex should reset to 0 after submit, got %d", e.historyIndex)
	}
}

// TestDoOnSubmitSkipsBlank 验证空白提交不入历史、不触发回调。
func TestDoOnSubmitSkipsBlank(t *testing.T) {
	e := newTestEditor(t, nil)
	submitted := false
	e.SetOnSubmit(func(text string) { submitted = true })

	e.DoOnSubmit("   ")

	if submitted {
		t.Error("blank submit should not invoke onSubmit")
	}
	if len(e.history.List()) != 0 {
		t.Error("blank submit should not record history")
	}
}

// TestHistoryDraftExpandsPaste 验证翻历史后折叠标记仍在：草稿连同缓冲原文（含折叠标记）
// 与粘贴内容一起保存/恢复，回草稿后缓冲仍显示折叠标记、Text() 仍能正确展开为实际内容。
func TestHistoryDraftExpandsPaste(t *testing.T) {
	e := newTestEditor(t, []string{"msg"})
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&sb, "line-%d\n", i)
	}
	content := sb.String()
	e.DoUpdate(core.Data{Msg: core.PasteMsg(uv.PasteEvent{Content: content})})

	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1") {
		t.Fatalf("expected collapsed paste marker, got %q", e.TextBuffer.Text())
	}

	press(e, uv.KeyUp)   // 首次上键：光标在行尾 → 先归位到行首
	press(e, uv.KeyUp)   // 再上：进入历史：草稿保存原文 + 粘贴内容
	press(e, uv.KeyDown) // 首次下键：条目行首 → 先到行尾
	press(e, uv.KeyDown) // 再下：回草稿
	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1") {
		t.Errorf("draft should keep collapsed marker, got %q", e.TextBuffer.Text())
	}
	if e.Text() != content {
		t.Errorf("draft should still expand to content, got %q", e.Text())
	}
}

// TestHistoryNavigationKeepsPasteMarker 验证历史条目里的折叠标记在导航恢复后仍显示为折叠标记：
// 提交含折叠标记的内容入历史 → 清空 → 上键翻回，缓冲应显示标记而非展开的正文。
func TestHistoryNavigationKeepsPasteMarker(t *testing.T) {
	e := newTestEditor(t, nil)
	var sb strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&sb, "pasted-line-%d\n", i)
	}
	content := sb.String()
	e.DoUpdate(core.Data{Msg: core.PasteMsg(uv.PasteEvent{Content: content})})

	raw := e.TextBuffer.Text()
	if !strings.Contains(raw, "[Pasted text #1") {
		t.Fatalf("expected collapsed paste marker, got %q", raw)
	}

	e.SetOnSubmit(func(string) {})
	e.DoOnSubmit(e.Text()) // 提交：raw+marker 连同 pastes 入历史
	if e.Text() != "" {
		t.Fatalf("input should be cleared after submit, got %q", e.Text())
	}

	press(e, uv.KeyUp) // 上键翻回该历史
	if !strings.Contains(e.TextBuffer.Text(), "[Pasted text #1") {
		t.Errorf("history entry should show collapsed marker, got %q", e.TextBuffer.Text())
	}
	if e.Text() != content {
		t.Errorf("history entry should expand to content, got %q", e.Text())
	}
}

// TestHistoryMissingPasteStaysLiteral 验证历史条目中哈希外置文件缺失时，
func TestHistoryMissingPasteStaysLiteral(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "history.jsonl")
	big2 := strings.Repeat("b", maxPastedContentLength+100)
	s := NewHistoryStore(path, "proj")
	s.Add(
		"[Pasted text #1 +1 lines][Pasted text #2 +1 lines][Pasted text #3 +1 lines]",
		map[int]string{1: "c1", 2: big2, 3: "c3"},
	)
	if err := os.Remove(filepath.Join(dir, pasteCacheDirName, hashPaste(big2)+".txt")); err != nil {
		t.Fatal(err)
	}

	e := New()
	e.SetHistoryStore(NewHistoryStore(path, "proj"))
	press(e, uv.KeyUp) // 空草稿行首 → 进入历史最新条目

	want := "c1[Pasted text #2 +1 lines]c3"
	if e.Text() != want {
		t.Fatalf("missing hash should keep marker literal, got %q want %q", e.Text(), want)
	}
	if e.TextBuffer.Text() != "[Pasted text #1 +1 lines][Pasted text #2 +1 lines][Pasted text #3 +1 lines]" {
		t.Errorf("display should keep all markers, got %q", e.TextBuffer.Text())
	}
}

// TestSetTextResetsHistory 验证 SetText 复位导航态（closePanel/中断清空等外部清空场景）。
func TestSetTextResetsHistory(t *testing.T) {
	e := newTestEditor(t, []string{"msg"})
	e.SetText("draft")

	press(e, uv.KeyUp) // 进入导航态
	if e.historyIndex != 1 {
		t.Fatalf("expected historyIndex 1 after up, got %d", e.historyIndex)
	}

	e.SetText("") // 外部清空 → 复位
	if e.historyIndex != 0 {
		t.Errorf("SetText should reset historyIndex, got %d", e.historyIndex)
	}
	if e.historyDraft.display != "" || len(e.historyDraft.pastes) != 0 {
		t.Errorf("SetText should reset draft, got %+v", e.historyDraft)
	}
}

// TestHistoryBoundarySingleLine 验证单行草稿先归位再翻历史：
// 行首下键先到行尾（不翻，草稿为最新下键无更可翻）；行尾上键先到行首（不翻）；
// 行首上键才翻历史；历史条目行首下键先到行尾（不翻），行尾下键才回草稿。
func TestHistoryBoundarySingleLine(t *testing.T) {
	e := newTestEditor(t, []string{"h"})
	e.SetText("abc") // 光标在行首 (0,0)

	// 行首下键 → 行尾，不翻历史
	press(e, uv.KeyDown)
	if e.Text() != "abc" || e.historyIndex != 0 {
		t.Fatalf("down at line start should not navigate, got %q index=%d", e.Text(), e.historyIndex)
	}
	if e.TextBuffer.CursorRow != 0 || e.TextBuffer.CursorCol != 3 {
		t.Errorf("cursor should be at line end (col 3), got row=%d col=%d", e.TextBuffer.CursorRow, e.TextBuffer.CursorCol)
	}

	// 行尾下键：草稿为最新，无更可翻（不动）
	press(e, uv.KeyDown)
	if e.Text() != "abc" || e.historyIndex != 0 {
		t.Errorf("down at draft line end should stay, got %q index=%d", e.Text(), e.historyIndex)
	}

	// 行尾上键 → 行首，不翻历史
	press(e, uv.KeyUp)
	if e.Text() != "abc" || e.historyIndex != 0 {
		t.Fatalf("up at line end should not navigate, got %q index=%d", e.Text(), e.historyIndex)
	}
	if e.TextBuffer.CursorCol != 0 {
		t.Errorf("cursor should be at line start, got col=%d", e.TextBuffer.CursorCol)
	}

	// 行首上键 → 翻历史，进入最新条目
	press(e, uv.KeyUp)
	if e.historyIndex != 1 || e.Text() != "h" {
		t.Fatalf("up at line start should flip history, got %q index=%d", e.Text(), e.historyIndex)
	}

	// 历史条目 "h" 行首下键 → 行尾，不翻
	press(e, uv.KeyDown)
	if e.Text() != "h" || e.historyIndex != 1 {
		t.Fatalf("down at entry start should not navigate, got %q index=%d", e.Text(), e.historyIndex)
	}
	if e.TextBuffer.CursorCol != 1 {
		t.Errorf("cursor should be at entry end, got col=%d", e.TextBuffer.CursorCol)
	}

	// 行尾下键 → 回草稿
	press(e, uv.KeyDown)
	if e.Text() != "abc" || e.historyIndex != 0 {
		t.Errorf("down at entry end should return to draft, got %q index=%d", e.Text(), e.historyIndex)
	}
}

// TestHistoryBoundaryMultiline 验证多行草稿下键逐行下降：先到对应下一行，
// 再到末行行尾，才到可翻历史位置（草稿为最新时下键停在行尾不动）。
func TestHistoryBoundaryMultiline(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(60)
	e := newTestEditor(t, []string{"h"})
	e.DoBefore(core.Data{})
	e.SetText("l1\nl2") // 光标在行首 (0,0)

	press(e, uv.KeyDown) // → (1,0) 对应下一行
	if e.Text() != "l1\nl2" || e.historyIndex != 0 {
		t.Fatalf("down should move to next line, got %q index=%d", e.Text(), e.historyIndex)
	}
	if e.TextBuffer.CursorRow != 1 || e.TextBuffer.CursorCol != 0 {
		t.Errorf("cursor should be at (1,0), got (%d,%d)", e.TextBuffer.CursorRow, e.TextBuffer.CursorCol)
	}

	press(e, uv.KeyDown) // → (1,2) 末行行尾
	if e.historyIndex != 0 {
		t.Fatalf("down at last line start should move to line end, got index=%d", e.historyIndex)
	}
	if e.TextBuffer.CursorRow != 1 || e.TextBuffer.CursorCol != 2 {
		t.Errorf("cursor should be at (1,2), got (%d,%d)", e.TextBuffer.CursorRow, e.TextBuffer.CursorCol)
	}

	press(e, uv.KeyDown) // 末行行尾：草稿为最新，无更可翻（不动）
	if e.Text() != "l1\nl2" || e.historyIndex != 0 {
		t.Errorf("down at draft line end should stay, got %q index=%d", e.Text(), e.historyIndex)
	}
}
