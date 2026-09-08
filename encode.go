package main

import (
	"fmt"
	"strings"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
)

// toLatin1String maps each input byte to the Unicode code point of the same
// value (U+0000-U+00FF), which is exactly what ISO-8859-1 represents. This
// lets gozxing's string-based encoder carry arbitrary bytes through its
// byte-mode segment unchanged instead of interpreting them as UTF-8.
func toLatin1String(data []byte) string {
	var sb strings.Builder
	sb.Grow(len(data))
	for _, b := range data {
		sb.WriteRune(rune(b))
	}
	return sb.String()
}

// encodeErrorMessage strips the Java exception names the encoders carry over
// from the library they were ported from. They mean nothing to anyone running
// the command.
func encodeErrorMessage(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"WriterException: ", "IllegalArgumentException: "} {
		msg = strings.TrimPrefix(msg, prefix)
	}
	return msg
}

// fromLatin1String is the inverse of toLatin1String: it recovers the bytes
// from a string whose code points are all U+0000-U+00FF, and reports whether
// the string was in that range at all.
func fromLatin1String(s string) ([]byte, bool) {
	data := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xff {
			return nil, false
		}
		data = append(data, byte(r))
	}
	return data, true
}

// encodeQR encodes data as a QR code and returns its module matrix (no
// pixel scaling; one BitMatrix cell == one QR module, including the quiet
// zone given by margin). ecLevel must already be normalised by parseLevel;
// version pins the symbol version (1-40), or 0 to pick the smallest that fits.
func encodeQR(data []byte, ecLevel string, margin, version int) (*gozxing.BitMatrix, error) {
	hints := map[gozxing.EncodeHintType]any{
		gozxing.EncodeHintType_ERROR_CORRECTION: ecLevel,
		gozxing.EncodeHintType_CHARACTER_SET:    "ISO-8859-1",
		gozxing.EncodeHintType_MARGIN:           margin,
	}
	if version != 0 {
		hints[gozxing.EncodeHintType_QR_VERSION] = version
	}
	writer := qrcode.NewQRCodeWriter()
	matrix, err := writer.Encode(toLatin1String(data), gozxing.BarcodeFormat_QR_CODE, 0, 0, hints)
	if err != nil {
		if version != 0 {
			return nil, fmt.Errorf("encode: %s (try a larger -v, a lower -l, or less data)", encodeErrorMessage(err))
		}
		return nil, fmt.Errorf("encode: %s (try a lower -l, or less data)", encodeErrorMessage(err))
	}
	return matrix, nil
}

// parseLevel normalises an error correction level name, rejecting anything
// gozxing would not understand.
func parseLevel(level string) (string, error) {
	upper := strings.ToUpper(level)
	switch upper {
	case "L", "M", "Q", "H":
		return upper, nil
	default:
		return "", fmt.Errorf("invalid -l: %q is not an error correction level (want L, M, Q or H)", level)
	}
}
