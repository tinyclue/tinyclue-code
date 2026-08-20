package tools

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// OutputCollector 增量跟踪流式输出，使用有界内存。
//
// 工作方式：
//   - Append 接收原始字节 → 处理跨 chunk UTF-8 边界 → 追加到滚动 tailText
//   - 超过 maxRollingBytes*2 时自动修剪尾部
//   - 行/字节超限时惰性打开临时文件写入完整输出
//   - Snapshot 返回截断后的可见内容 + 统计
type OutputCollector struct {
	mu sync.Mutex

	maxLines        int
	maxBytes        int
	maxRollingBytes int
	tempFilePrefix  string

	// 滚动缓冲区（只保留最近 maxRollingBytes 字节供 Snapshot）
	tailText                 strings.Builder
	tailBytes                int
	tailStartsAtLineBoundary bool
	tailStart                int // tailText 起点在整个解码流中的偏移（trimTail 丢头时推进）

	// 流式消费游标：已通过 ReadNew 交付的解码流偏移，保证每字节至多交付一次。
	streamPos int

	// 全局统计
	totalRawBytes  int
	totalDecoded   int // 解码后的总字节数
	completedLines int
	totalLines     int
	currentLineLen int // 当前行字节数
	hasOpenLine    bool
	finished       bool

	// 跨 chunk 的未完成 UTF-8 字节
	leftover []byte

	// 临时文件
	tempFilePath string
	tempFile     *os.File
	rawBuf       bytes.Buffer
}

// NewOutputCollector 创建 OutputCollector。
func NewOutputCollector(maxLines, maxBytes int, tempFilePrefix string) *OutputCollector {
	if maxLines <= 0 {
		maxLines = 2000
	}
	if maxBytes <= 0 {
		maxBytes = 50 * 1024
	}
	if tempFilePrefix == "" {
		tempFilePrefix = "bash-output"
	}
	return &OutputCollector{
		maxLines:                 maxLines,
		maxBytes:                 maxBytes,
		maxRollingBytes:          maxBytes * 2,
		tempFilePrefix:           tempFilePrefix,
		tailStartsAtLineBoundary: true,
	}
}

// Append 追加原始字节。goroutine-safe。
func (oc *OutputCollector) Append(data []byte) {
	oc.mu.Lock()
	defer oc.mu.Unlock()

	if oc.finished {
		return
	}

	oc.totalRawBytes += len(data)

	// 拼接上一段残留的 UTF-8 字节
	if len(oc.leftover) > 0 {
		data = append(oc.leftover, data...)
		oc.leftover = nil
	}

	// 处理跨 chunk UTF-8 边界
	validLen := findValidUTF8Prefix(data)
	if validLen < len(data) {
		oc.leftover = make([]byte, len(data)-validLen)
		copy(oc.leftover, data[validLen:])
		data = data[:validLen]
	}

	if len(data) == 0 {
		return
	}

	text := string(data)
	oc.writeTail(text)
	oc.trackLines(text)

	if oc.tempFile != nil {
		oc.tempFile.Write(data)
	} else if oc.shouldUseTempFile() {
		oc.ensureTempFile()
		oc.tempFile.Write(data)
	} else {
		oc.rawBuf.Write(data)
	}
}

// Finish 结束累加，刷新残留字节。之后不再接受数据。
func (oc *OutputCollector) Finish() {
	oc.mu.Lock()
	defer oc.mu.Unlock()

	if oc.finished {
		return
	}
	oc.finished = true

	if len(oc.leftover) > 0 {
		oc.writeTail(string(oc.leftover))
		oc.trackLines(string(oc.leftover))
		oc.leftover = nil
	}
	if oc.shouldUseTempFile() {
		oc.ensureTempFile()
	}
}

// OutputSnapshot 是 Snapshot 返回的结果。
type OutputSnapshot struct {
	Content        string // 截断后的可见内容
	Kind           utils.TruncationKind
	TotalLines     int    // 原始总行数
	TotalBytes     int    // 原始总字节数
	OutputLines    int    // 截断后行数
	OutputBytes    int    // 截断后字节数
	FullOutputPath string // 完整输出的临时文件路径（仅截断时存在）
}

// Snapshot 对当前内容执行 tail-truncate，返回可见片段和统计。
// persist：如果发生截断，是否确保临时文件已创建。
func (oc *OutputCollector) Snapshot(persist bool) OutputSnapshot {
	oc.mu.Lock()
	snapText := oc.getSnapshotText()
	totalLines := oc.totalLines
	totalBytes := oc.totalDecoded
	maxLines := oc.maxLines
	maxBytes := oc.maxBytes
	fullPath := oc.tempFilePath
	oc.mu.Unlock()

	truncated := totalLines > maxLines || totalBytes > maxBytes

	var content string
	var outputLines, outputBytes int
	kind := utils.TruncationNone
	if truncated {
		r := utils.TruncateTail(snapText, maxLines, maxBytes)
		content = r.Content
		outputLines = r.OutputLines
		outputBytes = r.OutputBytes
		kind = r.Kind
	} else {
		content, outputLines, outputBytes = snapText, totalLines, totalBytes
	}

	if persist && truncated {
		oc.mu.Lock()
		oc.ensureTempFile()
		fullPath = oc.tempFilePath
		oc.mu.Unlock()
	}

	return OutputSnapshot{
		Content:        content,
		Kind:           kind,
		TotalLines:     totalLines,
		TotalBytes:     totalBytes,
		OutputLines:    outputLines,
		OutputBytes:    outputBytes,
		FullOutputPath: fullPath,
	}
}

