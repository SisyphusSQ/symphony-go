package linear

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var (
	ErrMissingIssueID         = errors.New("missing_issue_id")
	ErrMissingCommentID       = errors.New("missing_comment_id")
	ErrMissingParentCommentID = errors.New("missing_parent_comment_id")
	ErrMissingCommentBody     = errors.New("missing_comment_body")
	ErrMissingWorkpadHeading  = errors.New("missing_workpad_heading")
	ErrMissingState           = errors.New("missing_issue_state")
	ErrStateNotFound          = errors.New("linear_state_not_found")
	ErrIssueNotFound          = errors.New("linear_issue_not_found")
	ErrMissingAttachmentURL   = errors.New("missing_attachment_url")
	ErrInvalidAttachmentURL   = errors.New("invalid_attachment_url")
	ErrMutationUnsuccessful   = errors.New("linear_mutation_unsuccessful")
)

// IssueComment is the typed result returned by Linear comment write operations.
type IssueComment struct {
	ID           string
	Body         string
	ParentID     string
	ThreadRootID string
	Depth        int
	CreatedAt    *time.Time
	UpdatedAt    *time.Time
}

type IssueCommentCreateInput struct {
	IssueID string
	Body    string
}

type IssueCommentReplyCreateInput struct {
	IssueID         string
	ParentCommentID string
	Body            string
}

type IssueCommentUpdateInput struct {
	CommentID string
	Body      string
}

type WorkpadUpsertInput struct {
	IssueID string
	Heading string
	Body    string
}

type WorkpadUpsertResult struct {
	Comment         IssueComment
	Created         bool
	MatchedComments int
}

type IssueState struct {
	ID   string
	Name string
	Type string
}

type IssueStateTransitionInput struct {
	IssueID   string
	StateID   string
	StateName string
}

type IssueStateTransitionResult struct {
	IssueID    string
	Identifier string
	State      IssueState
}

type IssueURLAttachmentInput struct {
	IssueID string
	URL     string
	Title   string
}

type IssueAttachment struct {
	ID         string
	Title      string
	URL        string
	SourceType string
}

// CreateIssueComment creates a Linear issue comment through a typed API.
func (c *Client) CreateIssueComment(ctx context.Context, input IssueCommentCreateInput) (IssueComment, error) {
	issueID := strings.TrimSpace(input.IssueID)
	body := strings.TrimSpace(input.Body)
	if issueID == "" {
		return IssueComment{}, ErrMissingIssueID
	}
	if body == "" {
		return IssueComment{}, ErrMissingCommentBody
	}

	var response commentCreateResponse
	err := c.post(ctx, commentCreateMutation, map[string]any{
		"issueID": issueID,
		"body":    input.Body,
	}, &response)
	if err != nil {
		return IssueComment{}, err
	}
	return normalizeCommentPayload(response.Data, "commentCreate")
}

// CreateIssueCommentReply creates a Linear reply under a parent issue comment.
func (c *Client) CreateIssueCommentReply(ctx context.Context, input IssueCommentReplyCreateInput) (IssueComment, error) {
	issueID := strings.TrimSpace(input.IssueID)
	parentCommentID := strings.TrimSpace(input.ParentCommentID)
	body := strings.TrimSpace(input.Body)
	if issueID == "" {
		return IssueComment{}, ErrMissingIssueID
	}
	if parentCommentID == "" {
		return IssueComment{}, ErrMissingParentCommentID
	}
	if body == "" {
		return IssueComment{}, ErrMissingCommentBody
	}

	var response commentCreateResponse
	err := c.post(ctx, commentReplyCreateMutation, map[string]any{
		"issueID":         issueID,
		"parentCommentID": parentCommentID,
		"body":            input.Body,
	}, &response)
	if err != nil {
		return IssueComment{}, err
	}
	return normalizeCommentPayload(response.Data, "commentCreate")
}

