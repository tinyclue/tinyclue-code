package types

const (
	BASH_TOOL_NAME              = "Bash"
	READ_TOOL_NAME              = "Read"
	WRITE_TOOL_NAME             = "Write"
	EDIT_TOOL_NAME              = "Edit"
	GLOB_TOOL_NAME              = "Glob"
	GREP_TOOL_NAME              = "Grep"
	ENTER_PLAN_MODE_TOOL_NAME   = "EnterPlanMode"
	EXIT_PLAN_MODE_TOOL_NAME    = "ExitPlanMode"
	WEB_SEARCH_TOOL_NAME        = "WebSearch"
	TOOL_SEARCH_TOOL_NAME       = "ToolSearch"
	ASK_USER_QUESTION_TOOL_NAME = "AskUserQuestion"
	RUN_AGENT_TOOL_NAME         = "RunAgent"
	SEND_MESSAGE_TOOL_NAME      = "SendMessage"
	TASK_CREATE_TOOL_NAME       = "TaskCreate"
	TASK_GET_TOOL_NAME          = "TaskGet"
	TASK_LIST_TOOL_NAME         = "TaskList"
	TASK_UPDATE_TOOL_NAME       = "TaskUpdate"
	SKILL_TOOL_NAME             = "Skill"
	WEB_FETCH_TOOL_NAME         = "WebFetch"

	// MCP 浏览器自动化工具名前缀（外部 MCP server 暴露的真实工具名）。
	// 带 __* 通配符表示整组工具。与模型 ID 同属外部标识符，不能改名。
	MCP_PLAYWRIGHT_TOOL = "mcp__playwright__*"
)
