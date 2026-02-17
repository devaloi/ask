package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/devaloi/ask/internal/provider"
	"github.com/devaloi/ask/internal/stream"
)

var continueFlag int64

func init() {
	rootCmd.Flags().Int64VarP(&continueFlag, "continue", "c", 0, "Continue conversation with ID")
}

func runChat(cmd *cobra.Command, args []string) error {
	// If no arguments and stdin is a terminal, enter interactive mode
	stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	if len(args) == 0 && stdinIsTerminal {
		return runInteractive()
	}

	// One-shot mode
	return runOneShot(args)
}

func runOneShot(args []string) error {
	ctx := context.Background()

	// Build prompt from args and stdin
	prompt, err := buildPrompt(args)
	if err != nil {
		return err
	}

	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("no prompt provided\n\nUsage: ask \"your question\"\n       cat file | ask \"explain this\"")
	}

	// Get system prompt if specified
	systemPrompt, err := resolveSystemPrompt(systemFlag)
	if err != nil {
		return err
	}

	// Create provider
	providerName := getProvider()
	p, err := provider.New(providerName, cfg)
	if err != nil {
		return err
	}

	// Build messages
	messages := []provider.Message{}
	if systemPrompt != "" {
		messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, provider.Message{Role: "user", Content: prompt})

	// Create request
	req := &provider.ChatRequest{
		Messages: messages,
		Model:    getModel(),
	}

	// Create stream channel
	tokens := make(chan string, 100)

	// Create writer
	stdoutIsTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	writer := stream.NewWriter(os.Stdout, stdoutIsTerminal)

	// Start streaming in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Chat(ctx, req, tokens)
	}()

	// Read and write tokens
	for token := range tokens {
		if err := writer.Write(token); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}
	writer.Flush()

	// Check for errors from provider
	if err := <-errCh; err != nil {
		return err
	}

	return nil
}

func buildPrompt(args []string) (string, error) {
	var parts []string

	// Read from stdin if data is available
	stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))
	if !stdinIsTerminal {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("failed to read from stdin: %w", err)
		}
		if len(data) > 0 {
			parts = append(parts, string(data))
		}
	}

	// Add command line arguments
	if len(args) > 0 {
		parts = append(parts, strings.Join(args, " "))
	}

	return strings.Join(parts, "\n\n"), nil
}

func resolveSystemPrompt(s string) (string, error) {
	if s == "" {
		return "", nil
	}

	// Check if it's a file reference
	if strings.HasPrefix(s, "@") {
		path := strings.TrimPrefix(s, "@")
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("failed to read system prompt file %s: %w", path, err)
		}
		return string(data), nil
	}

	return s, nil
}

func runInteractive() error {
	ctx := context.Background()

	// Create provider
	providerName := getProvider()
	p, err := provider.New(providerName, cfg)
	if err != nil {
		return err
	}

	fmt.Printf("ask — using %s/%s\n", p.Name(), getModel())
	fmt.Println("Type /quit to exit, /new to start fresh, /help for commands")
	fmt.Println()

	// Get system prompt if specified
	systemPrompt, err := resolveSystemPrompt(systemFlag)
	if err != nil {
		return err
	}

	// Message history for the conversation
	var messages []provider.Message
	if systemPrompt != "" {
		messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	}

	reader := bufio.NewReader(os.Stdin)
	writer := stream.NewWriter(os.Stdout, true)

	for {
		fmt.Print("> ")
		input, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				fmt.Println()
				return nil
			}
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle special commands
		if strings.HasPrefix(input, "/") {
			cmd := strings.ToLower(input)
			switch {
			case cmd == "/quit" || cmd == "/exit" || cmd == "/q":
				return nil
			case cmd == "/new" || cmd == "/clear":
				messages = messages[:0]
				if systemPrompt != "" {
					messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
				}
				fmt.Println("Started new conversation")
				continue
			case strings.HasPrefix(cmd, "/model "):
				newModel := strings.TrimPrefix(input, "/model ")
				modelFlag = strings.TrimSpace(newModel)
				fmt.Printf("Switched to model: %s\n", modelFlag)
				continue
			case cmd == "/help":
				printHelp()
				continue
			default:
				fmt.Printf("Unknown command: %s (type /help for commands)\n", input)
				continue
			}
		}

		// Add user message
		messages = append(messages, provider.Message{Role: "user", Content: input})

		// Create request
		req := &provider.ChatRequest{
			Messages: messages,
			Model:    getModel(),
		}

		// Stream response
		tokens := make(chan string, 100)
		errCh := make(chan error, 1)

		go func() {
			errCh <- p.Chat(ctx, req, tokens)
		}()

		// Collect response
		var response strings.Builder
		for token := range tokens {
			response.WriteString(token)
			if err := writer.Write(token); err != nil {
				fmt.Printf("\nError writing output: %v\n", err)
				break
			}
		}
		writer.Flush()
		fmt.Println()

		// Check for errors
		if err := <-errCh; err != nil {
			fmt.Printf("Error: %v\n", err)
			// Remove the failed user message
			messages = messages[:len(messages)-1]
			continue
		}

		// Add assistant response to history
		messages = append(messages, provider.Message{Role: "assistant", Content: response.String()})
	}
}

func printHelp() {
	fmt.Println(`Commands:
  /quit, /exit, /q  Exit interactive mode
  /new, /clear      Start a new conversation
  /model <name>     Switch model
  /help             Show this help`)
}