// UpdateIssueComment updates an existing Linear comment through a typed API.
func (c *Client) UpdateIssueComment(ctx context.Context, input IssueCommentUpdateInput) (IssueComment, error) {
	commentID := strings.TrimSpace(input.CommentID)
	body := strings.TrimSpace(input.Body)
	if commentID == "" {
		return IssueComment{}, ErrMissingCommentID
	}
	if body == "" {
		return IssueComment{}, ErrMissingCommentBody
	}

	var response commentUpdateResponse
	err := c.post(ctx, commentUpdateMutation, map[string]any{
		"commentID": commentID,
		"body":      input.Body,
	}, &response)
	if err != nil {
		return IssueComment{}, err
	}
	return normalizeCommentPayload(response.Data, "commentUpdate")
}

// UpsertIssueWorkpad creates or updates the single active issue workpad comment for a heading.
func (c *Client) UpsertIssueWorkpad(ctx context.Context, input WorkpadUpsertInput) (WorkpadUpsertResult, error) {
	issueID := strings.TrimSpace(input.IssueID)
	heading := strings.TrimSpace(input.Heading)
	body := strings.TrimSpace(input.Body)
	if issueID == "" {
		return WorkpadUpsertResult{}, ErrMissingIssueID
	}
	if heading == "" {
		return WorkpadUpsertResult{}, ErrMissingWorkpadHeading
	}
	if body == "" {
		return WorkpadUpsertResult{}, ErrMissingCommentBody
	}

	comments, err := c.FetchIssueComments(ctx, issueID)
	if err != nil {
		return WorkpadUpsertResult{}, err
	}

	existing, matched := newestCommentWithHeading(comments, heading)
	if matched > 0 {
		comment, err := c.UpdateIssueComment(ctx, IssueCommentUpdateInput{
			CommentID: existing.ID,
			Body:      input.Body,
		})
		if err != nil {
			return WorkpadUpsertResult{}, err
		}
		return WorkpadUpsertResult{Comment: comment, MatchedComments: matched}, nil
	}

	comment, err := c.CreateIssueComment(ctx, IssueCommentCreateInput{
		IssueID: issueID,
		Body:    input.Body,
	})
	if err != nil {
		return WorkpadUpsertResult{}, err
	}
	return WorkpadUpsertResult{Comment: comment, Created: true}, nil
}

// FetchIssueComments reads a bounded Linear issue discussion with reply metadata.
func (c *Client) FetchIssueComments(ctx context.Context, issueID string) ([]IssueComment, error) {
	issueID = strings.TrimSpace(issueID)
	if issueID == "" {
		return nil, ErrMissingIssueID
	}
	return c.fetchIssueComments(ctx, issueID)
}

// TransitionIssueState moves an issue to a target Linear workflow state.
func (c *Client) TransitionIssueState(ctx context.Context, input IssueStateTransitionInput) (IssueStateTransitionResult, error) {
	issueID := strings.TrimSpace(input.IssueID)
	if issueID == "" {
		return IssueStateTransitionResult{}, ErrMissingIssueID
	}

	stateID := strings.TrimSpace(input.StateID)
	if stateID == "" {
		resolved, err := c.resolveIssueStateID(ctx, issueID, input.StateName)
		if err != nil {
			return IssueStateTransitionResult{}, err
		}
		stateID = resolved
	}

	var response issueUpdateResponse
	err := c.post(ctx, issueUpdateStateMutation, map[string]any{
		"issueID": issueID,
		"stateID": stateID,
	}, &response)
	if err != nil {
		return IssueStateTransitionResult{}, err
	}
	if response.Data == nil || response.Data.IssueUpdate == nil {
		return IssueStateTransitionResult{}, fmt.Errorf("%w: missing data.issueUpdate", ErrUnknownPayload)
	}
	payload := response.Data.IssueUpdate
	if !payload.Success {
		return IssueStateTransitionResult{}, ErrMutationUnsuccessful
	}
	if payload.Issue == nil || payload.Issue.ID == "" || payload.Issue.State.ID == "" {
		return IssueStateTransitionResult{}, fmt.Errorf("%w: missing issueUpdate.issue", ErrUnknownPayload)
	}
	return IssueStateTransitionResult{
		IssueID:    payload.Issue.ID,
		Identifier: payload.Issue.Identifier,
		State:      payload.Issue.State.issueState(),
	}, nil
}

