package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

func TestRenderStatusGoldenSnapshots(t *testing.T) {
	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		state StateResponse
	}{
		{name: "idle", state: baseState(now, "idle")},
		{name: "running", state: runningState(now)},
		{name: "retry", state: retryState(now)},
		{name: "failed", state: failedState(now)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderStatus(tt.state, RenderOptions{Now: now, Width: 100})
			assertGolden(t, filepath.Join("testdata", "status_"+tt.name+".golden"), got)
		})
	}
}

func TestRenderStatusWidthCompression(t *testing.T) {
	now := time.Date(2026, 5, 6, 9, 0, 0, 0, time.UTC)
	state := runningState(now)
	state.Running[0].LatestEvent.Summary = strings.Repeat("long-event-", 10)

	got := RenderStatus(state, RenderOptions{Now: now, Width: 64})
	for _, line := range strings.Split(strings.TrimSuffix(got, "\n"), "\n") {
		if len(line) > 64 {
			t.Fatalf("line length = %d, want <= 64:\n%s", len(line), line)
		}
	}
	if !strings.Contains(got, "...") {
		t.Fatalf("expected compressed output to contain ellipsis:\n%s", got)
	}
}

func TestCompactSessionID(t *testing.T) {
	tests := []struct {
		name    string
		session string
		width   int
		want    string
	}{
		{name: "short", session: "session-1", width: 14, want: "session-1"},
		{name: "long", session: "session-abcdef1234567890", width: 14, want: "session...7890"},
		{name: "tiny", session: "abcdef", width: 3, want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CompactSessionID(tt.session, tt.width); got != tt.want {
				t.Fatalf("CompactSessionID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFitLineKeepsUTF8Valid(t *testing.T) {
	got := fitLine("事件事件事件事件", 6)
	if !utf8.ValidString(got) {
		t.Fatalf("fitLine returned invalid UTF-8: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("fitLine() = %q, want truncated marker", got)
	}
}

func TestRenderDetailTimelineGolden(t *testing.T) {
	started := time.Date(2026, 5, 6, 8, 55, 0, 0, time.UTC)
	input := RenderDetailInput{
		Detail: observability.RunDetail{
			Metadata: observability.RunMetadata{
				RunID:          "run-141",
				Status:         observability.RunStatusRunning,
				Attempt:        2,
				StartedAt:      started,
				RuntimeSeconds: 300,
			},
			Issue: observability.IssueIdentity{ID: "issue-141", Identifier: "TOO-141"},
			Session: observability.SessionSummary{
				ID:       "session-abcdef1234567890",
				ThreadID: "thread-1",
				TurnID:   "turn-2",
			},
			LatestEvent: &observability.EventSummary{Summary: "agent accepted"},
			TokenTotals: observability.TokenTotals{TotalTokens: 3200},
		},
		Events: observability.TimelinePage{Rows: []observability.TimelineEventRow{
			{
				Sequence: 1,
				At:       started.Add(10 * time.Second),
				Category: observability.TimelineCategoryLifecycle,
				Severity: "info",
				Title:    "Run started",
				Summary:  "run started",
			},
			{
				Sequence: 2,
				At:       started.Add(20 * time.Second),
				Category: observability.TimelineCategoryCommand,
				Severity: "info",
				Title:    "Command",
				Summary:  "command=go test ./...",
			},
			{
				Sequence: 3,
				At:       started.Add(30 * time.Second),
				Category: observability.TimelineCategoryError,
				Severity: "error",
				Title:    "Error",
				Summary:  "startup failed",
			},
		}},
	}

	got := RenderDetail(input, RenderOptions{Now: started.Add(5 * time.Minute), Width: 100})
	assertGolden(t, filepath.Join("testdata", "detail_timeline.golden"), got)
}

func baseState(now time.Time, lifecycle string) StateResponse {
	return StateResponse{
		GeneratedAt: now,
		Lifecycle:   LifecycleResponse{State: lifecycle},
		Ready:       ReadyResponse{OK: true},
		Counts: map[string]int{
			observability.RunStatusRunning:   0,
			observability.RunStatusRetrying:  0,
			observability.RunStatusCompleted: 0,
			observability.RunStatusFailed:    0,
		},
		RateLimit: observability.RateLimitSummary{Latest: json.RawMessage("null")},
	}
}

func runningState(now time.Time) StateResponse {
	state := baseState(now, "running")
	state.StateStore.Configured = true
	state.Runtime.TotalSeconds = 300
	state.Tokens.TotalTokens = 3200
	state.RateLimit = observability.RateLimitSummary{Latest: json.RawMessage(`{"remaining":42}`)}
	state.Running = []observability.RunRow{{
		RunID:           "run-141",
		IssueID:         "issue-141",
		IssueIdentifier: "TOO-141",
		Status:          observability.RunStatusRunning,
		Attempt:         2,
		SessionID:       "session-abcdef1234567890",
		TurnID:          "turn-2",
		StartedAt:       now.Add(-5 * time.Minute),
		RuntimeSeconds:  300,
		TokenTotals:     observability.TokenTotals{TotalTokens: 3200},
		LatestEvent: &observability.EventSummary{
			Summary: "tool=linear_graphql success=true",
		},
	}}
	return state
}

func retryState(now time.Time) StateResponse {
	state := baseState(now, "running")
	state.Retrying = []observability.RunRow{{
		RunID:           "run-142",
		IssueID:         "issue-142",
		IssueIdentifier: "TOO-142",
		Status:          observability.RunStatusRetrying,
		Attempt:         2,
		StartedAt:       now.Add(time.Minute),
		ErrorSummary:    "temporary failure",
		Retry: &observability.RetryInfo{
			Attempt:   2,
			DueAt:     now.Add(2 * time.Minute),
			BackoffMS: 120000,
			Error:     "temporary failure",
		},
	}}
	return state
}

func failedState(now time.Time) StateResponse {
	state := baseState(now, "running")
	state.Ready = ReadyResponse{OK: false, Error: "dispatch dependencies are not configured"}
	finished := now.Add(-3 * time.Minute)
	state.LatestCompletedOrFailed = []observability.RunRow{{
		RunID:           "run-143",
		IssueID:         "issue-143",
		IssueIdentifier: "TOO-143",
		Status:          observability.RunStatusFailed,
		Attempt:         1,
		SessionID:       "session-failed-abcdef",
		TurnID:          "turn-1",
		StartedAt:       now.Add(-5 * time.Minute),
		FinishedAt:      &finished,
		RuntimeSeconds:  120,
		ErrorSummary:    "guardrail stop",
		TokenTotals:     observability.TokenTotals{TotalTokens: 30},
	}}
	return state
}

func assertGolden(t *testing.T, path string, got string) {
	t.Helper()

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if got != string(want) {
		t.Fatalf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", path, got, string(want))
	}
}
