// Package tui renders the read-only operator terminal views.
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/SisyphusSQ/symphony-go/internal/observability"
)

const defaultTimelineLimit = 200

// Client reads the stable operator /api/v1 projection used by the TUI.
type Client struct {
	base       *url.URL
	httpClient *http.Client
}

// StateResponse is the /api/v1/state payload consumed by the status view.
type StateResponse struct {
	GeneratedAt             time.Time                      `json:"generated_at"`
	Lifecycle               LifecycleResponse              `json:"lifecycle"`
	Ready                   ReadyResponse                  `json:"ready"`
	Counts                  map[string]int                 `json:"counts"`
	Running                 []observability.RunRow         `json:"running"`
	Retrying                []observability.RunRow         `json:"retrying"`
	LatestCompletedOrFailed []observability.RunRow         `json:"latest_completed_or_failed"`
	Tokens                  observability.TokenTotals      `json:"tokens"`
	Runtime                 observability.RuntimeTotals    `json:"runtime"`
	RateLimit               observability.RateLimitSummary `json:"rate_limit"`
	StateStore              StateStoreResponse             `json:"state_store"`
}

// LifecycleResponse is the lifecycle section of /api/v1/state.
type LifecycleResponse struct {
	State string `json:"state"`
}

// ReadyResponse is the readiness section of /api/v1/state.
type ReadyResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

// StateStoreResponse is the state-store section of /api/v1/state.
type StateStoreResponse struct {
	Configured bool `json:"configured"`
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// NewClient creates a TUI API client for endpoint.
func NewClient(endpoint string, httpClient *http.Client) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("operator endpoint is required")
	}
	base, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil {
		return nil, fmt.Errorf("invalid operator endpoint: %w", err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("invalid operator endpoint: absolute http URL is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{base: base, httpClient: httpClient}, nil
}

// State fetches the dashboard summary.
func (c *Client) State(ctx context.Context) (StateResponse, error) {
	var state StateResponse
	if err := c.getJSON(ctx, "/api/v1/state", &state); err != nil {
		return StateResponse{}, err
	}
	return state, nil
}

// LatestRunForIssue fetches the latest known run for an issue identifier.
func (c *Client) LatestRunForIssue(ctx context.Context, identifier string) (observability.RunDetail, error) {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return observability.RunDetail{}, fmt.Errorf("issue identifier is required")
	}
	var detail observability.RunDetail
	apiPath := "/api/v1/issues/" + url.PathEscape(identifier) + "/latest"
	if err := c.getJSON(ctx, apiPath, &detail); err != nil {
		return observability.RunDetail{}, err
	}
	return detail, nil
}

// RunDetail fetches one run detail by run id.
func (c *Client) RunDetail(ctx context.Context, runID string) (observability.RunDetail, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return observability.RunDetail{}, fmt.Errorf("run id is required")
	}
	var detail observability.RunDetail
	apiPath := "/api/v1/runs/" + url.PathEscape(runID)
	if err := c.getJSON(ctx, apiPath, &detail); err != nil {
		return observability.RunDetail{}, err
	}
	return detail, nil
}

// RunEvents fetches one run's humanized timeline.
func (c *Client) RunEvents(ctx context.Context, runID string) (observability.TimelinePage, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return observability.TimelinePage{}, fmt.Errorf("run id is required")
	}
	values := url.Values{}
	values.Set("limit", fmt.Sprintf("%d", defaultTimelineLimit))
	apiPath := "/api/v1/runs/" + url.PathEscape(runID) + "/events?" + values.Encode()
	var page observability.TimelinePage
	if err := c.getJSON(ctx, apiPath, &page); err != nil {
		return observability.TimelinePage{}, err
	}
	return page, nil
}

func (c *Client) getJSON(ctx context.Context, apiPath string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.resolve(apiPath), nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		apiErr := decodeAPIError(data)
		if apiErr != "" {
			return fmt.Errorf("operator API request failed: %s: %s", resp.Status, apiErr)
		}
		return fmt.Errorf("operator API request failed: %s", resp.Status)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode operator API response: %w", err)
	}
	return nil
}

func (c *Client) resolve(apiPath string) string {
	base := *c.base
	apiPathOnly, rawQuery, _ := strings.Cut(apiPath, "?")
	base.Path = path.Join(base.Path, apiPathOnly)
	if strings.HasSuffix(apiPathOnly, "/") && !strings.HasSuffix(base.Path, "/") {
		base.Path += "/"
	}
	base.RawQuery = ""
	if rawQuery != "" {
		base.RawQuery = rawQuery
	}
	return base.String()
}

func decodeAPIError(data []byte) string {
	var envelope errorEnvelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return strings.TrimSpace(string(data))
	}
	if envelope.Error.Message != "" && envelope.Error.Code != "" {
		return envelope.Error.Code + ": " + envelope.Error.Message
	}
	if envelope.Error.Message != "" {
		return envelope.Error.Message
	}
	return strings.TrimSpace(string(data))
}
