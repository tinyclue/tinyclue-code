// 文件写入 <projectRoot>/.tinyclue/config/settings.local.json。
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// bashToolName 是 Bash 工具名，与 coding_agent/core/types.BASH_TOOL_NAME 一致。
// 这里用字面量避免 config 反向依赖 coding_agent。
const bashToolName = "Bash"

// ── 项目根 ──

var (
	projectRootOnce sync.Once
	projectRootVal  string

	// projectRootMu 保护 projectRootOverride（SetProjectRootForTest 与 ProjectRoot 可能并发）。
	projectRootMu       sync.RWMutex
	projectRootOverride string
)

// ProjectRoot 返回项目根目录：从进程 cwd 向上找 .git（目录或文件），找不到则回落 cwd。
// 结果进程内缓存一次（cwd 不变）；SetProjectRootForTest 设置的覆盖值优先。
func ProjectRoot() string {
	projectRootMu.RLock()
	override := projectRootOverride
	projectRootMu.RUnlock()
	if override != "" {
		return override
	}
	projectRootOnce.Do(func() {
		cwd := CLI.Cwd
		dir := cwd
		for {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				projectRootVal = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		projectRootVal = cwd
	})
	return projectRootVal
}

// SetProjectRootForTest 仅在测试中覆盖 ProjectRoot 的返回值（工具层测试隔离，
// 避免真实项目 .tinyclue/config/settings.local.json 污染断言）。返回恢复函数，
// 测试结束 defer / t.Cleanup 调用。
func SetProjectRootForTest(root string) func() {
	projectRootMu.Lock()
	old := projectRootOverride
	projectRootOverride = root
	projectRootMu.Unlock()
	return func() {
		projectRootMu.Lock()
		projectRootOverride = old
		projectRootMu.Unlock()
	}
}

// permissionFilePath 返回权限规则文件路径。
func permissionFilePath(root string) string {
	return filepath.Join(root, ".tinyclue", "config", "settings.local.json")
}

// escapeRuleContent 转义规则内容中的特殊字符，保证 "Tool(content)" 格式无损往返。
func escapeRuleContent(content string) string {
	r := strings.ReplaceAll(content, `\`, `\\`)
	r = strings.ReplaceAll(r, `(`, `\(`)
	r = strings.ReplaceAll(r, `)`, `\)`)
	return r
}

// unescapeRuleContent 还原 escapeRuleContent 的转义（逆序）。
func unescapeRuleContent(content string) string {
	r := strings.ReplaceAll(content, `\(`, `(`)
	r = strings.ReplaceAll(r, `\)`, `)`)
	r = strings.ReplaceAll(r, `\\`, `\`)
	return r
}

// ── 规则解析 ──

// parseRule 解析 "ToolName(pattern)" 规则字符串。
// 只认未转义的圆括号（奇数个前置反斜杠视为已转义）；要求右括号在串尾；
// 无 "(" / 无匹配右括号 / 内容为空 / 内容为 "*" → 工具级规则（pattern 返回 ""）。
func parseRule(rule string) (toolName, pattern string) {
	rule = strings.TrimSpace(rule)
	open := findUnescaped(rule, '(', false)
	if open < 0 {
		return rule, ""
	}
	close := findUnescaped(rule, ')', true)
	if close <= open || close != len(rule)-1 {
		return rule, "" // 右括号缺失/不在末尾 → 整串视为工具级
	}
	toolName = strings.TrimSpace(rule[:open])
	if toolName == "" {
		return rule, ""
	}
	content := strings.TrimSpace(unescapeRuleContent(rule[open+1 : close]))
	if content == "" || content == "*" {
		return toolName, "" // Bash() / Bash(*) → 工具级
	}
	return toolName, content
}

// findUnescaped 查找 s 中第 direction 侧第一个未被转义的字符。
// fromEnd=false 从前往后找第一个；fromEnd=true 从后往前找最后一个。找不到返回 -1。
func findUnescaped(s string, char byte, fromEnd bool) int {
	if fromEnd {
		for i := len(s) - 1; i >= 0; i-- {
			if s[i] == char && backslashCount(s, i)%2 == 0 {
				return i
			}
		}
		return -1
	}
	for i := 0; i < len(s); i++ {
		if s[i] == char && backslashCount(s, i)%2 == 0 {
			return i
		}
	}
	return -1
}

func backslashCount(s string, idx int) int {
	n := 0
	for j := idx - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n
}

// matchPattern 判断参数串是否命中规则模式：
//   - 空/`*` → 工具级，恒真；
//   - 其余做通配匹配：* → .*，^... 锚定（dotAll，. 匹配换行），结尾 "\n?$" 允许一个可选尾随换行，
//     尾部单独 " *" 特判为 "( .*)?"。
func matchPattern(pattern, value string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	re := wildcardToRegex(pattern)
	return re.MatchString(value)
}

func wildcardToRegex(pattern string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("(?s)^")
	starCount := 0
	for _, c := range pattern {
		switch c {
		case '*':
			starCount++
			b.WriteString(".*")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '?', '\\':
			b.WriteByte('\\')
			b.WriteRune(c)
		default:
			b.WriteString(string(c))
		}
	}
	regexStr := b.String()
	// 尾部单独 " *" → "( .*)?"：go build * 也匹配裸 go build（但 * 在中间时不特判）。
	if starCount == 1 && strings.HasSuffix(pattern, " *") {
		regexStr = strings.TrimSuffix(regexStr, " .*")
		regexStr += "( .*)?"
	}
	// 结尾 "\n?$"：对齐 JS 正则的 $ 语义。JS 的 $ 匹配结尾（含结尾前一个可选的换行），
	//（命令带尾随换行时同样命中），且方向仍是更严（多问一次），不会误放行。
	regexStr += "\\n?$"
	return regexp.MustCompile(regexStr)
}

// ── 参数渲染 ──

// RenderArgs 把工具调用参数渲染为可匹配/展示的字符串：
//   - Bash → command 参数原文；
//   - 其余 → json.Marshal(args)（Go 对 map 按 key 排序，确定性输出）；空参数 → ""。
func RenderArgs(toolName string, args map[string]any) string {
	if toolName == bashToolName {
		cmd, _ := args["command"].(string)
		return cmd
	}
	if len(args) == 0 {
		return ""
	}
	data, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return string(data)
}

// ── 规则构建 ──

// ruleString 根据工具类型构建要落盘的规则字符串：
//   - 其他 → 工具级 "toolName"（忽略参数，任意参数不再询问）。
func ruleString(toolName, argsString string) string {
	if toolName == bashToolName {
		if argsString == "" {
			return bashToolName + "(*)"
		}
		return bashToolName + "(" + escapeRuleContent(argsString) + " *)"
	}
	return toolName
}

// RememberLabel 返回权限面板第三选项的文案：
//   - Bash → "Allow, and don't ask again for Bash <command>"
//   - 其他 → "Allow, and don't ask again for <toolName>"
func RememberLabel(toolName, argsString string) string {
	const prefix = "Allow, and don't ask again for "
	if toolName == bashToolName {
		return prefix + bashToolName + " " + argsString
	}
	return prefix + toolName
}

// ── 加载与匹配 ──

// permMu 保护规则文件的读改写与规则缓存（agent goroutine 读 / TUI goroutine 写并发）。
var permMu sync.Mutex

// allowRule 是解析并预编译好的一条 allow 规则。
type allowRule struct {
	toolName string
	re       *regexp.Regexp // nil = 工具级规则（任意参数命中）
}

// ruleCacheEntry 是某 root 的规则快照，带文件 stat 用于失效。
type ruleCacheEntry struct {
	modTime time.Time
	size    int64
	rules   []allowRule
}

// ruleCache 按项目根缓存编译后的规则，避免每次工具调用都读盘 + 编译正则。
// 由 permMu 串行访问；addAllowRule 写盘后主动删除对应条目（防同秒同大小不刷新 mtime）。
var ruleCache = map[string]ruleCacheEntry{}

// loadCompiledRules 读取并编译 root 的 allow 规则（含文件 stat 供失效判断）。
// 文件缺失/损坏 → 空快照（绝不 panic）。调用方须持有 permMu。
func loadCompiledRules(root string) ruleCacheEntry {
	path := permissionFilePath(root)
	st, err := os.Stat(path)
	if err != nil {
		return ruleCacheEntry{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ruleCacheEntry{}
	}
	var doc struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return ruleCacheEntry{}
	}
	rules := make([]allowRule, 0, len(doc.Permissions.Allow))
	for _, rule := range doc.Permissions.Allow {
		rTool, pattern := parseRule(rule)
		if rTool == "" {
			continue
		}
		ar := allowRule{toolName: rTool}
		if pattern != "" {
			ar.re = wildcardToRegex(pattern)
		}
		rules = append(rules, ar)
	}
	return ruleCacheEntry{modTime: st.ModTime(), size: st.Size(), rules: rules}
}

// ruleCacheStale 判断缓存快照是否过期：文件 stat 变了或文件被删（被删 → 需重载为空）。
func ruleCacheStale(root string, entry ruleCacheEntry) bool {
	st, err := os.Stat(permissionFilePath(root))
	if err != nil {
		return true
	}
	return !st.ModTime().Equal(entry.modTime) || st.Size() != entry.size
}

// AllowMatch 判断一次工具调用是否命中 allow 规则（项目根取 ProjectRoot）。
func AllowMatch(toolName, argsString string) bool {
	return allowMatch(ProjectRoot(), toolName, argsString)
}

func allowMatch(root, toolName, argsString string) bool {
	if root == "" {
		return false
	}
	permMu.Lock()
	entry, ok := ruleCache[root]
	if !ok || ruleCacheStale(root, entry) {
		entry = loadCompiledRules(root)
		ruleCache[root] = entry
	}
	permMu.Unlock()
	for _, r := range entry.rules {
		if r.toolName != toolName {
			continue
		}
		if r.re == nil || r.re.MatchString(argsString) {
			return true
		}
	}
	return false
}

// ── 追加与写盘 ──

// jsonMarshalNoEscape 序列化 JSON，禁用 Go 默认的 HTML 转义（<、>、& 不再写成
// \u003c、\u003e、\u0026）。规则里常见 `> /dev/null`、`2>&1`，转义后既难读也难对齐
// indent 非空时按该缩进格式化（对齐 json.MarshalIndent）；Encode 的尾随换行被去掉。
func jsonMarshalNoEscape(v any, indent string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if indent != "" {
		enc.SetIndent("", indent)
	}
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// AddAllowRule 追加一条 allow 规则并写盘（项目根取 ProjectRoot）。
// 保留文件其他顶层键，只 patch permissions.allow；规则去重；失败返回 error。
func AddAllowRule(toolName, argsString string) error {
	return addAllowRule(ProjectRoot(), toolName, argsString)
}

func addAllowRule(root, toolName, argsString string) error {
	if root == "" {
		return fmt.Errorf("permissions: project root unavailable")
	}
	rule := ruleString(toolName, argsString)
	if rule == "" {
		return fmt.Errorf("permissions: empty rule")
	}

	permMu.Lock()
	defer permMu.Unlock()

	path := permissionFilePath(root)
	raw := map[string]json.RawMessage{}
	if data, err := os.ReadFile(path); err == nil {
		// 损坏则忽略，从空重建；保留其他顶层键。
		_ = json.Unmarshal(data, &raw)
	}
	allow := []string{}
	if p, ok := raw["permissions"]; ok {
		var perms struct {
			Allow []string `json:"allow"`
		}
		_ = json.Unmarshal(p, &perms)
		allow = perms.Allow
	}
	allow = appendUnique(allow, rule)

	allowJSON, err := jsonMarshalNoEscape(map[string]any{"allow": allow}, "")
	if err != nil {
		return fmt.Errorf("permissions: marshal allow: %w", err)
	}
	raw["permissions"] = allowJSON

	out, err := jsonMarshalNoEscape(raw, "\t")
	if err != nil {
		return fmt.Errorf("permissions: marshal: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("permissions: mkdir: %w", err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("permissions: write: %w", err)
	}
	// 写盘后失效缓存：同秒同大小重写可能不刷新 mtime，主动删除保证下次读取重新加载。
	delete(ruleCache, root)
	return nil
}

func appendUnique(list []string, item string) []string {
	if slices.Contains(list, item) {
		return list
	}
	return append(list, item)
}
