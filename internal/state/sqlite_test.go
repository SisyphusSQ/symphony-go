package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreCreatesMigratesAndPersistsRunSessionRetryAndEvent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.sqlite")
	store, err := OpenSQLiteStore(path, WithInstanceID("test-instance"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	run := Run{
		ID:          "run-1",
		IssueID:     "issue-1",
		IssueKey:    "TOO-1",
		Attempt:     0,
		WorkflowRef: "/repo/WORKFLOW.md",
		StartedAt:   started,
	}
	if err := store.ClaimRun(ctx, run, started.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun() error = %v", err)
	}
	run.WorkspacePath = "/tmp/workspaces/TOO-1"
	run.SessionID = "thread-1-turn-1"
	run.ThreadID = "thread-1"
	run.TurnID = "turn-1"
	run.UpdatedAt = started.Add(time.Second)
	if err := store.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun() error = %v", err)
	}
	if err := store.RecordSession(ctx, Session{
		ID:            "thread-1-turn-1",
		RunID:         "run-1",
		IssueID:       "issue-1",
		IssueKey:      "TOO-1",
		ThreadID:      "thread-1",
		TurnID:        "turn-1",
		Status:        "completed",
		Summary:       "done",
		WorkspacePath: "/tmp/workspaces/TOO-1",
		InputTokens:   10,
		OutputTokens:  20,
		TotalTokens:   30,
		TurnCount:     1,
		CreatedAt:     started.Add(time.Second),
		UpdatedAt:     started.Add(time.Second),
	}); err != nil {
		t.Fatalf("RecordSession() error = %v", err)
	}
	finished := started.Add(2 * time.Second)
	if err := store.CompleteRun(ctx, "run-1", RunStatusCompleted, finished, ""); err != nil {
		t.Fatalf("CompleteRun() error = %v", err)
	}
	if err := store.UpsertRetry(ctx, Retry{
		RunID:     "run-1",
		IssueID:   "issue-1",
		IssueKey:  "TOO-1",
		Attempt:   1,
		DueAt:     started.Add(3 * time.Second),
		CreatedAt: started.Add(2 * time.Second),
		UpdatedAt: started.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("UpsertRetry() error = %v", err)
	}
	if err := store.RecordEvent(ctx, Event{
		ID:          "event-1",
		RunID:       "run-1",
		IssueID:     "issue-1",
		IssueKey:    "TOO-1",
		SessionID:   "thread-1-turn-1",
		Type:        "agent.run.completed",
		PayloadJSON: `{"ok":true}`,
		CreatedAt:   started.Add(2 * time.Second),
	}); err != nil {
		t.Fatalf("RecordEvent() error = %v", err)
	}

	recovery, err := store.RecoverStartup(ctx, started.Add(4*time.Second))
	if err != nil {
		t.Fatalf("RecoverStartup() error = %v", err)
	}
	if len(recovery.InterruptedRuns) != 0 {
		t.Fatalf("InterruptedRuns = %#v, want none for completed run", recovery.InterruptedRuns)
	}
	if len(recovery.Retries) != 1 || recovery.Retries[0].IssueID != "issue-1" ||
		recovery.Retries[0].Attempt != 1 {
		t.Fatalf("Retries = %#v, want persisted retry", recovery.Retries)
	}
	if got := scalarString(t, store.db, `SELECT status FROM runs WHERE id = 'run-1'`); got != "completed" {
		t.Fatalf("run status = %q, want completed", got)
	}
	if got := scalarInt(t, store.db, `SELECT count(*) FROM sessions`); got != 1 {
		t.Fatalf("sessions count = %d, want 1", got)
	}
	if got := scalarInt(t, store.db, `SELECT count(*) FROM agent_events`); got != 1 {
		t.Fatalf("agent_events count = %d, want 1", got)
	}
}

func TestSQLiteStoreRestartRecoveryTurnsRunningRowsIntoSingleRetry(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.sqlite")
	started := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	store, err := OpenSQLiteStore(path, WithInstanceID("first"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	if err := store.ClaimRun(ctx, Run{
		ID:        "run-interrupted",
		IssueID:   "issue-2",
		IssueKey:  "TOO-2",
		Attempt:   2,
		StartedAt: started,
	}, started.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun() error = %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reopened, err := OpenSQLiteStore(path, WithInstanceID("second"))
	if err != nil {
		t.Fatalf("reopen OpenSQLiteStore() error = %v", err)
	}
	defer reopened.Close()

	recovery, err := reopened.RecoverStartup(ctx, started.Add(time.Minute))
	if err != nil {
		t.Fatalf("RecoverStartup() error = %v", err)
	}
	if len(recovery.InterruptedRuns) != 1 || recovery.InterruptedRuns[0].ID != "run-interrupted" {
		t.Fatalf("InterruptedRuns = %#v, want interrupted run", recovery.InterruptedRuns)
	}
	if len(recovery.Retries) != 1 || recovery.Retries[0].IssueID != "issue-2" ||
		recovery.Retries[0].Attempt != 3 || recovery.Retries[0].Error != RestartRecoveryError {
		t.Fatalf("Retries = %#v, want single restart recovery retry", recovery.Retries)
	}
	if got := scalarString(t, reopened.db, `SELECT status FROM runs WHERE id = 'run-interrupted'`); got != "interrupted" {
		t.Fatalf("run status = %q, want interrupted", got)
	}

	secondRecovery, err := reopened.RecoverStartup(ctx, started.Add(2*time.Minute))
	if err != nil {
		t.Fatalf("second RecoverStartup() error = %v", err)
	}
	if len(secondRecovery.InterruptedRuns) != 0 || len(secondRecovery.Retries) != 1 {
		t.Fatalf("second recovery = %#v, want no duplicate interrupted runs and one retry", secondRecovery)
	}
	if got := scalarInt(t, reopened.db, `SELECT count(*) FROM retry_queue WHERE issue_id = 'issue-2'`); got != 1 {
		t.Fatalf("retry rows = %d, want 1", got)
	}
}

func TestSQLiteStoreClaimRejectsActiveLeaseAndAllowsExpiredLease(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	run := Run{ID: "run-active", IssueID: "issue-3", IssueKey: "TOO-3", StartedAt: started}
	if err := store.ClaimRun(ctx, run, started.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimRun(active) error = %v", err)
	}
	err = store.ClaimRun(ctx, Run{
		ID:        "run-conflict",
		IssueID:   "issue-3",
		IssueKey:  "TOO-3",
		StartedAt: started.Add(time.Second),
	}, started.Add(2*time.Minute))
	if !errors.Is(err, ErrRunClaimed) {
		t.Fatalf("ClaimRun(conflict) error = %v, want ErrRunClaimed", err)
	}

	expired := Run{ID: "run-expired", IssueID: "issue-4", IssueKey: "TOO-4", StartedAt: started}
	if err := store.ClaimRun(ctx, expired, started.Add(-time.Second)); err != nil {
		t.Fatalf("ClaimRun(expired fixture) error = %v", err)
	}
	if err := store.ClaimRun(ctx, Run{
		ID:        "run-after-expiry",
		IssueID:   "issue-4",
		IssueKey:  "TOO-4",
		StartedAt: started.Add(time.Second),
	}, started.Add(time.Minute)); err != nil {
		t.Fatalf("ClaimRun(after expiry) error = %v", err)
	}
	if got := scalarString(t, store.db, `SELECT status FROM runs WHERE id = 'run-expired'`); got != "interrupted" {
		t.Fatalf("expired run status = %q, want interrupted", got)
	}
}

func TestOpenSQLiteStoreRejectsCorruptDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.sqlite")
	if err := os.WriteFile(path, []byte("not a sqlite database"), 0o644); err != nil {
		t.Fatalf("write corrupt db: %v", err)
	}
	store, err := OpenSQLiteStore(path)
	if err == nil {
		_ = store.Close()
		t.Fatal("OpenSQLiteStore(corrupt) returned nil error")
	}
}

func scalarString(t *testing.T, db *sql.DB, query string) string {
	t.Helper()
	var value string
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}

func scalarInt(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query).Scan(&value); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
	return value
}
