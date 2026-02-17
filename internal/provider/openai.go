package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1/chat/completions"

// OpenAI implements the Provider interface for OpenAI's API.
type OpenAI struct {
	apiKey  string
	client  *http.Client
	baseURL string
}

// NewOpenAI creates a new OpenAI provider with the given API key.
func NewOpenAI(apiKey string) *OpenAI {
	return &OpenAI{
		apiKey:  apiKey,
		client:  &http.Client{},
		baseURL: defaultOpenAIBaseURL,
	}
}

// NewOpenAIWithBaseURL creates a new OpenAI provider with a custom base URL (for testing).
func NewOpenAIWithBaseURL(apiKey, baseURL string) *OpenAI {
	return &OpenAI{
		apiKey:  apiKey,
		client:  &http.Client{},
		baseURL: baseURL,
	}
}

// Name returns the provider name.
func (o *OpenAI) Name() string {
	return "openai"
}

// Models returns the list of available models for OpenAI.
func (o *OpenAI) Models() []string {
	return []string{
		"gpt-4o",
		"gpt-4o-mini",
		"gpt-4-turbo",
		"gpt-3.5-turbo",
	}
}

// openAIRequest is the request body for the OpenAI chat completions API.
type openAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

// openAIStreamResponse represents a single SSE chunk from the OpenAI API.
type openAIStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// Chat sends a chat request to OpenAI and streams tokens to the channel.
func (o *OpenAI) Chat(ctx context.Context, req *ChatRequest, stream chan<- string) error {
	defer close(stream)

	reqBody := openAIRequest{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		Stream:      true,
	}
	if req.MaxTokens > 0 {
		reqBody.MaxTokens = req.MaxTokens
	}

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, o.baseURL, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := o.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return o.handleHTTPError(resp)
	}

	return o.parseSSEStream(ctx, resp.Body, stream)
}

// handleHTTPError returns an appropriate error message based on the HTTP status code.
func (o *OpenAI) handleHTTPError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("Invalid API key. Check your OPENAI_API_KEY.")
	case http.StatusTooManyRequests:
		return fmt.Errorf("Rate limited. Please wait and try again.")
	default:
		if resp.StatusCode >= 500 {
			return fmt.Errorf("OpenAI service error. Please try again later.")
		}
		return fmt.Errorf("OpenAI API error (status %d): %s", resp.StatusCode, string(body))
	}
}

// parseSSEStream reads the SSE stream and sends tokens to the channel.
func (o *OpenAI) parseSSEStream(ctx context.Context, body io.Reader, stream chan<- string) error {
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// SSE data lines start with "data: "
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for the [DONE] sentinel
		if data == "[DONE]" {
			return nil
		}

		var chunk openAIStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case stream <- chunk.Choices[0].Delta.Content:
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("error reading stream: %w", err)
	}

	return nil
}
