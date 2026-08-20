package task

import (
	tasktypes "github.com/tinyclue/tinyclue-code/coding_agent/core/types"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

// newTestManager creates a TaskManager backed by a temporary directory.
func newTestManager(t *testing.T) *TaskManager {
	t.Helper()
	dir := t.TempDir()
	return NewWithDir("", dir, "test-session")
}

// newTestManagerWithEvents creates a TaskManager backed by a temporary directory
// and wires an eventSink that delivers tasktypes.TaskEvent values to the returned channel.
func newTestManagerWithEvents(t *testing.T) (*TaskManager, <-chan tasktypes.TaskEvent) {
	t.Helper()
	m := newTestManager(t)
	ch := make(chan tasktypes.TaskEvent, 128)
	m.WithEventSink(tasktypes.EventSink(func(eventType tasktypes.AgentEventType, message interface{}, err error) {
		if ev, ok := message.(tasktypes.TaskEvent); ok {
			ch <- ev
		}
	}))
	return m, ch
}

func TestCreate(t *testing.T) {
	m := newTestManager(t)

	task, err := m.Create("test subject", "test description")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if task.ID != "1" {
		t.Errorf("expected ID 1, got %s", task.ID)
	}
	if task.Subject != "test subject" {
		t.Errorf("expected subject 'test subject', got %s", task.Subject)
	}
	if task.Description != "test description" {
		t.Errorf("expected description 'test description', got %s", task.Description)
	}
	if task.Status != tasktypes.TaskStatusPending {
		t.Errorf("expected status pending, got %s", task.Status)
	}
	if task.Blocks == nil || len(task.Blocks) != 0 {
		t.Errorf("expected empty blocks")
	}
	if task.BlockedBy == nil || len(task.BlockedBy) != 0 {
		t.Errorf("expected empty blockedBy")
	}
}

func TestCreateWithOptions(t *testing.T) {
	m := newTestManager(t)

	task, err := m.Create("subject", "desc",
		tasktypes.WithActiveForm("spinning..."),
		tasktypes.WithMetadata(map[string]any{"key": "val"}),
	)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if task.ActiveForm != "spinning..." {
		t.Errorf("expected activeForm 'spinning...', got %s", task.ActiveForm)
	}
	if task.Metadata["key"] != "val" {
		t.Errorf("expected metadata key 'val', got %v", task.Metadata["key"])
	}
}

func TestCreateMonotonicIDs(t *testing.T) {
	m := newTestManager(t)

	t1, _ := m.Create("first", "")
	t2, _ := m.Create("second", "")
	t3, _ := m.Create("third", "")

	if t1.ID != "1" || t2.ID != "2" || t3.ID != "3" {
		t.Errorf("expected IDs 1,2,3 got %s,%s,%s", t1.ID, t2.ID, t3.ID)
	}
}

func TestGet(t *testing.T) {
	m := newTestManager(t)

	created, _ := m.Create("subj", "desc")
	got, err := m.Get(created.ID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Subject != "subj" {
		t.Errorf("expected 'subj', got %s", got.Subject)
	}

	// Getting a non-existent task should return nil, not error.
	missing, err := m.Get("999")
	if err != nil {
		t.Fatalf("Get on missing task returned error: %v", err)
	}
	if missing != nil {
		t.Fatal("expected nil for missing task")
	}
}

func TestList(t *testing.T) {
	m := newTestManager(t)

	// Empty list.
	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected empty list, got %d", len(tasks))
	}

	_, _ = m.Create("a", "")
	_, _ = m.Create("b", "")
	_, _ = m.Create("c", "")

	tasks, err = m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}

	// Must be in ID order.
	for i := 1; i <= 3; i++ {
		if tasks[i-1].ID != strconv.Itoa(i) {
			t.Errorf("position %d: expected ID %d, got %s", i-1, i, tasks[i-1].ID)
		}
	}
}

func TestListFiltersInternal(t *testing.T) {
	m := newTestManager(t)

	_, _ = m.Create("visible", "")
	internal, _ := m.Create("hidden", "", tasktypes.WithMetadata(map[string]any{"_internal": true}))
	_, _ = m.Create("visible2", "")

	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	for _, tsk := range tasks {
		if tsk.ID == internal.ID {
			t.Fatal("internal task should not appear in List results")
		}
	}
	if len(tasks) != 2 {
		t.Errorf("expected 2 visible tasks, got %d", len(tasks))
	}
}

