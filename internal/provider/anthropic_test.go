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

// TestAnthropicName tests the Name method.
func TestAnthropicName(t *testing.T) {
	provider := NewAnthropic("test-api-key")
	if got := provider.Name(); got != "anthropic" {
		t.Errorf("Name() = %q, want %q", got, "anthropic")
	}
}

// TestAnthropicModels tests the Models method.
func TestAnthropicModels(t *testing.T) {
	provider := NewAnthropic("test-api-key")
	models := provider.Models()

	if len(models) == 0 {
		t.Fatal("Models() returned empty slice")
	}

	// Verify expected models are present
	expectedModels := map[string]bool{
		"claude-sonnet-4-20250514":  false,
		"claude-3-5-haiku-20241022": false,
		"claude-3-opus-20240229":    false,
	}

	for _, model := range models {
		if _, ok := expectedModels[model]; ok {
			expectedModels[model] = true
		}
	}

	for model, found := range expectedModels {
		if !found {
			t.Errorf("Models() missing expected model %q", model)
		}
	}
}

// newTestAnthropicWithServer creates an Anthropic provider configured to use a test server.
func newTestAnthropicWithServer(server *httptest.Server, apiKey string) *Anthropic {
	return &Anthropic{
		apiKey: apiKey,
		client: &http.Client{
			Transport: &testTransport{
				serverURL: server.URL,
			},
		},
	}
}

// testTransport redirects all requests to the test server.
type testTransport struct {
	serverURL string
}

func (t *testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Redirect the request to the test server
	req.URL.Scheme = "http"
	req.URL.Host = strings.TrimPrefix(t.serverURL, "http://")
	return http.DefaultTransport.RoundTrip(req)
}

// TestAnthropicChatSuccess tests a successful streaming response.
func TestAnthropicChatSuccess(t *testing.T) {
	sseResponse := "event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_123\",\"type\":\"message\",\"role\":\"assistant\"}}\n" +
		"\n" +
		"event: content_block_start\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n" +
		"\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n" +
		"\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\" world\"}}\n" +
		"\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"!\"}}\n" +
		"\n" +
		"event: content_block_stop\n" +
		"data: {\"type\":\"content_block_stop\",\"index\":0}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type header = %q, want %q", got, "application/json")
		}
		if got := r.Header.Get("x-api-key"); got != "test-api-key" {
			t.Errorf("x-api-key header = %q, want %q", got, "test-api-key")
		}
		if got := r.Header.Get("anthropic-version"); got != anthropicAPIVersion {
			t.Errorf("anthropic-version header = %q, want %q", got, anthropicAPIVersion)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Say hello"},
		},
		Model: "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	// Collect all tokens
	var tokens []string
	for token := range stream {
		tokens = append(tokens, token)
	}

	// Verify tokens arrived in order
	expectedTokens := []string{"Hello", " world", "!"}
	if len(tokens) != len(expectedTokens) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(expectedTokens), tokens)
	}

	for i, token := range tokens {
		if token != expectedTokens[i] {
			t.Errorf("token[%d] = %q, want %q", i, token, expectedTokens[i])
		}
	}
}

// TestAnthropicChatStreamClosed tests that the stream channel is closed when done.
func TestAnthropicChatStreamClosed(t *testing.T) {
	sseResponse := "event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"test\"}}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Model:    "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	// Verify channel is closed by trying to receive with a timeout
	select {
	case _, ok := <-stream:
		if ok {
			// Got a token, drain the rest
			for range stream {
			}
		}
		// Channel is closed as expected
	case <-time.After(time.Second):
		t.Error("stream channel was not closed")
	}
}

