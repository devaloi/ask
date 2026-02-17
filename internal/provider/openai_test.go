package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestOpenAI_Name verifies the Name method returns the correct provider name.
func TestOpenAI_Name(t *testing.T) {
	provider := NewOpenAI("test-api-key")
	if got := provider.Name(); got != "openai" {
		t.Errorf("Name() = %q, want %q", got, "openai")
	}
}

// TestOpenAI_Models verifies the Models method returns the expected model list.
func TestOpenAI_Models(t *testing.T) {
	provider := NewOpenAI("test-api-key")
	models := provider.Models()

	expectedModels := []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
	}

	if len(models) != len(expectedModels) {
		t.Fatalf("Models() returned %d models, want %d", len(models), len(expectedModels))
	}

	for i, model := range models {
		if model != expectedModels[i] {
			t.Errorf("Models()[%d] = %q, want %q", i, model, expectedModels[i])
		}
	}
}

// TestOpenAI_Chat_SuccessfulStreaming tests a successful streaming response.
func TestOpenAI_Chat_SuccessfulStreaming(t *testing.T) {
	expectedTokens := []string{"Hello", " world", "!"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("Authorization header = %q, want %q", r.Header.Get("Authorization"), "Bearer test-api-key")
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type header = %q, want %q", r.Header.Get("Content-Type"), "application/json")
		}
		if r.Header.Get("Accept") != "text/event-stream" {
			t.Errorf("Accept header = %q, want %q", r.Header.Get("Accept"), "text/event-stream")
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		// Send SSE events for each token
		for _, token := range expectedTokens {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", token)
			flusher.Flush()
		}

		// Send the [DONE] sentinel
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:       "gpt-4o",
		Messages:    []Message{{Role: "user", Content: "Hello"}},
		Temperature: 0.7,
	}

	var wg sync.WaitGroup
	var receivedTokens []string

	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	if len(receivedTokens) != len(expectedTokens) {
		t.Fatalf("received %d tokens, want %d", len(receivedTokens), len(expectedTokens))
	}

	for i, token := range receivedTokens {
		if token != expectedTokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, expectedTokens[i])
		}
	}
}

// TestOpenAI_Chat_HTTPErrors tests various HTTP error responses.
func TestOpenAI_Chat_HTTPErrors(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		expectedErrMsg string
	}{
		{
			name:           "401 Unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   `{"error": {"message": "Invalid API key"}}`,
			expectedErrMsg: "invalid API key",
		},
		{
			name:           "429 Rate Limited",
			statusCode:     http.StatusTooManyRequests,
			responseBody:   `{"error": {"message": "Rate limit exceeded"}}`,
			expectedErrMsg: "rate limited",
		},
		{
			name:           "500 Server Error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   `{"error": {"message": "Internal server error"}}`,
			expectedErrMsg: "OpenAI service error",
		},
		{
			name:           "502 Bad Gateway",
			statusCode:     http.StatusBadGateway,
			responseBody:   `{"error": {"message": "Bad gateway"}}`,
			expectedErrMsg: "OpenAI service error",
		},
		{
			name:           "503 Service Unavailable",
			statusCode:     http.StatusServiceUnavailable,
			responseBody:   `{"error": {"message": "Service unavailable"}}`,
			expectedErrMsg: "OpenAI service error",
		},
		{
			name:           "400 Bad Request",
			statusCode:     http.StatusBadRequest,
			responseBody:   `{"error": {"message": "Invalid request"}}`,
			expectedErrMsg: "OpenAI API error (status 400)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
			stream := make(chan string, 10)

			req := &ChatRequest{
				Model:    "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hello"}},
			}

			err := provider.Chat(context.Background(), req, stream)
			if err == nil {
				t.Fatal("Chat() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.expectedErrMsg) {
				t.Errorf("Chat() error = %q, want to contain %q", err.Error(), tt.expectedErrMsg)
			}

			// Verify channel is closed after error
			select {
			case _, ok := <-stream:
				if ok {
					t.Error("stream channel should be closed after error")
				}
			default:
				t.Error("stream channel should be closed after error")
			}
		})
	}
}

