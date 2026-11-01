package wizard_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
	"github.com/vardhan/internaltools/internal/ui/wizard"
)

// TestRespond_SavedTrigger (UI v2 §5.6, C3): every htmx step save, Back
// included, and a save that leaves the wizard carry HX-Trigger saved with
// "Saved {HH:MM}" on the IST business clock; a redraw (422, 409) or a plain
// POST saved nothing the cue can show. 18:31 UTC is 00:01 IST the next day.
func TestRespond_SavedTrigger(t *testing.T) {
	at := time.Date(2026, 11, 1, 18, 31, 40, 0, time.UTC)
	const want = `{"saved":"Saved 00:01"}`
	page := func(bool) templ.Component { return templ.Raw(`<div id="wizard"></div>`) }
	cases := []struct {
		name string
		o    wizard.Outcome
		htmx bool
		nav  string
		want string
	}{
		{"advance", wizard.Advance, true, "", want},
		{"back", wizard.Advance, true, "back", want},
		{"stay", wizard.Advance, true, "stay", want},
		{"finish", wizard.Finish, true, "finish", want},
		{"invalid", wizard.Invalid, true, "", ""},
		{"conflict", wizard.Conflict, true, "", ""},
		{"advance plain", wizard.Advance, false, "", ""},
		{"finish plain", wizard.Finish, false, "finish", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				wizard.Respond(w, r.WithContext(wizard.WithNow(r.Context(), func() time.Time { return at })), c.o, "/next", page)
			})
			var hdr []string
			if c.htmx {
				hdr = []string{"HX-Request", "true"}
			}
			rec := uitest.Do(t, h, http.MethodPost, "/step", url.Values{"nav": {c.nav}}, hdr...)
			if got := rec.Header().Get("HX-Trigger"); got != c.want {
				t.Errorf("HX-Trigger = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRespond_SavedTriggerNeedsAClock: without WithNow there is no clock,
// so a save carries no HX-Trigger (the cue never shows a wall-clock time).
func TestRespond_SavedTriggerNeedsAClock(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wizard.Respond(w, r, wizard.Advance, "/next", func(bool) templ.Component { return templ.Raw("") })
	})
	rec := uitest.Do(t, h, http.MethodPost, "/step", url.Values{}, "HX-Request", "true")
	if got, ok := rec.Header()[http.CanonicalHeaderKey("HX-Trigger")]; ok {
		t.Errorf("HX-Trigger = %q, want no header without a clock", got)
	}
}

// TestRespond_SavedTriggerUsesIST: a clock in another zone still reads IST.
func TestRespond_SavedTriggerUsesIST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip(err)
	}
	at := time.Date(2026, 11, 2, 0, 12, 0, 0, ny)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wizard.Respond(w, r.WithContext(wizard.WithNow(r.Context(), func() time.Time { return at })), wizard.Advance, "/next", func(bool) templ.Component { return templ.Raw("") })
	})
	rec := uitest.Do(t, h, http.MethodPost, "/step", url.Values{}, "HX-Request", "true")
	if got, want := rec.Header().Get("HX-Trigger"), `{"saved":"Saved 10:42"}`; got != want {
		t.Errorf("HX-Trigger = %q, want %q", got, want)
	}
}

func norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// TestFrame_WizardHead (UI v2 §5.6, N12): a document's head has its kind as
// the meta line, the h1 "New purchase order" before the first save and the
// draft's label after, and the save cue; a master wizard's head has its
// title as the h1 and no cue.
func TestFrame_WizardHead(t *testing.T) {
	saved := docModel()
	fresh := docModel()
	fresh.DocLabel = ""
	freshCaps := fresh
	freshCaps.Title = "GST Purchase Order"
	master := docModel()
	master.Mode, master.Title, master.DocLabel = wizard.Master, "New party", ""
	titled := docModel()
	titled.Title = "Purchase Order"
	cases := []struct {
		name          string
		m             wizard.Model
		meta, h1, cue string
	}{
		{"new document", fresh, "Purchase order", "New purchase order", "Not saved yet Saving…"},
		{"new document, title case", freshCaps, "GST purchase order", "New GST purchase order", "Not saved yet Saving…"},
		{"saved draft", saved, "Purchase order", "Draft 42", "Saving…"},
		{"master", master, "", "New party", ""},
		{"title case kind", titled, "Purchase order", "Draft 42", "Saving…"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := renderFrame(t, c.m, wizard.Bar{})
			head := d.Find("#wizard > .wizard-head")
			if head.Length() != 1 || head.Index() != 0 {
				t.Fatalf("want the wizard head as #wizard's first child, got %d", head.Length())
			}
			if got := norm(head.Find(".wizard-head__meta").Text()); got != c.meta {
				t.Errorf("meta line = %q, want %q", got, c.meta)
			}
			if h := d.Find("h1"); h.Length() != 1 || h.Closest(".wizard-head").Length() != 1 || norm(h.Text()) != c.h1 {
				t.Errorf("h1 = %d %q, want one in the head: %q", h.Length(), norm(h.Text()), c.h1)
			}
			cue := head.Find(".save-cue")
			if got := norm(cue.Text()); got != c.cue {
				t.Errorf("cue text = %q, want %q", got, c.cue)
			}
			if c.cue == "" {
				if cue.Length() != 0 || d.Find("#"+wizard.SavedAtID).Length() != 0 {
					t.Error("a master wizard has no save cue")
				}
				return
			}
			if id, _ := cue.Attr("id"); id != "save-cue" {
				t.Errorf("cue id = %q, want save-cue", id)
			}
			if _, ok := cue.Attr("hx-preserve"); !ok {
				t.Error("the cue has no hx-preserve: a step swap would lose what app.js wrote")
			}
			at := cue.Find("#" + wizard.SavedAtID)
			if at.Length() != 1 || at.AttrOr("aria-live", "") != "polite" || norm(at.Text()) != "" {
				t.Errorf("#%s: %d, aria-live=%q, text %q; want one empty polite live region", wizard.SavedAtID, at.Length(), at.AttrOr("aria-live", ""), at.Text())
			}
			if _, ok := cue.Attr("aria-live"); ok {
				t.Error("only #saved-at is live: Saving… and Not saved yet aren't announced")
			}
		})
	}
}

// TestFrame_CloseX: with Close set the head starts with a 44 px × link
// named Close; without it there is none.
func TestFrame_CloseX(t *testing.T) {
	m := docModel()
	m.Close = "/m/purchase"
	a := renderFrame(t, m, wizard.Bar{}).Find(".wizard-head > a.wizard-close")
	if a.Length() != 1 || a.AttrOr("href", "") != "/m/purchase" || a.AttrOr("aria-label", "") != "Close" || a.Index() != 0 {
		t.Errorf("close link: %d, href %q, aria-label %q; want the head's first child, /m/purchase, Close", a.Length(), a.AttrOr("href", ""), a.AttrOr("aria-label", ""))
	}
	if a.Find("svg").Length() != 1 {
		t.Error("the close link has no × icon")
	}
	if n := renderFrame(t, docModel(), wizard.Bar{}).Find(".wizard-close").Length(); n != 0 {
		t.Errorf("%d close links without Close, want none", n)
	}
}

// TestFrame_BackHasChevron (UI v2 §5.6): Back is a secondary with the
// chevron-left icon, and still the bar's second submit.
func TestFrame_BackHasChevron(t *testing.T) {
	d := renderFrame(t, docModel(), wizard.Bar{Back: true})
	b := d.Find(`.actionbar button[name="nav"][value="back"]`)
	if b.Length() != 1 || norm(b.Text()) != "Back" || !b.HasClass("btn--secondary") {
		t.Fatalf("Back: %d, %q", b.Length(), norm(b.Text()))
	}
	if b.Find("svg").Length() != 1 {
		t.Error("Back has no icon")
	}
	var want strings.Builder
	if err := icons.Icon("chevron-left").Render(context.Background(), &want); err != nil {
		t.Fatal(err)
	}
	if html, _ := goquery.OuterHtml(b); !strings.Contains(html, want.String()) {
		t.Errorf("Back's icon isn't chevron-left: %s", html)
	}
}
