package linear

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

func TestFetchCandidateIssuesPaginatesAndNormalizes(t *testing.T) {
	var requests []capturedRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		requests = append(requests, request)

		if r.Header.Get("Authorization") != "linear-token" {
			t.Fatalf("Authorization = %q, want linear-token", r.Header.Get("Authorization"))
		}
		assertQueryContains(t, request.Query, "query FetchIssuesByStates($projectSlug: String!, $stateNames: [String!]!, $after: String, $first: Int!)")
		assertQueryContains(t, request.Query, "project: { slugId: { eq: $projectSlug } }")
		assertQueryContains(t, request.Query, "state: { name: { in: $stateNames } }")
		assertQueryContains(t, request.Query, "comments(first: 20)")
		assertQueryContains(t, request.Query, "children(first: 20)")

		if got := request.Variables["projectSlug"]; got != "760daeff8700" {
			t.Fatalf("projectSlug = %v, want 760daeff8700", got)
		}
		if got := stringSliceVariable(t, request, "stateNames"); !reflect.DeepEqual(got, []string{"Todo", "In Progress"}) {
			t.Fatalf("stateNames = %#v", got)
		}
		if got := int(request.Variables["first"].(float64)); got != 1 {
			t.Fatalf("first = %d, want 1", got)
		}

		switch len(requests) {
		case 1:
			if request.Variables["after"] != nil {
				t.Fatalf("first page after = %#v, want nil", request.Variables["after"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"issues": map[string]any{
						"nodes": []any{
							map[string]any{
								"id":          "issue-1",
								"identifier":  "TOO-1",
								"title":       "Build adapter",
								"description": "Read Linear",
								"priority":    2,
								"branchName":  "too-1-build-adapter",
								"url":         "https://linear.app/issue/TOO-1",
								"createdAt":   "2026-05-01T10:00:00Z",
								"updatedAt":   "2026-05-02T10:00:00.123Z",
								"state":       map[string]any{"name": "Todo"},
								"labels": map[string]any{"nodes": []any{
									map[string]any{"name": "Repo:API"},
									map[string]any{"name": "bug"},
									map[string]any{"name": "BUG"},
								}},
								"inverseRelations": map[string]any{"nodes": []any{
									map[string]any{
										"type": "blocks",
										"issue": map[string]any{
											"id":         "blocker-1",
											"identifier": "TOO-0",
											"state":      map[string]any{"name": "Done"},
										},
									},
									map[string]any{
										"type": "relates",
										"issue": map[string]any{
											"id":         "related-1",
											"identifier": "TOO-99",
											"state":      map[string]any{"name": "Todo"},
										},
									},
								}},
								"comments": map[string]any{
									"nodes": []any{
										map[string]any{
											"id":        "comment-root",
											"body":      "top-level comment",
											"createdAt": "2026-05-01T11:00:00Z",
											"updatedAt": "2026-05-01T11:05:00Z",
											"children": map[string]any{
												"nodes": []any{
													map[string]any{
														"id":        "comment-reply",
														"body":      "reply body",
														"parentId":  "comment-root",
														"parent":    map[string]any{"id": "comment-root"},
														"createdAt": "2026-05-01T11:10:00Z",
														"updatedAt": "2026-05-01T11:10:00Z",
													},
												},
												"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
											},
										},
									},
									"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
								},
							},
						},
						"pageInfo": map[string]any{"hasNextPage": true, "endCursor": "cursor-1"},
					},
				},
			})
		case 2:
			if request.Variables["after"] != "cursor-1" {
				t.Fatalf("second page after = %#v, want cursor-1", request.Variables["after"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"issues": map[string]any{
						"nodes": []any{
							map[string]any{
								"id":               "issue-2",
								"identifier":       "TOO-2",
								"title":            "Refresh state",
								"priority":         0,
								"state":            map[string]any{"name": "In Progress"},
								"labels":           map[string]any{"nodes": []any{}},
								"inverseRelations": map[string]any{"nodes": []any{}},
							},
						},
						"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
					},
				},
			})
		default:
			t.Fatalf("unexpected request count %d", len(requests))
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{
		Endpoint:     server.URL,
		APIKey:       "linear-token",
		ProjectSlug:  "760daeff8700",
		ActiveStates: []string{"Todo", "In Progress"},
		PageSize:     1,
	})

	issues, err := client.FetchCandidateIssues(context.Background())
	if err != nil {
		t.Fatalf("FetchCandidateIssues() error = %v", err)
	}
	if len(issues) != 2 {
		t.Fatalf("len(issues) = %d, want 2", len(issues))
	}
	first := issues[0]
	if first.ID != "issue-1" || first.Identifier != "TOO-1" || first.State != "Todo" {
		t.Fatalf("first issue identity = %#v", first)
	}
	if first.Priority == nil || *first.Priority != 2 {
		t.Fatalf("Priority = %v, want 2", first.Priority)
	}
	if !reflect.DeepEqual(first.Labels, []string{"bug", "repo:api"}) {
		t.Fatalf("Labels = %#v, want lowercase sorted unique labels", first.Labels)
	}
	wantBlockers := []tracker.BlockerRef{{ID: "blocker-1", Identifier: "TOO-0", State: "Done"}}
	if !reflect.DeepEqual(first.BlockedBy, wantBlockers) {
		t.Fatalf("BlockedBy = %#v", first.BlockedBy)
	}
	if len(first.Comments) != 2 {
		t.Fatalf("Comments = %#v, want root comment and reply", first.Comments)
	}
	if first.Comments[0].ID != "comment-root" ||
		first.Comments[0].ParentID != "" ||
		first.Comments[0].ThreadRootID != "comment-root" ||
		first.Comments[0].Depth != 0 {
		t.Fatalf("root comment = %#v", first.Comments[0])
	}
	if first.Comments[1].ID != "comment-reply" ||
		first.Comments[1].ParentID != "comment-root" ||
		first.Comments[1].ThreadRootID != "comment-root" ||
		first.Comments[1].Depth != 1 {
		t.Fatalf("reply comment = %#v", first.Comments[1])
	}
	if first.CreatedAt == nil || first.CreatedAt.Format(time.RFC3339) != "2026-05-01T10:00:00Z" {
		t.Fatalf("CreatedAt = %v", first.CreatedAt)
	}
	if first.UpdatedAt == nil || first.UpdatedAt.Format(time.RFC3339Nano) != "2026-05-02T10:00:00.123Z" {
		t.Fatalf("UpdatedAt = %v", first.UpdatedAt)
	}
	if issues[1].Priority != nil {
		t.Fatalf("second issue Priority = %v, want nil for Linear priority 0", issues[1].Priority)
	}
}

