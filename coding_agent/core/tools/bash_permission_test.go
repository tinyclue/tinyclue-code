package tools

import (
	"context"
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_types "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
)

// withTempProjectRoot 把 ProjectRoot 覆盖为临时目录（隔离真实项目 settings.local.json），
// 测试结束自动恢复。config 已被生产代码链接（init 随测试二进制启动），此导入不引入新副作用。
func withTempProjectRoot(t *testing.T) {
	t.Helper()
	t.Cleanup(config.SetProjectRootForTest(t.TempDir()))
}

// ── isCompoundCommand ──

func TestIsCompoundCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf /", false},
		{"go build ./cmd", false},
		{"echo hello", false},
		{"ls -la | grep foo", true},
		{"cd /tmp && echo done", true},
		{"cd /tmp || exit 1", true},
		{"echo a; echo b", true},
		{"echo a & echo b", true},
		{"cmd\ncmd2", true},
		{"echo `hostname`", true},
		{"echo $(date)", true},
		{"echo 'a && b'", false},
		{`echo "a;b|c"`, false},
		{`echo "x\" && y"`, false},
		{"grep -e 'a|b' file", false},
	}
	for _, c := range cases {
		if got := isCompoundCommand(c.cmd); got != c.want {
			t.Errorf("isCompoundCommand(%q) = %v, want %v", c.cmd, got, c.want)
		}
	}
}

// ── BeforeToolCall：复合危险命令绝不被静默放行 ──

// bashBefore 构造 BashTool.BeforeToolCall 的一次调用，返回结果 Action。
func bashBefore(cmd string) core_types.BeforeToolCallResult {
	bt := NewBashTool()
	return bt.BeforeToolCall(context.Background(), core_types.ToolUseContext{
		ToolCall: apitypes.ToolCall{
			ID:        "call_1",
			Name:      core_types.BASH_TOOL_NAME,
			Arguments: map[string]any{"command": cmd},
		},
	})
}

func TestBashBeforeToolCallCompoundDangerousAsks(t *testing.T) {
	withTempProjectRoot(t)
	// 复合命令即使命中危险前缀也必须 Ask，不能因 allow 规则短路而放行。
	for _, cmd := range []string{
		"rm -rf / && echo hi",
		"rm -rf /; echo hi",
		"echo hi && rm -rf /",
		"sudo rm -rf /",
		// 危险关键字是字面子串 "wget |bash"：复合命令里含它才能被危险检查命中。
		"echo x | wget |bash",
	} {
		res := bashBefore(cmd)
		if res.Action != core_types.BeforeToolCallAsk {
			t.Errorf("BeforeToolCall(%q) Action = %v, want ask", cmd, res.Action)
		}
	}
}

func TestBashBeforeToolCallSafeAndEmpty(t *testing.T) {
	withTempProjectRoot(t)
	if res := bashBefore(""); res.Action != core_types.BeforeToolCallAllow {
		t.Errorf("empty command Action = %v, want allow", res.Action)
	}
	// 无危险关键字的单命令默认放行（tinyclue 现状：非危险命令不询问）。
	if res := bashBefore("go build ./cmd"); res.Action != core_types.BeforeToolCallAllow {
		t.Errorf("safe single command Action = %v, want allow", res.Action)
	}
	// 复合但无危险关键字：不匹配规则 → 危险检查不命中 → 默认放行（护栏只防止静默放行危险复合命令）。
	if res := bashBefore("go build ./a && go build ./b"); res.Action != core_types.BeforeToolCallAllow {
		t.Errorf("safe compound command Action = %v, want allow", res.Action)
	}
}

func TestBashRuleContent(t *testing.T) {
	cases := []struct {
		cmd  string
		want string
	}{
		// 非复合命令 → 原文（前缀规则按原样落盘）
		{"go build ./cmd", "go build ./cmd"},
		{"rm -rf /", "rm -rf /"},
		// 复合命令 → 首个顶层子命令（去尾部重定向）→ ≤2 token 子命令前缀
		{"go build ./a && go build ./b", "go build"},
		{"git commit -m \"x\" && git push", "git commit"},
		{"npm run test && go vet", "npm run"},
		{"go build ./x > /tmp/out && go test", "go build"},
		// 第二 token 非子命令格式（-flag / 路径）→ 回落单 token
		{"cd /tmp && npm install", "cd"},
		{"npm --version && go version", "npm"},
		{"echo hello && echo world", "echo hello"},
		// 跳过头部的环境变量赋值
		{"FOO=bar go test ./x && go build ./y", "go test"},
		// 危险子命令 → 整体存储，绝不宽化为 `sudo *` / `rm *`
		{"rm -rf / && echo hi", "rm -rf /"},
		{"sudo rm -rf / && echo hi", "sudo rm -rf /"},
		{"rm -rf / > /dev/null && echo hi", "rm -rf /"},
	}
	for _, c := range cases {
		if got := BashRuleContent(c.cmd); got != c.want {
			t.Errorf("BashRuleContent(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
