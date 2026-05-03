package observability

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Logger consumes structured operator-visible events.
type Logger interface {
	Log(context.Context, Event) error
}

// LoggerFunc adapts a function to Logger.
type LoggerFunc func(context.Context, Event) error

// Log implements Logger.
func (fn LoggerFunc) Log(ctx context.Context, event Event) error {
	return fn(ctx, event)
}

// DiscardLogger returns a logger that accepts events without side effects.
func DiscardLogger() Logger {
	return LoggerFunc(func(context.Context, Event) error {
		return nil
	})
}

// JSONLogger writes one normalized event per line.
type JSONLogger struct {
	mu  sync.Mutex
	out io.Writer
	now func() time.Time
}

// NewJSONLogger creates a line-oriented JSON logger.
func NewJSONLogger(out io.Writer) *JSONLogger {
	return &JSONLogger{
		out: out,
		now: time.Now,
	}
}

// Log implements Logger.
func (logger *JSONLogger) Log(_ context.Context, event Event) error {
	if logger == nil || logger.out == nil {
		return nil
	}

	now := time.Now
	if logger.now != nil {
		now = logger.now
	}
	event = event.normalize(now())

	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')

	logger.mu.Lock()
	defer logger.mu.Unlock()
	_, err = logger.out.Write(payload)
	return err
}

// Recorder is a deterministic in-memory logger for tests and fakes.
type Recorder struct {
	mu     sync.Mutex
	now    func() time.Time
	err    error
	events []Event
}

// RecorderOption configures a Recorder.
type RecorderOption func(*Recorder)

// WithRecorderClock sets the timestamp source for events without explicit time.
func WithRecorderClock(now func() time.Time) RecorderOption {
	return func(recorder *Recorder) {
		recorder.now = now
	}
}

// WithRecorderError makes Log return err after recording the event.
func WithRecorderError(err error) RecorderOption {
	return func(recorder *Recorder) {
		recorder.err = err
	}
}

// NewRecorder creates a deterministic in-memory event recorder.
func NewRecorder(opts ...RecorderOption) *Recorder {
	recorder := &Recorder{now: time.Now}
	for _, opt := range opts {
		if opt != nil {
			opt(recorder)
		}
	}
	return recorder
}

// Log implements Logger.
func (recorder *Recorder) Log(_ context.Context, event Event) error {
	if recorder == nil {
		return nil
	}

	now := time.Now
	if recorder.now != nil {
		now = recorder.now
	}
	event = event.normalize(now())

	recorder.mu.Lock()
	recorder.events = append(recorder.events, event)
	err := recorder.err
	recorder.mu.Unlock()
	return err
}

// Events returns a copy of recorded events.
func (recorder *Recorder) Events() []Event {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	return append([]Event(nil), recorder.events...)
}

// EventsByType returns recorded events with the requested type.
func (recorder *Recorder) EventsByType(eventType EventType) []Event {
	if recorder == nil {
		return nil
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()

	events := make([]Event, 0)
	for _, event := range recorder.events {
		if event.Type == eventType {
			events = append(events, event)
		}
	}
	return events
}
