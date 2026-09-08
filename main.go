package main

import (
	"bytes"
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/makiuchi-d/gozxing"
)

const usage = `Usage: qrx [-d] [-t TYPE] [-f FORMAT] [-l LEVEL] [-v VERSION] [-s SCALE]
           [-m MARGIN] [-bh HEIGHT] [-i] [-fg COLOR] [-bg COLOR] [-o FILE] [FILE]

Encode data to a QR code or a barcode, or decode one back to bytes.

  -d            decode: read a barcode image, write the decoded bytes. Reads
                QR (several per image), Data Matrix, Aztec, EAN/UPC, Code
                128/39/93, ITF and Codabar, dark or light on light or dark,
                from an image file or from qrx's own unicode/ansi output
  -t TYPE       symbology to encode: qr (default), datamatrix, code128,
                code39, code93, codabar, ean8, ean13, upca, upce, itf.
                qr takes any input; the rest are for text
  -f FORMAT     encode output format: unicode, ansi, sixel, png, svg
                (default "unicode")
  -l LEVEL      error correction level: L, M, Q, H (qr only, default "M")
  -v VERSION    symbol version 1-40, i.e. size; 0 picks the smallest that
                fits the data (qr only, default 0)
  -s SCALE      scale: repeat factor for unicode/ansi/sixel, pixels per
                module for png/svg (0 picks the format default: 1, or 8 for
                png and svg)
  -m MARGIN     quiet zone width in modules (default 4 for qr, 10 for the 1D
                symbologies, which need the wider zone)
  -bh HEIGHT    bar height in modules for the 1D symbologies; 0 follows the
                symbol width (default 0)
  -i            invert: light modules on a dark background
  -fg COLOR     colour of the dark modules (default "black")
  -bg COLOR     colour of the background (default "white"); "none" for
                transparent
  -o FILE       write output to FILE instead of stdout
  -h            show this help

COLOR is a name (black, white, none) or hex: #RGB, #RGBA, #RRGGBB, #RRGGBBAA.
Transparency works for png, svg and the terminal formats.

FILE is the input to encode/decode; stdin is read if omitted.

Examples:
  qrx 'https://youtu.be/dQw4w9WgXcQ'
  echo -n 'hello' | qrx -f png -o hello.png
  qrx -d hello.png
  qrx -f sixel 'WIFI:S:myssid;T:WPA;P:pass123;;'
  qrx -f svg -fg '#1e3a8a' -bg none -o code.svg 'https://youtu.be/dQw4w9WgXcQ'
  qrx -t ean13 -f png -o barcode.png 5901234123457
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
	symbol := fs.String("t", "qr", "")
	margin := fs.Int("m", 4, "")
	barHeight := fs.Int("bh", 0, "")
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
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if fs.NArg() > 1 {
		return usagef("too many arguments")
	}
	if *margin < 0 {
		return usagef("invalid -m: must be >= 0")
	}
	if *barHeight < 0 {
		return usagef("invalid -bh: must be >= 0")
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
	sym, ok := symbologies[*symbol]
	if !*decode {
		if !ok {
			return usagef("unknown symbology %q (want %s)", *symbol, symbologyNames())
		}
		if sym.format != gozxing.BarcodeFormat_QR_CODE {
			for _, name := range []string{"l", "v"} {
				if set[name] {
					return usagef("-%s applies to qr only, not -t %s", name, *symbol)
				}
			}
		}
		if sym.oneD {
			// 1D symbologies need a much wider quiet zone than a QR code.
			if !set["m"] {
				*margin = oneDQuietZone
			}
		} else if set["bh"] {
			return usagef("-bh applies to the 1D symbologies only, not -t %s", *symbol)
		}
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

	// The output is built in full before anything is opened, so a failure
	// part way through cannot truncate the file named by -o.
	var out bytes.Buffer

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
		if err := writeCodes(&out, codes); err != nil {
			return err
		}
	} else {
		// The positional argument is the literal data to encode; stdin otherwise.
		var data []byte
		if fs.NArg() == 1 {
			data = []byte(fs.Arg(0))
		} else {
			if data, err = io.ReadAll(stdin); err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		}

		var matrix *gozxing.BitMatrix
		switch {
		case sym.oneD:
			matrix, err = encodeBarcode(data, sym, *margin, *barHeight)
		case sym.format == gozxing.BarcodeFormat_DATA_MATRIX:
			matrix, err = encodeDataMatrix(data, *margin)
		default:
			matrix, err = encodeQR(data, ecLevel, *margin, *version)
		}
		if err != nil {
			return err
		}
		if err := render(&out, *format, matrix, st); err != nil {
			return err
		}
	}

	w, closeOut, err := openOutput(*output, stdout)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeOut()) }()

	_, err = w.Write(out.Bytes())
	return err
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
