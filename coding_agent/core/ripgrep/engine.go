package ripgrep

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ──────────────────────────── Public API ────────────────────────────

// RipGrepResult 是 ripgrep 执行的返回结果，行格式与 rg 命令行输出一致。
type RipGrepResult struct {
	Lines      []string // stdout 行
	ExitCode   int      // 0=有匹配, 1=无匹配
	Truncated  bool     // 结果因截断而不完整（超时/缓冲区溢出）
	DurationMs int64
}

// RipGrepError 包装 rg 执行失败的信息，符合 ExecFileException 语义。
type RipGrepError struct {
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *RipGrepError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("ripgrep: exit code %d: %v (stderr: %s)", e.ExitCode, e.Cause, e.Stderr)
	}
	return fmt.Sprintf("ripgrep: exit code %d (stderr: %s)", e.ExitCode, e.Stderr)
}

func (e *RipGrepError) Unwrap() error { return e.Cause }

const maxBufferSize = 20_000_000 // 20MB，与 TS MAX_BUFFER_SIZE 一致

// RipGrep 执行 ripgrep 搜索。
// args: rg 命令行参数列表（如 "-i", "--glob", "*.go", "pattern"），不含搜索路径
// searchDir: 搜索目标路径
// 返回值 lines 的格式与 rg 标准输出一致，由调用方按需解析。
func RipGrep(ctx context.Context, args []string, searchDir string) (*RipGrepResult, error) {
	start := time.Now()

	opts, err := parseArgs(args, searchDir)
	if err != nil {
		return nil, &RipGrepError{ExitCode: 2, Stderr: err.Error(), Cause: err}
	}

	result := &RipGrepResult{}

	// --files 模式：只列出文件
	if opts.filesMode {
		lines := listFiles(ctx, opts)
		result.Lines = lines
		result.ExitCode = 0
		if len(lines) == 0 {
			result.ExitCode = 1
		}
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	// 搜索模式
	re, err := opts.compile()
	if err != nil {
		return nil, &RipGrepError{ExitCode: 2, Stderr: err.Error(), Cause: err}
	}

	lines := searchContent(ctx, re, opts)
	result.Lines = lines
	if len(lines) == 0 {
		result.ExitCode = 1
	}
	result.DurationMs = time.Since(start).Milliseconds()
	return result, nil
}

// ──────────────────────────── 参数解析 ────────────────────────────

type rgOptions struct {
	// 模式
	filesMode        bool // --files
	filesWithMatches bool // -l（由调用方在 args 中传递，但我们自动检测 output_mode）
	countMode        bool // -c

	// 模式与正则
	pattern         string
	caseInsensitive bool // -i
	multiline       bool // --multiline / -U
	caseSensitive   bool // -s / --case-sensitive（覆盖 -i）

	// 输出格式
	lineNumber   bool // -n
	withFilename bool // -H（多文件时默认 true）

	// 过滤
	globFilter []string // -g / --glob
	typeFilter []string // -t / --type
	maxDepth   int      // --max-depth
	maxCount   int      // -m / --max-count
	hidden     bool     // --hidden

	// 上下文
	ctxBefore int // -B
	ctxAfter  int // -A

	// 排序
	sortMode string // --sort modified, --sortr modified
	sortAsc  bool

	// 输出
	nullSep           bool   // -0 / --null
	pathSeparator     string // --path-separator
	maxColumns        int    // --max-columns（0 = 不限）
	maxColumnsPreview bool   // --max-columns-preview

	// 搜索路径
	searchDir string
	paths     []string // 额外的搜索路径（positional 路径参数）
}

func parseArgs(args []string, searchDir string) (*rgOptions, error) {
	opts := &rgOptions{
		searchDir:    searchDir,
		lineNumber:   true, // rg 默认 -n
		withFilename: true, // 多文件时默认显示文件名
		maxDepth:     -1,   // -1 表示不限深度
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		// 非 flag 参数 = pattern 或 path
		if !strings.HasPrefix(arg, "-") {
			// 第一个非 flag 且非 path 的参数是 pattern
			// 但 rg 中可以有多个非 flag 参数，如 `rg pattern path`
			if opts.pattern == "" {
				// 检查是否是路径（存在）
				if _, err := os.Stat(arg); err == nil {
					opts.paths = append(opts.paths, arg)
				} else {
					opts.pattern = arg
				}
			} else {
				opts.paths = append(opts.paths, arg)
			}
			i++
			continue
		}

		switch arg {
		case "--":
			// 剩余参数全部视为 pattern/path
			i++
			for i < len(args) {
				if opts.pattern == "" {
					opts.pattern = args[i]
				} else {
					opts.paths = append(opts.paths, args[i])
				}
				i++
			}
			continue

		case "-i", "--ignore-case":
			opts.caseInsensitive = true
		case "-s", "--case-sensitive":
			opts.caseSensitive = true
		case "-U", "--multiline-dotall":
			opts.multiline = true
		case "--multiline":
			opts.multiline = true

		case "-n", "--line-number":
			opts.lineNumber = true
		case "-N", "--no-line-number":
			opts.lineNumber = false
		case "-H", "--with-filename":
			opts.withFilename = true
		case "-I", "--no-filename":
			opts.withFilename = false

		case "-l", "--files-with-matches":
			opts.filesWithMatches = true
		case "-c", "--count":
			opts.countMode = true
		case "--files":
			opts.filesMode = true

		case "-0", "--null":
			opts.nullSep = true

		case "--hidden":
			opts.hidden = true

		case "--no-ignore", "--no-ignore-parent", "--no-ignore-vcs":
			// 纯 Go 实现不涉及 .gitignore，忽略即可

		case "-g", "--glob":
			i++
			if i < len(args) {
				opts.globFilter = append(opts.globFilter, args[i])
			}
		case "-t", "--type":
			i++
			if i < len(args) {
				opts.typeFilter = append(opts.typeFilter, args[i])
			}

		case "-C", "--context":
			i++
			if i < len(args) {
				opts.ctxBefore, _ = strconv.Atoi(args[i])
				opts.ctxAfter = opts.ctxBefore
			}
		case "-B", "--before-context":
			i++
			if i < len(args) {
				opts.ctxBefore, _ = strconv.Atoi(args[i])
			}
		case "-A", "--after-context":
			i++
			if i < len(args) {
				opts.ctxAfter, _ = strconv.Atoi(args[i])
			}

		case "--sort":
			if strings.Contains(args[i], "=") {
				parts := strings.SplitN(args[i], "=", 2)
				opts.sortMode = parts[1]
				opts.sortAsc = true
			} else {
				i++
				if i < len(args) {
					opts.sortMode = args[i]
					opts.sortAsc = true
				}
			}
		case "--sortr":
			if strings.Contains(args[i], "=") {
				parts := strings.SplitN(args[i], "=", 2)
				opts.sortMode = parts[1]
				opts.sortAsc = false
			} else {
				i++
				if i < len(args) {
					opts.sortMode = args[i]
					opts.sortAsc = false
				}
			}

		case "--max-depth":
			i++
			if i < len(args) {
				opts.maxDepth, _ = strconv.Atoi(args[i])
			}
		case "-m", "--max-count":
			i++
			if i < len(args) {
				opts.maxCount, _ = strconv.Atoi(args[i])
			}

		case "--path-separator":
			i++
			if i < len(args) {
				opts.pathSeparator = args[i]
			}

		case "-e":
			i++
			if i < len(args) {
				opts.pattern = args[i]
			}

		case "--max-columns":
			i++
			if i < len(args) {
				if n, err := strconv.Atoi(args[i]); err == nil && n > 0 {
					opts.maxColumns = n
				}
			}
		case "--max-columns-preview":
			opts.maxColumnsPreview = true

		case "--no-config", "-j":
			// 忽略：纯 Go 实现无需配置
			i++
			if arg == "-j" && i < len(args) {
				// 跳过线程数参数
			}
		default:
			// 不认识的 flag 就忽略
		}
		i++
	}

	return opts, nil
}

func (o *rgOptions) compile() (*regexp.Regexp, error) {
	pattern := o.pattern

	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}

	// 将 \w → [\p{L}\p{N}_], \W → [^\p{L}\p{N}_] 以支持 Unicode 单词字符
	pattern = unicodeW(pattern)

	if o.multiline {
		pattern = "(?s)" + pattern
	}
	// --case-sensitive 覆盖 -i
	if o.caseInsensitive && !o.caseSensitive {
		pattern = "(?i)" + pattern
	}

	return regexp.Compile(pattern)
}

