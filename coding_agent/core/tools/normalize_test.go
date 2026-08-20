package tools

import (
	"testing"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
)

func TestNormalizeNameForMCP(t *testing.T) {
	cases := map[string]string{
		"my-server": "my-server",
		"my_server": "my_server",
		"MyServer1": "MyServer1",
		"my server": "my_server",
		"中文":        "__",
		"a.b/c":     "a_b_c",
		"a-b_c.d":   "a-b_c_d",
		"  leading": "__leading",
		"":          "",
	}
	for in, want := range cases {
		if got := NormalizeNameForMCP(in); got != want {
			t.Fatalf("NormalizeNameForMCP(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBridgeToolNormalizedName 验证桥接名做 normalize，而 Execute 路由仍用原始名。
func TestBridgeToolNormalizedName(t *testing.T) {
	bt := NewMCPBridgeTool("my server", "read file", "desc", apitypes.Parameters{})
	if got, want := bt.Name(), "mcp__my_server__read_file"; got != want {
		t.Fatalf("bridge name = %q, want %q", got, want)
	}
	if bt.server != "my server" || bt.tool != "read file" {
		t.Fatalf("route fields = %q/%q, want raw values", bt.server, bt.tool)
	}
}
