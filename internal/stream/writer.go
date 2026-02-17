// Package stream provides terminal streaming output functionality.
package stream

import (
	"io"
)

// Writer handles streaming output to the terminal.
// It adapts its behavior based on whether the output is a TTY or a pipe.
type Writer struct {
	out   io.Writer
	isTTY bool
}

// NewWriter creates a new stream writer.
// When isTTY is true, output may include formatting.
// When false (piped), output is raw text only.
func NewWriter(out io.Writer, isTTY bool) *Writer {
	return &Writer{
		out:   out,
		isTTY: isTTY,
	}
}

// Write writes a token to the output immediately.
func (w *Writer) Write(token string) error {
	_, err := io.WriteString(w.out, token)
	return err
}

// Flush ensures all output has been written.
// For TTY output, adds a newline if needed.
func (w *Writer) Flush() {
	if !w.isTTY {
		// For piped output, ensure there's a trailing newline
		_, _ = io.WriteString(w.out, "\n")
	}
}

// IsTTY returns whether the output is a terminal.
func (w *Writer) IsTTY() bool {
	return w.isTTY
}
