package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)


const (
	baseURL    = "https://www.tutti.ch"
	graphqlURL = "https://www.tutti.ch/api/v10/graphql"
	userAgent  = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
)

type Client struct {
	http      *http.Client
	buildID   string
	csrfToken string
}

func NewClient() (*Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &Client{
		http: &http.Client{
			Jar:     jar,
			Timeout: 30 * time.Second,
		},
	}
	if err := c.bootstrap(); err != nil {
		return nil, fmt.Errorf("bootstrap failed: %w", err)
	}
	return c, nil
}

func (c *Client) bootstrap() error {
	req, _ := http.NewRequest("GET", baseURL+"/de/q", nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "de")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Extract buildId
	re := regexp.MustCompile(`"buildId":"([^"]+)"`)
	if m := re.FindSubmatch(body); m != nil {
		c.buildID = string(m[1])
	}

	// Extract CSRF token from cookies
	u, _ := url.Parse(baseURL)
	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == "tutti_csrftoken" {
			c.csrfToken = cookie.Value
			break
		}
	}

	if c.buildID == "" {
		return fmt.Errorf("could not extract buildId from tutti.ch")
	}
	return nil
}

// rol2 rotates byte left by 2 bits
func rol2(b byte) byte {
	return (b << 2) | (b >> 6)
}

// BuildToken encodes a query string into the tutti.ch URL token format.
func BuildToken(query string) string {
	if query == "" {
		// All-listings token
		return "Ak8DAlMDAwMA"
	}
	q := []byte(query)
	n := len(q)

	// [0x02, 0x4E, length_byte, ...encoded_chars..., 0x02, 0x53, 0x03, 0x03, 0x03, 0x00]
	buf := make([]byte, 0, 3+n+6)
	buf = append(buf, 0x02, 0x4E)
	// length byte
	buf = append(buf, rol2(byte(n))|0x81)
	// encode chars
	for i, ch := range q {
		if i == n-1 {
			buf = append(buf, rol2(ch|0x80))
		} else {
			buf = append(buf, rol2(ch))
		}
	}
	buf = append(buf, 0x02, 0x53, 0x03, 0x03, 0x03, 0x00)
	return base64.RawURLEncoding.EncodeToString(buf)
}

type SearchOptions struct {
	Query    string
	Category string
	MinPrice int // CHF, 0 = no limit
	MaxPrice int // CHF, 0 = no limit
	Location string
	Sort     string // "newest", "relevance", "price_asc", "price_desc"
	Limit    int
	Page     int
}

type SearchResult struct {
	Listings   []Listing
	TotalCount int
	Token      string
}

func (c *Client) Search(opts SearchOptions) (*SearchResult, error) {
	if opts.Limit == 0 {
		opts.Limit = 30
	}
	if opts.Sort == "" {
		opts.Sort = "newest"
	}

	// Try GraphQL first
	result, err := c.searchGraphQL(opts)
	if err != nil {
		// Only fall back to _next/data on Cloudflare blocks (not constraint/schema errors)
		if strings.Contains(err.Error(), "cloudflare") {
			result, err = c.searchNextData(opts)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	return result, nil
}

var gqlQuery = `query SearchListingsByConstraints($query: String, $constraints: ListingSearchConstraints, $category: ID, $first: Int!, $offset: Int!, $sort: ListingSortMode!, $direction: SortDirection!) {
  searchListingsByQuery(query: $query, constraints: $constraints, category: $category) {
    listings(first: $first, offset: $offset, sort: $sort, direction: $direction) {
      totalCount
      edges { node {
        listingID title body
        postcodeInformation { postcode locationName canton { shortName name } }
        timestamp formattedPrice formattedSource highlighted
        primaryCategory { categoryID }
        sellerInfo { alias }
        images(first: 1) { __typename }
        thumbnail { normalRendition: rendition(width: 235, height: 167) { src } }
        seoInformation { deSlug: slug(language: DE) }
      }}
    }
    searchToken
    query
  }
}`

func sortToGQL(sort string) (string, string) {
	switch sort {
	case "newest":
		return "TIMESTAMP", "DESCENDING"
	case "oldest":
		return "TIMESTAMP", "ASCENDING"
	case "price_asc":
		return "PRICE", "ASCENDING"
	case "price_desc":
		return "PRICE", "DESCENDING"
	default:
		return "TIMESTAMP", "DESCENDING"
	}
}

func (c *Client) searchGraphQL(opts SearchOptions) (*SearchResult, error) {
	sortMode, direction := sortToGQL(opts.Sort)

	// Price and location filtering is done client-side; constraints = nil avoids schema errors
	var constraints interface{} = nil

	vars := map[string]interface{}{
		"query":       opts.Query,
		"constraints": constraints,
		"category":    nil,
		"first":       opts.Limit,
		"offset":      opts.Page * opts.Limit,
		"sort":        sortMode,
		"direction":   direction,
	}
	if opts.Category != "" {
		vars["category"] = opts.Category
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"operationName": "SearchListingsByConstraints",
		"query":         gqlQuery,
		"variables":     vars,
	})

	req, _ := http.NewRequest("POST", graphqlURL, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Language", "de")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("x-csrf-token", c.csrfToken)
	req.Header.Set("x-tutti-client-identifier", "web/1.0.0+env-live.git-6011c7b4")
	req.Header.Set("x-tutti-hash", "558d27a1-b808-4d7a-8a58-45004afe86ea")
	req.Header.Set("x-tutti-source", "web r1.0-2026-05-04-10-53")
	req.Header.Set("Referer", baseURL+"/de/q")
	req.Header.Set("Origin", baseURL)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		// Check if it's an HTML Cloudflare page
		if bytes.Contains(body, []byte("<html")) {
			return nil, fmt.Errorf("cloudflare blocked (status %d)", resp.StatusCode)
		}
		return nil, fmt.Errorf("graphql error %d: %s", resp.StatusCode, string(body[:min(200, len(body))]))
	}

	var sr SearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}
	if len(sr.Errors) > 0 {
		return nil, fmt.Errorf("graphql errors: %v", sr.Errors[0].Message)
	}

	listings := make([]Listing, 0, len(sr.Data.SearchListingsByQuery.Listings.Edges))
	for _, e := range sr.Data.SearchListingsByQuery.Listings.Edges {
		listings = append(listings, e.Node)
	}
	return &SearchResult{
		Listings:   listings,
		TotalCount: sr.Data.SearchListingsByQuery.Listings.TotalCount,
		Token:      sr.Data.SearchListingsByQuery.SearchToken,
	}, nil
}

