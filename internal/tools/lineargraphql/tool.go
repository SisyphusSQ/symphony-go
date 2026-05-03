package lineargraphql

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
	"unicode"
	"unicode/utf8"

	"github.com/SisyphusSQ/symphony-go/internal/config"
)

const (
	Name           = "linear_graphql"
	DefaultTimeout = 30 * time.Second
)

var (
	ErrMissingEndpoint        = errors.New("missing_linear_endpoint")
	ErrMissingAPIKey          = errors.New("missing_linear_api_key")
	ErrUnsupportedTrackerKind = errors.New("unsupported_tracker_kind")
)

type Config struct {
	Endpoint string
	APIKey   string
	Timeout  time.Duration
}

type Option func(*options)

func WithHTTPClient(client *http.Client) Option {
	return func(opts *options) {
		if client != nil {
			opts.httpClient = client
		}
	}
}

type options struct {
	httpClient *http.Client
}

type Tool struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

type Request struct {
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables,omitempty"`
}

type Output struct {
	Success  bool            `json:"success"`
	Response json.RawMessage `json:"response,omitempty"`
	Error    *ErrorPayload   `json:"error,omitempty"`
}

type ErrorPayload struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	StatusCode int    `json:"status_code,omitempty"`
}

type Result struct {
	Success bool
	Output  Output
}

type graphQLRequest struct {
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables,omitempty"`
}

type graphQLError struct {
	Message string `json:"message"`
}

type graphQLEnvelope struct {
	Errors []graphQLError `json:"errors"`
}

func New(cfg Config, opts ...Option) (*Tool, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = config.DefaultLinearEndpoint
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultTimeout
	}
	if cfg.Timeout < 0 {
		return nil, fmt.Errorf("timeout: must be positive")
	}
	toolOptions := options{
		httpClient: &http.Client{Timeout: cfg.Timeout},
	}
	for _, opt := range opts {
		opt(&toolOptions)
	}
	tool := &Tool{
		endpoint:   strings.TrimSpace(cfg.Endpoint),
		apiKey:     strings.TrimSpace(cfg.APIKey),
		httpClient: toolOptions.httpClient,
	}
	if tool.endpoint == "" {
		return nil, ErrMissingEndpoint
	}
	if tool.apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	return tool, nil
}

func NewFromTrackerConfig(cfg config.Tracker, opts ...Option) (*Tool, error) {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind != config.TrackerKindLinear {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedTrackerKind, cfg.Kind)
	}
	return New(Config{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
	}, opts...)
}

func Available(cfg config.Tracker) bool {
	kind := strings.ToLower(strings.TrimSpace(cfg.Kind))
	if kind != config.TrackerKindLinear {
		return false
	}
	return strings.TrimSpace(cfg.APIKey) != ""
}

func Spec() map[string]any {
	return map[string]any{
		"name":        Name,
		"description": "Execute one raw Linear GraphQL query or mutation using Symphony configured Linear tracker auth.",
		"inputSchema": map[string]any{
			"type":                 "object",
			"additionalProperties": false,
			"required":             []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "A single GraphQL query, mutation, or subscription document.",
				},
				"variables": map[string]any{
					"type":        "object",
					"description": "Optional GraphQL variables object.",
				},
			},
		},
	}
}

func (t *Tool) ExecuteJSON(ctx context.Context, raw json.RawMessage) Result {
	request, parseFailure := parseRequest(raw)
	if parseFailure != nil {
		return *parseFailure
	}
	if err := requireSingleOperation(request.Query); err != nil {
		return failure("invalid_input", err.Error(), 0, nil)
	}
	return t.execute(ctx, request)
}

