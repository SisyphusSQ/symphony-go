package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

const (
	defaultWidth          = 100
	defaultSessionIDWidth = 14
)

// RenderOptions controls deterministic terminal rendering.
type RenderOptions struct {
	Width int
	Now   time.Time
}

// RenderDetailInput contains the run detail and timeline rows for detail mode.
type RenderDetailInput struct {
	Detail observability.RunDetail
	Events observability.TimelinePage
}

// RenderStatus renders the read-only status dashboard.
func RenderStatus(state StateResponse, opts RenderOptions) string {
	opts = normalizeOptions(opts, state.GeneratedAt)
	var out bytes.Buffer

	writeLine(&out, opts.Width, "SYMPHONY STATUS")
	ready := "not_ready"
	if state.Ready.OK {
		ready = "ready"
	}
	stateStore := "memory"
	if state.StateStore.Configured {
		stateStore = "state-store"
	}
	writeLine(
		&out,
		opts.Width,
		"generated: %s  lifecycle: %s  ready: %s  source: %s",
		formatTime(state.GeneratedAt),
		emptyDefault(state.Lifecycle.State, "unknown"),
		ready,
		stateStore,
	)
	writeLine(
		&out,
		opts.Width,
		"agents: %d  runtime: %s  tokens: %s  rate_limits: %s",
		len(state.Running)+len(state.Retrying),
		formatDurationSeconds(state.Runtime.TotalSeconds),
		formatTokens(state.Tokens.TotalTokens),
		formatRateLimit(state.RateLimit),
	)
	if state.Ready.Error != "" {
		writeLine(&out, opts.Width, "ready_error: %s", state.Ready.Error)
	}

	writeRows(&out, opts, "RUNNING", state.Running, rowModeRunning)
	writeRows(&out, opts, "RETRY / BACKOFF", state.Retrying, rowModeRetry)
	writeRows(&out, opts, "LATEST COMPLETED / FAILED", state.LatestCompletedOrFailed, rowModeLatest)

	return out.String()
}

// RenderDetail renders a single run timeline detail view.
func RenderDetail(input RenderDetailInput, opts RenderOptions) string {
	opts = normalizeOptions(opts, input.Detail.Metadata.StartedAt)
	var out bytes.Buffer
	detail := input.Detail
	runID := emptyDefault(detail.Metadata.RunID, "unknown")
	issue := emptyDefault(detail.Issue.Identifier, detail.Issue.ID)
	sessionID := CompactSessionID(detail.Session.ID, defaultSessionIDWidth)
	latest := "n/a"
	if detail.LatestEvent != nil && detail.LatestEvent.Summary != "" {
		latest = detail.LatestEvent.Summary
	}

	writeLine(&out, opts.Width, "SYMPHONY RUN DETAIL")
	writeLine(
		&out,
		opts.Width,
		"issue: %s  run: %s  status: %s  attempt: %d",
		emptyDefault(issue, "unknown"),
		runID,
		emptyDefault(detail.Metadata.Status, "unknown"),
		detail.Metadata.Attempt,
	)
	writeLine(
		&out,
		opts.Width,
		"session: %s  runtime: %s  tokens: %s  latest: %s",
		emptyDefault(sessionID, "n/a"),
		formatDurationSeconds(detail.Metadata.RuntimeSeconds),
		formatTokens(detail.TokenTotals.TotalTokens),
		latest,
	)
	if detail.Failure != nil && detail.Failure.Error != "" {
		writeLine(&out, opts.Width, "failure: %s", detail.Failure.Error)
	}
	if detail.Retry != nil {
		writeLine(
			&out,
			opts.Width,
			"retry: attempt=%d due=%s backoff=%s error=%s",
			detail.Retry.Attempt,
			formatTime(detail.Retry.DueAt),
			formatMilliseconds(detail.Retry.BackoffMS),
			detail.Retry.Error,
		)
	}
	out.WriteByte('\n')
	writeLine(&out, opts.Width, "TIMELINE")
	writeLine(&out, opts.Width, "%-4s %-8s %-10s %-5s %s", "SEQ", "AT", "CATEGORY", "SEV", "EVENT")
	if len(input.Events.Rows) == 0 {
		writeLine(&out, opts.Width, "(no events)")
		return out.String()
	}
	rows := append([]observability.TimelineEventRow(nil), input.Events.Rows...)
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Sequence < rows[j].Sequence
	})
	for _, row := range rows {
		event := row.Title
		if row.Summary != "" && row.Summary != row.Title {
			event = row.Title + " - " + row.Summary
		}
		writeLine(
			&out,
			opts.Width,
			"%-4d %-8s %-10s %-5s %s",
			row.Sequence,
			formatClock(row.At),
			emptyDefault(row.Category, "event"),
			emptyDefault(row.Severity, "info"),
			event,
		)
	}
	return out.String()
}

type rowMode string

const (
	rowModeRunning rowMode = "running"
	rowModeRetry   rowMode = "retry"
	rowModeLatest  rowMode = "latest"
)

func writeRows(out *bytes.Buffer, opts RenderOptions, title string, rows []observability.RunRow, mode rowMode) {
	out.WriteByte('\n')
	writeLine(out, opts.Width, "%s", title)
	if len(rows) == 0 {
		writeLine(out, opts.Width, "(none)")
		return
	}
	writeLine(out, opts.Width, "%-10s %-12s %-12s %-8s %-14s %s", "ISSUE", "STAGE", "AGE/TURN", "TOKENS", "SESSION", "LATEST EVENT")
	for _, row := range rows {
		stage := row.Status
		ageTurn := formatAgeTurn(row, mode, opts.Now)
		tokens := formatTokens(row.TokenTotals.TotalTokens)
		sessionID := CompactSessionID(row.SessionID, defaultSessionIDWidth)
		latest := latestEventText(row)
		writeLine(
			out,
			opts.Width,
			"%-10s %-12s %-12s %-8s %-14s %s",
			emptyDefault(row.IssueIdentifier, row.IssueID),
			emptyDefault(stage, "unknown"),
			ageTurn,
			tokens,
			emptyDefault(sessionID, "n/a"),
			latest,
		)
	}
}