// TestAnthropicChatHTTPErrors tests handling of various HTTP error codes.
func TestAnthropicChatHTTPErrors(t *testing.T) {
	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		wantErrContain string
	}{
		{
			name:           "unauthorized",
			statusCode:     http.StatusUnauthorized,
			responseBody:   `{"error":{"message":"Invalid API Key"}}`,
			wantErrContain: "invalid API key",
		},
		{
			name:           "rate_limited",
			statusCode:     http.StatusTooManyRequests,
			responseBody:   `{"error":{"message":"Rate limit exceeded"}}`,
			wantErrContain: "rate limited",
		},
		{
			name:           "server_error",
			statusCode:     http.StatusInternalServerError,
			responseBody:   `{"error":{"message":"Internal server error"}}`,
			wantErrContain: "service error",
		},
		{
			name:           "bad_gateway",
			statusCode:     http.StatusBadGateway,
			responseBody:   `{"error":{"message":"Bad gateway"}}`,
			wantErrContain: "service error",
		},
		{
			name:           "service_unavailable",
			statusCode:     http.StatusServiceUnavailable,
			responseBody:   `{"error":{"message":"Service unavailable"}}`,
			wantErrContain: "service error",
		},
		{
			name:           "bad_request",
			statusCode:     http.StatusBadRequest,
			responseBody:   `{"error":{"message":"Invalid request body"}}`,
			wantErrContain: "API error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			provider := newTestAnthropicWithServer(server, "test-api-key")

			stream := make(chan string, 10)
			req := &ChatRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
				Model:    "claude-sonnet-4-20250514",
			}

			ctx := context.Background()
			err := provider.Chat(ctx, req, stream)

			if err == nil {
				t.Fatal("Chat() expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.wantErrContain) {
				t.Errorf("error = %q, want to contain %q", err.Error(), tt.wantErrContain)
			}

			// Verify stream is closed even on error
			select {
			case _, ok := <-stream:
				if ok {
					for range stream {
					}
				}
			case <-time.After(time.Second):
				t.Error("stream channel was not closed after error")
			}
		})
	}
}

// TestAnthropicChatContextCancellation tests that context cancellation stops the stream.
func TestAnthropicChatContextCancellation(t *testing.T) {
	// Server that streams slowly
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support Flusher")
		}

		// Send first token
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"First"}}`)
		fmt.Fprint(w, "\n\n")
		flusher.Flush()

		// Wait to simulate slow response - context should be cancelled during this time
		time.Sleep(500 * time.Millisecond)

		// This token should not be processed due to cancellation
		fmt.Fprint(w, "event: content_block_delta\n")
		fmt.Fprint(w, `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Second"}}`)
		fmt.Fprint(w, "\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Model:    "claude-sonnet-4-20250514",
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Start Chat in a goroutine
	var wg sync.WaitGroup
	var chatErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		chatErr = provider.Chat(ctx, req, stream)
	}()

	// Wait for first token then cancel
	select {
	case <-stream:
		// Got first token, now cancel
		cancel()
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for first token")
	}

	// Wait for Chat to complete
	wg.Wait()

	// Verify context.Canceled error
	if chatErr != context.Canceled {
		t.Errorf("Chat() error = %v, want context.Canceled", chatErr)
	}
}

// TestAnthropicChatMalformedSSE tests graceful handling of malformed SSE responses.
func TestAnthropicChatMalformedSSE(t *testing.T) {
	tests := []struct {
		name        string
		sseResponse string
		wantTokens  []string
	}{
		{
			name: "malformed_json_skipped",
			sseResponse: "event: content_block_delta\n" +
				"data: {invalid json}\n" +
				"\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"valid\"}}\n" +
				"\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n" +
				"\n",
			wantTokens: []string{"valid"},
		},
		{
			name: "malformed_delta_skipped",
			sseResponse: "event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{invalid}}\n" +
				"\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"good\"}}\n" +
				"\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n" +
				"\n",
			wantTokens: []string{"good"},
		},
		{
			name: "empty_text_skipped",
			sseResponse: "event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"\"}}\n" +
				"\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"content\"}}\n" +
				"\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n" +
				"\n",
			wantTokens: []string{"content"},
		},
		{
			name: "unknown_event_types_ignored",
			sseResponse: "event: ping\n" +
				"data: {}\n" +
				"\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n" +
				"\n" +
				"event: unknown_event\n" +
				"data: {\"some\":\"data\"}\n" +
				"\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n" +
				"\n",
			wantTokens: []string{"hello"},
		},
		{
			name: "missing_delta_field",
			sseResponse: "event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0}\n" +
				"\n" +
				"event: content_block_delta\n" +
				"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"works\"}}\n" +
				"\n" +
				"event: message_stop\n" +
				"data: {\"type\":\"message_stop\"}\n" +
				"\n",
			wantTokens: []string{"works"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(tt.sseResponse))
			}))
			defer server.Close()

			provider := newTestAnthropicWithServer(server, "test-api-key")

			stream := make(chan string, 10)
			req := &ChatRequest{
				Messages: []Message{{Role: "user", Content: "test"}},
				Model:    "claude-sonnet-4-20250514",
			}

			ctx := context.Background()
			err := provider.Chat(ctx, req, stream)
			if err != nil {
				t.Fatalf("Chat() returned error: %v", err)
			}

			var tokens []string
			for token := range stream {
				tokens = append(tokens, token)
			}

			if len(tokens) != len(tt.wantTokens) {
				t.Fatalf("got %d tokens %v, want %d tokens %v", len(tokens), tokens, len(tt.wantTokens), tt.wantTokens)
			}

			for i, token := range tokens {
				if token != tt.wantTokens[i] {
					t.Errorf("token[%d] = %q, want %q", i, token, tt.wantTokens[i])
				}
			}
		})
	}
}

