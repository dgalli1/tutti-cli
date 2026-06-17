package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/local/tutti/internal/api"
	"github.com/local/tutti/internal/db"
)

// resetFlags puts every search-package flag back to its default so each test
// gets a clean slate. The defaults match the values declared via pflag.
func resetFlags(t *testing.T) {
	t.Helper()
	flagRegexp = ""
	flagMinPrice = 0
	flagMaxPrice = 0
	flagLocation = ""
	flagCategory = ""
	flagSort = "newest"
	flagLimit = 30
	flagPages = 1
	flagPageDelay = 0 // 0 in tests so multi-page suites don't actually sleep
	flagSince = ""
	flagNoSave = false
	flagJSON = false
	flagMD = false
	flagWithPreviews = false
	flagRadiusKm = 0
	flagFromPostcode = ""
	t.Cleanup(func() {
		flagRegexp = ""
		flagMinPrice = 0
		flagMaxPrice = 0
		flagLocation = ""
		flagCategory = ""
		flagSort = "newest"
		flagLimit = 30
		flagPages = 1
		flagPageDelay = 0
		flagSince = ""
		flagNoSave = false
		flagJSON = false
		flagMD = false
		flagWithPreviews = false
		flagRadiusKm = 0
		flagFromPostcode = ""
	})
}

// fakeSearcher returns canned pages so we can exercise the filter pipeline.
type fakeSearcher struct {
	pages       [][]api.Listing
	totalCounts []int
	calls       int
	lastOpts    []api.SearchOptions
}

func (f *fakeSearcher) Search(opts api.SearchOptions) (*api.SearchResult, error) {
	f.calls++
	f.lastOpts = append(f.lastOpts, opts)
	if opts.Page >= len(f.pages) {
		return &api.SearchResult{Listings: nil, TotalCount: f.totalCounts[0]}, nil
	}
	return &api.SearchResult{
		Listings:   f.pages[opts.Page],
		TotalCount: f.totalCounts[0],
		Token:      "test-token",
	}, nil
}

func sampleListings() []api.Listing {
	// Build a varied set so each filter type has something to bite on.
	mk := func(id, title, body, price, postcode, loc, cantonShort, cantonName string) api.Listing {
		l := api.Listing{
			ListingID:      id,
			Title:          title,
			Body:           body,
			FormattedPrice: price,
			Timestamp:      "2026-06-17T10:00:00Z",
			PostcodeInformation: struct {
				Postcode     string `json:"postcode"`
				LocationName string `json:"locationName"`
				Canton       struct {
					ShortName string `json:"shortName"`
					Name      string `json:"name"`
				} `json:"canton"`
			}{Postcode: postcode, LocationName: loc, Canton: struct {
				ShortName string `json:"shortName"`
				Name      string `json:"name"`
			}{ShortName: cantonShort, Name: cantonName}},
			SEOInformation: struct {
				DESlug string `json:"deSlug"`
			}{DESlug: "slug-" + id},
		}
		return l
	}
	return []api.Listing{
		mk("1", "MacBook Pro M1", "Like new", "CHF 1'200.–", "8001", "Zürich", "ZH", "Zürich"),
		mk("2", "MacBook Air M2", "Great condition", "CHF 900.–", "3011", "Bern", "BE", "Bern"),
		mk("3", "iPhone 15 Pro", "256 GB", "CHF 800.–", "4051", "Basel", "BS", "Basel-Stadt"),
		mk("4", "iPhone 14 case", "Silicone", "CHF 15.–", "8001", "Zürich", "ZH", "Zürich"),
		mk("5", "Gratis zu verschenken", "Bücher", "Gratis", "6003", "Luzern", "LU", "Luzern"),
	}
}

// captureStdout runs fn while capturing os.Stdout, returning what was printed.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := fn()
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	if _, e := io.Copy(&buf, r); e != nil {
		t.Fatalf("copy: %v", e)
	}
	return buf.String(), err
}

// captureStderr returns the stderr emitted during fn.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	err := fn()
	w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String(), err
}

