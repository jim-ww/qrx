package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/aztec"
	"github.com/makiuchi-d/gozxing/datamatrix"
	multiqr "github.com/makiuchi-d/gozxing/multi/qrcode"
	"github.com/makiuchi-d/gozxing/oned"
)

// errNoCode is returned when an image holds no barcode any reader recognises.
var errNoCode = errors.New("no barcode found in the image")

// code is one decoded symbol.
type code struct {
	format gozxing.BarcodeFormat
	data   []byte
}

// terminalDecodeScale is how many pixels per module a grid parsed back from
// terminal output is drawn at before being handed to the readers.
const terminalDecodeScale = 4

// decodeImage finds every barcode in the image read from r, trying the
// supported symbologies in turn and, failing those, the inverted image.
func decodeImage(r io.Reader) ([]code, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read input: %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		// Not an image: it may still be a code drawn with terminal characters,
		// which is what qrx itself writes without -f png.
		grid, terr := parseTerminalGrid(data)
		if terr != nil {
			return nil, fmt.Errorf("decode image: %w", err)
		}
		// The readers want more than one pixel per module to work with.
		img = gridToImage(upscaleGrid(grid, terminalDecodeScale), style{dark: colorBlack, light: colorWhite})
	}

	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	if codes := readAll(bitmap); codes != nil {
		return codes, nil
	}

	// Nothing matched, so try again with the image inverted: light-on-dark
	// codes are common in the wild and are what qrx -i produces. gozxing has
	// no MultiFormatReader to do this for us, and its ALSO_INVERTED hint is a
	// name with no implementation behind it, so the retry is ours to make.
	inverted, err := gozxing.NewBinaryBitmap(gozxing.NewHybridBinarizer(gozxing.NewLuminanceSourceFromImage(img).Invert()))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if codes := readAll(inverted); codes != nil {
		return codes, nil
	}
	return nil, errNoCode
}

// readAll tries every reader against one bitmap, and returns nil if none of
// them recognise it. QR comes first and as a group, because an image may hold
// several QR codes; the remaining symbologies stop at the first hit, which is
// all a single-symbol image can offer anyway.
func readAll(bitmap *gozxing.BinaryBitmap) []code {
	// TRY_HARDER spends more time looking, which is the right trade for a
	// one-shot command.
	hints := map[gozxing.DecodeHintType]any{gozxing.DecodeHintType_TRY_HARDER: true}

	if results, err := multiqr.NewQRCodeMultiReader().DecodeMultiple(bitmap, hints); err == nil && len(results) > 0 {
		return codesFromResults(results)
	}
	for _, reader := range readers(hints) {
		if result, err := reader.Decode(bitmap, hints); err == nil {
			return codesFromResults([]*gozxing.Result{result})
		}
	}
	return nil
}

// readers lists the non-QR readers in the order they are tried: the 2D
// symbologies first, since they carry arbitrary data and are unlikely to be
// mistaken for anything else, then the 1D ones.
func readers(hints map[gozxing.DecodeHintType]any) []gozxing.Reader {
	return []gozxing.Reader{
		datamatrix.NewDataMatrixReader(),
		aztec.NewAztecReader(),
		// UPC-A before the UPC/EAN reader: it recognises the same symbol but
		// reports it as UPC-A without the EAN-13 leading zero.
		oned.NewUPCAReader(),
		oned.NewMultiFormatUPCEANReader(hints),
		oned.NewCode128Reader(),
		oned.NewCode39Reader(),
		oned.NewCode93Reader(),
		oned.NewITFReader(),
		oned.NewCodaBarReader(),
	}
}

func codesFromResults(results []*gozxing.Result) []code {
	codes := make([]code, 0, len(results))
	for _, result := range results {
		codes = append(codes, code{format: result.GetBarcodeFormat(), data: resultBytes(result)})
	}
	return codes
}

// resultBytes returns the exact bytes that were originally encoded.
//
// Two sources describe the content, and neither is right on its own. The
// BYTE_SEGMENTS metadata holds raw bytes, but only for the segments encoded in
// a byte mode: a QR code that mixes numeric or alphanumeric runs with byte runs
// reports only the byte ones, and a Data Matrix reports only its Base256 runs.
// Result.GetText() covers everything, but it has been through a charset
// decoder, which mangles data that is not text in that charset.
//
// So take the one that accounts for more of the symbol. A byte segment is one
// byte per character and a charset decoder never produces more characters than
// it consumed bytes, which makes "text longer than the segments" proof that the
// segments are missing part of the symbol.
func resultBytes(result *gozxing.Result) []byte {
	text := result.GetText()
	if segments := byteSegments(result); len(segments) >= len([]rune(text)) && len(segments) > 0 {
		return segments
	}
	// The decoders use ISO-8859-1 for anything they cannot place, so text whose
	// code points all fit in a byte is a faithful copy of those bytes.
	if data, ok := fromLatin1String(text); ok {
		return data
	}
	return []byte(text)
}

// byteSegments concatenates the raw byte-mode segments of a result, if it
// reports any.
func byteSegments(result *gozxing.Result) []byte {
	segs, ok := result.GetResultMetadata()[gozxing.ResultMetadataType_BYTE_SEGMENTS]
	if !ok {
		return nil
	}
	byteSegments, ok := segs.([][]byte)
	if !ok {
		return nil
	}
	var buf bytes.Buffer
	for _, seg := range byteSegments {
		buf.Write(seg)
	}
	return buf.Bytes()
}
