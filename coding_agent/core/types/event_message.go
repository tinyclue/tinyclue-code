package types

type AgentEventType string

const (
	UserInput        AgentEventType = "user_input"
	TaskNotification AgentEventType = "task_notification"

	AgentStart AgentEventType = "agent_start"
	AgentEnd   AgentEventType = "agent_end"
	TurnStart  AgentEventType = "turn_start"
	TurnEnd    AgentEventType = "turn_end"
	WaitJob    AgentEventType = "wait_job"
	AgentStat  AgentEventType = "agent_stat"
	//
	//QueueUpdateEventType          AgentEventType = "queue_update"
	//SessionInfoChangedEventType   AgentEventType = "session_info_changed"
	//ThinkingLevelChangedEventType AgentEventType = "thinking_level_changed"

	UserMessage       = "user_message"
	UserMessageDetail = "user_message_detail"

	AutoCompleteMessage = "auto_complete_message"
	AutoCompleteDetail  = "auto_complete_detail"

	AssistantThinkingStart  = "assistant_thinking_start"
	AssistantThinkingUpdate = "assistant_thinking_update"
	AssistantThinkingEnd    = "assistant_thinking_end"

	AssistantTextStart  = "assistant_text_start"
	AssistantTextUpdate = "assistant_text_update"
	AssistantTextEnd    = "assistant_text_end"

	ToolExecutionStart  AgentEventType = "tool_execution_start"
	ToolExecutionUpdate AgentEventType = "tool_execution_update"
	ToolExecutionEnd    AgentEventType = "tool_execution_end"

	CompactionStartEventType AgentEventType = "compaction_start"
	CompactionEndEventType   AgentEventType = "compaction_end"
	//
	AutoRetryStartEventType AgentEventType = "auto_retry_start"
	AutoRetryEndEventType   AgentEventType = "auto_retry_end"

	JobUpdateEventType = "job_update"

	TaskUpdateEventType = "task_update"

	ToolPermissionRequired AgentEventType = "tool_permission_required"
)

type EventMessage struct {
	EventType AgentEventType
	Message   interface{}
	Err       error
}

func NewEventMessage(eventType AgentEventType, message interface{}, err error) EventMessage {
	return EventMessage{
		EventType: eventType,
		Message:   message,
		Err:       err,
	}
}
