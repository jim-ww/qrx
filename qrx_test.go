package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
)

// monochrome is the default black-on-white style.
var monochrome = style{scale: 1, dark: colorBlack, light: colorWhite}

// roundTrip encodes data as a PNG QR code and decodes it back, exercising the
// full encode/render/decode path the way the command line does.
func roundTrip(t *testing.T, data []byte, level string, margin, scale int) []byte {
	t.Helper()

	matrix, err := encodeQR(data, level, margin, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}
	var buf bytes.Buffer
	if err := render(&buf, "png", matrix, style{scale: scale, dark: colorBlack, light: colorWhite}); err != nil {
		t.Fatalf("render png: %v", err)
	}
	codes, err := decodeImage(&buf)
	if err != nil {
		t.Fatalf("decodeImage: %v", err)
	}
	if len(codes) != 1 {
		t.Fatalf("decodeImage found %d codes, want 1", len(codes))
	}
	if codes[0].format != gozxing.BarcodeFormat_QR_CODE {
		t.Errorf("format = %v, want QR_CODE", codes[0].format)
	}
	return codes[0].data
}

func TestRoundTrip(t *testing.T) {
	allBytes := make([]byte, 256)
	for i := range allBytes {
		allBytes[i] = byte(i)
	}

	tests := []struct {
		name string
		data []byte
	}{
		{"ascii", []byte("hello")},
		{"url", []byte("https://example.com/a?b=c&d=e")},
		{"utf8", []byte("привет")},
		{"binary", []byte{0x00, 0x01, 0xfe, 0xff, 0x80, 0x7f}},
		{"all bytes", allBytes},
		{"long", bytes.Repeat([]byte("qrx"), 300)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := roundTrip(t, tt.data, "M", 2, 8); !bytes.Equal(got, tt.data) {
				t.Errorf("round trip = %q, want %q", got, tt.data)
			}
		})
	}
}

func TestRoundTripLevels(t *testing.T) {
	data := []byte("error correction")
	for _, level := range []string{"L", "M", "Q", "H"} {
		t.Run(level, func(t *testing.T) {
			if got := roundTrip(t, data, level, 2, 8); !bytes.Equal(got, data) {
				t.Errorf("round trip = %q, want %q", got, data)
			}
		})
	}
}

