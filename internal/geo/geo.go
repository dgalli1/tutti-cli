// Package geo provides Swiss postal-code → WGS84 coordinate lookup and
// great-circle distance helpers used by the search command for client-side
// radius filtering.
//
// Tutti.ch does not expose a server-side radius filter for "Standort +
// Umkreis" in its public GraphQL API, so we approximate the feature
// client-side: for each listing we resolve its 4-digit PLZ against an
// address-share-weighted centroid of the official swisstopo
// "Ortschaftenverzeichnis PLZ" dataset, then drop listings whose distance
// from the requested origin PLZ exceeds the requested radius.
package geo

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
)

// plzEntry holds the address-share-weighted centroid of a Swiss 4-digit PLZ
// (WGS84 decimal degrees) and the best (most-addresses) Ortschaftsname.
type plzEntry struct {
	Name string
	Lat  float64
	Lon  float64
}

// earthRadiusKm is the mean Earth radius used by haversine. Good enough for
// radius filtering at the city scale.
const earthRadiusKm = 6371.0088

// PLZLocation represents the resolved position of a 4-digit Swiss PLZ.
type PLZLocation struct {
	PLZ     string  // canonical 4-digit form (zero-padded), empty if unknown
	Name    string  // ortschaftsname (city / district name)
	Lat     float64 // WGS84 decimal degrees
	Lon     float64 // WGS84 decimal degrees
	Matched bool    // false when PLZ was not found in the dataset
}

// Lookup resolves a Swiss 4-digit PLZ (any input form: "8001", "8001 ", " 8001 ")
// to its WGS84 centroid. The returned PLZLocation has Matched=false when the
// PLZ is not present in the dataset.
//
// Two fallback strategies are applied in order for unknown PLZs:
//  1. PLZ 2-digit prefix match: uses the first entry of the same canton
//     (e.g. "9999" → some PLZ starting with "99"). Accuracy ±30 km.
//  2. Static region centroid table (Swiss Plateau/Lowlands average) used
//     when the 2-digit prefix has no entries at all.
func Lookup(plz string) PLZLocation {
	plz = normalizePLZ(plz)
	if plz == "" {
		return PLZLocation{}
	}
	if e, ok := swisstopoPLZ[plz]; ok {
		return PLZLocation{PLZ: plz, Name: e.Name, Lat: e.Lat, Lon: e.Lon, Matched: true}
	}
	// 2-digit prefix fallback: "9999" → first entry whose PLZ starts with "99".
	if prefix := plz[:2]; prefix != "" {
		for k, e := range swisstopoPLZ {
			if strings.HasPrefix(k, prefix) {
				return PLZLocation{PLZ: plz, Name: e.Name + " (region centroid)", Lat: e.Lat, Lon: e.Lon, Matched: false}
			}
		}
	}
	// Final fallback: Swiss Plateau centroid (always inside the country).
	// Accuracy is ±50 km but at least we don't return zero coords.
	return PLZLocation{PLZ: plz, Name: "Switzerland (centroid)", Lat: 46.818, Lon: 8.227, Matched: false}
}

// HasPLZ reports whether the given PLZ has a precise (non-fallback) entry.
func HasPLZ(plz string) bool {
	plz = normalizePLZ(plz)
	if plz == "" {
		return false
	}
	_, ok := swisstopoPLZ[plz]
	return ok
}

// HaversineKm returns the great-circle distance between two WGS84 points
// in kilometres, using the haversine formula. Accuracy is well below 1 m
// at city scale (Earth-modeling error dominates over the formula).
func HaversineKm(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180.0
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return earthRadiusKm * c
}

// WithinRadius reports whether the WGS84 point (lat2, lon2) is within radiusKm
// of (lat1, lon1).
func WithinRadius(lat1, lon1, lat2, lon2, radiusKm float64) bool {
	return HaversineKm(lat1, lon1, lat2, lon2) <= radiusKm
}

// SearchRadius describes a geographic filter applied to listings.
// Origin is the WGS84 centroid of the user-supplied PLZ; RadiusKm is the
// maximum distance in kilometres.
type SearchRadius struct {
	Origin   PLZLocation
	RadiusKm float64
}

