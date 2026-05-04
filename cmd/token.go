package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/local/tutti/internal/api"
)

var tokenCmd = &cobra.Command{
	Use:   "token <query>",
	Short: "Show the URL token for a query (debug)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		q := strings.Join(args, " ")
		tok := api.BuildToken(q)
		fmt.Printf("Query: %q\n", q)
		fmt.Printf("Token: %s\n", tok)
		fmt.Printf("URL:   https://www.tutti.ch/de/q/suche/%s\n", tok)
		return nil
	},
}