// unicodeW 将 \w 替换为 [\p{L}\p{N}_], \W 替换为 [^\p{L}\p{N}_]。
// 跳过 \\w 和 \\W（字面反斜杠 + w/W，不是正则转义序列）。
func unicodeW(pattern string) string {
	var b strings.Builder
	b.Grow(len(pattern))
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			next := pattern[i+1]
			if next == '\\' {
				// 转义反斜杠 \\，保持原样并跳过下一个字符
				b.WriteByte('\\')
				b.WriteByte('\\')
				i++
				continue
			}
			if next == 'w' {
				b.WriteString("[\\p{L}\\p{N}_]")
				i++
				continue
			}
			if next == 'W' {
				b.WriteString("[^\\p{L}\\p{N}_]")
				i++
				continue
			}
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}

func (o *rgOptions) separator() string {
	if o.nullSep {
		return "\x00"
	}
	if o.pathSeparator != "" {
		return o.pathSeparator
	}
	return "/"
}

// ──────────────────────────── 文件列表模式 ────────────────────────────

func listFiles(ctx context.Context, opts *rgOptions) []string {
	var files []string

	walkFn := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return filepath.SkipAll
		default:
		}

		if d.IsDir() {
			if vcsDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		rel, err := filepath.Rel(opts.searchDir, path)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		// 隐藏文件过滤
		if !opts.hidden {
			for _, part := range strings.Split(rel, "/") {
				if strings.HasPrefix(part, ".") && part != "." && part != ".." {
					return nil
				}
			}
		}

		// glob 过滤
		for _, g := range opts.globFilter {
			if strings.HasPrefix(g, "!") {
				if matchGlob(rel, g[1:]) {
					return nil
				}
			} else {
				if !matchGlob(rel, g) {
					return nil
				}
			}
		}

		// type 过滤
		for _, t := range opts.typeFilter {
			if !fileMatchesType(path, t) {
				return nil
			}
		}

		// 最大深度
		if opts.maxDepth >= 0 {
			depth := len(strings.Split(rel, "/"))
			if depth > opts.maxDepth {
				return nil
			}
		}

		absPath := filepath.ToSlash(path)
		if opts.pathSeparator != "" && opts.pathSeparator != "/" {
			absPath = strings.ReplaceAll(absPath, "/", opts.pathSeparator)
		}
		files = append(files, absPath)
		return nil
	}

	filepath.WalkDir(opts.searchDir, walkFn)

	applyFileSort(files, opts)
	return files
}