func (t *Tool) execute(ctx context.Context, request Request) Result {
	if t == nil || t.httpClient == nil {
		return failure("tool_not_configured", "linear_graphql tool is not configured", 0, nil)
	}

	payload, err := json.Marshal(graphQLRequest{
		Query:     request.Query,
		Variables: request.Variables,
	})
	if err != nil {
		return failure("encode_request", err.Error(), 0, nil)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return failure("transport_error", err.Error(), 0, nil)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", t.apiKey)

	response, err := t.httpClient.Do(httpReq)
	if err != nil {
		return failure("transport_error", err.Error(), 0, nil)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return failure("transport_error", "read response: "+err.Error(), 0, nil)
	}
	body = bytes.TrimSpace(body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return failure("http_status", trimBody(body), response.StatusCode, validRaw(body))
	}
	if !json.Valid(body) {
		return failure("invalid_response_json", "Linear response is not valid JSON", 0, nil)
	}

	var envelope graphQLEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return failure("invalid_response_json", err.Error(), 0, nil)
	}
	if len(envelope.Errors) > 0 {
		return failure("graphql_errors", graphQLErrorMessages(envelope.Errors), 0, append(json.RawMessage(nil), body...))
	}

	return Result{
		Success: true,
		Output: Output{
			Success:  true,
			Response: append(json.RawMessage(nil), body...),
		},
	}
}

func (r Result) Text() string {
	data, err := json.Marshal(r.Output)
	if err != nil {
		return `{"success":false,"error":{"code":"encode_output","message":"failed to encode tool output"}}`
	}
	return string(data)
}

func parseRequest(raw json.RawMessage) (Request, *Result) {
	if len(bytes.TrimSpace(raw)) == 0 || !json.Valid(raw) {
		result := failure("invalid_json", "tool arguments must be valid JSON", 0, nil)
		return Request{}, &result
	}

	if firstNonSpace(raw) == '"' {
		var query string
		if err := json.Unmarshal(raw, &query); err != nil {
			result := failure("invalid_json", err.Error(), 0, nil)
			return Request{}, &result
		}
		return Request{Query: strings.TrimSpace(query)}, nil
	}
	if firstNonSpace(raw) != '{' {
		result := failure("invalid_input", "tool arguments must be an object or GraphQL query string", 0, nil)
		return Request{}, &result
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		result := failure("invalid_json", err.Error(), 0, nil)
		return Request{}, &result
	}
	for key := range fields {
		if key != "query" && key != "variables" {
			result := failure("invalid_input", "unknown argument field: "+key, 0, nil)
			return Request{}, &result
		}
	}

	var request Request
	queryRaw, ok := fields["query"]
	if !ok {
		result := failure("invalid_input", "query is required", 0, nil)
		return Request{}, &result
	}
	if err := json.Unmarshal(queryRaw, &request.Query); err != nil {
		result := failure("invalid_input", "query must be a string", 0, nil)
		return Request{}, &result
	}
	request.Query = strings.TrimSpace(request.Query)

	if variablesRaw, ok := fields["variables"]; ok && string(bytes.TrimSpace(variablesRaw)) != "null" {
		if firstNonSpace(variablesRaw) != '{' {
			result := failure("invalid_input", "variables must be an object", 0, nil)
			return Request{}, &result
		}
		if !json.Valid(variablesRaw) {
			result := failure("invalid_json", "variables must be valid JSON", 0, nil)
			return Request{}, &result
		}
		request.Variables = append(json.RawMessage(nil), variablesRaw...)
	}

	if request.Query == "" {
		result := failure("invalid_input", "query is required", 0, nil)
		return Request{}, &result
	}
	return request, nil
}

func requireSingleOperation(query string) error {
	tokens, err := lexGraphQL(query)
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return errors.New("query is required")
	}
	operations := 0
	for index := 0; index < len(tokens); {
		token := tokens[index]
		switch {
		case token.kind == tokenName && isOperationKeyword(token.value):
			operations++
			next, err := skipDefinition(tokens, index+1)
			if err != nil {
				return err
			}
			index = next
		case token.kind == tokenPunct && token.value == "{":
			operations++
			next, err := skipBalanced(tokens, index)
			if err != nil {
				return err
			}
			index = next
		case token.kind == tokenName && token.value == "fragment":
			next, err := skipDefinition(tokens, index+1)
			if err != nil {
				return err
			}
			index = next
		default:
			return fmt.Errorf("unsupported top-level GraphQL token %q", token.value)
		}
		if operations > 1 {
			return errors.New("query must contain exactly one GraphQL operation")
		}
	}
	if operations != 1 {
		return errors.New("query must contain exactly one GraphQL operation")
	}
	return nil
}