// TestAnthropicChatSystemMessage tests that system messages are extracted to top-level field.
func TestAnthropicChatSystemMessage(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture the request body to verify system message handling
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"response\"}}\n" +
			"\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Hello"},
		},
		Model: "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	// Drain stream
	for range stream {
	}

	// Verify the request body
	bodyStr := string(capturedBody)

	// System message should be at top level, not in messages array
	if !strings.Contains(bodyStr, `"system":"You are a helpful assistant."`) {
		t.Errorf("request body should contain top-level system field: %s", bodyStr)
	}

	// Messages array should only contain user message
	if strings.Contains(bodyStr, `"role":"system"`) {
		t.Errorf("messages array should not contain system role: %s", bodyStr)
	}

	if !strings.Contains(bodyStr, `"role":"user"`) {
		t.Errorf("messages array should contain user role: %s", bodyStr)
	}
}

// TestAnthropicChatMultipleSystemMessages tests combining multiple system messages.
func TestAnthropicChatMultipleSystemMessages(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "First system message."},
			{Role: "system", Content: "Second system message."},
			{Role: "user", Content: "Hello"},
		},
		Model: "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	// Multiple system messages should be concatenated
	if !strings.Contains(bodyStr, "First system message.") || !strings.Contains(bodyStr, "Second system message.") {
		t.Errorf("request body should contain both system messages: %s", bodyStr)
	}
}

// TestAnthropicChatMessageStop tests that message_stop event terminates the stream properly.
func TestAnthropicChatMessageStop(t *testing.T) {
	// This response has tokens after message_stop which should be ignored
	sseResponse := "event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"before\"}}\n" +
		"\n" +
		"event: message_stop\n" +
		"data: {\"type\":\"message_stop\"}\n" +
		"\n" +
		"event: content_block_delta\n" +
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"after\"}}\n" +
		"\n"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Model:    "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	var tokens []string
	for token := range stream {
		tokens = append(tokens, token)
	}

	// Should only have "before", not "after"
	if len(tokens) != 1 || tokens[0] != "before" {
		t.Errorf("got tokens %v, want [\"before\"]", tokens)
	}
}

// TestAnthropicChatDefaultMaxTokens tests that default max_tokens is set when not specified.
func TestAnthropicChatDefaultMaxTokens(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages:  []Message{{Role: "user", Content: "test"}},
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 0, // Not specified
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	// Should use default max_tokens (4096)
	if !strings.Contains(bodyStr, `"max_tokens":4096`) {
		t.Errorf("request body should contain default max_tokens:4096: %s", bodyStr)
	}
}

// TestAnthropicChatCustomMaxTokens tests that custom max_tokens is used when specified.
func TestAnthropicChatCustomMaxTokens(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages:  []Message{{Role: "user", Content: "test"}},
		Model:     "claude-sonnet-4-20250514",
		MaxTokens: 1000,
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	if !strings.Contains(bodyStr, `"max_tokens":1000`) {
		t.Errorf("request body should contain max_tokens:1000: %s", bodyStr)
	}
}

