package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/devaloi/ask/internal/config"
	"github.com/devaloi/ask/internal/history"
	"github.com/devaloi/ask/internal/util"
)

var (
	searchFlag string
	limitFlag  int
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "List recent conversations",
	Long: `List recent conversations from the history.

Use --search to filter by content.
Use --limit to control how many results to show.`,
	RunE: runHistory,
}

func init() {
	rootCmd.AddCommand(historyCmd)
	historyCmd.Flags().StringVar(&searchFlag, "search", "", "Search conversations by content")
	historyCmd.Flags().IntVar(&limitFlag, "limit", util.DefaultHistoryLimit, "Maximum number of results")
}

func runHistory(cmd *cobra.Command, args []string) error {
	store, err := getStore()
	if err != nil {
		return fmt.Errorf("opening history store: %w", err)
	}
	defer store.Close()

	conversations, err := store.ListConversations(limitFlag, searchFlag)
	if err != nil {
		return fmt.Errorf("listing conversations: %w", err)
	}

	if len(conversations) == 0 {
		if searchFlag != "" {
			fmt.Printf("No conversations found matching '%s'\n", searchFlag)
		} else {
			fmt.Println("No conversations yet. Start chatting with: ask \"your question\"")
		}
		return nil
	}

	fmt.Println("ID    Model                  Date         Title")
	fmt.Println("----  ---------------------  -----------  ----------------------------------------")

	for _, conv := range conversations {
		date := conv.CreatedAt.Format("Jan 02 2006")
		model := util.Truncate(conv.Model, util.MaxModelDisplay)
		title := util.Truncate(conv.Title, util.MaxTitleDisplay)
		fmt.Printf("%-4d  %-21s  %-11s  %s\n", conv.ID, model, date, title)
	}

	return nil
}

func getStore() (*history.Store, error) {
	dataDir, err := config.GetDataDir()
	if err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dataDir, "history.db")
	return history.NewStore(dbPath)
}
