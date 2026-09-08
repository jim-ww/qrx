package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestRunUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"-z"}},
		{"missing flag value", []string{"-f"}},
		{"too many arguments", []string{"one", "two"}},
		{"negative margin", []string{"-m", "-1", "data"}},
		{"negative scale", []string{"-s", "-1", "data"}},
		{"unknown format", []string{"-f", "jpeg", "data"}},
		{"unknown level", []string{"-l", "Z", "data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			err := run(tt.args, strings.NewReader(""), &out)

			var ue *usageError
			if !errors.As(err, &ue) {
				t.Fatalf("run(%q) error = %v, want *usageError", tt.args, err)
			}
			if out.Len() != 0 {
				t.Errorf("run(%q) wrote %d bytes on a usage error, want none", tt.args, out.Len())
			}
		})
	}
}

func TestRunHelp(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"-h"}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("run(-h): %v", err)
	}
	if out.String() != usage {
		t.Error("run(-h) did not write the usage text to stdout")
	}
}

// Bad flags must be rejected before the output file is touched, so that a
// mistyped format cannot destroy an existing file.
func TestRunRejectsBadFlagsBeforeTouchingOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.png")
	const contents = "precious"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-f", "jpeg", "-o", path, "data"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("run with an unknown format = nil error, want error")
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != contents {
		t.Errorf("output file = %q, want it untouched (%q)", got, contents)
	}
}

func TestRunEncodeDecodeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.png")
	const data = "round trip through the command line"

	if err := run([]string{"-f", "png", "-o", path, data}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var out bytes.Buffer
	if err := run([]string{"-d", path}, strings.NewReader(""), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.String() != data {
		t.Errorf("decoded %q, want %q", out.String(), data)
	}
}

func TestRunEncodeFromStdin(t *testing.T) {
	const data = "from stdin"

	var encoded bytes.Buffer
	if err := run([]string{"-f", "png"}, strings.NewReader(data), &encoded); err != nil {
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

// Without -s, PNG must scale up to its own default while terminal formats
// stay at one cell per module.
func TestRunDefaultScale(t *testing.T) {
	var png, unicode bytes.Buffer
	if err := run([]string{"-f", "png", "-m", "0", "x"}, strings.NewReader(""), &png); err != nil {
		t.Fatalf("png: %v", err)
	}
	if err := run([]string{"-f", "unicode", "-m", "0", "x"}, strings.NewReader(""), &unicode); err != nil {
		t.Fatalf("unicode: %v", err)
	}

	matrix, err := encodeQR([]byte("x"), "M", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var want bytes.Buffer
	if err := render(&want, "png", matrix, style{scale: defaultScales["png"], dark: colorBlack, light: colorWhite}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(png.Bytes(), want.Bytes()) {
		t.Error("png output does not match the default scale of 8")
	}

	// One line per two module rows at scale 1.
	if lines := bytes.Count(unicode.Bytes(), []byte("\n")); lines != (matrix.GetHeight()+1)/2 {
		t.Errorf("unicode wrote %d lines, want %d", lines, (matrix.GetHeight()+1)/2)
	}
}

func TestRunInvert(t *testing.T) {
	var plain, inverted bytes.Buffer
	if err := run([]string{"data"}, strings.NewReader(""), &plain); err != nil {
		t.Fatalf("plain: %v", err)
	}
	if err := run([]string{"-i", "data"}, strings.NewReader(""), &inverted); err != nil {
		t.Fatalf("inverted: %v", err)
	}
	if plain.Len() == 0 || plain.String() == inverted.String() {
		t.Error("-i produced the same output as the plain render")
	}
}

func TestRunMissingFile(t *testing.T) {
	err := run([]string{"-d", filepath.Join(t.TempDir(), "nope.png")}, strings.NewReader(""), &bytes.Buffer{})
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("run on a missing file = %v, want os.ErrNotExist", err)
	}
}

func TestRunSVGAndColors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{"svg", []string{"-f", "svg", "data"}, []string{"<svg", `fill="#000000"`, "</svg>"}},
		{"colored svg", []string{"-f", "svg", "-fg", "#1e3a8a", "-bg", "none", "data"}, []string{`fill="#1e3a8a"`}},
		{"scaled svg", []string{"-f", "svg", "-s", "3", "-m", "0", "data"}, []string{`width="63"`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			if err := run(tt.args, strings.NewReader(""), &out); err != nil {
				t.Fatalf("run(%q): %v", tt.args, err)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output missing %q", want)
				}
			}
		})
	}
}

func TestRunColorAndVersionUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"bad fg", []string{"-fg", "chartreuse", "data"}},
		{"bad bg", []string{"-bg", "#12345", "data"}},
		{"version too high", []string{"-v", "41", "data"}},
		{"negative version", []string{"-v", "-1", "data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ue *usageError
			if err := run(tt.args, strings.NewReader(""), &bytes.Buffer{}); !errors.As(err, &ue) {
				t.Errorf("run(%q) error = %v, want *usageError", tt.args, err)
			}
		})
	}
}

// Data that does not fit a pinned version is a data error, not a usage error.
func TestRunVersionTooSmall(t *testing.T) {
	err := run([]string{"-v", "1", "-l", "H", strings.Repeat("x", 100)}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("run(-v 1) with too much data = nil error, want error")
	}
	var ue *usageError
	if errors.As(err, &ue) {
		t.Errorf("error = %v, want a plain error, not a usage error", err)
	}
}