// ReadNew 返回自上次调用以来新追加的解码文本（滚动窗口内），用于流式增量显示。
// 每个字节至多交付一次：若输出过快、滚动窗口已裁掉尚未交付的部分，
// 则跳过已丢弃的中间段、从窗口头继续，不会重发已显示内容。
func (oc *OutputCollector) ReadNew() string {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	if oc.streamPos < oc.tailStart {
		oc.streamPos = oc.tailStart
	}
	text := oc.tailText.String()[oc.streamPos-oc.tailStart:]
	oc.streamPos = oc.tailStart + len(text)
	return text
}

// CloseTempFile 关闭临时文件。
func (oc *OutputCollector) CloseTempFile() error {
	oc.mu.Lock()
	f := oc.tempFile
	oc.tempFile = nil
	oc.mu.Unlock()
	if f == nil {
		return nil
	}
	return f.Close()
}

// LastLineBytes 返回当前行的字节数。
func (oc *OutputCollector) LastLineBytes() int {
	oc.mu.Lock()
	defer oc.mu.Unlock()
	return oc.currentLineLen
}

// ── 内部方法 ──

// writeTail 将文本写入尾部滚动缓冲区。当缓冲区超过上限时自动修剪。
//
// 该函数只处理存储层面的事：
//   - 累加 totalDecoded（解码后总字节数）
//   - 写入 tailText（供 Snapshot 读取的滚动窗口）
//   - 超出 maxRollingBytes*2 时丢弃旧数据
func (oc *OutputCollector) writeTail(text string) {
	if len(text) == 0 {
		return
	}
	oc.totalDecoded += len(text)
	oc.tailText.WriteString(text)
	oc.tailBytes += len(text)
	if oc.tailBytes > oc.maxRollingBytes*2 {
		oc.trimTail()
	}
}

// trackLines 扫描文本中的换行符，更新行统计。
//
// 维护以下统计：
//   - completedLines：已完整结束的行数（以 \n 结尾）
//   - hasOpenLine：末尾是否有未关闭的行
//   - currentLineLen：当前（未关闭）行的字节数
//   - totalLines：总行数 = completedLines + (hasOpenLine ? 1 : 0)
func (oc *OutputCollector) trackLines(text string) {
	if len(text) == 0 {
		return
	}

	newlines := 0
	lastNewline := -1
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			newlines++
			lastNewline = i
		}
	}

	if newlines == 0 {
		oc.currentLineLen += len(text)
		oc.hasOpenLine = true
	} else {
		oc.completedLines += newlines
		tail := text[lastNewline+1:]
		oc.currentLineLen = len(tail)
		oc.hasOpenLine = len(tail) > 0
	}

	oc.totalLines = oc.completedLines
	if oc.hasOpenLine {
		oc.totalLines++
	}
}

func (oc *OutputCollector) trimTail() {
	buf := []byte(oc.tailText.String())
	if len(buf) <= oc.maxRollingBytes {
		oc.tailBytes = len(buf)
		return
	}

	start := len(buf) - oc.maxRollingBytes
	for start < len(buf) && (buf[start]&0xc0) == 0x80 {
		start++
	}

	oc.tailStart += start
	oc.tailStartsAtLineBoundary = start == 0 || buf[start-1] == '\n'
	oc.tailText.Reset()
	oc.tailText.Write(buf[start:])
	oc.tailBytes = oc.tailText.Len()
}

func (oc *OutputCollector) getSnapshotText() string {
	text := oc.tailText.String()
	if oc.tailStartsAtLineBoundary {
		return text
	}
	_, after, ok := strings.Cut(text, "\n")
	if !ok {
		return text
	}
	return after
}

func (oc *OutputCollector) shouldUseTempFile() bool {
	return oc.totalRawBytes > oc.maxBytes ||
		oc.totalDecoded > oc.maxBytes ||
		oc.totalLines > oc.maxLines
}

func (oc *OutputCollector) ensureTempFile() {
	if oc.tempFile != nil {
		return
	}

	id := make([]byte, 8)
	if _, err := rand.Read(id); err != nil {
		return
	}
	oc.tempFilePath = filepath.Join(os.TempDir(),
		fmt.Sprintf("%s-%s.log", oc.tempFilePrefix, hex.EncodeToString(id)))

	f, err := os.Create(oc.tempFilePath)
	if err != nil {
		oc.tempFilePath = ""
		return
	}
	oc.tempFile = f

	if oc.rawBuf.Len() > 0 {
		oc.tempFile.Write(oc.rawBuf.Bytes())
		oc.rawBuf.Reset()
	}
}

// findValidUTF8Prefix 返回 buf 中最长合法 UTF-8 前缀的长度。
func findValidUTF8Prefix(buf []byte) int {
	if utf8.Valid(buf) {
		return len(buf)
	}
	n := len(buf)
	for i := 1; i <= 4 && i < n; i++ {
		if utf8.Valid(buf[:n-i]) {
			return n - i
		}
	}
	for i := 0; i < n; {
		r, size := utf8.DecodeRune(buf[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return n
}