// LinkIssueURL attaches an external URL reference to a Linear issue.
func (c *Client) LinkIssueURL(ctx context.Context, input IssueURLAttachmentInput) (IssueAttachment, error) {
	issueID := strings.TrimSpace(input.IssueID)
	linkURL := strings.TrimSpace(input.URL)
	if issueID == "" {
		return IssueAttachment{}, ErrMissingIssueID
	}
	if linkURL == "" {
		return IssueAttachment{}, ErrMissingAttachmentURL
	}
	if err := validateAttachmentURL(linkURL); err != nil {
		return IssueAttachment{}, err
	}

	var response attachmentLinkURLResponse
	err := c.post(ctx, attachmentLinkURLMutation, map[string]any{
		"issueID": issueID,
		"url":     linkURL,
		"title":   strings.TrimSpace(input.Title),
	}, &response)
	if err != nil {
		return IssueAttachment{}, err
	}
	if response.Data == nil || response.Data.AttachmentLinkURL == nil {
		return IssueAttachment{}, fmt.Errorf("%w: missing data.attachmentLinkURL", ErrUnknownPayload)
	}
	payload := response.Data.AttachmentLinkURL
	if !payload.Success {
		return IssueAttachment{}, ErrMutationUnsuccessful
	}
	if payload.Attachment.ID == "" || payload.Attachment.URL == "" {
		return IssueAttachment{}, fmt.Errorf("%w: missing attachment payload", ErrUnknownPayload)
	}
	return IssueAttachment(payload.Attachment), nil
}

func (c *Client) fetchIssueComments(ctx context.Context, issueID string) ([]IssueComment, error) {
	var comments []IssueComment
	var after *string
	for {
		var response issueCommentsResponse
		err := c.post(ctx, issueCommentsQuery, map[string]any{
			"issueID": issueID,
			"first":   c.pageSize,
			"after":   after,
		}, &response)
		if err != nil {
			return nil, err
		}
		if response.Data == nil || response.Data.Issue == nil {
			return nil, ErrIssueNotFound
		}
		page := response.Data.Issue.Comments
		normalized, err := normalizeComments(page.Nodes)
		if err != nil {
			return nil, err
		}
		comments = append(comments, normalized...)

		if page.PageInfo == nil {
			return nil, fmt.Errorf("%w: missing data.issue.comments.pageInfo", ErrUnknownPayload)
		}
		if !page.PageInfo.HasNextPage {
			return comments, nil
		}
		if page.PageInfo.EndCursor == "" {
			return nil, ErrMissingEndCursor
		}
		cursor := page.PageInfo.EndCursor
		after = &cursor
	}
}

func (c *Client) resolveIssueStateID(ctx context.Context, issueID string, stateName string) (string, error) {
	name := strings.TrimSpace(stateName)
	if name == "" {
		return "", ErrMissingState
	}

	var response issueTeamStatesResponse
	err := c.post(ctx, issueTeamStatesQuery, map[string]any{"issueID": issueID}, &response)
	if err != nil {
		return "", err
	}
	if response.Data == nil || response.Data.Issue == nil {
		return "", ErrIssueNotFound
	}
	for _, state := range response.Data.Issue.Team.States.Nodes {
		if strings.EqualFold(strings.TrimSpace(state.Name), name) {
			if state.ID == "" {
				return "", fmt.Errorf("%w: matching state has empty id", ErrUnknownPayload)
			}
			return state.ID, nil
		}
	}
	return "", fmt.Errorf("%w: %s", ErrStateNotFound, name)
}

func normalizeCommentPayload(data *commentMutationData, field string) (IssueComment, error) {
	if data == nil {
		return IssueComment{}, fmt.Errorf("%w: missing data.%s", ErrUnknownPayload, field)
	}
	var payload *commentPayload
	switch field {
	case "commentCreate":
		payload = data.CommentCreate
	case "commentUpdate":
		payload = data.CommentUpdate
	default:
		return IssueComment{}, fmt.Errorf("%w: unsupported comment payload field %q", ErrUnknownPayload, field)
	}
	if payload == nil {
		return IssueComment{}, fmt.Errorf("%w: missing data.%s", ErrUnknownPayload, field)
	}
	if !payload.Success {
		return IssueComment{}, ErrMutationUnsuccessful
	}
	return normalizeComment(payload.Comment)
}