// TestAnthropicChatTemperature tests temperature parameter handling.
func TestAnthropicChatTemperature(t *testing.T) {
	tests := []struct {
		name        string
		temperature float64
		wantInBody  bool
	}{
		{
			name:        "zero_temperature_omitted",
			temperature: 0,
			wantInBody:  false,
		},
		{
			name:        "positive_temperature_included",
			temperature: 0.7,
			wantInBody:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBody []byte

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body := make([]byte, r.ContentLength)
				r.Body.Read(body)
				capturedBody = body

				w.Header().Set("Content-Type", "text/event-stream")
				w.WriteHeader(http.StatusOK)
				sseResponse := "event: message_stop\n" +
					"data: {\"type\":\"message_stop\"}\n" +
					"\n"
				w.Write([]byte(sseResponse))
			}))
			defer server.Close()

			provider := newTestAnthropicWithServer(server, "test-api-key")

			stream := make(chan string, 10)
			req := &ChatRequest{
				Messages:    []Message{{Role: "user", Content: "test"}},
				Model:       "claude-sonnet-4-20250514",
				Temperature: tt.temperature,
			}

			ctx := context.Background()
			err := provider.Chat(ctx, req, stream)
			if err != nil {
				t.Fatalf("Chat() returned error: %v", err)
			}

			for range stream {
			}

			bodyStr := string(capturedBody)
			hasTemperature := strings.Contains(bodyStr, `"temperature"`)

			if hasTemperature != tt.wantInBody {
				t.Errorf("temperature in body = %v, want %v: %s", hasTemperature, tt.wantInBody, bodyStr)
			}
		})
	}
}

// TestAnthropicChatConversationHistory tests multi-turn conversations.
func TestAnthropicChatConversationHistory(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: content_block_delta\n" +
			"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"response\"}}\n" +
			"\n" +
			"event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{
			{Role: "user", Content: "Hello"},
			{Role: "assistant", Content: "Hi there!"},
			{Role: "user", Content: "How are you?"},
		},
		Model: "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	// Verify all messages are present with correct roles
	if !strings.Contains(bodyStr, `"role":"user"`) {
		t.Error("request body should contain user role")
	}
	if !strings.Contains(bodyStr, `"role":"assistant"`) {
		t.Error("request body should contain assistant role")
	}
	if !strings.Contains(bodyStr, `"Hello"`) {
		t.Error("request body should contain first user message")
	}
	if !strings.Contains(bodyStr, `"Hi there!"`) {
		t.Error("request body should contain assistant message")
	}
	if !strings.Contains(bodyStr, `"How are you?"`) {
		t.Error("request body should contain second user message")
	}
}

// TestAnthropicChatStreamEnabled tests that stream is always enabled.
func TestAnthropicChatStreamEnabled(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Model:    "claude-sonnet-4-20250514",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	if !strings.Contains(bodyStr, `"stream":true`) {
		t.Errorf("request body should contain stream:true: %s", bodyStr)
	}
}

// TestAnthropicChatRequestModel tests that the model is correctly passed in the request.
func TestAnthropicChatRequestModel(t *testing.T) {
	var capturedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		capturedBody = body

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		sseResponse := "event: message_stop\n" +
			"data: {\"type\":\"message_stop\"}\n" +
			"\n"
		w.Write([]byte(sseResponse))
	}))
	defer server.Close()

	provider := newTestAnthropicWithServer(server, "test-api-key")

	stream := make(chan string, 10)
	req := &ChatRequest{
		Messages: []Message{{Role: "user", Content: "test"}},
		Model:    "claude-3-opus-20240229",
	}

	ctx := context.Background()
	err := provider.Chat(ctx, req, stream)
	if err != nil {
		t.Fatalf("Chat() returned error: %v", err)
	}

	for range stream {
	}

	bodyStr := string(capturedBody)

	if !strings.Contains(bodyStr, `"model":"claude-3-opus-20240229"`) {
		t.Errorf("request body should contain the specified model: %s", bodyStr)
	}
}