// A zero quiet zone is legal input even though the spec asks for four
// modules; the round trip must still hold.
func TestRoundTripMargins(t *testing.T) {
	data := []byte("margin")
	for _, margin := range []int{0, 1, 4, 10} {
		t.Run(strconv.Itoa(margin), func(t *testing.T) {
			if got := roundTrip(t, data, "M", margin, 8); !bytes.Equal(got, data) {
				t.Errorf("round trip = %q, want %q", got, data)
			}
		})
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"L", "L", false},
		{"m", "M", false},
		{"Q", "Q", false},
		{"h", "H", false},
		{"", "", true},
		{"X", "", true},
		{"MM", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseLevel(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLevel(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestToLatin1String(t *testing.T) {
	got := toLatin1String([]byte{0x00, 0x41, 0x80, 0xff})
	want := "\x00A\u0080\u00ff"
	if got != want {
		t.Errorf("toLatin1String = %q, want %q", got, want)
	}
}

func TestMatrixToGrid(t *testing.T) {
	matrix, err := encodeQR([]byte("scale"), "M", 0, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}
	const scale = 3
	w, h := matrix.GetWidth(), matrix.GetHeight()
	grid := matrixToGrid(matrix, scale, false)

	if len(grid) != h*scale {
		t.Fatalf("grid height = %d, want %d", len(grid), h*scale)
	}
	for y, row := range grid {
		if len(row) != w*scale {
			t.Fatalf("row %d width = %d, want %d", y, len(row), w*scale)
		}
		for x, got := range row {
			if want := matrix.Get(x/scale, y/scale); got != want {
				t.Fatalf("grid[%d][%d] = %v, want %v", y, x, got, want)
			}
		}
	}

	// Rows must not alias: writing to one scaled copy must not affect another.
	grid[0][0] = !grid[0][0]
	if grid[0][0] == grid[1][0] {
		t.Error("scaled rows share a backing array")
	}
}

func TestMatrixToGridInvert(t *testing.T) {
	matrix, err := encodeQR([]byte("invert"), "M", 1, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}
	plain := matrixToGrid(matrix, 1, false)
	inverted := matrixToGrid(matrix, 1, true)

	for y := range plain {
		for x := range plain[y] {
			if plain[y][x] == inverted[y][x] {
				t.Fatalf("cell [%d][%d] = %v in both the plain and inverted grid", y, x, plain[y][x])
			}
		}
	}
}

// A sixel band is six pixels tall; a short final band shows through as the
// terminal background instead of quiet zone.
func TestRenderSixelPadsToWholeBand(t *testing.T) {
	for _, h := range []int{1, 5, 6, 7, 12, 13} {
		t.Run(strconv.Itoa(h), func(t *testing.T) {
			grid := make([][]bool, h)
			for y := range grid {
				grid[y] = []bool{true, false, true}
			}

			var buf bytes.Buffer
			if err := renderSixel(&buf, grid, monochrome); err != nil {
				t.Fatalf("renderSixel: %v", err)
			}

			// Raster attributes: "1;1;<width>;<height>
			_, attrs, ok := strings.Cut(buf.String(), `"1;1;`)
			if !ok {
				t.Fatal("no raster attributes in sixel output")
			}
			attrs, _, _ = strings.Cut(attrs, "#")
			_, gotHeight, ok := strings.Cut(attrs, ";")
			if !ok {
				t.Fatalf("malformed raster attributes %q", attrs)
			}
			got, err := strconv.Atoi(strings.TrimSpace(gotHeight))
			if err != nil {
				t.Fatalf("raster height %q: %v", gotHeight, err)
			}
			if got%6 != 0 || got < h || got >= h+6 {
				t.Errorf("sixel height = %d, want the multiple of 6 at or just above %d", got, h)
			}
		})
	}
}

func TestRenderFormats(t *testing.T) {
	matrix, err := encodeQR([]byte("render"), "M", 1, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}

	for format := range defaultScales {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf, format, matrix, style{scale: 1, dark: colorBlack, light: colorWhite}); err != nil {
				t.Fatalf("render(%q): %v", format, err)
			}
			if buf.Len() == 0 {
				t.Errorf("render(%q) wrote nothing", format)
			}
		})
	}

	t.Run("unknown", func(t *testing.T) {
		if err := render(&bytes.Buffer{}, "jpeg", matrix, style{scale: 1, dark: colorBlack, light: colorWhite}); err == nil {
			t.Error("render with unknown format = nil, want error")
		}
	})
}

func TestRenderUnicode(t *testing.T) {
	var buf bytes.Buffer
	grid := [][]bool{
		{true, false, true, false},
		{false, true, true, false},
		{true, false, false, true},
	}
	if err := renderUnicode(&buf, grid, monochrome); err != nil {
		t.Fatalf("renderUnicode: %v", err)
	}
	want := "▀▄█ \n▀  ▀\n"
	if got := buf.String(); got != want {
		t.Errorf("renderUnicode = %q, want %q", got, want)
	}
}

func TestDecodeImageErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
	}{
		{"empty", nil},
		{"not an image", []byte("this is not an image")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := decodeImage(bytes.NewReader(tt.in)); err == nil {
				t.Error("decodeImage = nil error, want error")
			}
		})
	}
}

func TestDecodeImageNoCode(t *testing.T) {
	// A valid PNG that contains no QR code.
	var buf bytes.Buffer
	if err := renderPNG(&buf, [][]bool{{false, true}, {true, false}}, monochrome); err != nil {
		t.Fatalf("renderPNG: %v", err)
	}
	if _, err := decodeImage(&buf); !errors.Is(err, errNoCode) {
		t.Errorf("decodeImage of a codeless image = %v, want errNoCode", err)
	}
}

