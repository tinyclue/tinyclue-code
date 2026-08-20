// Package permission_render_repro 复现"Bash 工具权限面板 Deny 后渲染重叠"问题。
//
// 用真实组件（welcome/text/tool/permission/status_info/editor/footer）构建帧，
// 走真实 RendererV2 差分渲染 + 最小终端模拟器，重现：
//
//	Frame A 用户消息
//	Frame B 工具调用开始（chat 出现 ⏺ Bash(...)）
//	Frame C 权限面板弹出（status/editor/footer 隐藏）
//	Frame D Deny（面板隐藏、工具 detail 显示 denied、恢复 status/editor/footer）
//	Frame E 助手回复 + Finished
//
// 断言最终屏幕：无残留 "Tool Execution Confirmation"、无重复 Bash 块、用户消息可见。
package permission_render_repro

import (
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/tinyclue/tinyclue-code/tui/component"
	"github.com/tinyclue/tinyclue-code/tui/component/editor"
	"github.com/tinyclue/tinyclue-code/tui/component/footer"
	"github.com/tinyclue/tinyclue-code/tui/component/permission"
	"github.com/tinyclue/tinyclue-code/tui/component/status_info"
	"github.com/tinyclue/tinyclue-code/tui/component/text"
	"github.com/tinyclue/tinyclue-code/tui/component/tool"
	"github.com/tinyclue/tinyclue-code/tui/component/welcome"
	"github.com/tinyclue/tinyclue-code/tui/component/widget"
	"github.com/tinyclue/tinyclue-code/tui/core"
	"github.com/tinyclue/tinyclue-code/tui/renderer"
	"github.com/tinyclue/tinyclue-code/tui/terminal"
	"github.com/tinyclue/tinyclue-code/tui/types"
)

// ── stdout 捕获 ──

func capture(f func()) string {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	os.Stdout = w
	f()
	w.Close()
	os.Stdout = old
	b, _ := io.ReadAll(r)
	return string(b)
}

// ── 最小终端模拟器（同 renderer_v2_test 的 termScreen）──

type termScreen struct {
	rows, cols int
	grid       [][]string
	curRow     int
	curCol     int
}

func newTermScreen(rows, cols int) *termScreen {
	g := make([][]string, rows)
	for i := range g {
		g[i] = make([]string, cols)
	}
	return &termScreen{rows: rows, cols: cols, grid: g}
}

func (s *termScreen) feed(out string) {
	i := 0
	for i < len(out) {
		if out[i] != '\x1b' {
			switch out[i] {
			case '\r':
				s.curCol = 0
				i++
			case '\n':
				s.moveDown(1)
				s.curCol = 0
				i++
			default:
				if out[i] < 0x20 {
					i++
					continue
				}
				r, size := utf8.DecodeRuneInString(out[i:])
				s.write(r)
				i += size
			}
			continue
		}
		if i+1 >= len(out) {
			break
		}
		switch out[i+1] {
		case '[':
			j := i + 2
			for j < len(out) && !isLetter(out[j]) {
				j++
			}
			if j >= len(out) {
				i = j
				continue
			}
			param, final := out[i+2:j], out[j]
			s.applyCSI(param, final)
			i = j + 1
		case ']', '_':
			j := i + 2
			for j < len(out) && out[j] != '\x07' && !(out[j] == '\x1b' && j+1 < len(out) && out[j+1] == '\\') {
				j++
			}
			if j < len(out) && out[j] == '\x07' {
				i = j + 1
			} else if j < len(out) {
				i = j + 2
			} else {
				i = j
			}
		case '\\':
			i += 2
		default:
			i++
		}
	}
}

func (s *termScreen) applyCSI(param string, final byte) {
	switch final {
	case 'A':
		s.curRow = max(0, s.curRow-atoi(param, 1))
	case 'B':
		s.moveDown(atoi(param, 1))
	case 'G':
		s.curCol = min(max(atoi(param, 1)-1, 0), s.cols-1)
	case 'K':
		if strings.HasPrefix(param, "2") {
			for c := 0; c < s.cols; c++ {
				s.grid[s.curRow][c] = ""
			}
		}
	case 'H':
		s.curRow, s.curCol = 0, 0
	case 'J':
		if strings.HasPrefix(param, "2") {
			for r := 0; r < s.rows; r++ {
				for c := 0; c < s.cols; c++ {
					s.grid[r][c] = ""
				}
			}
			s.curRow, s.curCol = 0, 0
		}
	case 'm', 'h', 'l':
		// SGR / 模式：忽略
	}
}

