package companyweb_test

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/company"
)

// No case ID: the logo's stale upload is the conflict banner (CD-365), a
// warning with "Reload Company logo" to the logo page, on the page and in the
// htmx swap; a refused file stays the danger error summary with no warning.
func TestLogo_ConflictBanner(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya
	opened := lw.company(t).Version
	if err := lw.svc.SaveCompany(context.Background(), kavya, company.CompanyInput{LegalName: newName, PAN: pan}, opened); err != nil {
		t.Fatal(err)
	}
	for _, htmx := range []bool{false, true} {
		resp := lw.upload(t, kavya, logoPath+"?version="+strconv.Itoa(opened), "new.png", filestest.PNG(2048), htmx)
		webtest.WantStatus(t, resp, http.StatusConflict)
		b := resp.Doc.Find("#company-logo-area .banner")
		if b.Length() != 1 || !b.HasClass("banner--warning") {
			t.Fatalf("htmx %v: banners = %d (class %q); want one warning", htmx, b.Length(), b.AttrOr("class", ""))
		}
		if !strings.Contains(webtest.Squash(b.Text()), staleCompany) {
			t.Errorf("htmx %v: banner %q lacks %q", htmx, webtest.Squash(b.Text()), staleCompany)
		}
		a := b.Find("a.btn")
		if got := webtest.Squash(a.Text()); a.Length() != 1 || got != "Reload Company logo" || a.AttrOr("href", "") != logoPath {
			t.Errorf("htmx %v: Reload button = %q -> %q (%d); want \"Reload Company logo\" -> %s", htmx, got, a.AttrOr("href", ""), a.Length(), logoPath)
		}
	}

	resp := lw.post(t, "logo.svg", filestest.SVG())
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	if n := resp.Doc.Find("#main .banner--warning").Length(); n != 0 {
		t.Errorf("422 has %d warning banners; want none", n)
	}
	if n := resp.Doc.Find("#main .error-summary").Length(); n != 1 {
		t.Errorf("422 has %d error summaries; want 1", n)
	}
}