// TestOpenAI_Chat_ContextCancellation tests that context cancellation stops the stream.
func TestOpenAI_Chat_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		// Send first token
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		// Simulate slow response - wait for context to be cancelled
		time.Sleep(500 * time.Millisecond)

		// This should not be received because context was cancelled
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	var receivedTokens []string

	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
			// Cancel context after receiving first token
			cancel()
		}
	}()

	err := provider.Chat(ctx, req, stream)
	wg.Wait()

	if err == nil {
		t.Error("Chat() expected context cancellation error, got nil")
	}

	if err != nil && err != context.Canceled {
		t.Logf("Chat() returned error: %v (this is expected for context cancellation)", err)
	}

	// Verify we got at least one token before cancellation
	if len(receivedTokens) == 0 {
		t.Error("expected to receive at least one token before cancellation")
	}
}

// TestOpenAI_Chat_ContextCancelledBeforeRequest tests cancellation before the request starts.
func TestOpenAI_Chat_ContextCancelledBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be called when context is already cancelled")
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before calling Chat

	err := provider.Chat(ctx, req, stream)
	if err == nil {
		t.Error("Chat() expected context cancellation error, got nil")
	}

	// Verify channel is closed
	select {
	case _, ok := <-stream:
		if ok {
			t.Error("stream channel should be closed after error")
		}
	default:
		t.Error("stream channel should be closed after error")
	}
}

// TestOpenAI_Chat_MalformedSSEResponse tests graceful handling of malformed SSE data.
func TestOpenAI_Chat_MalformedSSEResponse(t *testing.T) {
	tests := []struct {
		name           string
		sseData        string
		expectedTokens []string
	}{
		{
			name: "valid tokens with malformed JSON in between",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {invalid json here}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "empty data lines",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: 

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "SSE comments (lines starting with colon)",
			sseData: `: this is a comment
data: {"choices":[{"delta":{"content":"Hello"}}]}

: another comment
data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "empty choices array",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "empty content string",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":""}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "missing delta field",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
		{
			name: "lines without data prefix",
			sseData: `data: {"choices":[{"delta":{"content":"Hello"}}]}

event: message
id: 1

data: {"choices":[{"delta":{"content":" world"}}]}

data: [DONE]

`,
			expectedTokens: []string{"Hello", " world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.sseData))
			}))
			defer server.Close()

			provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
			stream := make(chan string, 10)

			req := &ChatRequest{
				Model:    "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hello"}},
			}

			var wg sync.WaitGroup
			var receivedTokens []string

			wg.Add(1)
			go func() {
				defer wg.Done()
				for token := range stream {
					receivedTokens = append(receivedTokens, token)
				}
			}()

			err := provider.Chat(context.Background(), req, stream)
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}

			wg.Wait()

			if len(receivedTokens) != len(tt.expectedTokens) {
				t.Fatalf("received %d tokens, want %d: got %v", len(receivedTokens), len(tt.expectedTokens), receivedTokens)
			}

			for i, token := range receivedTokens {
				if token != tt.expectedTokens[i] {
					t.Errorf("token[%d] = %q, want %q", i, token, tt.expectedTokens[i])
				}
			}
		})
	}
}

// TestOpenAI_Chat_DoneSentinelTerminatesStream tests that [DONE] properly terminates the stream.
func TestOpenAI_Chat_DoneSentinelTerminatesStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		// Send tokens
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" world\"}}]}\n\n")
		flusher.Flush()

		// Send [DONE]
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()

		// These should NOT be received after [DONE]
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" extra\"}}]}\n\n")
		flusher.Flush()

		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\" tokens\"}}]}\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	var wg sync.WaitGroup
	var receivedTokens []string

	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	expectedTokens := []string{"Hello", " world"}
	if len(receivedTokens) != len(expectedTokens) {
		t.Fatalf("received %d tokens, want %d: got %v", len(receivedTokens), len(expectedTokens), receivedTokens)
	}

	for i, token := range receivedTokens {
		if token != expectedTokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, expectedTokens[i])
		}
	}
}

// TestOpenAI_Chat_StreamChannelClosed verifies that the stream channel is closed after completion.
func TestOpenAI_Chat_StreamChannelClosed(t *testing.T) {
	tests := []struct {
		name      string
		handler   http.HandlerFunc
		expectErr bool
	}{
		{
			name: "successful completion",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n\ndata: [DONE]\n\n"))
			},
			expectErr: false,
		},
		{
			name: "error response",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error": "server error"}`))
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
			stream := make(chan string, 10)

			req := &ChatRequest{
				Model:    "gpt-4o",
				Messages: []Message{{Role: "user", Content: "Hello"}},
			}

			err := provider.Chat(context.Background(), req, stream)
			if tt.expectErr && err == nil {
				t.Error("Chat() expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Chat() unexpected error: %v", err)
			}

			// Verify channel is closed by trying to receive
			select {
			case _, ok := <-stream:
				if ok {
					// Drain the channel
					for range stream {
					}
				}
			case <-time.After(time.Second):
				t.Error("stream channel should be closed, but receive timed out")
			}
		})
	}
}

