package policy

import (
	"reflect"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

func TestCheckEligibilityStateAndRuntimeGuards(t *testing.T) {
	cfg := testTrackerConfig()

	tests := []struct {
		name     string
		issue    tracker.Issue
		runtime  RuntimeState
		want     Reason
		wantOK   bool
		wantMeta string
	}{
		{
			name:   "active issue is eligible",
			issue:  testIssue("TOO-1", "Todo"),
			want:   ReasonEligible,
			wantOK: true,
		},
		{
			name:     "missing title is rejected",
			issue:    tracker.Issue{ID: "issue-1", Identifier: "TOO-1", State: "Todo"},
			want:     ReasonMissingRequiredField,
			wantMeta: "title",
		},
		{
			name:  "terminal state is rejected",
			issue: testIssue("TOO-1", "Done"),
			want:  ReasonTerminalState,
		},
		{
			name:  "inactive state is rejected",
			issue: testIssue("TOO-1", "Backlog"),
			want:  ReasonInactiveState,
		},
		{
			name:  "running issue is rejected",
			issue: testIssue("TOO-1", "Todo"),
			runtime: RuntimeState{
				RunningIssueIDs: map[string]struct{}{"issue-TOO-1": {}},
			},
			want: ReasonAlreadyRunning,
		},
		{
			name:  "claimed issue is rejected",
			issue: testIssue("TOO-1", "Todo"),
			runtime: RuntimeState{
				ClaimedIssueIDs: map[string]struct{}{"issue-TOO-1": {}},
			},
			want: ReasonAlreadyClaimed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckEligibility(cfg, tt.issue, tt.runtime)
			if got.Allowed != tt.wantOK || got.Reason != tt.want {
				t.Fatalf("CheckEligibility() = %#v, want allowed=%v reason=%s", got, tt.wantOK, tt.want)
			}
			if tt.wantMeta != "" && got.Field != tt.wantMeta {
				t.Fatalf("Field = %q, want %q", got.Field, tt.wantMeta)
			}
		})
	}
}

func TestCheckEligibilityTodoBlockersUseNormalizedTerminalStates(t *testing.T) {
	cfg := testTrackerConfig()

	tests := []struct {
		name   string
		issue  tracker.Issue
		want   Reason
		wantOK bool
	}{
		{
			name: "todo with terminal blockers is eligible",
			issue: withBlockers(testIssue("TOO-1", "Todo"),
				tracker.BlockerRef{ID: "blocker-1", Identifier: "TOO-0", State: " done "},
				tracker.BlockerRef{ID: "blocker-2", Identifier: "TOO-9", State: "CLOSED"},
			),
			want:   ReasonEligible,
			wantOK: true,
		},
		{
			name: "todo with non terminal blocker is rejected",
			issue: withBlockers(testIssue("TOO-1", "Todo"),
				tracker.BlockerRef{ID: "blocker-1", Identifier: "TOO-0", State: "In Progress"},
			),
			want: ReasonBlockedByNonTerminalIssue,
		},
		{
			name: "non todo state does not apply blocker dispatch gate",
			issue: withBlockers(testIssue("TOO-1", "In Progress"),
				tracker.BlockerRef{ID: "blocker-1", Identifier: "TOO-0", State: "In Progress"},
			),
			want:   ReasonEligible,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckEligibility(cfg, tt.issue, RuntimeState{})
			if got.Allowed != tt.wantOK || got.Reason != tt.want {
				t.Fatalf("CheckEligibility() = %#v, want allowed=%v reason=%s", got, tt.wantOK, tt.want)
			}
			if tt.want == ReasonBlockedByNonTerminalIssue && got.Blocker.Identifier != "TOO-0" {
				t.Fatalf("Blocker = %#v, want TOO-0", got.Blocker)
			}
		})
	}
}

