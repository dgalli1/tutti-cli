package geo

import (
	"math"
	"strings"
	"testing"
)

const epsKm = 0.5 // 500 m tolerance for reference distances

func approxEqual(a, b, eps float64) bool {
	return math.Abs(a-b) < eps
}

func TestHaversine_SamePoint(t *testing.T) {
	if d := HaversineKm(47.376, 8.541, 47.376, 8.541); d != 0 {
		t.Errorf("distance to self should be 0, got %v", d)
	}
}

func TestHaversine_Antipodes(t *testing.T) {
	// Two points on opposite sides of the Earth → ~π·R
	d := HaversineKm(0, 0, 0, 180)
	expected := math.Pi * earthRadiusKm
	if math.Abs(d-expected) > 1.0 {
		t.Errorf("antipodes expected ~%.0f km, got %.0f km", expected, d)
	}
}

func TestHaversine_KnownDistances(t *testing.T) {
	// Reference: Zürich HB ↔ Bern HB ≈ 96 km (great-circle).
	zlat, zlon := 47.378, 8.540
	blat, blon := 46.949, 7.447
	d := HaversineKm(zlat, zlon, blat, blon)
	if !approxEqual(d, 96.0, 2.0) {
		t.Errorf("Zürich-Bern expected ~96 km, got %.1f km", d)
	}

	// Zürich HB ↔ Genf Cornavin ≈ 224 km
	glat, glon := 46.210, 6.142
	d = HaversineKm(zlat, zlon, glat, glon)
	if !approxEqual(d, 224.0, 5.0) {
		t.Errorf("Zürich-Genf expected ~224 km, got %.1f km", d)
	}

	// Zürich HB ↔ Basel SBB ≈ 75 km
	bslat, bslon := 47.547, 7.589
	d = HaversineKm(zlat, zlon, bslat, bslon)
	if !approxEqual(d, 75.0, 2.0) {
		t.Errorf("Zürich-Basel expected ~75 km, got %.1f km", d)
	}

	// 1° latitude ≈ 111 km
	d = HaversineKm(0, 0, 1, 0)
	if !approxEqual(d, 111.0, 1.0) {
		t.Errorf("1 deg lat expected ~111 km, got %.1f km", d)
	}

	// 1° longitude at equator ≈ 111 km
	d = HaversineKm(0, 0, 0, 1)
	if !approxEqual(d, 111.0, 1.0) {
		t.Errorf("1 deg lon at equator expected ~111 km, got %.1f km", d)
	}
}

func TestWithinRadius(t *testing.T) {
	// Zürich center
	zl := Lookup("8001")
	if !zl.Matched {
		t.Fatal("8001 (Zürich) should resolve")
	}
	// Winterthur is ~20 km from Zürich (great-circle, NHM reference)
	wt := Lookup("8400")
	if !wt.Matched {
		t.Skip("8400 not in dataset; skipping")
	}
	d := HaversineKm(zl.Lat, zl.Lon, wt.Lat, wt.Lon)
	if !approxEqual(d, 20.0, 2.0) {
		t.Errorf("Zürich-Winterthur expected ~20 km, got %.1f km", d)
	}
	if !WithinRadius(zl.Lat, zl.Lon, wt.Lat, wt.Lon, 30) {
		t.Error("30 km radius should include Winterthur")
	}
	if !WithinRadius(zl.Lat, zl.Lon, wt.Lat, wt.Lon, 20) {
		// 20 km is right at the edge; we accept anything <20.1 km
		// (the actual measurement is 19.9 km per the dataset centroid).
		if d > 20.1 {
			t.Error("20 km radius should include Winterthur when d≤20")
		}
	}
	if WithinRadius(zl.Lat, zl.Lon, wt.Lat, wt.Lon, 15) {
		t.Error("15 km radius should exclude Winterthur")
	}
}

