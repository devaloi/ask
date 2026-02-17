package stream

import (
	"bytes"
	"testing"
)

func TestNewWriter(t *testing.T) {
	var buf bytes.Buffer

	t.Run("TTY mode", func(t *testing.T) {
		w := NewWriter(&buf, true)
		if !w.IsTTY() {
			t.Error("expected IsTTY() to return true")
		}
	})

	t.Run("pipe mode", func(t *testing.T) {
		w := NewWriter(&buf, false)
		if w.IsTTY() {
			t.Error("expected IsTTY() to return false")
		}
	})
}

func TestWriter_Write(t *testing.T) {
	tests := []struct {
		name   string
		tokens []string
		want   string
	}{
		{
			name:   "single token",
			tokens: []string{"hello"},
			want:   "hello",
		},
		{
			name:   "multiple tokens",
			tokens: []string{"hello", " ", "world"},
			want:   "hello world",
		},
		{
			name:   "empty tokens",
			tokens: []string{"", "hello", ""},
			want:   "hello",
		},
		{
			name:   "unicode tokens",
			tokens: []string{"こんにちは", " ", "🌍"},
			want:   "こんにちは 🌍",
		},
		{
			name:   "tokens with newlines",
			tokens: []string{"line1\n", "line2\n", "line3"},
			want:   "line1\nline2\nline3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			w := NewWriter(&buf, true)

			for _, token := range tt.tokens {
				if err := w.Write(token); err != nil {
					t.Fatalf("Write() error = %v", err)
				}
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("Write() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestWriter_Flush_PipeMode(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, false) // pipe mode

	_ = w.Write("hello")
	w.Flush()

	want := "hello\n"
	if got := buf.String(); got != want {
		t.Errorf("Flush() in pipe mode = %q, want %q", got, want)
	}
}

func TestWriter_Flush_TTYMode(t *testing.T) {
	var buf bytes.Buffer
	w := NewWriter(&buf, true) // TTY mode

	_ = w.Write("hello")
	w.Flush()

	// TTY mode should not add newline on Flush
	want := "hello"
	if got := buf.String(); got != want {
		t.Errorf("Flush() in TTY mode = %q, want %q", got, want)
	}
}

func TestWriter_IsTTY(t *testing.T) {
	var buf bytes.Buffer

	t.Run("returns true for TTY", func(t *testing.T) {
		w := NewWriter(&buf, true)
		if !w.IsTTY() {
			t.Error("IsTTY() = false, want true")
		}
	})

	t.Run("returns false for pipe", func(t *testing.T) {
		w := NewWriter(&buf, false)
		if w.IsTTY() {
			t.Error("IsTTY() = true, want false")
		}
	})
}