func TestRunColoredRoundTrip(t *testing.T) {
	const data = "coloured round trip"
	var encoded bytes.Buffer
	if err := run([]string{"-f", "png", "-fg", "#1e3a8a", "-bg", "#f8fafc", data}, strings.NewReader(""), &encoded); err != nil {
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

func TestRunBarcode(t *testing.T) {
	const ean = "5901234123457"

	var encoded bytes.Buffer
	if err := run([]string{"-t", "ean13", "-f", "png", ean}, strings.NewReader(""), &encoded); err != nil {
		t.Fatalf("encode: %v", err)
	}

	var decoded bytes.Buffer
	if err := run([]string{"-d"}, &encoded, &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.String() != ean {
		t.Errorf("decoded %q, want %q", decoded.String(), ean)
	}
}

// The 1D symbologies need a wider quiet zone than a QR code, so -m defaults
// differently for them — but an explicit -m always wins.
func TestRunBarcodeDefaultMargin(t *testing.T) {
	widthOf := func(t *testing.T, args ...string) int {
		t.Helper()
		var out bytes.Buffer
		if err := run(append(args, "5901234123457"), strings.NewReader(""), &out); err != nil {
			t.Fatalf("run(%q): %v", args, err)
		}
		// Count runes: the block characters are multi-byte.
		line, _, _ := strings.Cut(out.String(), "\n")
		return utf8.RuneCountInString(line)
	}

	if got, want := widthOf(t, "-t", "ean13"), widthOf(t, "-t", "ean13", "-m", strconv.Itoa(oneDQuietZone)); got != want {
		t.Errorf("default width = %d, want the -m %d width %d", got, oneDQuietZone, want)
	}
	if got, want := widthOf(t, "-t", "ean13", "-m", "0"), widthOf(t, "-t", "ean13"); got >= want {
		t.Errorf("-m 0 width = %d, want less than the default %d", got, want)
	}
}

func TestRunBarcodeUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"unknown symbology", []string{"-t", "qrcode", "data"}},
		{"level is qr only", []string{"-t", "ean13", "-l", "H", "5901234123457"}},
		{"version is qr only", []string{"-t", "ean13", "-v", "5", "5901234123457"}},
		{"bar height is 1D only", []string{"-bh", "20", "data"}},
		{"negative bar height", []string{"-t", "ean13", "-bh", "-1", "5901234123457"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ue *usageError
			if err := run(tt.args, strings.NewReader(""), &bytes.Buffer{}); !errors.As(err, &ue) {
				t.Errorf("run(%q) error = %v, want *usageError", tt.args, err)
			}
		})
	}
}

// Content the symbology cannot hold is a data error, not a usage error.
func TestRunBarcodeBadContent(t *testing.T) {
	err := run([]string{"-t", "ean13", "12345"}, strings.NewReader(""), &bytes.Buffer{})
	if err == nil {
		t.Fatal("run(-t ean13) with 5 digits = nil error, want error")
	}
	var ue *usageError
	if errors.As(err, &ue) {
		t.Errorf("error = %v, want a plain error, not a usage error", err)
	}
}

// A failure while encoding must leave the file named by -o alone: it is
// opened only once the output exists in full.
func TestRunKeepsOutputFileOnFailure(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"content the symbology cannot hold", []string{"-t", "ean13", "12345"}},
		{"data too big for the pinned version", []string{"-v", "1", "-l", "H", strings.Repeat("x", 100)}},
		{"content data matrix would mangle", []string{"-t", "datamatrix", "\x00\x01\xfe\xff\x80\x7f"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "keep.png")
			const contents = "precious"
			if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}

			if err := run(append(tt.args, "-o", path), strings.NewReader(""), &bytes.Buffer{}); err == nil {
				t.Fatal("run = nil error, want error")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != contents {
				t.Errorf("output file = %q, want it untouched (%q)", got, contents)
			}
		})
	}
}

// Decoding failures must not touch it either.
func TestRunKeepsOutputFileOnDecodeFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keep.txt")
	const contents = "precious"
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{"-d", "-o", path}, strings.NewReader("not an image"), &bytes.Buffer{})
	if err == nil {
		t.Fatal("run = nil error, want error")
	}
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != contents {
		t.Errorf("output file = %q, want it untouched (%q)", got, contents)
	}
}

// A code nobody can see is not a code.
func TestRunRejectsInvisibleCodes(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"transparent modules", []string{"-fg", "none", "data"}},
		{"transparent modules by hex", []string{"-fg", "#00000000", "data"}},
		{"same colour twice", []string{"-fg", "white", "-bg", "white", "data"}},
		{"same colour, different spelling", []string{"-fg", "#fff", "-bg", "#ffffff", "data"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ue *usageError
			if err := run(tt.args, strings.NewReader(""), &bytes.Buffer{}); !errors.As(err, &ue) {
				t.Errorf("run(%q) error = %v, want *usageError", tt.args, err)
			}
		})
	}

	// A transparent background is still fine: the modules are what must be seen.
	if err := run([]string{"-f", "png", "-bg", "none", "data"}, strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Errorf("run with a transparent background = %v, want nil", err)
	}
}

// The encoding flags mean nothing when decoding, so they are refused rather
// than quietly ignored.
func TestRunRejectsEncodingFlagsWhenDecoding(t *testing.T) {
	for _, flag := range []string{"-t=qr", "-f=png", "-l=H", "-v=5", "-s=2", "-m=2", "-bh=20", "-i", "-fg=red", "-bg=blue"} {
		t.Run(flag, func(t *testing.T) {
			var ue *usageError
			err := run([]string{"-d", flag}, strings.NewReader(""), &bytes.Buffer{})
			if !errors.As(err, &ue) {
				t.Errorf("run(-d %s) error = %v, want *usageError", flag, err)
			}
		})
	}
}
