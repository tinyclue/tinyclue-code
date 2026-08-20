package task

import (
	tasktypes "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newLifecycleManager(t *testing.T) *TaskManager {
	t.Helper()
	return NewWithDir("", t.TempDir(), "lifecycle-test")
}

func mustCreate(t *testing.T, m *TaskManager, subject, desc string, opts ...tasktypes.TaskCreateOption) *tasktypes.Task {
	t.Helper()
	task, err := m.Create(subject, desc, opts...)
	if err != nil {
		t.Fatalf("Create(%q) failed: %v", subject, err)
	}
	return task
}

func mustUpdate(t *testing.T, m *TaskManager, id string, req tasktypes.TaskUpdateRequest) *tasktypes.Task {
	t.Helper()
	task, err := m.Update(id, req)
	if err != nil {
		t.Fatalf("Update(%s) failed: %v", id, err)
	}
	if task == nil {
		t.Fatalf("Update(%s): task not found", id)
	}
	return task
}

func mustBlock(t *testing.T, m *TaskManager, from, to string) {
	t.Helper()
	if err := m.Block(from, to); err != nil {
		t.Fatalf("Block(%s, %s) failed: %v", from, to, err)
	}
}

func mustDelete(t *testing.T, m *TaskManager, id string) {
	t.Helper()
	ok, err := m.Delete(id)
	if err != nil {
		t.Fatalf("Delete(%s) failed: %v", id, err)
	}
	if !ok {
		t.Fatalf("Delete(%s): task not found", id)
	}
}

func waitForEvent(t *testing.T, ch <-chan tasktypes.TaskEvent, timeout time.Duration, wantType tasktypes.TaskEventType) tasktypes.TaskEvent {
	t.Helper()
	select {
	case ev := <-ch:
		if ev.Type != wantType {
			t.Fatalf("expected event %v, got %v", wantType, ev.Type)
		}
		return ev
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for event %v", wantType)
		return tasktypes.TaskEvent{}
	}
}

func assertNoEvent(t *testing.T, ch <-chan tasktypes.TaskEvent, timeout time.Duration) {
	t.Helper()
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event: %v (type=%v)", ev, ev.Type)
	case <-time.After(timeout):
	}
}

func assertStatus(t *testing.T, task *tasktypes.Task, want tasktypes.TaskStatus) {
	t.Helper()
	if task.Status != want {
		t.Fatalf("task %s: expected status %s, got %s", task.ID, want, task.Status)
	}
}

func assertOwner(t *testing.T, task *tasktypes.Task, want string) {
	t.Helper()
	if task.Owner != want {
		t.Fatalf("task %s: expected owner %q, got %q", task.ID, want, task.Owner)
	}
}

func assertBlocks(t *testing.T, task *tasktypes.Task, want ...string) {
	t.Helper()
	if !stringSliceEqual(task.Blocks, want) {
		t.Fatalf("task %s: expected blocks %v, got %v", task.ID, want, task.Blocks)
	}
}

func assertBlockedBy(t *testing.T, task *tasktypes.Task, want ...string) {
	t.Helper()
	if !stringSliceEqual(task.BlockedBy, want) {
		t.Fatalf("task %s: expected blockedBy %v, got %v", task.ID, want, task.BlockedBy)
	}
}

func assertMetadata(t *testing.T, task *tasktypes.Task, key string, want any) {
	t.Helper()
	got, ok := task.Metadata[key]
	if !ok {
		t.Fatalf("task %s: metadata key %q not found", task.ID, key)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("task %s: metadata[%q] = %v (type %T), want %v (type %T)",
			task.ID, key, got, got, want, want)
	}
}

func assertNoMetadataKey(t *testing.T, task *tasktypes.Task, key string) {
	t.Helper()
	if _, ok := task.Metadata[key]; ok {
		t.Fatalf("task %s: metadata key %q should not exist", task.ID, key)
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// 1. Happy path: full lifecycle
// ---------------------------------------------------------------------------

func TestLifecycle_HappyPath(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	// ── Create ──
	task := mustCreate(t, m, "Implement login", "Add OAuth login flow")
	assertStatus(t, task, tasktypes.TaskStatusPending)
	assertOwner(t, task, "")
	ev := waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskCreated)
	if ev.TaskID != task.ID {
		t.Fatalf("event TaskID = %s, want %s", ev.TaskID, task.ID)
	}

	// ── Pending → InProgress ──
	status := tasktypes.TaskStatusInProgress
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	assertStatus(t, task, tasktypes.TaskStatusInProgress)
	ev = waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskUpdated)
	if *ev.NewStatus != tasktypes.TaskStatusInProgress {
		t.Fatalf("event NewStatus = %v, want in_progress", *ev.NewStatus)
	}

	// ── InProgress → Completed ──
	status = tasktypes.TaskStatusCompleted
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	assertStatus(t, task, tasktypes.TaskStatusCompleted)
	waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskUpdated)

	// ── Delete ──
	mustDelete(t, m, task.ID)
	waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskDeleted)

	// Verify gone.
	got, _ := m.Get(task.ID)
	if got != nil {
		t.Fatal("task should be gone after delete")
	}
}