// ──────────────────────────── 搜索模式 ────────────────────────────

func searchContent(ctx context.Context, re *regexp.Regexp, opts *rgOptions) []string {
	var outLines []string
	fileList := collectSearchFiles(ctx, opts)

	type span struct{ s, e int }

	for _, path := range fileList {
		select {
		case <-ctx.Done():
			return outLines
		default:
		}

		absPath := filepath.ToSlash(path)
		// 应用 pathSeparator
		if opts.pathSeparator != "" && opts.pathSeparator != "/" {
			absPath = strings.ReplaceAll(absPath, "/", opts.pathSeparator)
		}

		if opts.filesWithMatches {
			if fileHasMatch(path, re) {
				outLines = append(outLines, absPath)
			}
			continue
		}

		if opts.countMode {
			n := countMatches(path, re)
			if n > 0 {
				if opts.nullSep {
					if opts.withFilename {
						outLines = append(outLines, fmt.Sprintf("%s%c%d", absPath, 0, n))
					} else {
						outLines = append(outLines, fmt.Sprintf("%d", n))
					}
				} else {
					if opts.withFilename {
						outLines = append(outLines, fmt.Sprintf("%s:%d", absPath, n))
					} else {
						outLines = append(outLines, fmt.Sprintf("%d", n))
					}
				}
			}
			continue
		}

		// 默认 content 模式
		lines, err := readFileLines(path)
		if err != nil {
			continue
		}

		var matchSpans []span
		if opts.multiline {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			locs := re.FindAllIndex(data, -1)
			if len(locs) == 0 {
				continue
			}
			if opts.maxCount > 0 && len(locs) > opts.maxCount {
				locs = locs[:opts.maxCount]
			}
			for _, loc := range locs {
				startLine := bytes.Count(data[:loc[0]], []byte("\n"))
				endLine := bytes.Count(data[:loc[1]], []byte("\n"))
				matchSpans = append(matchSpans, span{s: startLine, e: endLine})
			}
		} else {
			matchNums := findMatchLines(lines, re)
			if len(matchNums) == 0 {
				continue
			}
			if opts.maxCount > 0 && len(matchNums) > opts.maxCount {
				matchNums = matchNums[:opts.maxCount]
			}
			for _, ln := range matchNums {
				matchSpans = append(matchSpans, span{s: ln, e: ln})
			}
		}

		// 记录原始匹配行号（用于上下文行分隔符）
		matchLines := make(map[int]bool)
		for _, sp := range matchSpans {
			for ln := sp.s; ln <= sp.e; ln++ {
				matchLines[ln] = true
			}
		}

		// 上下文合并
		for i := range matchSpans {
			s := matchSpans[i].s - opts.ctxBefore
			if s < 0 {
				s = 0
			}
			e := matchSpans[i].e + opts.ctxAfter
			if e >= len(lines) {
				e = len(lines) - 1
			}
			matchSpans[i].s = s
			matchSpans[i].e = e
		}
		sort.Slice(matchSpans, func(i, j int) bool { return matchSpans[i].s < matchSpans[j].s })
		var merged []span
		for _, sp := range matchSpans {
			if len(merged) > 0 && sp.s <= merged[len(merged)-1].e+1 {
				if sp.e > merged[len(merged)-1].e {
					merged[len(merged)-1].e = sp.e
				}
			} else {
				merged = append(merged, sp)
			}
		}

		if len(outLines) > 0 && (opts.ctxBefore > 0 || opts.ctxAfter > 0) {
			if !opts.nullSep {
				outLines = append(outLines, "--")
			}
		}

		for i, sp := range merged {
			if i > 0 && (opts.ctxBefore > 0 || opts.ctxAfter > 0) {
				if !opts.nullSep {
					outLines = append(outLines, "--")
				}
			}
			for ln := sp.s; ln <= sp.e; ln++ {
				line := lines[ln]
				if opts.maxColumns > 0 && len(line) > opts.maxColumns {
					if opts.maxColumnsPreview {
						line = line[:opts.maxColumns] + " [... omitted end of long line]"
					} else {
						line = "[Omitted long matching line]"
					}
				} else if len(line) > maxLineLength {
					line = line[:maxLineLength]
				}
				var output string
				isMatch := matchLines[ln]
				if opts.withFilename {
					if opts.lineNumber {
						if isMatch {
							output = fmt.Sprintf("%s:%d:%s", absPath, ln+1, line)
						} else {
							output = fmt.Sprintf("%s-%d-%s", absPath, ln+1, line)
						}
					} else {
						output = fmt.Sprintf("%s:%s", absPath, line)
					}
				} else {
					if opts.lineNumber {
						if isMatch {
							output = fmt.Sprintf("%d:%s", ln+1, line)
						} else {
							output = fmt.Sprintf("%d-%s", ln+1, line)
						}
					} else {
						output = line
					}
				}
				outLines = append(outLines, output)
			}
		}
	}

	return outLines
}

