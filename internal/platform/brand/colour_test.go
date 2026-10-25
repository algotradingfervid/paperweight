package brand_test

import (
	"math"
	"regexp"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/brand"
)

func TestContrast_KnownPairs(t *testing.T) {
	got, err := brand.Contrast("#141A23", "#FFFFFF")
	if err != nil {
		t.Fatal(err)
	}
	if r := math.Round(got*100) / 100; r != 17.47 {
		t.Fatalf("Contrast(#141A23, #FFFFFF) = %v, want 17.47", r)
	}
	got, err = brand.Contrast("#FFFFFF", "#1F4FD1")
	if err != nil {
		t.Fatal(err)
	}
	if got < 4.5 {
		t.Fatalf("Contrast(#FFFFFF, #1F4FD1) = %v, want at least 4.5", got)
	}
	if _, err := brand.Contrast("#12345", "#FFFFFF"); err == nil {
		t.Fatal("Contrast with an invalid colour: want an error")
	}
}

func TestMix_Endpoints(t *testing.T) {
	hex := regexp.MustCompile(`^#[0-9A-F]{6}$`)
	pairs := [][2]string{
		{"#1F4FD1", "#000000"},
		{"#0F6E56", "#FFFFFF"},
		{"#B3261E", "#EEF3FF"},
	}
	for _, p := range pairs {
		for _, tc := range []struct {
			t    float64
			want string
		}{{0, p[0]}, {1, p[1]}} {
			got, err := brand.Mix(p[0], p[1], tc.t)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Errorf("Mix(%s, %s, %v) = %s, want %s", p[0], p[1], tc.t, got, tc.want)
			}
		}
		mid, err := brand.Mix(p[0], p[1], 0.5)
		if err != nil {
			t.Fatal(err)
		}
		if !hex.MatchString(mid) {
			t.Errorf("Mix(%s, %s, 0.5) = %q, want #RRGGBB", p[0], p[1], mid)
		}
	}
}