// CompactSessionID returns a stable short session id for narrow table columns.
func CompactSessionID(sessionID string, width int) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || width <= 0 {
		return ""
	}
	if len(sessionID) <= width {
		return sessionID
	}
	if width <= 3 {
		return sessionID[:width]
	}
	if width <= 8 {
		return sessionID[:width-3] + "..."
	}
	prefix := width - 7
	if prefix < 1 {
		prefix = 1
	}
	return sessionID[:prefix] + "..." + sessionID[len(sessionID)-4:]
}

func normalizeOptions(opts RenderOptions, fallback time.Time) RenderOptions {
	if opts.Width <= 0 {
		opts.Width = defaultWidth
	}
	if opts.Now.IsZero() {
		opts.Now = fallback
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}
	return opts
}

func writeLine(out *bytes.Buffer, width int, format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	out.WriteString(fitLine(line, width))
	out.WriteByte('\n')
}

func fitLine(line string, width int) string {
	runes := []rune(line)
	if width <= 0 || len(runes) <= width {
		return line
	}
	if width <= 3 {
		return string(runes[:width])
	}
	return string(runes[:width-3]) + "..."
}

func formatAgeTurn(row observability.RunRow, mode rowMode, now time.Time) string {
	turn := emptyDefault(row.TurnID, fmt.Sprintf("try-%d", row.Attempt))
	switch mode {
	case rowModeRetry:
		if row.Retry != nil {
			return formatDue(row.Retry.DueAt, now) + "/" + turn
		}
		return formatDue(row.StartedAt, now) + "/" + turn
	case rowModeLatest:
		if row.RuntimeSeconds > 0 {
			return formatDurationSeconds(row.RuntimeSeconds) + "/" + turn
		}
		if row.FinishedAt != nil && !row.StartedAt.IsZero() {
			return formatDuration(row.FinishedAt.Sub(row.StartedAt)) + "/" + turn
		}
		return "n/a/" + turn
	default:
		if row.RuntimeSeconds > 0 {
			return formatDurationSeconds(row.RuntimeSeconds) + "/" + turn
		}
		if !row.StartedAt.IsZero() {
			return formatDuration(now.Sub(row.StartedAt)) + "/" + turn
		}
		return "n/a/" + turn
	}
}

func latestEventText(row observability.RunRow) string {
	switch {
	case row.LatestEvent != nil && row.LatestEvent.Summary != "":
		return row.LatestEvent.Summary
	case row.SessionSummary != "":
		return row.SessionSummary
	case row.ErrorSummary != "":
		return row.ErrorSummary
	case row.Retry != nil && row.Retry.Error != "":
		return row.Retry.Error
	case row.Status != "":
		return row.Status
	default:
		return "n/a"
	}
}

func formatDue(due time.Time, now time.Time) string {
	if due.IsZero() {
		return "n/a"
	}
	delta := due.Sub(now)
	if delta >= 0 {
		return "in " + formatDuration(delta)
	}
	return "due " + formatDuration(-delta) + " ago"
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "n/a"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatClock(value time.Time) string {
	if value.IsZero() {
		return "n/a"
	}
	return value.UTC().Format("15:04:05")
}

func formatDurationSeconds(seconds float64) string {
	if seconds <= 0 {
		return "0s"
	}
	return formatDuration(time.Duration(seconds * float64(time.Second)))
}

func formatDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration < time.Second {
		return "0s"
	}
	seconds := int64(math.Round(duration.Seconds()))
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	seconds = seconds % 60
	if minutes < 60 {
		if seconds == 0 {
			return fmt.Sprintf("%dm", minutes)
		}
		return fmt.Sprintf("%dm%ds", minutes, seconds)
	}
	hours := minutes / 60
	minutes = minutes % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}

func formatMilliseconds(value int) string {
	if value <= 0 {
		return "0s"
	}
	return formatDuration(time.Duration(value) * time.Millisecond)
}

func formatTokens(tokens int64) string {
	switch {
	case tokens >= 1_000_000:
		return fmt.Sprintf("%.1fm", float64(tokens)/1_000_000)
	case tokens >= 1_000:
		return fmt.Sprintf("%.1fk", float64(tokens)/1_000)
	default:
		return fmt.Sprintf("%d", tokens)
	}
}

func formatRateLimit(rateLimit observability.RateLimitSummary) string {
	latest := bytes.TrimSpace(rateLimit.Latest)
	if len(latest) == 0 || bytes.Equal(latest, []byte("null")) {
		return "n/a"
	}
	var payload map[string]any
	if err := json.Unmarshal(latest, &payload); err == nil {
		if remaining := jsonField(payload, "remaining"); remaining != "" {
			return "remaining=" + remaining
		}
		if reset := jsonField(payload, "reset_at"); reset != "" {
			return "reset_at=" + reset
		}
	}
	if len(latest) > 40 {
		return string(latest[:37]) + "..."
	}
	return string(latest)
}

func jsonField(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", typed), "0"), ".")
	case bool:
		if typed {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}

func emptyDefault(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
