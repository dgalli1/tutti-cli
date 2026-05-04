package ascii

import (
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"
)

// Gradient from dark to light — best on dark terminals.
// Reversed version appended for light terminals via Invert().
const gradient = " `.-':_,^=;><+!rc*/z?sLTv)J7(|Fi{C}fI31tlu[neoZ5Yxjya]2ESwqkP6h9d4VpOGbUAKXHm8RD#$Bg0MNWQ%&@"

// Fetch downloads an image from url and decodes it.
func Fetch(url string) (image.Image, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	img, _, err := image.Decode(resp.Body)
	return img, err
}

// FetchReader decodes an image from an already-open reader.
func FetchReader(r io.Reader) (image.Image, error) {
	img, _, err := image.Decode(r)
	return img, err
}

// Render converts img to an ASCII string at the given terminal width.
// It corrects for the ~2:1 character aspect ratio automatically.
func Render(img image.Image, width int) string {
	if width < 4 {
		width = 4
	}
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	// Account for character cell being ~2× taller than wide
	height := width * srcH / srcW / 2
	if height < 2 {
		height = 2
	}

	var sb strings.Builder
	gLen := len(gradient)

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Nearest-neighbour sample from source
			srcX := bounds.Min.X + x*srcW/width
			srcY := bounds.Min.Y + y*srcH/height
			c := img.At(srcX, srcY)

			r16, g16, b16, _ := c.RGBA()
			// BT.709 luminance, values are 0–65535
			lum := (0.2126*float64(r16) + 0.7152*float64(g16) + 0.0722*float64(b16)) / 65535.0
			idx := int(lum * float64(gLen-1))
			if idx >= gLen {
				idx = gLen - 1
			}
			sb.WriteByte(gradient[idx])
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// RenderColored renders img as ANSI 256-color ASCII art.
// Falls back to RenderGrayscale if the terminal doesn't support color.
func RenderColored(img image.Image, width int) string {
	if width < 4 {
		width = 4
	}
	bounds := img.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()
	if srcW == 0 || srcH == 0 {
		return ""
	}

	height := width * srcH / srcW / 2
	if height < 2 {
		height = 2
	}

	gLen := len(gradient)
	var sb strings.Builder

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			srcX := bounds.Min.X + x*srcW/width
			srcY := bounds.Min.Y + y*srcH/height
			c := img.At(srcX, srcY)

			r16, g16, b16, _ := c.RGBA()
			r8 := uint8(r16 >> 8)
			g8 := uint8(g16 >> 8)
			b8 := uint8(b16 >> 8)

			lum := (0.2126*float64(r16) + 0.7152*float64(g16) + 0.0722*float64(b16)) / 65535.0
			idx := int(lum * float64(gLen-1))
			if idx >= gLen {
				idx = gLen - 1
			}

			// ANSI true-color foreground
			fmt.Fprintf(&sb, "\x1b[38;2;%d;%d;%dm%c", r8, g8, b8, gradient[idx])
		}
		sb.WriteString("\x1b[0m\n")
	}
	return sb.String()
}

// Grayscale converts an image to grayscale before rendering.
func toGray(img image.Image) *image.Gray {
	b := img.Bounds()
	gray := image.NewGray(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			gray.Set(x, y, color.GrayModel.Convert(img.At(x, y)).(color.Gray))
		}
	}
	return gray
}
