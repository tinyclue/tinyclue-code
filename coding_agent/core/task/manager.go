package task

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// DefaultTasksDirName is the directory under $HOME where task lists live.
const DefaultTasksDirName = ".tinyclue/tasks"

// ---------------------------------------------------------------------------
// Path helpers
// ---------------------------------------------------------------------------

var pathSanitizer = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizePathComponent(s string) string {
	return pathSanitizer.ReplaceAllString(s, "-")
}

const (
	highWaterMarkFile = ".highwatermark"
	lockFileName      = ".lock"
)

// ---------------------------------------------------------------------------
// TaskManager
// ---------------------------------------------------------------------------

// TaskManager provides the full task lifecycle API backed by a file-based
// storage. All public methods are safe for concurrent use from multiple
// goroutines / processes via OS-level file locking.
//
// Directory layout:
//
//	{baseDir}/{listID}/
//	  .lock             ← global lock (create, reset, claim-with-busy-check)
//	  .highwatermark    ← monotonic ID counter (prevents ID reuse after delete)
//	  1.json
//	  2.json
//	  ...
type TaskManager struct {
	ctx        context.Context
	runtimeCtx *types.AgentRuntimeContext
	agentId    string
	baseDir    string // e.g. /home/user/.tinyclue/tasks
	listID     string // sanitised session identifier
	eventSink  types.EventSink
}

// New creates a TaskManager bound to the given sessionID.
// The task list directory will be {home}/.tinyclue/tasks/{sanitized(sessionID)}/.
func NewTaskManager(ctx context.Context, runtimeCtx *types.AgentRuntimeContext) *TaskManager {
	return &TaskManager{
		ctx:        ctx,
		runtimeCtx: runtimeCtx,
		baseDir:    defaultBaseDir(),
	}
}

// NewWithDir creates a TaskManager with an explicit agent ID, base directory and session ID.
// Used primarily in tests.
func NewWithDir(agentId, baseDir, sessionId string) *TaskManager {
	return &TaskManager{
		agentId: agentId,
		baseDir: baseDir,
		listID:  sessionId,
	}
}

func (tm *TaskManager) WithEventSink(sink types.EventSink) *TaskManager {
	tm.eventSink = sink
	return tm
}

func (tm *TaskManager) Init(agentId, baseDir, sessionId string) {
	tm.agentId = agentId
	tm.baseDir = baseDir
	tm.listID = sessionId
}

// defaultBaseDir returns $HOME/.tinyclue/tasks (or a fallback).
func defaultBaseDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tinyclue/tasks"
	}
	return filepath.Join(home, DefaultTasksDirName)
}

// ---------------------------------------------------------------------------
// Path helpers (internal)
// ---------------------------------------------------------------------------

func (m *TaskManager) tasksDir() string {
	return filepath.Join(m.baseDir, sanitizePathComponent(m.listID))
}

func (m *TaskManager) taskPath(id string) string {
	return filepath.Join(m.tasksDir(), sanitizePathComponent(id)+".json")
}

func (m *TaskManager) lockPath() string {
	return filepath.Join(m.tasksDir(), lockFileName)
}

func (m *TaskManager) hwmPath() string {
	return filepath.Join(m.tasksDir(), highWaterMarkFile)
}

// ---------------------------------------------------------------------------
// High water mark
// ---------------------------------------------------------------------------

func (m *TaskManager) readHWM() (int, error) {
	data, err := os.ReadFile(m.hwmPath())
	if err != nil {
		// Swallow all errors — same as TS readHighWaterMark which catches
		// everything and returns 0 (ENOENT when dir/file doesn't exist, or
		// any other filesystem error).
		return 0, nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, nil
	}
	return v, nil
}

func (m *TaskManager) writeHWM(val int) error {
	return os.WriteFile(m.hwmPath(), []byte(strconv.Itoa(val)), 0644)
}

// findHighestIDFromFiles scans existing .json files for the largest numeric ID.
func (m *TaskManager) findHighestIDFromFiles() (int, error) {
	entries, err := os.ReadDir(m.tasksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	highest := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		idStr := strings.TrimSuffix(e.Name(), ".json")
		id, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		if id > highest {
			highest = id
		}
	}
	return highest, nil
}

