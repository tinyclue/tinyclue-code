// Package mcp 提供 MCP（Model Context Protocol）server 连接与工具桥接。
//
// 本文件是唯一直接触碰官方 SDK 的地方：连接、列工具、调工具、关闭全部封装在这里，
// SDK 签名漂移只需修改本文件。
package mcp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/tinyclue/tinyclue-code/config"
	"github.com/tinyclue/tinyclue-code/log"
)

// 默认连接超时；server Env 或进程 Env 中的 MCP_TIMEOUT 可覆盖。
// 30s：streamable 连接是多次 HTTP 往返（SEP-2575 discover + initialize + initialized），
// 慢网路径（如跨区域 TLS 握手 ~3-5s/跳）下 10s 预算会反复撞超时（deepwiki 等）。
const (
	defaultConnectTimeout = 30 * time.Second
	defaultCallTimeout    = 60 * time.Second
)

// defaultAuthTimeout 交互授权连接（/mcp [a]/[r]，浏览器授权可长）的整流程兜底超时：
// 超过则连接报错、面板 busy 正常清除，防授权挂起。var 便于测试覆盖为短值。
var defaultAuthTimeout = 5 * time.Minute

// sdkSession 抽象 SDK 的 ClientSession。
type sdkSession interface {
	ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)
	CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)
	Close() error
	// Wait 阻塞到连接被关闭（http 自动重连的被动断线信号）。仅 http 用到，stdio 不 watch。
	Wait() error
}

// toolInfo 与 SDK 无关的工具元数据。
type toolInfo struct {
	Name        string
	Description string
	InputSchema map[string]any
}

// toolResult 与 SDK 无关的调用结果（Content 为各内容块的文本拼接）。
type toolResult struct {
	Content string
	IsError bool
}

// stderrWriter 接管 MCP server 子进程的 stderr：默认会继承父进程 stderr，
// 在 raw-mode TUI 下会把 server 的输出（如 Playwright 的 banner/浏览器日志）污染到屏幕上。
// 这里改为写入项目日志（便于排查），并保留有界尾部供连接失败时报错。
type stderrWriter struct {
	name string
	buf  bytes.Buffer
	mu   sync.Mutex // Write（exec 拷贝 goroutine）与 tail（connectSession 读取）可能并发
}

func (w *stderrWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			log.Infof(context.Background(), "mcp %s stderr: %s", w.name, line)
		}
	}
	// 只保留有界尾部（8KB），防 server 无限打日志撑爆内存。
	if w.buf.Len() < 8<<10 {
		w.buf.Write(p)
	}
	return len(p), nil
}

// tail 返回最近 ~300 字符的 stderr 内容（供错误信息展示）。
func (w *stderrWriter) tail() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.TrimSpace(w.buf.String())
	if len(s) > 300 {
		s = s[len(s)-300:]
	}
	return s
}

// connectSession 按传输类型建立 MCP 会话：
//   - "" / "stdio" → 本地子进程（CommandTransport）；
//   - "http" → 远端 streamable HTTP（StreamableClientTransport + OAuthHandler）；
//   - 其余（如 "sse"）→ 报 unsupported。
//
// interactive 区分连接模式：
//   - false = 启动后台连接：http 遇 401 由 startupHandler 返回 ErrNeedsAuth，绝不开浏览器、不阻塞；
//   - true  = /mcp 面板用户触发（Authenticate / Re-authenticate）：http 走完整授权流程，可打开浏览器等回调，
//     此时不加连接超时（浏览器授权可长，仅受 ctx——应用生命周期——约束）。
//
// onAuthStart 在交互授权流程拿到授权 URL 时回调（用于发布"授权中"提示），可为 nil。
func connectSession(ctx context.Context, name string, s config.Server, interactive bool, onAuthStart func(server, url string)) (sdkSession, error) {
	switch s.Type {
	case "", "stdio":
		return connectStdio(ctx, name, s)
	case "http":
		return connectHTTP(ctx, name, s, interactive, onAuthStart)
	default:
		return nil, fmt.Errorf("MCP server %q: unsupported transport type %q (v1 supports stdio/http)", name, s.Type)
	}
}