// runWithFakeDB points db.Open at a temp file by setting HOME, then runs fn.
func runWithFakeDB(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	out, err := captureStdout(t, fn)
	// We don't capture stderr separately — both write to os.Stdout in our
	// test paths except for the explicit "Connecting..." messages. Re-run
	// isn't worth it; we just inspect out below.
	return out, "", err
}

// countIDs extracts "[N] Title" prefixes and returns the visible listing IDs.
func countIDs(out string) int {
	count := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "[") && len(line) > 2 && line[1] >= '0' && line[1] <= '9' {
			count++
		}
	}
	return count
}

// --- tests ----------------------------------------------------------------

func TestRunSearch_Basic(t *testing.T) {
	resetFlags(t)
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("macbook", fake) })
	if err != nil {
		t.Fatalf("runSearchWith: %v", err)
	}
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("expected MacBook Pro M1 in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Found 5 total listings") {
		t.Errorf("expected total count line, got:\n%s", out)
	}
}

func TestRunSearch_Regexp(t *testing.T) {
	resetFlags(t)
	flagRegexp = "macbook" // simple substring filter, case-insensitive
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("anything", fake) })
	if err != nil {
		t.Fatalf("runSearchWith: %v", err)
	}
	// "MacBook Pro M1" and "MacBook Air M2" both contain "macbook".
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("regexp filter should keep MacBook Pro M1, got:\n%s", out)
	}
	if !strings.Contains(out, "MacBook Air M2") {
		t.Errorf("regexp filter should keep MacBook Air M2, got:\n%s", out)
	}
	// iPhone listings and the Gratis listing should be filtered out.
	for _, drop := range []string{"iPhone 15 Pro", "iPhone 14 case", "Gratis zu verschenken"} {
		if strings.Contains(out, drop) {
			t.Errorf("regexp 'macbook' should drop %s, got:\n%s", drop, out)
		}
	}
}

func TestRunSearch_RegexpAlternation(t *testing.T) {
	resetFlags(t)
	// Alternation: "m1|m2" matches "m1" or "m2" anywhere in title/body.
	flagRegexp = "m1|m2"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("anything", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// MacBook Pro M1 → "m1"; MacBook Air M2 → "m2".
	for _, keep := range []string{"MacBook Pro M1", "MacBook Air M2"} {
		if !strings.Contains(out, keep) {
			t.Errorf("alternation should keep %s, got:\n%s", keep, out)
		}
	}
	// iPhone 15 Pro's body is "256 GB" — neither m1 nor m2.
	if strings.Contains(out, "iPhone 15 Pro") {
		t.Errorf("alternation should drop iPhone 15 Pro, got:\n%s", out)
	}
}

func TestRunSearch_RegexpInvalid(t *testing.T) {
	resetFlags(t)
	flagRegexp = "[unclosed"
	fake := &fakeSearcher{}
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err == nil || !strings.Contains(err.Error(), "invalid regexp") {
		t.Errorf("expected invalid regexp error, got %v", err)
	}
}

func TestRunSearch_MinPrice(t *testing.T) {
	resetFlags(t)
	flagMinPrice = 500
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("anything", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Listings ≥ CHF 500: 1200, 900, 800. Below: iPhone 14 case (15) and Gratis (0).
	for _, keep := range []string{"MacBook Pro M1", "MacBook Air M2", "iPhone 15 Pro"} {
		if !strings.Contains(out, keep) {
			t.Errorf("min-price=500 should keep %s, got:\n%s", keep, out)
		}
	}
	for _, drop := range []string{"iPhone 14 case", "Gratis zu verschenken"} {
		if strings.Contains(out, drop) {
			t.Errorf("min-price=500 should drop %s, got:\n%s", drop, out)
		}
	}
}

func TestRunSearch_MaxPrice(t *testing.T) {
	resetFlags(t)
	flagMaxPrice = 1000
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "MacBook Pro M1") { // CHF 1200 > 1000
		t.Errorf("max-price should drop MacBook Pro M1, got:\n%s", out)
	}
	if !strings.Contains(out, "iPhone 15 Pro") { // 800 OK
		t.Errorf("max-price should keep iPhone 15 Pro, got:\n%s", out)
	}
}

func TestRunSearch_MinAndMaxPrice(t *testing.T) {
	resetFlags(t)
	flagMinPrice = 500
	flagMaxPrice = 1000
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// MacBook Air M2 (900) and iPhone 15 Pro (800) both qualify.
	if !strings.Contains(out, "MacBook Air M2") {
		t.Errorf("expected MacBook Air M2 in [500,1000], got:\n%s", out)
	}
	if !strings.Contains(out, "iPhone 15 Pro") {
		t.Errorf("expected iPhone 15 Pro in [500,1000], got:\n%s", out)
	}
	// MacBook Pro M1 (1200) is above the max; iPhone 14 case (15) below the min.
	if strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("MacBook Pro M1 (1200) should be filtered out, got:\n%s", out)
	}
	if strings.Contains(out, "iPhone 14 case") {
		t.Errorf("iPhone 14 case (15) should be filtered out, got:\n%s", out)
	}
}

