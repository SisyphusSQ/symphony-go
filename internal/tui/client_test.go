package tui

import (
	"net/http"
	"testing"
)

func TestClientResolveDoesNotLeakEndpointQuery(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:4002/base?debug=true", http.DefaultClient)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if got, want := client.resolve("/api/v1/state"), "http://127.0.0.1:4002/base/api/v1/state"; got != want {
		t.Fatalf("resolve(state) = %q, want %q", got, want)
	}
	if got, want := client.resolve("/api/v1/runs/run-1/events?limit=200"), "http://127.0.0.1:4002/base/api/v1/runs/run-1/events?limit=200"; got != want {
		t.Fatalf("resolve(events) = %q, want %q", got, want)
	}
}