func TestParseColor(t *testing.T) {
	tests := []struct {
		in      string
		want    color.NRGBA
		wantErr bool
	}{
		{"black", colorBlack, false},
		{"WHITE", colorWhite, false},
		{"none", colorTransparent, false},
		{"#000000", colorBlack, false},
		{"ffffff", colorWhite, false},
		{"#abc", color.NRGBA{R: 0xaa, G: 0xbb, B: 0xcc, A: 0xff}, false},
		{"#1e3a8a", color.NRGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0xff}, false},
		{"#ff000080", color.NRGBA{R: 0xff, A: 0x80}, false},
		{"#0f08", color.NRGBA{R: 0x00, G: 0xff, B: 0x00, A: 0x88}, false},
		{"", color.NRGBA{}, true},
		{"#12345", color.NRGBA{}, true},
		{"#gggggg", color.NRGBA{}, true},
		{"blue", color.NRGBA{}, true},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseColor(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseColor(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parseColor(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Colour must survive into the image, and a coloured code must still scan.
func TestRenderPNGColors(t *testing.T) {
	matrix, err := encodeQR([]byte("coloured"), "M", 4, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}
	navy := color.NRGBA{R: 0x1e, G: 0x3a, B: 0x8a, A: 0xff}

	var buf bytes.Buffer
	if err := render(&buf, "png", matrix, style{scale: 8, dark: navy, light: colorWhite}); err != nil {
		t.Fatalf("render: %v", err)
	}
	raw := buf.Bytes()

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	if got := color.NRGBAModel.Convert(img.At(0, 0)); got != colorWhite {
		t.Errorf("quiet zone = %v, want white", got)
	}
	// The top-left finder pattern starts one module past the quiet zone.
	if got := color.NRGBAModel.Convert(img.At(4*8+1, 4*8+1)); got != navy {
		t.Errorf("dark module = %v, want %v", got, navy)
	}

	codes, err := decodeImage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("decodeImage of a coloured code: %v", err)
	}
	if got := codes[0].data; string(got) != "coloured" {
		t.Errorf("decoded %q, want %q", got, "coloured")
	}
}

func TestRenderSVG(t *testing.T) {
	matrix, err := encodeQR([]byte("vector"), "M", 4, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}
	const scale = 10
	modules := matrix.GetWidth()

	var buf bytes.Buffer
	if err := render(&buf, "svg", matrix, style{scale: scale, dark: colorBlack, light: colorWhite}); err != nil {
		t.Fatalf("render svg: %v", err)
	}
	out := buf.String()

	// Well-formed, and the viewBox is in modules while width/height are pixels.
	if err := xml.Unmarshal(buf.Bytes(), new(any)); err != nil {
		t.Fatalf("svg is not well-formed XML: %v", err)
	}
	for _, want := range []string{
		fmt.Sprintf(`width="%d"`, modules*scale),
		fmt.Sprintf(`height="%d"`, modules*scale),
		fmt.Sprintf(`viewBox="0 0 %d %d"`, modules, modules),
		`fill="#000000"`,
		`<path`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("svg output missing %s", want)
		}
	}
}

func TestRenderSVGTransparentBackground(t *testing.T) {
	matrix, err := encodeQR([]byte("transparent"), "M", 4, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}

	var buf bytes.Buffer
	if err := render(&buf, "svg", matrix, style{scale: 8, dark: colorBlack, light: colorTransparent}); err != nil {
		t.Fatalf("render svg: %v", err)
	}
	if strings.Contains(buf.String(), "<rect") {
		t.Error("svg with a transparent background still painted a background rect")
	}
}

func TestEncodeQRVersion(t *testing.T) {
	// A pinned version fixes the symbol size regardless of how little data
	// there is: version 1 is 21 modules, and each version adds 4.
	for _, version := range []int{1, 5, 12, 40} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			matrix, err := encodeQR([]byte("v"), "M", 0, version)
			if err != nil {
				t.Fatalf("encodeQR: %v", err)
			}
			if want := 17 + 4*version; matrix.GetWidth() != want {
				t.Errorf("version %d is %d modules wide, want %d", version, matrix.GetWidth(), want)
			}
		})
	}

	t.Run("too small", func(t *testing.T) {
		if _, err := encodeQR(bytes.Repeat([]byte("x"), 100), "H", 0, 1); err == nil {
			t.Error("encodeQR with data too big for -v 1 = nil error, want error")
		}
	})

	t.Run("automatic", func(t *testing.T) {
		matrix, err := encodeQR([]byte("v"), "M", 0, 0)
		if err != nil {
			t.Fatalf("encodeQR: %v", err)
		}
		if matrix.GetWidth() != 21 {
			t.Errorf("automatic version = %d modules, want the smallest (21)", matrix.GetWidth())
		}
	})
}

