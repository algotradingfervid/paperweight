package docscreen_test

import (
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// WP1-UI Task 14 review fixes.

// wizardFormID is the step form's id; the bar's buttons post it through
// their form attribute.
const wizardFormID = "wizard-form"

// htmxSelect evaluates an htmx extended selector (hx-disabled-elt,
// hx-target) from elt the way htmx 2's querySelectorAllExt does: the value
// is split on top-level commas; "this" is elt; "find X" is elt's first
// descendant matching X; "closest X" is the nearest ancestor-or-self
// matching X; "next X" / "previous X" the first match after / before elt
// in tree order; every other part is a CSS selector run on the document.
func htmxSelect(t *testing.T, doc *goquery.Document, elt *goquery.Selection, sel string) *goquery.Selection {
	t.Helper()
	if strings.TrimSpace(sel) == "this" {
		return elt
	}
	out := elt.Slice(0, 0)
	var plain []string
	order := doc.Find("*")
	at := order.IndexOfSelection(elt)
	for _, part := range strings.Split(sel, ",") {
		part = strings.TrimSpace(part)
		switch {
		case strings.HasPrefix(part, "find "):
			out = out.AddSelection(elt.Find(strings.TrimSpace(part[5:])).First())
		case strings.HasPrefix(part, "closest "):
			out = out.AddSelection(elt.Closest(strings.TrimSpace(part[8:])))
		case strings.HasPrefix(part, "next "):
			out = out.AddSelection(order.Slice(at+1, order.Length()).Filter(strings.TrimSpace(part[5:])).First())
		case strings.HasPrefix(part, "previous "):
			out = out.AddSelection(order.Slice(0, at).Filter(strings.TrimSpace(part[9:])).Last())
		case part == "this", part == "next", part == "previous", part == "document", part == "window", part == "body", part == "root", part == "host":
			t.Fatalf("htmxSelect: %q is not handled by this test helper", part)
		case part != "":
			plain = append(plain, part)
		}
	}
	if len(plain) > 0 {
		out = out.AddSelection(doc.Find(strings.Join(plain, ",")))
	}
	return out
}

// wizardSubmits are the submit buttons that belong to the step form, in
// tree order: inside it without a form attribute, or form="wizard-form".
func wizardSubmits(doc *goquery.Document) *goquery.Selection {
	return doc.Find(`button[type="submit"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		if f, ok := s.Attr("form"); ok {
			return f == wizardFormID
		}
		return s.Closest("form#"+wizardFormID).Length() == 1
	})
}

// Finding 1: the check page's submit form names where its htmx response
// goes, and every answer (the document page after success, a 409 refusal,
// a 422 sending the user to the first failing step) has that one element
// as its root, so the outerHTML swap replaces it.
func TestCheckPage_SubmitResponseMatchesTarget(t *testing.T) {
	const conflict = "Someone else changed Draft 42 after you opened it. Reload it and make your changes again."
	cases := []struct {
		name   string
		result doc.Result
		err    error
		status int
		check  func(t *testing.T, rec *http.Response, page *goquery.Document)
	}{
		{"success", doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}, nil, http.StatusOK,
			func(t *testing.T, _ *http.Response, page *goquery.Document) {
				want := "Approved. SO/26-27/00001 has been numbered."
				if got := bannerTexts(page.Selection, "success"); !slices.Equal(got, []string{want}) {
					t.Errorf("success banners = %q, want [%q] on the document page", got, want)
				}
			}},
		{"409", doc.Result{}, statusErr{status: http.StatusConflict, msg: conflict}, http.StatusConflict,
			func(t *testing.T, _ *http.Response, page *goquery.Document) {
				got := bannerTexts(page.Selection, "danger")
				if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, conflict) }) {
					t.Errorf("danger banners = %q, want DOC's %q", got, conflict)
				}
			}},
		{"422", doc.Result{}, statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.",
			fields: []doctype.FieldError{{Field: "qty", Line: 1, Message: "Quantity must be more than zero."}}}, http.StatusUnprocessableEntity,
			func(t *testing.T, res *http.Response, page *goquery.Document) {
				if got := pathOf(t, res.Header.Get("HX-Push-Url")); got != doc42+"/edit/items" {
					t.Errorf("HX-Push-Url = %q, want %s/edit/items", got, doc42)
				}
				if got := norm(page.Find("h1#step-title").Text()); got != "Items" {
					t.Errorf("heading = %q, want the Items step", got)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			e.d.Engine = stateEngine{e.docs}
			e.remount()
			e.seed(doctype.StateDraft)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, c.result, c.err)

			check := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
			submit := buttonsNamed(check.Selection, "Submit")
			if submit.Length() != 1 {
				t.Fatalf("check page has %d Submit buttons, want 1", submit.Length())
			}
			form := formOf(check, submit)
			if action, _ := form.Attr("hx-post"); action != doc42+"/submit" {
				t.Fatalf("Submit's form hx-post = %q, want %s/submit", action, doc42)
			}
			target, _ := form.Attr("hx-target")
			if target != "#main" {
				t.Errorf("Submit's form hx-target = %q, want #main: a submit leaves the wizard", target)
			}

			rec := e.post(t, doc42+"/submit", nil, "HX-Request", "true")
			if rec.Code != c.status {
				t.Fatalf("status = %d, want %d; body %.300s", rec.Code, c.status, rec.Body)
			}
			if ids := rootIDs(t, rec); !slices.Equal(ids, []string{strings.TrimPrefix(target, "#")}) {
				t.Errorf("response roots = %q, want only the form's hx-target %q", ids, target)
			}
			c.check(t, rec.Result(), uitest.Doc(t, rec))
		})
	}
}

// Finding 3: the bar's buttons sit outside the step form (form=
// "wizard-form"), so the form's hx-disabled-elt must reach them too, or a
// second tap queues a second save.
func TestWizardForm_DisablesBarButtons(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	for _, c := range []struct {
		step string
		want []string // labels of the bar buttons posting the step form
	}{
		{"items", []string{"Back", "Save and continue", "Save and finish later"}},
		{"check", []string{"Submit"}},
	} {
		t.Run(c.step, func(t *testing.T) {
			page := uitest.Doc(t, e.get(t, doc42+"/edit/"+c.step))
			form := page.Find("form#" + wizardFormID)
			if form.Length() != 1 {
				t.Fatalf("found %d form#%s, want 1", form.Length(), wizardFormID)
			}
			bar := page.Find(`button[form="` + wizardFormID + `"]`)
			labels := bar.Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
			for _, l := range c.want {
				if !slices.Contains(labels, l) {
					t.Errorf("bar buttons posting the step form = %q, want %q among them", labels, l)
				}
			}
			sel, ok := form.Attr("hx-disabled-elt")
			if !ok {
				t.Fatal("the step form has no hx-disabled-elt")
			}
			disabled := htmxSelect(t, page, form, sel)
			bar.Each(func(_ int, b *goquery.Selection) {
				if disabled.IndexOfSelection(b) < 0 {
					t.Errorf("hx-disabled-elt %q does not disable %q (form=%s)", sel, norm(b.Text()), wizardFormID)
				}
			})
		})
	}
}

// Finding 4: Back saves without UI validation, but when DOC refuses the
// save with a field error the user stays on the step, with the typed
// values and the error summary: the previous step is never shown as if
// saved.
func TestWizardForm_BackOn422StaysOnStep(t *testing.T) {
	const (
		ship = "/doc/" + shipType + "/60"
		msg  = "Deliver to must be 100 characters or fewer."
	)
	for _, hx := range []bool{true, false} {
		e := newFormEnv(t, allRights...)
		docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1", "deliver_to": "Gate 1"}})
		invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{{Field: "deliver_to", Message: msg}}}
		e.docs.SetResult(shipType, 60, doctype.ActionSave, doc.Result{}, invalid)
		var hdr []string
		if hx {
			hdr = []string{"HX-Request", "true"}
		}
		rec := e.post(t, ship+"/edit/delivery", url.Values{"nav": {"back"}, "deliver_on": {"25-10-2026"}, "deliver_to": {"Gate 2"}}, hdr...)

		calls := e.docs.Calls()
		if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].Input.Version != 3 {
			t.Fatalf("htmx=%v: engine calls = %+v, want one save at version 3", hx, calls)
		}
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("htmx=%v: status = %d, want 422; body %.300s", hx, rec.Code, rec.Body)
		}
		if h := rec.Header().Get("HX-Push-Url"); h != "" {
			t.Errorf("htmx=%v: a refused Back pushes %q", hx, h)
		}
		if hx {
			if ids := rootIDs(t, rec); !slices.Equal(ids, []string{"wizard"}) {
				t.Errorf("htmx back 422: roots = %q, want only #wizard", ids)
			}
		} else if !strings.Contains(rec.Body.String(), "<html") {
			t.Error("plain back 422 is not a full page")
		}
		page := uitest.Doc(t, rec)
		if got := norm(page.Find("h1#step-title").Text()); got != "Delivery" {
			t.Errorf("htmx=%v: heading = %q, want Delivery (the step stays)", hx, got)
		}
		if got := stepCount(page.Selection); got != "Step 2 of 3" {
			t.Errorf("htmx=%v: stepper = %q, want Step 2 of 3", hx, got)
		}
		link := page.Find(`.error-summary a[href="#f-deliver_to"]`)
		if link.Length() != 1 || norm(link.Text()) != msg {
			t.Errorf("htmx=%v: error summary link to #f-deliver_to = %q, want %q", hx, norm(link.Text()), msg)
		}
		if v, _ := page.Find(`#wizard input[name="deliver_to"]`).Attr("value"); v != "Gate 2" {
			t.Errorf("htmx=%v: deliver_to = %q, want the typed Gate 2", hx, v)
		}
		if v, _ := page.Find(`#wizard input[name="deliver_on"]`).Attr("value"); v != "25-10-2026" {
			t.Errorf("htmx=%v: deliver_on = %q, want the typed 25-10-2026", hx, v)
		}
	}
}

