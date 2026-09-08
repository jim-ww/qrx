package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// decodeQR reads an image from r, locates a QR code in it and returns the
// exact bytes that were originally encoded.
//
// It prefers the raw byte-mode segments (BYTE_SEGMENTS metadata) over
// Result.GetText(), because GetText() runs the decoded bytes through a
// charset decoder and can mangle data that isn't valid text in that
// charset. Falling back to GetText() only happens when the QR code was
// encoded in NUMERIC or ALPHANUMERIC mode, where the text is a lossless
// representation of the original bytes anyway.
func decodeQR(r io.Reader) ([]byte, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	reader := qrcode.NewQRCodeReader()
	result, err := reader.Decode(bitmap, nil)
	if err != nil {
		return nil, fmt.Errorf("no QR code found: %w", err)
	}

	if segs, ok := result.GetResultMetadata()[gozxing.ResultMetadataType_BYTE_SEGMENTS]; ok {
		if byteSegments, ok := segs.([][]byte); ok && len(byteSegments) > 0 {
			var buf bytes.Buffer
			for _, seg := range byteSegments {
				buf.Write(seg)
			}
			return buf.Bytes(), nil
		}
	}

	return []byte(result.GetText()), nil
}
