package types

// TaskManagerApi is the interface for creating and managing todo tasks.
// It is satisfied by core/task.TaskManager and used by tools (TaskCreate etc.)
// to avoid direct import of the task manager package.
type TaskManagerApi interface {
	Create(subject, description string, opts ...TaskCreateOption) (*Task, error)
	Get(id string) (*Task, error)
	List() ([]*Task, error)
	Update(id string, req TaskUpdateRequest) (*Task, error)
	Delete(id string) (bool, error)
	Block(fromID, toID string) error
}
