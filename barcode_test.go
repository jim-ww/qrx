package main

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
)

// Every 1D symbology must survive encode -> PNG -> decode. A few decoders
// normalise what they return, which the want column spells out.
func TestBarcodeRoundTrip(t *testing.T) {
	tests := []struct {
		symbology string
		content   string
		want      string
		format    gozxing.BarcodeFormat
	}{
		{"code128", "ABC-123", "ABC-123", gozxing.BarcodeFormat_CODE_128},
		{"code39", "ABC123", "ABC123", gozxing.BarcodeFormat_CODE_39},
		{"code93", "ABC123", "ABC123", gozxing.BarcodeFormat_CODE_93},
		// The reader drops the A-D start/stop characters by default.
		{"codabar", "A123456A", "123456", gozxing.BarcodeFormat_CODABAR},
		{"ean8", "96385074", "96385074", gozxing.BarcodeFormat_EAN_8},
		{"ean13", "5901234123457", "5901234123457", gozxing.BarcodeFormat_EAN_13},
		{"upca", "036000291452", "036000291452", gozxing.BarcodeFormat_UPC_A},
		{"upce", "01234565", "01234565", gozxing.BarcodeFormat_UPC_E},
		{"itf", "1234567890", "1234567890", gozxing.BarcodeFormat_ITF},
	}

	for _, tt := range tests {
		t.Run(tt.symbology, func(t *testing.T) {
			matrix, err := encodeBarcode([]byte(tt.content), symbologies[tt.symbology], oneDQuietZone, 0)
			if err != nil {
				t.Fatalf("encodeBarcode: %v", err)
			}

			var buf bytes.Buffer
			if err := render(&buf, "png", matrix, style{scale: 3, dark: colorBlack, light: colorWhite}); err != nil {
				t.Fatalf("render: %v", err)
			}
			codes, err := decodeImage(&buf)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if len(codes) != 1 {
				t.Fatalf("decoded %d codes, want 1", len(codes))
			}
			if got := string(codes[0].data); got != tt.want {
				t.Errorf("decoded %q, want %q", got, tt.want)
			}
			if codes[0].format != tt.format {
				t.Errorf("format = %v, want %v", codes[0].format, tt.format)
			}
		})
	}
}

func TestEncodeBarcodeInvalidContent(t *testing.T) {
	tests := []struct {
		name      string
		symbology string
		content   string
		wantHint  string
	}{
		{"short ean13", "ean13", "12345", "12 digits"},
		{"odd itf", "itf", "123", "even number"},
		{"letters in ean8", "ean8", "abcdefgh", "7 digits"},
		{"empty", "code128", "", "printable ASCII"},
		{"trailing newline", "ean13", "5901234123457\n", "echo -n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodeBarcode([]byte(tt.content), symbologies[tt.symbology], oneDQuietZone, 0)
			if err == nil {
				t.Fatal("encodeBarcode = nil error, want error")
			}
			if !strings.Contains(err.Error(), tt.wantHint) {
				t.Errorf("error %q does not mention %q", err, tt.wantHint)
			}
			// gozxing's Java exception names must not reach the user.
			if strings.Contains(err.Error(), "IllegalArgumentException") {
				t.Errorf("error %q still carries the Java exception name", err)
			}
		})
	}
}

// The quiet zone surrounds the symbol on every side, and -m counts modules per
// side, not in total.
func TestEncodeBarcodeQuietZone(t *testing.T) {
	const margin = 7
	matrix, err := encodeBarcode([]byte("5901234123457"), symbologies["ean13"], margin, 10)
	if err != nil {
		t.Fatalf("encodeBarcode: %v", err)
	}

	darkInRow := func(y int) int {
		n := 0
		for x := range matrix.GetWidth() {
			if matrix.Get(x, y) {
				n++
			}
		}
		return n
	}
	darkInColumn := func(x int) int {
		n := 0
		for y := range matrix.GetHeight() {
			if matrix.Get(x, y) {
				n++
			}
		}
		return n
	}

	for y := range margin {
		if darkInRow(y) != 0 || darkInRow(matrix.GetHeight()-1-y) != 0 {
			t.Fatalf("row %d of the vertical quiet zone is not empty", y)
		}
	}
	if darkInRow(margin) == 0 {
		t.Error("the symbol does not start right after the quiet zone")
	}
	for x := range margin {
		if darkInColumn(x) != 0 || darkInColumn(matrix.GetWidth()-1-x) != 0 {
			t.Fatalf("column %d of the side quiet zone is not empty", x)
		}
	}
}

func TestEncodeBarcodeHeight(t *testing.T) {
	content := []byte("5901234123457")

	t.Run("explicit", func(t *testing.T) {
		for _, h := range []int{1, 5, 40} {
			t.Run(strconv.Itoa(h), func(t *testing.T) {
				matrix, err := encodeBarcode(content, symbologies["ean13"], 0, h)
				if err != nil {
					t.Fatalf("encodeBarcode: %v", err)
				}
				if matrix.GetHeight() != h {
					t.Errorf("height = %d, want %d", matrix.GetHeight(), h)
				}
			})
		}
	})

	t.Run("automatic follows the width", func(t *testing.T) {
		matrix, err := encodeBarcode(content, symbologies["ean13"], 0, 0)
		if err != nil {
			t.Fatalf("encodeBarcode: %v", err)
		}
		if want := max(minBarHeight, matrix.GetWidth()/5); matrix.GetHeight() != want {
			t.Errorf("height = %d, want %d", matrix.GetHeight(), want)
		}
	})
}

func TestSymbologyNames(t *testing.T) {
	got := symbologyNames()
	for name := range symbologies {
		if !strings.Contains(got, name) {
			t.Errorf("symbologyNames() = %q, missing %q", got, name)
		}
		if symbologies[name].name != name {
			t.Errorf("symbology %q carries the name %q", name, symbologies[name].name)
		}
	}
}
