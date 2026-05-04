package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/local/tutti/internal/db"
)

var historyCmd = &cobra.Command{
	Use:   "history [query]",
	Short: "Show price history from local database",
	Long: `Browse your local database of previously seen listings.

Examples:
  tutti history                     # show all tracked listings
  tutti history "iphone"            # show listings matching "iphone"
  tutti history --stats "macbook"   # show price stats for macbook`,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := ""
		if len(args) > 0 {
			q = strings.Join(args, " ")
		}
		return runHistory(q)
	},
}

var flagStats bool

func init() {
	historyCmd.Flags().BoolVar(&flagStats, "stats", false, "Show only price stats, not individual listings")
}

func runHistory(query string) error {
	database, err := db.Open()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	total, _ := database.CountListings()
	fmt.Printf("Local database: %d listings tracked\n\n", total)

	if flagStats && query != "" {
		stats, err := database.PriceStatsForQuery(query)
		if err != nil {
			return err
		}
		if stats.Count == 0 {
			fmt.Printf("No price data for query %q\n", query)
			return nil
		}
		fmt.Printf("Price stats for %q (%d listings):\n", query, stats.Count)
		fmt.Printf("  Min:    CHF %d\n", stats.Min)
		fmt.Printf("  25th%%:  CHF %d\n", stats.P25)
		fmt.Printf("  Median: CHF %d\n", stats.Median)
		fmt.Printf("  Mean:   CHF %d\n", stats.Mean)
		fmt.Printf("  75th%%:  CHF %d\n", stats.P75)
		fmt.Printf("  Max:    CHF %d\n", stats.Max)
		return nil
	}

	listings, err := database.SearchHistory(query)
	if err != nil {
		return err
	}
	if len(listings) == 0 {
		fmt.Println("No listings found in database.")
		return nil
	}

	sep := strings.Repeat("─", 80)
	for _, l := range listings {
		fmt.Printf("%s\n", sep)
		fmt.Printf("%s\n", l.Title)
		fmt.Printf("  Price: %s  |  %s (%s)\n", l.PriceStr, l.Location, l.Canton)
		fmt.Printf("  Query: %q  |  Seen %d times  |  Last: %s\n",
			l.Query, l.SeenCount, l.LastSeen.Format("2006-01-02"))
		if l.URL != "" {
			fmt.Printf("  %s\n", l.URL)
		}

		history, _ := database.PriceHistory(l.ListingID)
		if len(history) > 1 {
			fmt.Printf("  Price history:")
			for _, h := range history {
				fmt.Printf("  %s → %s", h.RecordedAt.Format("Jan 02"), h.PriceStr)
			}
			fmt.Println()
		}
	}
	fmt.Printf("%s\n", sep)
	return nil
}