// Finding 5: Enter in a field submits the form's default button, the first
// submit button in tree order that belongs to the form. On every step with
// Back that must be "Save and continue", not Back.
func TestWizardForm_EnterSubmitsPrimary(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1"}})
	for _, u := range []string{doc42 + "/edit/items", "/doc/" + shipType + "/60/edit/delivery"} {
		page := uitest.Doc(t, e.get(t, u))
		subs := wizardSubmits(page)
		if subs.Length() == 0 {
			t.Errorf("%s: no submit button belongs to #%s", u, wizardFormID)
			continue
		}
		labels := subs.Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
		if !slices.Contains(labels, "Back") {
			t.Errorf("%s: buttons of #%s = %q, want Back among them", u, wizardFormID, labels)
		}
		first := subs.First()
		if got := norm(first.Text()); got != "Save and continue" {
			t.Errorf("%s: first submit of #%s in tree order = %q (all %q), want Save and continue", u, wizardFormID, got, labels)
		}
		if n, _ := first.Attr("name"); n == "nav" {
			t.Errorf("%s: the default button posts nav=%s", u, first.AttrOr("value", ""))
		}
	}
}

// Finding 6 (CD-94): a header-step save sends the version the page posted,
// never the one loaded from the record.
func TestWizardForm_HeaderSaveUsesPostedVersion(t *testing.T) {
	e := newFormEnv(t, allRights...)
	rec := sampleRecord()
	rec.Version = 6
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)

	res := e.post(t, doc42+"/edit/details", url.Values{"version": {"5"}, "supplier_id": {"7"}, "order_date": {"24-10-2026"}}, "HX-Request", "true")
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Action != doctype.ActionSave || calls[0].Input.Version != 5 {
		t.Errorf("engine calls = %+v, want one save at the posted version 5 (record at 6)", calls)
	}
}

