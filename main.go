package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
)

const usage = `Usage: qrx [-d] [-f FORMAT] [-l LEVEL] [-v VERSION] [-s SCALE] [-m MARGIN]
           [-i] [-fg COLOR] [-bg COLOR] [-o FILE] [FILE]

Encode data to a QR code, or decode one back to bytes.

  -d            decode: read a barcode image, write the decoded bytes. Reads
                QR (several per image), Data Matrix, Aztec, EAN/UPC, Code
                128/39/93, ITF and Codabar, dark or light on light or dark
  -f FORMAT     encode output format: unicode, ansi, sixel, png, svg
                (default "unicode")
  -l LEVEL      error correction level: L, M, Q, H (default "M")
  -v VERSION    symbol version 1-40, i.e. size; 0 picks the smallest that
                fits the data (default 0)
  -s SCALE      scale: repeat factor for unicode/ansi/sixel, pixels per
                module for png/svg (0 picks the format default: 1, or 8 for
                png and svg)
  -m MARGIN     quiet zone width in modules; the QR spec asks for 4 (default 4)
  -i            invert: light modules on a dark background
  -fg COLOR     colour of the dark modules (default "black")
  -bg COLOR     colour of the background (default "white")
  -o FILE       write output to FILE instead of stdout
  -h            show this help

COLOR is a name (black, white, none) or hex: #RGB, #RGBA, #RRGGBB, #RRGGBBAA.
"none" is transparent, for png, svg and the terminal formats.

FILE is the input to encode/decode; stdin is read if omitted.

Examples:
  qrx 'https://example.com'
  echo -n 'hello' | qrx -f png -o hello.png
  qrx -d hello.png
  qrx -f sixel 'WIFI:S:myssid;T:WPA;P:pass123;;'
  qrx -f svg -fg '#1e3a8a' -bg none -o code.svg 'https://example.com'
`

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "qrx: %v\n", err)
		var ue *usageError
		if errors.As(err, &ue) {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// usageError reports a malformed command line. It is answered with the usage
// text and exit status 2, while every other error exits with status 1.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usagef(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

func run(args []string, stdin io.Reader, stdout io.Writer) (err error) {
	fs := flag.NewFlagSet("qrx", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	decode := fs.Bool("d", false, "")
	format := fs.String("f", "unicode", "")
	level := fs.String("l", "M", "")
	scale := fs.Int("s", 0, "")
	margin := fs.Int("m", 4, "")
	version := fs.Int("v", 0, "")
	invert := fs.Bool("i", false, "")
	fg := fs.String("fg", "black", "")
	bg := fs.String("bg", "white", "")
	output := fs.String("o", "", "")
	help := fs.Bool("h", false, "")

	if err := fs.Parse(args); err != nil {
		return usagef("%v", err)
	}
	if *help {
		_, err := fmt.Fprint(stdout, usage)
		return err
	}
	if fs.NArg() > 1 {
		return usagef("too many arguments")
	}
	if *margin < 0 {
		return usagef("invalid -m: must be >= 0")
	}
	if *scale < 0 {
		return usagef("invalid -s: must be >= 0")
	}

	// Everything the command line can get wrong is settled before any file is
	// created or any input is consumed.
	var (
		ecLevel string
		st      style
	)
	if !*decode {
		if ecLevel, err = parseLevel(*level); err != nil {
			return usagef("%v", err)
		}
		defaultScale, ok := defaultScales[*format]
		if !ok {
			return usagef("unknown output format %q (want %s)", *format, strings.Join(slices.Sorted(maps.Keys(defaultScales)), ", "))
		}
		if *version < 0 || *version > 40 {
			return usagef("invalid -v: must be 0 (automatic) or a version 1-40")
		}
		st = style{scale: cmp.Or(*scale, defaultScale), invert: *invert}
		if st.dark, err = parseColor(*fg); err != nil {
			return usagef("invalid -fg: %v", err)
		}
		if st.light, err = parseColor(*bg); err != nil {
			return usagef("invalid -bg: %v", err)
		}
	}

	// A PNG dumped into a terminal is unreadable noise that can leave the
	// terminal in a strange state, and it is nearly always a forgotten -o.
	if *format == "png" && *output == "" && isTerminal(stdout) {
		return usagef("refusing to write PNG to the terminal: use -o FILE, or pipe the output")
	}

	out, closeOut, err := openOutput(*output, stdout)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeOut()) }()

	if *decode {
		// The positional argument is a path to an image file; stdin otherwise.
		in, closeIn, err := openInput(fs.Arg(0), stdin)
		if err != nil {
			return err
		}
		defer closeIn()

		codes, err := decodeImage(in)
		if err != nil {
			return err
		}
		return writeCodes(out, codes)
	}

	// The positional argument is the literal data to encode; stdin otherwise.
	var data []byte
	if fs.NArg() == 1 {
		data = []byte(fs.Arg(0))
	} else {
		if data, err = io.ReadAll(stdin); err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	}

	matrix, err := encodeQR(data, ecLevel, *margin, *version)
	if err != nil {
		return err
	}
	return render(out, *format, matrix, st)
}

// writeCodes writes the decoded data. A single code is written verbatim, so
// that binary data survives the round trip byte for byte; several codes in one
// image are separated — and terminated — by newlines, so they stay apart.
func writeCodes(w io.Writer, codes []code) error {
	if len(codes) == 1 {
		_, err := w.Write(codes[0].data)
		return err
	}
	for _, c := range codes {
		if _, err := w.Write(c.data); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\n"); err != nil {
			return err
		}
	}
	return nil
}

// isTerminal reports whether w is a character device, i.e. a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// openInput returns the file at path, or fallback when path is empty or "-".
func openInput(path string, fallback io.Reader) (io.Reader, func(), error) {
	if path == "" || path == "-" {
		return fallback, func() {}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

// openOutput returns the file at path, or fallback when path is empty or "-".
// The returned close function is a no-op for fallback, so it is always safe
// to defer.
func openOutput(path string, fallback io.Writer) (io.Writer, func() error, error) {
	if path == "" || path == "-" {
		return fallback, func() error { return nil }, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