func normalizeComments(nodes []commentNode) ([]IssueComment, error) {
	comments := make([]IssueComment, 0, len(nodes))
	for _, node := range nodes {
		threadComments, err := normalizeCommentThread(node, "", "", 0)
		if err != nil {
			return nil, err
		}
		comments = append(comments, threadComments...)
	}
	return comments, nil
}

func normalizeComment(node commentNode) (IssueComment, error) {
	return normalizeCommentInThread(node, "", "", 0)
}

func normalizeCommentThread(
	node commentNode,
	fallbackParentID string,
	threadRootID string,
	depth int,
) ([]IssueComment, error) {
	comment, err := normalizeCommentInThread(node, fallbackParentID, threadRootID, depth)
	if err != nil {
		return nil, err
	}
	comments := []IssueComment{comment}
	for _, child := range node.Children.Nodes {
		childComments, err := normalizeCommentThread(child, comment.ID, comment.ThreadRootID, comment.Depth+1)
		if err != nil {
			return nil, err
		}
		comments = append(comments, childComments...)
	}
	return comments, nil
}

func normalizeCommentInThread(
	node commentNode,
	fallbackParentID string,
	threadRootID string,
	depth int,
) (IssueComment, error) {
	if node.ID == "" {
		return IssueComment{}, fmt.Errorf("%w: comment requires id", ErrUnknownPayload)
	}
	createdAt, err := parseOptionalTime(node.CreatedAt, "comment.createdAt")
	if err != nil {
		return IssueComment{}, err
	}
	updatedAt, err := parseOptionalTime(node.UpdatedAt, "comment.updatedAt")
	if err != nil {
		return IssueComment{}, err
	}

	parentID := firstNonEmptyString(node.ParentID, node.Parent.ID, fallbackParentID)
	if parentID != "" && depth == 0 {
		depth = 1
	}
	if threadRootID == "" {
		if parentID != "" {
			threadRootID = parentID
		} else {
			threadRootID = node.ID
		}
	}
	return IssueComment{
		ID:           node.ID,
		Body:         node.Body,
		ParentID:     parentID,
		ThreadRootID: threadRootID,
		Depth:        depth,
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}, nil
}

func newestCommentWithHeading(comments []IssueComment, heading string) (IssueComment, int) {
	var selected IssueComment
	matched := 0
	for _, comment := range comments {
		if comment.ParentID != "" || comment.Depth > 0 {
			continue
		}
		if !commentHasHeading(comment.Body, heading) {
			continue
		}
		matched++
		if matched == 1 || !commentTimestamp(comment).Before(commentTimestamp(selected)) {
			selected = comment
		}
	}
	return selected, matched
}

func commentHasHeading(body string, heading string) bool {
	body = strings.TrimSpace(body)
	if body == "" {
		return false
	}
	firstLine, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(firstLine) == heading
}

func commentTimestamp(comment IssueComment) time.Time {
	if comment.UpdatedAt != nil {
		return *comment.UpdatedAt
	}
	if comment.CreatedAt != nil {
		return *comment.CreatedAt
	}
	return time.Time{}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validateAttachmentURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: %s", ErrInvalidAttachmentURL, value)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return nil
	default:
		return fmt.Errorf("%w: unsupported scheme %q", ErrInvalidAttachmentURL, parsed.Scheme)
	}
}

type commentCreateResponse struct {
	Data *commentMutationData `json:"data"`
}

type commentUpdateResponse struct {
	Data *commentMutationData `json:"data"`
}

type commentMutationData struct {
	CommentCreate *commentPayload `json:"commentCreate"`
	CommentUpdate *commentPayload `json:"commentUpdate"`
}

type commentPayload struct {
	Success bool        `json:"success"`
	Comment commentNode `json:"comment"`
}

type issueCommentsResponse struct {
	Data *struct {
		Issue *struct {
			ID       string            `json:"id"`
			Comments commentConnection `json:"comments"`
		} `json:"issue"`
	} `json:"data"`
}