func (s *termScreen) moveDown(n int) {
	if s.curRow+n >= s.rows {
		for k := s.curRow + n - s.rows + 1; k > 0; k-- {
			for r := 1; r < s.rows; r++ {
				s.grid[r-1] = append([]string(nil), s.grid[r]...)
			}
			s.grid[s.rows-1] = make([]string, s.cols)
		}
		s.curRow = s.rows - 1
	} else {
		s.curRow += n
	}
}

func (s *termScreen) write(r rune) {
	if s.curRow >= 0 && s.curRow < s.rows && s.curCol >= 0 && s.curCol < s.cols {
		s.grid[s.curRow][s.curCol] = string(r)
	}
	s.curCol++
}

func (s *termScreen) dump() []string {
	res := make([]string, s.rows)
	for r := 0; r < s.rows; r++ {
		res[r] = strings.Join(s.grid[r], "")
	}
	return res
}

func atoi(param string, def int) int {
	p := strings.TrimPrefix(param, "?")
	if p == "" {
		return def
	}
	n := 0
	for _, ch := range p {
		if ch < '0' || ch > '9' {
			return def
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// ── 复刻 tui.renderComponent：Render → 前缀 → 背景 → 子组件 ──

func renderComp(comp core.Component) []string {
	view := comp.Render(core.Data{})
	if p, ok := comp.(component.PrefixProvider); ok {
		view.Lines = component.ApplyPrefix(view.Lines, p.Widget(), p.Level())
	}
	if bg, ok := comp.(component.BackgroundProvider); ok {
		if c := bg.BackgroundColor(); c != "" {
			for i, line := range view.Lines {
				view.Lines[i] = component.FillBackground(line, c, bg.FullWidth())
			}
		}
	}
	for _, child := range comp.Children() {
		view.Lines = append(view.Lines, renderComp(child)...)
	}
	return view.Lines
}

// ── 帧构建 ──

type frameSpec struct {
	header, chat, thinking, agentTask, panel, status, editor, config, footer []core.Component
}

func (fs frameSpec) build() []string {
	var lines []string
	for _, cs := range [][]core.Component{fs.header, fs.chat, fs.thinking, fs.agentTask, fs.panel, fs.status, fs.editor, fs.config, fs.footer} {
		for _, comp := range cs {
			lines = append(lines, renderComp(comp)...)
		}
	}
	return lines
}

const width = 120

var todoCmd = "rm -rf /tmp/todohome && mkdir -p /tmp/todohome\n" +
	"cat <<'EOF' | HOME=/tmp/todohome /tmp/todo-test 2>&1\n" +
	"add 带空格 标题 -d \"含 空格 的描述\" -p low\n" +
	"add 引号测试 -d \"她说 \\\"你好\\\"\"\n" +
	"list\n" +
	"show 1\n" +
	"edit 1 -s 改过的标题\n" +
	"done 1\n" +
	"start 2\n" +
	"pending 1\n" +
	"list\n" +
	"show 999\n" +
	"list -s 无效状态\n" +
	"list -p 无效优先级\n" +
	"add\n" +
	"delete 999\n" +
	"EOF"

func buildComponents() (frames []frameSpec) {
	wc := welcome.New()
	wc.SetShortCwd("~/data/release/tinyclue")

	userComp := text.New("REPL 可以管道输入。现在测试边界情况。")
	userComp.SetWidget(&widget.PromptWidget{})
	userComp.SetBackground(types.BgGray)

	// 工具：面板弹出阶段 detail 为空；Deny 后 detail = denied。
	newTool := func(detailText string) *tool.ToolComponent {
		tc := tool.NewToolComponent("Bash", todoCmd)
		dot := widget.NewStatusWidget()
		dot.SetState(widget.StatusWidgetDefault)
		tc.SetWidget(dot)
		d := text.New(detailText)
		d.SetLevel(1)
		tc.AddChild(d)
		return tc
	}
	toolEmpty := newTool("")
	toolDenied := newTool("Tool execution denied by user")

	responseComp := text.New("刚才那条测试被拒绝了。你是不希望我实际运行这些命令（比如跑一堆 add/edit 测试写进数据库），还是希望我换个方式排查（例如只做静态代码审查，或改用 go test）？")
	responseComp.SetWidget(widget.NewStatusWidget())

	panelComp := permission.New("Bash", todoCmd, "Dangerous command detected: Test ls Command\nCommand: "+todoCmd, "")

	statusComp := status_info.NewStatusInfoComponent("Processing...")
	statusComp.SetState(status_info.StatusRunning)
	statusDone := status_info.NewStatusInfoComponent("Finished")
	statusDone.SetState(status_info.StatusDone)

	ed := editor.New()
	ed.SetFocused(true)

	footerComp := footer.New()
	footerComp.SetData(footer.FooterData{
		DefaultModel: "gpt-5", ReasoningEffort: "medium",
		ContextWindow: 200000, ContextPercent: 12.3,
	})

	return []frameSpec{
		// A：用户消息 + status/editor/footer
		{header: []core.Component{wc}, chat: []core.Component{userComp}, status: []core.Component{statusComp}, editor: []core.Component{ed}, footer: []core.Component{footerComp}},
		// B：工具开始（chat 出现 Bash 块，detail 空）
		{header: []core.Component{wc}, chat: []core.Component{userComp, toolEmpty}, status: []core.Component{statusComp}, editor: []core.Component{ed}, footer: []core.Component{footerComp}},
		// C：面板弹出，status/editor/footer 隐藏
		{header: []core.Component{wc}, chat: []core.Component{userComp, toolEmpty}, panel: []core.Component{panelComp}},
		// D：Deny，面板隐藏，detail 更新，status/editor/footer 恢复
		{header: []core.Component{wc}, chat: []core.Component{userComp, toolDenied}, status: []core.Component{statusComp}, editor: []core.Component{ed}, footer: []core.Component{footerComp}},
		// E：助手回复 + Finished
		{header: []core.Component{wc}, chat: []core.Component{userComp, toolDenied, responseComp}, status: []core.Component{statusDone}, editor: []core.Component{ed}, footer: []core.Component{footerComp}},
	}
}

func stripAnsi(s string) string {
	return types.AnsiPattern.ReplaceAllString(s, "")
}

func TestPermissionDenyRepro(t *testing.T) {
	terminal.DefaultTerminalContext.SetWidth(width)
	frames := buildComponents()

	heights := []int{30, 40, 50, 60}
	for _, h := range heights {
		t.Run(fmt.Sprintf("height=%d", h), func(t *testing.T) {
			r := renderer.NewV2()
			r.SetCols(width)
			r.SetTermHeight(h)
			sc := newTermScreen(h, width)

			for i, fs := range frames {
				frame := fs.build()
				out := capture(func() { r.Render(frame) })
				sc.feed(out)
				fmt.Printf("==== height=%d frame[%d] frameRows=%d ====\n", h, i, len(frame))
				for ri, row := range sc.dump() {
					row = strings.TrimRight(stripAnsi(row), " ")
					if row == "" {
						continue
					}
					fmt.Printf("  %2d| %s\n", ri, row)
				}
				fmt.Println()
			}

			final := strings.Join(sc.dump(), "\n")
			clean := stripAnsi(final)

			if strings.Contains(clean, "Tool Execution Confirmation") {
				t.Errorf("STALE: final screen still contains Tool Execution Confirmation")
			}
			if !strings.Contains(clean, "列出目录") && !strings.Contains(clean, "REPL 可以管道输入") {
				t.Errorf("MISSING: user message not visible in final screen")
			}
			if n := strings.Count(clean, "⏺ Bash(rm"); n != 1 {
				t.Errorf("DUPLICATE: found %d Bash blocks, want 1", n)
			}
		})
	}
}
