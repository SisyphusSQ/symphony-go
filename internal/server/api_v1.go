package server

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
	runstate "github.com/SisyphusSQ/symphony-go/internal/state"
)

const (
	defaultRunLimit  = 50
	maxRunLimit      = 100
	stateLatestLimit = 5
)

var allowedRunStatuses = []string{
	observability.RunStatusRunning,
	observability.RunStatusRetrying,
	observability.RunStatusCompleted,
	observability.RunStatusFailed,
	observability.RunStatusStopped,
	observability.RunStatusInterrupted,
}

type apiStateResponse struct {
	GeneratedAt             time.Time                      `json:"generated_at"`
	Lifecycle               lifecycleResponse              `json:"lifecycle"`
	Ready                   readyResponse                  `json:"ready"`
	Counts                  map[string]int                 `json:"counts"`
	Running                 []observability.RunRow         `json:"running"`
	Retrying                []observability.RunRow         `json:"retrying"`
	LatestCompletedOrFailed []observability.RunRow         `json:"latest_completed_or_failed"`
	Tokens                  observability.TokenTotals      `json:"tokens"`
	Runtime                 observability.RuntimeTotals    `json:"runtime"`
	RateLimit               observability.RateLimitSummary `json:"rate_limit"`
	StateStore              stateStoreResponse             `json:"state_store"`
}

type lifecycleResponse struct {
	State string `json:"state"`
}

type readyResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

type stateStoreResponse struct {
	Configured bool `json:"configured"`
}

type stateQueryStoreProvider interface {
	StateQueryStore() runstate.QueryStore
}

func (h *handler) handleAPIState(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}

	snapshot := h.runtime.Snapshot()
	ready, readyErr := h.ready()
	running := rowsFromRunningSnapshot(snapshot.ActiveRuns)
	retrying := rowsFromRetrySnapshot(snapshot.RetryQueue)
	counts := emptyAPICounts()
	counts[observability.RunStatusRunning] = len(running)
	counts[observability.RunStatusRetrying] = len(retrying)

	store := h.queryStore()
	summary := observability.StoreSummary{
		Counts: emptyAPICounts(),
	}
	if store != nil {
		var err error
		summary, err = store.StateSummary(r.Context(), stateLatestLimit)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_store_query_failed", err.Error())
			return
		}
		for status, count := range summary.Counts {
			switch status {
			case observability.RunStatusRunning:
				if count > counts[status] {
					counts[status] = count
				}
			case observability.RunStatusRetrying:
				if count > counts[status] {
					counts[status] = count
				}
			default:
				counts[status] = count
			}
		}
	}

	writeJSON(w, http.StatusOK, apiStateResponse{
		GeneratedAt:             snapshot.GeneratedAt,
		Lifecycle:               lifecycleResponse{State: snapshot.LifecycleState},
		Ready:                   readyResponse{OK: ready, Error: errorString(readyErr)},
		Counts:                  counts,
		Running:                 running,
		Retrying:                retrying,
		LatestCompletedOrFailed: summary.LatestCompletedOrFailed,
		Tokens:                  summary.Tokens,
		Runtime:                 runtimeWithActiveSeconds(summary.Runtime, running),
		RateLimit:               summary.RateLimit,
		StateStore:              stateStoreResponse{Configured: store != nil},
	})
}

func (h *handler) handleAPIRuns(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	query, err := parseRunQuery(r.URL.Query())
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCode(err), err.Error())
		return
	}

	store := h.queryStore()
	var page observability.RunPage
	if store != nil {
		page, err = store.QueryRuns(r.Context(), query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_store_query_failed", err.Error())
			return
		}
	} else {
		page = snapshotRunPage(h.runtime.Snapshot(), query)
	}
	if page.HasMore {
		page.NextCursor = encodeCursor(query.Offset + query.Limit)
	}
	writeJSON(w, http.StatusOK, page)
}