func (c *Client) searchNextData(opts SearchOptions) (*SearchResult, error) {
	token := BuildToken(opts.Query)

	sortParam := "newest"
	if opts.Sort == "price_asc" || opts.Sort == "price_desc" {
		sortParam = opts.Sort
	} else if opts.Sort == "oldest" {
		sortParam = "oldest"
	}

	page := opts.Page + 1 // _next/data is 1-indexed
	nextURL := fmt.Sprintf("%s/_next/data/%s/de/q/suche/%s.json?lang=de&slug=suche&slug=%s&sorting=%s&page=%d",
		baseURL, c.buildID, token, token, sortParam, page)

	if opts.Query != "" {
		nextURL += "&query=" + url.QueryEscape(opts.Query)
	}

	req, _ := http.NewRequest("GET", nextURL, nil)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept-Language", "de")
	req.Header.Set("Referer", baseURL+"/de/q")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("_next/data error %d", resp.StatusCode)
	}

	var nd NextDataResponse
	if err := json.NewDecoder(resp.Body).Decode(&nd); err != nil {
		return nil, fmt.Errorf("decode next data: %w", err)
	}

	// Find the SearchListingsByConstraints query in dehydratedState
	for _, q := range nd.PageProps.DehydratedState.Queries {
		slbq := q.State.Data.SearchListingsByQuery
		if slbq.Listings.TotalCount > 0 || len(slbq.Listings.Edges) > 0 {
			listings := make([]Listing, 0, len(slbq.Listings.Edges))
			for _, e := range slbq.Listings.Edges {
				listings = append(listings, e.Node)
			}
			return &SearchResult{
				Listings:   listings,
				TotalCount: slbq.Listings.TotalCount,
				Token:      slbq.SearchToken,
			}, nil
		}
	}
	return nil, fmt.Errorf("no listings found in _next/data response")
}

// ParsePrice extracts the integer CHF price from formatted strings like "CHF 1'200.–" or "CHF 50.–"
func ParsePrice(formatted string) int {
	if formatted == "" || strings.Contains(strings.ToLower(formatted), "gratis") {
		return 0
	}
	// Remove currency, apostrophes, dots, dashes
	re := regexp.MustCompile(`[^\d]`)
	digits := re.ReplaceAllString(formatted, "")
	if digits == "" {
		return 0
	}
	// The last two digits are cents – but tutti mostly shows whole CHF
	// "CHF 1'200.–" → digits = "1200" → CHF 1200
	v, _ := strconv.Atoi(digits)
	return v
}

// ListingURL returns the full tutti.ch URL for a listing
func ListingURL(slug string) string {
	if slug == "" {
		return ""
	}
	return baseURL + "/de/q/details/" + slug
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