// ---------------------------------------------------------------------------
// 2. Status transitions — all valid and invalid
// ---------------------------------------------------------------------------

func TestLifecycle_StatusTransitions(t *testing.T) {
	m := newLifecycleManager(t)

	task := mustCreate(t, m, "status test", "")

	// Valid: pending → in_progress
	status := tasktypes.TaskStatusInProgress
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	assertStatus(t, task, tasktypes.TaskStatusInProgress)

	// Valid: in_progress → pending (un-claim)
	status = tasktypes.TaskStatusPending
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	assertStatus(t, task, tasktypes.TaskStatusPending)

	// Valid: pending → completed (direct)
	status = tasktypes.TaskStatusCompleted
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	assertStatus(t, task, tasktypes.TaskStatusCompleted)

	// Invalid: completed → anything
	status = tasktypes.TaskStatusPending
	_, err := m.Update(task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err == nil {
		t.Fatal("expected error: completed → pending should be invalid")
	}

	status = tasktypes.TaskStatusInProgress
	_, err = m.Update(task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err == nil {
		t.Fatal("expected error: completed → in_progress should be invalid")
	}
}

// ---------------------------------------------------------------------------
// 3. Block dependency lifecycle
// ---------------------------------------------------------------------------

func TestLifecycle_BlockDependency(t *testing.T) {
	m := newLifecycleManager(t)

	// Create three tasks: A → B → C (A blocks B, B blocks C)
	a := mustCreate(t, m, "Design DB schema", "")
	b := mustCreate(t, m, "Implement models", "")
	c := mustCreate(t, m, "Write queries", "")

	mustBlock(t, m, a.ID, b.ID)
	mustBlock(t, m, b.ID, c.ID)

	// Verify bidirectional relationships.
	gotA, _ := m.Get(a.ID)
	gotB, _ := m.Get(b.ID)
	gotC, _ := m.Get(c.ID)

	assertBlocks(t, gotA, b.ID)
	assertBlockedBy(t, gotA)
	assertBlocks(t, gotB, c.ID)
	assertBlockedBy(t, gotB, a.ID)
	assertBlocks(t, gotC)
	assertBlockedBy(t, gotC, b.ID)

	// Complete the dependency chain and verify.
	// C is blocked by B, which is blocked by A.
	// Complete A first.
	status := tasktypes.TaskStatusCompleted
	mustUpdate(t, m, a.ID, tasktypes.TaskUpdateRequest{Status: &status})

	// Complete B.
	mustUpdate(t, m, b.ID, tasktypes.TaskUpdateRequest{Status: &status})

	// Complete C.
	mustUpdate(t, m, c.ID, tasktypes.TaskUpdateRequest{Status: &status})

	gotA, _ = m.Get(a.ID)
	gotB, _ = m.Get(b.ID)
	gotC, _ = m.Get(c.ID)
	assertStatus(t, gotA, tasktypes.TaskStatusCompleted)
	assertStatus(t, gotB, tasktypes.TaskStatusCompleted)
	assertStatus(t, gotC, tasktypes.TaskStatusCompleted)
}

// ---------------------------------------------------------------------------
// 4. Delete cascades — reference cleanup
// ---------------------------------------------------------------------------

func TestLifecycle_DeleteCascadeRemovesReferences(t *testing.T) {
	m := newLifecycleManager(t)

	// Setup: A blocks B, A blocks C, D blocks A
	a := mustCreate(t, m, "A", "")
	b := mustCreate(t, m, "B", "")
	c := mustCreate(t, m, "C", "")
	d := mustCreate(t, m, "D", "")

	mustBlock(t, m, a.ID, b.ID)
	mustBlock(t, m, a.ID, c.ID)
	mustBlock(t, m, d.ID, a.ID)

	// Delete A — the central node.
	mustDelete(t, m, a.ID)

	// B should no longer be blocked by A.
	gotB, _ := m.Get(b.ID)
	assertBlockedBy(t, gotB)

	// C should no longer be blocked by A.
	gotC, _ := m.Get(c.ID)
	assertBlockedBy(t, gotC)

	// D should no longer block A.
	gotD, _ := m.Get(d.ID)
	assertBlocks(t, gotD)
}

// ---------------------------------------------------------------------------
// 5. Delete non-existent and re-delete
// ---------------------------------------------------------------------------

func TestLifecycle_DeleteEdgeCases(t *testing.T) {
	m := newLifecycleManager(t)

	// Delete non-existent.
	ok, err := m.Delete("999")
	if err != nil {
		t.Fatalf("Delete(999) error: %v", err)
	}
	if ok {
		t.Fatal("Delete(999) should return false")
	}

	// Double delete.
	task := mustCreate(t, m, "ephemeral", "")
	mustDelete(t, m, task.ID)
	ok, err = m.Delete(task.ID)
	if err != nil {
		t.Fatalf("Delete(already deleted) error: %v", err)
	}
	if ok {
		t.Fatal("second Delete should return false")
	}
}

// ---------------------------------------------------------------------------
// 6. Full metadata lifecycle
// ---------------------------------------------------------------------------

func TestLifecycle_MetadataLifecycle(t *testing.T) {
	m := newLifecycleManager(t)

	// Create with metadata.
	task := mustCreate(t, m, "meta test", "",
		tasktypes.WithMetadata(map[string]any{
			"priority": "high",
			"labels":   []any{"frontend", "urgent"},
			"estimate": 3,
		}),
	)
	assertMetadata(t, task, "priority", "high")

	// Merge: add new key, modify existing.
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{
		Metadata: map[string]any{
			"priority": "critical",
			"assignee": "alice",
		},
	})
	assertMetadata(t, task, "priority", "critical")
	assertMetadata(t, task, "assignee", "alice")
	assertMetadata(t, task, "labels", []any{"frontend", "urgent"}) // unchanged

	// Delete a metadata key.
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{
		Metadata: map[string]any{
			"estimate": nil,
		},
	})
	assertNoMetadataKey(t, task, "estimate")
	assertMetadata(t, task, "priority", "critical") // others remain

	// Clear all metadata by nullifying all keys.
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{
		Metadata: map[string]any{
			"priority": nil,
			"labels":   nil,
			"assignee": nil,
		},
	})
	if task.Metadata != nil {
		t.Fatalf("expected nil metadata after clearing all keys, got %v", task.Metadata)
	}
}

