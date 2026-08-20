package mcp

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestManagerToyServer 端到端验证：配置 → 连接 → 工具可见 → 调用往返 →
// 坏 server 降级 → 禁用/启用/重试。
func TestManagerToyServer(t *testing.T) {
	dir := t.TempDir()
	bin := buildToyServer(t, dir)

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYCLUE_CONFIG_DIR", home)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"toy": map[string]any{"command": bin},
			"bad": map[string]any{"command": "/nonexistent/tinyclue-mcp-bad"},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	// 1. 工具清单：toy 的 echo/add 可见，bad 的不可见。
	names := map[string]bool{}
	for _, tool := range m.Tools() {
		names[tool.Name()] = true
	}
	for _, want := range []string{"mcp__toy__echo", "mcp__toy__add"} {
		if !names[want] {
			t.Fatalf("tools = %v, missing %s", names, want)
		}
	}
	if names["mcp__bad__echo"] {
		t.Fatalf("bad server should expose no tools, got %v", names)
	}

	// 2. echo 往返。
	res, err := m.CallTool(ctx, "toy", "echo", map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if res.Content != "hello" {
		t.Fatalf("echo = %q, want %q", res.Content, "hello")
	}

	// 3. add 往返。
	res, err = m.CallTool(ctx, "toy", "add", map[string]any{"a": 2.0, "b": 3.0})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if res.Content != "5" {
		t.Fatalf("add = %q, want %q", res.Content, "5")
	}

	// 4. 坏 server 降级为 Error，不 panic。
	sums := m.Summary()
	byName := map[string]ServerSummary{}
	for _, s := range sums {
		byName[s.Name] = s
	}
	bad, ok := byName["bad"]
	if !ok {
		t.Fatalf("summary missing bad server: %v", byName)
	}
	if bad.Status != StatusError {
		t.Fatalf("bad server status = %v, want error", bad.Status)
	}
	toy := byName["toy"]
	if toy.Status != StatusConnected || toy.ToolCount != 2 {
		t.Fatalf("toy status = %v count = %d, want connected/2", toy.Status, toy.ToolCount)
	}

	// 5. 禁用后工具不可见、调用报错。
	m.Disable("toy")
	for _, tool := range m.Tools() {
		if tool.Name() == "mcp__toy__echo" {
			t.Fatal("echo should be gone after disable")
		}
	}
	if _, err := m.CallTool(ctx, "toy", "echo", map[string]any{"msg": "x"}); err == nil {
		t.Fatal("call after disable should error")
	}

	// 6. 启用后重连，工具恢复。
	added, err := m.Enable("toy")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("enable returned %d tools, want 2", len(added))
	}
	res, err = m.CallTool(ctx, "toy", "echo", map[string]any{"msg": "again"})
	if err != nil {
		t.Fatalf("echo after enable: %v", err)
	}
	if res.Content != "again" {
		t.Fatalf("echo after enable = %q, want %q", res.Content, "again")
	}

	// 7. 重试坏 server 仍失败。
	if _, err := m.Reconnect("bad"); err == nil {
		t.Fatal("reconnect bad should fail")
	}
}

// TestManagerReconnectReflectsServerToolChange 验证 mcp 契约：Reconnect 返回该 server 当前暴露的工具集
// （工具集变化时如实反映，diff/归并由注册表层负责，mcp 层不做增删计算）。
func TestManagerReconnectReflectsServerToolChange(t *testing.T) {
	dir := t.TempDir()
	bin := buildToyServer(t, dir)

	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TINYCLUE_CONFIG_DIR", home)

	cfg := map[string]any{
		"mcpServers": map[string]any{
			"toy": map[string]any{
				"command": bin,
				"env":     map[string]string{"TOY_TOOLS": "echo,add"},
			},
		},
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "mcp.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m := NewManager()
	m.Init(ctx)
	defer m.Close()

	st := m.serverByName("toy")
	if st == nil {
		t.Fatal("toy server missing")
	}
	// 项目级 .mcp.json 可能额外加载 toy-demo，故只核对 toy 自身的桥接工具数。
	st.mu.RLock()
	initial := len(st.tools)
	st.mu.RUnlock()
	if initial != 2 {
		t.Fatalf("initial toy tools = %d, want 2", initial)
	}

	// 模拟 server 工具集变化：下次连接只注册 echo。
	st.mu.Lock()
	st.named.Server.Env["TOY_TOOLS"] = "echo"
	st.mu.Unlock()

	tools, err := m.Reconnect("toy")
	if err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	var got []string
	for _, bt := range tools {
		got = append(got, bt.Name())
	}
	if len(got) != 1 || got[0] != "mcp__toy__echo" {
		t.Fatalf("tools = %v, want [mcp__toy__echo]", got)
	}
}

// buildToyServer 从模块根构建玩具 server 二进制，返回其绝对路径。
func buildToyServer(t *testing.T, outDir string) string {
	t.Helper()
	bin := filepath.Join(outDir, "tinyclue-test-mcp")
	cmd := exec.Command("go", "build", "-o", bin, "./tests/mcp_test_server")
	cmd.Dir = moduleRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build toy server: %v\n%s", err, out)
	}
	return bin
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
