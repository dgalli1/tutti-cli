package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	db *sql.DB
}

type StoredListing struct {
	ListingID  string
	Title      string
	Price      int // CHF integer
	PriceStr   string
	Location   string
	Canton     string
	URL        string
	Query      string
	FirstSeen  time.Time
	LastSeen   time.Time
	SeenCount  int
}

type PriceStats struct {
	Count  int
	Min    int
	Max    int
	Median int
	Mean   int
	P25    int
	P75    int
}

func Open() (*DB, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(home, ".tutti")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "db.sqlite")
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	d := &DB{db: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) Close() { d.db.Close() }

func (d *DB) migrate() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS listings (
			listing_id  TEXT PRIMARY KEY,
			title       TEXT NOT NULL,
			price       INTEGER NOT NULL DEFAULT 0,
			price_str   TEXT NOT NULL DEFAULT '',
			location    TEXT NOT NULL DEFAULT '',
			canton      TEXT NOT NULL DEFAULT '',
			url         TEXT NOT NULL DEFAULT '',
			query       TEXT NOT NULL DEFAULT '',
			first_seen  INTEGER NOT NULL,
			last_seen   INTEGER NOT NULL,
			seen_count  INTEGER NOT NULL DEFAULT 1
		);
		CREATE INDEX IF NOT EXISTS idx_listings_query ON listings(query);
		CREATE INDEX IF NOT EXISTS idx_listings_price ON listings(price);
		CREATE TABLE IF NOT EXISTS price_history (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			listing_id  TEXT NOT NULL,
			price       INTEGER NOT NULL,
			price_str   TEXT NOT NULL,
			recorded_at INTEGER NOT NULL,
			FOREIGN KEY(listing_id) REFERENCES listings(listing_id)
		);
		CREATE INDEX IF NOT EXISTS idx_ph_listing ON price_history(listing_id);
	`)
	return err
}

func (d *DB) UpsertListing(l StoredListing) error {
	now := time.Now().Unix()
	res, err := d.db.Exec(`
		INSERT INTO listings (listing_id, title, price, price_str, location, canton, url, query, first_seen, last_seen, seen_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
		ON CONFLICT(listing_id) DO UPDATE SET
			last_seen  = excluded.last_seen,
			seen_count = seen_count + 1,
			price      = excluded.price,
			price_str  = excluded.price_str
	`, l.ListingID, l.Title, l.Price, l.PriceStr, l.Location, l.Canton, l.URL, l.Query, now, now)
	if err != nil {
		return err
	}

	// Record price history if price changed or first time
	rows, _ := res.RowsAffected()
	if rows > 0 && l.Price > 0 {
		var lastPrice int
		d.db.QueryRow(`SELECT price FROM price_history WHERE listing_id=? ORDER BY recorded_at DESC LIMIT 1`, l.ListingID).Scan(&lastPrice)
		if lastPrice != l.Price {
			_, err = d.db.Exec(`INSERT INTO price_history (listing_id, price, price_str, recorded_at) VALUES (?, ?, ?, ?)`,
				l.ListingID, l.Price, l.PriceStr, now)
		}
	}
	return err
}

// PriceStatsForQuery returns price statistics for all listings seen for a given query.
func (d *DB) PriceStatsForQuery(query string) (*PriceStats, error) {
	rows, err := d.db.Query(`SELECT price FROM listings WHERE query=? AND price > 0`, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var prices []int
	for rows.Next() {
		var p int
		if err := rows.Scan(&p); err == nil && p > 0 {
			prices = append(prices, p)
		}
	}
	if len(prices) == 0 {
		return &PriceStats{}, nil
	}
	sort.Ints(prices)
	sum := 0
	for _, p := range prices {
		sum += p
	}
	return &PriceStats{
		Count:  len(prices),
		Min:    prices[0],
		Max:    prices[len(prices)-1],
		Median: prices[len(prices)/2],
		Mean:   sum / len(prices),
		P25:    prices[len(prices)/4],
		P75:    prices[len(prices)*3/4],
	}, nil
}

// PriceHistory returns recorded prices for a specific listing
func (d *DB) PriceHistory(listingID string) ([]struct {
	Price      int
	PriceStr   string
	RecordedAt time.Time
}, error) {
	rows, err := d.db.Query(`SELECT price, price_str, recorded_at FROM price_history WHERE listing_id=? ORDER BY recorded_at`, listingID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var history []struct {
		Price      int
		PriceStr   string
		RecordedAt time.Time
	}
	for rows.Next() {
		var p int
		var ps string
		var ts int64
		if err := rows.Scan(&p, &ps, &ts); err == nil {
			history = append(history, struct {
				Price      int
				PriceStr   string
				RecordedAt time.Time
			}{p, ps, time.Unix(ts, 0)})
		}
	}
	return history, nil
}

// SearchHistory returns previously seen listings matching a query pattern
func (d *DB) SearchHistory(queryLike string) ([]StoredListing, error) {
	rows, err := d.db.Query(`
		SELECT listing_id, title, price, price_str, location, canton, url, query, first_seen, last_seen, seen_count
		FROM listings WHERE query LIKE ? ORDER BY last_seen DESC LIMIT 100
	`, "%"+queryLike+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredListing
	for rows.Next() {
		var l StoredListing
		var fs, ls int64
		err := rows.Scan(&l.ListingID, &l.Title, &l.Price, &l.PriceStr,
			&l.Location, &l.Canton, &l.URL, &l.Query, &fs, &ls, &l.SeenCount)
		if err == nil {
			l.FirstSeen = time.Unix(fs, 0)
			l.LastSeen = time.Unix(ls, 0)
			out = append(out, l)
		}
	}
	return out, nil
}

func (d *DB) CountListings() (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM listings`).Scan(&n)
	return n, err
}

// PriceCategory classifies a price relative to the price stats
func (s *PriceStats) PriceCategory(price int) string {
	if s.Count == 0 || price == 0 {
		return ""
	}
	switch {
	case price <= s.P25:
		return fmt.Sprintf("very cheap (bottom 25%%, median CHF %d)", s.Median)
	case price <= s.Median:
		return fmt.Sprintf("below median (median CHF %d)", s.Median)
	case price <= s.P75:
		return fmt.Sprintf("above median (median CHF %d)", s.Median)
	default:
		return fmt.Sprintf("expensive (top 25%%, median CHF %d)", s.Median)
	}
}
