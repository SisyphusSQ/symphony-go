// Package safety contains production baseline controls shared by runtime paths.
package safety

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

const (
	Redacted     = "[REDACTED]"
	RedactedPath = "[REDACTED_PATH]"
)

const minLiteralLength = 4

var (
	secretKeyPattern = regexp.MustCompile(`(?i)(api[_-]?key|token|secret|password|passwd|access[_-]?key|secret[_-]?key|client[_-]?secret|authorization|pat)`)
	tokenPattern     = regexp.MustCompile(`\b(gh[pousr]_[A-Za-z0-9_]{8,}|github_pat_[A-Za-z0-9_]+|sk-[A-Za-z0-9_-]{8,}|xox[baprs]-[A-Za-z0-9-]{8,})\b`)
	keyValuePattern  = regexp.MustCompile(`(?i)\b(api[_-]?key|token|secret|password|passwd|access[_-]?key|secret[_-]?key|client[_-]?secret|authorization|pat)\b([[:space:]]*[:=][[:space:]]*)("[^"]+"|'[^']+'|[^[:space:],;]+)`)
	pathPattern      = regexp.MustCompile(`(^|[[:space:]=:"'])(/[^[:space:],"']*(?:\.env|credentials?|secrets?|tokens?)[^[:space:],"']*)`)
)

// Redactor redacts known secret literals and common secret-bearing shapes.
type Redactor struct {
	literals []string
}

// NewRedactor creates a redactor from the current runtime config.
func NewRedactor(cfg config.Config) Redactor {
	return NewRedactorFromLiterals(cfg.Tracker.APIKey)
}

// NewRedactorFromLiterals creates a redactor with additional exact secret
// values. Empty and tiny literals are ignored to avoid noisy over-redaction.
func NewRedactorFromLiterals(literals ...string) Redactor {
	seen := map[string]struct{}{}
	normalized := make([]string, 0, len(literals))
	for _, literal := range literals {
		literal = strings.TrimSpace(literal)
		if len(literal) < minLiteralLength {
			continue
		}
		if _, ok := seen[literal]; ok {
			continue
		}
		seen[literal] = struct{}{}
		normalized = append(normalized, literal)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return len(normalized[i]) > len(normalized[j])
	})
	return Redactor{literals: normalized}
}

// String redacts secret-looking content in a single string.
func (r Redactor) String(value string) string {
	if value == "" {
		return ""
	}
	redacted := value
	for _, literal := range r.literals {
		redacted = strings.ReplaceAll(redacted, literal, Redacted)
	}
	redacted = keyValuePattern.ReplaceAllString(redacted, `$1$2`+Redacted)
	redacted = tokenPattern.ReplaceAllString(redacted, Redacted)
	redacted = pathPattern.ReplaceAllString(redacted, `${1}`+RedactedPath)
	return redacted
}

// Any recursively redacts string values and secret-like keyed fields.
func (r Redactor) Any(value any) any {
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return r.String(typed)
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, raw := range typed {
			if secretKeyPattern.MatchString(key) {
				result[key] = Redacted
				continue
			}
			result[key] = r.Any(raw)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = r.Any(item)
		}
		return result
	case []string:
		result := make([]string, len(typed))
		for i, item := range typed {
			result[i] = r.String(item)
		}
		return result
	default:
		return value
	}
}

// Event returns a redacted copy of an observability event.
func (r Redactor) Event(event observability.Event) observability.Event {
	event.Message = r.String(event.Message)
	event.IssueID = r.String(event.IssueID)
	event.IssueIdentifier = r.String(event.IssueIdentifier)
	event.SessionID = r.String(event.SessionID)
	event.RunStatus = r.String(event.RunStatus)
	event.RetryState = r.String(event.RetryState)
	event.Error = r.String(event.Error)
	if event.Fields != nil {
		if fields, ok := r.Any(event.Fields).(map[string]any); ok {
			event.Fields = fields
		}
	}
	return event
}

// JSON redacts a JSON payload while preserving JSON shape when possible.
func (r Redactor) JSON(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return "{}"
	}

	var decoded any
	if err := json.Unmarshal([]byte(payload), &decoded); err != nil {
		encoded, _ := json.Marshal(r.String(payload))
		return string(encoded)
	}
	redacted, err := json.Marshal(r.Any(decoded))
	if err != nil {
		return "{}"
	}
	return string(redacted)
}

// ConfigEvent redacts an observability event using secrets visible in cfg.
func ConfigEvent(cfg config.Config, event observability.Event) observability.Event {
	return NewRedactor(cfg).Event(event)
}
