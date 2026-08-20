package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestExpandPath 验证 expandPath 对 ~、~/ 与 ${VAR}/$VAR 的展开，空串/绝对路径原样返回。
func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"~", home},
		{"~/x", filepath.Join(home, "x")},
		{"~/.tinyclue/a", filepath.Join(home, ".tinyclue", "a")},
		{"${HOME}/x", filepath.Join(home, "x")},
		{"$HOME/x", filepath.Join(home, "x")},
		{"/absolute/path", "/absolute/path"},
	}
	for _, c := range cases {
		if got := expandPath(c.in); got != c.want {
			t.Errorf("expandPath(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestLoadMCPConfigExpandsPaths 验证 LoadMCPConfig 合并项目 .mcp.json 时，对 command、
// args、env 中的 ~ 与 ${VAR} 做展开，使配置不写死个人主目录绝对路径。
func TestLoadMCPConfigExpandsPaths(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	dir := t.TempDir()
	proj := filepath.Join(dir, "project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	content := `{
	  "mcpServers": {
	    "local": {
	      "command": "~/data/bin/tinyclue-test-mcp",
	      "args": ["--profile", "~/.tinyclue/p", "--dir", "${HOME}/out"],
	      "env": { "CACHE": "~/.cache/tinyclue" }
	    }
	  }
	}`
	if err := os.WriteFile(filepath.Join(proj, ".mcp.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 用户级 mcp.json 不存在（$TINYCLUE_CONFIG_DIR 指向空目录），只测项目级合并。
	t.Setenv("TINYCLUE_CONFIG_DIR", filepath.Join(dir, "home"))

	cfg, err := LoadMCPConfig(proj)
	if err != nil {
		t.Fatalf("LoadMCPConfig: %v", err)
	}
	s, ok := cfg.Servers["local"]
	if !ok {
		t.Fatal("server \"local\" not merged")
	}
	if want := filepath.Join(home, "data", "bin", "tinyclue-test-mcp"); s.Command != want {
		t.Errorf("Command = %q, want %q", s.Command, want)
	}
	if want := filepath.Join(home, ".tinyclue", "p"); len(s.Args) < 2 || s.Args[1] != want {
		t.Errorf("Args[1] = %q, want %q", s.Args[1], want)
	}
	if want := filepath.Join(home, "out"); len(s.Args) < 4 || s.Args[3] != want {
		t.Errorf("Args[3] = %q, want %q", s.Args[3], want)
	}
	if want := filepath.Join(home, ".cache", "tinyclue"); s.Env["CACHE"] != want {
		t.Errorf("Env[CACHE] = %q, want %q", s.Env["CACHE"], want)
	}
}
