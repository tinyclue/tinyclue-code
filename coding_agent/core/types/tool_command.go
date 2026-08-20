package types

type ToolCommand string

const (
	EnterPlanCommand ToolCommand = "enter_plan_command"
	ExitPlanCommand  ToolCommand = "exit_plan_command"
	RunAgentCommand  ToolCommand = "run_agent_command"
)
