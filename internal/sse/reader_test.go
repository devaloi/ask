package sse

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestReader_Read(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []Event
	}{
		{
			name:  "single data event",
			input: "data: hello\n\n",
			want: []Event{
				{Data: "hello"},
			},
		},
		{
			name:  "multiple events",
			input: "data: first\n\ndata: second\n\n",
			want: []Event{
				{Data: "first"},
				{Data: "second"},
			},
		},
		{
			name:  "event with type",
			input: "event: message\ndata: hello\n\n",
			want: []Event{
				{Type: "message", Data: "hello"},
			},
		},
		{
			name:  "data without space after colon",
			input: "data:nospace\n\n",
			want: []Event{
				{Data: "nospace"},
			},
		},
		{
			name:  "multiple data lines merged",
			input: "data: line1\ndata: line2\n\n",
			want: []Event{
				{Data: "line1\nline2"},
			},
		},
		{
			name:  "comment lines ignored",
			input: ": this is a comment\ndata: hello\n\n",
			want: []Event{
				{Data: "hello"},
			},
		},
		{
			name:  "empty data ignored",
			input: "data: \n\n",
			want:  []Event{},
		},
		{
			name:  "JSON data",
			input: "data: {\"key\": \"value\"}\n\n",
			want: []Event{
				{Data: `{"key": "value"}`},
			},
		},
		{
			name:  "mixed events",
			input: "event: start\ndata: begin\n\nevent: delta\ndata: content\n\nevent: stop\ndata: end\n\n",
			want: []Event{
				{Type: "start", Data: "begin"},
				{Type: "delta", Data: "content"},
				{Type: "stop", Data: "end"},
			},
		},
		{
			name:  "empty input",
			input: "",
			want:  []Event{},
		},
		{
			name:  "trailing event without newline",
			input: "data: last",
			want: []Event{
				{Data: "last"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reader := NewReader(ctx, strings.NewReader(tt.input))

			events := make(chan Event, 10)
			done := make(chan error, 1)

			go func() {
				done <- reader.Read(events)
				close(events)
			}()

			var got []Event
			for e := range events {
				got = append(got, e)
			}

			if err := <-done; err != nil {
				t.Fatalf("Read() error = %v", err)
			}

			if len(got) != len(tt.want) {
				t.Fatalf("Read() got %d events, want %d", len(got), len(tt.want))
			}

			for i := range tt.want {
				if got[i].Type != tt.want[i].Type {
					t.Errorf("Event[%d].Type = %q, want %q", i, got[i].Type, tt.want[i].Type)
				}
				if got[i].Data != tt.want[i].Data {
					t.Errorf("Event[%d].Data = %q, want %q", i, got[i].Data, tt.want[i].Data)
				}
			}
		})
	}
}

func TestReader_Read_ContextCancellation(t *testing.T) {
	// Create a slow reader that never ends
	slowReader := &slowStringReader{
		data:  "data: token\n\n",
		delay: 100 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	reader := NewReader(ctx, slowReader)

	events := make(chan Event, 10)
	done := make(chan error, 1)

	go func() {
		done <- reader.Read(events)
	}()

	// Cancel the context after receiving one event
	select {
	case <-events:
		cancel()
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first event")
	}

	// Should receive context cancelled error
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("Read() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for Read to return")
	}
}

// slowStringReader repeats its data slowly
type slowStringReader struct {
	data  string
	delay time.Duration
	pos   int
}

func (r *slowStringReader) Read(p []byte) (n int, err error) {
	time.Sleep(r.delay)
	if r.pos >= len(r.data) {
		r.pos = 0 // Loop forever
	}
	n = copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func TestReader_Read_EventTypeVariations(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Event
	}{
		{
			name:  "event with extra whitespace",
			input: "event:   spaced   \ndata: test\n\n",
			want:  Event{Type: "spaced", Data: "test"},
		},
		{
			name:  "data line with JSON object",
			input: `data: {"type":"content_block_delta","delta":{"text":"hello"}}` + "\n\n",
			want:  Event{Data: `{"type":"content_block_delta","delta":{"text":"hello"}}`},
		},
		{
			name:  "OpenAI style DONE",
			input: "data: [DONE]\n\n",
			want:  Event{Data: "[DONE]"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			reader := NewReader(ctx, strings.NewReader(tt.input))

			events := make(chan Event, 1)
			done := make(chan error, 1)

			go func() {
				done <- reader.Read(events)
				close(events)
			}()

			event := <-events
			if err := <-done; err != nil {
				t.Fatalf("Read() error = %v", err)
			}

			if event.Type != tt.want.Type {
				t.Errorf("Event.Type = %q, want %q", event.Type, tt.want.Type)
			}
			if event.Data != tt.want.Data {
				t.Errorf("Event.Data = %q, want %q", event.Data, tt.want.Data)
			}
		})
	}
}
