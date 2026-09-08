package main

import (
	"bytes"
	"strings"
	"testing"
)

// Anything qrx draws with terminal characters must read back through -d.
func TestTerminalRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		args []string
		data string
	}{
		{"unicode", nil, "terminal round trip"},
		{"unicode scaled", []string{"-s", "3"}, "scaled"},
		{"unicode inverted", []string{"-i"}, "inverted"},
		{"unicode without a quiet zone", []string{"-m", "0"}, "no margin"},
		{"unicode coloured", []string{"-fg", "#ff8800", "-bg", "#1e3a8a"}, "coloured"},
		{"ansi", []string{"-f", "ansi"}, "ansi round trip"},
		{"ansi scaled", []string{"-f", "ansi", "-s", "2"}, "scaled"},
		{"ansi inverted", []string{"-f", "ansi", "-i"}, "inverted"},
		{"ansi coloured", []string{"-f", "ansi", "-fg", "#1e3a8a"}, "coloured"},
		{"data matrix", []string{"-t", "datamatrix"}, "PN:4815162342"},
		{"ean13", []string{"-t", "ean13"}, "5901234123457"},
		{"code128", []string{"-t", "code128"}, "ABC-123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var drawn bytes.Buffer
			if err := run(append(tt.args, tt.data), strings.NewReader(""), &drawn); err != nil {
				t.Fatalf("encode: %v", err)
			}

			var decoded bytes.Buffer
			if err := run([]string{"-d"}, &drawn, &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.String() != tt.data {
				t.Errorf("decoded %q, want %q", decoded.String(), tt.data)
			}
		})
	}
}

// Terminal output survives a trip through a text editor: trailing whitespace
// stripped, a stray blank line at the end, indentation added by a paste.
func TestTerminalRoundTripMangled(t *testing.T) {
	var drawn bytes.Buffer
	if err := run([]string{"pasted"}, strings.NewReader(""), &drawn); err != nil {
		t.Fatalf("encode: %v", err)
	}

	mangle := map[string]func(string) string{
		"trailing whitespace stripped": func(s string) string {
			lines := strings.Split(s, "\n")
			for i, line := range lines {
				lines[i] = strings.TrimRight(line, " ")
			}
			return strings.Join(lines, "\n")
		},
		"extra blank lines":   func(s string) string { return "\n\n" + s + "\n\n" },
		"no trailing newline": func(s string) string { return strings.TrimRight(s, "\n") },
	}

	for name, f := range mangle {
		t.Run(name, func(t *testing.T) {
			var decoded bytes.Buffer
			if err := run([]string{"-d"}, strings.NewReader(f(drawn.String())), &decoded); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.String() != "pasted" {
				t.Errorf("decoded %q, want %q", decoded.String(), "pasted")
			}
		})
	}
}

func TestParseTerminalGridRejects(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"prose", "just some prose here, with no code in it at all\n"},
		{"png header", "\x89PNG\r\n\x1a\n"},
		{"too small", "██\n▀▄\n"},
		{"escapes but no cells", "\x1b[0m\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseTerminalGrid([]byte(tt.in)); err == nil {
				t.Error("parseTerminalGrid = nil error, want error")
			}
		})
	}
}

func TestParseLineBlocks(t *testing.T) {
	rows := parseLine("█▀▄ ", true)
	if len(rows) != 2 {
		t.Fatalf("parseLine returned %d rows, want 2", len(rows))
	}
	wantTop := []bool{true, true, false, false}
	wantBottom := []bool{true, false, true, false}
	for x := range wantTop {
		if rows[0][x] != wantTop[x] || rows[1][x] != wantBottom[x] {
			t.Fatalf("column %d = (%v, %v), want (%v, %v)", x, rows[0][x], rows[1][x], wantTop[x], wantBottom[x])
		}
	}
}

func TestBackgroundIsDark(t *testing.T) {
	tests := []struct {
		name     string
		params   string
		dark, ok bool
	}{
		{"reset", "0", false, true},
		{"truecolor black", "48;2;0;0;0", true, true},
		{"truecolor white", "48;2;255;255;255", false, true},
		{"truecolor navy", "48;2;30;58;138", true, true},
		{"truecolor orange", "48;2;255;136;0", false, true},
		{"legacy black", "40", true, true},
		{"legacy white", "47", false, true},
		{"bright background", "107", false, true},
		// The zeros inside a foreground colour must not read as a reset.
		{"foreground only", "38;2;255;0;0", false, false},
		{"foreground then background", "38;2;255;0;0;48;2;0;0;0", true, true},
		{"palette background", "48;5;0", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dark, ok := backgroundIsDark(tt.params)
			if dark != tt.dark || ok != tt.ok {
				t.Errorf("backgroundIsDark(%q) = (%v, %v), want (%v, %v)", tt.params, dark, ok, tt.dark, tt.ok)
			}
		})
	}
}

func TestEscapeAt(t *testing.T) {
	tests := []struct {
		in     string
		params string
		size   int
		ok     bool
	}{
		{"\x1b[0m rest", "0", 4, true},
		{"\x1b[48;2;1;2;3m", "48;2;1;2;3", 13, true},
		{"no escape", "", 0, false},
		// 'u' is a valid final byte, so this sequence is complete.
		{"\x1b[u", "", 3, true},
		{"\x1b[48;2;1;2;3", "", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			params, size, ok := escapeAt(tt.in)
			if params != tt.params || size != tt.size || ok != tt.ok {
				t.Errorf("escapeAt(%q) = (%q, %d, %v), want (%q, %d, %v)",
					tt.in, params, size, ok, tt.params, tt.size, tt.ok)
			}
		})
	}
}
