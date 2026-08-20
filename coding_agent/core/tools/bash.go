package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	agent_core "github.com/tinyclue/tinyclue-code/coding_agent/core"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/job"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/prompt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/utils"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"
	core_type "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"github.com/tinyclue/tinyclue-code/config"
)

type BashTool struct {
	*ToolBase
}

func NewBashTool() *BashTool {
	return &BashTool{
		ToolBase: &ToolBase{
			deferred:   false,
			searchHint: "execute shell commands",
		},
	}
}

func (bt *BashTool) Name() string {
	return core_type.BASH_TOOL_NAME
}

var dangerousCommands = []struct {
	keyword string
	reason  string
}{
	{"rm -rf /", "Recursive force delete on root filesystem"},
	{"rm -rf --no-preserve-root", "Recursive force delete without root protection"},
	{"sudo ", "Command runs with superuser privileges"},
	{"dd if=", "Low-level disk write operation"},
	{"mkfs.", "Filesystem creation operation"},
	{"fdisk ", "Disk partition operation"},
	{"mkswap ", "Swap partition operation"},
	{"chmod 777 /", "Permission change on root"},
	{"chown /", "Ownership change on root"},
	{":(){", "Fork bomb detected"},
	{"> /dev/sd", "Direct block device write"},
	{"wget |bash", "Piped web download to shell"},
	{"curl |bash", "Piped web download to shell"},
	{"eval ", "Dynamic code evaluation"},
	{"ls ", "Test ls Command"},
}

// isCompoundCommand 判断命令是否为复合命令：含顶层 &&、||、;、|、&、换行、
// 前缀/通配 allow 规则不自动放行复合命令，防止 `rm -rf /` 的规则被
// `rm -rf / && evil` 静默绕过。保守判定（多判为复合 → 多问一次）更安全。
func isCompoundCommand(cmd string) bool {
	var quote byte // 0 / '\'' / '"'
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			}
			continue
		case '"':
			if c == '\\' && i+1 < len(cmd) {
				i++
				continue
			}
			if c == '"' {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '\n', '&', '|', ';', '`':
			return true
		case '$':
			if i+1 < len(cmd) && cmd[i+1] == '(' {
				return true
			}
		}
	}
	return false
}

// matchDangerousCommand 在命令中查找危险关键字，命中返回原因（BeforeToolCall 检查与
// BashRuleContent 提取共用，避免两处各自维护危险子串逻辑）。
func matchDangerousCommand(cmd string) (reason string, ok bool) {
	cmdLower := strings.ToLower(cmd)
	for _, dc := range dangerousCommands {
		if strings.Contains(cmdLower, strings.ToLower(dc.keyword)) {
			return dc.reason, true
		}
	}
	return "", false
}

// BashRuleContent 计算 Bash 工具"不再二次询问"要存储的规则内容。
// getSimpleCommandPrefix：复合命令存子命令前缀，避免把 `cd /tmp && git pull` 存成
// 整条永不匹配的死规则。危险子命令整体存储、绝不宽化。规则：
//   - 非复合命令 → 原文（前缀规则按原样落盘，现状不变）；
//   - 复合命令 → 取首个顶层子命令 → 去掉尾部重定向：
//     危险 → 整体存储（`sudo rm -rf / && x` → `sudo rm -rf /`，绝不存 `sudo *`）；
//     否则 → 跳过头部的环境变量赋值，取 ≤2 token 的子命令前缀
//     `git commit -m "x" && git push` → `git commit`）；
//     前缀为空（如子命令以重定向开头）→ 回落整个子命令。
func BashRuleContent(command string) string {
	if !isCompoundCommand(command) {
		return command
	}
	sub := firstTopLevelSubcommand(command)
	sub = stripTrailingRedirections(sub)
	if sub == "" {
		return command // 保守：解析不出子命令 → 整体存储
	}
	if _, dangerous := matchDangerousCommand(sub); dangerous {
		return sub // 危险子命令整体存储，绝不宽化为 `sudo *` 之类
	}
	sub = stripEnvAssigns(sub)
	if prefix := subcommandPrefix(sub); prefix != "" {
		return prefix
	}
	return sub
}

