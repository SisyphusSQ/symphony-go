package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	sqliteDriverName  = "sqlite"
	defaultInstanceID = "local"
	timeFormat        = time.RFC3339Nano
)

// SQLiteStore persists orchestration state in one local SQLite database.
type SQLiteStore struct {
	db         *sql.DB
	instanceID string
}

type sqliteOptions struct {
	instanceID string
}

// SQLiteOption customizes SQLiteStore construction.
type SQLiteOption func(*sqliteOptions)

// WithInstanceID records the owner string used by durable run claims.
func WithInstanceID(instanceID string) SQLiteOption {
	return func(opts *sqliteOptions) {
		opts.instanceID = strings.TrimSpace(instanceID)
	}
}

// OpenSQLiteStore opens or creates a SQLite state database and runs migrations.
func OpenSQLiteStore(path string, opts ...SQLiteOption) (*SQLiteStore, error) {
	options := sqliteOptions{instanceID: defaultInstanceID}
	for _, opt := range opts {
		opt(&options)
	}
	if options.instanceID == "" {
		options.instanceID = defaultInstanceID
	}

	cleanPath := strings.TrimSpace(path)
	if cleanPath == "" {
		return nil, fmt.Errorf("%w: sqlite path is required", ErrInvalidStoreRequest)
	}
	if err := os.MkdirAll(filepath.Dir(cleanPath), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite state directory: %w", err)
	}

	db, err := sql.Open(sqliteDriverName, cleanPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db, instanceID: options.instanceID}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

// NewSQLiteStore wraps an existing database handle. It is useful in tests.
func NewSQLiteStore(db *sql.DB, opts ...SQLiteOption) (*SQLiteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	db.SetMaxOpenConns(1)
	options := sqliteOptions{instanceID: defaultInstanceID}
	for _, opt := range opts {
		opt(&options)
	}
	if options.instanceID == "" {
		options.instanceID = defaultInstanceID
	}
	store := &SQLiteStore{db: db, instanceID: options.instanceID}
	if err := store.migrate(context.Background()); err != nil {
		return nil, err
	}
	return store, nil
}

// Close closes the underlying database handle.
func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	statements := []string{
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version integer primary key,
			applied_at text not null
		)`,
		`CREATE TABLE IF NOT EXISTS runs (
			id text primary key,
			instance_id text not null,
			issue_id text not null,
			issue_identifier text not null,
			status text not null,
			attempt integer not null,
			workspace_path text not null default '',
			session_id text not null default '',
			thread_id text not null default '',
			turn_id text not null default '',
			workflow_ref text not null default '',
			claimed_by text not null default '',
			lease_expires_at text,
			error text not null default '',
			started_at text not null,
			finished_at text,
			created_at text not null,
			updated_at text not null
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_issue_status
			ON runs(issue_id, status, lease_expires_at)`,
		`CREATE TABLE IF NOT EXISTS retry_queue (
			issue_id text primary key,
			run_id text not null default '',
			issue_identifier text not null,
			attempt integer not null,
			due_at text not null,
			backoff_ms integer not null default 0,
			error text not null default '',
			created_at text not null,
			updated_at text not null
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			session_id text primary key,
			run_id text not null,
			issue_id text not null,
			issue_identifier text not null,
			thread_id text not null default '',
			turn_id text not null default '',
			status text not null default '',
			summary text not null default '',
			workspace_path text not null default '',
			input_tokens integer not null default 0,
			output_tokens integer not null default 0,
			reasoning_tokens integer not null default 0,
			total_tokens integer not null default 0,
			cached_tokens integer not null default 0,
			turn_count integer not null default 0,
			created_at text not null,
			updated_at text not null
		)`,
		`CREATE TABLE IF NOT EXISTS agent_events (
			id text primary key,
			run_id text not null default '',
			issue_id text not null default '',
			issue_identifier text not null default '',
			session_id text not null default '',
			event_type text not null,
			payload_json text not null default '{}',
			created_at text not null
		)`,
		`INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES(1, ?)`,
	}

	for _, statement := range statements {
		args := []any{}
		if strings.Contains(statement, "VALUES(1, ?)") {
			args = append(args, formatTime(time.Now().UTC()))
		}
		if _, err := s.db.ExecContext(ctx, statement, args...); err != nil {
			return fmt.Errorf("migrate sqlite state store: %w", err)
		}
	}
	return nil
}

// ClaimRun records a running issue attempt if no active claim exists.
func (s *SQLiteStore) ClaimRun(ctx context.Context, run Run, leaseExpiresAt time.Time) error {
	if err := validateRun(run); err != nil {
		return err
	}
	now := normalizedTime(run.StartedAt)
	run.Status = RunStatusRunning
	run.InstanceID = firstNonEmpty(run.InstanceID, s.instanceID)
	run.ClaimedBy = firstNonEmpty(run.ClaimedBy, s.instanceID)
	run.CreatedAt = normalizedTime(run.CreatedAt)
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	run.UpdatedAt = normalizedTime(run.UpdatedAt)
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = now
	}
	run.LeaseExpiresAt = &leaseExpiresAt

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollback(tx)

	nowText := formatTime(now)
	if _, err := tx.ExecContext(
		ctx,
		`UPDATE runs
		 SET status = ?, error = ?, claimed_by = '', lease_expires_at = NULL, updated_at = ?
		 WHERE issue_id = ? AND status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?`,
		string(RunStatusInterrupted),
		"lease_expired",
		nowText,
		run.IssueID,
		string(RunStatusRunning),
		nowText,
	); err != nil {
		return err
	}

	var existing string
	err = tx.QueryRowContext(
		ctx,
		`SELECT id FROM runs
		 WHERE issue_id = ? AND status = ?
		   AND (lease_expires_at IS NULL OR lease_expires_at > ?)
		 LIMIT 1`,
		run.IssueID,
		string(RunStatusRunning),
		nowText,
	).Scan(&existing)
	switch {
	case err == nil:
		return ErrRunClaimed
	case errors.Is(err, sql.ErrNoRows):
	default:
		return err
	}

	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO runs (
			id, instance_id, issue_id, issue_identifier, status, attempt,
			workspace_path, session_id, thread_id, turn_id, workflow_ref,
			claimed_by, lease_expires_at, error, started_at, finished_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.InstanceID,
		run.IssueID,
		run.IssueKey,
		string(run.Status),
		run.Attempt,
		run.WorkspacePath,
		run.SessionID,
		run.ThreadID,
		run.TurnID,
		run.WorkflowRef,
		run.ClaimedBy,
		formatTime(leaseExpiresAt),
		run.Error,
		formatTime(run.StartedAt),
		formatOptionalTime(run.FinishedAt),
		formatTime(run.CreatedAt),
		formatTime(run.UpdatedAt),
	)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM retry_queue WHERE issue_id = ?`, run.IssueID); err != nil {
		return err
	}
	return tx.Commit()
}