// composeCodes lays several PNG-encoded matrices out side by side on a white
// canvas, the way a photo of a page of stickers would.
func composeCodes(t *testing.T, st style, texts ...string) image.Image {
	t.Helper()

	const gap = 24
	var images []*image.Paletted
	width, height := gap, 0
	for _, text := range texts {
		matrix, err := encodeQR([]byte(text), "M", 4, 0)
		if err != nil {
			t.Fatalf("encodeQR: %v", err)
		}
		img := gridToImage(matrixToGrid(matrix, st.scale, st.invert), st)
		images = append(images, img)
		width += img.Rect.Dx() + gap
		height = max(height, img.Rect.Dy()+2*gap)
	}

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{st.light}, image.Point{}, draw.Src)
	x := gap
	for _, img := range images {
		r := image.Rect(x, gap, x+img.Rect.Dx(), gap+img.Rect.Dy())
		draw.Draw(canvas, r, img, image.Point{}, draw.Src)
		x += img.Rect.Dx() + gap
	}
	return canvas
}

func TestDecodeImageMultipleCodes(t *testing.T) {
	want := []string{"first", "second", "third"}
	canvas := composeCodes(t, style{scale: 6, dark: colorBlack, light: colorWhite}, want...)

	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		t.Fatalf("png.Encode: %v", err)
	}

	codes, err := decodeImage(&buf)
	if err != nil {
		t.Fatalf("decodeImage: %v", err)
	}
	got := make([]string, 0, len(codes))
	for _, c := range codes {
		got = append(got, string(c.data))
	}
	slices.Sort(got)
	if want := []string{"first", "second", "third"}; !slices.Equal(got, want) {
		t.Errorf("decoded %q, want %q (in any order)", got, want)
	}
}

// -i produces a light-on-dark code, which the ALSO_INVERTED hint must handle.
func TestDecodeImageInverted(t *testing.T) {
	matrix, err := encodeQR([]byte("inverted"), "M", 4, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}

	var buf bytes.Buffer
	if err := render(&buf, "png", matrix, style{scale: 8, invert: true, dark: colorBlack, light: colorWhite}); err != nil {
		t.Fatalf("render: %v", err)
	}

	codes, err := decodeImage(&buf)
	if err != nil {
		t.Fatalf("decodeImage of an inverted code: %v", err)
	}
	if string(codes[0].data) != "inverted" {
		t.Errorf("decoded %q, want %q", codes[0].data, "inverted")
	}
}

func TestWriteCodes(t *testing.T) {
	tests := []struct {
		name  string
		codes []code
		want  string
	}{
		{"single code is verbatim", []code{{data: []byte("one")}}, "one"},
		{"binary stays intact", []code{{data: []byte{0x00, 0xff}}}, "\x00\xff"},
		{"several codes are newline terminated", []code{{data: []byte("a")}, {data: []byte("b")}}, "a\nb\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeCodes(&buf, tt.codes); err != nil {
				t.Fatalf("writeCodes: %v", err)
			}
			if buf.String() != tt.want {
				t.Errorf("writeCodes = %q, want %q", buf.String(), tt.want)
			}
		})
	}
}

