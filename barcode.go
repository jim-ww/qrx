package main

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
)

// minBarHeight is the shortest bar worth printing when -bh is not given, in
// modules. Above it the height follows the symbol's own width, since a long
// barcode needs taller bars to stay readable at an angle.
const minBarHeight = 20

// oneDQuietZone is the side quiet zone 1D symbologies expect, in modules. It
// is much wider than the four modules a QR code asks for, so -m defaults to it
// rather than to the QR value when a 1D symbology is selected.
const oneDQuietZone = 10

// symbology is one barcode type qrx can write.
type symbology struct {
	name      string
	format    gozxing.BarcodeFormat
	newWriter func() gozxing.Writer
	oneD      bool
	accepts   string // what the content has to look like, for error messages
}

// symbologies are the types -t accepts. QR is the default and the only 2D
// entry: it is the one that carries arbitrary bytes.
var symbologies = map[string]symbology{
	"qr": {format: gozxing.BarcodeFormat_QR_CODE, accepts: "any bytes"},
	"code128": {format: gozxing.BarcodeFormat_CODE_128, oneD: true,
		newWriter: oned.NewCode128Writer,
		accepts:   "printable ASCII",
	},
	"code39": {format: gozxing.BarcodeFormat_CODE_39, oneD: true,
		newWriter: oned.NewCode39Writer,
		accepts:   "digits, A-Z and -.$/+% or space",
	},
	"code93": {format: gozxing.BarcodeFormat_CODE_93, oneD: true,
		newWriter: oned.NewCode93Writer,
		accepts:   "digits, A-Z and -.$/+% or space",
	},
	"codabar": {format: gozxing.BarcodeFormat_CODABAR, oneD: true,
		newWriter: oned.NewCodaBarWriter,
		accepts:   "digits and -$:/.+, optionally wrapped in A-D start/stop characters",
	},
	"ean8": {format: gozxing.BarcodeFormat_EAN_8, oneD: true,
		newWriter: oned.NewEAN8Writer,
		accepts:   "7 digits, or 8 with the check digit",
	},
	"ean13": {format: gozxing.BarcodeFormat_EAN_13, oneD: true,
		newWriter: oned.NewEAN13Writer,
		accepts:   "12 digits, or 13 with the check digit",
	},
	"upca": {format: gozxing.BarcodeFormat_UPC_A, oneD: true,
		newWriter: oned.NewUPCAWriter,
		accepts:   "11 digits, or 12 with the check digit",
	},
	"upce": {format: gozxing.BarcodeFormat_UPC_E, oneD: true,
		newWriter: oned.NewUPCEWriter,
		accepts:   "7 digits, or 8 with the check digit",
	},
	"itf": {format: gozxing.BarcodeFormat_ITF, oneD: true,
		newWriter: oned.NewITFWriter,
		accepts:   "an even number of digits",
	},
}

// Each symbology knows the -t name it is registered under, for error messages.
func init() {
	for name, sym := range symbologies {
		sym.name = name
		symbologies[name] = sym
	}
}

// symbologyNames lists the -t values in a stable order, for help and errors.
func symbologyNames() string {
	return strings.Join(slices.Sorted(maps.Keys(symbologies)), ", ")
}

// encodeBarcode encodes data as a 1D barcode and returns its module matrix.
// The bars are barHeight modules tall, or a height proportional to the symbol
// when barHeight is 0, and margin modules of quiet zone surround the symbol on
// every side.
func encodeBarcode(data []byte, sym symbology, margin, barHeight int) (*gozxing.BitMatrix, error) {
	content := string(data)
	writer := sym.newWriter()
	// The 1D writers read MARGIN as the combined width of both side quiet
	// zones, while -m is per side everywhere else in qrx, so double it. Getting
	// this wrong halves the quiet zone, and UPC-E in particular then fails to
	// scan at all.
	hints := map[gozxing.EncodeHintType]any{gozxing.EncodeHintType_MARGIN: 2 * margin}

	// The writer decides how wide the symbol is, so ask it once at minimum
	// height before choosing a bar height to go with that width.
	probe, err := writer.Encode(content, sym.format, 0, 1, hints)
	if err != nil {
		return nil, barcodeError(sym, data, err)
	}
	if barHeight == 0 {
		barHeight = max(minBarHeight, probe.GetWidth()/5)
	}

	matrix, err := writer.Encode(content, sym.format, 0, barHeight, hints)
	if err != nil {
		return nil, barcodeError(sym, data, err)
	}
	return padVertically(matrix, margin)
}

// padVertically adds margin rows of quiet zone above and below the symbol. The
// 1D writers only pad the sides, and a barcode flush against the top of a PNG
// scans poorly.
func padVertically(m *gozxing.BitMatrix, margin int) (*gozxing.BitMatrix, error) {
	if margin == 0 {
		return m, nil
	}
	w, h := m.GetWidth(), m.GetHeight()
	padded, err := gozxing.NewBitMatrix(w, h+2*margin)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	for y := range h {
		for x := range w {
			if m.Get(x, y) {
				padded.Set(x, y+margin)
			}
		}
	}
	return padded, nil
}

// barcodeError turns a writer complaint into something that says what this
// symbology actually wants, without gozxing's Java exception names.
func barcodeError(sym symbology, data []byte, err error) error {
	msg := err.Error()
	for _, prefix := range []string{"WriterException: ", "IllegalArgumentException: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	if bytes.HasSuffix(data, []byte("\n")) {
		return fmt.Errorf("encode: %s (%s takes %s; the input ends with a newline, try printf or echo -n)",
			msg, sym.name, sym.accepts)
	}
	return fmt.Errorf("encode: %s (%s takes %s)", msg, sym.name, sym.accepts)
}
