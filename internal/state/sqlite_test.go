package state

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	"github.com/SisyphusSQ/symphony-go/internal/safety"
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

func TestSQLiteStoreQueryRunEventsProjectsPaginatesFiltersAndRedacts(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	run := Run{
		ID:            "run-events",
		IssueID:       "issue-events",
		IssueKey:      "TOO-140",
		Attempt:       1,
		WorkspacePath: "/tmp/workspaces/TOO-140",
		SessionID:     "session-events",
		ThreadID:      "thread-run",
		TurnID:        "turn-run",
		StartedAt:     started,
	}
	if err := store.ClaimRun(ctx, run, started.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun() error = %v", err)
	}
	if err := store.RecordSession(ctx, Session{
		ID:        "session-events",
		RunID:     "run-events",
		IssueID:   "issue-events",
		IssueKey:  "TOO-140",
		ThreadID:  "thread-session",
		TurnID:    "turn-session",
		Status:    "running",
		Summary:   "running",
		CreatedAt: started,
		UpdatedAt: started,
	}); err != nil {
		t.Fatalf("RecordSession() error = %v", err)
	}
	events := []Event{
		{
			ID:          "event-1",
			RunID:       "run-events",
			IssueID:     "issue-events",
			IssueKey:    "TOO-140",
			SessionID:   "session-events",
			Type:        "agent.turn_started",
			PayloadJSON: `{"kind":"turn_started","message":"turn started"}`,
			CreatedAt:   started.Add(time.Second),
		},
		{
			ID:          "event-2",
			RunID:       "run-events",
			IssueID:     "issue-events",
			IssueKey:    "TOO-140",
			SessionID:   "session-events",
			Type:        "agent.tool_call",
			PayloadJSON: `{"kind":"tool_call","method":"item/tool/call","message":"token=literal-secret","payload":{"tool":"linear_graphql","success":true,"token":"literal-secret"}}`,
			CreatedAt:   started.Add(2 * time.Second),
		},
		{
			ID:          "event-3",
			RunID:       "run-events",
			IssueID:     "issue-events",
			IssueKey:    "TOO-140",
			SessionID:   "session-events",
			Type:        "agent.rate_limits_updated",
			PayloadJSON: `{"kind":"rate_limits_updated","method":"account/rateLimits/updated"}`,
			CreatedAt:   started.Add(3 * time.Second),
		},
	}
	for _, event := range events {
		if err := store.RecordEvent(ctx, event); err != nil {
			t.Fatalf("RecordEvent(%s) error = %v", event.ID, err)
		}
	}

	redactor := safety.NewRedactorFromLiterals("literal-secret")
	page, err := store.QueryRunEvents(ctx, "run-events", observability.TimelineQuery{Limit: 2}, redactor)
	if err != nil {
		t.Fatalf("QueryRunEvents() error = %v", err)
	}
	if len(page.Rows) != 2 || !page.HasMore || page.Rows[0].Sequence != 1 || page.Rows[1].Sequence != 2 {
		t.Fatalf("page = %#v, want first two rows with hasMore", page)
	}
	if page.Rows[1].Category != observability.TimelineCategoryTool {
		t.Fatalf("second category = %q, want tool", page.Rows[1].Category)
	}
	if page.Rows[1].ThreadID != "thread-session" || page.Rows[1].TurnID != "turn-session" {
		t.Fatalf("thread/turn = %q/%q, want session identifiers", page.Rows[1].ThreadID, page.Rows[1].TurnID)
	}
	if strings.Contains(string(page.Rows[1].Payload), "literal-secret") ||
		strings.Contains(page.Rows[1].Summary, "literal-secret") {
		t.Fatalf("tool event leaked secret: summary=%q payload=%s", page.Rows[1].Summary, page.Rows[1].Payload)
	}

	filtered, err := store.QueryRunEvents(ctx, "run-events", observability.TimelineQuery{
		Category: observability.TimelineCategoryResource,
		Limit:    10,
	}, redactor)
	if err != nil {
		t.Fatalf("QueryRunEvents(resource) error = %v", err)
	}
	if len(filtered.Rows) != 1 || filtered.Rows[0].ID != "event-3" || filtered.Rows[0].Sequence != 3 {
		t.Fatalf("filtered rows = %#v, want only event-3 with original sequence", filtered.Rows)
	}

	emptyRun := Run{ID: "run-empty", IssueID: "issue-empty", IssueKey: "TOO-EMPTY", StartedAt: started}
	if err := store.ClaimRun(ctx, emptyRun, started.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun(empty) error = %v", err)
	}
	empty, err := store.QueryRunEvents(ctx, "run-empty", observability.TimelineQuery{Limit: 10}, redactor)
	if err != nil {
		t.Fatalf("QueryRunEvents(empty) error = %v", err)
	}
	if len(empty.Rows) != 0 || empty.HasMore {
		t.Fatalf("empty page = %#v, want no rows and no hasMore", empty)
	}

	if _, err := store.QueryRunEvents(ctx, "missing", observability.TimelineQuery{Limit: 10}, redactor); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("QueryRunEvents(missing) error = %v, want ErrRunNotFound", err)
	}
}

func TestSQLiteStoreLatestRunForIssue(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLiteStore(filepath.Join(t.TempDir(), "state.sqlite"))
	if err != nil {
		t.Fatalf("OpenSQLiteStore() error = %v", err)
	}
	defer store.Close()

	started := time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC)
	firstFinished := started.Add(time.Minute)
	secondFinished := started.Add(2 * time.Minute)
	first := Run{ID: "run-first", IssueID: "issue-latest", IssueKey: "TOO-140", StartedAt: started}
	second := Run{ID: "run-second", IssueID: "issue-latest", IssueKey: "TOO-140", StartedAt: started.Add(time.Minute)}
	if err := store.ClaimRun(ctx, first, first.StartedAt.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun(first) error = %v", err)
	}
	if err := store.CompleteRun(ctx, first.ID, RunStatusCompleted, firstFinished, ""); err != nil {
		t.Fatalf("CompleteRun(first) error = %v", err)
	}
	if err := store.ClaimRun(ctx, second, second.StartedAt.Add(5*time.Minute)); err != nil {
		t.Fatalf("ClaimRun(second) error = %v", err)
	}
	if err := store.CompleteRun(ctx, second.ID, RunStatusFailed, secondFinished, "failed"); err != nil {
		t.Fatalf("CompleteRun(second) error = %v", err)
	}

	latest, err := store.LatestRunForIssue(ctx, "TOO-140")
	if err != nil {
		t.Fatalf("LatestRunForIssue() error = %v", err)
	}
	if latest.Metadata.RunID != "run-second" || latest.Metadata.Status != "failed" {
		t.Fatalf("latest = %#v, want run-second failed", latest.Metadata)
	}
	if _, err := store.LatestRunForIssue(ctx, "TOO-MISSING"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("LatestRunForIssue(missing) error = %v, want ErrRunNotFound", err)
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
