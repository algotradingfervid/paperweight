package wizard_test

import (
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// TestFrame_LeaveDialog (UI v2 §5.6, C3b): the × of a saved document goes
// straight to Close; the × of a new document or of a master wizard opens
// "Leave without saving?", whose Leave goes to Close. Without Close there
// is neither.
func TestFrame_LeaveDialog(t *testing.T) {
	newDoc := docModel()
	newDoc.DocLabel = ""
	master := docModel()
	master.Mode = wizard.Master
	master.Steps = steps("Details", "Addresses", "Contacts", "Check and save")
	for _, c := range []struct {
		name   string
		m      wizard.Model
		dialog bool
	}{
		{"saved document", docModel(), false},
		{"new document", newDoc, true},
		{"master wizard", master, true},
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
			if !c.dialog {
				if dlg.Length() != 0 || opens {
					t.Errorf("a saved document's × asks before leaving (dialog %d, opener %q)", dlg.Length(), opener)
				}
				return
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
			if got := norm(dlg.Find("p").Text()); got != "What you typed in this wizard is lost." {
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
	t.Run("no Close", func(t *testing.T) {
		if n := renderFrame(t, newDoc, wizard.Bar{}).Find("dialog#leave-wizard").Length(); n != 0 {
			t.Errorf("%d leave dialogs without Close, want 0", n)
		}
	})
}
