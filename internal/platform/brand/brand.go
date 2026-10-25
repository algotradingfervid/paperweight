// Package brand holds the build-time branding (design-system §5.1, CD-99): the app name,
// brand colours and logo files from brand.json and assets/, checked for contrast and
// logo size when the tests and go generate run. It imports no internal package.
package brand

//go:generate go run ./gen

import (
	"bytes"
	"embed"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
)

// Config is the parsed brand.json. Colours are #RRGGBB in upper case; image fields are
// file names inside assets/.
type Config struct {
	AppName        string `json:"app_name"`
	Primary        string `json:"primary"`
	Primary700     string `json:"primary_700"`
	Primary800     string `json:"primary_800"`
	Primary50      string `json:"primary_50"`
	DocumentAccent string `json:"document_accent"`
	AppLogo        string `json:"app_logo"`
	DocumentLogo   string `json:"document_logo"`
	Favicon        string `json:"favicon"`
}

// Fixed token neutrals the brand colours are checked against.
const (
	surface = "#FFFFFF" // also text on a primary fill
	canvas  = "#F5F6F8"
	sunken  = "#EDEFF2"
)

//go:embed brand.json assets
var embedded embed.FS

// assets returns the embedded assets/ folder.
func assets() fs.FS {
	sub, err := fs.Sub(embedded, "assets")
	if err != nil {
		panic(err)
	}
	return sub
}

// Load parses the embedded brand.json and assets/. It panics on an invalid config;
// the tests and go generate fail first, so a bad config never builds.
func Load() Config {
	b, err := embedded.ReadFile("brand.json")
	if err != nil {
		panic(err)
	}
	c, err := Parse(b, assets())
	if err != nil {
		panic(err)
	}
	return c
}

// Parse reads a brand.json; files holds the images it names.
func Parse(b []byte, files fs.FS) (Config, error) {
	var c Config
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		if key, ok := strings.CutPrefix(err.Error(), "json: unknown field "); ok {
			return Config{}, fmt.Errorf("brand.json: unknown key %s; remove it (the keys are listed in internal/platform/brand/README.md)", key)
		}
		return Config{}, fmt.Errorf("brand.json: %v; fix the file so it is valid JSON", err)
	}
	if strings.TrimSpace(c.AppName) == "" {
		return Config{}, errors.New(`brand.json: "app_name" is missing; set it to the name shown in the app header`)
	}
	if err := c.colours(); err != nil {
		return Config{}, err
	}
	for _, im := range c.slots() {
		if _, _, err := measure(files, im); err != nil {
			return Config{}, err
		}
	}
	return c, c.checkContrast()
}

// colours validates and upper-cases the colours, then fills in the derived ones.
func (c *Config) colours() error {
	if c.Primary == "" {
		return errors.New(`brand.json: "primary" is missing; set it to the main brand colour as #RRGGBB`)
	}
	derived := []struct {
		key  string
		v    *string
		with string
		t    float64 //nolint:forbidigo // colour maths, not a business value
	}{
		{"primary", &c.Primary, "", 0},
		{"primary_700", &c.Primary700, "#000000", 0.15},
		{"primary_800", &c.Primary800, "#000000", 0.30},
		{"primary_50", &c.Primary50, "#FFFFFF", 0.93},
		{"document_accent", &c.DocumentAccent, "", 0},
	}
	for _, d := range derived {
		if *d.v == "" {
			if d.with == "" { // document_accent defaults to primary
				*d.v = c.Primary
				continue
			}
			m, err := Mix(c.Primary, d.with, d.t)
			if err != nil {
				return err
			}
			*d.v = m
			continue
		}
		if _, err := parseHex(*d.v); err != nil {
			return fmt.Errorf("brand.json: %q: %v", d.key, err)
		}
		*d.v = strings.ToUpper(*d.v)
	}
	return nil
}

// checkContrast applies every contrast pair of design-system §5.1. The focus ring is
// primary, so its 3:1 rule on each surface is covered by the 4.5:1 text checks.
func (c Config) checkContrast() error {
	type pair struct {
		key, fg, bg, what string
		min               float64 //nolint:forbidigo // colour maths, not a business value
	}
	pairs := []pair{
		{"primary", surface, c.Primary, "white text on it", 4.5},
		{"primary_700", surface, c.Primary700, "white text on it", 4.5},
		{"primary", c.Primary, surface, "as text on the surface " + surface, 4.5},
		{"primary", c.Primary, canvas, "as text on the canvas " + canvas, 4.5},
		{"primary", c.Primary, sunken, "as text on the sunken background " + sunken, 4.5},
		{"document_accent", c.DocumentAccent, surface, "as text on white", 4.5},
	}
	for _, p := range pairs {
		r, err := Contrast(p.fg, p.bg)
		if err != nil {
			return err
		}
		if r < p.min {
			v := p.fg
			if p.fg == surface {
				v = p.bg
			}
			return fmt.Errorf("brand.json: %q %s is too light: %s is %.2f:1 and needs at least %.1f:1; choose a darker colour", p.key, v, p.what, r, p.min)
		}
	}
	r, err := Contrast(c.Primary800, c.Primary50)
	if err != nil {
		return err
	}
	if r < 4.5 {
		return fmt.Errorf(`brand.json: "primary_800" %s on "primary_50" %s is %.2f:1 and needs at least 4.5:1; choose a darker primary_800 or a lighter primary_50`, c.Primary800, c.Primary50, r)
	}
	return nil
}

