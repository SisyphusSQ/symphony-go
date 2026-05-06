package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

var ErrRunNotFound = errors.New("state_run_not_found")

// QueryStore exposes the durable state read model needed by operator APIs.
type QueryStore interface {
	QueryRuns(context.Context, observability.RunQuery) (observability.RunPage, error)
	GetRun(context.Context, string) (observability.RunDetail, error)
	LatestRunForIssue(context.Context, string) (observability.RunDetail, error)
	QueryRunEvents(
		context.Context,
		string,
		observability.TimelineQuery,
		observability.TimelineRedactor,
	) (observability.TimelinePage, error)
	StateSummary(context.Context, int) (observability.StoreSummary, error)
}

// QueryRuns returns a filtered, stable page of durable run rows.
func (s *SQLiteStore) QueryRuns(ctx context.Context, query observability.RunQuery) (observability.RunPage, error) {
	if s == nil || s.db == nil {
		return observability.RunPage{}, fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}

	rows, err := s.queryStoredRunRows(ctx, query.Issue)
	if err != nil {
		return observability.RunPage{}, err
	}
	retries, err := s.queryRetryRows(ctx, query.Issue)
	if err != nil {
		return observability.RunPage{}, err
	}
	rows = append(rows, retries...)
	rows = filterRowsByStatus(rows, query.Statuses)
	sortRunRows(rows)

	start := query.Offset
	if start > len(rows) {
		start = len(rows)
	}
	end := start + query.Limit
	hasMore := false
	if end < len(rows) {
		hasMore = true
	} else {
		end = len(rows)
	}
	pageRows := make([]observability.RunRow, 0, end-start)
	pageRows = append(pageRows, rows[start:end]...)
	return observability.RunPage{Rows: pageRows, Limit: query.Limit, HasMore: hasMore}, nil
}

// GetRun returns one durable run detail by stable run id.
func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (observability.RunDetail, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return observability.RunDetail{}, ErrRunNotFound
	}
	rows, err := s.queryStoredRunRowsByRunID(ctx, runID)
	if err != nil {
		return observability.RunDetail{}, err
	}
	if len(rows) == 0 {
		return observability.RunDetail{}, ErrRunNotFound
	}
	row := rows[0]
	latest, err := s.latestEventSummary(ctx, runID)
	if err != nil {
		return observability.RunDetail{}, err
	}
	row.LatestEvent = latest
	return detailFromRunRow(row), nil
}

// LatestRunForIssue returns the latest durable run for an issue identifier or id.
func (s *SQLiteStore) LatestRunForIssue(ctx context.Context, issue string) (observability.RunDetail, error) {
	if s == nil || s.db == nil {
		return observability.RunDetail{}, fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return observability.RunDetail{}, ErrRunNotFound
	}
	rows, err := s.queryStoredRunRows(ctx, issue)
	if err != nil {
		return observability.RunDetail{}, err
	}
	if len(rows) == 0 {
		return observability.RunDetail{}, ErrRunNotFound
	}
	row := rows[0]
	latest, err := s.latestEventSummary(ctx, row.RunID)
	if err != nil {
		return observability.RunDetail{}, err
	}
	row.LatestEvent = latest
	return detailFromRunRow(row), nil
}

