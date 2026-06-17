package db

import (
	"path/filepath"
	"testing"
	"time"
)

func newTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	d, err := OpenAt(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestOpenAt_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "test.sqlite")
	d, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt: %v", err)
	}
	defer d.Close()
	if _, err := d.db.Exec("SELECT 1"); err != nil {
		t.Errorf("db not usable: %v", err)
	}
}

func TestUpsertListing_InsertAndUpdate(t *testing.T) {
	d := newTestDB(t)
	l := StoredListing{
		ListingID: "L1",
		Title:     "iPhone 15",
		Price:     500,
		PriceStr:  "CHF 500.–",
		Location:  "Zürich",
		Canton:    "ZH",
		URL:       "https://example.com/L1",
		Query:     "iphone",
	}
	if err := d.UpsertListing(l); err != nil {
		t.Fatalf("insert: %v", err)
	}
	n, _ := d.CountListings()
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}

	// Second upsert: same listing, new price.
	l.Price = 450
	l.PriceStr = "CHF 450.–"
	if err := d.UpsertListing(l); err != nil {
		t.Fatalf("update: %v", err)
	}

	// First price should be in history; second price appended.
	h, err := d.PriceHistory("L1")
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 2 {
		t.Errorf("history len = %d, want 2", len(h))
	}
	if h[0].Price != 500 || h[1].Price != 450 {
		t.Errorf("history = %+v, want prices 500 then 450", h)
	}

	// Upsert with same price → no new history entry.
	if err := d.UpsertListing(l); err != nil {
		t.Fatal(err)
	}
	h, _ = d.PriceHistory("L1")
	if len(h) != 2 {
		t.Errorf("history grew without price change: len = %d, want 2", len(h))
	}
}

func TestUpsertListing_ZeroPriceNoHistory(t *testing.T) {
	d := newTestDB(t)
	l := StoredListing{
		ListingID: "L2", Title: "Free stuff", Price: 0, PriceStr: "Gratis",
		Query: "free",
	}
	if err := d.UpsertListing(l); err != nil {
		t.Fatal(err)
	}
	h, _ := d.PriceHistory("L2")
	if len(h) != 0 {
		t.Errorf("expected no history for price=0, got %d entries", len(h))
	}
}

func TestPriceStatsForQuery(t *testing.T) {
	d := newTestDB(t)
	prices := []int{100, 200, 300, 400, 500}
	for i, p := range prices {
		l := StoredListing{
			ListingID: id("S", i),
			Title:     "macbook",
			Price:     p,
			PriceStr:  "x",
			Query:     "macbook",
		}
		if err := d.UpsertListing(l); err != nil {
			t.Fatal(err)
		}
	}
	// Add a row with different query — should be excluded.
	_ = d.UpsertListing(StoredListing{
		ListingID: "other", Title: "iphone", Price: 999, PriceStr: "x", Query: "iphone",
	})
	// Add a row with price=0 — should be excluded.
	_ = d.UpsertListing(StoredListing{
		ListingID: "zero", Title: "macbook", Price: 0, PriceStr: "Gratis", Query: "macbook",
	})

	s, err := d.PriceStatsForQuery("macbook")
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 5 {
		t.Errorf("Count = %d, want 5", s.Count)
	}
	if s.Min != 100 || s.Max != 500 {
		t.Errorf("Min/Max = %d/%d, want 100/500", s.Min, s.Max)
	}
	if s.Median != 300 {
		t.Errorf("Median = %d, want 300", s.Median)
	}
	wantMean := 300
	if s.Mean != wantMean {
		t.Errorf("Mean = %d, want %d", s.Mean, wantMean)
	}
}

func TestPriceStatsForQuery_Empty(t *testing.T) {
	d := newTestDB(t)
	s, err := d.PriceStatsForQuery("nothing")
	if err != nil {
		t.Fatal(err)
	}
	if s.Count != 0 {
		t.Errorf("Count = %d, want 0", s.Count)
	}
}

func TestSearchHistory_LikeMatch(t *testing.T) {
	d := newTestDB(t)
	_ = d.UpsertListing(StoredListing{ListingID: "A", Title: "iPhone 15", Price: 500, PriceStr: "x", Query: "iphone 15"})
	_ = d.UpsertListing(StoredListing{ListingID: "B", Title: "MacBook Air", Price: 800, PriceStr: "x", Query: "macbook"})
	_ = d.UpsertListing(StoredListing{ListingID: "C", Title: "iPhone 14 case", Price: 10, PriceStr: "x", Query: "iphone 14"})

	out, err := d.SearchHistory("iphone")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Errorf("len(out) = %d, want 2", len(out))
	}
}

func TestSearchHistory_Empty(t *testing.T) {
	d := newTestDB(t)
	out, err := d.SearchHistory("")
	if err != nil {
		t.Fatal(err)
	}
	// Empty match returns nothing because of LIKE '%%' which should match all.
	// We don't strictly require any particular behaviour here, just no error.
	_ = out
}

func TestPriceCategory(t *testing.T) {
	s := &PriceStats{Count: 4, Min: 100, Max: 400, P25: 150, Median: 200, P75: 350, Mean: 250}

	cases := []struct {
		price int
		want  string
	}{
		{100, "very cheap"},
		{150, "very cheap"},
		{175, "below median"},
		{200, "below median"},
		{300, "above median"},
		{350, "above median"},
		{400, "expensive"},
		{500, "expensive"},
		{0, ""}, // price 0 → empty label
	}
	for _, c := range cases {
		got := s.PriceCategory(c.price)
		if !contains(got, c.want) {
			t.Errorf("PriceCategory(%d) = %q, want substring %q", c.price, got, c.want)
		}
	}

	// Zero-count stats returns empty.
	empty := &PriceStats{}
	if got := empty.PriceCategory(100); got != "" {
		t.Errorf("empty stats: got %q, want \"\"", got)
	}
}

func contains(s, sub string) bool {
	if sub == "" {
		return s == ""
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func id(prefix string, i int) string {
	return prefix + time.Duration(i).String()
}
