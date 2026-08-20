package types

// REJECT_MESSAGE / REJECT_MESSAGE_WITH_REASON_PREFIX）。Bash / AskUserQuestion /
// ExitPlanMode 等所有权限拒绝共用，由 agent_loop 统一注入。
const (
	RejectMessage                 = "The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). STOP what you are doing and wait for the user to tell you how to proceed."
	RejectMessageWithReasonPrefix = "The user doesn't want to proceed with this tool use. The tool use was rejected (eg. if it was a file edit, the new_string was NOT written to the file). To tell you how to proceed, the user said:\n"
)
