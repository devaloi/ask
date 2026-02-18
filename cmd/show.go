package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Display a conversation",
	Long:  `Display the full conversation history for a given conversation ID.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	rootCmd.AddCommand(showCmd)
}

func runShow(cmd *cobra.Command, args []string) error {
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("invalid conversation ID: %s", args[0])
	}

	store, err := getStore()
	if err != nil {
		return fmt.Errorf("opening history store: %w", err)
	}
	defer store.Close()

	conv, err := store.GetConversation(id)
	if err != nil {
		return fmt.Errorf("loading conversation %d: %w", id, err)
	}

	fmt.Printf("Conversation #%d: %s\n", conv.ID, conv.Title)
	fmt.Printf("Model: %s | Provider: %s | Date: %s\n",
		conv.Model, conv.Provider, conv.CreatedAt.Format("Jan 02 2006 15:04"))
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println()

	for _, msg := range conv.Messages {
		if msg.Role == "system" {
			continue // Skip system messages in display
		}

		roleLabel := "You"
		if msg.Role == "assistant" {
			roleLabel = "Assistant"
		}

		fmt.Printf("[%s]\n", roleLabel)
		fmt.Println(msg.Content)
		fmt.Println()
	}

	return nil
}
