package main

import (
	"bytes"
	"testing"

	"github.com/makiuchi-d/gozxing"
)

// resultBytes has to choose between the raw byte segments and the decoded
// text, either of which can be the incomplete one.
func TestResultBytes(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		segments [][]byte
		want     []byte
	}{
		{
			name: "no segments, text is the only source",
			text: "5901234123457",
			want: []byte("5901234123457"),
		},
		{
			name:     "segments match the text, either would do",
			text:     "hello",
			segments: [][]byte{[]byte("hello")},
			want:     []byte("hello"),
		},
		{
			// The charset decoder turned two bytes into one character, so the
			// segments hold what the text lost.
			name:     "text is shorter, the segments are the raw bytes",
			text:     "é",
			segments: [][]byte{{0xc3, 0xa9}},
			want:     []byte{0xc3, 0xa9},
		},
		{
			// A symbol mixing numeric and byte runs: the segments cover the
			// byte run alone, so using them would drop the digits.
			name:     "segments cover only part of the symbol",
			text:     "12345678abc",
			segments: [][]byte{[]byte("abc")},
			want:     []byte("12345678abc"),
		},
		{
			name:     "several segments are concatenated",
			text:     "abcdef",
			segments: [][]byte{[]byte("abc"), []byte("def")},
			want:     []byte("abcdef"),
		},
		{
			// Latin-1 text comes back as the bytes it was decoded from.
			name: "high code points are bytes",
			text: "\u00ff\u0080",
			want: []byte{0xff, 0x80},
		},
		{
			// Text beyond Latin-1 with no segments to fall back on: UTF-8 is
			// the only sensible reading.
			name: "text beyond latin-1 without segments",
			text: "привет",
			want: []byte("привет"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gozxing.NewResult(tt.text, nil, nil, gozxing.BarcodeFormat_QR_CODE)
			if tt.segments != nil {
				result.PutMetadata(gozxing.ResultMetadataType_BYTE_SEGMENTS, tt.segments)
			}
			if got := resultBytes(result); !bytes.Equal(got, tt.want) {
				t.Errorf("resultBytes = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestByteSegments(t *testing.T) {
	t.Run("absent", func(t *testing.T) {
		result := gozxing.NewResult("x", nil, nil, gozxing.BarcodeFormat_QR_CODE)
		if got := byteSegments(result); got != nil {
			t.Errorf("byteSegments = %v, want nil", got)
		}
	})

	t.Run("wrong type", func(t *testing.T) {
		result := gozxing.NewResult("x", nil, nil, gozxing.BarcodeFormat_QR_CODE)
		result.PutMetadata(gozxing.ResultMetadataType_BYTE_SEGMENTS, "not segments")
		if got := byteSegments(result); got != nil {
			t.Errorf("byteSegments = %v, want nil", got)
		}
	})
}
