package types

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
)

// TaskStatuses is the ordered list of valid statuses.
var TaskStatuses = []TaskStatus{TaskStatusPending, TaskStatusInProgress, TaskStatusCompleted}

// Task is the core data model. Each task is persisted as a single JSON file.
// Blocks — 我阻塞了谁，即下游依赖。如果 task A 的 Blocks 包含 B 的 ID，表示 B 依赖 A，A 没完成前 B 不应开始。
//
// BlockedBy — 我被谁阻塞，即上游依赖。如果 task B 的 BlockedBy 包含 A 的 ID，表示 B 被 A 阻塞，A 没完成前 B 不应开始。
//
// 举个例子，Block("1", "2") 调用后：
//
// task1.Blocks    = ["2"]   // 1 阻塞了 2
// task2.BlockedBy = ["1"]   // 2 被 1 阻塞
type Task struct {
	ID          string         `json:"id"`
	Subject     string         `json:"subject"`
	Description string         `json:"description"`
	ActiveForm  string         `json:"activeForm,omitempty"`
	Status      TaskStatus     `json:"status"`
	Owner       string         `json:"owner,omitempty"`
	Blocks      []string       `json:"blocks"`
	BlockedBy   []string       `json:"blockedBy"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// validTransitions defines allowed status transitions.
// Completed is a terminal state — no transitions out.
var validTransitions = map[TaskStatus][]TaskStatus{
	TaskStatusPending:    {TaskStatusInProgress, TaskStatusCompleted},
	TaskStatusInProgress: {TaskStatusPending, TaskStatusCompleted},
}

// ValidateTransition returns an error if the transition from → to is disallowed.
// If to is nil (no status change requested), returns nil.
func ValidateTransition(from, to *TaskStatus) error {
	if to == nil || from == nil {
		return nil
	}
	allowed, ok := validTransitions[*from]
	if !ok {
		return &ErrInvalidTransition{From: *from, To: *to}
	}
	for _, a := range allowed {
		if a == *to {
			return nil
		}
	}
	return &ErrInvalidTransition{From: *from, To: *to}
}

// ErrInvalidTransition is returned when a status change is not allowed.
type ErrInvalidTransition struct {
	From TaskStatus
	To   TaskStatus
}

func (e *ErrInvalidTransition) Error() string {
	return "invalid status transition from " + string(e.From) + " to " + string(e.To)
}

// ---------------------------------------------------------------------------
// TaskCreateOption — functional options for TaskManager.Create
// ---------------------------------------------------------------------------

// TaskCreateOption is a functional option for Create.
type TaskCreateOption func(*TaskCreateConfig)

// TaskCreateConfig holds the optional parameters for Create.
type TaskCreateConfig struct {
	ActiveForm string
	Metadata   map[string]any
}

// WithActiveForm sets the spinner text shown while the task is in_progress.
func WithActiveForm(s string) TaskCreateOption {
	return func(c *TaskCreateConfig) { c.ActiveForm = s }
}

// WithMetadata attaches arbitrary metadata to the new task.
func WithMetadata(m map[string]any) TaskCreateOption {
	return func(c *TaskCreateConfig) { c.Metadata = m }
}

// ---------------------------------------------------------------------------
// TaskUpdateRequest
// ---------------------------------------------------------------------------

// TaskUpdateRequest describes a partial update to a task. Only non-nil / non-empty
// fields are applied. Metadata is merged with the existing value (nil value =
// delete key). AddBlocks / AddBlockedBy are appended (not replaced).
type TaskUpdateRequest struct {
	Subject      *string
	Description  *string
	ActiveForm   *string
	Status       *TaskStatus
	Owner        *string
	AddBlocks    []string
	AddBlockedBy []string
	Metadata     map[string]any
}

// ---------------------------------------------------------------------------
// Notification event
// ---------------------------------------------------------------------------

// TaskEventType classifies a task notification.
type TaskEventType int

const (
	TaskEventTaskCreated TaskEventType = iota
	TaskEventTaskUpdated
	TaskEventTaskDeleted
	TaskEventListReset
)

// TaskEvent is emitted to subscribers whenever tasks change.
type TaskEvent struct {
	AgentId   string
	Type      TaskEventType
	TaskID    string
	Subject   string
	OldStatus *TaskStatus // only for TaskEventTaskUpdated
	NewStatus *TaskStatus // only for TaskEventTaskUpdated
}
