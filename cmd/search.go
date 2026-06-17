package cmd

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/local/tutti/internal/api"
	"github.com/local/tutti/internal/ascii"
	"github.com/local/tutti/internal/db"
	"github.com/local/tutti/internal/geo"
)

var (
	flagRegexp       string
	flagMinPrice     int
	flagMaxPrice     int
	flagLocation     string
	flagCategory     string
	flagSort         string
	flagLimit        int
	flagPages        int
	flagPageDelay    time.Duration
	flagSince        string
	flagNoSave       bool
	flagJSON         bool
	flagMD           bool
	flagWithPreviews bool
	flagRadiusKm     float64
	flagFromPostcode string
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search tutti.ch listings",
	Long: `Search tutti.ch for listings matching your query.

Examples:
  tutti search "iphone 15"
  tutti search "macbook pro" --min-price 500 --max-price 2000 --sort price_asc
  tutti search "velo" --regexp "carbon|trek|specialized" --location "Zürich"
  tutti search "möbel" --pages 3 --sort newest
  tutti search "velo" --from-postcode 8001 --radius-km 20
  tutti search "iphone" --from-postcode 3011 --radius-km 50 --sort newest
  tutti search "macbook" --pages 0                 # fetch every page
  tutti search "macbook" --since 2026-06-17T08:00:00Z  # only listings newer than this
  tutti search "macbook" --page-delay 2s            # 2s between page fetches`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")
		return runSearch(query)
	},
}

// Searcher is the subset of *api.Client used by runSearch. Defined as an
// interface so tests can substitute a fake without hitting the network.
type Searcher interface {
	Search(opts api.SearchOptions) (*api.SearchResult, error)
}

func init() {
	searchCmd.Flags().StringVar(&flagRegexp, "regexp", "", "Client-side regexp filter on title+body")
	searchCmd.Flags().IntVar(&flagMinPrice, "min-price", 0, "Minimum price in CHF")
	searchCmd.Flags().IntVar(&flagMaxPrice, "max-price", 0, "Maximum price in CHF")
	searchCmd.Flags().StringVar(&flagLocation, "location", "", "Location filter (e.g. 'Zürich', 'Bern')")
	searchCmd.Flags().StringVar(&flagCategory, "category", "", "Category ID filter")
	searchCmd.Flags().StringVar(&flagSort, "sort", "newest", "Sort: newest, oldest, price_asc, price_desc, relevance")
	searchCmd.Flags().IntVar(&flagLimit, "limit", 30, "Listings per page (max 100)")
	searchCmd.Flags().IntVar(&flagPages, "pages", 1, "Number of pages to fetch (0 = fetch all pages)")
	searchCmd.Flags().DurationVar(&flagPageDelay, "page-delay", 1*time.Second, "Delay between page fetches (0 disables; helps avoid rate limits when fetching many pages)")
	searchCmd.Flags().StringVar(&flagSince, "since", "", "Stop paging once listings are older than this RFC3339 timestamp (e.g. 2026-06-17T08:00:00Z). Requires a time-based sort (newest/oldest).")
	searchCmd.Flags().BoolVar(&flagNoSave, "no-save", false, "Don't save results to local database")
	searchCmd.Flags().BoolVar(&flagJSON, "json", false, "Output raw JSON")
	searchCmd.Flags().BoolVar(&flagMD, "md", false, "Output Markdown")
	searchCmd.Flags().BoolVar(&flagWithPreviews, "with-previews", false, "Fetch and render listing thumbnails as ASCII art")
	searchCmd.Flags().Float64Var(&flagRadiusKm, "radius-km", 0, "Maximum distance in km from --from-postcode (client-side haversine filter; default 0 = disabled)")
	searchCmd.Flags().StringVar(&flagFromPostcode, "from-postcode", "", "Origin 4-digit Swiss PLZ for --radius-km (e.g. 8001 for Zürich). Falls back to a per-canton centroid when the PLZ is unknown.")
}

func runSearch(query string) error {
	fmt.Fprintf(os.Stderr, "Connecting to tutti.ch...\n")
	client, err := api.NewClient()
	if err != nil {
		return fmt.Errorf("init client: %w", err)
	}

	return runSearchWith(query, client)
}

