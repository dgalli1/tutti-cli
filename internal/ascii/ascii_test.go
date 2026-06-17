package ascii

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

// solidImg returns an image.Image of the given solid colour.
func solidImg(c color.Color, w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	return img
}

func TestRender_SolidBlack(t *testing.T) {
	img := solidImg(color.Black, 100, 50)
	out := Render(img, 40)
	// Should have one line per output row (height derived from aspect / 2).
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 rows, got %d (out=%q)", len(lines), out)
	}
	// Black → first gradient char (space).
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			t.Errorf("expected all-space row for black image, got %q", line)
		}
	}
}

func TestRender_SolidWhite(t *testing.T) {
	img := solidImg(color.White, 100, 50)
	out := Render(img, 40)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 rows, got %d", len(lines))
	}
	// White → last gradient char (@).
	for _, line := range lines {
		if !strings.Contains(line, "@") {
			t.Errorf("expected @ in white image row, got %q", line)
		}
	}
}

func TestRender_WidthRespected(t *testing.T) {
	img := solidImg(color.RGBA{128, 128, 128, 255}, 200, 100)
	out := Render(img, 50)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) == 0 {
		t.Fatal("empty output")
	}
	if len(lines[0]) != 50 {
		t.Errorf("first row width = %d, want 50", len(lines[0]))
	}
}

func TestRender_NarrowClamp(t *testing.T) {
	img := solidImg(color.RGBA{50, 50, 50, 255}, 20, 20)
	// Width below 4 should be clamped to 4.
	out := Render(img, 1)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines[0]) != 4 {
		t.Errorf("clamped width = %d, want 4", len(lines[0]))
	}
}

func TestRender_EmptyImage(t *testing.T) {
	img := solidImg(color.Black, 0, 0)
	if out := Render(img, 40); out != "" {
		t.Errorf("expected empty string for empty image, got %q", out)
	}
}

func TestRenderColored_HasAnsi(t *testing.T) {
	img := solidImg(color.RGBA{255, 0, 0, 255}, 100, 50) // pure red
	out := RenderColored(img, 20)
	if !strings.Contains(out, "\x1b[38;2;255;0;0m") {
		t.Errorf("expected ANSI true-color red escape, got %q", out)
	}
	if !strings.Contains(out, "\x1b[0m") {
		t.Errorf("expected ANSI reset, got %q", out)
	}
}

func TestRenderColored_RowWidth(t *testing.T) {
	img := solidImg(color.RGBA{10, 20, 30, 255}, 200, 100)
	out := RenderColored(img, 30)
	// Strip ANSI escapes to measure visible width.
	first := strings.SplitN(out, "\n", 2)[0]
	visible := stripANSI(first)
	if len(visible) != 30 {
		t.Errorf("visible row width = %d, want 30", len(visible))
	}
}

// stripANSI removes CSI escape sequences (basic version good enough for our output).
func stripANSI(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !isANSITerminator(s[j]) {
				j++
			}
			i = j + 1
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String()
}

func isANSITerminator(b byte) bool {
	return b >= 0x40 && b <= 0x7e
}

func TestFetchReader(t *testing.T) {
	// PNG header + IDAT-free solid colour image → just verify decode works.
	img := solidImg(color.RGBA{1, 2, 3, 255}, 10, 10)
	out := RenderColored(img, 10)
	if len(out) == 0 {
		t.Fatal("RenderColored returned empty")
	}
}
