package types

const (
	PLAN_MODE                = "plan_mode"
	PLAN_MODE_EXIT           = "plan_mode_exit"
	CRITICAL_SYSTEM_REMINDER = "critical_system_reminder"
)

type AttachmentMessage struct {
	Type string
	Data map[string]any
}