// firstTopLevelSubcommand 提取命令的第一个顶层子命令（到第一个顶层分隔符为止），
// 去首尾空白。引号/反引号/$( ) 内内容整体跳过；2>&1 / <& 的 & 是重定向算子不算分隔符。
func firstTopLevelSubcommand(cmd string) string {
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch quote {
		case '\'':
			if c == '\'' {
				quote = 0
			}
			continue
		case '"':
			if c == '\\' && i+1 < len(cmd) {
				i++
				continue
			}
			if c == '"' {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '`':
			if j := skipBacktick(cmd, i); j > i {
				i = j
			}
		case '$':
			if i+1 < len(cmd) && cmd[i+1] == '(' {
				if j := skipCommandSubst(cmd, i); j > i {
					i = j
				}
			}
		case '&':
			if i > 0 && (cmd[i-1] == '>' || cmd[i-1] == '<') {
				continue
			}
			return strings.TrimSpace(cmd[:i])
		case '|', ';', '\n':
			return strings.TrimSpace(cmd[:i])
		}
	}
	return strings.TrimSpace(cmd)
}

// skipBacktick 返回反引号对闭合下标（含），找不到返回 -1（反引号命令替换不嵌套）。
func skipBacktick(cmd string, i int) int {
	for j := i + 1; j < len(cmd); j++ {
		if cmd[j] == '`' {
			return j
		}
	}
	return -1
}

// skipCommandSubst 返回 $( ... ) 匹配的右括号下标（含），支持嵌套与引号，找不到返回 -1。
func skipCommandSubst(cmd string, i int) int {
	var q byte
	depth := 1 // 已进入 $( 的外层
	for j := i + 2; j < len(cmd); j++ {
		c := cmd[j]
		switch q {
		case '\'':
			if c == '\'' {
				q = 0
			}
			continue
		case '"':
			if c == '\\' && j+1 < len(cmd) {
				j++
				continue
			}
			if c == '"' {
				q = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			q = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return j
			}
		}
	}
	return -1
}

// redirTailRe 匹配"重定向算子与目标粘连"的尾部 token：2>/dev/null、>x、2>&1、>>log、<in、<<EOF。
var redirTailRe = regexp.MustCompile(`^[0-9]*[<>][<>]?&?[^ \t]*$`)

// redirOpRe 匹配独立的重定向算子 token（目标在下一 token）：>、>>、<、<<、2>、2>&1、&>。
var redirOpRe = regexp.MustCompile(`^(?:[0-9]*[<>][<>]?&?[0-9]?|&>)$`)

// stripTrailingRedirections 去掉子命令尾部的重定向（> file、2>/dev/null、< in、<<EOF、
// 2>&1 等），便于提取可复用的命令前缀。保守：只剥能明确识别为重定向的尾部 token。
func stripTrailingRedirections(s string) string {
	fields := strings.Fields(s)
	for len(fields) > 0 {
		last := fields[len(fields)-1]
		if redirTailRe.MatchString(last) {
			fields = fields[:len(fields)-1]
			continue
		}
		if len(fields) >= 2 && redirOpRe.MatchString(fields[len(fields)-2]) {
			fields = fields[:len(fields)-2]
			continue
		}
		break
	}
	return strings.Join(fields, " ")
}

// envAssignRe 匹配环境变量赋值前缀：FOO=bar。
var envAssignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripEnvAssigns 跳过头部的环境变量赋值（VAR=val cmd … → cmd …）。
func stripEnvAssigns(s string) string {
	fields := strings.Fields(s)
	i := 0
	for i < len(fields) && envAssignRe.MatchString(fields[i]) {
		i++
	}
	return strings.Join(fields[i:], " ")
}

