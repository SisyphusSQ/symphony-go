package linear

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateAndUpdateIssueComment(t *testing.T) {
	var sawCreate bool
	var sawUpdate bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		if r.Header.Get("Authorization") != "linear-token" {
			t.Fatalf("Authorization = %q, want linear-token", r.Header.Get("Authorization"))
		}

		switch {
		case strings.Contains(request.Query, "mutation CreateIssueComment"):
			sawCreate = true
			if request.Variables["issueID"] != "issue-1" {
				t.Fatalf("create issueID = %#v", request.Variables["issueID"])
			}
			if request.Variables["body"] != "created body" {
				t.Fatalf("create body = %#v", request.Variables["body"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"commentCreate": map[string]any{
						"success": true,
						"comment": map[string]any{
							"id":        "comment-1",
							"body":      "created body",
							"createdAt": "2026-05-03T12:00:00Z",
							"updatedAt": "2026-05-03T12:00:00Z",
						},
					},
				},
			})
		case strings.Contains(request.Query, "mutation UpdateIssueComment"):
			sawUpdate = true
			if request.Variables["commentID"] != "comment-1" {
				t.Fatalf("update commentID = %#v", request.Variables["commentID"])
			}
			if request.Variables["body"] != "updated body" {
				t.Fatalf("update body = %#v", request.Variables["body"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"commentUpdate": map[string]any{
						"success": true,
						"comment": map[string]any{
							"id":        "comment-1",
							"body":      "updated body",
							"createdAt": "2026-05-03T12:00:00Z",
							"updatedAt": "2026-05-03T12:05:00Z",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected query:\n%s", request.Query)
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	created, err := client.CreateIssueComment(context.Background(), IssueCommentCreateInput{
		IssueID: "issue-1",
		Body:    "created body",
	})
	if err != nil {
		t.Fatalf("CreateIssueComment() error = %v", err)
	}
	if created.ID != "comment-1" || created.Body != "created body" || created.CreatedAt == nil {
		t.Fatalf("created comment = %#v", created)
	}

	updated, err := client.UpdateIssueComment(context.Background(), IssueCommentUpdateInput{
		CommentID: "comment-1",
		Body:      "updated body",
	})
	if err != nil {
		t.Fatalf("UpdateIssueComment() error = %v", err)
	}
	if updated.ID != "comment-1" || updated.Body != "updated body" || updated.UpdatedAt == nil {
		t.Fatalf("updated comment = %#v", updated)
	}
	if !sawCreate || !sawUpdate {
		t.Fatalf("sawCreate=%v sawUpdate=%v, want both true", sawCreate, sawUpdate)
	}
}

func TestCreateIssueCommentReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		assertQueryContains(t, request.Query, "mutation CreateIssueCommentReply")
		assertQueryContains(t, request.Query, "parentId: $parentCommentID")
		if request.Variables["issueID"] != "issue-1" {
			t.Fatalf("issueID = %#v", request.Variables["issueID"])
		}
		if request.Variables["parentCommentID"] != "comment-parent" {
			t.Fatalf("parentCommentID = %#v", request.Variables["parentCommentID"])
		}
		if request.Variables["body"] != "reply body" {
			t.Fatalf("body = %#v", request.Variables["body"])
		}
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"commentCreate": map[string]any{
					"success": true,
					"comment": map[string]any{
						"id":        "comment-reply",
						"body":      "reply body",
						"parentId":  "comment-parent",
						"parent":    map[string]any{"id": "comment-parent"},
						"createdAt": "2026-05-14T08:00:00Z",
						"updatedAt": "2026-05-14T08:00:00Z",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	reply, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
		IssueID:         "issue-1",
		ParentCommentID: "comment-parent",
		Body:            "reply body",
	})
	if err != nil {
		t.Fatalf("CreateIssueCommentReply() error = %v", err)
	}
	if reply.ID != "comment-reply" ||
		reply.ParentID != "comment-parent" ||
		reply.ThreadRootID != "comment-parent" ||
		reply.Depth != 1 {
		t.Fatalf("reply = %#v", reply)
	}
}

func TestUpsertIssueWorkpadCreatesWhenMissing(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		request := captureRequest(t, r)
		switch requests {
		case 1:
			assertQueryContains(t, request.Query, "query IssueComments")
			if request.Variables["issueID"] != "issue-1" {
				t.Fatalf("issueID = %#v", request.Variables["issueID"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id": "issue-1",
						"comments": map[string]any{
							"nodes":    []any{},
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
						},
					},
				},
			})
		case 2:
			assertQueryContains(t, request.Query, "mutation CreateIssueComment")
			if request.Variables["body"] != "## Symphony Workpad\n\nfresh body" {
				t.Fatalf("body = %#v", request.Variables["body"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"commentCreate": map[string]any{
						"success": true,
						"comment": map[string]any{
							"id":        "comment-new",
							"body":      "## Symphony Workpad\n\nfresh body",
							"createdAt": "2026-05-03T12:00:00Z",
							"updatedAt": "2026-05-03T12:00:00Z",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	result, err := client.UpsertIssueWorkpad(context.Background(), WorkpadUpsertInput{
		IssueID: "issue-1",
		Heading: "## Symphony Workpad",
		Body:    "## Symphony Workpad\n\nfresh body",
	})
	if err != nil {
		t.Fatalf("UpsertIssueWorkpad() error = %v", err)
	}
	if !result.Created || result.MatchedComments != 0 || result.Comment.ID != "comment-new" {
		t.Fatalf("result = %#v", result)
	}
}

func TestUpsertIssueWorkpadUpdatesNewestMatchWithoutCreatingDuplicate(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		request := captureRequest(t, r)
		switch requests {
		case 1:
			assertQueryContains(t, request.Query, "query IssueComments")
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id": "issue-1",
						"comments": map[string]any{
							"nodes": []any{
								map[string]any{
									"id":        "comment-old",
									"body":      "## Symphony Workpad\n\nold",
									"createdAt": "2026-05-03T10:00:00Z",
									"updatedAt": "2026-05-03T10:00:00Z",
								},
								map[string]any{
									"id":        "comment-other",
									"body":      "## Other Workpad\n\nignore",
									"createdAt": "2026-05-03T11:00:00Z",
									"updatedAt": "2026-05-03T11:00:00Z",
								},
								map[string]any{
									"id":        "comment-stale",
									"body":      "## Symphony Workpad (stale)\n\nignore",
									"createdAt": "2026-05-03T11:15:00Z",
									"updatedAt": "2026-05-03T12:30:00Z",
								},
								map[string]any{
									"id":        "comment-newer",
									"body":      "## Symphony Workpad\n\nnewer",
									"createdAt": "2026-05-03T11:30:00Z",
									"updatedAt": "2026-05-03T12:00:00Z",
								},
							},
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
						},
					},
				},
			})
		case 2:
			assertQueryContains(t, request.Query, "mutation UpdateIssueComment")
			if request.Variables["commentID"] != "comment-newer" {
				t.Fatalf("commentID = %#v, want newest matching comment", request.Variables["commentID"])
			}
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"commentUpdate": map[string]any{
						"success": true,
						"comment": map[string]any{
							"id":        "comment-newer",
							"body":      "## Symphony Workpad\n\nupdated",
							"createdAt": "2026-05-03T11:30:00Z",
							"updatedAt": "2026-05-03T12:05:00Z",
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	result, err := client.UpsertIssueWorkpad(context.Background(), WorkpadUpsertInput{
		IssueID: "issue-1",
		Heading: "## Symphony Workpad",
		Body:    "## Symphony Workpad\n\nupdated",
	})
	if err != nil {
		t.Fatalf("UpsertIssueWorkpad() error = %v", err)
	}
	if result.Created || result.MatchedComments != 2 || result.Comment.ID != "comment-newer" {
		t.Fatalf("result = %#v", result)
	}
}

func TestTransitionIssueStateByIDSkipsStateLookup(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		request := captureRequest(t, r)
		if strings.Contains(request.Query, "query IssueTeamStates") {
			t.Fatal("TransitionIssueState with StateID should not lookup team states")
		}
		assertQueryContains(t, request.Query, "mutation TransitionIssueState")
		if request.Variables["stateID"] != "state-in-progress" {
			t.Fatalf("stateID = %#v", request.Variables["stateID"])
		}
		writeJSON(w, transitionPayload("issue-1", "TOO-1", "state-in-progress", "In Progress", "started"))
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	result, err := client.TransitionIssueState(context.Background(), IssueStateTransitionInput{
		IssueID: "issue-1",
		StateID: "state-in-progress",
	})
	if err != nil {
		t.Fatalf("TransitionIssueState() error = %v", err)
	}
	if result.State.Name != "In Progress" || result.Identifier != "TOO-1" || requests != 1 {
		t.Fatalf("result=%#v requests=%d", result, requests)
	}
}

func TestTransitionIssueStateByNameLooksUpTeamState(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		request := captureRequest(t, r)
		switch requests {
		case 1:
			assertQueryContains(t, request.Query, "query IssueTeamStates")
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"issue": map[string]any{
						"id": "issue-1",
						"team": map[string]any{
							"states": map[string]any{"nodes": []any{
								map[string]any{"id": "state-todo", "name": "Todo", "type": "unstarted"},
								map[string]any{"id": "state-human-review", "name": "Human Review", "type": "started"},
							}},
						},
					},
				},
			})
		case 2:
			assertQueryContains(t, request.Query, "mutation TransitionIssueState")
			if request.Variables["stateID"] != "state-human-review" {
				t.Fatalf("stateID = %#v", request.Variables["stateID"])
			}
			writeJSON(w, transitionPayload("issue-1", "TOO-1", "state-human-review", "Human Review", "started"))
		default:
			t.Fatalf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	result, err := client.TransitionIssueState(context.Background(), IssueStateTransitionInput{
		IssueID:   "issue-1",
		StateName: "human review",
	})
	if err != nil {
		t.Fatalf("TransitionIssueState() error = %v", err)
	}
	if result.State.ID != "state-human-review" || requests != 2 {
		t.Fatalf("result=%#v requests=%d", result, requests)
	}
}

func TestTransitionIssueStateMissingStateDoesNotMutate(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		request := captureRequest(t, r)
		if requests != 1 {
			t.Fatalf("unexpected mutation after missing state:\n%s", request.Query)
		}
		assertQueryContains(t, request.Query, "query IssueTeamStates")
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"issue": map[string]any{
					"id": "issue-1",
					"team": map[string]any{
						"states": map[string]any{"nodes": []any{
							map[string]any{"id": "state-todo", "name": "Todo", "type": "unstarted"},
						}},
					},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	_, err := client.TransitionIssueState(context.Background(), IssueStateTransitionInput{
		IssueID:   "issue-1",
		StateName: "Done",
	})
	if !errors.Is(err, ErrStateNotFound) {
		t.Fatalf("error = %v, want ErrStateNotFound", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestLinkIssueURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		request := captureRequest(t, r)
		assertQueryContains(t, request.Query, "mutation LinkIssueURL")
		if request.Variables["issueID"] != "issue-1" {
			t.Fatalf("issueID = %#v", request.Variables["issueID"])
		}
		if request.Variables["url"] != "https://github.com/SisyphusSQ/symphony-go/pull/10" {
			t.Fatalf("url = %#v", request.Variables["url"])
		}
		if request.Variables["title"] != "PR #10" {
			t.Fatalf("title = %#v", request.Variables["title"])
		}
		writeJSON(w, map[string]any{
			"data": map[string]any{
				"attachmentLinkURL": map[string]any{
					"success": true,
					"attachment": map[string]any{
						"id":         "attachment-1",
						"title":      "PR #10",
						"url":        "https://github.com/SisyphusSQ/symphony-go/pull/10",
						"sourceType": "github",
					},
				},
			},
		})
	}))
	defer server.Close()

	client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
	attachment, err := client.LinkIssueURL(context.Background(), IssueURLAttachmentInput{
		IssueID: "issue-1",
		URL:     "https://github.com/SisyphusSQ/symphony-go/pull/10",
		Title:   "PR #10",
	})
	if err != nil {
		t.Fatalf("LinkIssueURL() error = %v", err)
	}
	if attachment.ID != "attachment-1" || attachment.SourceType != "github" {
		t.Fatalf("attachment = %#v", attachment)
	}
}

func TestWriteAPIErrors(t *testing.T) {
	t.Run("http status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "forbidden", http.StatusForbidden)
		}))
		defer server.Close()

		client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueComment(context.Background(), IssueCommentCreateInput{
			IssueID: "issue-1",
			Body:    "body",
		})
		if !errors.Is(err, ErrLinearStatus) {
			t.Fatalf("error = %v, want ErrLinearStatus", err)
		}
	})

	t.Run("missing comment graphql error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "comment not found"}}})
		}))
		defer server.Close()

		client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.UpdateIssueComment(context.Background(), IssueCommentUpdateInput{
			CommentID: "missing-comment",
			Body:      "body",
		})
		if !errors.Is(err, ErrGraphQLErrors) {
			t.Fatalf("error = %v, want ErrGraphQLErrors", err)
		}
	})

	t.Run("missing issue on workpad lookup", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"data": map[string]any{"issue": nil}})
		}))
		defer server.Close()

		client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.UpsertIssueWorkpad(context.Background(), WorkpadUpsertInput{
			IssueID: "missing-issue",
			Heading: "## Symphony Workpad",
			Body:    "## Symphony Workpad\n\nbody",
		})
		if !errors.Is(err, ErrIssueNotFound) {
			t.Fatalf("error = %v, want ErrIssueNotFound", err)
		}
	})

	t.Run("invalid attachment url", func(t *testing.T) {
		client := mustClient(t, Config{Endpoint: "http://127.0.0.1", APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.LinkIssueURL(context.Background(), IssueURLAttachmentInput{
			IssueID: "issue-1",
			URL:     "not-a-url",
		})
		if !errors.Is(err, ErrInvalidAttachmentURL) {
			t.Fatalf("error = %v, want ErrInvalidAttachmentURL", err)
		}
	})

	t.Run("missing required fields", func(t *testing.T) {
		client := mustClient(t, Config{Endpoint: "http://127.0.0.1", APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.UpdateIssueComment(context.Background(), IssueCommentUpdateInput{Body: "body"})
		if !errors.Is(err, ErrMissingCommentID) {
			t.Fatalf("error = %v, want ErrMissingCommentID", err)
		}
	})

	t.Run("missing issue id for reply", func(t *testing.T) {
		client := mustClient(t, Config{Endpoint: "http://127.0.0.1", APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
			ParentCommentID: "comment-parent",
			Body:            "body",
		})
		if !errors.Is(err, ErrMissingIssueID) {
			t.Fatalf("error = %v, want ErrMissingIssueID", err)
		}
	})

	t.Run("missing parent comment id for reply", func(t *testing.T) {
		client := mustClient(t, Config{Endpoint: "http://127.0.0.1", APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
			IssueID: "issue-1",
			Body:    "body",
		})
		if !errors.Is(err, ErrMissingParentCommentID) {
			t.Fatalf("error = %v, want ErrMissingParentCommentID", err)
		}
	})

	t.Run("missing body for reply", func(t *testing.T) {
		client := mustClient(t, Config{Endpoint: "http://127.0.0.1", APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
			IssueID:         "issue-1",
			ParentCommentID: "comment-parent",
		})
		if !errors.Is(err, ErrMissingCommentBody) {
			t.Fatalf("error = %v, want ErrMissingCommentBody", err)
		}
	})

	t.Run("reply graphql error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"errors": []any{map[string]any{"message": "parent missing"}}})
		}))
		defer server.Close()

		client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
			IssueID:         "issue-1",
			ParentCommentID: "missing-parent",
			Body:            "body",
		})
		if !errors.Is(err, ErrGraphQLErrors) {
			t.Fatalf("error = %v, want ErrGraphQLErrors", err)
		}
	})

	t.Run("reply unknown payload", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{
				"data": map[string]any{
					"commentCreate": map[string]any{
						"success": true,
						"comment": map[string]any{"body": "body"},
					},
				},
			})
		}))
		defer server.Close()

		client := mustClient(t, Config{Endpoint: server.URL, APIKey: "linear-token", ProjectSlug: "project"})
		_, err := client.CreateIssueCommentReply(context.Background(), IssueCommentReplyCreateInput{
			IssueID:         "issue-1",
			ParentCommentID: "comment-parent",
			Body:            "body",
		})
		if !errors.Is(err, ErrUnknownPayload) {
			t.Fatalf("error = %v, want ErrUnknownPayload", err)
		}
	})
}

func transitionPayload(issueID string, identifier string, stateID string, stateName string, stateType string) map[string]any {
	return map[string]any{
		"data": map[string]any{
			"issueUpdate": map[string]any{
				"success": true,
				"issue": map[string]any{
					"id":         issueID,
					"identifier": identifier,
					"state": map[string]any{
						"id":   stateID,
						"name": stateName,
						"type": stateType,
					},
				},
			},
		},
	}
}
