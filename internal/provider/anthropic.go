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

const (
	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"
	defaultMaxTokens    = 4096
)

// Anthropic implements the Provider interface for Anthropic's Claude API.
type Anthropic struct {
	apiKey string
	client *http.Client
}

// NewAnthropic creates a new Anthropic provider with the given API key.
func NewAnthropic(apiKey string) *Anthropic {
	return &Anthropic{
		apiKey: apiKey,
		client: &http.Client{},
	}
}

// Name returns the provider name.
func (a *Anthropic) Name() string {
	return "anthropic"
}

// Models returns the list of available Claude models.
func (a *Anthropic) Models() []string {
	return []string{
		"claude-sonnet-4-20250514",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
	}
}

// anthropicRequest is the request body for the Anthropic API.
type anthropicRequest struct {
	Model       string             `json:"model"`
	Messages    []anthropicMessage `json:"messages"`
	System      string             `json:"system,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature float64            `json:"temperature,omitempty"`
	Stream      bool               `json:"stream"`
}

// anthropicMessage represents a message in the Anthropic API format.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicSSEEvent represents a parsed SSE event from the Anthropic API.
type anthropicSSEEvent struct {
	Type  string          `json:"type"`
	Delta json.RawMessage `json:"delta,omitempty"`
}

// anthropicDelta represents the delta object in content_block_delta events.
type anthropicDelta struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Chat sends a chat request to the Anthropic API and streams tokens to the channel.
func (a *Anthropic) Chat(ctx context.Context, req *ChatRequest, stream chan<- string) error {
	defer close(stream)

	// Separate system messages from user/assistant messages
	var systemPrompt string
	var messages []anthropicMessage

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			// Anthropic expects system as a top-level field
			if systemPrompt != "" {
				systemPrompt += "\n\n"
			}
			systemPrompt += msg.Content
		} else {
			messages = append(messages, anthropicMessage{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Set max_tokens to default if not specified (required by Anthropic API)
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	// Build the request body
	apiReq := anthropicRequest{
		Model:     req.Model,
		Messages:  messages,
		System:    systemPrompt,
		MaxTokens: maxTokens,
		Stream:    true,
	}

	// Only include temperature if it's set (non-zero)
	if req.Temperature > 0 {
		apiReq.Temperature = req.Temperature
	}

	body, err := json.Marshal(apiReq)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create the HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Set required headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicAPIVersion)

	// Send the request
	resp, err := a.client.Do(httpReq)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Handle HTTP errors
	if resp.StatusCode != http.StatusOK {
		return a.handleHTTPError(resp)
	}

	// Parse SSE stream
	return a.parseSSEStream(ctx, resp.Body, stream)
}

// handleHTTPError returns an appropriate error message based on the HTTP status code.
func (a *Anthropic) handleHTTPError(resp *http.Response) error {
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return fmt.Errorf("Invalid API key. Check your ANTHROPIC_API_KEY.")
	case http.StatusTooManyRequests:
		return fmt.Errorf("Rate limited. Please wait and try again.")
	default:
		if resp.StatusCode >= 500 {
			return fmt.Errorf("Anthropic service error. Please try again later.")
		}
		// Read error body for other errors
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(body))
	}
}

// parseSSEStream parses the SSE stream from the Anthropic API and sends tokens to the channel.
func (a *Anthropic) parseSSEStream(ctx context.Context, body io.Reader, stream chan<- string) error {
	scanner := bufio.NewScanner(body)

	var eventType string

	for scanner.Scan() {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := scanner.Text()

		// Parse event type
		if strings.HasPrefix(line, "event: ") {
			eventType = strings.TrimPrefix(line, "event: ")
			continue
		}

		// Parse data line
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Handle message_stop event
			if eventType == "message_stop" {
				return nil
			}

			// Only process content_block_delta events
			if eventType == "content_block_delta" {
				var event anthropicSSEEvent
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue // Skip malformed JSON
				}

				// Extract text from delta
				if event.Delta != nil {
					var delta anthropicDelta
					if err := json.Unmarshal(event.Delta, &delta); err != nil {
						continue // Skip malformed delta
					}

					if delta.Text != "" {
						select {
						case stream <- delta.Text:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
				}
			}
		}

		// Empty line marks end of an event
		if line == "" {
			eventType = ""
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