// 小写字母开头的 [a-z0-9-] 串（go、npm、go-build），排除 -flag 与路径等参数。
var bashSubcommandRe = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*$`)

// subcommandPrefix 取 ≤2 token 的子命令前缀：
// 前两个 token 都符合子命令格式 → "cmd sub"；仅第一个符合 → 单 token；否则空（回落整个子命令）。
func subcommandPrefix(s string) string {
	fields := strings.Fields(s)
	if len(fields) >= 2 && bashSubcommandRe.MatchString(fields[0]) && bashSubcommandRe.MatchString(fields[1]) {
		return fields[0] + " " + fields[1]
	}
	if len(fields) >= 1 && bashSubcommandRe.MatchString(fields[0]) {
		return fields[0]
	}
	return ""
}

func (bt *BashTool) BeforeToolCall(_ context.Context, toolUseContext core_type.ToolUseContext) core_type.BeforeToolCallResult {
	cmd, _ := toolUseContext.ToolCall.Arguments["command"].(string)
	if cmd == "" {
		return core_type.BeforeToolCallResult{Action: core_type.BeforeToolCallAllow}
	}
	// 命中项目 allow 规则（用户选过 "不再二次询问"）则免确认；
	// 放在危险命令检查之前：显式 allow 过的危险命令不再询问。
	// 前缀规则，绝不能静默放行 `rm -rf / && evil`。
	if !isCompoundCommand(cmd) && config.AllowMatch(core_type.BASH_TOOL_NAME, cmd) {
		return core_type.BeforeToolCallResult{Action: core_type.BeforeToolCallAllow}
	}
	if reason, ok := matchDangerousCommand(cmd); ok {
		return core_type.BeforeToolCallResult{
			Action:  core_type.BeforeToolCallAsk,
			Message: "Dangerous command detected: " + reason + "\nCommand: " + cmd,
		}
	}
	return core_type.BeforeToolCallResult{Action: core_type.BeforeToolCallAllow}
}

func (bt *BashTool) GetTool() apitypes.Tool {
	result := apitypes.Tool{
		Name:        core_type.BASH_TOOL_NAME,
		Description: prompt.GetBashSimplePrompt(),
		Parameters: apitypes.Parameters{
			Type: "object",
			Required: []string{
				"command",
			},
			Properties: map[string]apitypes.SchemaItem{
				"command": {
					Type:        "string",
					Description: "Bash command to execute",
				},
				"timeout": {
					Type:        "number",
					Description: "Optional timeout in milliseconds (max 600000)",
				},
				"run_in_background": {
					Type:        "boolean",
					Description: "Set to true to run this command in the background. Use Read to read the output later.",
				},
			},
		},
		Strict: true,
	}
	return result
}

const (
	bashMaxOutputLines   = 2000
	bashMaxOutputBytes   = 50 * 1024 // 50KB
	bashUpdateThrottleMs = 100       // 100ms
	bashReadBufSize      = 32 * 1024
)

func (bt *BashTool) Execute(ctx context.Context, toolUseContext core_type.ToolUseContext) (core_type.ToolContext, error) {
	toolCall := toolUseContext.ToolCall
	command, _ := toolCall.Arguments["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		emitProgress(toolUseContext, "Error: command is required")
		return bt.ErrorReturn(toolCall, fmt.Errorf("Error: command is required"))
	}

	// 后台执行：立即返回，输出写入任务文件，完成后发通知
	if runInBackground, _ := toolCall.Arguments["run_in_background"].(bool); runInBackground {
		return bt.runInBackground(toolUseContext, command)
	}

	timeoutSec := 0
	if v, ok := toolCall.Arguments["timeout"]; ok {
		if f, ok := v.(float64); ok {
			timeoutSec = int(f)
		}
	}

	output := NewOutputCollector(bashMaxOutputLines, bashMaxOutputBytes, "tinyclue-bash")

	// ── 创建命令 + 管道 ──
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("StdoutPipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("StderrPipe: %w", err))
	}

	if err := cmd.Start(); err != nil {
		return bt.ErrorReturn(toolCall, fmt.Errorf("Start: %w", err))
	}

	// ── 超时 ──
	var timedOut bool
	if timeoutSec > 0 {
		time.AfterFunc(time.Duration(timeoutSec)*time.Millisecond, func() {
			timedOut = true
			if cmd.Process != nil {
				cmd.Process.Kill()
			}
			stdout.Close()
			stderr.Close()
		})
	}

	// ── 节流式更新 goroutine ──
	stopUpdate := make(chan struct{})
	var updateWg sync.WaitGroup
	updateWg.Add(1)
	go func(wg *sync.WaitGroup) {
		defer wg.Done()
		ticker := time.NewTicker(bashUpdateThrottleMs * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if delta := output.ReadNew(); delta != "" {
					emitProgress(toolUseContext, delta)
				}
			case <-stopUpdate:
				return
			}
		}
	}(&updateWg)

	// ── 流式读取 stdout / stderr ──
	var readWg sync.WaitGroup
	readWg.Add(2)

	go readPipe(stdout, output, &readWg)
	go readPipe(stderr, output, &readWg)

	// 等待所有读取结束（管道在子进程退出后返回 EOF，或被超时关闭）
	readWg.Wait()

	// 停止更新 goroutine
	close(stopUpdate)
	updateWg.Wait()

	// 获取退出码
	execErr := cmd.Wait()

	// 最终化输出
	output.Finish()

	// ── 超时错误 ──
	if timedOut {
		errMsg := fmt.Sprintf("Error: command timed out after %ds", timeoutSec)
		emitProgress(toolUseContext, errMsg)
		return bt.ErrorReturn(toolCall, fmt.Errorf("bash: %s", errMsg))
	}

	// ── 最终 Snapshot ──
	snap := output.Snapshot(true)
	defer output.CloseTempFile()

	// ── 构建最终文本 ──
	var buf strings.Builder
	if snap.Content != "" {
		buf.WriteString(snap.Content)
	}

	// 截断警告
	if snap.Kind != utils.TruncationNone {
		startLine := snap.TotalLines - snap.OutputLines + 1
		endLine := snap.TotalLines

		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}

		if snap.Kind == utils.TruncationLastLinePartial {
			lastLineSize := utils.FormatSize(output.LastLineBytes())
			fmt.Fprintf(&buf,
				"[Showing last %s of line %d (line is %s). Full output: %s]",
				utils.FormatSize(snap.OutputBytes), endLine, lastLineSize, snap.FullOutputPath)
		} else if snap.Kind == utils.TruncationLines {
			fmt.Fprintf(&buf,
				"[Showing lines %d-%d of %d (line limit). Full output: %s]",
				startLine, endLine, snap.TotalLines, snap.FullOutputPath)
		} else {
			fmt.Fprintf(&buf,
				"[Showing lines %d-%d of %d (%s limit). Full output: %s]",
				startLine, endLine, snap.TotalLines,
				utils.FormatSize(bashMaxOutputBytes), snap.FullOutputPath)
		}
	}

	// exec 错误追加到输出
	if execErr != nil {
		if buf.Len() > 0 {
			buf.WriteByte('\n')
		}
		fmt.Fprintf(&buf, "Error: %v", execErr)
	}

	// 退出码（仅记录，输出文本不受影响，保持与改前一致）
	exitCode := 0
	if execErr != nil {
		var exitErr *exec.ExitError
		if errors.As(execErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}

	text := strings.TrimRight(buf.String(), "\n")

	return core_type.ToolContext{
		ToolCall: toolCall,
		Content: core_type.BashTaskContent{
			Status:    string(core_type.JobStatusCompleted),
			Command:   command,
			ToolUseId: toolCall.ID,
			ExitCode:  exitCode,
			Text:      text,
			Details: map[string]any{
				"snap": snap,
			},
		},
	}, nil
}

func (bt *BashTool) BuildToolResult(toolContext core_type.ToolContext) apitypes.ToolResultMessage {
	// 仅 err==nil 时被调用（agent_loop），Bash Execute 成功路径只返回 BashTaskContent。
	data := toolContext.Content.(core_type.BashTaskContent)
	text := data.Text
	if data.Status == "async_launched" {
		// 后台启动即返回：任务 ID + 输出文件路径 + 完成通知说明。
		text = fmt.Sprintf("Command running in background with ID: %s\nOutput is being written to: %s\nYou will be notified when it completes.",
			data.TaskId, data.OutputFilePath)
	}
	return bt.BuildToolResultByText(text, toolContext)
}

// 从 io.ReadCloser 流式读取数据到 OutputCollector。
func readPipe(pipe io.ReadCloser, output *OutputCollector, wg *sync.WaitGroup) {
	defer wg.Done()
	defer pipe.Close()

	buf := make([]byte, bashReadBufSize)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			d := make([]byte, n)
			copy(d, buf[:n])
			output.Append(d)
		}
		if err != nil {
			return
		}
	}
}

// ── 后台执行（run_in_background） ──

// runInBackground 后台执行命令：
//   - 立即返回（不阻塞工具调用）
//   - 独立 ctx 驱动命令生命周期（Execute 返回后工具 ctx 即被取消）
//   - 输出持续写入任务输出文件（后续由模型用 Read 工具读取）
//   - 命令结束 → 更新任务状态 → 发送 task-notification 唤醒主 agent
func (bt *BashTool) runInBackground(toolUseContext core_type.ToolUseContext, command string) (core_type.ToolContext, error) {
	toolCall := toolUseContext.ToolCall

	agentId := ""
	if toolUseContext.AgentUseContext != nil {
		agentId = toolUseContext.AgentUseContext.AgentId
	}

	taskId := utils.CreateAgentId("")
	outputFilePath := job.GetJobOutputPath(taskId)

	// 独立 ctx：命令生命周期不受工具 Execute 返回影响
	bgCtx, bgCancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(bgCtx, "bash", "-c", command)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		bgCancel()
		return bt.ErrorReturn(toolCall, fmt.Errorf("StdoutPipe: %w", err))
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		bgCancel()
		return bt.ErrorReturn(toolCall, fmt.Errorf("StderrPipe: %w", err))
	}
	if err := cmd.Start(); err != nil {
		bgCancel()
		return bt.ErrorReturn(toolCall, fmt.Errorf("Start: %w", err))
	}

	// abortFunc 由 Ctrl+C（AbortAll → Job Abort）触发：置 aborted 标记并杀进程，
	// 便于后续把被中止的命令标记为 JobStatusKilled 而非 JobStatusFailed。
	var aborted int32
	abortFunc := func() {
		atomic.StoreInt32(&aborted, 1)
		bgCancel()
	}

	jobData := &core_type.LocalBashJobState{
		Command:   command,
		ToolUseId: toolCall.ID,
		ExitCode:  0,
		IsAsync:   true,
	}
	job.JobInstance.Register(taskId, core_type.LOCAL_BASH, "bash: "+command, abortFunc, agentId, jobData)
	job.JobInstance.Update(taskId, func(t *core_type.JobState) {
		t.Status = core_type.JobStatusRunning
	})

	// 最近行缓冲 + 节流更新：把输出进度写入任务状态，驱动 UI 实时渲染。
	ring := newRecentLines(5)
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(bashUpdateThrottleMs * time.Millisecond)
		defer ticker.Stop()
		var last string
		for {
			select {
			case <-ticker.C:
				lines := ring.snapshot()
				if joined := strings.Join(lines, "\n"); joined != last {
					last = joined
					updateBashProgress(taskId, lines)
				}
			case <-done:
				return
			}
		}
	}()

	go func() {
		defer close(done) // 所有退出路径都关闭 done，保证节流 goroutine 不泄漏
		if err := os.MkdirAll(filepath.Dir(outputFilePath), 0755); err != nil {
			bgCancel()
			completeBashTask(taskId, core_type.JobStatusFailed, -1,
				fmt.Sprintf("create output dir: %v", err), command, toolCall.ID, outputFilePath)
			return
		}
		f, err := os.Create(outputFilePath)
		if err != nil {
			bgCancel()
			completeBashTask(taskId, core_type.JobStatusFailed, -1,
				fmt.Sprintf("create output file: %v", err), command, toolCall.ID, outputFilePath)
			return
		}

		// 先读完管道再 Wait，避免子进程写满管道阻塞
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			writePipeTracked(stdout, f, ring)
		}()
		go func() {
			defer wg.Done()
			writePipeTracked(stderr, f, ring)
		}()
		wg.Wait()
		ring.flush()
		f.Close()

		execErr := cmd.Wait()
		updateBashProgress(taskId, ring.snapshot())

		status := core_type.JobStatusCompleted
		exitCode := 0
		if execErr != nil {
			if atomic.LoadInt32(&aborted) == 1 {
				// 被用户中止（Ctrl+C）：进程由 bgCancel 杀死
				status = core_type.JobStatusKilled
				exitCode = -1
			} else {
				status = core_type.JobStatusFailed
				exitCode = -1
				var exitErr *exec.ExitError
				if errors.As(execErr, &exitErr) {
					exitCode = exitErr.ExitCode()
				}
			}
		}
		completeBashTask(taskId, status, exitCode, "", command, toolCall.ID, outputFilePath)
	}()

	content := core_type.BashTaskContent{
		Status:         "async_launched",
		TaskId:         taskId,
		Command:        command,
		ToolUseId:      toolCall.ID,
		OutputFilePath: outputFilePath,
		IsAsync:        true,
	}
	return core_type.ToolContext{
		ToolCall: toolCall,
		Content:  content,
	}, nil
}

// writePipeTracked 将 pipe 数据流式写入输出文件（多 goroutine 并发写同一 *os.File 是安全的），
// 并同步到最近行缓冲（供 UI 实时渲染）。
func writePipeTracked(pipe io.ReadCloser, f *os.File, ring *recentLines) {
	defer pipe.Close()
	buf := make([]byte, bashReadBufSize)
	for {
		n, err := pipe.Read(buf)
		if n > 0 {
			f.Write(buf[:n])
			ring.append(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

// recentLines 保留最近 N 行输出，跨 chunk 拼行，供后台 bash 任务进度渲染。
type recentLines struct {
	mu    sync.Mutex
	n     int
	lines []string
	buf   []byte // 未完成的当前行
}

func newRecentLines(n int) *recentLines {
	return &recentLines{n: n}
}

func (r *recentLines) append(data []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, data...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			break
		}
		r.lines = append(r.lines, string(r.buf[:i]))
		r.buf = r.buf[i+1:]
		if len(r.lines) > r.n {
			r.lines = r.lines[len(r.lines)-r.n:]
		}
	}
}

// flush 交付最后一行未以换行结尾的内容。
func (r *recentLines) flush() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) > 0 {
		r.lines = append(r.lines, string(r.buf))
		r.buf = nil
		if len(r.lines) > r.n {
			r.lines = r.lines[len(r.lines)-r.n:]
		}
	}
}

func (r *recentLines) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.lines))
	copy(out, r.lines)
	return out
}

// updateBashProgress 将后台 bash 最近输出行写入任务进度，触发 UI 重渲染。
func updateBashProgress(taskId string, lines []string) {
	if len(lines) == 0 {
		return
	}
	job.JobInstance.Update(taskId, func(t *core_type.JobState) {
		if data, ok := t.Data.(*core_type.LocalBashJobState); ok {
			data.Progress.RecentLines = lines
		}
	})
}

// completeBashTask 将后台 bash 任务标记为终态并发送完成通知。
// 顺序与异步 agent 一致：先置状态 → 发布通知 → 再置 Notified，保证等待循环不提前退出。
func completeBashTask(taskId string, status core_type.JobStatus, exitCode int, errMsg, command, toolUseId, outputFilePath string) {
	job.JobInstance.Update(taskId, func(t *core_type.JobState) {
		t.Status = status
		t.Error = errMsg
		if data, ok := t.Data.(*core_type.LocalBashJobState); ok {
			data.ExitCode = exitCode
			data.Result = core_type.BashTaskContent{
				Status:         string(status),
				TaskId:         taskId,
				Command:        command,
				ToolUseId:      toolUseId,
				ExitCode:       exitCode,
				OutputFilePath: outputFilePath,
				DurationMs:     int(time.Since(t.StartTime).Milliseconds()),
				IsAsync:        true,
				Error:          errMsg,
			}
		}
	})
	bashTaskNotification(taskId, toolUseId, outputFilePath, status, exitCode, command)
	job.JobInstance.Update(taskId, func(t *core_type.JobState) {
		t.Notified = true
	})
}

// bashTaskNotification 将后台 bash 完成通知发布到 AgentNotificationQueue，
// 由 agent_interactive 订阅后 PushDrainQueue 注入为主 agent 的 user 消息。
func bashTaskNotification(taskId, toolUseId, outputFilePath string, status core_type.JobStatus, exitCode int, command string) {
	taskNotificationXml := BuildBashTaskNotificationXml(taskId, toolUseId, outputFilePath, status, exitCode, command)
	userMsg := apitypes.UserMessage{
		Role:      apitypes.UserRole,
		Text:      taskNotificationXml,
		CreatedAt: time.Now(),
	}
	agent_core.Publish(agent_core.AgentNotificationQueue, core_type.NewEventMessage(core_type.TaskNotification, userMsg, nil))
}

// BuildBashTaskNotificationXml 生成后台 bash 任务完成通知的 XML 文本，
// 对齐参考项目的 task-notification 结构（无 agent 的 result/usage 段）。
func BuildBashTaskNotificationXml(taskId, toolUseId, outputFilePath string, status core_type.JobStatus, exitCode int, command string) string {
	summary := fmt.Sprintf("Background command %q %s", command, bashStatusText(status))
	if status == core_type.JobStatusCompleted || status == core_type.JobStatusFailed {
		summary += fmt.Sprintf(" (exit code %d)", exitCode)
	}
	message := "<task-notification>"
	message += "\n<task-id>" + taskId + "</task-id>"
	message += "\n<tool-use-id>" + toolUseId + "</tool-use-id>"
	message += "\n<output-file>" + outputFilePath + "</output-file>"
	message += "\n<status>" + string(status) + "</status>"
	message += "\n<summary>" + summary + "</summary>"
	message += "\n</task-notification>"
	return message
}

// bashStatusText 将任务状态转为通知摘要里的人类可读描述。
func bashStatusText(status core_type.JobStatus) string {
	switch status {
	case core_type.JobStatusCompleted:
		return "completed"
	case core_type.JobStatusFailed:
		return "failed"
	case core_type.JobStatusKilled:
		return "was killed"
	}
	return string(status)
}