// The SVG path must place a rect on exactly the dark modules, so what a
// browser draws is the same symbol the matrix describes.
func TestRenderSVGModules(t *testing.T) {
	matrix, err := encodeQR([]byte("https://youtu.be/dQw4w9WgXcQ"), "M", 4, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}

	var buf bytes.Buffer
	if err := render(&buf, "svg", matrix, style{scale: 8, dark: colorBlack, light: colorWhite}); err != nil {
		t.Fatalf("render svg: %v", err)
	}

	// Each subpath is one horizontal run: M<x> <y>h<run>v1h-<run>z
	runs := regexp.MustCompile(`M(\d+) (\d+)h(\d+)v1h-(\d+)z`).FindAllStringSubmatch(buf.String(), -1)
	if len(runs) == 0 {
		t.Fatal("no module runs in the svg path")
	}

	drawn := make(map[[2]int]bool)
	for _, run := range runs {
		x, _ := strconv.Atoi(run[1])
		y, _ := strconv.Atoi(run[2])
		width, _ := strconv.Atoi(run[3])
		if back, _ := strconv.Atoi(run[4]); back != width {
			t.Fatalf("run at (%d,%d) is %d wide but steps back %d", x, y, width, back)
		}
		for i := range width {
			drawn[[2]int{x + i, y}] = true
		}
	}

	for y := range matrix.GetHeight() {
		for x := range matrix.GetWidth() {
			if got, want := drawn[[2]int{x, y}], matrix.Get(x, y); got != want {
				t.Fatalf("module (%d,%d) drawn = %v, want %v", x, y, got, want)
			}
		}
	}
}

// Data that fits no symbol at all is a different message from data that does
// not fit the version that was asked for.
func TestEncodeQRDataTooBig(t *testing.T) {
	// The largest QR code holds under 3 KB, and less at level H.
	tooMuch := bytes.Repeat([]byte("x"), 4000)

	t.Run("without a pinned version", func(t *testing.T) {
		_, err := encodeQR(tooMuch, "M", 4, 0)
		if err == nil {
			t.Fatal("encodeQR = nil error, want error")
		}
		if !strings.Contains(err.Error(), "-l") || strings.Contains(err.Error(), "-v") {
			t.Errorf("error %q should suggest -l and not -v, which was not given", err)
		}
	})

	t.Run("with a pinned version", func(t *testing.T) {
		_, err := encodeQR(tooMuch, "M", 4, 40)
		if err == nil {
			t.Fatal("encodeQR = nil error, want error")
		}
		if !strings.Contains(err.Error(), "-v") {
			t.Errorf("error %q should suggest a larger -v", err)
		}
	})
}

// A transparent background leaves the terminal's own colour showing, which is
// a reset rather than a background colour.
func TestRenderANSITransparentBackground(t *testing.T) {
	matrix, err := encodeQR([]byte("transparent"), "M", 2, 0)
	if err != nil {
		t.Fatalf("encodeQR: %v", err)
	}

	var buf bytes.Buffer
	if err := render(&buf, "ansi", matrix, style{scale: 1, dark: colorBlack, light: colorTransparent}); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, "\x1b[48;2;0;0;0m") {
		t.Error("the dark modules lost their colour")
	}
	if strings.Contains(out, "\x1b[48;2;255;255;255m") {
		t.Error("a transparent background was painted white")
	}

	// It still has to decode: the parser reads a reset as light.
	var decoded bytes.Buffer
	if err := run([]string{"-d"}, &buf, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.String() != "transparent" {
		t.Errorf("decoded %q, want %q", decoded.String(), "transparent")
	}
}
