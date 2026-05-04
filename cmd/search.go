package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/local/tutti/internal/api"
	"github.com/local/tutti/internal/ascii"
	"github.com/local/tutti/internal/db"
)

var (
	flagRegexp   string
	flagMinPrice int
	flagMaxPrice int
	flagLocation string
	flagCategory string
	flagSort     string
	flagLimit    int
	flagPages    int
	flagNoSave      bool
	flagJSON        bool
	flagMD          bool
	flagWithPreviews bool
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tutti.ch listings",
	Long: `Search tutti.ch for listings matching your query.

Examples:
  tutti search "iphone 15"
  tutti search "macbook pro" --min-price 500 --max-price 2000 --sort price_asc
  tutti search "velo" --regexp "carbon|trek|specialized" --location "Zürich"
  tutti search "möbel" --pages 3 --sort newest`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")
		return runSearch(query)
	},
}

func init() {
	searchCmd.Flags().StringVar(&flagRegexp, "regexp", "", "Client-side regexp filter on title+body")
	searchCmd.Flags().IntVar(&flagMinPrice, "min-price", 0, "Minimum price in CHF")
	searchCmd.Flags().IntVar(&flagMaxPrice, "max-price", 0, "Maximum price in CHF")
	searchCmd.Flags().StringVar(&flagLocation, "location", "", "Location filter (e.g. 'Zürich', 'Bern')")
	searchCmd.Flags().StringVar(&flagCategory, "category", "", "Category ID filter")
	searchCmd.Flags().StringVar(&flagSort, "sort", "newest", "Sort: newest, oldest, price_asc, price_desc, relevance")
	searchCmd.Flags().IntVar(&flagLimit, "limit", 30, "Listings per page (max 100)")
	searchCmd.Flags().IntVar(&flagPages, "pages", 1, "Number of pages to fetch")
	searchCmd.Flags().BoolVar(&flagNoSave, "no-save", false, "Don't save results to local database")
	searchCmd.Flags().BoolVar(&flagJSON, "json", false, "Output raw JSON")
	searchCmd.Flags().BoolVar(&flagMD, "md", false, "Output Markdown")
	searchCmd.Flags().BoolVar(&flagWithPreviews, "with-previews", false, "Fetch and render listing thumbnails as ASCII art")
}

