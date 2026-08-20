// Command mcp_test_server is a toy stdio MCP server used by tinyclue's
// automated tests and manual TUI verification. It registers two tools:
//   - echo(msg string)  -> returns the message as-is
//   - add(a, b number)  -> returns a + b
//
// The TOY_TOOLS environment variable (comma-separated) restricts which tools
// are registered, so tests can simulate a server whose tool set changes
// between connects. Empty/absent registers all tools.
//
// It speaks MCP over stdio and blocks until SIGINT/SIGTERM.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	log.SetOutput(os.Stderr)

	server := mcp.NewServer(&mcp.Implementation{Name: "tinyclue-test-mcp", Version: "0.1.0"}, nil)

	echoTool := &mcp.Tool{
		Name:        "echo",
		Description: "Echoes the given message back verbatim.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}},"required":["msg"]}`),
	}
	echoHandler := func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct{ Msg string }
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: args.Msg}},
		}, nil
	}

	addTool := &mcp.Tool{
		Name:        "add",
		Description: "Sums two numbers and returns the result.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"a":{"type":"number"},"b":{"type":"number"}},"required":["a","b"]}`),
	}
	addHandler := func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			A float64 `json:"a"`
			B float64 `json:"b"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%g", args.A+args.B)}},
		}, nil
	}

	// TOY_TOOLS 门控注册子集，供测试模拟工具集变化。
	allowed := map[string]bool{}
	for _, s := range strings.Split(os.Getenv("TOY_TOOLS"), ",") {
		if s != "" {
			allowed[s] = true
		}
	}
	register := func(name string, tool *mcp.Tool, handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error)) {
		if len(allowed) > 0 && !allowed[name] {
			return
		}
		server.AddTool(tool, handler)
	}
	register("echo", echoTool, echoHandler)
	register("add", addTool, addHandler)

	session, err := server.Connect(context.Background(), &mcp.StdioTransport{}, nil)
	if err != nil {
		log.Fatalf("mcp_test_server: connect: %v", err)
	}

	// 阻塞直到客户端关闭连接（stdin EOF，测试/应用退出时的快速路径）或收到终止信号。
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	wait := make(chan error, 1)
	go func() { wait <- session.Wait() }()

	select {
	case <-wait:
		// 客户端已关闭 stdio 连接，正常退出。
	case <-sig:
		_ = session.Close()
	}
}