type tokenKind int

const (
	tokenName tokenKind = iota + 1
	tokenPunct
)

type token struct {
	kind  tokenKind
	value string
}

func lexGraphQL(input string) ([]token, error) {
	var tokens []token
	for index := 0; index < len(input); {
		r, width := rune(input[index]), 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(input[index:])
		}
		switch {
		case unicode.IsSpace(r) || r == ',':
			index += width
		case r == '#':
			for index < len(input) && input[index] != '\n' && input[index] != '\r' {
				index++
			}
		case r == '"':
			next, err := skipGraphQLString(input, index)
			if err != nil {
				return nil, err
			}
			index = next
		case isNameStart(r):
			start := index
			index += width
			for index < len(input) {
				next, nextWidth := rune(input[index]), 1
				if next >= utf8.RuneSelf {
					next, nextWidth = utf8.DecodeRuneInString(input[index:])
				}
				if !isNameContinue(next) {
					break
				}
				index += nextWidth
			}
			tokens = append(tokens, token{kind: tokenName, value: input[start:index]})
		default:
			tokens = append(tokens, token{kind: tokenPunct, value: string(r)})
			index += width
		}
	}
	return tokens, nil
}

func skipDefinition(tokens []token, index int) (int, error) {
	parenDepth := 0
	bracketDepth := 0
	for index < len(tokens) {
		if tokens[index].kind == tokenPunct {
			switch tokens[index].value {
			case "(":
				parenDepth++
			case ")":
				if parenDepth > 0 {
					parenDepth--
				}
			case "[":
				bracketDepth++
			case "]":
				if bracketDepth > 0 {
					bracketDepth--
				}
			case "{":
				if parenDepth == 0 && bracketDepth == 0 {
					return skipBalanced(tokens, index)
				}
			}
		}
		index++
	}
	return 0, errors.New("GraphQL definition is missing selection set")
}

func skipBalanced(tokens []token, index int) (int, error) {
	if index >= len(tokens) || tokens[index].kind != tokenPunct || tokens[index].value != "{" {
		return 0, errors.New("GraphQL definition is missing selection set")
	}
	depth := 0
	for ; index < len(tokens); index++ {
		if tokens[index].kind != tokenPunct {
			continue
		}
		switch tokens[index].value {
		case "{":
			depth++
		case "}":
			depth--
			if depth == 0 {
				return index + 1, nil
			}
			if depth < 0 {
				return 0, errors.New("GraphQL braces are unbalanced")
			}
		}
	}
	return 0, errors.New("GraphQL braces are unbalanced")
}

func skipGraphQLString(input string, index int) (int, error) {
	if strings.HasPrefix(input[index:], `"""`) {
		end := strings.Index(input[index+3:], `"""`)
		if end < 0 {
			return 0, errors.New("unterminated GraphQL block string")
		}
		return index + 3 + end + 3, nil
	}
	index++
	for index < len(input) {
		switch input[index] {
		case '\\':
			index += 2
		case '"':
			return index + 1, nil
		default:
			index++
		}
	}
	return 0, errors.New("unterminated GraphQL string")
}

func isOperationKeyword(value string) bool {
	return value == "query" || value == "mutation" || value == "subscription"
}

func isNameStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isNameContinue(r rune) bool {
	return isNameStart(r) || unicode.IsDigit(r)
}

func firstNonSpace(raw []byte) byte {
	for _, ch := range raw {
		if !unicode.IsSpace(rune(ch)) {
			return ch
		}
	}
	return 0
}

func failure(code string, message string, statusCode int, response json.RawMessage) Result {
	return Result{
		Success: false,
		Output: Output{
			Success:  false,
			Response: response,
			Error: &ErrorPayload{
				Code:       code,
				Message:    message,
				StatusCode: statusCode,
			},
		},
	}
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

func validRaw(body []byte) json.RawMessage {
	if json.Valid(body) {
		return append(json.RawMessage(nil), body...)
	}
	return nil
}
