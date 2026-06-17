package api

import (
	"encoding/base64"
	"reflect"
	"testing"
)

func TestBuildToken_Empty(t *testing.T) {
	got := BuildToken("")
	want := "Ak8DAlMDAwMA"
	if got != want {
		t.Errorf("BuildToken(\"\") = %q, want %q", got, want)
	}
}

func TestBuildToken_NonEmpty(t *testing.T) {
	// Build the expected bytes from the algorithm and check we round-trip.
	q := "iphone 15 pro"
	got := BuildToken(q)
	raw, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatalf("token is not valid base64: %v", err)
	}

	// Header: 0x02 0x4E
	if len(raw) < 2 || raw[0] != 0x02 || raw[1] != 0x4E {
		t.Fatalf("missing header bytes, got % x", raw)
	}
	// Length byte: rol2(n) | 0x81
	n := len(q)
	expectedLen := byte((n<<2)|0x80) | 0x81 // (n<<2)&0xFF with OR 0x81, but rotate first
	// rol2(byte(n)) = (n<<2)|(n>>6) for n < 64; for 14 that's 14<<2=56, 14>>6=0 → 56
	expectedLen = byte(((n << 2) | (n >> 6)) | 0x81)
	if raw[2] != expectedLen {
		t.Errorf("length byte = %#x, want %#x", raw[2], expectedLen)
	}

	// Trailer: 0x02 0x53 0x03 0x03 0x03 0x00
	tail := []byte{0x02, 0x53, 0x03, 0x03, 0x03, 0x00}
	if !reflect.DeepEqual(raw[len(raw)-6:], tail) {
		t.Errorf("trailer = % x, want % x", raw[len(raw)-6:], tail)
	}

	// Encoded chars: first n-1 with rol2 only; last with rol2(ch | 0x80).
	body := raw[3 : 3+n]
	for i := 0; i < n-1; i++ {
		want := byte((q[i] << 2) | (q[i] >> 6))
		if body[i] != want {
			t.Errorf("body[%d] = %#x, want %#x (from %q)", i, body[i], want, q[i])
		}
	}
	last := q[n-1]
	lastWant := byte(((last | 0x80) << 2) | ((last | 0x80) >> 6))
	if body[n-1] != lastWant {
		t.Errorf("last body byte = %#x, want %#x", body[n-1], lastWant)
	}
}

func TestBuildToken_KnownShortQuery(t *testing.T) {
	// Single char — last byte gets |0x80, length byte has |0x81.
	got := BuildToken("a")
	raw, err := base64.RawURLEncoding.DecodeString(got)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 3+1+6 {
		t.Fatalf("raw len = %d, want %d", len(raw), 10)
	}
}

func TestParsePrice(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"CHF 1'200.–", 1200},
		{"CHF 50.–", 50},
		{"CHF 9.–", 9},
		{"", 0},
		{"Gratis", 0},
		{"gratis zu verschenken", 0},
		{"CHF 1'234'567.–", 1234567},
		{"CHF 999.-", 999},
		{"999.-", 999},
	}
	for _, c := range cases {
		got := ParsePrice(c.in)
		if got != c.want {
			t.Errorf("ParsePrice(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestListingURL(t *testing.T) {
	cases := []struct {
		slug string
		want string
	}{
		{"some-slug", "https://www.tutti.ch/de/q/details/some-slug"},
		{"", ""},
	}
	for _, c := range cases {
		got := ListingURL(c.slug)
		if got != c.want {
			t.Errorf("ListingURL(%q) = %q, want %q", c.slug, got, c.want)
		}
	}
}

func TestSortToGQL(t *testing.T) {
	cases := []struct {
		in            string
		wantSort      string
		wantDirection string
	}{
		{"newest", "TIMESTAMP", "DESCENDING"},
		{"oldest", "TIMESTAMP", "ASCENDING"},
		{"price_asc", "PRICE", "ASCENDING"},
		{"price_desc", "PRICE", "DESCENDING"},
		{"relevance", "TIMESTAMP", "DESCENDING"},
		{"bogus", "TIMESTAMP", "DESCENDING"},
	}
	for _, c := range cases {
		gotSort, gotDir := sortToGQL(c.in)
		if gotSort != c.wantSort || gotDir != c.wantDirection {
			t.Errorf("sortToGQL(%q) = (%q, %q), want (%q, %q)",
				c.in, gotSort, gotDir, c.wantSort, c.wantDirection)
		}
	}
}