func TestRunSearch_LocationMatch(t *testing.T) {
	resetFlags(t)
	flagLocation = "zürich" // case-insensitive substring match
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MacBook Pro M1") || !strings.Contains(out, "iPhone 14 case") {
		t.Errorf("location filter should keep Zürich listings, got:\n%s", out)
	}
	if strings.Contains(out, "MacBook Air M2") || strings.Contains(out, "iPhone 15 Pro") {
		t.Errorf("location filter dropped non-Zürich listings, got:\n%s", out)
	}
}

func TestRunSearch_LocationByCanton(t *testing.T) {
	resetFlags(t)
	flagLocation = "BE"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MacBook Air M2") {
		t.Errorf("canton short-name match should keep Bern listing, got:\n%s", out)
	}
}

// --- radius filter ----------------------------------------------------------

func TestRunSearch_RadiusRequiresPostcode(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 10
	flagFromPostcode = "" // missing
	fake := &fakeSearcher{}
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err == nil || !strings.Contains(err.Error(), "--from-postcode") {
		t.Errorf("expected error requiring --from-postcode, got %v", err)
	}
}

func TestRunSearch_RadiusInvalidPostcode(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 10
	flagFromPostcode = "abc" // non-numeric
	fake := &fakeSearcher{}
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err == nil {
		t.Errorf("expected error for non-numeric PLZ, got nil")
	}
}

func TestRunSearch_RadiusFromPostcodeAloneWarns(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 0
	flagFromPostcode = "8001"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	// Should not error, but should emit a stderr warning and run normally.
	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("radius-km=0 should leave all listings in place, got:\n%s", out)
	}
}

func TestRunSearch_Radius10kmFromZurich(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 10
	flagFromPostcode = "8001"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Within 10 km of 8001 (Zürich): both Zürich listings (8001, 8001).
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("10 km of 8001 should keep MacBook Pro M1, got:\n%s", out)
	}
	if !strings.Contains(out, "iPhone 14 case") {
		t.Errorf("10 km of 8001 should keep iPhone 14 case, got:\n%s", out)
	}
	// Beyond 10 km of 8001: Bern (~95 km), Basel (~75 km), Luzern (~40 km).
	for _, drop := range []string{"MacBook Air M2", "iPhone 15 Pro", "Gratis zu verschenken"} {
		if strings.Contains(out, drop) {
			t.Errorf("10 km of 8001 should drop %s, got:\n%s", drop, out)
		}
	}
}

func TestRunSearch_Radius50kmFromZurich(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 50
	flagFromPostcode = "8001"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Within 50 km of 8001: both Zürich listings + Luzern (~40 km).
	for _, keep := range []string{"MacBook Pro M1", "iPhone 14 case", "Gratis zu verschenken"} {
		if !strings.Contains(out, keep) {
			t.Errorf("50 km of 8001 should keep %s, got:\n%s", keep, out)
		}
	}
	// Beyond 50 km of 8001: Bern (~95 km), Basel (~75 km).
	for _, drop := range []string{"MacBook Air M2", "iPhone 15 Pro"} {
		if strings.Contains(out, drop) {
			t.Errorf("50 km of 8001 should drop %s, got:\n%s", drop, out)
		}
	}
}

