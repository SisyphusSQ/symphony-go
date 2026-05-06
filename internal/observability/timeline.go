package observability

import (
	"encoding/json"
	"strings"
	"unicode"
)

// IsTimelineCategory reports whether category is part of the stable timeline
// contract accepted by /api/v1/runs/{run_id}/events.
func IsTimelineCategory(category string) bool {
	category = strings.ToLower(strings.TrimSpace(category))
	for _, allowed := range TimelineCategories {
		if category == allowed {
			return true
		}
	}
	return false
}

// ProjectTimelineEvent maps one durable raw event into the redacted operator
// timeline shape used by Web GUI and TUI clients.
func ProjectTimelineEvent(
	sequence int,
	raw RawTimelineEvent,
	redactor TimelineRedactor,
) TimelineEventRow {
	payload := decodePayload(raw.PayloadJSON)
	category := timelineCategory(raw.Type, payload)
	severity := timelineSeverity(raw.Type, payload)
	title := timelineTitle(category, raw.Type)
	summary := timelineSummary(category, raw.Type, payload)
	threadID := firstTimelineNonEmpty(raw.ThreadID, payloadIdentifier(payload, "thread"), payloadString(payload, "threadId"))
	turnID := firstTimelineNonEmpty(raw.TurnID, payloadIdentifier(payload, "turn"), payloadString(payload, "turnId"))

	return TimelineEventRow{
		Sequence:        sequence,
		ID:              redactTimelineString(redactor, raw.ID),
		At:              raw.At,
		Category:        category,
		Severity:        severity,
		Title:           redactTimelineString(redactor, title),
		Summary:         redactTimelineString(redactor, summary),
		IssueID:         redactTimelineString(redactor, raw.IssueID),
		IssueIdentifier: redactTimelineString(redactor, raw.IssueIdentifier),
		RunID:           redactTimelineString(redactor, raw.RunID),
		SessionID:       redactTimelineString(redactor, raw.SessionID),
		ThreadID:        redactTimelineString(redactor, threadID),
		TurnID:          redactTimelineString(redactor, turnID),
		Payload:         redactTimelineJSON(redactor, raw.PayloadJSON),
	}
}

func decodePayload(payloadText string) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(payloadText)), &payload); err != nil {
		return map[string]any{}
	}
	return payload
}

func timelineCategory(eventType string, payload map[string]any) string {
	combined := timelineSearchText(eventType, payload)
	switch {
	case containsAny(combined, "guardrail", "approval", "sandbox"):
		return TimelineCategoryGuardrail
	case containsAny(combined, "tool_call", "item/tool/call", "mcp", "tool="):
		return TimelineCategoryTool
	case containsAny(combined, "hooks.", "command", "stdout", "stderr", "exit_code"):
		return TimelineCategoryCommand
	case containsAny(combined, "apply_patch", "patch", "diff"):
		return TimelineCategoryDiff
	case containsAny(combined, "rate_limits", "rate limit", "token_usage", "token usage", "timeout", "stall"):
		return TimelineCategoryResource
	case containsAny(combined, "failed", "failure", "error", "malformed", "unsupported", "non-retryable"):
		return TimelineCategoryError
	case containsAny(combined, "message", "reasoning", "summary"):
		return TimelineCategoryMessage
	case containsAny(
		combined,
		"started",
		"completed",
		"stopped",
		"interrupted",
		"session",
		"thread",
		"turn",
		"run",
		"retry",
		"process",
	):
		return TimelineCategoryLifecycle
	default:
		return TimelineCategoryLifecycle
	}
}

func timelineSeverity(eventType string, payload map[string]any) string {
	combined := timelineSearchText(eventType, payload)
	switch {
	case containsAny(combined, "failed", "failure", "error", "malformed", "unsupported", "guardrail"):
		return string(LevelError)
	case containsAny(combined, "timeout", "stall", "retry", "backoff", "stopped", "interrupted"):
		return string(LevelWarn)
	case containsAny(combined, "debug"):
		return string(LevelDebug)
	default:
		return string(LevelInfo)
	}
}

func timelineTitle(category string, eventType string) string {
	switch category {
	case TimelineCategoryTool:
		return "Tool call"
	case TimelineCategoryCommand:
		return "Command"
	case TimelineCategoryDiff:
		return "Diff"
	case TimelineCategoryResource:
		return "Resource update"
	case TimelineCategoryGuardrail:
		return "Guardrail"
	case TimelineCategoryError:
		return "Error"
	case TimelineCategoryMessage:
		return "Message"
	default:
		return humanizeEventType(eventType)
	}
}