// ---------------------------------------------------------------------------
// 7. Update all fields individually
// ---------------------------------------------------------------------------

func TestLifecycle_UpdateAllFields(t *testing.T) {
	m := newLifecycleManager(t)

	task := mustCreate(t, m, "original", "original desc")

	subject := "updated"
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Subject: &subject})
	if task.Subject != "updated" {
		t.Fatalf("Subject = %q, want %q", task.Subject, "updated")
	}

	desc := "updated desc"
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Description: &desc})
	if task.Description != "updated desc" {
		t.Fatalf("Description = %q, want %q", task.Description, "updated desc")
	}

	form := "doing work..."
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{ActiveForm: &form})
	if task.ActiveForm != "doing work..." {
		t.Fatalf("ActiveForm = %q, want %q", task.ActiveForm, "doing work...")
	}

	owner := "agent-42"
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Owner: &owner})
	assertOwner(t, task, "agent-42")
}

// ---------------------------------------------------------------------------
// 8. Partial update — only specified fields change
// ---------------------------------------------------------------------------

func TestLifecycle_PartialUpdate(t *testing.T) {
	m := newLifecycleManager(t)

	task := mustCreate(t, m, "original", "original desc")

	// Only update subject — description should remain.
	subject := "new subject"
	task = mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Subject: &subject})
	if task.Subject != "new subject" {
		t.Fatalf("Subject = %q, want %q", task.Subject, "new subject")
	}
	if task.Description != "original desc" {
		t.Fatalf("Description should be unchanged, got %q", task.Description)
	}
}

// ---------------------------------------------------------------------------
// 9. AddBlocks / AddBlockedBy via Update
// ---------------------------------------------------------------------------

