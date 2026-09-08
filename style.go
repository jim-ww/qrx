package main

import (
	"fmt"
	"image/color"
	"strconv"
	"strings"
)

// style is everything that affects how a module matrix is turned into output:
// its size, its orientation and its two colours.
type style struct {
	scale  int
	invert bool
	dark   color.NRGBA
	light  color.NRGBA
}

var (
	colorBlack       = color.NRGBA{A: 0xff}
	colorWhite       = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	colorTransparent = color.NRGBA{}
)

// namedColors are the few names worth spelling out; everything else is hex.
var namedColors = map[string]color.NRGBA{
	"black":       colorBlack,
	"white":       colorWhite,
	"none":        colorTransparent,
	"transparent": colorTransparent,
}

// parseColor accepts a colour name, or hex in the #RGB, #RGBA, #RRGGBB or
// #RRGGBBAA forms, with or without the leading '#'.
func parseColor(s string) (color.NRGBA, error) {
	if c, ok := namedColors[strings.ToLower(s)]; ok {
		return c, nil
	}

	hex := strings.TrimPrefix(s, "#")
	var digits int
	switch len(hex) {
	case 3, 4:
		digits = 1
	case 6, 8:
		digits = 2
	default:
		return color.NRGBA{}, fmt.Errorf("%q is not a colour (want a name, or hex as #RGB, #RGBA, #RRGGBB or #RRGGBBAA)", s)
	}

	c := color.NRGBA{A: 0xff}
	channels := []*uint8{&c.R, &c.G, &c.B, &c.A}
	for i := 0; i*digits < len(hex); i++ {
		v, err := strconv.ParseUint(hex[i*digits:(i+1)*digits], 16, 8)
		if err != nil {
			return color.NRGBA{}, fmt.Errorf("%q is not a colour: %w", s, err)
		}
		if digits == 1 {
			v *= 0x11 // #abc is #aabbcc
		}
		*channels[i] = uint8(v)
	}
	return c, nil
}

// hex renders a colour as #RRGGBB for SVG. Alpha is carried separately,
// because SVG spells it as a fill-opacity attribute.
func hex(c color.NRGBA) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// opacity renders a colour's alpha as an SVG fill-opacity value.
func opacity(c color.NRGBA) string {
	return strconv.FormatFloat(float64(c.A)/0xff, 'g', 4, 64)
}