func (h *handler) handleAPIRunPath(w http.ResponseWriter, r *http.Request) {
	if !allowMethod(w, r, http.MethodGet) {
		return
	}
	if h.runtime == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "runtime is unavailable")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/runs/")
	path = strings.Trim(path, "/")
	if path == "" {
		writeError(w, http.StatusNotFound, "not_found", "run id is required")
		return
	}
	parts := strings.Split(path, "/")
	if len(parts) != 1 {
		writeError(w, http.StatusNotFound, "not_found", "endpoint not found")
		return
	}
	runID, err := pathUnescape(parts[0])
	if err != nil || strings.TrimSpace(runID) == "" {
		writeError(w, http.StatusBadRequest, "invalid_run_id", "run id is invalid")
		return
	}

	if store := h.queryStore(); store != nil {
		detail, err := store.GetRun(r.Context(), runID)
		if err == nil {
			writeJSON(w, http.StatusOK, detail)
			return
		}
		if !errors.Is(err, runstate.ErrRunNotFound) {
			writeError(w, http.StatusInternalServerError, "state_store_query_failed", err.Error())
			return
		}
	}
	if detail, ok := snapshotRunDetail(h.runtime.Snapshot(), runID); ok {
		writeJSON(w, http.StatusOK, detail)
		return
	}
	writeError(w, http.StatusNotFound, "run_not_found", "run not found")
}

func (h *handler) queryStore() runstate.QueryStore {
	if h.config.StateStore != nil {
		return h.config.StateStore
	}
	provider, ok := h.runtime.(stateQueryStoreProvider)
	if !ok {
		return nil
	}
	return provider.StateQueryStore()
}

func parseRunQuery(values url.Values) (observability.RunQuery, error) {
	query := observability.RunQuery{Limit: defaultRunLimit}
	if raw := strings.TrimSpace(values.Get("limit")); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit <= 0 {
			return query, queryError{code: "invalid_limit", message: "limit must be a positive integer"}
		}
		if limit > maxRunLimit {
			return query, queryError{
				code:    "invalid_limit",
				message: fmt.Sprintf("limit must be less than or equal to %d", maxRunLimit),
			}
		}
		query.Limit = limit
	}
	if raw := strings.TrimSpace(values.Get("cursor")); raw != "" {
		offset, err := decodeCursor(raw)
		if err != nil {
			return query, queryError{code: "invalid_cursor", message: "cursor is invalid"}
		}
		query.Offset = offset
	}
	if raw := strings.TrimSpace(values.Get("status")); raw != "" {
		parts := strings.Split(raw, ",")
		seen := map[string]struct{}{}
		for _, part := range parts {
			status := strings.ToLower(strings.TrimSpace(part))
			if status == "" {
				return query, queryError{code: "invalid_status", message: "status must not be empty"}
			}
			if !slices.Contains(allowedRunStatuses, status) {
				return query, queryError{code: "invalid_status", message: "status is unsupported: " + status}
			}
			if _, ok := seen[status]; !ok {
				query.Statuses = append(query.Statuses, status)
				seen[status] = struct{}{}
			}
		}
	}
	query.Issue = strings.TrimSpace(values.Get("issue"))
	return query, nil
}

type queryError struct {
	code    string
	message string
}

func (err queryError) Error() string {
	return err.message
}

func errorCode(err error) string {
	var queryErr queryError
	if errors.As(err, &queryErr) {
		return queryErr.code
	}
	return "invalid_query"
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("offset:%d", offset)))
}

func decodeCursor(cursor string) (int, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, err
	}
	text := string(decoded)
	if !strings.HasPrefix(text, "offset:") {
		return 0, errors.New("invalid cursor prefix")
	}
	offset, err := strconv.Atoi(strings.TrimPrefix(text, "offset:"))
	if err != nil || offset < 0 {
		return 0, errors.New("invalid cursor offset")
	}
	return offset, nil
}

func rowsFromRunningSnapshot(runs []observability.RunSnapshot) []observability.RunRow {
	rows := make([]observability.RunRow, 0, len(runs))
	for _, run := range runs {
		sortAt := run.StartedAt
		rows = append(rows, observability.RunRow{
			RunID:           firstNonEmpty(run.RunID, run.IssueID, run.IssueIdentifier),
			IssueID:         run.IssueID,
			IssueIdentifier: run.IssueIdentifier,
			Status:          firstNonEmpty(run.RunStatus, observability.RunStatusRunning),
			Attempt:         run.Attempt,
			WorkspacePath:   run.WorkspacePath,
			SessionID:       run.SessionID,
			StartedAt:       run.StartedAt,
			RuntimeSeconds:  run.SecondsRunning,
			SortAt:          sortAt,
		})
	}
	sortRunRows(rows)
	return rows
}

