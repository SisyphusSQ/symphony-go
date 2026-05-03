package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

var (
	ErrTemplateParse  = errors.New("template_parse_error")
	ErrTemplateRender = errors.New("template_render_error")
)

var interpolationPattern = regexp.MustCompile(`{{\s*([^{}]+?)\s*}}`)

// TemplateError reports strict prompt rendering failures.
type TemplateError struct {
	Kind       error
	Expression string
	Message    string
}

func (err *TemplateError) Error() string {
	if err == nil {
		return ""
	}
	parts := []string{err.Kind.Error()}
	if err.Expression != "" {
		parts = append(parts, "expression="+err.Expression)
	}
	if err.Message != "" {
		parts = append(parts, err.Message)
	}
	return strings.Join(parts, ": ")
}

func (err *TemplateError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Kind
}

// RenderPrompt renders the supported Liquid-style interpolation subset with
// strict variable and filter checking.
func RenderPrompt(template string, issue tracker.Issue, attempt *int) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", templateError(ErrTemplateRender, "", "prompt template is required")
	}
	if strings.Contains(template, "{%") || strings.Contains(template, "{#") {
		return "", templateError(ErrTemplateParse, "", "template tags are not supported")
	}
	matches := interpolationPattern.FindAllString(template, -1)
	if strings.Count(template, "{{") != len(matches) || strings.Count(template, "}}") != len(matches) {
		return "", templateError(ErrTemplateParse, "", "unbalanced interpolation delimiters")
	}

	var renderErr error
	rendered := interpolationPattern.ReplaceAllStringFunc(template, func(match string) string {
		if renderErr != nil {
			return match
		}
		submatches := interpolationPattern.FindStringSubmatch(match)
		if len(submatches) != 2 {
			renderErr = templateError(ErrTemplateParse, match, "invalid interpolation")
			return match
		}
		expression := strings.TrimSpace(submatches[1])
		value, err := resolveExpression(expression, issue, attempt)
		if err != nil {
			renderErr = err
			return match
		}
		return renderValue(value)
	})
	if renderErr != nil {
		return "", renderErr
	}
	return rendered, nil
}

func resolveExpression(expression string, issue tracker.Issue, attempt *int) (any, error) {
	if expression == "" {
		return nil, templateError(ErrTemplateParse, expression, "empty interpolation")
	}
	if strings.Contains(expression, "|") {
		return nil, templateError(ErrTemplateRender, expression, "unknown filter")
	}
	if strings.ContainsAny(expression, " \t\r\n") {
		return nil, templateError(ErrTemplateParse, expression, "invalid whitespace in expression")
	}

	parts := strings.Split(expression, ".")
	for _, part := range parts {
		if part == "" {
			return nil, templateError(ErrTemplateParse, expression, "invalid dotted path")
		}
	}

	switch parts[0] {
	case "issue":
		return resolveIssuePath(expression, issue, parts[1:])
	case "attempt":
		if len(parts) != 1 {
			return nil, templateError(ErrTemplateRender, expression, "attempt has no nested fields")
		}
		if attempt == nil {
			return nil, nil
		}
		return *attempt, nil
	default:
		return nil, templateError(ErrTemplateRender, expression, "unknown variable")
	}
}

func resolveIssuePath(expression string, issue tracker.Issue, path []string) (any, error) {
	value := any(issueTemplateData(issue))
	if len(path) == 0 {
		return value, nil
	}
	for _, part := range path {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, templateError(ErrTemplateRender, expression, "field is not an object")
		}
		next, ok := object[part]
		if !ok {
			return nil, templateError(ErrTemplateRender, expression, "unknown issue field")
		}
		value = next
	}
	return value, nil
}

func issueTemplateData(issue tracker.Issue) map[string]any {
	data := map[string]any{
		"id":          issue.ID,
		"identifier":  issue.Identifier,
		"title":       issue.Title,
		"description": issue.Description,
		"state":       issue.State,
		"branch_name": issue.BranchName,
		"branchName":  issue.BranchName,
		"url":         issue.URL,
		"labels":      append([]string(nil), issue.Labels...),
		"blocked_by":  blockerData(issue.BlockedBy),
		"blockedBy":   blockerData(issue.BlockedBy),
	}
	if issue.Priority != nil {
		data["priority"] = *issue.Priority
	} else {
		data["priority"] = nil
	}
	if issue.CreatedAt != nil {
		created := issue.CreatedAt.UTC().Format(time.RFC3339)
		data["created_at"] = created
		data["createdAt"] = created
	} else {
		data["created_at"] = nil
		data["createdAt"] = nil
	}
	if issue.UpdatedAt != nil {
		updated := issue.UpdatedAt.UTC().Format(time.RFC3339)
		data["updated_at"] = updated
		data["updatedAt"] = updated
	} else {
		data["updated_at"] = nil
		data["updatedAt"] = nil
	}
	return data
}

func blockerData(blockers []tracker.BlockerRef) []map[string]any {
	result := make([]map[string]any, 0, len(blockers))
	for _, blocker := range blockers {
		result = append(result, map[string]any{
			"id":         blocker.ID,
			"identifier": blocker.Identifier,
			"state":      blocker.State,
		})
	}
	return result
}

func renderValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case int, int64, int32, float64, float32, bool:
		return fmt.Sprint(typed)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return fmt.Sprint(typed)
		}
		return string(encoded)
	}
}

func templateError(kind error, expression string, message string) error {
	return &TemplateError{Kind: kind, Expression: expression, Message: message}
}