// UpdateRun updates mutable metadata for a claimed run.
func (s *SQLiteStore) UpdateRun(ctx context.Context, run Run) error {
	if strings.TrimSpace(run.ID) == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidStoreRequest)
	}
	now := normalizedTime(run.UpdatedAt)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE runs
		 SET workspace_path = ?, session_id = ?, thread_id = ?, turn_id = ?,
		     workflow_ref = CASE WHEN ? = '' THEN workflow_ref ELSE ? END,
		     updated_at = ?
		 WHERE id = ?`,
		run.WorkspacePath,
		run.SessionID,
		run.ThreadID,
		run.TurnID,
		run.WorkflowRef,
		run.WorkflowRef,
		formatTime(now),
		run.ID,
	)
	return err
}

// CompleteRun marks a run as no longer running and clears its lease.
func (s *SQLiteStore) CompleteRun(
	ctx context.Context,
	runID string,
	status RunStatus,
	finishedAt time.Time,
	errText string,
) error {
	if strings.TrimSpace(runID) == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidStoreRequest)
	}
	if status == "" {
		return fmt.Errorf("%w: run status is required", ErrInvalidStoreRequest)
	}
	now := normalizedTime(finishedAt)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`UPDATE runs
		 SET status = ?, finished_at = ?, error = ?, claimed_by = '',
		     lease_expires_at = NULL, updated_at = ?
		 WHERE id = ?`,
		string(status),
		formatTime(now),
		errText,
		formatTime(now),
		runID,
	)
	return err
}

// UpsertRetry stores or replaces one retry queue row by issue id.
func (s *SQLiteStore) UpsertRetry(ctx context.Context, retry Retry) error {
	if strings.TrimSpace(retry.IssueID) == "" {
		return fmt.Errorf("%w: retry issue id is required", ErrInvalidStoreRequest)
	}
	if retry.Attempt < 1 {
		retry.Attempt = 1
	}
	now := normalizedTime(retry.UpdatedAt)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := normalizedTime(retry.CreatedAt)
	if createdAt.IsZero() {
		createdAt = now
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO retry_queue (
			issue_id, run_id, issue_identifier, attempt, due_at,
			backoff_ms, error, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issue_id) DO UPDATE SET
			run_id = excluded.run_id,
			issue_identifier = excluded.issue_identifier,
			attempt = excluded.attempt,
			due_at = excluded.due_at,
			backoff_ms = excluded.backoff_ms,
			error = excluded.error,
			updated_at = excluded.updated_at`,
		retry.IssueID,
		retry.RunID,
		retry.IssueKey,
		retry.Attempt,
		formatTime(retry.DueAt),
		retry.BackoffMS,
		retry.Error,
		formatTime(createdAt),
		formatTime(now),
	)
	return err
}