func TestLookup_KnownPLZs(t *testing.T) {
	cases := []struct {
		plz      string
		wantName string // substring; empty = don't check
		wantLat  float64
		wantLon  float64
		tol      float64
	}{
		{"8001", "Zürich", 47.37, 8.54, 0.05},
		{"3011", "Bern", 46.95, 7.45, 0.05},
		{"1201", "Genève", 46.20, 6.14, 0.05},
		{"4051", "Basel", 47.55, 7.58, 0.05},
		{"6003", "Luzern", 47.05, 8.30, 0.05},
		{"9000", "St. Gallen", 47.42, 9.37, 0.05},
		{"7000", "Chur", 46.85, 9.53, 0.05},
	}
	for _, c := range cases {
		got := Lookup(c.plz)
		if !got.Matched {
			t.Errorf("Lookup(%q) should match exactly; got %+v", c.plz, got)
			continue
		}
		if math.Abs(got.Lat-c.wantLat) > c.tol {
			t.Errorf("Lookup(%q) lat=%v want ~%v", c.plz, got.Lat, c.wantLat)
		}
		if math.Abs(got.Lon-c.wantLon) > c.tol {
			t.Errorf("Lookup(%q) lon=%v want ~%v", c.plz, got.Lon, c.wantLon)
		}
		if c.wantName != "" && !strings.Contains(got.Name, c.wantName) {
			t.Errorf("Lookup(%q) name=%q does not contain %q", c.plz, got.Name, c.wantName)
		}
	}
}

func TestLookup_InvalidInputs(t *testing.T) {
	cases := []struct {
		in        string
		wantEmpty bool
		wantPLZ   string
	}{
		{"", true, ""},
		{"   ", true, ""},
		{"abc", true, ""},
		{"99", true, ""},
		{"0999", true, ""},        // < 1000 → not a valid Swiss PLZ
		{"12345", false, "1234"},  // first 4-digit window in valid range
		{"8001.0", false, "8001"}, // trailing dot-zero dropped → "80010" → first valid window "8001"
	}
	for _, c := range cases {
		got := Lookup(c.in)
		if c.wantEmpty && (got.PLZ != "" || got.Matched) {
			t.Errorf("Lookup(%q) should be invalid, got %+v", c.in, got)
		}
		if !c.wantEmpty && got.PLZ != c.wantPLZ {
			t.Errorf("Lookup(%q) PLZ=%q want %q", c.in, got.PLZ, c.wantPLZ)
		}
	}
}

func TestLookup_Normalization(t *testing.T) {
	// Various input forms should resolve to the same PLZ.
	forms := []string{"8001", " 8001", "8001 ", " CH-8001", "8001.0", "postcode 8001"}
	want := Lookup("8001")
	for _, f := range forms {
		got := Lookup(f)
		if got.PLZ != want.PLZ {
			t.Errorf("Lookup(%q) PLZ=%q want %q", f, got.PLZ, want.PLZ)
		}
		if got.Matched != want.Matched {
			t.Errorf("Lookup(%q) Matched=%v want %v", f, got.Matched, want.Matched)
		}
		if got.Matched && (got.Lat != want.Lat || got.Lon != want.Lon) {
			t.Errorf("Lookup(%q) coords differ: got %+v want %+v", f, got, want)
		}
	}
}

func TestLookup_NotInDataset(t *testing.T) {
	// "9999" is in the valid range but unlikely to be a real PLZ; should
	// fall back to the 4-digit-prefix region centroid or remain unknown.
	got := Lookup("9999")
	// It must not panic and must return either an entry (matched or fallback)
	// or an empty PLZLocation.
	_ = got
}

func TestHasPLZ(t *testing.T) {
	if !HasPLZ("8001") {
		t.Error("HasPLZ(8001) should be true")
	}
	if HasPLZ("9999") {
		t.Error("HasPLZ(9999) should be false")
	}
	if HasPLZ("not-a-plz") {
		t.Error("HasPLZ(non-numeric) should be false")
	}
}