// collectSearchFiles 收集需要搜索的文件列表。
func collectSearchFiles(ctx context.Context, opts *rgOptions) []string {
	dirs := []string{opts.searchDir}
	if len(opts.paths) > 0 {
		dirs = opts.paths
	}

	var files []string
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			if isBinaryFile(dir) {
				continue
			}
			files = append(files, dir)
			continue
		}

		filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			select {
			case <-ctx.Done():
				return filepath.SkipAll
			default:
			}

			if d.IsDir() {
				if vcsDirs[d.Name()] {
					return filepath.SkipDir
				}
				return nil
			}

			rel, err := filepath.Rel(opts.searchDir, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)

			// 隐藏文件
			if !opts.hidden {
				for _, part := range strings.Split(rel, "/") {
					if strings.HasPrefix(part, ".") && part != "." && part != ".." {
						return nil
					}
				}
			}
			if isBinaryFile(path) {
				return nil
			}

			// glob 过滤（rg 语义：正 glob 为 OR，负 glob 为排除）
			var positiveGlobs, negativeGlobs []string
			for _, g := range opts.globFilter {
				if strings.HasPrefix(g, "!") {
					negativeGlobs = append(negativeGlobs, g[1:])
				} else {
					positiveGlobs = append(positiveGlobs, g)
				}
			}
			for _, g := range negativeGlobs {
				if matchGlob(rel, g) {
					return nil
				}
			}
			if len(positiveGlobs) > 0 {
				matched := false
				for _, g := range positiveGlobs {
					if matchGlob(rel, g) {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}

			// type 过滤
			for _, t := range opts.typeFilter {
				if !fileMatchesType(path, t) {
					return nil
				}
			}

			// 最大深度
			if opts.maxDepth >= 0 {
				depth := len(strings.Split(rel, "/"))
				if depth > opts.maxDepth {
					return nil
				}
			}

			files = append(files, path)
			return nil
		})
	}

	// 排序
	if opts.sortMode != "" {
		sort.Slice(files, func(i, j int) bool {
			fi, errI := os.Stat(files[i])
			fj, errJ := os.Stat(files[j])
			if errI != nil || errJ != nil {
				return files[i] < files[j]
			}
			var cmp bool
			switch opts.sortMode {
			case "modified":
				cmp = fi.ModTime().Before(fj.ModTime())
			default:
				cmp = files[i] < files[j]
			}
			if !opts.sortAsc {
				return !cmp
			}
			return cmp
		})
	} else {
		sort.Slice(files, func(i, j int) bool {
			return files[i] < files[j]
		})
	}

	if len(files) > maxGlobResults {
		files = files[:maxGlobResults]
	}

	return files
}