type commentConnection struct {
	Nodes    []commentNode `json:"nodes"`
	PageInfo *pageInfo     `json:"pageInfo"`
}

type commentNode struct {
	ID        string            `json:"id"`
	Body      string            `json:"body"`
	ParentID  string            `json:"parentId"`
	Parent    commentParentNode `json:"parent"`
	Children  commentConnection `json:"children"`
	CreatedAt string            `json:"createdAt"`
	UpdatedAt string            `json:"updatedAt"`
}

type commentParentNode struct {
	ID string `json:"id"`
}

type issueTeamStatesResponse struct {
	Data *struct {
		Issue *struct {
			ID   string `json:"id"`
			Team struct {
				States workflowStateConnection `json:"states"`
			} `json:"team"`
		} `json:"issue"`
	} `json:"data"`
}

type workflowStateConnection struct {
	Nodes []workflowStateNode `json:"nodes"`
}

type workflowStateNode struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

func (s workflowStateNode) issueState() IssueState {
	return IssueState{ID: s.ID, Name: s.Name, Type: s.Type}
}

type issueUpdateResponse struct {
	Data *struct {
		IssueUpdate *issueUpdatePayload `json:"issueUpdate"`
	} `json:"data"`
}

type issueUpdatePayload struct {
	Success bool `json:"success"`
	Issue   *struct {
		ID         string            `json:"id"`
		Identifier string            `json:"identifier"`
		State      workflowStateNode `json:"state"`
	} `json:"issue"`
}

type attachmentLinkURLResponse struct {
	Data *struct {
		AttachmentLinkURL *attachmentPayload `json:"attachmentLinkURL"`
	} `json:"data"`
}

type attachmentPayload struct {
	Success    bool                  `json:"success"`
	Attachment attachmentPayloadNode `json:"attachment"`
}

type attachmentPayloadNode struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	URL        string `json:"url"`
	SourceType string `json:"sourceType"`
}

const commentCreateMutation = `
mutation CreateIssueComment($issueID: String!, $body: String!) {
  commentCreate(input: { issueId: $issueID, body: $body }) {
    success
    comment { id body parentId parent { id } createdAt updatedAt }
  }
}`

// Linear GraphQL schema introspection on 2026-05-14 confirmed
// CommentCreateInput.parentId plus Comment.parentId/parent/children support.
const commentReplyCreateMutation = `
mutation CreateIssueCommentReply($issueID: String!, $parentCommentID: String!, $body: String!) {
  commentCreate(input: { issueId: $issueID, parentId: $parentCommentID, body: $body }) {
    success
    comment { id body parentId parent { id } createdAt updatedAt }
  }
}`

const commentUpdateMutation = `
mutation UpdateIssueComment($commentID: String!, $body: String!) {
  commentUpdate(id: $commentID, input: { body: $body }) {
    success
    comment { id body parentId parent { id } createdAt updatedAt }
  }
}`

const issueCommentsQuery = `
query IssueComments($issueID: String!, $first: Int!, $after: String) {
  issue(id: $issueID) {
    id
    comments(first: $first, after: $after) {
      nodes {
        id
        body
        parentId
        parent { id }
        createdAt
        updatedAt
        children(first: 20) {
          nodes { id body parentId parent { id } createdAt updatedAt }
          pageInfo { hasNextPage endCursor }
        }
      }
      pageInfo { hasNextPage endCursor }
    }
  }
}`

const issueTeamStatesQuery = `
query IssueTeamStates($issueID: String!) {
  issue(id: $issueID) {
    id
    team {
      states {
        nodes { id name type }
      }
    }
  }
}`

const issueUpdateStateMutation = `
mutation TransitionIssueState($issueID: String!, $stateID: String!) {
  issueUpdate(id: $issueID, input: { stateId: $stateID }) {
    success
    issue {
      id
      identifier
      state { id name type }
    }
  }
}`

const attachmentLinkURLMutation = `
mutation LinkIssueURL($issueID: String!, $url: String!, $title: String) {
  attachmentLinkURL(issueId: $issueID, url: $url, title: $title) {
    success
    attachment { id title url sourceType }
  }
}`
