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

// decodeImage finds every barcode in the image read from r, trying the
// supported symbologies in turn and, failing those, the inverted image.
func decodeImage(r io.Reader) ([]code, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
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
// It prefers the raw byte-mode segments (BYTE_SEGMENTS metadata) over
// Result.GetText(), because GetText() runs the decoded bytes through a
// charset decoder and can mangle data that isn't valid text in that
// charset. Falling back to GetText() only happens when the symbol was
// encoded in a mode — NUMERIC or ALPHANUMERIC for QR, and every 1D
// symbology — whose text is a lossless representation of the data anyway.
func resultBytes(result *gozxing.Result) []byte {
	if segs, ok := result.GetResultMetadata()[gozxing.ResultMetadataType_BYTE_SEGMENTS]; ok {
		if byteSegments, ok := segs.([][]byte); ok && len(byteSegments) > 0 {
			var buf bytes.Buffer
			for _, seg := range byteSegments {
				buf.Write(seg)
			}
			return buf.Bytes()
		}
	}
	return []byte(result.GetText())
}
