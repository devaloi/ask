// Package provider defines the interface for LLM providers
// and implements the provider factory.
package provider

import (
	"context"
	"fmt"

	"github.com/devaloi/ask/internal/config"
)

// Message represents a chat message.
type Message struct {
	Role    string `json:"role"` // "system", "user", or "assistant"
	Content string `json:"content"`
}

// ChatRequest contains the parameters for a chat completion request.
type ChatRequest struct {
	Messages    []Message
	Model       string
	Temperature float64
	MaxTokens   int
}

// Provider is the interface that all LLM providers must implement.
// Adding a new provider requires implementing this interface in a single file.
type Provider interface {
	// Chat sends a chat request and streams tokens to the channel.
	// The channel is closed when the response is complete or an error occurs.
	Chat(ctx context.Context, req *ChatRequest, stream chan<- string) error

	// Models returns the list of available models for this provider.
	Models() []string

	// Name returns the provider name.
	Name() string
}

// New creates a new provider instance based on the provider name.
// It validates that the required API key is configured.
func New(name string, cfg *config.Config) (Provider, error) {
	apiKey := cfg.GetAPIKey(name)
	switch name {
	case "openai":
		if apiKey == "" {
			return nil, fmt.Errorf("OpenAI API key not found.\n\nSet OPENAI_API_KEY environment variable or add it to ~/.config/ask/config.yaml:\n\n  providers:\n    openai:\n      api_key: your-key-here")
		}
		return NewOpenAI(apiKey), nil
	case "anthropic":
		if apiKey == "" {
			return nil, fmt.Errorf("Anthropic API key not found.\n\nSet ANTHROPIC_API_KEY environment variable or add it to ~/.config/ask/config.yaml:\n\n  providers:\n    anthropic:\n      api_key: your-key-here")
		}
		return NewAnthropic(apiKey), nil
	default:
		return nil, fmt.Errorf("unknown provider: %s\n\nAvailable providers: openai, anthropic", name)
	}
}