func rowsFromRetrySnapshot(retries []observability.RetrySnapshot) []observability.RunRow {
	rows := make([]observability.RunRow, 0, len(retries))
	for _, retry := range retries {
		rows = append(rows, observability.RunRow{
			RunID:           firstNonEmpty(retry.RunID, retry.IssueID, retry.IssueIdentifier),
			IssueID:         retry.IssueID,
			IssueIdentifier: retry.IssueIdentifier,
			Status:          observability.RunStatusRetrying,
			Attempt:         retry.Attempt,
			StartedAt:       retry.DueAt,
			ErrorSummary:    retry.Error,
			Retry: &observability.RetryInfo{
				Attempt: retry.Attempt,
				DueAt:   retry.DueAt,
				Error:   retry.Error,
			},
			SortAt: retry.DueAt,
		})
	}
	sortRunRows(rows)
	return rows
}

func snapshotRunPage(snapshot observability.Snapshot, query observability.RunQuery) observability.RunPage {
	rows := append(rowsFromRunningSnapshot(snapshot.ActiveRuns), rowsFromRetrySnapshot(snapshot.RetryQueue)...)
	rows = filterRowsByStatus(rows, query.Statuses)
	rows = filterRowsByIssue(rows, query.Issue)
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
	return observability.RunPage{
		Rows:    pageRows,
		Limit:   query.Limit,
		HasMore: hasMore,
	}
}

func snapshotRunDetail(snapshot observability.Snapshot, runID string) (observability.RunDetail, bool) {
	for _, row := range rowsFromRunningSnapshot(snapshot.ActiveRuns) {
		if row.RunID == runID {
			return detailFromSnapshotRow(row), true
		}
	}
	for _, row := range rowsFromRetrySnapshot(snapshot.RetryQueue) {
		if row.RunID == runID {
			return detailFromSnapshotRow(row), true
		}
	}
	return observability.RunDetail{}, false
}

func detailFromSnapshotRow(row observability.RunRow) observability.RunDetail {
	detail := observability.RunDetail{
		Metadata: observability.RunMetadata{
			RunID:          row.RunID,
			Status:         row.Status,
			Attempt:        row.Attempt,
			StartedAt:      row.StartedAt,
			RuntimeSeconds: row.RuntimeSeconds,
		},
		Issue: observability.IssueIdentity{
			ID:         row.IssueID,
			Identifier: row.IssueIdentifier,
		},
		Workspace:   observability.WorkspaceSummary{Path: row.WorkspacePath},
		Session:     observability.SessionSummary{ID: row.SessionID},
		TokenTotals: row.TokenTotals,
		Retry:       row.Retry,
	}
	if row.ErrorSummary != "" {
		detail.Failure = &observability.FailureSummary{Error: row.ErrorSummary}
	}
	return detail
}

func runtimeWithActiveSeconds(
	runtime observability.RuntimeTotals,
	running []observability.RunRow,
) observability.RuntimeTotals {
	for _, row := range running {
		runtime.TotalSeconds += row.RuntimeSeconds
	}
	return runtime
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

func filterRowsByIssue(rows []observability.RunRow, issue string) []observability.RunRow {
	issue = strings.TrimSpace(issue)
	if issue == "" {
		return rows
	}
	filtered := rows[:0]
	for _, row := range rows {
		if strings.EqualFold(row.IssueID, issue) || strings.EqualFold(row.IssueIdentifier, issue) {
			filtered = append(filtered, row)
		}
	}
	return filtered
}

func sortRunRows(rows []observability.RunRow) {
	sort.SliceStable(rows, func(i, j int) bool {
		left := firstNonZeroTime(rows[i].SortAt, rows[i].StartedAt)
		right := firstNonZeroTime(rows[j].SortAt, rows[j].StartedAt)
		if left.Equal(right) {
			return rows[i].RunID < rows[j].RunID
		}
		return left.After(right)
	})
}

func firstNonZeroTime(values ...time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func emptyAPICounts() map[string]int {
	return map[string]int{
		observability.RunStatusRunning:     0,
		observability.RunStatusRetrying:    0,
		observability.RunStatusCompleted:   0,
		observability.RunStatusFailed:      0,
		observability.RunStatusStopped:     0,
		observability.RunStatusInterrupted: 0,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
