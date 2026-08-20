package core

import (
	"context"
	"strings"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/tui"
	"github.com/tinyclue/tinyclue-code/tui/component/markdown"
	textcomp "github.com/tinyclue/tinyclue-code/tui/component/text"
	tuicore "github.com/tinyclue/tinyclue-code/tui/core"
)

// collectTexts 递归收集组件树里的文本（textcomp.TextComponent / markdown）。
func collectTexts(comps []tuicore.Component) []string {
	var out []string
	for _, c := range comps {
		out = append(out, collectText(c)...)
	}
	return out
}

func collectText(c tuicore.Component) []string {
	var out []string
	if tc, ok := c.(*textcomp.TextComponent); ok {
		if s := tc.Text(); s != "" {
			out = append(out, s)
		}
	}
	if md, ok := c.(*markdown.MarkdownNewComponent); ok {
		if s := md.Text(); s != "" {
			out = append(out, s)
		}
	}
	for _, ch := range c.Children() {
		out = append(out, collectText(ch)...)
	}
	return out
}

// replayModel 构建带 chat container 的 ModelV2 + TuiEvent（镜像 interactive.go 的容器创建）。
func replayModel(t *testing.T) (*TuiEvent, *tuicore.Container) {
	t.Helper()
	ctx := context.Background()
	model := tui.NewV2(ctx)
	chat := tuicore.NewContainer(tuicore.ChatContainerType)
	model.AddContainer(chat)
	te := NewTuiEvent(ctx).WithTui(model)
	return te, chat
}

// TestReplaySessionRendersHistory 验证 resume 重放：user 块 / assistant markdown /
// 工具组件（含结果）/ 压缩分隔 / resumed 提示 都出现在 chat 容器。
func TestReplaySessionRendersHistory(t *testing.T) {
	te, chat := replayModel(t)

	entries := []core_types.SessionEntry{
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e1"},
			Message:          apitypes.UserMessage{Role: apitypes.UserRole, Text: "hello tinyclue"},
		},
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e2"},
			Message: apitypes.AssistantMessage{
				Role:        apitypes.AssistantRole,
				TextContent: apitypes.TextContent{Text: "thinking answer"},
				ToolCalls:   []apitypes.ToolCall{{ID: "tc1", Name: "Bash", Arguments: map[string]any{"command": "echo 42"}}},
			},
		},
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e3"},
			Message: apitypes.ToolResultMessage{
				Role:       apitypes.ToolRole,
				ToolCallId: "tc1",
				ToolName:   "Bash",
				Contents:   []apitypes.ContentBlock{{Type: "text", Text: "42"}},
			},
		},
		&core_types.CompactionEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeCompaction, Id: "e4"},
			Tokens:           1234,
		},
	}

	te.ReplaySession(entries)

	texts := collectTexts(chat.Children())
	joined := strings.Join(texts, "\n")
	for _, want := range []string{"Resumed session", "hello tinyclue", "thinking answer", "42", "context compressed"} {
		if !strings.Contains(joined, want) {
			t.Errorf("replay output missing %q; got:\n%s", want, joined)
		}
	}
}

// TestReplaySessionSkipsMetaUser 验证 plan 提示等 IsMeta 用户消息不渲染为 chat 块。
func TestReplaySessionSkipsMetaUser(t *testing.T) {
	te, chat := replayModel(t)

	entries := []core_types.SessionEntry{
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e1"},
			Message:          apitypes.NewUserMetaMessage("plan-mode instructions"),
		},
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e2"},
			Message:          apitypes.UserMessage{Role: apitypes.UserRole, Text: "real prompt"},
		},
	}

	te.ReplaySession(entries)

	texts := collectTexts(chat.Children())
	joined := strings.Join(texts, "\n")
	if strings.Contains(joined, "plan-mode instructions") {
		t.Errorf("meta user message should not be rendered, got:\n%s", joined)
	}
	if !strings.Contains(joined, "real prompt") {
		t.Errorf("real user prompt missing, got:\n%s", joined)
	}
}

// TestReplayUnmatchedToolResultNoPanic 验证工具结果没有对应 ToolCall（跨压缩窗口等）时不 panic。
func TestReplayUnmatchedToolResultNoPanic(t *testing.T) {
	te, chat := replayModel(t)

	entries := []core_types.SessionEntry{
		&core_types.MessageEntry{
			SessionEntryBase: core_types.SessionEntryBase{Type: core_types.EntryTypeMessage, Id: "e1"},
			Message: apitypes.ToolResultMessage{
				Role:       apitypes.ToolRole,
				ToolCallId: "orphan",
				ToolName:   "Read",
				Contents:   []apitypes.ContentBlock{{Type: "text", Text: "orphan output"}},
			},
		},
	}

	te.ReplaySession(entries) // 不应 panic

	texts := collectTexts(chat.Children())
	if strings.Contains(strings.Join(texts, "\n"), "orphan output") {
		t.Errorf("orphan tool result should be skipped, got:\n%s", strings.Join(texts, "\n"))
	}
}
