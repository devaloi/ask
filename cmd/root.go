// Package cmd implements the CLI commands for ask.
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/devaloi/ask/internal/config"
)

var (
	cfg *config.Config

	// Global flags
	providerFlag string
	modelFlag    string
	systemFlag   string
)

var rootCmd = &cobra.Command{
	Use:   "ask [prompt]",
	Short: "Chat with LLMs from your terminal",
	Long: `ask is a fast, pipe-friendly CLI for chatting with LLMs.

Supports OpenAI (GPT-4o) and Anthropic (Claude) with real streaming,
conversation history, and system prompts.

Examples:
  ask "What is a goroutine?"
  cat error.log | ask "What's wrong here?"
  ask -p anthropic "Explain this code"
  ask -s "Be concise" "Review this function"
  ask                             # interactive mode

Configuration:
  Config file: ~/.config/ask/config.yaml
  Environment: OPENAI_API_KEY, ANTHROPIC_API_KEY, ASK_PROVIDER, ASK_MODEL`,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE:          runChat,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&providerFlag, "provider", "p", "", "LLM provider (openai, anthropic)")
	rootCmd.PersistentFlags().StringVarP(&modelFlag, "model", "m", "", "Model to use")
	rootCmd.PersistentFlags().StringVarP(&systemFlag, "system", "s", "", "System prompt (or @filepath)")
}

func initConfig() {
	var err error
	cfg, err = config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: config load failed: %v, using defaults\n", err)
		cfg = config.DefaultConfig()
	}
}

// getProvider returns the provider name to use, applying flag/env/config precedence.
func getProvider() string {
	if providerFlag != "" {
		return providerFlag
	}
	return cfg.DefaultProvider
}

// getModel returns the model to use, applying flag/env/config precedence.
func getModel() string {
	if modelFlag != "" {
		return modelFlag
	}
	return cfg.DefaultModel
}