// findHighestID returns the maximum of the on-disk max ID and the high-water mark.
func (m *TaskManager) findHighestID() (int, error) {
	fromFiles, err := m.findHighestIDFromFiles()
	if err != nil {
		return 0, err
	}
	fromMark, err := m.readHWM()
	if err != nil {
		return 0, err
	}
	if fromFiles > fromMark {
		return fromFiles, nil
	}
	return fromMark, nil
}

// ---------------------------------------------------------------------------
// Task file I/O
// ---------------------------------------------------------------------------

func (m *TaskManager) readTask(id string) (*types.Task, error) {
	data, err := os.ReadFile(m.taskPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var t types.Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, nil // silently discard corrupt files (same as original)
	}
	if t.ID == "" {
		return nil, nil
	}
	if t.Status != types.TaskStatusPending && t.Status != types.TaskStatusInProgress && t.Status != types.TaskStatusCompleted {
		return nil, nil
	}
	if t.Blocks == nil {
		t.Blocks = []string{}
	}
	if t.BlockedBy == nil {
		t.BlockedBy = []string{}
	}
	return &t, nil
}

func (m *TaskManager) writeTask(t *types.Task) error {
	_ = ensureDir(m.tasksDir())
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.taskPath(t.ID), data, 0644)
}

// ---------------------------------------------------------------------------
// Create
// ---------------------------------------------------------------------------