func TestFetchIssueStatesByIDsUsesGraphQLIDVariable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		assertQueryContains(t, request.Query, "query RefreshIssueStates($ids: [ID!]!, $first: Int!)")
		assertQueryContains(t, request.Query, "filter: { id: { in: $ids } }")

		if got := stringSliceVariable(t, request, "ids"); !reflect.DeepEqual(got, []string{"issue-1", "issue-2"}) {
			t.Fatalf("ids = %#v", got)
		}
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": []any{
						map[string]any{
							"id":               "issue-1",
							"identifier":       "TOO-1",
							"title":            "Refresh me",
							"state":            map[string]any{"name": "Done"},
							"labels":           map[string]any{"nodes": []any{}},
							"inverseRelations": map[string]any{"nodes": []any{}},
						},
					},
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "760daeff8700"})

	issues, err := client.FetchIssueStatesByIDs(context.Background(), []string{"issue-1", "issue-2"})
	if err != nil {
		t.Fatalf("FetchIssueStatesByIDs() error = %v", err)
	}
	if len(issues) != 1 || issues[0].State != "Done" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestFetchIssuesByStatesSupportsTerminalFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		if got := stringSliceVariable(t, request, "stateNames"); !reflect.DeepEqual(got, []string{"Done", "Closed"}) {
			t.Fatalf("stateNames = %#v", got)
		}
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": []any{
						map[string]any{
							"id":               "issue-done",
							"identifier":       "TOO-9",
							"title":            "Terminal issue",
							"state":            map[string]any{"name": "Done"},
							"labels":           map[string]any{"nodes": []any{}},
							"inverseRelations": map[string]any{"nodes": []any{}},
						},
					},
					"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{
		Endpoint:       server.URL,
		APIKey:         "linear-token",
		ProjectSlug:    "760daeff8700",
		TerminalStates: []string{"Done", "Closed"},
	})

	issues, err := client.FetchTerminalIssues(context.Background())
	if err != nil {
		t.Fatalf("FetchTerminalIssues() error = %v", err)
	}
	if len(issues) != 1 || issues[0].Identifier != "TOO-9" {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestFetchIssuesReportsGraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "bad filter"}}})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "760daeff8700"})
	_, err := client.FetchIssuesByStates(context.Background(), []string{"Todo"})
	if !errors.Is(err, ErrGraphQLErrors) {
		t.Fatalf("error = %v, want ErrGraphQLErrors", err)
	}
}

func TestFetchIssuesReportsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "760daeff8700"})
	_, err := client.FetchIssuesByStates(context.Background(), []string{"Todo"})
	if !errors.Is(err, ErrLinearStatus) {
		t.Fatalf("error = %v, want ErrLinearStatus", err)
	}
}

func TestFetchIssuesReportsMalformedPayload(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload any
	}{
		{
			name:    "missing issues",
			payload: map[string]any{"data": map[string]any{}},
		},
		{
			name: "missing page info",
			payload: map[string]any{
				"data": map[string]any{
					"issues": map[string]any{"nodes": []any{}},
				},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeJSON(w, tt.payload)
			}))
			defer server.Close()

			client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "760daeff8700"})
			_, err := client.FetchIssuesByStates(context.Background(), []string{"Todo"})
			if !errors.Is(err, ErrUnknownPayload) {
				t.Fatalf("error = %v, want ErrUnknownPayload", err)
			}
		})
	}
}

func TestFetchIssuesRequiresEndCursorWhenPaginating(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes":    []any{},
					"pageInfo": map[string]any{"hasNextPage": true, "endCursor": ""},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "760daeff8700"})
	_, err := client.FetchIssuesByStates(context.Background(), []string{"Todo"})
	if !errors.Is(err, ErrMissingEndCursor) {
		t.Fatalf("error = %v, want ErrMissingEndCursor", err)
	}
}

func TestNewValidatesRequiredSettings(t *testing.T) {
	for _, tt := range []struct {
		name string
		cfg  Config
		want error
	}{
		{name: "missing api key", cfg: Config{ProjectSlug: "project"}, want: ErrMissingAPIKey},
		{name: "missing project", cfg: Config{APIKey: "token"}, want: ErrMissingProject},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.cfg)
			if !errors.Is(err, tt.want) {
				t.Fatalf("New() error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestNewFromTrackerConfigRejectsUnsupportedKind(t *testing.T) {
	_, err := NewFromTrackerConfig(config.Tracker{
		Kind:        "github",
		Endpoint:    "https://example.test/graphql",
		APIKey:      "token",
		ProjectSlug: "project",
	})
	if !errors.Is(err, ErrUnsupportedKind) {
		t.Fatalf("NewFromTrackerConfig() error = %v, want ErrUnsupportedKind", err)
	}
}

type capturedRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

func captureRequest(t *testing.T, r *http.Request) capturedRequest {
	t.Helper()

	if r.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", r.Method)
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", r.Header.Get("Content-Type"))
	}

	var request capturedRequest
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		t.Fatalf("decode request: %v", err)
	}
	return request
}

func stringSliceVariable(t *testing.T, request capturedRequest, key string) []string {
	t.Helper()

	raw, ok := request.Variables[key].([]any)
	if !ok {
		t.Fatalf("variable %s = %#v, want []any", key, request.Variables[key])
	}
	values := make([]string, 0, len(raw))
	for _, value := range raw {
		text, ok := value.(string)
		if !ok {
			t.Fatalf("variable %s item = %#v, want string", key, value)
		}
		values = append(values, text)
	}
	return values
}

func assertQueryContains(t *testing.T, query string, want string) {
	t.Helper()

	if !strings.Contains(query, want) {
		t.Fatalf("query missing %q:\n%s", want, query)
	}
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func mustClient(t *testing.T, cfg Config) *Client {
	t.Helper()

	client, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}
