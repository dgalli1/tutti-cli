package cmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/local/tutti/internal/db"
)

func TestRunHistory_EmptyDB(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	flagStats = false
	t.Cleanup(func() { flagStats = false })

	out, err := captureStdout(t, func() error { return runHistory("anything") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "0 listings tracked") {
		t.Errorf("expected 0 listings line, got: %s", out)
	}
	if !strings.Contains(out, "No listings found") {
		t.Errorf("expected no listings found message, got: %s", out)
	}
}

func TestRunHistory_WithListings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	flagStats = false
	t.Cleanup(func() { flagStats = false })

	// Seed DB.
	seed, err := db.OpenAt(filepath.Join(dir, ".tutti", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	_ = seed.UpsertListing(db.StoredListing{
		ListingID: "A", Title: "iPhone 15", Price: 500, PriceStr: "CHF 500.–",
		Location: "Zürich", Canton: "ZH", URL: "https://x/A", Query: "iphone",
	})
	_ = seed.UpsertListing(db.StoredListing{
		ListingID: "B", Title: "MacBook Pro", Price: 1500, PriceStr: "CHF 1'500.–",
		Location: "Bern", Canton: "BE", URL: "https://x/B", Query: "macbook",
	})
	// Two price changes for A so history shows up.
	_ = seed.UpsertListing(db.StoredListing{
		ListingID: "A", Title: "iPhone 15", Price: 480, PriceStr: "CHF 480.–",
		Query: "iphone",
	})
	seed.Close()

	out, err := captureStdout(t, func() error { return runHistory("iphone") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "iPhone 15") {
		t.Errorf("expected iPhone 15, got:\n%s", out)
	}
	if strings.Contains(out, "MacBook Pro") {
		t.Errorf("query filter should exclude macbook, got:\n%s", out)
	}
	if !strings.Contains(out, "Price history:") {
		t.Errorf("expected Price history section, got:\n%s", out)
	}
}

func TestRunHistory_Stats(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	flagStats = true
	t.Cleanup(func() { flagStats = false })

	seed, err := db.OpenAt(filepath.Join(dir, ".tutti", "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	for i, p := range []int{100, 200, 300, 400} {
		_ = seed.UpsertListing(db.StoredListing{
			ListingID: string(rune('A' + i)),
			Title:     "macbook",
			Price:     p,
			PriceStr:  "x",
			Query:     "macbook",
		})
	}
	seed.Close()

	out, err := captureStdout(t, func() error { return runHistory("macbook") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Price stats for \"macbook\"") {
		t.Errorf("expected stats header, got:\n%s", out)
	}
	if !strings.Contains(out, "Median: CHF") {
		t.Errorf("expected median, got:\n%s", out)
	}
}

func TestRunHistory_StatsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	flagStats = true
	t.Cleanup(func() { flagStats = false })

	out, err := captureStdout(t, func() error { return runHistory("nothing") })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No price data") {
		t.Errorf("expected empty-stats message, got:\n%s", out)
	}
}