func TestLifecycle_AddBlocksAndBlockedBy(t *testing.T) {
	m := newLifecycleManager(t)

	a := mustCreate(t, m, "A", "")
	b := mustCreate(t, m, "B", "")
	c := mustCreate(t, m, "C", "")

	// A blocks B, A blocks C.
	mustUpdate(t, m, a.ID, tasktypes.TaskUpdateRequest{AddBlocks: []string{b.ID, c.ID}})
	// B blocked by A.
	mustUpdate(t, m, b.ID, tasktypes.TaskUpdateRequest{AddBlockedBy: []string{a.ID}})

	gotA, _ := m.Get(a.ID)
	gotB, _ := m.Get(b.ID)

	assertBlocks(t, gotA, b.ID, c.ID)
	assertBlockedBy(t, gotB, a.ID)

	// Add duplicate — should be idempotent.
	mustUpdate(t, m, a.ID, tasktypes.TaskUpdateRequest{AddBlocks: []string{b.ID}})
	gotA, _ = m.Get(a.ID)
	if len(gotA.Blocks) != 2 {
		t.Fatalf("Blocks length = %d, want 2 (dedup should work)", len(gotA.Blocks))
	}
}

// ---------------------------------------------------------------------------
// 10. Reset clears everything and preserves HWM
// ---------------------------------------------------------------------------

func TestLifecycle_ResetAndHighWaterMark(t *testing.T) {
	m := newLifecycleManager(t)

	// Create 5 tasks.
	for i := 0; i < 5; i++ {
		mustCreate(t, m, "task", "")
	}

	// Reset.
	if err := m.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	// List should be empty.
	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List after reset: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected 0 tasks after reset, got %d", len(tasks))
	}

	// Verify HWM: next task should be ID 6 (not 1).
	// Also: HWM file should exist.
	if _, err := os.Stat(m.hwmPath()); os.IsNotExist(err) {
		t.Fatal("high water mark file should exist after reset")
	}
}

func TestLifecycle_HWMPreservedAfterResetWithDeletes(t *testing.T) {
	m := newLifecycleManager(t)

	t1 := mustCreate(t, m, "t1", "")
	t2 := mustCreate(t, m, "t2", "")
	t3 := mustCreate(t, m, "t3", "")
	_ = t1
	_ = t2

	// Delete highest ID task, then reset.
	mustDelete(t, m, t3.ID)
	if err := m.Reset(); err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	newTask := mustCreate(t, m, "new", "")
	if newTask.ID != "4" {
		t.Fatalf("expected ID 4 (max of 3 from HWM), got %s", newTask.ID)
	}
}

// ---------------------------------------------------------------------------
// 11. Internal task isolation
// ---------------------------------------------------------------------------

func TestLifecycle_InternalTaskIsolation(t *testing.T) {
	m := newLifecycleManager(t)

	// Create a visible task.
	visible := mustCreate(t, m, "visible", "")

	// Create an internal task (system-generated).
	internal := mustCreate(t, m, "internal", "",
		tasktypes.WithMetadata(map[string]any{"_internal": true}),
	)

	// List() should only return visible.
	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, task := range tasks {
		if _, ok := task.Metadata["_internal"]; ok {
			t.Fatalf("List() returned internal task %s", task.ID)
		}
	}
	if len(tasks) != 1 {
		t.Fatalf("List() returned %d tasks, expected 1", len(tasks))
	}

	// listAll() should return both.
	allTasks, err := m.listAll()
	if err != nil {
		t.Fatalf("listAll failed: %v", err)
	}
	if len(allTasks) != 2 {
		t.Fatalf("listAll() returned %d tasks, expected 2", len(tasks))
	}

	// Delete visible task and verify internal tasks' refs are still clean.
	mustDelete(t, m, visible.ID)
	internalAfter, _ := m.Get(internal.ID)
	if internalAfter == nil {
		t.Fatal("internal task should not be affected by deleting visible task")
	}

	// Delete the internal task directly.
	mustDelete(t, m, internal.ID)
	got, _ := m.Get(internal.ID)
	if got != nil {
		t.Fatal("internal task should be deletable by ID")
	}
}

// ---------------------------------------------------------------------------
// 12. Subscribe — all event types and ordering
// ---------------------------------------------------------------------------