// applyFileSort 对文件列表应用排序（支持 --sort modified / --sortr modified）。
func applyFileSort(files []string, opts *rgOptions) {
	if opts.sortMode != "" {
		sort.Slice(files, func(i, j int) bool {
			fi, errI := os.Stat(files[i])
			fj, errJ := os.Stat(files[j])
			if errI != nil || errJ != nil {
				return files[i] < files[j]
			}
			var cmp bool
			switch opts.sortMode {
			case "modified":
				cmp = fi.ModTime().Before(fj.ModTime())
			default:
				cmp = files[i] < files[j]
			}
			if !opts.sortAsc {
				return !cmp
			}
			return cmp
		})
	} else {
		sort.Slice(files, func(i, j int) bool {
			return files[i] < files[j]
		})
	}
}

// ──────────────────────────── 内部工具函数 ────────────────────────────

var vcsDirs = map[string]bool{
	".git": true, ".svn": true, ".hg": true, ".bzr": true, ".jj": true, ".sl": true,
}

// isBinaryFile 检测文件是否为二进制（检查前 8KB 中是否有 NUL 字节）。
// 与 ripgrep 默认行为一致。
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 8192)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return false
	}
	return bytes.IndexByte(buf[:n], 0) >= 0
}

const (
	defaultHeadLimit = 250
	maxLineLength    = 500
	maxGlobResults   = 10000
)

// fileHasMatch 判断文件内容是否匹配正则。
func fileHasMatch(path string, re *regexp.Regexp) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		if re.Match(scanner.Bytes()) {
			return true
		}
	}
	return false
}