// TestOpenAI_Chat_TokenOrder verifies tokens arrive in the correct order.
func TestOpenAI_Chat_TokenOrder(t *testing.T) {
	tokens := []string{"The", " quick", " brown", " fox", " jumps", " over", " the", " lazy", " dog", "."}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		for _, token := range tokens {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", token)
			flusher.Flush()
		}

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, len(tokens)+1)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	var wg sync.WaitGroup
	var receivedTokens []string
	var mu sync.Mutex

	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			mu.Lock()
			receivedTokens = append(receivedTokens, token)
			mu.Unlock()
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	if len(receivedTokens) != len(tokens) {
		t.Fatalf("received %d tokens, want %d", len(receivedTokens), len(tokens))
	}

	for i, token := range receivedTokens {
		if token != tokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, tokens[i])
		}
	}

	// Verify the concatenated message
	fullMessage := strings.Join(receivedTokens, "")
	expectedMessage := "The quick brown fox jumps over the lazy dog."
	if fullMessage != expectedMessage {
		t.Errorf("full message = %q, want %q", fullMessage, expectedMessage)
	}
}

// TestOpenAI_Chat_RequestBody verifies the request body is correctly formatted.
func TestOpenAI_Chat_RequestBody(t *testing.T) {
	tests := []struct {
		name      string
		request   *ChatRequest
		checkBody func(t *testing.T, body string)
	}{
		{
			name: "basic request",
			request: &ChatRequest{
				Model:       "gpt-4o",
				Messages:    []Message{{Role: "user", Content: "Hello"}},
				Temperature: 0.7,
			},
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, `"model":"gpt-4o"`) {
					t.Error("body should contain model")
				}
				if !strings.Contains(body, `"stream":true`) {
					t.Error("body should have stream:true")
				}
				if !strings.Contains(body, `"temperature":0.7`) {
					t.Error("body should contain temperature")
				}
				if strings.Contains(body, `"max_tokens"`) {
					t.Error("body should not contain max_tokens when not set")
				}
			},
		},
		{
			name: "request with max_tokens",
			request: &ChatRequest{
				Model:       "gpt-4o",
				Messages:    []Message{{Role: "user", Content: "Hello"}},
				Temperature: 0.5,
				MaxTokens:   1000,
			},
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, `"max_tokens":1000`) {
					t.Error("body should contain max_tokens")
				}
			},
		},
		{
			name: "request with system message",
			request: &ChatRequest{
				Model: "gpt-4o",
				Messages: []Message{
					{Role: "system", Content: "You are a helpful assistant"},
					{Role: "user", Content: "Hello"},
				},
				Temperature: 0.7,
			},
			checkBody: func(t *testing.T, body string) {
				if !strings.Contains(body, `"role":"system"`) {
					t.Error("body should contain system role")
				}
				if !strings.Contains(body, `"role":"user"`) {
					t.Error("body should contain user role")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var receivedBody string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				buf := make([]byte, r.ContentLength)
				r.Body.Read(buf)
				receivedBody = string(buf)

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Hi\"}}]}\n\ndata: [DONE]\n\n"))
			}))
			defer server.Close()

			provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
			stream := make(chan string, 10)

			err := provider.Chat(context.Background(), tt.request, stream)
			if err != nil {
				t.Fatalf("Chat() error = %v", err)
			}

			// Drain the stream
			for range stream {
			}

			tt.checkBody(t, receivedBody)
		})
	}
}

