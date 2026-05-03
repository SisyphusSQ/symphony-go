package lineargraphql

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

func TestExecuteSuccessWithVariablesAndAuth(t *testing.T) {
	var sawAuth string
	var sawRequest graphQLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&sawRequest); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"issue":{"identifier":"TOO-128"}}}`))
	}))
	defer server.Close()

	tool := newTestTool(t, server.URL)
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{
		"query": "query Issue($id: String!) { issue(id: $id) { identifier } }",
		"variables": {"id": "TOO-128"}
	}`))
	if !result.Success {
		t.Fatalf("result = %#v", result.Output)
	}
	if sawAuth != "linear-token" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if sawRequest.Query == "" || !strings.Contains(sawRequest.Query, "Issue") {
		t.Fatalf("Query = %q", sawRequest.Query)
	}
	var variables map[string]string
	if err := json.Unmarshal(sawRequest.Variables, &variables); err != nil {
		t.Fatalf("variables decode: %v", err)
	}
	if variables["id"] != "TOO-128" {
		t.Fatalf("variables = %#v", variables)
	}
	var output Output
	mustDecodeOutput(t, result, &output)
	if !output.Success || !strings.Contains(string(output.Response), "TOO-128") {
		t.Fatalf("output = %#v", output)
	}
}

func TestExecuteAcceptsRawQueryString(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Query != "{ viewer { id } }" {
			t.Fatalf("Query = %q", request.Query)
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"user-1"}}}`))
	}))
	defer server.Close()

	tool := newTestTool(t, server.URL)
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`"{ viewer { id } }"`))
	if !result.Success {
		t.Fatalf("result = %#v", result.Output)
	}
}

func TestExecuteTransportError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should be closed before request")
	}))
	server.Close()

	tool := newTestTool(t, server.URL)
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{"query":"{ viewer { id } }"}`))
	assertFailureCode(t, result, "transport_error")
}

func TestExecuteInvalidJSON(t *testing.T) {
	tool := newTestTool(t, "http://127.0.0.1")
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{"query":`))
	assertFailureCode(t, result, "invalid_json")
}

func TestExecuteRejectsNonObjectVariables(t *testing.T) {
	tool := newTestTool(t, "http://127.0.0.1")
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{"query":"{ viewer { id } }","variables":[]}`))
	assertFailureCode(t, result, "invalid_input")
}

func TestExecuteGraphQLErrorIsFailureAndPreservesBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errors":[{"message":"Forbidden"}],"data":{"viewer":null}}`))
	}))
	defer server.Close()

	tool := newTestTool(t, server.URL)
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{"query":"{ viewer { id } }"}`))
	assertFailureCode(t, result, "graphql_errors")
	if !strings.Contains(string(result.Output.Response), `"errors"`) {
		t.Fatalf("Response = %s, want preserved GraphQL body", result.Output.Response)
	}
}

func TestExecuteRejectsMultipleOperations(t *testing.T) {
	tool := newTestTool(t, "http://127.0.0.1")
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{"query":"query A { viewer { id } } mutation B { issueUpdate(id: \"1\", input: {}) { success } }"}`))
	assertFailureCode(t, result, "invalid_input")
}

func TestExecuteAllowsOneOperationWithFragment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"viewer":{"id":"user-1"}}}`))
	}))
	defer server.Close()

	tool := newTestTool(t, server.URL)
	result := tool.ExecuteJSON(context.Background(), json.RawMessage(`{
		"query": "query Viewer { viewer { ...UserFields } } fragment UserFields on User { id }"
	}`))
	if !result.Success {
		t.Fatalf("result = %#v", result.Output)
	}
}

func TestAvailableOnlyForLinearTrackerWithAuth(t *testing.T) {
	if !Available(config.Tracker{Kind: config.TrackerKindLinear, APIKey: "token"}) {
		t.Fatal("Available(linear with token) = false")
	}
	if Available(config.Tracker{Kind: "github", APIKey: "token"}) {
		t.Fatal("Available(non-linear) = true")
	}
	if Available(config.Tracker{APIKey: "token"}) {
		t.Fatal("Available(empty kind) = true")
	}
	if Available(config.Tracker{Kind: config.TrackerKindLinear}) {
		t.Fatal("Available(linear without token) = true")
	}
}

func TestNewFromTrackerConfigRequiresLinearKind(t *testing.T) {
	if _, err := NewFromTrackerConfig(config.Tracker{APIKey: "token"}); !errors.Is(err, ErrUnsupportedTrackerKind) {
		t.Fatalf("NewFromTrackerConfig(empty kind) error = %v, want ErrUnsupportedTrackerKind", err)
	}
}

func newTestTool(t *testing.T, endpoint string) *Tool {
	t.Helper()
	tool, err := New(Config{Endpoint: endpoint, APIKey: "linear-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return tool
}

func mustDecodeOutput(t *testing.T, result Result, out *Output) {
	t.Helper()
	if err := json.Unmarshal([]byte(result.Text()), out); err != nil {
		t.Fatalf("decode output text: %v", err)
	}
}

func assertFailureCode(t *testing.T, result Result, code string) {
	t.Helper()
	if result.Success {
		t.Fatalf("Success = true, want failure")
	}
	var output Output
	mustDecodeOutput(t, result, &output)
	if output.Success || output.Error == nil || output.Error.Code != code {
		t.Fatalf("output = %#v, want code %q", output, code)
	}
}
