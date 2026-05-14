package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/config"
	"github.com/SisyphusSQ/symphony-go/internal/tracker"
)

const (
	DefaultEndpoint = config.DefaultLinearEndpoint
	DefaultPageSize = 50
	DefaultTimeout  = 30 * time.Second
)

var (
	ErrMissingEndpoint  = errors.New("missing_linear_endpoint")
	ErrUnsupportedKind  = errors.New("unsupported_tracker_kind")
	ErrMissingAPIKey    = errors.New("missing_tracker_api_key")
	ErrMissingProject   = errors.New("missing_tracker_project_slug")
	ErrMissingStates    = errors.New("missing_tracker_states")
	ErrLinearRequest    = errors.New("linear_api_request")
	ErrLinearStatus     = errors.New("linear_api_status")
	ErrGraphQLErrors    = errors.New("linear_graphql_errors")
	ErrUnknownPayload   = errors.New("linear_unknown_payload")
	ErrMissingEndCursor = errors.New("linear_missing_end_cursor")
)

// Config contains the Linear tracker-read settings needed by Client.
type Config struct {
	Endpoint       string
	APIKey         string
	ProjectSlug    string
	ActiveStates   []string
	TerminalStates []string
	PageSize       int
	Timeout        time.Duration
}

// Option customizes Client construction.
type Option func(*clientOptions)

// WithHTTPClient injects the HTTP client used by the adapter.
func WithHTTPClient(client *http.Client) Option {
	return func(opts *clientOptions) {
		if client != nil {
			opts.httpClient = client
		}
	}
}

// Client is a Linear-compatible read adapter.
type Client struct {
	endpoint       string
	apiKey         string
	projectSlug    string
	activeStates   []string
	terminalStates []string
	pageSize       int
	httpClient     *http.Client
}

var _ tracker.Client = (*Client)(nil)

// New creates a Linear read adapter from explicit adapter settings.
func New(cfg Config, opts ...Option) (*Client, error) {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	if cfg.PageSize == 0 {
		cfg.PageSize = DefaultPageSize
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("timeout: must be positive")
	}

	clientOptions := clientOptions{
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}
	for _, opt := range opts {
		opt(&clientOptions)
	}

	client := &Client{
		endpoint:       strings.TrimSpace(cfg.Endpoint),
		apiKey:         strings.TrimSpace(cfg.APIKey),
		projectSlug:    strings.TrimSpace(cfg.ProjectSlug),
		activeStates:   cleanStrings(cfg.ActiveStates),
		terminalStates: cleanStrings(cfg.TerminalStates),
		pageSize:       cfg.PageSize,
		httpClient:     clientOptions.httpClient,
	}
	if client.endpoint == "" {
		return nil, ErrMissingEndpoint
	}
	if client.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if client.projectSlug == "" {
		return nil, ErrMissingProject
	}
	if client.pageSize <= 0 {
		return nil, fmt.Errorf("page_size: must be positive")
	}
	return client, nil
}

// NewFromTrackerConfig creates a Linear adapter from the typed workflow config.
func NewFromTrackerConfig(cfg config.Tracker, opts ...Option) (*Client, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind != "" && kind != config.TrackerKindLinear {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedKind, cfg.Kind)
	}
	return New(Config{
		Endpoint:       cfg.Endpoint,
		APIKey:         cfg.APIKey,
		ProjectSlug:    cfg.ProjectSlug,
		ActiveStates:   cfg.ActiveStates,
		TerminalStates: cfg.TerminalStates,
	}, opts...)
}

// FetchCandidateIssues returns issues in the configured active states.
func (c *Client) FetchCandidateIssues(ctx context.Context) ([]tracker.Issue, error) {
	return c.FetchIssuesByStates(ctx, c.activeStates)
}

// FetchTerminalIssues returns issues in the configured terminal states.
func (c *Client) FetchTerminalIssues(ctx context.Context) ([]tracker.Issue, error) {
	return c.FetchIssuesByStates(ctx, c.terminalStates)
}

