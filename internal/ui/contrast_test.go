package ui_test

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var hexTokenRe = regexp.MustCompile(`(--[a-z0-9-]+):\s*(#[0-9A-Fa-f]{6})\s*;`)

// parseTokens maps every `--name: #RRGGBB;` in css to its colour.
func parseTokens(css string) map[string]string {
	m := map[string]string{}
	for _, d := range hexTokenRe.FindAllStringSubmatch(css, -1) {
		m[d[1]] = strings.ToUpper(d[2])
	}
	return m
}

func luminance(hex string) float64 {
	var lin [3]float64
	for i := range lin {
		v, _ := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		c := float64(v) / 255
		if c <= 0.04045 {
			lin[i] = c / 12.92
		} else {
			lin[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*lin[0] + 0.7152*lin[1] + 0.0722*lin[2]
}

// contrast is the WCAG 2.x contrast ratio of two #RRGGBB colours.
func contrast(a, b string) float64 {
	l1, l2 := luminance(a), luminance(b)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

// tokenContrast is contrast over two token names; an unknown name is an error.
func tokenContrast(tokens map[string]string, fg, bg string) (float64, error) {
	a, ok := tokens[fg]
	if !ok {
		return 0, fmt.Errorf("tokens.css has no %s", fg)
	}
	b, ok := tokens[bg]
	if !ok {
		return 0, fmt.Errorf("tokens.css has no %s", bg)
	}
	return contrast(a, b), nil
}

type contrastPair struct {
	fg, bg        string
	ratio, minimum float64
}

// contrastPairs copies both research §9 contrast tables
// (docs/research/frontend-tech.md), less the "Disabled text" and
// "decorative only" rows, which have no minimum.
var contrastPairs = []contrastPair{
	// Foreground on the three backgrounds.
	{"--color-text", "--color-surface", 17.47, 4.5},
	{"--color-text", "--color-canvas", 16.16, 4.5},
	{"--color-text", "--color-sunken", 15.17, 4.5},
	{"--color-text-secondary", "--color-surface", 8.17, 4.5},
	{"--color-text-secondary", "--color-canvas", 7.56, 4.5},
	{"--color-text-secondary", "--color-sunken", 7.09, 4.5},
	{"--color-text-muted", "--color-surface", 5.63, 4.5},
	{"--color-text-muted", "--color-canvas", 5.21, 4.5},
	{"--color-text-muted", "--color-sunken", 4.89, 4.5},
	{"--color-border", "--color-surface", 3.68, 3.0},
	{"--color-border", "--color-canvas", 3.40, 3.0},
	{"--color-border", "--color-sunken", 3.19, 3.0},
	{"--color-primary-600", "--color-surface", 6.78, 4.5},
	{"--color-primary-600", "--color-canvas", 6.27, 4.5},
	{"--color-primary-600", "--color-sunken", 5.89, 4.5},
	{"--color-focus", "--color-surface", 6.78, 3.0},
	{"--color-focus", "--color-canvas", 6.27, 3.0},
	{"--color-focus", "--color-sunken", 5.89, 3.0},
	{"--color-success-700", "--color-surface", 6.18, 4.5},
	{"--color-success-700", "--color-canvas", 5.71, 4.5},
	{"--color-success-700", "--color-sunken", 5.36, 4.5},
	{"--color-warning-700", "--color-surface", 6.03, 4.5},
	{"--color-warning-700", "--color-canvas", 5.58, 4.5},
	{"--color-warning-700", "--color-sunken", 5.23, 4.5},
	{"--color-danger-700", "--color-surface", 6.54, 4.5},
	{"--color-danger-700", "--color-canvas", 6.04, 4.5},
	{"--color-danger-700", "--color-sunken", 5.67, 4.5},
	{"--color-info-700", "--color-surface", 6.28, 4.5},
	{"--color-info-700", "--color-canvas", 5.81, 4.5},
	{"--color-info-700", "--color-sunken", 5.45, 4.5},
	// Solid fills, tints and component pairs.
	{"--color-text-on-fill", "--color-primary-600", 6.78, 4.5},
	{"--color-text-on-fill", "--color-primary-700", 9.05, 4.5},
	{"--color-text-on-fill", "--color-primary-800", 11.10, 4.5},
	{"--color-text-on-fill", "--color-success-700", 6.18, 4.5},
	{"--color-text-on-fill", "--color-warning-700", 6.03, 4.5},
	{"--color-text-on-fill", "--color-danger-700", 6.54, 4.5},
	{"--color-text-on-fill", "--color-danger-800", 8.83, 4.5},
	{"--color-text-on-fill", "--color-info-700", 6.28, 4.5},
	{"--color-primary-800", "--color-primary-50", 9.99, 4.5},
	{"--color-primary-700", "--color-primary-50", 8.15, 4.5},
	{"--color-success-800", "--color-success-50", 8.33, 4.5},
	{"--color-warning-800", "--color-warning-50", 7.71, 4.5},
	{"--color-danger-800", "--color-danger-50", 7.83, 4.5},
	{"--color-info-800", "--color-info-50", 8.12, 4.5},
	{"--color-text", "--color-primary-50", 15.73, 4.5},
	{"--color-text", "--color-warning-50", 16.28, 4.5},
	{"--color-text-secondary", "--color-sunken", 7.09, 4.5},
	{"--color-success-600", "--color-surface", 4.40, 3.0},
	{"--color-warning-600", "--color-surface", 4.39, 3.0},
	{"--color-danger-600", "--color-surface", 4.99, 3.0},
	{"--color-info-600", "--color-surface", 4.71, 3.0},
	{"--color-success-600", "--color-success-50", 3.99, 3.0},
	{"--color-warning-600", "--color-warning-50", 4.09, 3.0},
	{"--color-danger-600", "--color-danger-50", 4.43, 3.0},
	{"--color-info-600", "--color-info-50", 4.23, 3.0},
	{"--color-primary-600", "--color-progress-track", 4.83, 3.0},
}

func TestContrast_ResearchTable(t *testing.T) {
	tokens := parseTokens(readFile(t, "css/tokens.css"))
	for _, p := range contrastPairs {
		got, err := tokenContrast(tokens, p.fg, p.bg)
		if err != nil {
			t.Error(err)
			continue
		}
		if r := math.Round(got*100) / 100; r != p.ratio {
			t.Errorf("%s on %s = %.2f, research lists %.2f", p.fg, p.bg, r, p.ratio)
		}
		if got < p.minimum {
			t.Errorf("%s on %s = %.2f, below %.1f", p.fg, p.bg, got, p.minimum)
		}
	}
}

func TestContrast_FocusOnEverySurface(t *testing.T) {
	tokens := parseTokens(readFile(t, "css/tokens.css"))
	bgs := []string{"--color-canvas", "--color-surface", "--color-sunken"}
	tints := 0
	for name := range tokens {
		if strings.HasSuffix(name, "-50") {
			bgs = append(bgs, name)
			tints++
		}
	}
	if tints == 0 {
		t.Fatal("tokens.css has no *-50 tints")
	}
	for _, bg := range bgs {
		got, err := tokenContrast(tokens, "--color-focus", bg)
		if err != nil {
			t.Error(err)
		} else if got < 3.0 {
			t.Errorf("--color-focus on %s = %.2f, below 3.0", bg, got)
		}
	}
}

func TestContrast_StateBadges(t *testing.T) {
	tokens := parseTokens(readFile(t, "css/tokens.css"))
	for _, b := range []struct{ state, fg, bg string }{
		{"Draft", "--color-text-secondary", "--color-sunken"},
		{"Cancelled", "--color-text-secondary", "--color-sunken"},
		{"Pending", "--color-warning-800", "--color-warning-50"},
		{"Approved", "--color-success-800", "--color-success-50"},
		{"Rejected", "--color-danger-800", "--color-danger-50"},
		{"Closed", "--color-primary-800", "--color-primary-50"},
	} {
		got, err := tokenContrast(tokens, b.fg, b.bg)
		if err != nil {
			t.Error(err)
		} else if got < 4.5 {
			t.Errorf("%s badge %s on %s = %.2f, below 4.5", b.state, b.fg, b.bg, got)
		}
	}
}

func TestContrast_UnknownTokenFails(t *testing.T) {
	tokens := parseTokens(readFile(t, "css/tokens.css"))
	if _, err := tokenContrast(tokens, "--color-nope", "--color-surface"); err == nil {
		t.Error("an unknown fg token gave no error")
	}
	if _, err := tokenContrast(tokens, "--color-text", "--color-nope"); err == nil {
		t.Error("an unknown bg token gave no error")
	}
}
