package policy

import (
	"sort"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

// Reason is a stable, machine-readable dispatch eligibility result.
type Reason string

const (
	ReasonEligible                  Reason = "eligible"
	ReasonMissingRequiredField      Reason = "missing_required_field"
	ReasonTerminalState             Reason = "terminal_state"
	ReasonInactiveState             Reason = "inactive_state"
	ReasonAlreadyRunning            Reason = "already_running"
	ReasonAlreadyClaimed            Reason = "already_claimed"
	ReasonBlockedByNonTerminalIssue Reason = "blocked_by_non_terminal_issue"
	ReasonMissingRequiredLabel      Reason = "missing_required_label"
	ReasonRejectedLabelPresent      Reason = "rejected_label_present"
	ReasonMissingAnyRequiredLabel   Reason = "missing_any_required_label"
	ReasonMissingRepoRoutingLabel   Reason = "missing_repo_routing_label"
	ReasonAmbiguousRepoRoutingLabel Reason = "ambiguous_repo_routing_label"
)

// RuntimeState captures the in-memory dispatch reservations that make an issue
// ineligible even when tracker/config state is otherwise dispatchable.
type RuntimeState struct {
	RunningIssueIDs map[string]struct{}
	ClaimedIssueIDs map[string]struct{}
}

// Eligibility explains whether an issue may be dispatched.
type Eligibility struct {
	Allowed   bool
	Reason    Reason
	Field     string
	Label     string
	RepoLabel string
	Blocker   tracker.BlockerRef
}

// Decision binds a normalized issue to the eligibility result produced for it.
type Decision struct {
	Issue       tracker.Issue
	Eligibility Eligibility
}

// EvaluateCandidates sorts candidate issues in dispatch order and evaluates
// each one independently. The returned slice does not share ordering with the
// caller's input slice.
func EvaluateCandidates(
	cfg config.Tracker,
	issues []tracker.Issue,
	runtime RuntimeState,
) []Decision {
	ordered := SortedIssuesForDispatch(issues)
	decisions := make([]Decision, 0, len(ordered))
	for _, issue := range ordered {
		decisions = append(decisions, EvaluateIssue(cfg, issue, runtime))
	}
	return decisions
}

// EvaluateIssue returns a deterministic policy decision for one normalized issue.
func EvaluateIssue(cfg config.Tracker, issue tracker.Issue, runtime RuntimeState) Decision {
	return Decision{
		Issue:       issue,
		Eligibility: CheckEligibility(cfg, issue, runtime),
	}
}

// CheckEligibility evaluates one issue without wrapping it in a Decision.
func CheckEligibility(cfg config.Tracker, issue tracker.Issue, runtime RuntimeState) Eligibility {
	issueID := strings.TrimSpace(issue.ID)
	if issueID == "" {
		return ineligibleField("id")
	}
	if strings.TrimSpace(issue.Identifier) == "" {
		return ineligibleField("identifier")
	}
	if strings.TrimSpace(issue.Title) == "" {
		return ineligibleField("title")
	}
	if strings.TrimSpace(issue.State) == "" {
		return ineligibleField("state")
	}

	issueState := normalize(issue.State)
	activeStates := normalizedSet(cfg.ActiveStates)
	terminalStates := normalizedSet(cfg.TerminalStates)

	if _, ok := terminalStates[issueState]; ok {
		return Eligibility{Reason: ReasonTerminalState}
	}
	if _, ok := activeStates[issueState]; !ok {
		return Eligibility{Reason: ReasonInactiveState}
	}
	if _, ok := runtime.RunningIssueIDs[issueID]; ok {
		return Eligibility{Reason: ReasonAlreadyRunning}
	}
	if _, ok := runtime.ClaimedIssueIDs[issueID]; ok {
		return Eligibility{Reason: ReasonAlreadyClaimed}
	}
	if issueState == "todo" {
		for _, blocker := range issue.BlockedBy {
			if _, ok := terminalStates[normalize(blocker.State)]; !ok {
				return Eligibility{
					Reason:  ReasonBlockedByNonTerminalIssue,
					Blocker: blocker,
				}
			}
		}
	}

	if eligibility := checkIssueFilter(issue, cfg.IssueFilter); !eligibility.Allowed {
		return eligibility
	} else if eligibility.RepoLabel != "" {
		return Eligibility{
			Allowed:   true,
			Reason:    ReasonEligible,
			RepoLabel: eligibility.RepoLabel,
		}
	}

	return Eligibility{Allowed: true, Reason: ReasonEligible}
}

// SortedIssuesForDispatch returns a stable dispatch-order copy of issues.
func SortedIssuesForDispatch(issues []tracker.Issue) []tracker.Issue {
	ordered := append([]tracker.Issue(nil), issues...)
	SortIssuesForDispatch(ordered)
	return ordered
}

// SortIssuesForDispatch sorts issues in-place by priority, created_at, and
// identifier. Unknown priority and missing created_at sort last.
func SortIssuesForDispatch(issues []tracker.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left, right := issues[i], issues[j]
		if leftPriority, rightPriority := priorityRank(left), priorityRank(right); leftPriority != rightPriority {
			return leftPriority < rightPriority
		}
		if left.CreatedAt != nil && right.CreatedAt != nil && !left.CreatedAt.Equal(*right.CreatedAt) {
			return left.CreatedAt.Before(*right.CreatedAt)
		}
		if left.CreatedAt != nil && right.CreatedAt == nil {
			return true
		}
		if left.CreatedAt == nil && right.CreatedAt != nil {
			return false
		}
		return left.Identifier < right.Identifier
	})
}

