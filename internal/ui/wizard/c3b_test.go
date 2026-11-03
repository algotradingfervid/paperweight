package wizard_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// TestFrame_LeaveDialog (UI v2 §5.6, C3b, CD-387 (4)): every × goes to Close
// and opens "Leave without saving?" only when the step form has unsaved
// changes (data-if-dirty, app.js); Leave goes to Close. A saved document's
// dialog says the draft keeps what was saved. Without Close there is neither.
func TestFrame_LeaveDialog(t *testing.T) {
	newDoc := docModel()
	newDoc.DocLabel = ""
	master := docModel()
	master.Mode = wizard.Master
	master.Steps = steps("Details", "Addresses", "Contacts", "Check")
	for _, c := range []struct {
		name string
		m    wizard.Model
		body string
	}{
		{"saved document", docModel(), "What you changed on this step is lost. The draft keeps what was saved before."},
		{"new document", newDoc, "What you typed in this wizard is lost."},
		{"master wizard", master, "What you typed in this wizard is lost."},
	} {
		t.Run(c.name, func(t *testing.T) {
			c.m.Close = "/doc/po"
			d := renderFrame(t, c.m, wizard.Bar{})
			x := d.Find(".wizard-head > a.wizard-close")
			if x.Length() != 1 || x.AttrOr("href", "") != "/doc/po" {
				t.Fatalf("close link: %d, href %q", x.Length(), x.AttrOr("href", ""))
			}
			dlg := d.Find("dialog#leave-wizard")
			opener, opens := x.Attr("data-open-dialog")
			if got := x.AttrOr("data-if-dirty", ""); got != "wizard-form" {
				t.Errorf("× data-if-dirty = %q, want wizard-form", got)
			}
			if !opens || opener != "leave-wizard" || x.AttrOr("aria-haspopup", "") != "dialog" {
				t.Errorf("× opener = %q, aria-haspopup %q; want leave-wizard, dialog", opener, x.AttrOr("aria-haspopup", ""))
			}
			if dlg.Length() != 1 {
				t.Fatalf("%d leave dialogs, want 1", dlg.Length())
			}
			if got := norm(dlg.Find("h2").Text()); got != "Leave without saving?" {
				t.Errorf("title = %q", got)
			}
			if got := norm(dlg.Find("p").Text()); got != c.body {
				t.Errorf("body = %q", got)
			}
			if k := dlg.Find("button[data-close-dialog]").FilterFunction(func(_ int, s *goquery.Selection) bool { return norm(s.Text()) == "Keep editing" }); k.Length() != 1 {
				t.Error("no Keep editing button that closes the dialog")
			}
			if l := dlg.Find("a.btn"); l.Length() != 1 || norm(l.Text()) != "Leave" || l.AttrOr("href", "") != "/doc/po" {
				t.Errorf("Leave link: %d, %q, href %q; want one Leave to /doc/po", l.Length(), norm(l.Text()), l.AttrOr("href", ""))
			}
		})
	}
	// A step redrawn with errors or a refusal holds values that weren't
	// saved: its form is marked, so the × asks however it was reached.
	for _, c := range []struct {
		name   string
		adjust func(*wizard.Model)
		marked bool
	}{
		{"plain", func(*wizard.Model) {}, false},
		{"errors", func(m *wizard.Model) { m.Errors = []components.FieldErr{{ID: "f-a", Message: "A is required."}} }, true},
		{"conflict", func(m *wizard.Model) { m.Conflict = "Someone saved this first." }, true},
		{"danger", func(m *wizard.Model) { m.Danger = "This can't be saved." }, true},
	} {
		t.Run("unsaved "+c.name, func(t *testing.T) {
			m := docModel()
			m.Close = "/doc/po"
			c.adjust(&m)
			_, got := renderFrame(t, m, wizard.Bar{}).Find("form#wizard-form").Attr("data-unsaved")
			if got != c.marked {
				t.Errorf("form data-unsaved = %v, want %v", got, c.marked)
			}
		})
	}
	t.Run("no Close", func(t *testing.T) {
		if n := renderFrame(t, newDoc, wizard.Bar{}).Find("dialog#leave-wizard").Length(); n != 0 {
			t.Errorf("%d leave dialogs without Close, want 0", n)
		}
	})
}