// slot is one image slot of brand.json and its rules from design-system §5.1.
type slot struct {
	key, name string
	exts      []string
	maxBytes  int64
	logo      bool // aspect between 1:2 and 8:1
	favicon   bool // square, at least 32 × 32
}

func (c Config) slots() []slot {
	return []slot{
		{key: "app_logo", name: c.AppLogo, exts: []string{".svg", ".png"}, maxBytes: 200 << 10, logo: true},
		{key: "document_logo", name: c.DocumentLogo, exts: []string{".png", ".jpg", ".jpeg"}, maxBytes: 1 << 20, logo: true},
		{key: "favicon", name: c.Favicon, exts: []string{".png"}, favicon: true},
	}
}

// measure checks one image against its slot's rules and returns its intrinsic size.
func measure(files fs.FS, im slot) (w, h int, err error) {
	if im.name == "" {
		return 0, 0, fmt.Errorf("brand.json: %q is missing; set it to a file name in internal/platform/brand/assets/", im.key)
	}
	ext := strings.ToLower(path.Ext(im.name))
	if !slices.Contains(im.exts, ext) {
		return 0, 0, fmt.Errorf("brand.json: %q %s must be one of %s; convert the file", im.key, im.name, strings.Join(im.exts, ", "))
	}
	data, err := fs.ReadFile(files, im.name)
	if err != nil {
		return 0, 0, fmt.Errorf("brand.json: %q %s cannot be read (%v); put the file in internal/platform/brand/assets/", im.key, im.name, err)
	}
	if im.maxBytes > 0 && int64(len(data)) > im.maxBytes {
		return 0, 0, fmt.Errorf("brand.json: %q %s is %d KB, more than the %d KB allowed; make the file smaller", im.key, im.name, (len(data)+1023)/1024, im.maxBytes>>10)
	}
	switch ext {
	case ".svg":
		w, h, err = svgSize(data)
	case ".png":
		cfg, e := png.DecodeConfig(bytes.NewReader(data))
		w, h, err = cfg.Width, cfg.Height, e
	default:
		cfg, e := jpeg.DecodeConfig(bytes.NewReader(data))
		w, h, err = cfg.Width, cfg.Height, e
	}
	if errors.Is(err, errNoViewBox) {
		return 0, 0, fmt.Errorf(`brand.json: %q %s has no valid viewBox on its <svg> element; add viewBox="0 0 <width> <height>"`, im.key, im.name)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("brand.json: %q %s cannot be decoded (%v); save it again as a valid %s file", im.key, im.name, err, ext)
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("brand.json: %q %s has size %d × %d; give it a positive width and height", im.key, im.name, w, h)
	}
	if im.logo && (2*w < h || w > 8*h) {
		return 0, 0, fmt.Errorf("brand.json: %q %s is %d × %d; its width:height must be between 1:2 and 8:1, so crop or pad it", im.key, im.name, w, h)
	}
	if im.favicon && (w != h || w < 32) {
		return 0, 0, fmt.Errorf("brand.json: %q %s is %d × %d; it must be square and at least 32 × 32", im.key, im.name, w, h)
	}
	return w, h, nil
}

var errNoViewBox = errors.New("no valid viewBox")

// svgSize returns the width and height of the root element's viewBox, rounded.
func svgSize(data []byte) (w, h int, err error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return 0, 0, errors.New("no <svg> element")
		}
		if err != nil {
			return 0, 0, err
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		if el.Name.Local != "svg" {
			return 0, 0, errors.New("the root element is not <svg>")
		}
		for _, a := range el.Attr {
			if a.Name.Local != "viewBox" {
				continue
			}
			f := strings.FieldsFunc(a.Value, func(r rune) bool { return unicode.IsSpace(r) || r == ',' })
			if len(f) != 4 {
				break
			}
			vw, e1 := strconv.ParseFloat(f[2], 64)
			vh, e2 := strconv.ParseFloat(f[3], 64)
			if e1 != nil || e2 != nil {
				break
			}
			return int(math.Round(vw)), int(math.Round(vh)), nil
		}
		return 0, 0, errNoViewBox
	}
}

// CSS returns the app theme overrides as one unlayered :root block.
func CSS(c Config) string {
	return ":root{" +
		"--color-primary-600:" + c.Primary + ";" +
		"--color-primary-700:" + c.Primary700 + ";" +
		"--color-primary-800:" + c.Primary800 + ";" +
		"--color-primary-50:" + c.Primary50 + ";" +
		"--color-focus:" + c.Primary + ";" +
		"--color-primary:" + c.Primary + ";" +
		"--color-accent:" + c.Primary + ";" +
		"--color-doc-accent:" + c.DocumentAccent + "}"
}

// PrintCSS returns the print stylesheet's accent variable.
func PrintCSS(c Config) string {
	return ":root{--doc-accent:" + c.DocumentAccent + "}"
}