func runSearch(query string) error {
	var rx *regexp.Regexp
	if flagRegexp != "" {
		var err error
		rx, err = regexp.Compile("(?i)" + flagRegexp)
		if err != nil {
			return fmt.Errorf("invalid regexp: %w", err)
		}
	}

	fmt.Fprintf(os.Stderr, "Connecting to tutti.ch...\n")
	client, err := api.NewClient()
	if err != nil {
		return fmt.Errorf("init client: %w", err)
	}

	var database *db.DB
	if !flagNoSave {
		database, err = db.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open database: %v\n", err)
		}
		if database != nil {
			defer database.Close()
		}
	}

	var allListings []api.Listing
	var totalCount int

	for page := 0; page < flagPages; page++ {
		fmt.Fprintf(os.Stderr, "Fetching page %d/%d...\n", page+1, flagPages)
		result, err := client.Search(api.SearchOptions{
			Query:    query,
			Category: flagCategory,
			MinPrice: flagMinPrice,
			MaxPrice: flagMaxPrice,
			Location: flagLocation,
			Sort:     flagSort,
			Limit:    flagLimit,
			Page:     page,
		})
		if err != nil {
			return fmt.Errorf("search error: %w", err)
		}
		if page == 0 {
			totalCount = result.TotalCount
		}
		allListings = append(allListings, result.Listings...)
		if len(result.Listings) < flagLimit {
			break
		}
	}

	// Apply client-side filters: regexp, price range, location
	filtered := make([]api.Listing, 0, len(allListings))
	for _, l := range allListings {
		// regexp
		if rx != nil && !rx.MatchString(l.Title) && !rx.MatchString(l.Body) {
			continue
		}
		// price range
		if flagMinPrice > 0 || flagMaxPrice > 0 {
			price := api.ParsePrice(l.FormattedPrice)
			if flagMinPrice > 0 && price < flagMinPrice {
				continue
			}
			if flagMaxPrice > 0 && price > flagMaxPrice && price > 0 {
				continue
			}
		}
		// location substring match
		if flagLocation != "" {
			loc := strings.ToLower(l.PostcodeInformation.LocationName + " " + l.PostcodeInformation.Canton.Name + " " + l.PostcodeInformation.Canton.ShortName)
			if !strings.Contains(loc, strings.ToLower(flagLocation)) {
				continue
			}
		}
		filtered = append(filtered, l)
	}

	// Save to database
	if database != nil {
		for _, l := range filtered {
			price := api.ParsePrice(l.FormattedPrice)
			loc := l.PostcodeInformation.LocationName
			canton := l.PostcodeInformation.Canton.ShortName
			u := api.ListingURL(l.SEOInformation.DESlug)
			_ = database.UpsertListing(db.StoredListing{
				ListingID: l.ListingID,
				Title:     l.Title,
				Price:     price,
				PriceStr:  l.FormattedPrice,
				Location:  loc,
				Canton:    canton,
				URL:       u,
				Query:     query,
			})
		}
	}

	// Price stats from database
	var stats *db.PriceStats
	if database != nil {
		stats, _ = database.PriceStatsForQuery(query)
	}

	if flagJSON {
		out := struct {
			Query      string        `json:"query"`
			TotalCount int           `json:"total_count"`
			Count      int           `json:"count"`
			Listings   []api.Listing `json:"listings"`
		}{query, totalCount, len(filtered), filtered}
		enc, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(enc))
		return nil
	}

	if flagMD {
		printMarkdown(query, filtered, totalCount, rx, stats, flagWithPreviews)
		return nil
	}

	printResults(query, filtered, totalCount, rx, stats, flagWithPreviews)
	return nil
}

func printResults(query string, listings []api.Listing, totalCount int, rx *regexp.Regexp, stats *db.PriceStats, withPreviews bool) {
	rxNote := ""
	if rx != nil {
		rxNote = fmt.Sprintf(" (regexp filter: %s)", rx.String())
	}
	fmt.Printf("tutti.ch search: %q%s\n", query, rxNote)
	fmt.Printf("Found %d total listings, showing %d\n", totalCount, len(listings))

	if stats != nil && stats.Count > 0 {
		fmt.Printf("\nPrice analysis (%d listings in DB for this query):\n", stats.Count)
		fmt.Printf("  Min: CHF %d  |  Median: CHF %d  |  Max: CHF %d\n", stats.Min, stats.Median, stats.Max)
		fmt.Printf("  Mean: CHF %d  |  25th%%: CHF %d  |  75th%%: CHF %d\n\n", stats.Mean, stats.P25, stats.P75)
	}

	sep := strings.Repeat("─", 80)
	for i, l := range listings {
		price := api.ParsePrice(l.FormattedPrice)
		priceStr := l.FormattedPrice
		if priceStr == "" {
			priceStr = "(no price)"
		}

		loc := l.PostcodeInformation.LocationName
		if c := l.PostcodeInformation.Canton.ShortName; c != "" {
			loc += " (" + c + ")"
		}

		age := ""
		if l.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, l.Timestamp); err == nil {
				age = formatAge(time.Since(t))
			}
		}

		priceCategory := ""
		if stats != nil && price > 0 {
			priceCategory = "  [" + stats.PriceCategory(price) + "]"
		}

		u := api.ListingURL(l.SEOInformation.DESlug)

		fmt.Printf("%s\n", sep)

		// ASCII preview
		if withPreviews {
			if src := l.Thumbnail.NormalRendition.Src; src != "" {
				if img, err := ascii.Fetch(src); err == nil {
					art := ascii.RenderColored(img, 72)
					// Indent each line by 4 spaces
					for _, line := range strings.Split(strings.TrimRight(art, "\n"), "\n") {
						fmt.Printf("    %s\n", line)
					}
				}
			}
		}

		fmt.Printf("[%d] %s\n", i+1, l.Title)
		fmt.Printf("    Price: %s%s\n", priceStr, priceCategory)
		fmt.Printf("    Location: %s  |  %s  |  Seller: %s\n", loc, age, l.SellerInfo.Alias)
		if l.Body != "" {
			body := strings.ReplaceAll(l.Body, "\n", " ")
			if len(body) > 120 {
				body = body[:117] + "..."
			}
			fmt.Printf("    %s\n", body)
		}
		if u != "" {
			fmt.Printf("    %s\n", u)
		}
	}
	if len(listings) > 0 {
		fmt.Printf("%s\n", sep)
	}
	if len(listings) == 0 {
		fmt.Println("No listings found.")
	}
}

