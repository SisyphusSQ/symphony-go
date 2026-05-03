package safety

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

func TestRedactorRedactsLiteralSecretKeysTokensAndSecretPaths(t *testing.T) {
	redactor := NewRedactorFromLiterals("literal-secret-value")

	input := strings.Join([]string{
		"api_key=literal-secret-value",
		"Authorization: Bearer gho_1234567890abcdef",
		`password="open-sesame"`,
		"file=/Users/example/.env.local",
	}, " ")

	got := redactor.String(input)
	for _, leaked := range []string{
		"literal-secret-value",
		"gho_1234567890abcdef",
		"open-sesame",
		"/Users/example/.env.local",
	} {
		if strings.Contains(got, leaked) {
			t.Fatalf("redacted string leaked %q: %s", leaked, got)
		}
	}
	for _, want := range []string{Redacted, RedactedPath} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted string missing %q: %s", want, got)
		}
	}
}

func TestRedactorRedactsNestedValuesBySecretLikeKeys(t *testing.T) {
	redactor := NewRedactorFromLiterals("literal-secret-value")
	got := redactor.Any(map[string]any{
		"safe": "literal-secret-value",
		"nested": map[string]any{
			"client_secret": "nested-secret",
			"items": []any{
				"token=abc123456789",
				map[string]any{"password": "pass"},
			},
		},
	}).(map[string]any)

	if got["safe"] != Redacted {
		t.Fatalf("safe literal = %#v, want redacted", got["safe"])
	}
	nested := got["nested"].(map[string]any)
	if nested["client_secret"] != Redacted {
		t.Fatalf("client_secret = %#v, want redacted", nested["client_secret"])
	}
	items := nested["items"].([]any)
	if strings.Contains(items[0].(string), "abc123456789") {
		t.Fatalf("token item leaked: %#v", items[0])
	}
	if items[1].(map[string]any)["password"] != Redacted {
		t.Fatalf("password item = %#v, want redacted", items[1])
	}
}

func TestConfigEventRedactsLoggerPayload(t *testing.T) {
	event := ConfigEvent(config.Config{
		Tracker: config.Tracker{APIKey: "linear-secret-token"},
	}, observability.Event{
		Type:    observability.EventAgentRunFailed,
		Message: "token=linear-secret-token",
		Error:   "stderr contains linear-secret-token",
		Fields: map[string]any{
			"workspace_path": "/tmp/repo/.env.local",
			"authorization":  "Bearer linear-secret-token",
		},
	})

	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	for _, leaked := range []string{"linear-secret-token", "/tmp/repo/.env.local"} {
		if strings.Contains(string(raw), leaked) {
			t.Fatalf("redacted event leaked %q: %s", leaked, raw)
		}
	}
}

func TestJSONRedactsStructuredAndMalformedPayloads(t *testing.T) {
	redactor := NewRedactorFromLiterals("secret-token")
	got := redactor.JSON(`{"token":"secret-token","safe":"prefix secret-token suffix"}`)
	if strings.Contains(got, "secret-token") || !strings.Contains(got, Redacted) {
		t.Fatalf("redacted JSON = %s", got)
	}

	malformed := redactor.JSON(`token=secret-token`)
	if strings.Contains(malformed, "secret-token") {
		t.Fatalf("redacted malformed JSON leaked: %s", malformed)
	}
}