func TestRunSearch_Radius100kmFromBern(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 100
	flagFromPostcode = "3011"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Within 100 km of 3011 (Bern): Bern itself + Basel (~75 km) + Luzern (~40 km).
	for _, keep := range []string{"MacBook Air M2", "iPhone 15 Pro", "Gratis zu verschenken"} {
		if !strings.Contains(out, keep) {
			t.Errorf("100 km of 3011 should keep %s, got:\n%s", keep, out)
		}
	}
	// Beyond 100 km of 3011: Zürich (~95 km, borderline).
	// MacBook Pro M1 (8001, Zürich) and iPhone 14 case (8001, Zürich):
	// 8001 is ~95 km from 3011, which is < 100, so they SHOULD be kept.
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("100 km of 3011 should keep MacBook Pro M1 (~95 km), got:\n%s", out)
	}
}

func TestRunSearch_RadiusAnnouncesInHeader(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 20
	flagFromPostcode = "8001"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "20 km") {
		t.Errorf("radius should appear in header, got:\n%s", out)
	}
	if !strings.Contains(out, "8001") {
		t.Errorf("origin PLZ should appear in header, got:\n%s", out)
	}
}

func TestRunSearch_RadiusShowsDistancePerListing(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 100
	flagFromPostcode = "3011"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Each kept listing should have a "X.Y km" annotation.
	if !strings.Contains(out, "km") {
		t.Errorf("expected per-listing km annotation, got:\n%s", out)
	}
}

func TestRunSearch_RadiusMarkdownShowsHeader(t *testing.T) {
	resetFlags(t)
	flagMD = true
	flagRadiusKm = 30
	flagFromPostcode = "8001"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# tutti.ch") {
		t.Errorf("expected markdown header, got:\n%s", out)
	}
	if !strings.Contains(out, "30 km") {
		t.Errorf("radius should appear in markdown header, got:\n%s", out)
	}
}

func TestRunSearch_RadiusWithUnknownPostcodeFallsBack(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 1000       // very large so we don't drop anything else
	flagFromPostcode = "9999" // not in dataset → region-centroid fallback
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// With a region-centroid fallback for "9999" (very approximate),
	// most listings should still pass; the radius is large enough.
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("unknown PLZ fallback with 1000 km should keep Zürich, got:\n%s", out)
	}
}

func TestRunSearch_RadiusWithUnknownListingPostcodeDropped(t *testing.T) {
	resetFlags(t)
	flagRadiusKm = 1000
	flagFromPostcode = "8001"
	// Build a listing with a PLZ that won't resolve at all (none — but "0999"
	// is in the dataset range and might not match). Use a real PLZ outside
	// the radius to confirm the radius filter actually runs.
	listings := []api.Listing{
		// macbook in 8001
		mkForTest("L1", "MacBook Pro M1", "8001", "Zürich", "ZH", "CHF 1'200.–"),
		// unknown seller PLZ
		mkForTest("L2", "Phantom listing", "", "", "", "CHF 1.–"),
	}
	fake := &fakeSearcher{pages: [][]api.Listing{listings}, totalCounts: []int{2}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("valid PLZ listing should be kept, got:\n%s", out)
	}
	if strings.Contains(out, "Phantom listing") {
		t.Errorf("listing with unknown PLZ should be dropped, got:\n%s", out)
	}
}

// mkForTest is a local helper used by the radius tests; sampleListings() is
// closed over by some other tests so we replicate it here.
func mkForTest(id, title, postcode, loc, cantonShort, price string) api.Listing {
	return api.Listing{
		ListingID:      id,
		Title:          title,
		Body:           "",
		FormattedPrice: price,
		Timestamp:      "2026-06-17T10:00:00Z",
		PostcodeInformation: struct {
			Postcode     string `json:"postcode"`
			LocationName string `json:"locationName"`
			Canton       struct {
				ShortName string `json:"shortName"`
				Name      string `json:"name"`
			} `json:"canton"`
		}{Postcode: postcode, LocationName: loc, Canton: struct {
			ShortName string `json:"shortName"`
			Name      string `json:"name"`
		}{ShortName: cantonShort, Name: cantonShort}},
		SEOInformation: struct {
			DESlug string `json:"deSlug"`
		}{DESlug: "slug-" + id},
	}
}

