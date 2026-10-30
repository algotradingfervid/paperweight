package companyweb_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// ATT-028, render side (UX design 2026-10-31 §6 change 2(b)): the logo's
// Remove opens the confirm instead of posting; the confirm's form posts to
// the logo page's remove route and redraws the upload area, as Remove did.
// The click-through is TestE2E_CompanyLogoRemoveConfirms.
func TestLogo_RemoveOpensTheConfirm(t *testing.T) {
	lw := seeded(t)
	kavya := lw.h.Env.Kavya
	lw.post(t, "logo.png", filestest.PNG(1024))
	metas := lw.logos(t)
	if len(metas) != 1 {
		t.Fatalf("logos = %+v; want one", metas)
	}
	id := strconv.FormatInt(metas[0].ID, 10)
	dialogID := "remove-file-" + id

	page := lw.h.Get(t, kavya, logoPath)
	webtest.WantStatus(t, page, http.StatusOK)
	squash := func(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

	rm := page.Doc.Find(`button[aria-label="Remove logo.png"]`)
	if rm.Length() != 1 {
		t.Fatalf("found %d Remove logo.png buttons; want 1: %s", rm.Length(), webtest.Squash(page.Body))
	}
	if got, _ := rm.Attr("type"); got != "button" {
		t.Errorf("Remove type = %q; want button, so it posts nothing", got)
	}
	if got, _ := rm.Attr("data-open-dialog"); got != dialogID {
		t.Errorf("Remove data-open-dialog = %q; want %q", got, dialogID)
	}

	want := removePath + "?version=1&logo=" + id
	forms := page.Doc.Find(`form[action="` + want + `"]`)
	if forms.Length() != 1 {
		t.Fatalf("found %d forms posting %s; want only the confirm's", forms.Length(), want)
	}
	dlg := forms.Closest("dialog")
	if got, _ := dlg.Attr("id"); got != dialogID {
		t.Fatalf("the remove form is not in dialog#%s (in %q)", dialogID, got)
	}
	if got := squash(dlg.Find("h2")); got != "Remove this file?" {
		t.Errorf("confirm heading = %q", got)
	}
	if got, body := squash(dlg), "logo.png will be removed from this document. You can attach it again later."; !strings.Contains(got, body) {
		t.Errorf("confirm text = %q; want %q", got, body)
	}
	if got, _ := forms.Attr("hx-post"); got != want {
		t.Errorf("confirm hx-post = %q; want %q", got, want)
	}
	if got, _ := forms.Attr("hx-target"); got != "#company-logo-area" {
		t.Errorf("confirm hx-target = %q; want #company-logo-area, the area the remove answer redraws", got)
	}
	if got := squash(forms.Find(`button[type=submit]`)); got != "Remove file" {
		t.Errorf("confirm submit = %q; want Remove file", got)
	}
	keep := dlg.Find(`button[type=button][data-close-dialog]`).FilterFunction(func(_ int, b *goquery.Selection) bool { return squash(b) == "Keep file" })
	if keep.Length() != 1 {
		t.Errorf("found %d Keep file buttons; want 1", keep.Length())
	}
	if dlg.Closest("#company-logo-area").Length() != 1 {
		t.Error("the confirm is outside the upload area, so the redraw after a remove leaves it behind")
	}
}
