package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Server struct {
	Type     string            `json:"type,omitempty"`     // 传输类型：""|"stdio"|"http"
	Command  string            `json:"command,omitempty"`  // stdio 启动命令
	URL      string            `json:"url,omitempty"`      // http endpoint
	Headers  map[string]string `json:"headers,omitempty"`  // http 附加请求头（非 OAuth 鉴权/自定义头）
	Args     []string          `json:"args,omitempty"`     // 命令参数
	Env      map[string]string `json:"env,omitempty"`      // 注入到子进程的额外环境变量
	Disabled bool              `json:"disabled,omitempty"` // 为 true 则跳过该 server
}

// NamedServer 携带来源信息的 server 条目（Source: "user" 或所在目录路径）。
type NamedServer struct {
	Name   string
	Server Server
	Source string
}

type MCPConfig struct {
	Servers                map[string]Server `json:"mcpServers"`
	EnabledMcpjsonServers  []string          `json:"enabledMcpjsonServers,omitempty"`
	DisabledMcpjsonServers []string          `json:"disabledMcpjsonServers,omitempty"`

	// sources 记录每个 server 名来自哪个文件（合并时最后写入的为准）。
	sources map[string]string
}

// LoadMCPConfig 加载并合并 MCP 配置，范围与优先级（浅→深覆盖）：
//
//  1. 用户级：<TinyClueDir>/mcp.json（$TINYCLUE_CONFIG_DIR 生效，默认 ~/.tinyclue/mcp.json）；
//  2. 项目级：从 cwd 向上走到文件系统根，收集每个 .mcp.json，浅→深逐层合并（最深 win）。
//
// 解析失败的单个文件会被跳过（不中断），并聚合为返回的 error（仅提示，非致命）。
func LoadMCPConfig(cwd string) (*MCPConfig, error) {
	cfg := &MCPConfig{Servers: map[string]Server{}, sources: map[string]string{}}
	var errs []string

	home, err := TinyClueDir()
	if err != nil {
		errs = append(errs, fmt.Sprintf("resolve home: %v", err))
	} else if err := cfg.mergeFile(filepath.Join(home, "mcp.json"), "user"); err != nil {
		errs = append(errs, err.Error())
	}

	for _, d := range walkUpDirs(cwd) {
		if err := cfg.mergeFile(filepath.Join(d, ".mcp.json"), d); err != nil {
			errs = append(errs, err.Error())
		}
	}

	if len(errs) > 0 {
		return cfg, fmt.Errorf("部分 MCP 配置文件解析失败（已跳过）: %s", strings.Join(errs, "; "))
	}
	return cfg, nil
}

// mergeFile 读取一个 mcp.json 并合并进 cfg。文件不存在则跳过。
// 同名 server 覆盖（更深/后合并的赢），并记录来源。enabled/disabled 列表取并集。
//
// 合并时对每个 server 做 ~ / ${VAR} 展开（见 expandServer），因此 mcp.json 里可写
// "~/path" 或 "${HOME}/path" 代替写死的主目录绝对路径，避免把个人绝对路径提交到公开仓库。
func (c *MCPConfig) mergeFile(path, source string) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	var sub MCPConfig
	if err := json.Unmarshal(data, &sub); err != nil {
		return fmt.Errorf("%s: %v", path, err)
	}
	for name, s := range sub.Servers {
		c.Servers[name] = expandServer(s)
		c.sources[name] = source
	}
	c.EnabledMcpjsonServers = dedupeStrings(append(c.EnabledMcpjsonServers, sub.EnabledMcpjsonServers...))
	c.DisabledMcpjsonServers = dedupeStrings(append(c.DisabledMcpjsonServers, sub.DisabledMcpjsonServers...))
	return nil
}

// expandServer 展开 server 配置中所有可能含路径/环境的字段（command、url、headers、
// args、env），使 mcp.json 可用 ~ 或 ${VAR} 而非写死绝对路径。
func expandServer(s Server) Server {
	s.Command = expandPath(s.Command)
	s.URL = expandPath(s.URL)
	for k, v := range s.Headers {
		s.Headers[k] = expandPath(v)
	}
	for i, a := range s.Args {
		s.Args[i] = expandPath(a)
	}
	for k, v := range s.Env {
		s.Env[k] = expandPath(v)
	}
	return s
}

// expandPath 展开路径/字符串中的 ~ 与 ${VAR} / $VAR：
//   - 开头的 ~ 或 ~/ 替换为用户主目录（优先 os.UserHomeDir，比 $HOME 更可靠）；
//   - 其余 ${VAR} / $VAR 由 os.ExpandEnv 展开（未定义的环境变量替换为空串）。
//
// 空串原样返回。这样 .mcp.json 可写 "~/.tinyclue/..." 或 "${HOME}/..."，提交到仓库时
// 不暴露个人主目录路径。
func expandPath(v string) string {
	if v == "" {
		return v
	}
	if v == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	} else if strings.HasPrefix(v, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[2:])
		}
	}
	return os.ExpandEnv(v)
}

// EnabledServers 返回过滤（Disabled 标志 + enabled/disabled 列表）后按名称排序的 server 列表。
func (c *MCPConfig) EnabledServers() []NamedServer {
	servers := make(map[string]Server, len(c.Servers))
	if len(c.EnabledMcpjsonServers) > 0 {
		allowed := make(map[string]bool, len(c.EnabledMcpjsonServers))
		for _, n := range c.EnabledMcpjsonServers {
			allowed[n] = true
		}
		for name, s := range c.Servers {
			if allowed[name] {
				servers[name] = s
			}
		}
	} else {
		for name, s := range c.Servers {
			servers[name] = s
		}
	}

	blocked := make(map[string]bool, len(c.DisabledMcpjsonServers))
	for _, n := range c.DisabledMcpjsonServers {
		blocked[n] = true
	}

	out := make([]NamedServer, 0, len(servers))
	for name, s := range servers {
		if blocked[name] || s.Disabled {
			continue
		}
		out = append(out, NamedServer{Name: name, Server: s, Source: c.sources[name]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// walkUpDirs 返回从文件系统根到 dir 的目录序列（浅→深），供逐层合并。
func walkUpDirs(dir string) []string {
	dir = filepath.Clean(dir)
	var dirs []string
	cur := dir
	for {
		dirs = append(dirs, cur)
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	for i, j := 0, len(dirs)-1; i < j; i, j = i+1, j-1 {
		dirs[i], dirs[j] = dirs[j], dirs[i]
	}
	return dirs
}

func dedupeStrings(in []string) []string {
	if len(in) < 2 {
		return in
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
