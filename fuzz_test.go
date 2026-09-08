package main

import (
	"strings"
	"testing"
)

// parseTerminalGrid walks arbitrary input byte by byte, so it must not panic
// on anything, and whatever it accepts must be a rectangle.
func FuzzParseTerminalGrid(f *testing.F) {
	seeds := []string{
		"",
		"█▀▄ \n▀▄█ \n",
		"\x1b[48;2;0;0;0m  \x1b[0m\n",
		"\x1b[",
		"\x1b[48;2;",
		"\x1b[48;5;\n█\n",
		"█\n\n█\n",
		strings.Repeat("█▀▄ ", 10) + "\n",
		"\xff\xfe invalid utf-8 \x00\n█",
	}
	for _, seed := range seeds {
		f.Add([]byte(seed))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		grid, err := parseTerminalGrid(data)
		if err != nil {
			return
		}
		if len(grid) == 0 {
			t.Fatal("parseTerminalGrid returned an empty grid without an error")
		}
		width := len(grid[0])
		for y, row := range grid {
			if len(row) != width {
				t.Fatalf("row %d is %d wide, want %d: the grid is not a rectangle", y, len(row), width)
			}
		}
	})
}

// parseColor is fed whatever -fg and -bg are given.
func FuzzParseColor(f *testing.F) {
	for _, seed := range []string{"", "#", "black", "NONE", "#abc", "#abcd", "#aabbcc", "#aabbccdd", "#gggggg", "12345678", "#12345"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		c, err := parseColor(s)
		if err != nil {
			return
		}
		// An accepted colour must survive being spelled back out as hex.
		if _, err := parseColor(hex(c)); err != nil {
			t.Fatalf("parseColor(%q) = %v, which does not parse back: %v", s, c, err)
		}
	})
}

// escapeAt indexes into arbitrary strings looking for a final byte.
func FuzzEscapeAt(f *testing.F) {
	for _, seed := range []string{"", "\x1b[", "\x1b[0m", "\x1b[48;2;1;2;3m", "\x1b[\x00m", "plain"} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		params, size, ok := escapeAt(s)
		if !ok {
			return
		}
		if size <= 0 || size > len(s) {
			t.Fatalf("escapeAt(%q) reported size %d, outside the string", s, size)
		}
		if len(params) > size {
			t.Fatalf("escapeAt(%q) reported %d parameter bytes in a %d byte sequence", s, len(params), size)
		}
		// The reported parameters must be the bytes between the introducer and
		// the final byte.
		if params != s[2:size-1] {
			t.Fatalf("escapeAt(%q) params = %q, want %q", s, params, s[2:size-1])
		}
	})
}
