package observability

import (
	"strings"
	"testing"
	"time"
)

type timelineTestRedactor struct{}

func (timelineTestRedactor) String(value string) string {
	return strings.ReplaceAll(value, "literal-secret", "[REDACTED]")
}

func (timelineTestRedactor) JSON(value string) string {
	return strings.ReplaceAll(value, "literal-secret", "[REDACTED]")
}

func TestProjectTimelineEventCategorizesAndRedactsToolCall(t *testing.T) {
	row := ProjectTimelineEvent(2, RawTimelineEvent{
		ID:              "event-2",
		RunID:           "run-1",
		IssueID:         "issue-1",
		IssueIdentifier: "TOO-1",
		SessionID:       "session-1",
		Type:            "agent.tool_call",
		PayloadJSON: `{
			"kind":"tool_call",
			"method":"item/tool/call",
			"message":"token=literal-secret",
			"thread":"thread-1",
			"turn":"turn-1",
			"payload":{"tool":"linear_graphql","success":true,"token":"literal-secret"}
		}`,
		At: time.Date(2026, 5, 6, 10, 0, 0, 0, time.UTC),
	}, timelineTestRedactor{})

	if row.Sequence != 2 || row.Category != TimelineCategoryTool || row.Severity != string(LevelInfo) {
		t.Fatalf("row = %#v, want sequence=2 category=tool severity=info", row)
	}
	if row.Title != "Tool call" || row.Summary != "tool=linear_graphql success=true" {
		t.Fatalf("title/summary = %q/%q", row.Title, row.Summary)
	}
	if row.ThreadID != "thread-1" || row.TurnID != "turn-1" {
		t.Fatalf("thread/turn = %q/%q", row.ThreadID, row.TurnID)
	}
	if strings.Contains(string(row.Payload), "literal-secret") || strings.Contains(row.Summary, "literal-secret") {
		t.Fatalf("row leaked secret: summary=%q payload=%s", row.Summary, row.Payload)
	}
}

func TestProjectTimelineEventMapsCoreCategories(t *testing.T) {
	tests := []struct {
		name      string
		eventType string
		payload   string
		category  string
		severity  string
	}{
		{
			name:      "lifecycle",
			eventType: "agent.turn_started",
			payload:   `{"kind":"turn_started"}`,
			category:  TimelineCategoryLifecycle,
			severity:  string(LevelInfo),
		},
		{
			name:      "resource",
			eventType: "agent.rate_limits_updated",
			payload:   `{"kind":"rate_limits_updated","method":"account/rateLimits/updated"}`,
			category:  TimelineCategoryResource,
			severity:  string(LevelInfo),
		},
		{
			name:      "guardrail",
			eventType: "guardrail.exceeded",
			payload:   `{"message":"guardrail exceeded"}`,
			category:  TimelineCategoryGuardrail,
			severity:  string(LevelError),
		},
		{
			name:      "error",
			eventType: "agent.error",
			payload:   `{"error":{"message":"failed"}}`,
			category:  TimelineCategoryError,
			severity:  string(LevelError),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := ProjectTimelineEvent(1, RawTimelineEvent{
				ID:          "event",
				RunID:       "run",
				Type:        tt.eventType,
				PayloadJSON: tt.payload,
			}, nil)
			if row.Category != tt.category || row.Severity != tt.severity {
				t.Fatalf("row category/severity = %q/%q, want %q/%q", row.Category, row.Severity, tt.category, tt.severity)
			}
		})
	}
}
