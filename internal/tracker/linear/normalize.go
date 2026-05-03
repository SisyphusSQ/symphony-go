package linear

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

type issuesResponse struct {
	Data *struct {
		Issues *issueConnection `json:"issues"`
	} `json:"data"`
}

func (r issuesResponse) issuePage() (*issueConnection, error) {
	if r.Data == nil || r.Data.Issues == nil {
		return nil, fmt.Errorf("%w: missing data.issues", ErrUnknownPayload)
	}
	return r.Data.Issues, nil
}

type issueConnection struct {
	Nodes    []issueNode `json:"nodes"`
	PageInfo *pageInfo   `json:"pageInfo"`
}

type pageInfo struct {
	HasNextPage bool   `json:"hasNextPage"`
	EndCursor   string `json:"endCursor"`
}

type issueNode struct {
	ID               string          `json:"id"`
	Identifier       string          `json:"identifier"`
	Title            string          `json:"title"`
	Description      string          `json:"description"`
	Priority         json.RawMessage `json:"priority"`
	State            stateNode       `json:"state"`
	BranchName       string          `json:"branchName"`
	URL              string          `json:"url"`
	Labels           labelConnection `json:"labels"`
	InverseRelations relationConn    `json:"inverseRelations"`
	CreatedAt        string          `json:"createdAt"`
	UpdatedAt        string          `json:"updatedAt"`
}

type stateNode struct {
	Name string `json:"name"`
}

type labelConnection struct {
	Nodes []labelNode `json:"nodes"`
}

type labelNode struct {
	Name string `json:"name"`
}

type relationConn struct {
	Nodes []relationNode `json:"nodes"`
}

type relationNode struct {
	Type  string        `json:"type"`
	Issue relationIssue `json:"issue"`
}

type relationIssue struct {
	ID         string    `json:"id"`
	Identifier string    `json:"identifier"`
	State      stateNode `json:"state"`
}

func normalizeIssues(nodes []issueNode) ([]tracker.Issue, error) {
	issues := make([]tracker.Issue, 0, len(nodes))
	for _, node := range nodes {
		issue, err := normalizeIssue(node)
		if err != nil {
			return nil, err
		}
		issues = append(issues, issue)
	}
	return issues, nil
}

func normalizeIssue(node issueNode) (tracker.Issue, error) {
	if node.ID == "" || node.Identifier == "" || node.State.Name == "" {
		return tracker.Issue{}, fmt.Errorf(
			"%w: issue requires id, identifier, and state.name",
			ErrUnknownPayload,
		)
	}

	createdAt, err := parseOptionalTime(node.CreatedAt, "createdAt")
	if err != nil {
		return tracker.Issue{}, err
	}
	updatedAt, err := parseOptionalTime(node.UpdatedAt, "updatedAt")
	if err != nil {
		return tracker.Issue{}, err
	}

	return tracker.Issue{
		ID:          node.ID,
		Identifier:  node.Identifier,
		Title:       node.Title,
		Description: node.Description,
		Priority:    parsePriority(node.Priority),
		State:       node.State.Name,
		BranchName:  node.BranchName,
		URL:         node.URL,
		Labels:      normalizeLabels(node.Labels.Nodes),
		BlockedBy:   normalizeBlockers(node.InverseRelations.Nodes),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func parseOptionalTime(value string, field string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil, fmt.Errorf("%w: %s is not RFC3339: %v", ErrUnknownPayload, field, err)
	}
	return &parsed, nil
}

func parsePriority(raw json.RawMessage) *int {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err != nil {
		return nil
	}
	value, err := strconv.Atoi(number.String())
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

func normalizeLabels(nodes []labelNode) []string {
	seen := map[string]struct{}{}
	for _, node := range nodes {
		name := strings.ToLower(strings.TrimSpace(node.Name))
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	labels := make([]string, 0, len(seen))
	for label := range seen {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels
}

func normalizeBlockers(nodes []relationNode) []tracker.BlockerRef {
	blockers := make([]tracker.BlockerRef, 0, len(nodes))
	for _, node := range nodes {
		if !strings.EqualFold(strings.TrimSpace(node.Type), "blocks") {
			continue
		}
		if node.Issue.ID == "" && node.Issue.Identifier == "" {
			continue
		}
		blockers = append(blockers, tracker.BlockerRef{
			ID:         node.Issue.ID,
			Identifier: node.Issue.Identifier,
			State:      node.Issue.State.Name,
		})
	}
	sort.Slice(blockers, func(i, j int) bool {
		if blockers[i].Identifier == blockers[j].Identifier {
			return blockers[i].ID < blockers[j].ID
		}
		return blockers[i].Identifier < blockers[j].Identifier
	})
	return blockers
}