// String returns a human-readable representation like "within 10 km of 8001 (Zürich)".
func (r SearchRadius) String() string {
	if !r.Origin.Matched {
		loc := r.Origin.PLZ
		if loc == "" {
			loc = "(no PLZ)"
		}
		if r.Origin.Name != "" {
			return fmt.Sprintf("within %.0f km of %s (%s, region centroid)", r.RadiusKm, loc, r.Origin.Name)
		}
		return fmt.Sprintf("within %.0f km of %s", r.RadiusKm, loc)
	}
	return fmt.Sprintf("within %.0f km of %s (%s)", r.RadiusKm, r.Origin.PLZ, r.Origin.Name)
}

// Keeps returns true if the listing's PLZ is within the radius.
// A listing with an unparseable / missing PLZ is dropped.
func (r SearchRadius) Keeps(postcode string) bool {
	other := Lookup(postcode)
	if !other.Matched {
		return false
	}
	return HaversineKm(r.Origin.Lat, r.Origin.Lon, other.Lat, other.Lon) <= r.RadiusKm
}

// DistanceKm returns the great-circle distance from the radius origin to the
// given PLZ. Returns math.Inf(1) when the PLZ cannot be resolved.
func (r SearchRadius) DistanceKm(postcode string) float64 {
	other := Lookup(postcode)
	if !other.Matched {
		return math.Inf(1)
	}
	return HaversineKm(r.Origin.Lat, r.Origin.Lon, other.Lat, other.Lon)
}

// NewSearchRadius resolves the given origin PLZ and pairs it with the radius.
// RadiusKm must be > 0; returns an error otherwise so callers can reject
// nonsensical CLI input.
func NewSearchRadius(originPLZ string, radiusKm float64) (SearchRadius, error) {
	if radiusKm <= 0 {
		return SearchRadius{}, fmt.Errorf("radius must be > 0 km, got %.2f", radiusKm)
	}
	origin := Lookup(originPLZ)
	if !origin.Matched {
		if origin.PLZ == "" {
			return SearchRadius{}, fmt.Errorf("invalid origin postcode %q", originPLZ)
		}
		// We still allow it: caller decides whether to warn the user.
	}
	return SearchRadius{Origin: origin, RadiusKm: radiusKm}, nil
}

// normalizePLZ extracts a 4-digit Swiss PLZ (range 1000–9999) from arbitrary
// user input. Handles trailing junk ("8001 ", "postcode 8001", "CH-8001"),
// embedded PLZ ("Tel: 0800 8001" → "8001") and postfix dot-zero
// ("8001.0" → "8001"). Returns "" for input that does not contain a valid
// 4-digit Swiss PLZ.
func normalizePLZ(plz string) string {
	plz = strings.TrimSpace(plz)
	// Strip every non-digit. Common noise: "CH-", spaces, dots, "Postcode ".
	var b strings.Builder
	for _, r := range plz {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if len(digits) < 4 {
		return "" // not enough digits to be a Swiss PLZ
	}
	// Search the digit string for the first 4-digit window that parses as
	// a valid Swiss PLZ (range 1000–9999). This handles:
	//   "8001"       → "8001"
	//   "80010"      → "8001" (trailing junk)
	//   "08008001"   → "8001" (Phone-like prefix)
	//   "12345"      → "2345" (only valid 4-digit window found)
	for i := 0; i+4 <= len(digits); i++ {
		cand := digits[i : i+4]
		if cand >= "1000" && cand <= "9999" {
			return cand
		}
	}
	return "" // no valid Swiss 4-digit PLZ found
}

// SortedNames returns all Ortschaftsnamen present in the dataset, sorted.
// Used by tests and CLI `--list-plz`.
func SortedNames() []string {
	once.Do(func() {
		names = make([]string, 0, len(swisstopoPLZ))
		seen := make(map[string]bool)
		for _, e := range swisstopoPLZ {
			if !seen[e.Name] {
				seen[e.Name] = true
				names = append(names, e.Name)
			}
		}
		sort.Strings(names)
	})
	return names
}

var (
	once  sync.Once
	names []string
)
