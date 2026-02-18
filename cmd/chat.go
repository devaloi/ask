package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/devaloi/ask/internal/config"
	"github.com/devaloi/ask/internal/history"
	"github.com/devaloi/ask/internal/provider"
	"github.com/devaloi/ask/internal/stream"
	"github.com/devaloi/ask/internal/util"
)

var continueFlag int64

func init() {
	rootCmd.Flags().Int64VarP(&continueFlag, "continue", "c", 0, "Continue conversation with ID")
}

func runChat(cmd *cobra.Command, args []string) error {
	// If no arguments and stdin is a terminal, enter interactive mode
	stdinIsTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	if len(args) == 0 && stdinIsTerminal && continueFlag == 0 {
		return runInteractive()
	}

	// One-shot mode (or continue mode)
	return runOneShot(args)
}

func runOneShot(args []string) error {
	ctx := context.Background()

	// Build prompt from args and stdin
	prompt, err := buildPrompt(args)
	if err != nil {
		return fmt.Errorf("building prompt: %w", err)
	}

	if strings.TrimSpace(prompt) == "" && continueFlag == 0 {
		return fmt.Errorf("no prompt provided\n\nUsage: ask \"your question\"\n       cat file | ask \"explain this\"")
	}

	// Get system prompt if specified
	systemPrompt, err := resolveSystemPrompt(systemFlag)
	if err != nil {
		return fmt.Errorf("resolving system prompt: %w", err)
	}

	// Create provider
	providerName := getProvider()
	p, err := provider.New(providerName, cfg)
	if err != nil {
		return fmt.Errorf("creating provider: %w", err)
	}

	// Build messages - either new or from continued conversation
	var messages []provider.Message
	var conv *history.Conversation

	if continueFlag > 0 {
		// Load previous conversation
		store, err := openStore()
		if err != nil {
			return fmt.Errorf("opening history store: %w", err)
		}
		defer store.Close()

		conv, err = store.GetConversation(continueFlag)
		if err != nil {
			return fmt.Errorf("loading conversation %d: %w", continueFlag, err)
		}

		// Convert history messages to provider messages
		for _, msg := range conv.Messages {
			messages = append(messages, provider.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	}

	// Add system prompt if starting fresh
	if systemPrompt != "" && continueFlag == 0 {
		messages = append(messages, provider.Message{Role: "system", Content: systemPrompt})
	}

	// Add user message if provided
	if strings.TrimSpace(prompt) != "" {
		messages = append(messages, provider.Message{Role: "user", Content: prompt})
	}

	// Create request
	req := &provider.ChatRequest{
		Messages: messages,
		Model:    getModel(),
	}

	// Create stream channel
	tokens := make(chan string, util.DefaultChannelBuffer)

	// Create writer
	stdoutIsTerminal := term.IsTerminal(int(os.Stdout.Fd()))
	writer := stream.NewWriter(os.Stdout, stdoutIsTerminal)

	// Start streaming in goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- p.Chat(ctx, req, tokens)
	}()

	// Read and write tokens, collect response
	var response strings.Builder
	for token := range tokens {
		response.WriteString(token)
		if err := writer.Write(token); err != nil {
			return fmt.Errorf("failed to write output: %w", err)
		}
	}
	writer.Flush()

	// Check for errors from provider
	if err := <-errCh; err != nil {
		return fmt.Errorf("chat stream: %w", err)
	}

	// Save to history if TTY (don't save when piped)
	if stdoutIsTerminal && strings.TrimSpace(prompt) != "" {
		if err := saveToHistory(p.Name(), getModel(), messages, response.String(), conv); err != nil {
			// Don't fail the command, just warn about history
			fmt.Fprintf(os.Stderr, "Warning: failed to save to history: %v\n", err)
		}
	}

	return nil
}

func saveToHistory(providerName, model string, messages []provider.Message, response string, existingConv *history.Conversation) error {
	store, err := openStore()
	if err != nil {
		return err
	}
	defer store.Close()

	conv := existingConv
	if conv == nil {
		conv = &history.Conversation{
			Model:    model,
			Provider: providerName,
		}
	}

	// Add the new messages
	var newMessages []history.Message

	// If this is a new conversation, add all messages
	if existingConv == nil {
		for _, msg := range messages {
			newMessages = append(newMessages, history.Message{
				Role:    msg.Role,
				Content: msg.Content,
			})
		}
	} else {
		// Just add the last user message
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			newMessages = append(newMessages, history.Message{
				Role:    lastMsg.Role,
				Content: lastMsg.Content,
			})
		}
	}

	// Add assistant response
	newMessages = append(newMessages, history.Message{
		Role:    "assistant",
		Content: response,
	})

	conv.Messages = newMessages
	_, err = store.SaveConversation(conv)
	return err
}

func openStore() (*history.Store, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "history.db")
	return history.NewStore(dbPath)
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

	// Track conversation for history
	var conv *history.Conversation

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
				conv = nil
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
		tokens := make(chan string, util.DefaultChannelBuffer)
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
		responseContent := response.String()
		messages = append(messages, provider.Message{Role: "assistant", Content: responseContent})

		// Save to history
		if conv == nil {
			conv = &history.Conversation{
				Model:    getModel(),
				Provider: p.Name(),
			}
		}
		conv.Messages = []history.Message{
			{Role: "user", Content: input},
			{Role: "assistant", Content: responseContent},
		}

		if store, err := openStore(); err == nil {
			defer store.Close()
			if id, err := store.SaveConversation(conv); err == nil && conv.ID == 0 {
				conv.ID = id
			}
		}
	}
}

func printHelp() {
	fmt.Println(`Commands:
  /quit, /exit, /q  Exit interactive mode
  /new, /clear      Start a new conversation
  /model <name>     Switch model
  /help             Show this help`)
}