func checkIssueFilter(issue tracker.Issue, filter config.IssueFilter) Eligibility {
	labels := normalizedSet(issue.Labels)
	for _, label := range normalizedLabels(filter.RequireLabels) {
		if _, ok := labels[label]; !ok {
			return Eligibility{Reason: ReasonMissingRequiredLabel, Label: label}
		}
	}
	for _, label := range normalizedLabels(filter.RejectLabels) {
		if _, ok := labels[label]; ok {
			return Eligibility{Reason: ReasonRejectedLabelPresent, Label: label}
		}
	}

	anyLabels := normalizedLabels(filter.RequireAnyLabels)
	if len(anyLabels) > 0 {
		matched := false
		for _, label := range anyLabels {
			if _, ok := labels[label]; ok {
				matched = true
				break
			}
		}
		if !matched {
			return Eligibility{Reason: ReasonMissingAnyRequiredLabel}
		}
	}

	prefix := normalize(filter.RequireExactlyOneLabelPrefix)
	if prefix == "" {
		return Eligibility{Allowed: true, Reason: ReasonEligible}
	}
	var repoLabel string
	for label := range labels {
		if strings.HasPrefix(label, prefix) {
			if repoLabel != "" {
				return Eligibility{Reason: ReasonAmbiguousRepoRoutingLabel, Label: prefix}
			}
			repoLabel = label
		}
	}
	if repoLabel == "" {
		return Eligibility{Reason: ReasonMissingRepoRoutingLabel, Label: prefix}
	}
	return Eligibility{Allowed: true, Reason: ReasonEligible, RepoLabel: repoLabel}
}

func priorityRank(issue tracker.Issue) int {
	if issue.Priority == nil || *issue.Priority <= 0 {
		return int(^uint(0) >> 1)
	}
	return *issue.Priority
}

func ineligibleField(field string) Eligibility {
	return Eligibility{
		Reason: ReasonMissingRequiredField,
		Field:  field,
	}
}

func normalizedSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := normalize(value)
		if normalized != "" {
			result[normalized] = struct{}{}
		}
	}
	return result
}

func normalizedLabels(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := normalize(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}
