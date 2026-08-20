package types

import apitypes "github.com/tinyclue/tinyclue-code/api_provider/types"

type BaseContent interface {
	Type() string
}

// TextContent 文本工具输出。
// Delta 是流式增量文本（ToolExecutionUpdate 事件携带，逐次追加显示）；
// Text 是最终文本（ToolExecutionEnd 事件携带，替换显示）。
type TextContent struct {
	Delta   string
	Text    string
	Details map[string]any
}

func (TextContent) Type() string { return "text" }

type ImageContent struct {
	Text     string
	Data     string
	MimeType string
}

func (ImageContent) Type() string {
	return "image"
}

type ToolSearchContent struct {
	Matchs             []string
	Query              string
	TotalDeferredTools int
}

func (ToolSearchContent) Type() string {
	return "tool_search"
}

type WebSearchHit struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type WebSearchResult struct {
	ToolUseID string         `json:"tool_use_id"`
	Content   []WebSearchHit `json:"content"`
}

type WebSearchContent struct {
	Query           string
	Results         []any // WebSearchResult or string
	DurationSeconds float64
}

func (WebSearchContent) Type() string { return "web_search" }

// GlobContent 是 Glob 工具返回的内容。
type GlobContent struct {
	Filenames  []string
	NumFiles   int
	Truncated  bool
	DurationMs int64
}

func (GlobContent) Type() string { return "glob" }

// GrepContent 是 Grep 工具返回的内容。
type GrepContent struct {
	Mode          string   // "content", "files_with_matches", "count"
	NumFiles      int      // 匹配的文件数
	Filenames     []string // 匹配的文件列表
	Content       string   // content/count 模式下的输出文本
	NumLines      int      // content 模式下的行数
	NumMatches    int      // count 模式下的总匹配数
	AppliedLimit  int      // 实际应用的 head_limit
	AppliedOffset int      // 实际应用的 offset
	DurationMs    int64    // 执行耗时
}

func (GrepContent) Type() string { return "grep" }

// PlanModeContent 是 EnterPlanMode 工具返回的内容。
type PlanModeContent struct {
	Message string
}

func (PlanModeContent) Type() string { return "plan_mode" }

// ExitPlanModeContent 是 ExitPlanMode 工具返回的内容。
type ExitPlanModeContent struct {
	Plan       string
	IsSubAgent bool
	FilePath   string
	//HasTaskTool            bool
	//PlanWasEdited          bool
	//AwaitingLeaderApproval bool
	//RequestId              string
}

func (ExitPlanModeContent) Type() string { return "exit_plan_mode" }

// TaskCreateContent 是 TaskCreate 工具返回的内容。
type TaskCreateContent struct {
	TaskID  string
	Subject string
}

func (TaskCreateContent) Type() string { return "task_create" }

// TaskGetTask 是 TaskGet 工具返回的任务详情。
type TaskGetTask struct {
	ID          string
	Subject     string
	Description string
	Status      string
	Blocks      []string
	BlockedBy   []string
}

// TaskGetContent 是 TaskGet 工具返回的内容。
type TaskGetContent struct {
	Task *TaskGetTask
}

func (TaskGetContent) Type() string { return "task_get" }

// TaskListTask 是 TaskList 工具返回的单条任务摘要。
type TaskListTask struct {
	ID        string
	Subject   string
	Status    string
	Owner     string
	BlockedBy []string
}

// TaskListContent 是 TaskList 工具返回的内容。
type TaskListContent struct {
	Tasks []TaskListTask
}

func (TaskListContent) Type() string { return "task_list" }

// StatusChange 记录任务状态的变化。
type StatusChange struct {
	From string
	To   string
}

// TaskUpdateContent 是 TaskUpdate 工具返回的内容。
type TaskUpdateContent struct {
	Success                 bool
	TaskID                  string
	UpdatedFields           []string
	Error                   string
	StatusChange            *StatusChange
	VerificationNudgeNeeded bool
}

func (TaskUpdateContent) Type() string { return "task_update" }

// SubAgentContent 是 RunAgent 工具返回的内容，携带子 agent 的创建参数。
type SubAgentContent struct {
	Status            string //子agent状态
	Prompt            string //输入的prompt
	AgentId           string //子agent的ID
	AgentType         AgentType
	Description       string //输入的description
	ToolUseId         string
	TotalToolUseCount int          //工具总使用数量
	TotalDurationMs   int          //总耗时
	TotalTokens       int          //使用token数量
	SessionUsage      SessionUsage //子agent的token使用明细
	WorkTreePath      string
	WorkTreeBranch    string
	IsAsync           bool
	Context           []apitypes.ContentBlock //子agent最终返回结果
	OutputFilePath    string                  //子agent的session file.jsonl文件
	Error             string
}

func (SubAgentContent) Type() string { return "sub_agent" }

// BashTaskContent 是 Bash 工具执行返回的内容，形式对齐 SubAgentContent。
// 同步执行：携带完整输出文本（Text）+ 截断详情（Details）；
// 后台执行（run_in_background）：携带后台任务信息（Status=async_launched 启动即返回，
// 终态由 LocalBashJobState.Result 携带）。
type BashTaskContent struct {
	Status         string // 任务状态：async_launched（启动即返回）/ completed / failed / killed
	TaskId         string // 后台任务 ID（同步为空）
	Command        string // 执行的命令
	ToolUseId      string
	ExitCode       int            // 退出码
	OutputFilePath string         // 输出文件路径（同步为空）
	DurationMs     int            // 耗时
	IsAsync        bool           // 是否后台执行
	Error          string         // 错误信息
	Text           string         // 同步 bash 的完整输出文本（异步为空，从输出文件读取）
	Details        map[string]any // 附加详情（同步的 snap 截断信息等）
}

func (BashTaskContent) Type() string { return "bash_task" }

type ValidateContent struct {
	Result  bool
	Message string
}

func (ValidateContent) Type() string { return "validate" }

// AskUserQuestionContent is the AskUserQuestion tool result.
type AskUserQuestionContent struct {
	Questions   []any          `json:"questions"`
	Answers     map[string]any `json:"answers"`
	Annotations map[string]any `json:"annotations,omitempty"`
}

func (AskUserQuestionContent) Type() string { return "ask_user_question" }

type ToolContext struct {
	ToolCall apitypes.ToolCall
	Content  BaseContent
	Commands []ToolCommand
}