func TestCheckEligibilityIssueFilter(t *testing.T) {
	base := testTrackerConfig()
	base.IssueFilter = config.IssueFilter{
		RequireLabels:                []string{"repo:api"},
		RejectLabels:                 []string{"cross-repo"},
		RequireExactlyOneLabelPrefix: "repo:",
	}

	tests := []struct {
		name          string
		filter        config.IssueFilter
		labels        []string
		want          Reason
		wantOK        bool
		wantLabel     string
		wantRepoLabel string
	}{
		{
			name:          "matching repo label is eligible",
			filter:        base.IssueFilter,
			labels:        []string{"REPO:API", "low-risk"},
			want:          ReasonEligible,
			wantOK:        true,
			wantRepoLabel: "repo:api",
		},
		{
			name:      "missing required label is rejected",
			filter:    base.IssueFilter,
			labels:    []string{"repo:web"},
			want:      ReasonMissingRequiredLabel,
			wantLabel: "repo:api",
		},
		{
			name:      "reject label is rejected",
			filter:    base.IssueFilter,
			labels:    []string{"repo:api", "cross-repo"},
			want:      ReasonRejectedLabelPresent,
			wantLabel: "cross-repo",
		},
		{
			name:      "missing repo routing prefix is rejected",
			filter:    config.IssueFilter{RequireExactlyOneLabelPrefix: "repo:"},
			labels:    []string{"low-risk"},
			want:      ReasonMissingRepoRoutingLabel,
			wantLabel: "repo:",
		},
		{
			name:      "ambiguous repo routing prefix is rejected",
			filter:    config.IssueFilter{RequireExactlyOneLabelPrefix: "repo:"},
			labels:    []string{"repo:api", "repo:web"},
			want:      ReasonAmbiguousRepoRoutingLabel,
			wantLabel: "repo:",
		},
		{
			name:   "require any labels rejects when none match",
			filter: config.IssueFilter{RequireAnyLabels: []string{"low-risk", "migration"}},
			labels: []string{"repo:api"},
			want:   ReasonMissingAnyRequiredLabel,
		},
		{
			name:   "require any labels accepts when one matches",
			filter: config.IssueFilter{RequireAnyLabels: []string{"low-risk", "migration"}},
			labels: []string{"migration"},
			want:   ReasonEligible,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base
			cfg.IssueFilter = tt.filter
			issue := testIssue("TOO-1", "Todo")
			issue.Labels = tt.labels
			got := CheckEligibility(cfg, issue, RuntimeState{})
			if got.Allowed != tt.wantOK || got.Reason != tt.want {
				t.Fatalf("CheckEligibility() = %#v, want allowed=%v reason=%s", got, tt.wantOK, tt.want)
			}
			if got.Label != tt.wantLabel {
				t.Fatalf("Label = %q, want %q", got.Label, tt.wantLabel)
			}
			if got.RepoLabel != tt.wantRepoLabel {
				t.Fatalf("RepoLabel = %q, want %q", got.RepoLabel, tt.wantRepoLabel)
			}
		})
	}
}

func TestEvaluateCandidatesSortsByDispatchOrder(t *testing.T) {
	oldest := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	middle := time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)
	newest := time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC)
	priorityOne := 1
	priorityTwo := 2

	issues := []tracker.Issue{
		withPriorityAndCreated(testIssue("TOO-4", "Todo"), nil, &oldest),
		withPriorityAndCreated(testIssue("TOO-3", "Todo"), &priorityTwo, nil),
		withPriorityAndCreated(testIssue("TOO-2", "Todo"), &priorityOne, &newest),
		withPriorityAndCreated(testIssue("TOO-1", "Todo"), &priorityOne, &middle),
	}

	got := EvaluateCandidates(testTrackerConfig(), issues, RuntimeState{})
	var identifiers []string
	for _, decision := range got {
		identifiers = append(identifiers, decision.Issue.Identifier)
		if !decision.Eligibility.Allowed || decision.Eligibility.Reason != ReasonEligible {
			t.Fatalf("decision for %s = %#v, want eligible", decision.Issue.Identifier, decision.Eligibility)
		}
	}

	want := []string{"TOO-1", "TOO-2", "TOO-3", "TOO-4"}
	if !reflect.DeepEqual(identifiers, want) {
		t.Fatalf("dispatch order = %#v, want %#v", identifiers, want)
	}
	if issues[0].Identifier != "TOO-4" {
		t.Fatalf("EvaluateCandidates mutated input order")
	}
}

func testTrackerConfig() config.Tracker {
	return config.Tracker{
		ActiveStates:   []string{"Todo", "In Progress", "Rework", "Merging"},
		TerminalStates: []string{"Done", "Closed", "Canceled", "Cancelled", "Duplicate"},
	}
}

func testIssue(identifier string, state string) tracker.Issue {
	return tracker.Issue{
		ID:         "issue-" + identifier,
		Identifier: identifier,
		Title:      "Title " + identifier,
		State:      state,
	}
}

func withBlockers(issue tracker.Issue, blockers ...tracker.BlockerRef) tracker.Issue {
	issue.BlockedBy = blockers
	return issue
}

func withPriorityAndCreated(issue tracker.Issue, priority *int, createdAt *time.Time) tracker.Issue {
	issue.Priority = priority
	issue.CreatedAt = createdAt
	return issue
}
