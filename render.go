package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"slices"

	"github.com/makiuchi-d/gozxing"
	"github.com/mattn/go-sixel"
)

const (
	ansiReset = "\x1b[0m"

	uBlockNone  = ' '
	uBlockUpper = '▀'
	uBlockLower = '▄'
	uBlockFull  = '█'
)

// defaultScales lists the supported output formats along with the scale each
// one uses when -s is not given. The raster and vector formats need more than
// one pixel per module to be scannable; terminal formats are one cell per
// module.
var defaultScales = map[string]int{
	"unicode": 1,
	"ansi":    1,
	"sixel":   1,
	"png":     8,
	"svg":     8,
}

// matrixToGrid converts a gozxing BitMatrix (one cell per QR module) into a
// [][]bool, then nearest-neighbor-scales it by scale in both dimensions.
// A true cell is a dark module; invert swaps dark and light.
func matrixToGrid(m *gozxing.BitMatrix, scale int, invert bool) [][]bool {
	w, h := m.GetWidth(), m.GetHeight()
	grid := make([][]bool, h*scale)
	for y := range h {
		row := make([]bool, w*scale)
		for x := range w {
			v := m.Get(x, y) != invert
			for sx := range scale {
				row[x*scale+sx] = v
			}
		}
		for sy := range scale {
			grid[y*scale+sy] = slices.Clone(row)
		}
	}
	return grid
}

// upscaleGrid repeats every cell n times in both dimensions.
func upscaleGrid(grid [][]bool, n int) [][]bool {
	scaled := make([][]bool, 0, len(grid)*n)
	for _, row := range grid {
		wide := make([]bool, 0, len(row)*n)
		for _, v := range row {
			for range n {
				wide = append(wide, v)
			}
		}
		for range n {
			scaled = append(scaled, slices.Clone(wide))
		}
	}
	return scaled
}

// gridToImage draws the grid as a two-colour paletted image, which is both the
// smallest thing to PNG-encode and the fast path in the sixel encoder.
func gridToImage(grid [][]bool, st style) *image.Paletted {
	h := len(grid)
	w := 0
	if h > 0 {
		w = len(grid[0])
	}
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{st.light, st.dark})
	for y, row := range grid {
		for x, dark := range row {
			if dark {
				img.SetColorIndex(x, y, 1)
			}
		}
	}
	return img
}

func renderUnicode(w io.Writer, grid [][]bool, st style) error {
	bw := bufio.NewWriter(w)
	h := len(grid)
	for y := 0; y < h; y += 2 {
		colored := writeTerminalColor(bw, st)
		for x := range grid[y] {
			top := grid[y][x]
			bot := false
			if y+1 < h {
				bot = grid[y+1][x]
			}
			switch {
			case top && bot:
				bw.WriteRune(uBlockFull)
			case top:
				bw.WriteRune(uBlockUpper)
			case bot:
				bw.WriteRune(uBlockLower)
			default:
				bw.WriteRune(uBlockNone)
			}
		}
		if colored {
			bw.WriteString(ansiReset)
		}
		bw.WriteByte('\n')
	}
	return bw.Flush()
}

func renderANSI(w io.Writer, grid [][]bool, st style) error {
	bw := bufio.NewWriter(w)
	dark := ansiBackground(st.dark)
	light := ansiBackground(st.light)
	for _, row := range grid {
		// The escape only has to be written where the colour changes.
		current := ""
		for _, isDark := range row {
			want := light
			if isDark {
				want = dark
			}
			if want != current {
				bw.WriteString(want)
				current = want
			}
			bw.WriteString("  ")
		}
		bw.WriteString(ansiReset)
		bw.WriteByte('\n')
	}
	return bw.Flush()
}