// QueryRunEvents returns a projected, redacted operation timeline for one run.
func (s *SQLiteStore) QueryRunEvents(
	ctx context.Context,
	runID string,
	query observability.TimelineQuery,
	redactor observability.TimelineRedactor,
) (observability.TimelinePage, error) {
	if s == nil || s.db == nil {
		return observability.TimelinePage{}, fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return observability.TimelinePage{}, ErrRunNotFound
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Offset < 0 {
		query.Offset = 0
	}
	exists, err := s.runExists(ctx, runID)
	if err != nil {
		return observability.TimelinePage{}, err
	}
	if !exists {
		return observability.TimelinePage{}, ErrRunNotFound
	}
	rawEvents, err := s.queryRawTimelineEvents(ctx, runID)
	if err != nil {
		return observability.TimelinePage{}, err
	}

	projected := make([]observability.TimelineEventRow, 0, len(rawEvents))
	for index, raw := range rawEvents {
		row := observability.ProjectTimelineEvent(index+1, raw, redactor)
		if query.Category != "" && row.Category != query.Category {
			continue
		}
		projected = append(projected, row)
	}

	start := query.Offset
	if start > len(projected) {
		start = len(projected)
	}
	end := start + query.Limit
	hasMore := false
	if end < len(projected) {
		hasMore = true
	} else {
		end = len(projected)
	}
	pageRows := make([]observability.TimelineEventRow, 0, end-start)
	pageRows = append(pageRows, projected[start:end]...)
	return observability.TimelinePage{Rows: pageRows, Limit: query.Limit, HasMore: hasMore}, nil
}

// StateSummary returns durable aggregate data for /api/v1/state.
func (s *SQLiteStore) StateSummary(ctx context.Context, latestLimit int) (observability.StoreSummary, error) {
	if s == nil || s.db == nil {
		return observability.StoreSummary{}, fmt.Errorf("%w: sqlite db is required", ErrInvalidStoreRequest)
	}
	if latestLimit <= 0 {
		latestLimit = 5
	}
	summary := observability.StoreSummary{Counts: emptyCounts()}

	countRows, err := s.db.QueryContext(ctx, `SELECT status, count(*) FROM runs GROUP BY status`)
	if err != nil {
		return observability.StoreSummary{}, err
	}
	for countRows.Next() {
		var status string
		var count int
		if err := countRows.Scan(&status, &count); err != nil {
			_ = countRows.Close()
			return observability.StoreSummary{}, err
		}
		summary.Counts[status] = count
	}
	if err := countRows.Close(); err != nil {
		return observability.StoreSummary{}, err
	}

	var retrying int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM retry_queue`).Scan(&retrying); err != nil {
		return observability.StoreSummary{}, err
	}
	summary.Counts[observability.RunStatusRetrying] = retrying

	if err := s.db.QueryRowContext(
		ctx,
		`SELECT
			COALESCE(sum(input_tokens), 0),
			COALESCE(sum(output_tokens), 0),
			COALESCE(sum(reasoning_tokens), 0),
			COALESCE(sum(cached_tokens), 0),
			COALESCE(sum(total_tokens), 0)
		 FROM sessions`,
	).Scan(
		&summary.Tokens.InputTokens,
		&summary.Tokens.OutputTokens,
		&summary.Tokens.ReasoningTokens,
		&summary.Tokens.CachedTokens,
		&summary.Tokens.TotalTokens,
	); err != nil {
		return observability.StoreSummary{}, err
	}

	totalSeconds, err := s.totalRuntimeSeconds(ctx)
	if err != nil {
		return observability.StoreSummary{}, err
	}
	summary.Runtime.TotalSeconds = totalSeconds

	latest, err := s.QueryRuns(ctx, observability.RunQuery{
		Statuses: []string{observability.RunStatusCompleted, observability.RunStatusFailed},
		Limit:    latestLimit,
	})
	if err != nil {
		return observability.StoreSummary{}, err
	}
	summary.LatestCompletedOrFailed = latest.Rows

	rateLimit, err := s.latestRateLimit(ctx)
	if err != nil {
		return observability.StoreSummary{}, err
	}
	summary.RateLimit = rateLimit
	return summary, nil
}

func (s *SQLiteStore) queryStoredRunRows(ctx context.Context, issue string) ([]observability.RunRow, error) {
	return s.queryStoredRunRowsWhere(ctx, issue, "")
}

func (s *SQLiteStore) queryStoredRunRowsByRunID(ctx context.Context, runID string) ([]observability.RunRow, error) {
	return s.queryStoredRunRowsWhere(ctx, "", runID)
}

func (s *SQLiteStore) queryStoredRunRowsWhere(
	ctx context.Context,
	issue string,
	runID string,
) ([]observability.RunRow, error) {
	issue = strings.TrimSpace(issue)
	runID = strings.TrimSpace(runID)
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			r.id, r.issue_id, r.issue_identifier, r.status, r.attempt,
			r.workspace_path, r.session_id, r.thread_id, r.turn_id,
			r.started_at, r.finished_at, r.error,
			COALESCE(s.session_id, ''), COALESCE(s.thread_id, ''), COALESCE(s.turn_id, ''),
			COALESCE(s.status, ''), COALESCE(s.summary, ''), COALESCE(s.workspace_path, ''),
			COALESCE(s.input_tokens, 0), COALESCE(s.output_tokens, 0),
			COALESCE(s.reasoning_tokens, 0), COALESCE(s.cached_tokens, 0),
			COALESCE(s.total_tokens, 0),
			COALESCE(rq.attempt, 0), rq.due_at, COALESCE(rq.backoff_ms, 0), COALESCE(rq.error, '')
		 FROM runs r
		 LEFT JOIN sessions s ON s.session_id = (
			SELECT latest.session_id FROM sessions latest
			WHERE latest.run_id = r.id
			ORDER BY latest.updated_at DESC, latest.created_at DESC
			LIMIT 1
		 )
		 LEFT JOIN retry_queue rq ON rq.run_id = r.id
		 WHERE (? = '' OR lower(r.issue_id) = lower(?) OR lower(r.issue_identifier) = lower(?))
		   AND (? = '' OR r.id = ?)
		 ORDER BY COALESCE(r.finished_at, r.updated_at, r.started_at) DESC, r.issue_identifier, r.id`,
		issue,
		issue,
		issue,
		runID,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []observability.RunRow
	for rows.Next() {
		row, err := scanRunRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) queryRetryRows(ctx context.Context, issue string) ([]observability.RunRow, error) {
	issue = strings.TrimSpace(issue)
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			rq.run_id, rq.issue_id, rq.issue_identifier, rq.attempt, rq.due_at,
			rq.backoff_ms, rq.error,
			COALESCE(r.workspace_path, ''), COALESCE(r.session_id, ''),
			COALESCE(r.thread_id, ''), COALESCE(r.turn_id, ''),
			COALESCE(r.started_at, rq.created_at),
			COALESCE(s.session_id, ''), COALESCE(s.thread_id, ''), COALESCE(s.turn_id, ''),
			COALESCE(s.status, ''), COALESCE(s.summary, ''), COALESCE(s.workspace_path, ''),
			COALESCE(s.input_tokens, 0), COALESCE(s.output_tokens, 0),
			COALESCE(s.reasoning_tokens, 0), COALESCE(s.cached_tokens, 0),
			COALESCE(s.total_tokens, 0)
		 FROM retry_queue rq
		 LEFT JOIN runs r ON r.id = rq.run_id
		 LEFT JOIN sessions s ON s.session_id = (
			SELECT latest.session_id FROM sessions latest
			WHERE latest.run_id = rq.run_id
			ORDER BY latest.updated_at DESC, latest.created_at DESC
			LIMIT 1
		 )
		 WHERE (? = '' OR lower(rq.issue_id) = lower(?) OR lower(rq.issue_identifier) = lower(?))
		 ORDER BY rq.due_at ASC, rq.issue_identifier, rq.issue_id`,
		issue,
		issue,
		issue,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []observability.RunRow
	for rows.Next() {
		var row observability.RunRow
		var startedText, dueText string
		var runWorkspace, runSession, runThread, runTurn string
		var sessionWorkspace string
		var retryError string
		var retryBackoff int
		if err := rows.Scan(
			&row.RunID,
			&row.IssueID,
			&row.IssueIdentifier,
			&row.Attempt,
			&dueText,
			&retryBackoff,
			&retryError,
			&runWorkspace,
			&runSession,
			&runThread,
			&runTurn,
			&startedText,
			&row.SessionID,
			&row.ThreadID,
			&row.TurnID,
			&row.SessionStatus,
			&row.SessionSummary,
			&sessionWorkspace,
			&row.TokenTotals.InputTokens,
			&row.TokenTotals.OutputTokens,
			&row.TokenTotals.ReasoningTokens,
			&row.TokenTotals.CachedTokens,
			&row.TokenTotals.TotalTokens,
		); err != nil {
			return nil, err
		}
		startedAt, err := parseTime(startedText)
		if err != nil {
			return nil, err
		}
		dueAt, err := parseTime(dueText)
		if err != nil {
			return nil, err
		}
		row.Status = observability.RunStatusRetrying
		row.WorkspacePath = firstNonEmpty(sessionWorkspace, runWorkspace)
		row.SessionID = firstNonEmpty(row.SessionID, runSession)
		row.ThreadID = firstNonEmpty(row.ThreadID, runThread)
		row.TurnID = firstNonEmpty(row.TurnID, runTurn)
		row.StartedAt = startedAt
		row.SortAt = dueAt
		row.Retry = &observability.RetryInfo{
			Attempt:   row.Attempt,
			DueAt:     dueAt,
			BackoffMS: retryBackoff,
			Error:     retryError,
		}
		row.ErrorSummary = retryError
		result = append(result, row)
	}
	return result, rows.Err()
}

func (s *SQLiteStore) runExists(ctx context.Context, runID string) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM runs WHERE id = ? LIMIT 1`, runID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *SQLiteStore) queryRawTimelineEvents(
	ctx context.Context,
	runID string,
) ([]observability.RawTimelineEvent, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT
			e.id,
			COALESCE(NULLIF(e.run_id, ''), r.id),
			COALESCE(NULLIF(e.issue_id, ''), r.issue_id),
			COALESCE(NULLIF(e.issue_identifier, ''), r.issue_identifier),
			COALESCE(NULLIF(e.session_id, ''), NULLIF(s.session_id, ''), r.session_id, ''),
			COALESCE(NULLIF(s.thread_id, ''), r.thread_id, ''),
			COALESCE(NULLIF(s.turn_id, ''), r.turn_id, ''),
			e.event_type,
			e.payload_json,
			e.created_at
		 FROM agent_events e
		 JOIN runs r ON r.id = e.run_id
		 LEFT JOIN sessions s ON s.session_id = CASE
			WHEN e.session_id != '' THEN e.session_id
			ELSE (
				SELECT latest.session_id FROM sessions latest
				WHERE latest.run_id = e.run_id
				ORDER BY latest.updated_at DESC, latest.created_at DESC
				LIMIT 1
			)
		 END
		 WHERE e.run_id = ?
		 ORDER BY e.created_at ASC, e.id ASC`,
		runID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []observability.RawTimelineEvent{}
	for rows.Next() {
		var event observability.RawTimelineEvent
		var createdText string
		if err := rows.Scan(
			&event.ID,
			&event.RunID,
			&event.IssueID,
			&event.IssueIdentifier,
			&event.SessionID,
			&event.ThreadID,
			&event.TurnID,
			&event.Type,
			&event.PayloadJSON,
			&createdText,
		); err != nil {
			return nil, err
		}
		createdAt, err := parseTime(createdText)
		if err != nil {
			return nil, err
		}
		event.At = createdAt
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanRunRow(rows *sql.Rows) (observability.RunRow, error) {
	var row observability.RunRow
	var startedText string
	var finishedText sql.NullString
	var runWorkspace, runSession, runThread, runTurn string
	var sessionWorkspace string
	var retryAttempt int
	var retryBackoff int
	var retryError string
	var retryDue sql.NullString
	if err := rows.Scan(
		&row.RunID,
		&row.IssueID,
		&row.IssueIdentifier,
		&row.Status,
		&row.Attempt,
		&runWorkspace,
		&runSession,
		&runThread,
		&runTurn,
		&startedText,
		&finishedText,
		&row.ErrorSummary,
		&row.SessionID,
		&row.ThreadID,
		&row.TurnID,
		&row.SessionStatus,
		&row.SessionSummary,
		&sessionWorkspace,
		&row.TokenTotals.InputTokens,
		&row.TokenTotals.OutputTokens,
		&row.TokenTotals.ReasoningTokens,
		&row.TokenTotals.CachedTokens,
		&row.TokenTotals.TotalTokens,
		&retryAttempt,
		&retryDue,
		&retryBackoff,
		&retryError,
	); err != nil {
		return observability.RunRow{}, err
	}
	startedAt, err := parseTime(startedText)
	if err != nil {
		return observability.RunRow{}, err
	}
	row.StartedAt = startedAt
	row.WorkspacePath = firstNonEmpty(sessionWorkspace, runWorkspace)
	row.SessionID = firstNonEmpty(row.SessionID, runSession)
	row.ThreadID = firstNonEmpty(row.ThreadID, runThread)
	row.TurnID = firstNonEmpty(row.TurnID, runTurn)
	row.SortAt = startedAt
	if finishedText.Valid && finishedText.String != "" {
		finishedAt, err := parseTime(finishedText.String)
		if err != nil {
			return observability.RunRow{}, err
		}
		row.FinishedAt = &finishedAt
		row.RuntimeSeconds = finishedAt.Sub(startedAt).Seconds()
		row.SortAt = finishedAt
	}
	if retryDue.Valid && retryDue.String != "" {
		dueAt, err := parseTime(retryDue.String)
		if err != nil {
			return observability.RunRow{}, err
		}
		row.Retry = &observability.RetryInfo{
			Attempt:   retryAttempt,
			DueAt:     dueAt,
			BackoffMS: retryBackoff,
			Error:     retryError,
		}
	}
	return row, nil
}

func (s *SQLiteStore) latestEventSummary(ctx context.Context, runID string) (*observability.EventSummary, error) {
	var id, eventType, payloadText, createdText string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, event_type, payload_json, created_at
		 FROM agent_events
		 WHERE run_id = ?
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
		runID,
	).Scan(&id, &eventType, &payloadText, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	at, err := parseTime(createdText)
	if err != nil {
		return nil, err
	}
	return &observability.EventSummary{
		ID:      id,
		Type:    eventType,
		At:      at,
		Summary: eventPayloadSummary(eventType, payloadText),
	}, nil
}

func (s *SQLiteStore) latestRateLimit(ctx context.Context) (observability.RateLimitSummary, error) {
	var payloadText, createdText string
	err := s.db.QueryRowContext(
		ctx,
		`SELECT payload_json, created_at
		 FROM agent_events
		 WHERE event_type = 'agent.rate_limits_updated'
		 ORDER BY created_at DESC, id DESC
		 LIMIT 1`,
	).Scan(&payloadText, &createdText)
	if errors.Is(err, sql.ErrNoRows) {
		return observability.RateLimitSummary{Latest: json.RawMessage("null")}, nil
	}
	if err != nil {
		return observability.RateLimitSummary{}, err
	}
	at, err := parseTime(createdText)
	if err != nil {
		return observability.RateLimitSummary{}, err
	}
	payload := extractNestedPayload(payloadText)
	return observability.RateLimitSummary{Latest: payload, UpdatedAt: &at}, nil
}

func (s *SQLiteStore) totalRuntimeSeconds(ctx context.Context) (float64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT started_at, finished_at FROM runs WHERE finished_at IS NOT NULL`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var total float64
	for rows.Next() {
		var startedText, finishedText string
		if err := rows.Scan(&startedText, &finishedText); err != nil {
			return 0, err
		}
		startedAt, err := parseTime(startedText)
		if err != nil {
			return 0, err
		}
		finishedAt, err := parseTime(finishedText)
		if err != nil {
			return 0, err
		}
		total += finishedAt.Sub(startedAt).Seconds()
	}
	return total, rows.Err()
}

func detailFromRunRow(row observability.RunRow) observability.RunDetail {
	detail := observability.RunDetail{
		Metadata: observability.RunMetadata{
			RunID:          row.RunID,
			Status:         row.Status,
			Attempt:        row.Attempt,
			StartedAt:      row.StartedAt,
			FinishedAt:     row.FinishedAt,
			RuntimeSeconds: row.RuntimeSeconds,
		},
		Issue: observability.IssueIdentity{
			ID:         row.IssueID,
			Identifier: row.IssueIdentifier,
		},
		Workspace: observability.WorkspaceSummary{Path: row.WorkspacePath},
		Session: observability.SessionSummary{
			ID:       row.SessionID,
			ThreadID: row.ThreadID,
			TurnID:   row.TurnID,
			Status:   row.SessionStatus,
			Summary:  row.SessionSummary,
		},
		LatestEvent: row.LatestEvent,
		TokenTotals: row.TokenTotals,
		Retry:       row.Retry,
	}
	if row.ErrorSummary != "" {
		detail.Failure = &observability.FailureSummary{Error: row.ErrorSummary}
	}
	return detail
}

func filterRowsByStatus(rows []observability.RunRow, statuses []string) []observability.RunRow {
	if len(statuses) == 0 {
		return rows
	}
	allowed := map[string]struct{}{}
	for _, status := range statuses {
		allowed[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
	}
	filtered := rows[:0]
	for _, row := range rows {
		if _, ok := allowed[strings.ToLower(row.Status)]; ok {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func sortRunRows(rows []observability.RunRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := rows[i].SortAt
		if left.IsZero() {
			left = rows[i].StartedAt
		}
		right := rows[j].SortAt
		if right.IsZero() {
			right = rows[j].StartedAt
		}
		if left.Equal(right) {
			return rows[i].RunID < rows[j].RunID
		}
		return left.After(right)
	})
}

func emptyCounts() map[string]int {
	return map[string]int{
		observability.RunStatusRunning:     0,
		observability.RunStatusRetrying:    0,
		observability.RunStatusCompleted:   0,
		observability.RunStatusFailed:      0,
		observability.RunStatusStopped:     0,
		observability.RunStatusInterrupted: 0,
	}
}

func eventPayloadSummary(eventType string, payloadText string) string {
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return eventType
	}
	for _, key := range []string{"message", "summary", "method", "kind"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return eventType
}

func extractNestedPayload(payloadText string) json.RawMessage {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payloadText), &payload); err == nil {
		if nested := payload["payload"]; len(nested) > 0 {
			return append(json.RawMessage(nil), nested...)
		}
	}
	if strings.TrimSpace(payloadText) == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(payloadText)
}

var _ QueryStore = (*SQLiteStore)(nil)