// Finding 6 (CD-94): submit from the check page sends the posted version.
func TestSubmit_UsesPostedVersion(t *testing.T) {
	e := newFormEnv(t, allRights...)
	rec := sampleRecord()
	rec.Version = 6
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 7}, nil)

	res := e.post(t, doc42+"/submit", url.Values{"version": {"5"}})
	if res.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303; body %.300s", res.Code, res.Body)
	}
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Action != doctype.ActionSubmit || calls[0].Input.Version != 5 {
		t.Errorf("engine calls = %+v, want one submit at the posted version 5 (record at 6)", calls)
	}
}

// Finding 7: DOC's save replaces the lines (syncLines deletes missing
// ones), so a header-step save resends every loaded line with its id.
func TestWizardForm_HeaderSaveKeepsLines(t *testing.T) {
	for _, nav := range []string{"", "back", "finish"} {
		t.Run("nav="+nav, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			rec := sampleRecord()
			rec.Lines = append(rec.Lines, map[string]string{"id": "9", "item_id": "3", "qty": "5.000", "uom": "kg", "rate": "70.0000"})
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}, nil)

			form := url.Values{"supplier_id": {"7"}, "order_date": {"24-10-2026"}}
			if nav != "" {
				form.Set("nav", nav)
			}
			e.post(t, doc42+"/edit/details", form, "HX-Request", "true")
			calls := e.docs.Calls()
			if len(calls) != 1 || calls[0].Action != doctype.ActionSave {
				t.Fatalf("engine calls = %+v, want one save", calls)
			}
			got := calls[0].Input.Lines
			if !slices.EqualFunc(got, rec.Lines, maps.Equal) {
				t.Errorf("saved Lines = %q, want the loaded lines with their ids %q", got, rec.Lines)
			}
		})
	}
}