// TestOpenAI_Chat_EmptyResponse tests handling of empty response body.
func TestOpenAI_Chat_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		// Send [DONE] immediately without any tokens
		w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	var receivedTokens []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	if len(receivedTokens) != 0 {
		t.Errorf("expected no tokens, got %v", receivedTokens)
	}
}

// TestOpenAI_Chat_FinishReason tests handling of finish_reason in response.
func TestOpenAI_Chat_FinishReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		sseData := `data: {"choices":[{"delta":{"content":"Hello"}}]}

data: {"choices":[{"delta":{"content":" world"}}]}

data: {"choices":[{"delta":{},"finish_reason":"stop"}]}

data: [DONE]

`
		w.Write([]byte(sseData))
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	var receivedTokens []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	expectedTokens := []string{"Hello", " world"}
	if len(receivedTokens) != len(expectedTokens) {
		t.Fatalf("received %d tokens, want %d: got %v", len(receivedTokens), len(expectedTokens), receivedTokens)
	}

	for i, token := range receivedTokens {
		if token != expectedTokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, expectedTokens[i])
		}
	}
}

// TestOpenAI_Chat_UnicodeContent tests handling of Unicode content in responses.
func TestOpenAI_Chat_UnicodeContent(t *testing.T) {
	expectedTokens := []string{"Hello", " 世界", " 🌍", " مرحبا"}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		for _, token := range expectedTokens {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", token)
			flusher.Flush()
		}

		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := NewOpenAIWithBaseURL("test-api-key", server.URL)
	stream := make(chan string, 10)

	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "Hello"}},
	}

	var receivedTokens []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for token := range stream {
			receivedTokens = append(receivedTokens, token)
		}
	}()

	err := provider.Chat(context.Background(), req, stream)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	wg.Wait()

	if len(receivedTokens) != len(expectedTokens) {
		t.Fatalf("received %d tokens, want %d", len(receivedTokens), len(expectedTokens))
	}

	for i, token := range receivedTokens {
		if token != expectedTokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, expectedTokens[i])
		}
	}
}

// TestNewOpenAIWithBaseURL verifies the constructor sets the correct base URL.
func TestNewOpenAIWithBaseURL(t *testing.T) {
	customURL := "https://custom.api.example.com/v1/chat"
	provider := NewOpenAIWithBaseURL("test-key", customURL)

	if provider.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", provider.apiKey, "test-key")
	}
	if provider.baseURL != customURL {
		t.Errorf("baseURL = %q, want %q", provider.baseURL, customURL)
	}
	if provider.client == nil {
		t.Error("client should not be nil")
	}
}

// TestNewOpenAI verifies the default constructor sets the default base URL.
func TestNewOpenAI(t *testing.T) {
	provider := NewOpenAI("test-key")

	if provider.apiKey != "test-key" {
		t.Errorf("apiKey = %q, want %q", provider.apiKey, "test-key")
	}
	if provider.baseURL != defaultOpenAIBaseURL {
		t.Errorf("baseURL = %q, want %q", provider.baseURL, defaultOpenAIBaseURL)
	}
	if provider.client == nil {
		t.Error("client should not be nil")
	}
}