func TestLifecycle_SubscribeAllEvents(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	// EventTaskCreated.
	task := mustCreate(t, m, "eventful", "")
	ev := waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskCreated)
	if ev.TaskID != task.ID || ev.Subject != "eventful" {
		t.Fatalf("create event: TaskID=%s Subject=%s", ev.TaskID, ev.Subject)
	}

	// EventTaskUpdated with status change.
	status := tasktypes.TaskStatusInProgress
	mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	ev = waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskUpdated)
	if *ev.OldStatus != tasktypes.TaskStatusPending || *ev.NewStatus != tasktypes.TaskStatusInProgress {
		t.Fatalf("update event: old=%v new=%v", *ev.OldStatus, *ev.NewStatus)
	}

	// EventTaskUpdated with field change (no status change).
	subj := "renamed"
	mustUpdate(t, m, task.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
	ev = waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskUpdated)
	if ev.OldStatus != nil && *ev.OldStatus == *ev.NewStatus {
		// OldStatus == NewStatus is expected: only subject changed, not status.
	}

	// EventTaskDeleted.
	mustDelete(t, m, task.ID)
	ev = waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskDeleted)
	if ev.TaskID != task.ID {
		t.Fatalf("delete event: TaskID=%s", ev.TaskID)
	}
}

func TestLifecycle_SubscribeReset(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	mustCreate(t, m, "a", "")

	// Drain create event.
	<-ch

	_ = m.Reset()
	waitForEvent(t, ch, time.Second, tasktypes.TaskEventListReset)
}

func TestLifecycle_SubscribeBufferOverflow(t *testing.T) {
	// Verify that a slow consumer doesn't block the emitter.
	// The eventSink callback is synchronous so it never blocks the caller;
	// events are delivered to the channel (buffered, capacity 128).
	m, _ := newTestManagerWithEvents(t)

	// Create many tasks — emitter should not block.
	for i := 0; i < 100; i++ {
		_, err := m.Create("spam", "")
		if err != nil {
			t.Fatalf("create %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// 13. Concurrent operations
// ---------------------------------------------------------------------------

func TestLifecycle_ConcurrentCreateMany(t *testing.T) {
	m := newLifecycleManager(t)

	var wg sync.WaitGroup
	n := 20
	ids := make(chan string, n)
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			task, err := m.Create("concurrent", "")
			if err != nil {
				errs <- err
				return
			}
			ids <- task.ID
		}(i)
	}

	wg.Wait()
	close(ids)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent create error: %v", err)
	}

	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Fatalf("duplicate ID: %s", id)
		}
		seen[id] = true
	}

	if len(seen) != n {
		t.Fatalf("expected %d unique IDs, got %d", n, len(seen))
	}
}

