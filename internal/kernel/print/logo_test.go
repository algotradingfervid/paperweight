package print_test

import (
	"regexp"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
	"github.com/vardhan/internaltools/internal/platform/brand"
)

var logoImg = regexp.MustCompile(`<img class="logo"[^>]*>`)

// CD-140: the uploaded logo, else the brand logo; print.css sizes it.
func TestPrint_LetterheadLogo(t *testing.T) {
	for _, tc := range []struct{ logo, src string }{
		{"/files/1", "/files/1"},
		{"", brand.DocumentLogoURL},
	} {
		d := printtest.ReferencePO()
		d.Letterhead.LogoURL = tc.logo
		html := renderTrade(t, d, false)
		mustContain(t, html, `<img class="logo" src="`+tc.src+`"`)
		img := logoImg.FindString(html)
		if regexp.MustCompile(`\s(width|height)=`).MatchString(img) {
			t.Errorf("LogoURL %q: logo img has width/height: %s", tc.logo, img)
		}
	}
}
