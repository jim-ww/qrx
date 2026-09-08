package main

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"
)

// errNotTerminalOutput means the input holds nothing that looks like a code
// drawn with terminal characters.
var errNotTerminalOutput = errors.New("not terminal output")

// Half block characters: the unicode renderer packs two module rows into each
// line, so every character carries the top row, the bottom row, or both.
const blockRunes = string(uBlockFull) + string(uBlockUpper) + string(uBlockLower)

// parseTerminalGrid reads back what renderUnicode and renderANSI write, so a
// code pasted out of a terminal or a text file can be decoded again.
//
// Both formats are lossless grids of modules, they just spell a module
// differently: unicode uses one character per column and half blocks to pack
// two rows into a line, while ANSI colours two spaces per module and says
// nothing in the characters themselves. Escape sequences are read for their
// background colour in the ANSI case and skipped in the unicode one, where the
// block characters already carry the answer.
func parseTerminalGrid(data []byte) ([][]bool, error) {
	text := string(data)
	// The unicode renderer is recognised by its block characters; anything
	// else with escape sequences in it is treated as ANSI.
	blocks := strings.ContainsAny(text, blockRunes)
	if !blocks && !strings.Contains(text, "\x1b[") {
		return nil, errNotTerminalOutput
	}

	var grid [][]bool
	width := 0
	for _, line := range strings.Split(text, "\n") {
		rows := parseLine(line, blocks)
		for _, row := range rows {
			width = max(width, len(row))
		}
		grid = append(grid, rows...)
	}

	// Lines end as soon as their last module does, so short rows are quiet
	// zone rather than missing data.
	for i, row := range grid {
		for len(row) < width {
			row = append(row, false)
		}
		grid[i] = row
	}
	if len(grid) < minGridSize || width < minGridSize {
		return nil, errNotTerminalOutput
	}

	if !blocks {
		// An ANSI module is two characters wide and one line tall, so the rows
		// have to be doubled to make the modules square again.
		doubled := make([][]bool, 0, 2*len(grid))
		for _, row := range grid {
			doubled = append(doubled, row, row)
		}
		grid = doubled
	}

	// The symbol may have been written with a narrow quiet zone, or none, and
	// trailing whitespace may have been stripped from the lines on its way
	// here. Detectors need that zone, so give the grid one of its own.
	return padGrid(grid, parsedQuietZone), nil
}

// parsedQuietZone is the quiet zone, in modules, added around a grid parsed
// back from terminal output.
const parsedQuietZone = 4

// padGrid surrounds the grid with n modules of light on every side.
func padGrid(grid [][]bool, n int) [][]bool {
	if len(grid) == 0 {
		return grid
	}
	width := len(grid[0]) + 2*n
	padded := make([][]bool, 0, len(grid)+2*n)
	for range n {
		padded = append(padded, make([]bool, width))
	}
	for _, row := range grid {
		wide := make([]bool, width)
		copy(wide[n:], row)
		padded = append(padded, wide)
	}
	for range n {
		padded = append(padded, make([]bool, width))
	}
	return padded
}

// minGridSize is the smallest symbol worth trying to read: the smallest QR
// code is 21 modules square, and the shortest 1D symbol is well over 21
// columns, so anything smaller than this is not a code.
const minGridSize = 8

// parseLine turns one line of terminal output into module rows: two rows for a
// line of half blocks, one for a line of coloured spaces.
func parseLine(line string, blocks bool) [][]bool {
	if blocks {
		var top, bottom []bool
		for _, r := range stripEscapes(line) {
			switch r {
			case uBlockFull:
				top, bottom = append(top, true), append(bottom, true)
			case uBlockUpper:
				top, bottom = append(top, true), append(bottom, false)
			case uBlockLower:
				top, bottom = append(top, false), append(bottom, true)
			default:
				top, bottom = append(top, false), append(bottom, false)
			}
		}
		if len(top) == 0 {
			return nil
		}
		return [][]bool{top, bottom}
	}

	var row []bool
	dark := false
	for i := 0; i < len(line); {
		if params, size, ok := escapeAt(line[i:]); ok {
			if isDark, known := backgroundIsDark(params); known {
				dark = isDark
			}
			i += size
			continue
		}
		_, size := utf8.DecodeRuneInString(line[i:])
		row = append(row, dark)
		i += size
	}
	if len(row) == 0 {
		return nil
	}
	return [][]bool{row}
}

// stripEscapes removes CSI sequences, which carry no module information once
// the block characters have been read.
func stripEscapes(line string) string {
	var sb strings.Builder
	for i := 0; i < len(line); {
		if _, size, ok := escapeAt(line[i:]); ok {
			i += size
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		sb.WriteRune(r)
		i += size
	}
	return sb.String()
}

// escapeAt reports whether s starts with a CSI sequence, returning its
// parameter bytes and its total length.
func escapeAt(s string) (params string, size int, ok bool) {
	if !strings.HasPrefix(s, "\x1b[") {
		return "", 0, false
	}
	for i := 2; i < len(s); i++ {
		// The sequence ends at its final byte, in the range @ to ~.
		if c := s[i]; c >= '@' && c <= '~' {
			return s[2:i], i + 1, true
		}
	}
	return "", 0, false
}

// backgroundIsDark reads an SGR parameter list for a background colour, and
// reports whether that colour is a dark one. A reset counts as light, since
// the renderers only drop back to the terminal's own colours between modules.
func backgroundIsDark(params string) (dark, known bool) {
	fields := strings.Split(params, ";")
	for i := 0; i < len(fields); i++ {
		code, err := strconv.Atoi(fields[i])
		if err != nil {
			continue
		}
		// 38 and 48 introduce an extended colour whose arguments follow: three
		// for 38;2/48;2 (RGB) and one for 38;5/48;5 (palette index). They have
		// to be consumed, or a zero inside them reads as a reset.
		if code == 38 || code == 48 {
			args := 0
			if i+1 < len(fields) {
				switch fields[i+1] {
				case "2":
					args = 4
				case "5":
					args = 2
				}
			}
			if code == 48 && args == 4 && i+4 < len(fields) {
				r, _ := strconv.Atoi(fields[i+2])
				g, _ := strconv.Atoi(fields[i+3])
				b, _ := strconv.Atoi(fields[i+4])
				dark, known = luminance(r, g, b) < 0x80, true
			}
			i += args
			continue
		}
		switch {
		case code == 0:
			dark, known = false, true
		case code >= 40 && code <= 47:
			// The legacy background colours: black and the darker half count
			// as dark, white and the lighter half as light.
			dark, known = code-40 <= 4 && code != 42, true
		case code >= 100 && code <= 107:
			dark, known = false, true
		}
	}
	return dark, known
}

// luminance is the usual perceptual weighting, in the same 0-255 range as its
// inputs.
func luminance(r, g, b int) int {
	return (299*r + 587*g + 114*b) / 1000
}