// Create creates a new task with status "pending" and returns it.
func (m *TaskManager) Create(subject, description string, opts ...types.TaskCreateOption) (*types.Task, error) {
	var cfg types.TaskCreateConfig
	for _, o := range opts {
		o(&cfg)
	}

	_ = ensureDir(m.tasksDir())
	lockP := m.lockPath()

	var t *types.Task
	err := withLock(lockP, nil, func() error {
		highest, err := m.findHighestID()
		if err != nil {
			return fmt.Errorf("find highest ID: %w", err)
		}
		id := highest + 1

		t = &types.Task{
			ID:          strconv.Itoa(id),
			Subject:     subject,
			Description: description,
			ActiveForm:  cfg.ActiveForm,
			Status:      types.TaskStatusPending,
			Blocks:      []string{},
			BlockedBy:   []string{},
			Metadata:    cfg.Metadata,
		}
		if err := m.writeTask(t); err != nil {
			return err
		}
		if err := m.writeHWM(id); err != nil {
			return err
		}

		m.eventSink.Emit(types.TaskUpdateEventType, types.TaskEvent{
			AgentId: m.agentId,
			Type:    types.TaskEventTaskCreated,
			TaskID:  t.ID,
			Subject: t.Subject,
		}, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return t, nil
}

// ---------------------------------------------------------------------------
// Get
// ---------------------------------------------------------------------------

// Get retrieves a single task by ID. Returns nil without error when the
// task does not exist.
func (m *TaskManager) Get(id string) (*types.Task, error) {
	return m.readTask(id)
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

// listAll returns all tasks regardless of metadata._internal (used internally
// by Delete and Reset for complete reference cleanup).
func (m *TaskManager) listAll() ([]*types.Task, error) {
	entries, err := os.ReadDir(m.tasksDir())
	if err != nil {
		if os.IsNotExist(err) {
			return []*types.Task{}, nil
		}
		return nil, err
	}

	var ids []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		ids = append(ids, strings.TrimSuffix(e.Name(), ".json"))
	}

	// Parallel fetch (same as original Promise.all).
	type r struct {
		task *types.Task
		err  error
	}
	ch := make(chan r, len(ids))
	for _, id := range ids {
		id := id
		go func() {
			t, err := m.readTask(id)
			ch <- r{t, err}
		}()
	}

	var tasks []*types.Task
	for range ids {
		res := <-ch
		if res.err != nil || res.task == nil {
			continue
		}
		tasks = append(tasks, res.task)
	}

	sortTasksByID(tasks)
	return tasks, nil
}

// List returns all visible tasks. Tasks with metadata key "_internal" are
// filtered out (same as the original TaskListTool behaviour).
func (m *TaskManager) List() ([]*types.Task, error) {
	all, err := m.listAll()
	if err != nil {
		return nil, err
	}
	filtered := make([]*types.Task, 0, len(all))
	for _, t := range all {
		if _, ok := t.Metadata["_internal"]; ok {
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered, nil
}

func sortTasksByID(tasks []*types.Task) {
	for i := 0; i < len(tasks); i++ {
		for j := i + 1; j < len(tasks); j++ {
			if compareNumericID(tasks[i].ID, tasks[j].ID) > 0 {
				tasks[i], tasks[j] = tasks[j], tasks[i]
			}
		}
	}
}

func compareNumericID(a, b string) int {
	ai, _ := strconv.Atoi(a)
	bi, _ := strconv.Atoi(b)
	if ai < bi {
		return -1
	}
	if ai > bi {
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// Update (public, with lock)
// ---------------------------------------------------------------------------

// Update applies a partial update to the task identified by id.
// Returns nil without error when the task does not exist.
func (m *TaskManager) Update(id string, req types.TaskUpdateRequest) (*types.Task, error) {
	taskPath := m.taskPath(id)

	before, err := m.readTask(id)
	if err != nil {
		return nil, err
	}
	if before == nil {
		return nil, nil
	}

	var updated *types.Task
	err = withLock(taskPath, nil, func() error {
		t, err := m.readTask(id)
		if err != nil {
			return err
		}
		if t == nil {
			return nil
		}

		var oldStatus *types.TaskStatus
		s := t.Status
		oldStatus = &s

		// Apply fields.
		if req.Subject != nil {
			t.Subject = *req.Subject
		}
		if req.Description != nil {
			t.Description = *req.Description
		}
		if req.ActiveForm != nil {
			t.ActiveForm = *req.ActiveForm
		}
		if req.Owner != nil {
			t.Owner = *req.Owner
		}

		// Status transition validation.
		if req.Status != nil {
			if err := types.ValidateTransition(&t.Status, req.Status); err != nil {
				return err
			}
			t.Status = *req.Status
		}

		// Metadata merge (same as TaskUpdateTool.ts:200-211).
		if req.Metadata != nil {
			if t.Metadata == nil {
				t.Metadata = make(map[string]any)
			}
			for k, v := range req.Metadata {
				if v == nil {
					delete(t.Metadata, k)
				} else {
					t.Metadata[k] = v
				}
			}
			if len(t.Metadata) == 0 {
				t.Metadata = nil
			}
		}

		// AddBlocks — append to existing (deduplicated).
		if len(req.AddBlocks) > 0 {
			existing := make(map[string]bool, len(t.Blocks))
			for _, b := range t.Blocks {
				existing[b] = true
			}
			for _, b := range req.AddBlocks {
				if !existing[b] {
					t.Blocks = append(t.Blocks, b)
					existing[b] = true
				}
			}
		}

		// AddBlockedBy — append to existing (deduplicated).
		if len(req.AddBlockedBy) > 0 {
			existing := make(map[string]bool, len(t.BlockedBy))
			for _, b := range t.BlockedBy {
				existing[b] = true
			}
			for _, b := range req.AddBlockedBy {
				if !existing[b] {
					t.BlockedBy = append(t.BlockedBy, b)
					existing[b] = true
				}
			}
		}

		if err := m.writeTask(t); err != nil {
			return err
		}
		updated = t

		var newStatus *types.TaskStatus
		ns := t.Status
		newStatus = &ns
		m.eventSink.Emit(types.TaskUpdateEventType, types.TaskEvent{
			Type:      types.TaskEventTaskUpdated,
			AgentId:   m.agentId,
			TaskID:    t.ID,
			Subject:   t.Subject,
			OldStatus: oldStatus,
			NewStatus: newStatus,
		}, nil)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return updated, nil
}

// updateTaskUnsafe writes a task file directly without acquiring a file lock.
// Callers MUST hold the appropriate lock before calling this.
func (m *TaskManager) updateTaskUnsafe(t *types.Task) error {
	oldStatus := t.Status
	if err := m.writeTask(t); err != nil {
		return err
	}
	ns := t.Status
	m.eventSink.Emit(types.TaskUpdateEventType, types.TaskEvent{
		Type:      types.TaskEventTaskUpdated,
		AgentId:   m.agentId,
		TaskID:    t.ID,
		Subject:   t.Subject,
		OldStatus: &oldStatus,
		NewStatus: &ns,
	}, nil)
	return nil
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// Delete removes a task by ID. Also cleans up references from other tasks'
// blocks / blockedBy arrays. Returns false when the task does not exist.
func (m *TaskManager) Delete(id string) (bool, error) {
	taskP := m.taskPath(id)

	// Update high water mark before deletion.
	numID, err := strconv.Atoi(id)
	if err == nil {
		current, _ := m.readHWM()
		if numID > current {
			_ = m.writeHWM(numID)
		}
	}

	// Delete the task file.
	if err := os.Remove(taskP); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	// Clean up references from other tasks (same as original deleteTask).
	// Each write is protected by a per-task file lock (TS uses updateTask
	// which acquires a per-task lock before calling updateTaskUnsafe).
	all, err := m.listAll()
	if err == nil {
		for _, t := range all {
			needsBlocks := contains(t.Blocks, id)
			needsBlockedBy := contains(t.BlockedBy, id)
			if !needsBlocks && !needsBlockedBy {
				continue
			}

			_ = withLock(m.taskPath(t.ID), nil, func() error {
				task, err := m.readTask(t.ID)
				if err != nil || task == nil {
					return nil
				}
				changed := false
				newBlocks := removeString(task.Blocks, id)
				if len(newBlocks) != len(task.Blocks) {
					task.Blocks = newBlocks
					changed = true
				}
				newBlockedBy := removeString(task.BlockedBy, id)
				if len(newBlockedBy) != len(task.BlockedBy) {
					task.BlockedBy = newBlockedBy
					changed = true
				}
				if changed {
					_ = m.writeTask(task)
				}
				return nil
			})
		}
	}
	m.eventSink.Emit(types.TaskUpdateEventType, types.TaskEvent{
		Type:    types.TaskEventTaskDeleted,
		AgentId: m.agentId,
		TaskID:  id,
	}, nil)
	return true, nil
}

func removeString(slice []string, s string) []string {
	var out []string
	for _, v := range slice {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func (m *TaskManager) Block(fromID, toID string) error {
	from, err := m.Get(fromID)
	if err != nil {
		return err
	}
	if from == nil {
		return fmt.Errorf("task %s not found", fromID)
	}
	to, err := m.Get(toID)
	if err != nil {
		return err
	}
	if to == nil {
		return fmt.Errorf("task %s not found", toID)
	}
	if !contains(from.Blocks, toID) {
		if err := withLock(m.taskPath(fromID), nil, func() error {
			t, err := m.readTask(fromID)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("task %s not found", fromID)
			}
			if !contains(t.Blocks, toID) {
				t.Blocks = append(t.Blocks, toID)
			}
			return m.updateTaskUnsafe(t)
		}); err != nil {
			return err
		}
	}
	if !contains(to.BlockedBy, fromID) {
		if err := withLock(m.taskPath(toID), nil, func() error {
			t, err := m.readTask(toID)
			if err != nil {
				return err
			}
			if t == nil {
				return fmt.Errorf("task %s not found", toID)
			}
			if !contains(t.BlockedBy, fromID) {
				t.BlockedBy = append(t.BlockedBy, fromID)
			}
			return m.updateTaskUnsafe(t)
		}); err != nil {
			return err
		}
	}
	return nil
}

func (m *TaskManager) Reset() error {
	_ = ensureDir(m.tasksDir())
	lockP := m.lockPath()
	return withLock(lockP, nil, func() error {
		highest, err := m.findHighestIDFromFiles()
		if err != nil {
			return err
		}
		if highest > 0 {
			existing, rErr := m.readHWM()
			if rErr != nil {
				return rErr
			}
			if highest > existing {
				if wErr := m.writeHWM(highest); wErr != nil {
					return wErr
				}
			}
		}
		entries, err := os.ReadDir(m.tasksDir())
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			_ = os.Remove(filepath.Join(m.tasksDir(), e.Name()))
		}
		m.eventSink.Emit(types.TaskUpdateEventType, types.TaskEvent{Type: types.TaskEventListReset, AgentId: m.agentId}, nil)
		return nil
	})
}

func unique(slice []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, s := range slice {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
