package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
)

// Text round trips through Data Matrix exactly. Binary deliberately is not
// tested: gozxing's Base256 encodation does not survive the round trip, which
// is why qrx documents Data Matrix as a text symbology — see encodeDataMatrix.
func TestDataMatrixRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"ascii", []byte("hello")},
		{"digits", []byte("0123456789")},
		{"punctuation", []byte("PN:4815162342/REV-B")},
		{"latin-1 text", []byte("caf\u00e9 na\u00efve")},
		{"mixed case and digits", []byte("Part7Number9Xyz")},
		{"long", bytes.Repeat([]byte("qrx"), 300)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matrix, err := encodeDataMatrix(tt.data, 4)
			if err != nil {
				t.Fatalf("encodeDataMatrix: %v", err)
			}

			var buf bytes.Buffer
			if err := render(&buf, "png", matrix, style{scale: 6, dark: colorBlack, light: colorWhite}); err != nil {
				t.Fatalf("render: %v", err)
			}
			codes, err := decodeImage(&buf)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if codes[0].format != gozxing.BarcodeFormat_DATA_MATRIX {
				t.Errorf("format = %v, want DATA_MATRIX", codes[0].format)
			}
			if !bytes.Equal(codes[0].data, tt.data) {
				t.Errorf("round trip = %q, want %q", codes[0].data, tt.data)
			}
		})
	}
}

func TestDataMatrixQuietZone(t *testing.T) {
	const margin = 5
	bare, err := encodeDataMatrix([]byte("quiet"), 0)
	if err != nil {
		t.Fatalf("encodeDataMatrix: %v", err)
	}
	padded, err := encodeDataMatrix([]byte("quiet"), margin)
	if err != nil {
		t.Fatalf("encodeDataMatrix: %v", err)
	}

	// The writer has no margin of its own, so qrx adds it on all four sides.
	if got, want := padded.GetWidth(), bare.GetWidth()+2*margin; got != want {
		t.Errorf("width = %d, want %d", got, want)
	}
	if got, want := padded.GetHeight(), bare.GetHeight()+2*margin; got != want {
		t.Errorf("height = %d, want %d", got, want)
	}
	for y := range margin {
		for x := range padded.GetWidth() {
			if padded.Get(x, y) || padded.Get(x, padded.GetHeight()-1-y) {
				t.Fatalf("quiet zone row %d is not empty", y)
			}
		}
	}
}

func TestEncodeDataMatrixEmpty(t *testing.T) {
	if _, err := encodeDataMatrix(nil, 4); err == nil {
		t.Error("encodeDataMatrix with no data = nil error, want error")
	}
}

func TestFromLatin1String(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []byte
		ok   bool
	}{
		{"empty", "", []byte{}, true},
		{"ascii", "hi", []byte("hi"), true},
		{"high code points", "\x00A\u0080\u00ff", []byte{0x00, 0x41, 0x80, 0xff}, true},
		{"beyond latin-1", "привет", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := fromLatin1String(tt.in)
			if ok != tt.ok {
				t.Fatalf("fromLatin1String(%q) ok = %v, want %v", tt.in, ok, tt.ok)
			}
			if ok && !bytes.Equal(got, tt.want) {
				t.Errorf("fromLatin1String(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// Whatever toLatin1String produces, fromLatin1String must give back.
func TestLatin1RoundTrip(t *testing.T) {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	got, ok := fromLatin1String(toLatin1String(data))
	if !ok {
		t.Fatal("fromLatin1String rejected what toLatin1String produced")
	}
	if !bytes.Equal(got, data) {
		t.Errorf("round trip = %v, want %v", got, data)
	}
}

func TestRunDataMatrix(t *testing.T) {
	const data = "data matrix through the command line"

	var encoded bytes.Buffer
	if err := run([]string{"-t", "datamatrix", "-f", "png", data}, strings.NewReader(""), &encoded); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded bytes.Buffer
	if err := run([]string{"-d"}, &encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.String() != data {
		t.Errorf("decoded %q, want %q", decoded.String(), data)
	}
}

// Data Matrix is 2D, so the QR-only and the 1D-only flags are both wrong for it.
func TestRunDataMatrixUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"-t", "datamatrix", "-l", "H", "data"},
		{"-t", "datamatrix", "-v", "5", "data"},
		{"-t", "datamatrix", "-bh", "20", "data"},
	} {
		t.Run(strings.Join(args[2:4], " "), func(t *testing.T) {
			var ue *usageError
			if err := run(args, strings.NewReader(""), &bytes.Buffer{}); !errors.As(err, &ue) {
				t.Errorf("run(%q) error = %v, want *usageError", args, err)
			}
		})
	}
}

// Content that gozxing would mangle must be refused, not printed: a label that
// scans as something else is worse than no label. Binary that happens to
// survive is still accepted — the guard is the round trip itself, not a guess
// about which bytes are safe.
func TestEncodeDataMatrixRefusesMangledContent(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"binary", []byte{0x00, 0x01, 0xfe, 0xff, 0x80, 0x7f}},
		{"high bytes", bytes.Repeat([]byte{0xaa}, 20)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encodeDataMatrix(tt.data, 4)
			if err == nil {
				t.Fatal("encodeDataMatrix accepted content it cannot round trip")
			}
			if !strings.Contains(err.Error(), "-t qr") {
				t.Errorf("error %q does not point at -t qr", err)
			}
		})
	}
}

// Whatever encodeDataMatrix does return is guaranteed to read back.
func TestEncodeDataMatrixAcceptedContentRoundTrips(t *testing.T) {
	for n := 1; n <= 60; n++ {
		data := bytes.Repeat([]byte("Part7Number9Xyz-"), 1+n/16)[:n]
		matrix, err := encodeDataMatrix(data, 4)
		if err != nil {
			t.Fatalf("length %d: encodeDataMatrix: %v", n, err)
		}

		var buf bytes.Buffer
		if err := render(&buf, "png", matrix, style{scale: 6, dark: colorBlack, light: colorWhite}); err != nil {
			t.Fatalf("length %d: render: %v", n, err)
		}
		codes, err := decodeImage(&buf)
		if err != nil {
			t.Fatalf("length %d: decodeImage: %v", n, err)
		}
		if !bytes.Equal(codes[0].data, data) {
			t.Errorf("length %d: round trip = %q, want %q", n, codes[0].data, data)
		}
	}
}