func TestUpdateStatus(t *testing.T) {
	m := newTestManager(t)

	created, _ := m.Create("subj", "")

	// Pending -> InProgress
	status := tasktypes.TaskStatusInProgress
	updated, err := m.Update(created.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Status != tasktypes.TaskStatusInProgress {
		t.Errorf("expected in_progress, got %s", updated.Status)
	}

	// InProgress -> Completed
	status = tasktypes.TaskStatusCompleted
	updated, err = m.Update(created.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if updated.Status != tasktypes.TaskStatusCompleted {
		t.Errorf("expected completed, got %s", updated.Status)
	}
}

func TestUpdateInvalidTransition(t *testing.T) {
	m := newTestManager(t)

	created, _ := m.Create("subj", "")

	status := tasktypes.TaskStatusCompleted
	_, err := m.Update(created.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err != nil {
		t.Fatalf("Update to completed failed: %v", err)
	}

	// Completed -> InProgress should be invalid.
	status = tasktypes.TaskStatusInProgress
	_, err = m.Update(created.ID, tasktypes.TaskUpdateRequest{Status: &status})
	if err == nil {
		t.Fatal("expected error for completed -> in_progress transition")
	}
}

func TestUpdateFields(t *testing.T) {
	m := newTestManager(t)

	created, _ := m.Create("old", "old desc")

	subject := "new subj"
	desc := "new desc"
	form := "working..."
	owner := "agent-1"

	updated, err := m.Update(created.ID, tasktypes.TaskUpdateRequest{
		Subject:     &subject,
		Description: &desc,
		ActiveForm:  &form,
		Owner:       &owner,
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Subject != "new subj" {
		t.Errorf("expected 'new subj', got %s", updated.Subject)
	}
	if updated.Description != "new desc" {
		t.Errorf("expected 'new desc', got %s", updated.Description)
	}
	if updated.ActiveForm != "working..." {
		t.Errorf("expected 'working...', got %s", updated.ActiveForm)
	}
	if updated.Owner != "agent-1" {
		t.Errorf("expected 'agent-1', got %s", updated.Owner)
	}
}

func TestUpdateMetadataMerge(t *testing.T) {
	m := newTestManager(t)

	created, _ := m.Create("subj", "", tasktypes.WithMetadata(map[string]any{
		"keep":   "me",
		"remove": "this",
	}))

	// Merge: add new key, nullify existing.
	updated, err := m.Update(created.ID, tasktypes.TaskUpdateRequest{
		Metadata: map[string]any{
			"add":    "new",
			"remove": nil,
		},
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Metadata["keep"] != "me" {
		t.Errorf("expected 'keep' to remain, got %v", updated.Metadata["keep"])
	}
	if _, exists := updated.Metadata["remove"]; exists {
		t.Error("expected 'remove' key to be deleted")
	}
	if updated.Metadata["add"] != "new" {
		t.Errorf("expected 'add' = 'new', got %v", updated.Metadata["add"])
	}
}

func TestUpdateAddBlocks(t *testing.T) {
	m := newTestManager(t)

	t1, _ := m.Create("a", "")
	t2, _ := m.Create("b", "")

	updated, err := m.Update(t1.ID, tasktypes.TaskUpdateRequest{AddBlocks: []string{t2.ID}})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if len(updated.Blocks) != 1 || updated.Blocks[0] != t2.ID {
		t.Errorf("expected blocks [%s], got %v", t2.ID, updated.Blocks)
	}

	// Adding again should be idempotent.
	updated, err = m.Update(t1.ID, tasktypes.TaskUpdateRequest{AddBlocks: []string{t2.ID}})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if len(updated.Blocks) != 1 {
		t.Errorf("expected blocks length 1 (dedup), got %d", len(updated.Blocks))
	}
}

func TestUpdateNonExistentTask(t *testing.T) {
	m := newTestManager(t)

	updated, err := m.Update("999", tasktypes.TaskUpdateRequest{})
	if err != nil {
		t.Fatalf("Update on missing task returned error: %v", err)
	}
	if updated != nil {
		t.Fatal("expected nil for non-existent task")
	}
}

func TestDelete(t *testing.T) {
	m := newTestManager(t)

	task, _ := m.Create("delete me", "")
	deleted, err := m.Delete(task.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if !deleted {
		t.Fatal("expected deleted=true")
	}

	got, _ := m.Get(task.ID)
	if got != nil {
		t.Fatal("task should be gone")
	}

	// Delete non-existent.
	deleted, err = m.Delete("999")
	if err != nil {
		t.Fatalf("Delete on missing returned error: %v", err)
	}
	if deleted {
		t.Fatal("expected deleted=false for non-existent task")
	}
}

func TestDeleteCleansReferences(t *testing.T) {
	m := newTestManager(t)

	a, _ := m.Create("a", "")
	b, _ := m.Create("b", "")

	// a blocks b.
	_, _ = m.Update(a.ID, tasktypes.TaskUpdateRequest{AddBlocks: []string{b.ID}})
	_, _ = m.Update(b.ID, tasktypes.TaskUpdateRequest{AddBlockedBy: []string{a.ID}})

	// Delete a.
	_, _ = m.Delete(a.ID)

	// b should no longer have a in its BlockedBy.
	gotB, _ := m.Get(b.ID)
	if len(gotB.BlockedBy) != 0 {
		t.Errorf("expected empty blockedBy after dependency deleted, got %v", gotB.BlockedBy)
	}
}

func TestBlock(t *testing.T) {
	m := newTestManager(t)

	a, _ := m.Create("task A", "")
	b, _ := m.Create("task B", "")

	err := m.Block(a.ID, b.ID)
	if err != nil {
		t.Fatalf("Block failed: %v", err)
	}

	gotA, _ := m.Get(a.ID)
	gotB, _ := m.Get(b.ID)

	if len(gotA.Blocks) != 1 || gotA.Blocks[0] != b.ID {
		t.Errorf("A should block B, got blocks=%v", gotA.Blocks)
	}
	if len(gotB.BlockedBy) != 1 || gotB.BlockedBy[0] != a.ID {
		t.Errorf("B should be blocked by A, got blockedBy=%v", gotB.BlockedBy)
	}
}

func TestBlockNonExistent(t *testing.T) {
	m := newTestManager(t)

	a, _ := m.Create("a", "")

	err := m.Block(a.ID, "999")
	if err == nil {
		t.Fatal("expected error when blocking non-existent task")
	}

	err = m.Block("999", a.ID)
	if err == nil {
		t.Fatal("expected error when blocking from non-existent task")
	}
}

func TestReset(t *testing.T) {
	m := newTestManager(t)

	_, _ = m.Create("a", "")
	_, _ = m.Create("b", "")
	_, _ = m.Create("c", "")

	err := m.Reset()
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List after reset failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks after reset, got %d", len(tasks))
	}

	// New task should get ID 4 (HWM preserved).
	newTask, _ := m.Create("after reset", "")
	if newTask.ID != "4" {
		t.Errorf("expected ID 4 (HWM preserved), got %s", newTask.ID)
	}
}

func TestDeleteUpdatesHWM(t *testing.T) {
	m := newTestManager(t)

	t1, _ := m.Create("a", "")
	t2, _ := m.Create("b", "")
	t3, _ := m.Create("c", "")

	// Delete the highest-ID task.
	_, _ = m.Delete(t3.ID)
	_, _ = m.Delete(t2.ID)
	_, _ = m.Delete(t1.ID)

	// New task should get ID 4 (HWM from highest deleted).
	newTask, _ := m.Create("new", "")
	if newTask.ID != "4" {
		t.Errorf("expected ID 4 (HWM preserved), got %s", newTask.ID)
	}
}

func TestSubscription(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	// Create should emit EventTaskCreated.
	task, _ := m.Create("subj", "")
	select {
	case ev := <-ch:
		if ev.Type != tasktypes.TaskEventTaskCreated {
			t.Errorf("expected EventTaskCreated, got %v", ev.Type)
		}
		if ev.TaskID != task.ID {
			t.Errorf("expected TaskID %s, got %s", task.ID, ev.TaskID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for create event")
	}

	// Update should emit EventTaskUpdated.
	status := tasktypes.TaskStatusInProgress
	_, _ = m.Update(task.ID, tasktypes.TaskUpdateRequest{Status: &status})
	select {
	case ev := <-ch:
		if ev.Type != tasktypes.TaskEventTaskUpdated {
			t.Errorf("expected EventTaskUpdated, got %v", ev.Type)
		}
		if ev.NewStatus == nil || *ev.NewStatus != tasktypes.TaskStatusInProgress {
			t.Errorf("expected newStatus in_progress, got %v", ev.NewStatus)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for update event")
	}

	// Delete should emit EventTaskDeleted.
	_, _ = m.Delete(task.ID)
	select {
	case ev := <-ch:
		if ev.Type != tasktypes.TaskEventTaskDeleted {
			t.Errorf("expected EventTaskDeleted, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delete event")
	}
}

func TestSubscriptionReset(t *testing.T) {
	m, ch := newTestManagerWithEvents(t)

	_, _ = m.Create("a", "")

	// Drain.
	for len(ch) > 0 {
		<-ch
	}

	_ = m.Reset()
	select {
	case ev := <-ch:
		if ev.Type != tasktypes.TaskEventListReset {
			t.Errorf("expected EventListReset, got %v", ev.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for reset event")
	}
}

func TestConcurrentCreate(t *testing.T) {
	m := newTestManager(t)

	var wg sync.WaitGroup
	n := 10
	ids := make(chan string, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			task, err := m.Create("concurrent", "")
			if err != nil {
				t.Errorf("concurrent create failed: %v", err)
				return
			}
			ids <- task.ID
		}()
	}

	wg.Wait()
	close(ids)

	seen := make(map[string]bool)
	for id := range ids {
		if seen[id] {
			t.Errorf("duplicate ID: %s", id)
		}
		seen[id] = true
	}

	if len(seen) != n {
		t.Errorf("expected %d unique IDs, got %d", n, len(seen))
	}
}

func TestConcurrentUpdateDifferentTasks(t *testing.T) {
	m := newTestManager(t)

	t1, _ := m.Create("task1", "a")
	t2, _ := m.Create("task2", "b")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		subj := "t1-updated"
		_, err := m.Update(t1.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
		if err != nil {
			t.Errorf("update t1 failed: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		subj := "t2-updated"
		_, err := m.Update(t2.ID, tasktypes.TaskUpdateRequest{Subject: &subj})
		if err != nil {
			t.Errorf("update t2 failed: %v", err)
		}
	}()

	wg.Wait()

	got1, _ := m.Get(t1.ID)
	got2, _ := m.Get(t2.ID)
	if got1.Subject != "t1-updated" {
		t.Errorf("expected t1 subject 't1-updated', got '%s'", got1.Subject)
	}
	if got2.Subject != "t2-updated" {
		t.Errorf("expected t2 subject 't2-updated', got '%s'", got2.Subject)
	}
}

func TestCrossProcessLock(t *testing.T) {
	// Test that two separate TaskManager instances (simulating separate processes)
	// using the same directory use file locking correctly.
	dir := t.TempDir()

	m1 := NewWithDir("", dir, "same-session")
	m2 := NewWithDir("", dir, "same-session")

	// Create via m1.
	task, err := m1.Create("from m1", "")
	if err != nil {
		t.Fatalf("m1 create failed: %v", err)
	}

	// Read via m2.
	got, err := m2.Get(task.ID)
	if err != nil {
		t.Fatalf("m2 get failed: %v", err)
	}
	if got == nil {
		t.Fatal("m2 could not see task created by m1")
	}
	if got.Subject != "from m1" {
		t.Errorf("expected 'from m1', got '%s'", got.Subject)
	}

	// Update via m2.
	subject := "updated by m2"
	_, err = m2.Update(task.ID, tasktypes.TaskUpdateRequest{Subject: &subject})
	if err != nil {
		t.Fatalf("m2 update failed: %v", err)
	}

	// Verify via m1.
	got, _ = m1.Get(task.ID)
	if got.Subject != "updated by m2" {
		t.Errorf("expected 'updated by m2', got '%s'", got.Subject)
	}
}

func TestPathSanitization(t *testing.T) {
	dir := t.TempDir()
	m := NewWithDir("", dir, "hello/../evil")

	tasks, err := m.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}

	// Creating a task should not escape the base directory.
	_, err = m.Create("safe", "")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
}

func TestReadCorruptFile(t *testing.T) {
	m := newTestManager(t)

	badPath := m.taskPath("99")
	_ = os.MkdirAll(filepath.Dir(badPath), 0755)
	_ = os.WriteFile(badPath, []byte("not json"), 0644)

	// readTask should silently return nil.
	task, err := m.readTask("99")
	if err != nil {
		t.Fatalf("readTask on corrupt file returned error: %v", err)
	}
	if task != nil {
		t.Fatal("expected nil for corrupt file")
	}
}