// FetchIssuesByStates returns issues in a configured Linear project and state set.
func (c *Client) FetchIssuesByStates(ctx context.Context, stateNames []string) ([]tracker.Issue, error) {
	states := cleanStrings(stateNames)
	if len(states) == 0 {
		return nil, ErrMissingStates
	}

	var issues []tracker.Issue
	var after *string
	for {
		var response issuesResponse
		err := c.post(ctx, issuesByStatesQuery, map[string]any{
			"projectSlug": c.projectSlug,
			"stateNames":  states,
			"after":       after,
			"first":       c.pageSize,
		}, &response)
		if err != nil {
			return nil, err
		}
		page, err := response.issuePage()
		if err != nil {
			return nil, err
		}
		normalized, err := normalizeIssues(page.Nodes)
		if err != nil {
			return nil, err
		}
		issues = append(issues, normalized...)

		if page.PageInfo == nil {
			return nil, fmt.Errorf("%w: missing data.issues.pageInfo", ErrUnknownPayload)
		}
		if !page.PageInfo.HasNextPage {
			return issues, nil
		}
		if page.PageInfo.EndCursor == "" {
			return nil, ErrMissingEndCursor
		}
		cursor := page.PageInfo.EndCursor
		after = &cursor
	}
}

// FetchIssueStatesByIDs refreshes current issue snapshots by Linear GraphQL IDs.
func (c *Client) FetchIssueStatesByIDs(ctx context.Context, issueIDs []string) ([]tracker.Issue, error) {
	ids := cleanStrings(issueIDs)
	if len(ids) == 0 {
		return nil, nil
	}

	var response issuesResponse
	if err := c.post(ctx, issueStatesByIDsQuery, map[string]any{
		"ids":   ids,
		"first": len(ids),
	}, &response); err != nil {
		return nil, err
	}
	page, err := response.issuePage()
	if err != nil {
		return nil, err
	}
	if page.PageInfo == nil {
		return nil, fmt.Errorf("%w: missing data.issues.pageInfo", ErrUnknownPayload)
	}
	return normalizeIssues(page.Nodes)
}

type clientOptions struct {
	httpClient *http.Client
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphQLResponse struct {
	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (c *Client) post(ctx context.Context, query string, variables map[string]any, out any) error {
	payload, err := json.Marshal(graphQLRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("%w: encode request: %v", ErrUnknownPayload, err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", ErrLinearRequest, err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", c.apiKey)

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrLinearRequest, err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("%w: read response: %v", ErrLinearRequest, err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: status=%d body=%s", ErrLinearStatus, response.StatusCode, trimBody(body))
	}

	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: decode response: %v", ErrUnknownPayload, err)
	}
	var envelope graphQLResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%w: decode graphql envelope: %v", ErrUnknownPayload, err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("%w: %s", ErrGraphQLErrors, graphQLErrorMessages(envelope.Errors))
	}
	return nil
}

func graphQLErrorMessages(errs []graphQLError) string {
	messages := make([]string, 0, len(errs))
	for _, err := range errs {
		if strings.TrimSpace(err.Message) != "" {
			messages = append(messages, strings.TrimSpace(err.Message))
		}
	}
	if len(messages) == 0 {
		return "unknown GraphQL error"
	}
	return strings.Join(messages, "; ")
}

func trimBody(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 512 {
		return text[:512]
	}
	return text
}

func cleanStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

const issuesByStatesQuery = `
query FetchIssuesByStates($projectSlug: String!, $stateNames: [String!]!, $after: String, $first: Int!) {
  issues(
    first: $first
    after: $after
    filter: {
      project: { slugId: { eq: $projectSlug } }
      state: { name: { in: $stateNames } }
    }
  ) {
    nodes {
      id
      identifier
      title
      description
      priority
      branchName
      url
      createdAt
      updatedAt
      state { name }
      labels(first: 50) { nodes { name } }
      inverseRelations(first: 50) {
        nodes {
          type
          issue {
            id
            identifier
            state { name }
          }
        }
      }
      comments(first: 20) {
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
    pageInfo { hasNextPage endCursor }
  }
}`

const issueStatesByIDsQuery = `
query RefreshIssueStates($ids: [ID!]!, $first: Int!) {
  issues(first: $first, filter: { id: { in: $ids } }) {
    nodes {
      id
      identifier
      title
      description
      priority
      branchName
      url
      createdAt
      updatedAt
      state { name }
      labels(first: 50) { nodes { name } }
      inverseRelations(first: 50) {
        nodes {
          type
          issue {
            id
            identifier
            state { name }
          }
        }
      }
      comments(first: 20) {
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
    pageInfo { hasNextPage endCursor }
  }
}`