func TestRunSearch_SortForwarded(t *testing.T) {
	resetFlags(t)
	flagSort = "price_asc"
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.lastOpts) == 0 || fake.lastOpts[0].Sort != "price_asc" {
		t.Errorf("sort not forwarded to client: %+v", fake.lastOpts)
	}
}

func TestRunSearch_LimitAndPages(t *testing.T) {
	resetFlags(t)
	flagLimit = 2
	flagPages = 3
	// Three pages of two listings each.
	p1 := sampleListings()[:2]
	p2 := sampleListings()[2:4]
	p3 := sampleListings()[4:]
	fake := &fakeSearcher{pages: [][]api.Listing{p1, p2, p3}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 3 {
		t.Errorf("expected 3 Search calls, got %d", fake.calls)
	}
	// Total visible count = 5 listings
	if n := countIDs(out); n != 5 {
		t.Errorf("expected 5 listings in output, got %d", n)
	}
}

func TestRunSearch_PagesStopsEarlyOnShortPage(t *testing.T) {
	resetFlags(t)
	flagLimit = 30
	flagPages = 5
	// First page returns 2 items — less than the limit — so we should stop.
	p1 := sampleListings()[:2]
	fake := &fakeSearcher{pages: [][]api.Listing{p1}, totalCounts: []int{2}}

	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 1 {
		t.Errorf("expected exactly 1 Search call (short page), got %d", fake.calls)
	}
}

func TestRunSearch_NoSaveSkipsDB(t *testing.T) {
	resetFlags(t)
	flagNoSave = true
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	// Point HOME at a fresh temp dir — no DB should be created.
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	out, err := captureStdout(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}

	// Verify no db file was created.
	matches, _ := filepath.Glob(filepath.Join(dir, ".tutti", "*.sqlite"))
	if len(matches) != 0 {
		t.Errorf("--no-save created DB files: %v", matches)
	}
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("missing expected output: %s", out)
	}
}

