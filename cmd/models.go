package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/devaloi/ask/internal/provider"
)

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List available models for each provider",
	RunE:  runModels,
}

func init() {
	rootCmd.AddCommand(modelsCmd)
}

func runModels(cmd *cobra.Command, args []string) error {
	defaultProvider := getProvider()
	defaultModel := getModel()

	providers := []string{"openai", "anthropic"}

	for _, name := range providers {
		p, err := provider.New(name, cfg)
		if err != nil {
			fmt.Printf("%s: (not configured)\n", name)
			continue
		}

		models := p.Models()
		fmt.Printf("%s:\n", name)
		for _, m := range models {
			marker := "  "
			if name == defaultProvider && m == defaultModel {
				marker = "* "
			}
			fmt.Printf("  %s%s\n", marker, m)
		}
		fmt.Println()
	}

	return nil
}
