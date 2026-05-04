package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tutti",
	Short: "tutti.ch classified ad search with local price tracking",
	Long: `tutti searches tutti.ch with advanced filtering, regexp matching,
and tracks listing prices in a local SQLite database for comparison over time.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(searchCmd)
	rootCmd.AddCommand(historyCmd)
	rootCmd.AddCommand(tokenCmd)
}
