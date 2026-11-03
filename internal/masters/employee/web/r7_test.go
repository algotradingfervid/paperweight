package employeeweb_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// UI v2 §5.6, shell M2–M4: a master wizard step's primary reads Continue and
// Check's reads Save; the wizard head's × is the one exit (no Back link above
// it); Start over opens "Start over?", whose confirm posts the same form, and
// without JavaScript the opener posts it straight.
func TestScreens_EmployeeWizardR7(t *testing.T) {
	w := newWorld(t)
	b := w.h.Browser(w.h.Env.Kavya)
	primary := func(r webtest.Response) string {
		return webtest.Squash(r.Doc.Find(".actionbar button.btn-primary").Text())
	}
	first := b.Get(t, stepOne)
	if got := primary(first); got != "Continue" {
		t.Errorf("step 1 primary = %q, want Continue", got)
	}
	if n := first.Doc.Find("main a.back-link").Length(); n != 0 {
		t.Errorf("%d Back links beside the wizard head's ×, want 0", n)
	}
	if n := first.Doc.Find(".wizard-head a.wizard-close").Length(); n != 1 {
		t.Errorf("%d wizard × links, want 1", n)
	}
	w.toCheck(t, b, details("EMP-131"), w.reporting())
	check := b.Get(t, stepCheck)
	if got := primary(check); got != "Save" {
		t.Errorf("Check primary = %q, want Save", got)
	}
	const startOver = wizardMain + "/start-over"
	form := check.Doc.Find(`form[action="` + startOver + `"]`)
	if form.Length() != 1 {
		t.Fatalf("%d Start over forms, want 1", form.Length())
	}
	opener := form.Children().Filter("button")
	dlgID := opener.AttrOr("data-open-dialog", "")
	if opener.Length() != 1 || webtest.Squash(opener.Text()) != "Start over" || dlgID == "" || opener.AttrOr("type", "submit") != "submit" {
		t.Fatalf("opener: %d, %q, dialog %q, type %q; want one submit Start over opening a dialog", opener.Length(), webtest.Squash(opener.Text()), dlgID, opener.AttrOr("type", ""))
	}
	dlg := form.Find("dialog#" + dlgID)
	if dlg.Length() != 1 {
		t.Fatalf("no dialog#%s inside the Start over form", dlgID)
	}
	if got := webtest.Squash(dlg.Find("h2").Text()); got != "Start over?" {
		t.Errorf("dialog title = %q", got)
	}
	if got := webtest.Squash(dlg.Find("p").Text()); got != "What you typed in this wizard is cleared." {
		t.Errorf("dialog text = %q", got)
	}
	keep := dlg.Find(`button[data-close-dialog]`).FilterFunction(func(_ int, s *goquery.Selection) bool { return webtest.Squash(s.Text()) == "Keep editing" })
	if keep.Length() != 1 {
		t.Errorf("%d Keep editing buttons, want 1", keep.Length())
	}
	confirm := dlg.Find(`button.btn-error`)
	if confirm.Length() != 1 || webtest.Squash(confirm.Text()) != "Start over" || confirm.AttrOr("type", "submit") != "submit" {
		t.Errorf("confirm: %d, %q; want one danger submit Start over", confirm.Length(), webtest.Squash(confirm.Text()))
	}
}