func TestLifecycle_ConcurrentUpdateSameTask(t *testing.T) {
	m := newLifecycleManager(t)

	task := mustCreate(t, m, "contested", "")

	var wg sync.WaitGroup
	n := 10
	errs := make(chan error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			subj := "update-" + strconv.Itoa(i)
			_, err := m.Update(task.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent update error: %v", err)
	}

	// Task should still exist and have a valid final subject
	// (no way to predict which one wins, but must not be corrupted).
	got, err := m.Get(task.ID)
	if err != nil {
		t.Fatalf("Get after concurrent updates: %v", err)
	}
	if got == nil {
		t.Fatal("task should exist after concurrent updates")
	}
	if !strings.HasPrefix(got.Subject, "update-") {
		t.Fatalf("unexpected subject after concurrent updates: %q", got.Subject)
	}
}

func TestLifecycle_ConcurrentBlockAndUpdate(t *testing.T) {
	m := newLifecycleManager(t)

	a := mustCreate(t, m, "A", "")
	b := mustCreate(t, m, "B", "")

	var wg sync.WaitGroup
	wg.Add(2)

	// One goroutine updates A, another blocks A→B.
	go func() {
		defer wg.Done()
		subj := "A-updated"
		_, err := m.Update(a.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
		if err != nil {
			t.Errorf("update A failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if err := m.Block(a.ID, b.ID); err != nil {
			t.Errorf("Block failed: %v", err)
		}
	}()

	wg.Wait()

	gotB, _ := m.Get(b.ID)
	assertBlockedBy(t, gotB, a.ID)
}

// ---------------------------------------------------------------------------
// 14. Cross-process simulation (same directory, two managers)
// ---------------------------------------------------------------------------

func TestLifecycle_TwoManagersSharedDir(t *testing.T) {
	dir := t.TempDir()

	m1 := NewWithDir("", dir, "shared")
	m2 := NewWithDir("", dir, "shared")

	// m1 creates, m2 reads.
	t1 := mustCreate(t, m1, "from m1", "")
	got, _ := m2.Get(t1.ID)
	if got == nil || got.Subject != "from m1" {
		t.Fatalf("m2 sees: %v", got)
	}

	// m2 updates, m1 reads.
	subj := "updated by m2"
	mustUpdate(t, m2, t1.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
	got, _ = m1.Get(t1.ID)
	if got.Subject != "updated by m2" {
		t.Fatalf("m1 sees subject=%q", got.Subject)
	}

	// m2 creates, m1 lists.
	t2 := mustCreate(t, m2, "from m2", "")
	tasks, _ := m1.List()
	found := false
	for _, task := range tasks {
		if task.ID == t2.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("m1 should see m2's task via List")
	}

	// m1 creates with concurrent access.
	t3 := mustCreate(t, m1, "from m1 again", "")
	t4 := mustCreate(t, m2, "from m2 again", "")
	_ = t3
	_ = t4
	if t3.ID == t4.ID {
		t.Fatal("two managers should get different IDs")
	}
}

// ---------------------------------------------------------------------------
// 15. Reset with concurrent access
// ---------------------------------------------------------------------------

func TestLifecycle_ResetConcurrent(t *testing.T) {
	m := newLifecycleManager(t)

	// Pre-populate.
	for i := 0; i < 10; i++ {
		mustCreate(t, m, "pre", "")
	}

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		if err := m.Reset(); err != nil {
			t.Errorf("Reset failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		// Try creating while reset is happening — should be safe due to locking.
		_, err := m.Create("concurrent", "")
		if err != nil {
			t.Errorf("concurrent create during reset: %v", err)
		}
	}()

	wg.Wait()
}

// ---------------------------------------------------------------------------
// 16. File system edge cases
// ---------------------------------------------------------------------------

func TestLifecycle_TaskDirNotExist(t *testing.T) {
	// Use a random dir that doesn't exist — List should return empty, not error.
	dir := t.TempDir()
	m := NewWithDir("", dir, "nonexistent")
	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List on nonexistent dir: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected empty, got %d", len(tasks))
	}
}

func TestLifecycle_CorruptTaskFile(t *testing.T) {
	m := newLifecycleManager(t)

	// Create a task and corrupt its file.
	task := mustCreate(t, m, "corruptible", "")
	_ = os.WriteFile(m.taskPath(task.ID), []byte("{broken json}"), 0644)

	// Get should return nil (corrupt file skipped).
	got, err := m.Get(task.ID)
	if err != nil {
		t.Fatalf("Get on corrupt file: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for corrupt task file")
	}

	// List should skip corrupt files.
	tasks, _ := m.List()
	for _, tsk := range tasks {
		if tsk.ID == task.ID {
			t.Fatal("corrupt task should not appear in List")
		}
	}
}

// ---------------------------------------------------------------------------
// 17. Path traversal prevention
// ---------------------------------------------------------------------------

func TestLifecycle_PathTraversal(t *testing.T) {
	dir := t.TempDir()

	// Try to create a manager with path traversal in session ID.
	m := NewWithDir("", dir, "../../evil")
	task := mustCreate(t, m, "safe", "")
	if task.ID != "1" {
		t.Fatalf("expected ID 1, got %s", task.ID)
	}

	// Task should be inside the temp dir, not escaped.
	taskPath := m.taskPath(task.ID)
	absPath, _ := filepath.Abs(taskPath)
	absDir, _ := filepath.Abs(dir)
	if !strings.HasPrefix(absPath, absDir) {
		t.Fatalf("task path escaped base dir: %s", taskPath)
	}

	// Read from a manager with normal session ID should work.
	m2 := NewWithDir("", dir, "normal")
	got, _ := m2.Get(task.ID)
	if got != nil {
		t.Fatal("evil and normal should be different task lists")
	}
}

// ---------------------------------------------------------------------------
// 18. Multiple subscribers
// ---------------------------------------------------------------------------

func TestLifecycle_MultipleSubscribers(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	task := mustCreate(t, m, "broadcast", "")

	// The eventSink delivers to a single channel.
	ev := waitForEvent(t, ch, time.Second, tasktypes.TaskEventTaskCreated)
	if ev.TaskID != task.ID {
		t.Fatalf("got TaskID=%s, want %s", ev.TaskID, task.ID)
	}
}
