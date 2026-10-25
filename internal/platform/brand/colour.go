package brand

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Colour maths uses floating point; these are display colours, not business values,
// so the float rule (D-NUM-01) does not apply and each use carries a nolint.

// rgb is a colour with sRGB channels in 0..1.
type rgb [3]float64 //nolint:forbidigo // colour maths, not a business value

// parseHex reads "#RRGGBB" (either case).
func parseHex(s string) (rgb, error) {
	if len(s) != 7 || s[0] != '#' {
		return rgb{}, fmt.Errorf("%q is not a colour; write it as #RRGGBB, for example #1F4FD1", s)
	}
	var c rgb
	for i := range 3 {
		v, err := strconv.ParseUint(s[1+2*i:3+2*i], 16, 8)
		if err != nil {
			return rgb{}, fmt.Errorf("%q is not a colour; write it as #RRGGBB, for example #1F4FD1", s)
		}
		c[i] = float64(v) / 255 //nolint:forbidigo // colour maths, not a business value
	}
	return c, nil
}

func (c rgb) hex() string {
	var b strings.Builder
	b.WriteByte('#')
	for _, v := range c {
		fmt.Fprintf(&b, "%02X", int(math.Round(math.Max(0, math.Min(1, v))*255)))
	}
	return b.String()
}

// Contrast returns the WCAG 2.x contrast ratio of two #RRGGBB colours (at least 1).
//
//nolint:forbidigo // colour maths, not a business value
func Contrast(a, b string) (float64, error) {
	ca, err := parseHex(a)
	if err != nil {
		return 0, err
	}
	cb, err := parseHex(b)
	if err != nil {
		return 0, err
	}
	la, lb := luminance(ca), luminance(cb)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05), nil
}

// luminance is the WCAG 2.x relative luminance.
//
//nolint:forbidigo // colour maths, not a business value
func luminance(c rgb) float64 {
	l := c.linear()
	return 0.2126*l[0] + 0.7152*l[1] + 0.0722*l[2]
}

// linear applies the standard sRGB transfer function (gamma decode).
func (c rgb) linear() rgb {
	var l rgb
	for i, v := range c {
		if v <= 0.04045 {
			l[i] = v / 12.92
		} else {
			l[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return l
}

// encode applies the inverse sRGB transfer function (gamma encode) to linear channels.
func (l rgb) encode() rgb {
	var c rgb
	for i, v := range l {
		if v <= 0.0031308 {
			c[i] = 12.92 * v
		} else {
			c[i] = 1.055*math.Pow(v, 1/2.4) - 0.055
		}
	}
	return c
}

// Mix interpolates from a (t=0) to b (t=1) in OKLab and returns #RRGGBB.
//
//nolint:forbidigo // colour maths, not a business value
func Mix(a, b string, t float64) (string, error) {
	ca, err := parseHex(a)
	if err != nil {
		return "", err
	}
	cb, err := parseHex(b)
	if err != nil {
		return "", err
	}
	if t < 0 || t > 1 {
		return "", fmt.Errorf("mix amount %v is outside 0 to 1; use a value from 0 to 1", t)
	}
	la, lb := toOKLab(ca.linear()), toOKLab(cb.linear())
	var m rgb
	for i := range m {
		m[i] = la[i] + (lb[i]-la[i])*t
	}
	return fromOKLab(m).encode().hex(), nil
}

// toOKLab and fromOKLab convert between linear sRGB and OKLab with the matrices from
// Björn Ottosson, "A perceptual color space for image processing" (2020),
// https://bottosson.github.io/posts/oklab/ (section "Converting from linear sRGB to Oklab").
func toOKLab(c rgb) rgb {
	l := math.Cbrt(0.4122214708*c[0] + 0.5363325363*c[1] + 0.0514459929*c[2])
	m := math.Cbrt(0.2119034982*c[0] + 0.6806995451*c[1] + 0.1073969566*c[2])
	s := math.Cbrt(0.0883024619*c[0] + 0.2817188376*c[1] + 0.6299787005*c[2])
	return rgb{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

func fromOKLab(c rgb) rgb {
	l := c[0] + 0.3963377774*c[1] + 0.2158037573*c[2]
	m := c[0] - 0.1055613458*c[1] - 0.0638541728*c[2]
	s := c[0] - 0.0894841775*c[1] - 1.2914855480*c[2]
	l, m, s = l*l*l, m*m*m, s*s*s
	return rgb{
		+4.0767416621*l - 3.3077115913*m + 0.2309699292*s,
		-1.2684380046*l + 2.6097574011*m - 0.3413193965*s,
		-0.0041960863*l - 0.7034186147*m + 1.7076147010*s,
	}
}