// writeTerminalColor emits the foreground/background pair for the half-block
// renderer, and reports whether it wrote anything: with the default colours the
// output stays plain text that inherits the terminal's own palette.
func writeTerminalColor(bw *bufio.Writer, st style) bool {
	if st.dark == colorBlack && st.light == colorWhite {
		return false
	}
	fmt.Fprintf(bw, "\x1b[38;2;%d;%d;%dm", st.dark.R, st.dark.G, st.dark.B)
	if st.light.A != 0 {
		fmt.Fprintf(bw, "\x1b[48;2;%d;%d;%dm", st.light.R, st.light.G, st.light.B)
	}
	return true
}

// ansiBackground selects a background colour, or resets to the terminal's own
// when the colour is transparent.
func ansiBackground(c color.NRGBA) string {
	if c.A == 0 {
		return ansiReset
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", c.R, c.G, c.B)
}

func renderSixel(w io.Writer, grid [][]bool, st style) error {
	enc := sixel.NewEncoder(w)
	return enc.Encode(padToSixelBand(gridToImage(grid, st)))
}

// padToSixelBand grows img downwards to a whole six-pixel sixel band. Rows
// past the image height are never encoded, so the terminal paints them in its
// own background colour — a dark bar under the code on a dark terminal.
// Padding them with light modules turns that bar into quiet zone.
func padToSixelBand(img *image.Paletted) *image.Paletted {
	h := img.Rect.Dy()
	if h%6 == 0 {
		return img
	}
	// Palette index 0 is the light colour, so the padding needs no filling.
	padded := image.NewPaletted(image.Rect(0, 0, img.Rect.Dx(), h+6-h%6), img.Palette)
	copy(padded.Pix, img.Pix)
	return padded
}

func renderPNG(w io.Writer, grid [][]bool, st style) error {
	return png.Encode(w, gridToImage(grid, st))
}

// renderSVG writes the code as vector paths. The grid must be unscaled — one
// cell per module — because SVG scales by itself; st.scale becomes the pixel
// size of a module in the width and height attributes.
func renderSVG(w io.Writer, grid [][]bool, st style) error {
	h := len(grid)
	width := 0
	if h > 0 {
		width = len(grid[0])
	}

	bw := bufio.NewWriter(w)
	fmt.Fprintf(bw, `<?xml version="1.0" encoding="UTF-8"?>`+"\n")
	fmt.Fprintf(bw, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" shape-rendering="crispEdges">`+"\n",
		width*st.scale, h*st.scale, width, h)
	if st.light.A != 0 {
		fmt.Fprintf(bw, `<rect width="%d" height="%d" fill="%s" fill-opacity="%s"/>`+"\n",
			width, h, hex(st.light), opacity(st.light))
	}
	if st.dark.A != 0 {
		fmt.Fprintf(bw, `<path fill="%s" fill-opacity="%s" d="`, hex(st.dark), opacity(st.dark))
		// One horizontal run of dark modules per subpath keeps the file small.
		for y, row := range grid {
			for x := 0; x < len(row); x++ {
				if !row[x] {
					continue
				}
				run := 1
				for x+run < len(row) && row[x+run] {
					run++
				}
				fmt.Fprintf(bw, "M%d %dh%dv1h-%dz", x, y, run, run)
				x += run - 1
			}
		}
		bw.WriteString(`"/>` + "\n")
	}
	bw.WriteString("</svg>\n")
	return bw.Flush()
}

func render(w io.Writer, format string, matrix *gozxing.BitMatrix, st style) error {
	// SVG is the one format that carries its own scaling, so it takes the
	// matrix at its natural size.
	if format == "svg" {
		return renderSVG(w, matrixToGrid(matrix, 1, st.invert), st)
	}

	grid := matrixToGrid(matrix, st.scale, st.invert)
	switch format {
	case "unicode":
		return renderUnicode(w, grid, st)
	case "ansi":
		return renderANSI(w, grid, st)
	case "sixel":
		return renderSixel(w, grid, st)
	case "png":
		return renderPNG(w, grid, st)
	default:
		return fmt.Errorf("unknown output format %q", format)
	}
}