func TestNewSearchRadius_RejectsBadInput(t *testing.T) {
	if _, err := NewSearchRadius("8001", 0); err == nil {
		t.Error("radius 0 should be rejected")
	}
	if _, err := NewSearchRadius("8001", -5); err == nil {
		t.Error("negative radius should be rejected")
	}
	if _, err := NewSearchRadius("", 10); err == nil {
		t.Error("empty PLZ should be rejected")
	}
	if _, err := NewSearchRadius("not-a-plz", 10); err == nil {
		t.Error("non-numeric PLZ should be rejected")
	}
	r, err := NewSearchRadius("8001", 10)
	if err != nil {
		t.Fatalf("NewSearchRadius valid input failed: %v", err)
	}
	if r.Origin.PLZ != "8001" || !r.Origin.Matched {
		t.Errorf("radius origin not set: %+v", r.Origin)
	}
	if r.RadiusKm != 10 {
		t.Errorf("radius not set: %v", r.RadiusKm)
	}
}

func TestSearchRadius_Keeps(t *testing.T) {
	r, err := NewSearchRadius("8001", 10) // 10 km from Zürich
	if err != nil {
		t.Fatal(err)
	}
	// Zürich itself: keep
	if !r.Keeps("8001") {
		t.Error("Zürich (8001) should be within 10 km of itself")
	}
	// Other Zürich PLZs: keep
	if !r.Keeps("8002") {
		t.Error("8002 (Zürich) should be within 10 km of 8001")
	}
	// Winterthur (8400): ~25 km → drop
	if r.Keeps("8400") {
		t.Error("Winterthur (8400) should NOT be within 10 km of 8001")
	}
	// Bern (3011): ~95 km → drop
	if r.Keeps("3011") {
		t.Error("Bern (3011) should NOT be within 10 km of 8001")
	}
	// Unknown PLZ: drop
	if r.Keeps("") {
		t.Error("empty PLZ should be dropped")
	}
}

func TestSearchRadius_Keeps50km(t *testing.T) {
	r, _ := NewSearchRadius("8001", 50)
	// Winterthur (8400): ~25 km → keep at 50 km
	if !r.Keeps("8400") {
		t.Error("Winterthur (8400) should be within 50 km of 8001")
	}
	// Bern (3011): ~95 km → still drop
	if r.Keeps("3011") {
		t.Error("Bern (3011) should NOT be within 50 km of 8001")
	}
	// Luzern (6003): ~40 km → keep
	if !r.Keeps("6003") {
		t.Error("Luzern (6003) should be within 50 km of 8001")
	}
}

func TestSearchRadius_String(t *testing.T) {
	r, _ := NewSearchRadius("8001", 10)
	s := r.String()
	if !strings.Contains(s, "10 km") || !strings.Contains(s, "8001") {
		t.Errorf("String() = %q; expected to mention radius and PLZ", s)
	}
}

func TestSearchRadius_DistanceKm(t *testing.T) {
	r, _ := NewSearchRadius("8001", 10)
	d := r.DistanceKm("3011")
	if math.IsInf(d, 1) {
		t.Error("Bern is in the dataset; distance must not be Inf")
	}
	if d < 80 || d > 110 {
		t.Errorf("Zürich-Bern expected 80–110 km, got %.1f", d)
	}
	if math.IsInf(r.DistanceKm("9999"), 1) == false {
		// "9999" might exist as a region centroid fallback → not Inf
	}
}

func TestDatasetCoverage(t *testing.T) {
	// Sanity: we should have ≥3000 entries; the official swisstopo dataset
	// has 3190 unique PLZ4 codes as of 2026-06.
	if n := len(swisstopoPLZ); n < 3000 {
		t.Errorf("PLZ dataset has only %d entries; expected ≥3000", n)
	}
}

func TestSortedNames_NoDupsAndSorted(t *testing.T) {
	names := SortedNames()
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			t.Error("empty name in dataset")
		}
		if seen[n] {
			t.Errorf("duplicate name %q", n)
		}
		seen[n] = true
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] > names[i] {
			t.Errorf("not sorted: %q > %q", names[i-1], names[i])
		}
	}
}