func timelineSummary(category string, eventType string, payload map[string]any) string {
	switch category {
	case TimelineCategoryTool:
		tool := firstTimelineNonEmpty(
			payloadString(payload, "tool"),
			payloadString(nestedPayload(payload), "tool"),
		)
		success := firstTimelineNonEmpty(
			payloadString(payload, "success"),
			payloadString(nestedPayload(payload), "success"),
		)
		if tool != "" && success != "" {
			return "tool=" + tool + " success=" + success
		}
		if tool != "" {
			return "tool=" + tool
		}
	case TimelineCategoryCommand:
		fields := payloadMap(payload, "fields")
		if command := firstTimelineNonEmpty(payloadString(fields, "command"), payloadString(payload, "command")); command != "" {
			return "command=" + command
		}
		if stderr := payloadString(fields, "stderr"); stderr != "" {
			return stderr
		}
		if stdout := payloadString(fields, "stdout"); stdout != "" {
			return stdout
		}
	case TimelineCategoryError, TimelineCategoryGuardrail:
		if errText := firstTimelineNonEmpty(
			payloadString(payload, "error"),
			payloadString(payloadMap(payload, "error"), "message"),
			payloadString(payload, "message"),
		); errText != "" {
			return errText
		}
	case TimelineCategoryResource:
		if method := payloadString(payload, "method"); method != "" {
			return method
		}
		if kind := payloadString(payload, "kind"); kind != "" {
			return kind
		}
	}

	return firstTimelineNonEmpty(
		payloadString(payload, "message"),
		payloadString(payload, "summary"),
		payloadString(payload, "method"),
		payloadString(payload, "kind"),
		humanizeEventType(eventType),
	)
}

func timelineSearchText(eventType string, payload map[string]any) string {
	parts := []string{
		eventType,
		payloadString(payload, "event_type"),
		payloadString(payload, "kind"),
		payloadString(payload, "method"),
		payloadString(payload, "message"),
		payloadString(payload, "error"),
		payloadString(nestedPayload(payload), "tool"),
	}
	if fields := payloadMap(payload, "fields"); len(fields) > 0 {
		for key := range fields {
			parts = append(parts, key)
		}
	}
	return strings.ToLower(strings.Join(parts, " "))
}

func payloadIdentifier(payload map[string]any, key string) string {
	value, ok := payload[key]
	if !ok {
		if nested := nestedPayload(payload); len(nested) > 0 {
			return payloadIdentifier(nested, key)
		}
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		return payloadString(typed, "id")
	default:
		return ""
	}
}

func payloadString(payload map[string]any, key string) string {
	if len(payload) == 0 {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case bool:
		if typed {
			return "true"
		}
		return "false"
	case float64:
		raw := jsonNumberString(typed)
		if strings.Contains(raw, ".") {
			raw = strings.TrimRight(strings.TrimRight(raw, "0"), ".")
		}
		return raw
	default:
		return ""
	}
}

func jsonNumberString(value float64) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(raw)
}

func payloadMap(payload map[string]any, key string) map[string]any {
	value, ok := payload[key]
	if !ok {
		return nil
	}
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return typed
}

func nestedPayload(payload map[string]any) map[string]any {
	return payloadMap(payload, "payload")
}

func redactTimelineString(redactor TimelineRedactor, value string) string {
	value = strings.TrimSpace(value)
	if redactor == nil {
		return value
	}
	return redactor.String(value)
}

func redactTimelineJSON(redactor TimelineRedactor, payloadText string) json.RawMessage {
	payloadText = strings.TrimSpace(payloadText)
	if payloadText == "" {
		payloadText = "{}"
	}
	if redactor != nil {
		payloadText = redactor.JSON(payloadText)
	}
	if !json.Valid([]byte(payloadText)) {
		encoded, _ := json.Marshal(payloadText)
		payloadText = string(encoded)
	}
	return json.RawMessage(payloadText)
}

func humanizeEventType(eventType string) string {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return "Event"
	}
	replacer := strings.NewReplacer(".", " ", "_", " ", "/", " ", "-", " ")
	words := strings.Fields(replacer.Replace(eventType))
	for i, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) == 0 {
			continue
		}
		runes[0] = unicode.ToUpper(runes[0])
		words[i] = string(runes)
	}
	return strings.Join(words, " ")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func firstTimelineNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
