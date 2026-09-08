package main

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/datamatrix"
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

// symbologies are the types -t accepts. qr is the default; it and datamatrix
// are the 2D entries, and the only ones that carry arbitrary bytes.
var symbologies = map[string]symbology{
	"qr": {format: gozxing.BarcodeFormat_QR_CODE, accepts: "any bytes"},
	"datamatrix": {
		format:    gozxing.BarcodeFormat_DATA_MATRIX,
		newWriter: datamatrix.NewDataMatrixWriter,
		accepts:   "text; use -t qr for binary",
	},
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
	return padMatrix(matrix, 0, margin)
}

// encodeDataMatrix encodes data as a Data Matrix symbol surrounded by margin
// modules of quiet zone.
//
// The content goes through toLatin1String for the same reason it does for QR:
// the encoder runs the string through an ISO-8859-1 encoder before it starts,
// so every byte has to arrive as the code point of the same value. The writer
// has no MARGIN hint, so the quiet zone is ours to add.
//
// Text is safe here, but binary is not: content that pushes the encoder into
// Base256 encodation does not survive a round trip through gozxing v0.1.1.
// The decoded data comes back with a leading zero byte and trailing pad bytes,
// or empty for very short input — measured at 40/40 ASCII payloads intact
// against 27/40 random binary ones. Whether the writer or the reader is at
// fault is not visible from here.
//
// Silently printing a label that scans as something else is the worst way to
// lose, so the symbol is read back before it is returned and content that does
// not survive is refused. Only Data Matrix pays for this; the other
// symbologies are sound.
func encodeDataMatrix(data []byte, margin int) (*gozxing.BitMatrix, error) {
	sym := symbologies["datamatrix"]
	matrix, err := sym.newWriter().Encode(toLatin1String(data), sym.format, 0, 0, nil)
	if err != nil {
		return nil, barcodeError(sym, data, err)
	}
	if err := verifyDataMatrix(matrix, data); err != nil {
		return nil, err
	}
	return padMatrix(matrix, margin, margin)
}

// verifyDataMatrix decodes the symbol just encoded and checks it still says
// what it was given. See encodeDataMatrix for why this is worth the work.
func verifyDataMatrix(matrix *gozxing.BitMatrix, data []byte) error {
	// A generous quiet zone and scale, so a failure here means the symbol is
	// wrong rather than merely hard to read.
	var buf bytes.Buffer
	padded, err := padMatrix(matrix, 4, 4)
	if err != nil {
		return err
	}
	if err := render(&buf, "png", padded, style{scale: 6, dark: colorBlack, light: colorWhite}); err != nil {
		return err
	}

	codes, err := decodeImage(&buf)
	if err == nil && bytes.Equal(codes[0].data, data) {
		return nil
	}
	return errors.New("encode: a data matrix of this content does not read back as what went in; use -t qr for binary data")
}

// padMatrix surrounds the symbol with quiet zone: x modules on the left and
// right, y modules on the top and bottom. The 1D writers pad the sides
// themselves but not the top and bottom, and the Data Matrix writer pads
// neither.
func padMatrix(m *gozxing.BitMatrix, x, y int) (*gozxing.BitMatrix, error) {
	if x == 0 && y == 0 {
		return m, nil
	}
	w, h := m.GetWidth(), m.GetHeight()
	padded, err := gozxing.NewBitMatrix(w+2*x, h+2*y)
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	for row := range h {
		for col := range w {
			if m.Get(col, row) {
				padded.Set(col+x, row+y)
			}
		}
	}
	return padded, nil
}

// barcodeError turns a writer complaint into something that says what this
// symbology actually wants.
func barcodeError(sym symbology, data []byte, err error) error {
	msg := encodeErrorMessage(err)
	if bytes.HasSuffix(data, []byte("\n")) {
		return fmt.Errorf("encode: %s (%s takes %s; the input ends with a newline, try printf or echo -n)",
			msg, sym.name, sym.accepts)
	}
	return fmt.Errorf("encode: %s (%s takes %s)", msg, sym.name, sym.accepts)
}