// readFileLines 读取文件并按行分割。
func readFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	content := string(bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n")))
	content = strings.TrimRight(content, "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

// findMatchLines 返回所有匹配行的行号（0-indexed）。
func findMatchLines(lines []string, re *regexp.Regexp) []int {
	var nums []int
	for i, line := range lines {
		if re.MatchString(line) {
			nums = append(nums, i)
		}
	}
	return nums
}

// countMatches 统计文件中的正则匹配总数。
func countMatches(path string, re *regexp.Regexp) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return len(re.FindAll(data, -1))
}

// ──────────────────────────── Glob 匹配 ────────────────────────────

// matchGlob 判断 path 是否匹配 glob pattern，支持 ** 和 {a,b}。
func matchGlob(path, pattern string) bool {
	pattern = filepath.ToSlash(pattern)
	path = filepath.ToSlash(path)

	if strings.ContainsAny(pattern, "{}") {
		for _, p := range expandBraces(pattern) {
			if matchGlob(path, p) {
				return true
			}
		}
		return false
	}

	if strings.Contains(pattern, "[!") {
		pattern = strings.ReplaceAll(pattern, "[!", "[^")
	}

	if !strings.Contains(pattern, "**") {
		if m, _ := filepath.Match(pattern, path); m {
			return true
		}
		if m, _ := filepath.Match(pattern, filepath.Base(path)); m {
			return true
		}
		return false
	}

	parts := strings.SplitN(pattern, "**", 2)
	prefix := strings.TrimRight(parts[0], "/")
	suffix := strings.TrimLeft(parts[1], "/")

	if prefix != "" {
		if !strings.HasPrefix(path, prefix) {
			return false
		}
		path = path[len(prefix):]
	}
	path = strings.TrimLeft(path, "/")

	if suffix == "" {
		return true
	}

	remainingParts := strings.Split(path, "/")
	for i := 0; i <= len(remainingParts); i++ {
		rest := strings.Join(remainingParts[i:], "/")
		if matchGlob(rest, suffix) {
			return true
		}
	}
	return false
}

// expandBraces 展开 {a,b} 花括号模式，支持嵌套。
func expandBraces(pattern string) []string {
	openIdx := strings.IndexByte(pattern, '{')
	if openIdx == -1 {
		return []string{pattern}
	}

	depth := 1
	closeIdx := -1
braceLoop:
	for i := openIdx + 1; i < len(pattern); i++ {
		switch pattern[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				closeIdx = i
				break braceLoop
			}
		}
	}
	if closeIdx == -1 {
		return []string{pattern}
	}

	content := pattern[openIdx+1 : closeIdx]
	var alternatives []string
	depth = 0
	start := 0
	for i := 0; i < len(content); i++ {
		switch content[i] {
		case '{':
			depth++
		case '}':
			depth--
		case ',':
			if depth == 0 {
				alternatives = append(alternatives, content[start:i])
				start = i + 1
			}
		}
	}
	alternatives = append(alternatives, content[start:])

	prefix := pattern[:openIdx]
	suffix := pattern[closeIdx+1:]
	var result []string
	for _, alt := range alternatives {
		result = append(result, expandBraces(prefix+alt+suffix)...)
	}
	return result
}

// fileMatchesGlob 判断文件是否匹配 glob 模式（支持逗号/空白分割多个模式）。
func fileMatchesGlob(filePath, globPattern string) bool {
	if globPattern == "" {
		return true
	}
	for _, raw := range strings.Fields(globPattern) {
		if strings.Contains(raw, "{") && strings.Contains(raw, "}") {
			if matchGlob(filePath, raw) {
				return true
			}
		} else {
			for _, p := range strings.Split(raw, ",") {
				p = strings.TrimSpace(p)
				if p == "" {
					continue
				}
				if matchGlob(filePath, p) {
					return true
				}
			}
		}
	}
	return false
}

// ──────────────────────────── Type 过滤 ────────────────────────────

var rgTypeToExts = map[string][]string{
	"js":   {".js", ".jsx", ".mjs", ".cjs"},
	"ts":   {".ts", ".tsx", ".mts", ".cts"},
	"py":   {".py"},
	"rust": {".rs"},
	"go":   {".go"},
	"java": {".java"},
	"c":    {".c", ".h"},
	"cpp":  {".cpp", ".cc", ".cxx", ".hpp", ".hh", ".hxx"},
	"rb":   {".rb"},
	"rs":   {".rs"},
	"sh":   {".sh", ".bash"},
	"json": {".json"},
	"yaml": {".yaml", ".yml"},
	"toml": {".toml"},
	"md":   {".md", ".mdx"},
	"html": {".html", ".htm"},
	"css":  {".css"},
	"txt":  {".txt"},
}

func fileMatchesType(filename, typeFilter string) bool {
	if typeFilter == "" {
		return true
	}
	exts, ok := rgTypeToExts[typeFilter]
	if !ok {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	return slices.Contains(exts, ext)
}
