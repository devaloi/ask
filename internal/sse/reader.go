// Package sse provides a shared SSE (Server-Sent Events) reader
// for parsing streaming responses from LLM providers.
package sse

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// Event represents a parsed SSE event.
type Event struct {
	Type string // The event type (from "event:" lines), may be empty
	Data string // The event data (from "data:" lines)
}

// Reader reads SSE events from an io.Reader.
type Reader struct {
	scanner *bufio.Scanner
	ctx     context.Context
}

// NewReader creates a new SSE reader.
func NewReader(ctx context.Context, r io.Reader) *Reader {
	return &Reader{
		scanner: bufio.NewScanner(r),
		ctx:     ctx,
	}
}

// Read reads SSE events and sends them to the provided channel.
// The channel is NOT closed by this function; the caller is responsible for closing it.
// Returns nil when the stream ends normally, or an error if something goes wrong.
func (r *Reader) Read(events chan<- Event) error {
	var currentEvent Event

	for r.scanner.Scan() {
		// Check for context cancellation
		select {
		case <-r.ctx.Done():
			return r.ctx.Err()
		default:
		}

		line := r.scanner.Text()

		// Empty line marks the end of an event
		if line == "" {
			// Only emit if we have data
			if currentEvent.Data != "" {
				select {
				case events <- currentEvent:
				case <-r.ctx.Done():
					return r.ctx.Err()
				}
			}
			currentEvent = Event{}
			continue
		}

		// Skip comments (lines starting with ':')
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Parse event type
		if strings.HasPrefix(line, "event:") {
			currentEvent.Type = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		// Parse data
		if strings.HasPrefix(line, "data:") {
			data := strings.TrimPrefix(line, "data:")
			// Handle optional space after "data:"
			data = strings.TrimPrefix(data, " ")
			// Append to existing data (SSE spec allows multiple data lines)
			if currentEvent.Data != "" {
				currentEvent.Data += "\n"
			}
			currentEvent.Data += data
			continue
		}
	}

	// Check for scanner errors
	if err := r.scanner.Err(); err != nil {
		if r.ctx.Err() != nil {
			return r.ctx.Err()
		}
		return fmt.Errorf("error reading SSE stream: %w", err)
	}

	// Emit any remaining event
	if currentEvent.Data != "" {
		select {
		case events <- currentEvent:
		case <-r.ctx.Done():
			return r.ctx.Err()
		}
	}

	return nil
}