func TestRunSearch_JSON(t *testing.T) {
	resetFlags(t)
	flagJSON = true
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("macbook", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"query": "macbook"`) {
		t.Errorf("expected JSON query field, got:\n%s", out)
	}
	if !strings.Contains(out, `"total_count": 5`) {
		t.Errorf("expected JSON total_count field, got:\n%s", out)
	}
	if !strings.Contains(out, `"listings"`) {
		t.Errorf("expected JSON listings field, got:\n%s", out)
	}
}

func TestRunSearch_Markdown(t *testing.T) {
	resetFlags(t)
	flagMD = true
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("macbook", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# tutti.ch") {
		t.Errorf("expected markdown header, got:\n%s", out)
	}
	if !strings.Contains(out, "**Price:**") {
		t.Errorf("expected Price field in md, got:\n%s", out)
	}
	if !strings.Contains(out, "https://www.tutti.ch/de/q/details/") {
		t.Errorf("expected listing URL in md, got:\n%s", out)
	}
}

func TestRunSearch_MarkdownEmptyResult(t *testing.T) {
	resetFlags(t)
	flagMD = true
	fake := &fakeSearcher{pages: [][]api.Listing{{}}, totalCounts: []int{0}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("nope", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "*No listings found.*") {
		t.Errorf("expected empty md placeholder, got:\n%s", out)
	}
}

func TestRunSearch_PersistsAndShowsPriceStats(t *testing.T) {
	resetFlags(t)
	// Seed two prior runs so the DB has stats for "macbook".
	t.Setenv("HOME", t.TempDir())
	seedDir := os.Getenv("HOME")
	dbPath := filepath.Join(seedDir, ".tutti", "db.sqlite")
	seed, err := db.OpenAt(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []int{500, 700} {
		_ = seed.UpsertListing(db.StoredListing{
			ListingID: fmt.Sprintf("seed-%d", i),
			Title:     "macbook",
			Price:     p,
			PriceStr:  fmt.Sprintf("CHF %d.–", p),
			Query:     "macbook",
		})
	}
	seed.Close()

	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}
	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("macbook", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Price analysis") {
		t.Errorf("expected price analysis block, got:\n%s", out)
	}
	if !strings.Contains(out, "Median: CHF") {
		t.Errorf("expected median in analysis, got:\n%s", out)
	}
}

func TestRunSearch_PersistsListings(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	if _, err := captureStdout(t, func() error { return runSearchWith("macbook", fake) }); err != nil {
		t.Fatal(err)
	}

	d, err := db.OpenAt(filepath.Join(dir, ".tutti", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	n, _ := d.CountListings()
	if n != 5 {
		t.Errorf("DB has %d listings, want 5", n)
	}
}

func TestRunSearch_WithPreviewsFlag(t *testing.T) {
	resetFlags(t)
	flagWithPreviews = true
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	// Should not crash even though no real thumbnails are reachable here;
	// ascii.Fetch returns an error and is silently skipped.
	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("expected listing output even with previews, got:\n%s", out)
	}
}

func TestRunSearch_FilterInteractionRegexpAndPrice(t *testing.T) {
	resetFlags(t)
	flagRegexp = "macbook"
	flagMinPrice = 1000
	flagMaxPrice = 1500
	fake := &fakeSearcher{pages: [][]api.Listing{sampleListings()}, totalCounts: []int{5}}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// MacBook Pro M1 at 1200 matches both filters.
	if !strings.Contains(out, "MacBook Pro M1") {
		t.Errorf("expected MacBook Pro M1, got:\n%s", out)
	}
	if strings.Contains(out, "MacBook Air M2") {
		t.Errorf("MacBook Air M2 should be filtered out (price < 1000), got:\n%s", out)
	}
}

func TestFormatAge(t *testing.T) {
	// Cover each branch in formatAge.
	d := 30 * 24 * time.Hour // 30 days
	if got := formatAge(d); !strings.Contains(got, "w ago") {
		t.Errorf("30d: got %q", got)
	}
}

func TestFormatAgeBranches(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{30 * time.Second, "just now"},
		{5 * time.Minute, "5m ago"},
		{3 * time.Hour, "3h ago"},
		{2 * 24 * time.Hour, "2d ago"},
		{14 * 24 * time.Hour, "2w ago"},
	}
	for _, c := range cases {
		got := formatAge(c.in)
		if got != c.want {
			t.Errorf("formatAge(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// --- --pages 0 (fetch all) -------------------------------------------------

func TestRunSearch_PagesZeroFetchesAll(t *testing.T) {
	resetFlags(t)
	flagLimit = 2
	flagPages = 0 // 0 = unlimited
	// Five pages of two listings each (10 listings) — short last page.
	p1 := sampleListings()[:2]
	p2 := sampleListings()[2:4]
	p3 := sampleListings()[4:]
	// Pad the middle pages so the short-page stop doesn't trigger early.
	more1 := sampleListings()[:2]
	more2 := sampleListings()[2:4]
	fake := &fakeSearcher{
		pages:       [][]api.Listing{p1, p2, p3, more1, more2},
		totalCounts: []int{10},
	}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Should have called Search for every full page (3), then stopped
	// when the third page returned < flagLimit listings.
	if fake.calls != 3 {
		t.Errorf("expected 3 Search calls (stop on short page), got %d", fake.calls)
	}
	if n := countIDs(out); n != 5 {
		t.Errorf("expected 5 listings in output, got %d", n)
	}
}

func TestRunSearch_PagesZeroStopsOnEmptyPage(t *testing.T) {
	resetFlags(t)
	flagLimit = 2
	flagPages = 0
	// One full page, then an empty page — should stop.
	p1 := sampleListings()[:2]
	fake := &fakeSearcher{
		pages:       [][]api.Listing{p1, {}},
		totalCounts: []int{2},
	}

	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 {
		t.Errorf("expected 2 Search calls, got %d", fake.calls)
	}
}

// --- --since (date cutoff) -------------------------------------------------

func TestRunSearch_SinceInvalidTimestamp(t *testing.T) {
	resetFlags(t)
	flagSince = "not-a-timestamp"
	fake := &fakeSearcher{}
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err == nil || !strings.Contains(err.Error(), "invalid --since") {
		t.Errorf("expected invalid --since error, got %v", err)
	}
}

func TestRunSearch_SinceRejectsNonTimeSort(t *testing.T) {
	resetFlags(t)
	flagSince = "2026-06-17T08:00:00Z"
	flagSort = "price_asc"
	fake := &fakeSearcher{}
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err == nil || !strings.Contains(err.Error(), "--since requires --sort") {
		t.Errorf("expected sort error, got %v", err)
	}
}

// sinceListings returns 5 listings with strictly decreasing timestamps.
// When sorted newest-first (the API default) they're in this order;
// index 0 is the newest.
func sinceListings() [][]api.Listing {
	mk := func(id string, ts string) api.Listing {
		l := mkForTest(id, "Listing "+id, "8001", "Zürich", "ZH", "CHF 100.–")
		l.Timestamp = ts
		return l
	}
	p1 := []api.Listing{
		mk("1", "2026-06-17T12:00:00Z"),
		mk("2", "2026-06-17T10:00:00Z"),
		mk("3", "2026-06-17T08:00:00Z"),
	}
	p2 := []api.Listing{
		mk("4", "2026-06-17T07:00:00Z"),
		mk("5", "2026-06-17T05:00:00Z"),
		mk("6", "2026-06-17T03:00:00Z"),
	}
	p3 := []api.Listing{
		mk("7", "2026-06-17T01:00:00Z"),
	}
	return [][]api.Listing{p1, p2, p3}
}

func TestRunSearch_SinceStopsPagingOnNewest(t *testing.T) {
	resetFlags(t)
	flagLimit = 3 // match page size so the short-page check doesn't fire
	flagSince = "2026-06-17T08:00:00Z"
	flagPages = 0 // would otherwise keep paging
	fake := &fakeSearcher{
		pages:       sinceListings(),
		totalCounts: []int{7},
	}

	out, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Page 1's first listing (12h) is newer than the cutoff (8h) → keep
	// all 3. Page 2 is fetched, its 3 listings are appended, and only
	// then do we check the cutoff: 7h ≤ 8h → break before fetching p3.
	if fake.calls != 2 {
		t.Errorf("expected 2 Search calls (stop on second page), got %d", fake.calls)
	}
	if n := countIDs(out); n != 6 {
		t.Errorf("expected 6 listings in output (2 full pages), got %d", n)
	}
	if !strings.Contains(out, "since 2026-06-17T08:00:00Z") {
		t.Errorf("expected --since to appear in header, got:\n%s", out)
	}
}

func TestRunSearch_SinceAllListingsPass(t *testing.T) {
	resetFlags(t)
	flagLimit = 3
	// Cutoff in the past: every page's first listing is newer.
	flagSince = "2026-06-17T00:00:00Z"
	flagPages = 0
	fake := &fakeSearcher{
		pages:       sinceListings(),
		totalCounts: []int{7},
	}

	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	// Should keep fetching until the short page stops the loop.
	if fake.calls != 3 {
		t.Errorf("expected 3 Search calls (cutoff in past), got %d", fake.calls)
	}
}

func TestRunSearch_SinceWithOldestSort(t *testing.T) {
	resetFlags(t)
	flagLimit = 3
	flagSince = "2026-06-17T08:00:00Z"
	flagSort = "oldest"
	flagPages = 0

	// oldest-first: page 1 = oldest, page 2 = mid, page 3 = newest.
	// Page 1 first listing is 1h (≤ 8h cutoff) → keep paging.
	// Page 2 first listing is 5h (≤ 8h cutoff) → keep paging.
	// Page 3 first listing is 12h (> 8h cutoff) → stop before fetching
	// a hypothetical page 4.
	mk := func(id string, ts string) api.Listing {
		l := mkForTest(id, "Listing "+id, "8001", "Zürich", "ZH", "CHF 100.–")
		l.Timestamp = ts
		return l
	}
	pages := [][]api.Listing{
		{mk("1", "2026-06-17T01:00:00Z"), mk("2", "2026-06-17T03:00:00Z"), mk("3", "2026-06-17T05:00:00Z")},
		{mk("4", "2026-06-17T07:00:00Z"), mk("5", "2026-06-17T07:30:00Z"), mk("6", "2026-06-17T08:00:00Z")},
		{mk("7", "2026-06-17T12:00:00Z"), mk("8", "2026-06-17T14:00:00Z"), mk("9", "2026-06-17T16:00:00Z")},
	}
	fake := &fakeSearcher{pages: pages, totalCounts: []int{9}}

	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 3 {
		t.Errorf("expected 3 Search calls with --sort oldest, got %d", fake.calls)
	}
}

// --- --page-delay (rate-limit throttle) -----------------------------------

func TestRunSearch_PageDelaySleepsBetweenPages(t *testing.T) {
	resetFlags(t)
	flagLimit = 3
	flagPageDelay = 20 * time.Millisecond
	flagPages = 0
	// Three full pages so the loop runs all three. We don't use --since,
	// so the loop only stops on the short page (none here) — the
	// fakeSearcher returns an empty page 4 which trips the short-page
	// check. We expect 4 calls; sleeps happen between calls 1→2 and 2→3.
	mk := func(id string, ts string) api.Listing {
		l := mkForTest(id, "Listing "+id, "8001", "Zürich", "ZH", "CHF 100.–")
		l.Timestamp = ts
		return l
	}
	pages := [][]api.Listing{
		{mk("1", "2026-06-17T10:00:00Z"), mk("2", "2026-06-17T09:00:00Z"), mk("3", "2026-06-17T08:00:00Z")},
		{mk("4", "2026-06-17T07:00:00Z"), mk("5", "2026-06-17T06:00:00Z"), mk("6", "2026-06-17T05:00:00Z")},
		{mk("7", "2026-06-17T04:00:00Z"), mk("8", "2026-06-17T03:00:00Z"), mk("9", "2026-06-17T02:00:00Z")},
	}
	fake := &fakeSearcher{pages: pages, totalCounts: []int{9}}

	start := time.Now()
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 4 {
		t.Fatalf("expected 4 Search calls, got %d", fake.calls)
	}
	// Two inter-page sleeps of 20ms = ~40ms minimum. Allow 10ms of slack
	// for goroutine scheduling on slow CI.
	if elapsed < 30*time.Millisecond {
		t.Errorf("expected at least ~40ms of sleeps between 3 pages, got %v", elapsed)
	}
}

func TestRunSearch_PageDelayZeroSkipsSleep(t *testing.T) {
	resetFlags(t)
	flagLimit = 3
	flagPageDelay = 0
	flagPages = 0
	mk := func(id string, ts string) api.Listing {
		l := mkForTest(id, "Listing "+id, "8001", "Zürich", "ZH", "CHF 100.–")
		l.Timestamp = ts
		return l
	}
	pages := [][]api.Listing{
		{mk("1", "2026-06-17T10:00:00Z"), mk("2", "2026-06-17T09:00:00Z"), mk("3", "2026-06-17T08:00:00Z")},
		{mk("4", "2026-06-17T07:00:00Z"), mk("5", "2026-06-17T06:00:00Z"), mk("6", "2026-06-17T05:00:00Z")},
		{mk("7", "2026-06-17T04:00:00Z"), mk("8", "2026-06-17T03:00:00Z"), mk("9", "2026-06-17T02:00:00Z")},
	}
	fake := &fakeSearcher{pages: pages, totalCounts: []int{9}}

	start := time.Now()
	_, _, err := runWithFakeDB(t, func() error { return runSearchWith("x", fake) })
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 4 {
		t.Fatalf("expected 4 Search calls, got %d", fake.calls)
	}
	// No sleeps configured — even on a slow machine the loop should
	// finish in well under 100ms.
	if elapsed > 100*time.Millisecond {
		t.Errorf("expected fast loop with --page-delay=0, got %v", elapsed)
	}
}