// DeleteRetry removes any queued retry row for an issue.
func (s *SQLiteStore) DeleteRetry(ctx context.Context, issueID string) error {
	if strings.TrimSpace(issueID) == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM retry_queue WHERE issue_id = ?`, issueID)
	return err
}

// RecordSession upserts the latest session metadata for a run.
func (s *SQLiteStore) RecordSession(ctx context.Context, session Session) error {
	if strings.TrimSpace(session.ID) == "" {
		return nil
	}
	if strings.TrimSpace(session.RunID) == "" {
		return fmt.Errorf("%w: session run id is required", ErrInvalidStoreRequest)
	}
	now := normalizedTime(session.UpdatedAt)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	createdAt := normalizedTime(session.CreatedAt)
	if createdAt.IsZero() {
		createdAt = now
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO sessions (
			session_id, run_id, issue_id, issue_identifier, thread_id, turn_id,
			status, summary, workspace_path, input_tokens, output_tokens,
			reasoning_tokens, total_tokens, cached_tokens, turn_count,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			run_id = excluded.run_id,
			issue_id = excluded.issue_id,
			issue_identifier = excluded.issue_identifier,
			thread_id = excluded.thread_id,
			turn_id = excluded.turn_id,
			status = excluded.status,
			summary = excluded.summary,
			workspace_path = excluded.workspace_path,
			input_tokens = excluded.input_tokens,
			output_tokens = excluded.output_tokens,
			reasoning_tokens = excluded.reasoning_tokens,
			total_tokens = excluded.total_tokens,
			cached_tokens = excluded.cached_tokens,
			turn_count = excluded.turn_count,
			updated_at = excluded.updated_at`,
		session.ID,
		session.RunID,
		session.IssueID,
		session.IssueKey,
		session.ThreadID,
		session.TurnID,
		session.Status,
		session.Summary,
		session.WorkspacePath,
		session.InputTokens,
		session.OutputTokens,
		session.ReasoningTokens,
		session.TotalTokens,
		session.CachedTokens,
		session.TurnCount,
		formatTime(createdAt),
		formatTime(now),
	)
	return err
}

