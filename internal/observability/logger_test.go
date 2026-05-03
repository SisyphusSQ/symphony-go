package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestJSONLoggerWritesNormalizedStructuredEvent(t *testing.T) {
	var out bytes.Buffer
	logger := NewJSONLogger(&out)
	now := time.Date(2026, 5, 3, 10, 0, 0, 0, time.UTC)
	logger.now = func() time.Time { return now }

	err := logger.Log(context.Background(), Event{
		Type:            EventAgentRunFailed,
		IssueID:         "issue-1",
		IssueIdentifier: "TOO-129",
		SessionID:       "thread-1-turn-1",
		RunStatus:       RunStatusFailed,
		RetryState:      RetryStateFailure,
		Error:           "agent failed",
	})
	if err != nil {
		t.Fatalf("Log returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("json.Unmarshal returned error: %v\n%s", err, out.String())
	}
	for key, want := range map[string]string{
		"time":             now.Format(time.RFC3339Nano),
		"level":            string(LevelInfo),
		"event_type":       string(EventAgentRunFailed),
		"issue_id":         "issue-1",
		"issue_identifier": "TOO-129",
		"session_id":       "thread-1-turn-1",
		"run_status":       RunStatusFailed,
		"retry_state":      RetryStateFailure,
		"error":            "agent failed",
	} {
		if got[key] != want {
			t.Fatalf("%s = %#v, want %q in %s", key, got[key], want, out.String())
		}
	}
}

func TestRecorderRecordsBeforeReturningSinkError(t *testing.T) {
	wantErr := errors.New("sink unavailable")
	now := time.Date(2026, 5, 3, 10, 1, 0, 0, time.UTC)
	recorder := NewRecorder(
		WithRecorderClock(func() time.Time { return now }),
		WithRecorderError(wantErr),
	)

	err := recorder.Log(context.Background(), Event{Type: EventRetryScheduled})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Log error = %v, want %v", err, wantErr)
	}

	events := recorder.EventsByType(EventRetryScheduled)
	if len(events) != 1 {
		t.Fatalf("EventsByType = %#v, want one event", events)
	}
	if !events[0].Time.Equal(now) || events[0].Level != LevelInfo {
		t.Fatalf("event = %#v, want normalized time and info level", events[0])
	}
}

func TestRetryStateForError(t *testing.T) {
	if got := RetryStateForError(""); got != RetryStateContinuation {
		t.Fatalf("RetryStateForError(empty) = %q", got)
	}
	if got := RetryStateForError("agent failed"); got != RetryStateFailure {
		t.Fatalf("RetryStateForError(error) = %q", got)
	}
}