func printMarkdown(query string, listings []api.Listing, totalCount int, rx *regexp.Regexp, stats *db.PriceStats, withPreviews bool) {
	rxNote := ""
	if rx != nil {
		rxNote = fmt.Sprintf(" · regexp `%s`", rx.String())
	}
	fmt.Printf("# tutti.ch — \"%s\"%s\n\n", query, rxNote)
	fmt.Printf("**%d** total results, showing **%d**\n\n", totalCount, len(listings))

	if stats != nil && stats.Count > 0 {
		fmt.Printf("## Price Analysis (%d listings in DB)\n\n", stats.Count)
		fmt.Printf("| Min | 25th%% | Median | Mean | 75th%% | Max |\n")
		fmt.Printf("|-----|--------|--------|------|--------|-----|\n")
		fmt.Printf("| CHF %d | CHF %d | CHF %d | CHF %d | CHF %d | CHF %d |\n\n",
			stats.Min, stats.P25, stats.Median, stats.Mean, stats.P75, stats.Max)
	}

	if len(listings) == 0 {
		fmt.Println("*No listings found.*")
		return
	}

	fmt.Printf("## Listings\n\n")
	for i, l := range listings {
		price := api.ParsePrice(l.FormattedPrice)
		priceStr := l.FormattedPrice
		if priceStr == "" {
			priceStr = "—"
		}

		loc := l.PostcodeInformation.LocationName
		if c := l.PostcodeInformation.Canton.ShortName; c != "" {
			loc += " (" + c + ")"
		}

		age := ""
		if l.Timestamp != "" {
			if t, err := time.Parse(time.RFC3339, l.Timestamp); err == nil {
				age = formatAge(time.Since(t))
			}
		}

		priceCategory := ""
		if stats != nil && price > 0 {
			priceCategory = " · " + stats.PriceCategory(price)
		}

		u := api.ListingURL(l.SEOInformation.DESlug)

		title := l.Title
		if u != "" {
			title = fmt.Sprintf("[%s](%s)", l.Title, u)
		}

		fmt.Printf("### %d. %s\n\n", i+1, title)

		// In markdown mode, embed thumbnail as an image link
		if withPreviews {
			if src := l.Thumbnail.NormalRendition.Src; src != "" {
				fmt.Printf("![thumbnail](%s)\n\n", src)
			}
		}

		fmt.Printf("**Price:** %s%s  \n", priceStr, priceCategory)
		fmt.Printf("**Location:** %s  · **Age:** %s  · **Seller:** %s\n\n", loc, age, l.SellerInfo.Alias)

		if l.Body != "" {
			body := strings.ReplaceAll(l.Body, "\n", " ")
			if len(body) > 200 {
				body = body[:197] + "..."
			}
			fmt.Printf("> %s\n\n", body)
		}
	}
}

func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dw ago", int(d.Hours()/24/7))
	}
}
