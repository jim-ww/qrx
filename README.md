# qrx

Minimalistic QR code and barcode encoder/decoder for the terminal. Encodes
arbitrary bytes (not just text) to a QR code and decodes them back exactly,
byte for byte, and writes the common 1D barcode symbologies too.

```console
$ qrx 'https://youtu.be/dQw4w9WgXcQ'


    █▀▀▀▀▀█ █▀▄  █▀▀▄ █ ▀ █▀▀▀▀▀█
    █ ███ █ ▄██ ▀ ▄▄▀▄▀ ▄ █ ███ █
    █ ▀▀▀ █  █▀▀█▄▀▄██▀█▄ █ ▀▀▀ █
    ▀▀▀▀▀▀▀ █▄█ ▀ ▀▄█ █▄▀ ▀▀▀▀▀▀▀
    █▄▀█▄█▀▀▄ ▀ ▄█▄█   ▀▀ ▀▄▄█ █▀
    ▀ ▀▄▀▄▀▄█▀█ ▄▀  █▀█▄▄█ ▄▀▀ █
    ▄▀▄▀ ▀▀▀█   █ █▀▄█▄    ▀▄▄▄
    ▄  ▀█▄▀ ▀▀ ▀ ▀▄▀█▀██▄▀█▄█ ▀▄▄
         █▀▄██▄▄  █▀█ █ ▄█▄ ▄ ▀▀▄
    ▀  ▀ ▄▀▄ █▄ ▀██▀▀ ▄█▄▀▄▀ ▀█▄▀
     ▀▀▀ ▀▀▀██▀  ▀▀▄█ ▄██▀▀▀█ ▀▄▀
    █▀▀▀▀▀█ █ ▄▀█▄██  ▀ █ ▀ ██▄█▄
    █ ███ █ ▄█  ▀    ██ ▀██▀█ █▀
    █ ▀▀▀ █ ▀██▀▀ █▄▀ ▀ ▄  ▀▀██ █
    ▀▀▀▀▀▀▀ ▀▀  ▀   ▀▀  ▀ ▀▀ ▀ ▀


```

## Install

Prebuilt binaries for Linux, macOS and Windows are on the
[releases page](https://github.com/jim-ww/qrx/releases).

Or build it yourself — requires Go 1.24+.

```sh
go install github.com/jim-ww/qrx@latest
```

Or try it with Nix:

```sh
nix run github:jim-ww/qrx             # once
nix profile install github:jim-ww/qrx # to keep it
```

Or add it to your flake inputs:

```nix
inputs.qrx.url = "github:jim-ww/qrx";
# environment.systemPackages = [ inputs.qrx.packages.${system}.default ];
```

## Usage

```
qrx [-d] [-t TYPE] [-f FORMAT] [-l LEVEL] [-v VERSION] [-s SCALE]
    [-m MARGIN] [-bh HEIGHT] [-i] [-fg COLOR] [-bg COLOR] [-o FILE] [FILE]
```

- `-d` decode: read a barcode image, write the decoded bytes
- `-t TYPE` symbology to encode: `qr` (default), `datamatrix`, `code128`,
  `code39`, `code93`, `codabar`, `ean8`, `ean13`, `upca`, `upce`, `itf`
- `-f FORMAT` encode output format: `unicode` (default), `ansi`, `sixel`,
  `png`, `svg`
- `-l LEVEL` error correction level: `L`, `M`, `Q`, `H` (`qr` only, default `M`)
- `-v VERSION` symbol version 1–40, i.e. size; 0 (the default) picks the
  smallest that fits the data (`qr` only)
- `-s SCALE` repeat factor for `unicode`/`ansi`/`sixel`, pixels per module
  for `png`/`svg` (0, the default, picks 1 — or 8 for `png` and `svg`)
- `-m MARGIN` quiet zone width in modules, per side (default 4 for `qr`, 10
  for the 1D symbologies, which need the wider zone)
- `-bh HEIGHT` bar height in modules for the 1D symbologies; 0 follows the
  symbol width (default 0)
- `-i` invert: light modules on a dark background
- `-fg COLOR` colour of the dark modules (default `black`)
- `-bg COLOR` colour of the background (default `white`)
- `-o FILE` write output to `FILE` instead of stdout
- `-h` show help

`COLOR` is a name (`black`, `white`, `none`) or hex — `#RGB`, `#RGBA`,
`#RRGGBB`, `#RRGGBBAA`. `none` is transparent, which works for `png`, `svg`
and the terminal formats.

Decoding is not limited to QR: it reads QR (including several codes in one
image, written out newline-separated), Data Matrix, Aztec, EAN-8/13,
UPC-A/E, Code 128, Code 39, Code 93, ITF and Codabar, and retries inverted
if nothing matches, so light-on-dark codes work too.

`-d` also reads back the `unicode` and `ansi` output, so a code pasted out
of a terminal or committed to a text file decodes like an image does:

```sh
qrx 'https://youtu.be/dQw4w9WgXcQ' > code.txt
qrx -d code.txt
qrx 'round trip' | qrx -d
```

When encoding, `FILE` (if given) is the literal text to encode; stdin is
read otherwise. When decoding, `FILE` is a path to an image; stdin is read
otherwise.

## Examples

```sh
qrx 'https://youtu.be/dQw4w9WgXcQ'
echo -n 'hello' | qrx -f png -o hello.png
qrx -d hello.png
qrx -f sixel 'WIFI:S:myssid;T:WPA;P:pass123;;'
qrx -f png -o - < data.bin | qrx -d -
qrx -f svg -fg '#1e3a8a' -bg none -o code.svg 'https://youtu.be/dQw4w9WgXcQ'
qrx -v 10 -l H -o backup.png < secret.key
qrx -t ean13 -f png -o barcode.png 5901234123457
qrx -t code128 -bh 30 'PKG-00417'
qrx -t datamatrix -f png -o part.png 'PN:4815162342'
```

Only `qr` carries arbitrary bytes. `datamatrix` is reliable for text but not
for binary.

On a dark terminal the blocks come out light-on-dark — an inverted code, so
pass `-i` if a scanner refuses it.

`-d` reads back `png`, `unicode` and `ansi`, but not `sixel` or `svg`.

## Support the project

**Monero (XMR)**

`83YGRqP8uHed6NeegZQeX9ccCxbzoRHHEEi7pTwk4aqdJZEVXXA6NWtetnsEM2v33zFBBt3Rp6DNhU9qhJEGPspU14yN8t7`

## License

GPL-3.0. Free to use, study, share, and modify — provided you keep the same freedoms for others.