// runSearchWith is the testable inner loop of runSearch. It takes a Searcher
// (rather than constructing *api.Client) so tests can supply a fake.
func runSearchWith(query string, client Searcher) error {
	var rx *regexp.Regexp
	if flagRegexp != "" {
		var err error
		rx, err = regexp.Compile("(?i)" + flagRegexp)
		if err != nil {
			return fmt.Errorf("invalid regexp: %w", err)
		}
	}

	// Radius filter (client-side haversine from a Swiss PLZ).
	// --radius-km=0 (default) disables the filter; --from-postcode must be
	// supplied together with a positive --radius-km.
	var radius *geo.SearchRadius
	if flagRadiusKm > 0 {
		if flagFromPostcode == "" {
			return fmt.Errorf("--radius-km requires --from-postcode")
		}
		r, err := geo.NewSearchRadius(flagFromPostcode, flagRadiusKm)
		if err != nil {
			return fmt.Errorf("radius filter: %w", err)
		}
		if !r.Origin.Matched {
			// Use the (less precise) region-centroid fallback; warn but proceed.
			fmt.Fprintf(os.Stderr, "Warning: PLZ %q not found in swisstopo dataset; using %s as approximation (accuracy ±30 km).\n",
				flagFromPostcode, r.Origin.Name)
		}
		radius = &r
	} else if flagFromPostcode != "" {
		// Surface a helpful message — many users will pass the PLZ without km.
		fmt.Fprintf(os.Stderr, "Warning: --from-postcode given but --radius-km is 0; radius filter disabled.\n")
	}

	var database *db.DB
	var err error
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

	// --since parsing. Only meaningful for time-based sorts.
	var sinceTime time.Time
	sinceMode := flagSince != ""
	if sinceMode {
		t, err := time.Parse(time.RFC3339, flagSince)
		if err != nil {
			return fmt.Errorf("invalid --since (want RFC3339, e.g. 2006-01-02T15:04:05Z): %w", err)
		}
		sinceTime = t
		if flagSort != "" && flagSort != "newest" && flagSort != "oldest" {
			return fmt.Errorf("--since requires --sort newest or --sort oldest (got %q)", flagSort)
		}
	}

	maxPages := flagPages
	if maxPages == 0 {
		maxPages = 1<<31 - 1 // effectively unlimited; the empty-page check stops us
	}

	for page := 0; page < maxPages; page++ {
		// Throttle between page fetches to avoid rate limits. Skip on the
		// first page (nothing to wait for) and skip entirely when delay = 0.
		if page > 0 && flagPageDelay > 0 {
			fmt.Fprintf(os.Stderr, "  (sleeping %s before next page...)\n", flagPageDelay)
			time.Sleep(flagPageDelay)
		}
		fmt.Fprintf(os.Stderr, "Fetching page %d...\n", page+1)
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

		// --since cutoff: when sorting newest-first, the first listing on this
		// page is the newest. If it's already older than the cutoff, every
		// later listing (and every subsequent page) will be too — stop now.
		if sinceMode && (flagSort == "" || flagSort == "newest") && len(result.Listings) > 0 {
			if firstTS, err := time.Parse(time.RFC3339, result.Listings[0].Timestamp); err == nil {
				if !firstTS.After(sinceTime) {
					break
				}
			}
		}

		// --since with --sort oldest: stop when the first listing on the
		// page is already newer than the cutoff (we've moved past the window).
		if sinceMode && flagSort == "oldest" && len(result.Listings) > 0 {
			if firstTS, err := time.Parse(time.RFC3339, result.Listings[0].Timestamp); err == nil {
				if firstTS.After(sinceTime) {
					break
				}
			}
		}

		if len(result.Listings) < flagLimit {
			break
		}
	}

	// Apply client-side filters: regexp, price range, location, radius
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
		// radius filter (haversine from --from-postcode)
		if radius != nil && !radius.Keeps(l.PostcodeInformation.Postcode) {
			continue
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
		printMarkdown(query, filtered, totalCount, rx, stats, radius, flagWithPreviews)
		return nil
	}

	printResults(query, filtered, totalCount, rx, stats, radius, flagWithPreviews)
	return nil
}

func printResults(query string, listings []api.Listing, totalCount int, rx *regexp.Regexp, stats *db.PriceStats, radius *geo.SearchRadius, withPreviews bool) {
	rxNote := ""
	if rx != nil {
		rxNote = fmt.Sprintf(" (regexp filter: %s)", rx.String())
	}
	radiusNote := ""
	if radius != nil {
		radiusNote = fmt.Sprintf(" (%s)", radius.String())
	}
	sinceNote := ""
	if flagSince != "" {
		sinceNote = fmt.Sprintf(" (since %s)", flagSince)
	}
	fmt.Printf("tutti.ch search: %q%s%s%s\n", query, rxNote, radiusNote, sinceNote)
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
		locLine := fmt.Sprintf("    Location: %s  |  %s  |  Seller: %s", loc, age, l.SellerInfo.Alias)
		if radius != nil {
			if d := radius.DistanceKm(l.PostcodeInformation.Postcode); !math.IsInf(d, 1) {
				locLine += fmt.Sprintf("  |  %.1f km", d)
			}
		}
		fmt.Println(locLine)
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

func printMarkdown(query string, listings []api.Listing, totalCount int, rx *regexp.Regexp, stats *db.PriceStats, radius *geo.SearchRadius, withPreviews bool) {
	rxNote := ""
	if rx != nil {
		rxNote = fmt.Sprintf(" · regexp `%s`", rx.String())
	}
	radiusNote := ""
	if radius != nil {
		radiusNote = fmt.Sprintf(" · %s", radius.String())
	}
	sinceNote := ""
	if flagSince != "" {
		sinceNote = fmt.Sprintf(" · since `%s`", flagSince)
	}
	fmt.Printf("# tutti.ch — \"%s\"%s%s%s\n\n", query, rxNote, radiusNote, sinceNote)
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
		distLine := ""
		if radius != nil {
			if d := radius.DistanceKm(l.PostcodeInformation.Postcode); !math.IsInf(d, 1) {
				distLine = fmt.Sprintf(" · **%.1f km**", d)
			}
		}
		fmt.Printf("**Location:** %s  · **Age:** %s  · **Seller:** %s%s\n\n", loc, age, l.SellerInfo.Alias, distLine)

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