// connectStdio 启动 stdio 子进程并建立 MCP 会话。
func connectStdio(ctx context.Context, name string, s config.Server) (sdkSession, error) {
	cmd := exec.Command(s.Command, s.Args...)
	cmd.Env = mergeEnv(os.Environ(), s.Env)
	stderr := &stderrWriter{name: name}
	cmd.Stderr = stderr
	transport := &mcp.CommandTransport{Command: cmd}
	client := mcp.NewClient(&mcp.Implementation{Name: "tinyclue", Version: "0.1.0"}, nil)

	connectCtx, cancel := context.WithTimeout(ctx, serverTimeout(s, defaultConnectTimeout))
	defer cancel()
	sess, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		msg := fmt.Errorf("connect MCP server %q: %w", s.Command, err)
		if tail := stderr.tail(); tail != "" {
			msg = fmt.Errorf("connect MCP server %q: %w (stderr: %s)", s.Command, err, tail)
		}
		return nil, msg
	}
	return sess, nil
}

// connectHTTP 通过 streamable HTTP 建立 MCP 会话，携带 OAuth handler 与自定义请求头。
func connectHTTP(ctx context.Context, name string, s config.Server, interactive bool, onAuthStart func(server, url string)) (sdkSession, error) {
	if !strings.HasPrefix(s.URL, "http://") && !strings.HasPrefix(s.URL, "https://") {
		return nil, fmt.Errorf("MCP server %q: invalid url %q (want http:// or https://)", name, s.URL)
	}
	handler, cleanup, err := buildAuthHandler(name, interactive, onAuthStart)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	transport := &mcp.StreamableClientTransport{
		Endpoint:     s.URL,
		HTTPClient:   httpClientFor(s.Headers),
		OAuthHandler: handler,
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "tinyclue", Version: "0.1.0"}, nil)

	connectCtx := ctx
	var budget time.Duration
	if !interactive {
		budget = serverTimeout(s, defaultConnectTimeout)
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	} else {
		// 交互授权：浏览器授权可长，但整个连接（含回调等待）有兜底超时，防挂起。
		budget = defaultAuthTimeout
		var cancel context.CancelFunc
		connectCtx, cancel = context.WithTimeout(ctx, budget)
		defer cancel()
	}
	sess, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		if connectCtx.Err() == context.DeadlineExceeded {
			// 超时是"连不上"最常见的根因（慢网路径下多次往返撞预算），把预算与超时性质
			// 明示出来，别让用户只看到裸 context deadline exceeded；%w 保留原始错误链
			//（含失败步骤，如 sending "notifications/initialized"）供 errors.Is/As 判定。
			return nil, fmt.Errorf("connect MCP server %q: connect timeout (budget %s): %w", s.URL, budget, err)
		}
		return nil, fmt.Errorf("connect MCP server %q: %w", s.URL, err)
	}
	return sess, nil
}

// listTools 拉取 server 的工具清单。
func listTools(ctx context.Context, sess sdkSession) ([]toolInfo, error) {
	res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := make([]toolInfo, 0, len(res.Tools))
	for _, t := range res.Tools {
		schema, _ := t.InputSchema.(map[string]any)
		out = append(out, toolInfo{Name: t.Name, Description: t.Description, InputSchema: schema})
	}
	return out, nil
}

// callTool 调用 MCP 工具，返回文本拼接结果与 isError 标记。
// 超时与连接一致走 serverTimeout：server Env 或进程 Env 的 MCP_TIMEOUT 可覆盖默认 60s。
func callTool(ctx context.Context, sess sdkSession, s config.Server, name string, args map[string]any) (toolResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, serverTimeout(s, defaultCallTimeout))
	defer cancel()
	res, err := sess.CallTool(callCtx, &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		return toolResult{}, err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		switch v := c.(type) {
		case *mcp.TextContent:
			sb.WriteString(v.Text)
		case *mcp.ImageContent:
			fmt.Fprintf(&sb, "[image: %s, %d bytes]", v.MIMEType, len(v.Data))
		default:
			sb.WriteString("[mcp content block]")
		}
	}
	return toolResult{Content: sb.String(), IsError: res.IsError}, nil
}

// mergeEnv 以父进程环境为基础，叠加 server 配置的 env。
func mergeEnv(parent []string, extra map[string]string) []string {
	env := append([]string{}, parent...)
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// serverTimeout 解析 server Env 或进程 Env 中的 MCP_TIMEOUT；无效则回落默认值。
func serverTimeout(s config.Server, def time.Duration) time.Duration {
	if v := lookupEnv(s.Env, "MCP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	if v := os.Getenv("MCP_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func lookupEnv(m map[string]string, key string) string {
	if m == nil {
		return ""
	}
	return m[key]
}