// RecordEvent inserts one durable event row.
func (s *SQLiteStore) RecordEvent(ctx context.Context, event Event) error {
	if strings.TrimSpace(event.Type) == "" {
		return nil
	}
	if event.ID == "" {
		event.ID = NewID("event")
	}
	if event.PayloadJSON == "" {
		payload, err := json.Marshal(map[string]any{"event_type": event.Type})
		if err != nil {
			return err
		}
		event.PayloadJSON = string(payload)
	}
	createdAt := normalizedTime(event.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO agent_events (
			id, run_id, issue_id, issue_identifier, session_id, event_type,
			payload_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.RunID,
		event.IssueID,
		event.IssueKey,
		event.SessionID,
		event.Type,
		event.PayloadJSON,
		formatTime(createdAt),
	)
	return err
}

// RecoverStartup transitions interrupted running rows and returns retry rows.
func (s *SQLiteStore) RecoverStartup(ctx context.Context, now time.Time) (RecoverySnapshot, error) {
	recoveryTime := normalizedTime(now)
	if recoveryTime.IsZero() {
		recoveryTime = time.Now().UTC()
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	defer rollback(tx)

	rows, err := tx.QueryContext(
		ctx,
		`SELECT id, instance_id, issue_id, issue_identifier, status, attempt,
			workspace_path, session_id, thread_id, turn_id, workflow_ref,
			claimed_by, lease_expires_at, error, started_at, finished_at,
			created_at, updated_at
		 FROM runs
		 WHERE status = ?`,
		string(RunStatusRunning),
	)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	running, err := scanRuns(rows)
	if err != nil {
		return RecoverySnapshot{}, err
	}

	for _, run := range running {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE runs
			 SET status = ?, error = ?, claimed_by = '', lease_expires_at = NULL,
			     finished_at = ?, updated_at = ?
			 WHERE id = ?`,
			string(RunStatusInterrupted),
			RestartRecoveryError,
			formatTime(recoveryTime),
			formatTime(recoveryTime),
			run.ID,
		); err != nil {
			return RecoverySnapshot{}, err
		}

		attempt := run.Attempt + 1
		if attempt < 1 {
			attempt = 1
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO retry_queue (
				issue_id, run_id, issue_identifier, attempt, due_at,
				backoff_ms, error, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(issue_id) DO UPDATE SET
				run_id = excluded.run_id,
				issue_identifier = excluded.issue_identifier,
				attempt = max(retry_queue.attempt, excluded.attempt),
				due_at = excluded.due_at,
				error = excluded.error,
				updated_at = excluded.updated_at`,
			run.IssueID,
			run.ID,
			run.IssueKey,
			attempt,
			formatTime(recoveryTime),
			0,
			RestartRecoveryError,
			formatTime(recoveryTime),
			formatTime(recoveryTime),
		); err != nil {
			return RecoverySnapshot{}, err
		}
	}

	retryRows, err := tx.QueryContext(
		ctx,
		`SELECT run_id, issue_id, issue_identifier, attempt, due_at,
			backoff_ms, error, created_at, updated_at
		 FROM retry_queue
		 ORDER BY due_at, issue_identifier, issue_id`,
	)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	retries, err := scanRetries(retryRows)
	if err != nil {
		return RecoverySnapshot{}, err
	}
	if err := tx.Commit(); err != nil {
		return RecoverySnapshot{}, err
	}
	return RecoverySnapshot{InterruptedRuns: running, Retries: retries}, nil
}

func validateRun(run Run) error {
	if strings.TrimSpace(run.ID) == "" {
		return fmt.Errorf("%w: run id is required", ErrInvalidStoreRequest)
	}
	if strings.TrimSpace(run.IssueID) == "" {
		return fmt.Errorf("%w: run issue id is required", ErrInvalidStoreRequest)
	}
	if run.Attempt < 0 {
		return fmt.Errorf("%w: run attempt must not be negative", ErrInvalidStoreRequest)
	}
	if run.StartedAt.IsZero() {
		return fmt.Errorf("%w: run started_at is required", ErrInvalidStoreRequest)
	}
	return nil
}

func scanRuns(rows *sql.Rows) ([]Run, error) {
	defer rows.Close()
	var runs []Run
	for rows.Next() {
		var run Run
		var status string
		var startedText, createdText, updatedText string
		var leaseText, finishedText sql.NullString
		if err := rows.Scan(
			&run.ID,
			&run.InstanceID,
			&run.IssueID,
			&run.IssueKey,
			&status,
			&run.Attempt,
			&run.WorkspacePath,
			&run.SessionID,
			&run.ThreadID,
			&run.TurnID,
			&run.WorkflowRef,
			&run.ClaimedBy,
			&leaseText,
			&run.Error,
			&startedText,
			&finishedText,
			&createdText,
			&updatedText,
		); err != nil {
			return nil, err
		}
		run.Status = RunStatus(status)
		startedAt, err := parseTime(startedText)
		if err != nil {
			return nil, err
		}
		createdAt, err := parseTime(createdText)
		if err != nil {
			return nil, err
		}
		updatedAt, err := parseTime(updatedText)
		if err != nil {
			return nil, err
		}
		run.StartedAt = startedAt
		run.CreatedAt = createdAt
		run.UpdatedAt = updatedAt
		if leaseText.Valid && leaseText.String != "" {
			parsed, err := parseTime(leaseText.String)
			if err != nil {
				return nil, err
			}
			run.LeaseExpiresAt = &parsed
		}
		if finishedText.Valid && finishedText.String != "" {
			parsed, err := parseTime(finishedText.String)
			if err != nil {
				return nil, err
			}
			run.FinishedAt = &parsed
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanRetries(rows *sql.Rows) ([]Retry, error) {
	defer rows.Close()
	var retries []Retry
	for rows.Next() {
		var retry Retry
		var dueText, createdText, updatedText string
		if err := rows.Scan(
			&retry.RunID,
			&retry.IssueID,
			&retry.IssueKey,
			&retry.Attempt,
			&dueText,
			&retry.BackoffMS,
			&retry.Error,
			&createdText,
			&updatedText,
		); err != nil {
			return nil, err
		}
		dueAt, err := parseTime(dueText)
		if err != nil {
			return nil, err
		}
		createdAt, err := parseTime(createdText)
		if err != nil {
			return nil, err
		}
		updatedAt, err := parseTime(updatedText)
		if err != nil {
			return nil, err
		}
		retry.DueAt = dueAt
		retry.CreatedAt = createdAt
		retry.UpdatedAt = updatedAt
		retries = append(retries, retry)
	}
	return retries, rows.Err()
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func normalizedTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func formatTime(value time.Time) string {
	return normalizedTime(value).Format(timeFormat)
}

func formatOptionalTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return formatTime(*value)
}

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(timeFormat, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
