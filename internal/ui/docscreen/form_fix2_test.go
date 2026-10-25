package docscreen_test

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// WP1-UI Task 14 review fixes, attempt 2 (CD-178).

// CD-178: Back saves without UI validation, but a value NUM can't parse is
// never saved: the user stays on the step with the typed text and the error
// summary, as on Save and continue, and DOC is not called.
func TestWizardForm_BackOnParseErrorStaysOnStep(t *testing.T) {
	_, dateErr := types.ParseDate("x")
	if dateErr == nil {
		t.Fatal("ParseDate(x) parsed")
	}
	_, moneyErr := types.ParseMoney("x")
	if moneyErr == nil {
		t.Fatal("ParseMoney(x) parsed")
	}
	cases := []struct {
		name  string
		seed  func(e *docEnv)
		url   string
		form  url.Values
		field string // the field holding the bad "x"
		msg   string // NUM's message for it
		head  string
		count string
		kept  map[string]string // other typed inputs kept as posted
	}{
		{
			name:  "date order_date",
			seed:  func(e *docEnv) { e.seed(doctype.StateDraft) },
			url:   doc42 + "/edit/details",
			form:  url.Values{"supplier_id": {"7"}, "order_date": {"x"}},
			field: "order_date", msg: dateErr.Error(), head: "Details", count: "Step 1 of 3",
			kept: map[string]string{"supplier_id": "7"},
		},
		{
			name: "date deliver_on",
			seed: func(e *docEnv) {
				docscreentest.Seed(e.docs, draftInfo(shipType, "Shipment", 60), doc.Record{Version: 3, Values: map[string]string{"ref": "SHP-1", "deliver_to": "Gate 1"}})
			},
			url:   "/doc/" + shipType + "/60/edit/delivery",
			form:  url.Values{"deliver_on": {"x"}, "deliver_to": {"Gate 2"}},
			field: "deliver_on", msg: dateErr.Error(), head: "Delivery", count: "Step 2 of 3",
			kept: map[string]string{"deliver_to": "Gate 2"},
		},
		{
			name: "money total",
			seed: func(e *docEnv) {
				docscreentest.Seed(e.docs, draftInfo(claimType, "Expense Claim", 50), doc.Record{Version: 3, Values: map[string]string{"claim_date": "2026-10-24"}})
			},
			url:   "/doc/" + claimType + "/50/edit/details",
			form:  url.Values{"claim_date": {"24-10-2026"}, "total": {"x"}},
			field: "total", msg: moneyErr.Error(), head: "Details", count: "Step 1 of 3",
			kept: map[string]string{"claim_date": "24-10-2026"},
		},
	}
	for _, c := range cases {
		for _, hx := range []bool{true, false} {
			t.Run(c.name+map[bool]string{true: "/htmx", false: "/plain"}[hx], func(t *testing.T) {
				e := newFormEnv(t, allRights...)
				c.seed(e)
				var hdr []string
				if hx {
					hdr = []string{"HX-Request", "true"}
				}
				form := url.Values{"nav": {"back"}}
				for k, v := range c.form {
					form[k] = slices.Clone(v)
				}
				rec := e.post(t, c.url, form, hdr...)

				if calls := e.docs.Calls(); len(calls) != 0 {
					t.Errorf("engine called on a parse error: %+v", calls)
				}
				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
				}
				if h := rec.Header().Get("HX-Push-Url"); h != "" {
					t.Errorf("a refused Back pushes %q", h)
				}
				if hx {
					if ids := rootIDs(t, rec); !slices.Equal(ids, []string{"wizard"}) {
						t.Errorf("htmx back 422: roots = %q, want only #wizard", ids)
					}
				} else if !strings.Contains(rec.Body.String(), "<html") {
					t.Error("plain back 422 is not a full page")
				}
				page := uitest.Doc(t, rec)
				if got := norm(page.Find("h1#step-title").Text()); got != c.head {
					t.Errorf("heading = %q, want %s (the step stays)", got, c.head)
				}
				if got := stepCount(page.Selection); got != c.count {
					t.Errorf("stepper = %q, want %s", got, c.count)
				}
				link := page.Find(`.error-summary a[href="#f-` + c.field + `"]`)
				if link.Length() != 1 || norm(link.Text()) != c.msg {
					t.Errorf("error summary link to #f-%s = %q (%d links), want NUM's %q", c.field, norm(link.Text()), link.Length(), c.msg)
				}
				if v, ok := page.Find(`#wizard input[name="` + c.field + `"]`).Attr("value"); !ok || v != "x" {
					t.Errorf("%s = %q, want the typed x kept", c.field, v)
				}
				for name, want := range c.kept {
					if v, _ := page.Find(`#wizard input[name="` + name + `"]`).Attr("value"); v != want {
						t.Errorf("%s = %q, want the typed %q", name, v, want)
					}
				}
			})
		}
	}
}

// CD-178: the check step lists a missing Required header field under its
// step, with DOC's text, and marks that step Needs attention.
func TestCheckPage_MissingRequiredNeedsAttention(t *testing.T) {
	const msg = "Supplier is required."
	t.Run("empty", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		rec := sampleRecord()
		rec.Values["supplier_id"] = ""
		docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)

		res := e.get(t, doc42+"/edit/check")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
		}
		page := uitest.Doc(t, res)
		sec := checkSection(page.Selection, "Details")
		if sec.Length() != 1 {
			t.Fatalf("found %d Details sections, want 1", sec.Length())
		}
		if got := norm(sec.Find(".check-status").Text()); got != "Needs attention" {
			t.Errorf("Details status = %q, want Needs attention", got)
		}
		if text := norm(sec.Text()); !strings.Contains(text, msg) {
			t.Errorf("Details section = %q, want it to list %q", text, msg)
		}
		items := checkSection(page.Selection, "Items")
		if strings.Contains(norm(items.Text()), msg) {
			t.Errorf("Items section lists %q; it belongs to Details", msg)
		}
		if got := norm(items.Find(".check-status").Text()); got != "Complete" {
			t.Errorf("Items status = %q, want Complete", got)
		}
	})
	t.Run("filled", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		page := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
		sec := checkSection(page.Selection, "Details")
		if got := norm(sec.Find(".check-status").Text()); got != "Complete" {
			t.Errorf("Details status = %q, want Complete", got)
		}
		if text := norm(sec.Text()); strings.Contains(text, "is required.") {
			t.Errorf("complete Details section = %q, lists a required message", text)
		}
	})
}

// CD-178 with spec R8: a draft with no lines shows the Items step as Needs
// attention with DOC's "Add at least one line."
func TestCheckPage_NoLinesItemsNeedsAttention(t *testing.T) {
	const msg = "Add at least one line."
	t.Run("no lines", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		rec := sampleRecord()
		rec.Lines = nil
		docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)

		res := e.get(t, doc42+"/edit/check")
		if res.Code != http.StatusOK {
			t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
		}
		page := uitest.Doc(t, res)
		sec := checkSection(page.Selection, "Items")
		if sec.Length() != 1 {
			t.Fatalf("found %d Items sections, want 1", sec.Length())
		}
		if got := norm(sec.Find(".check-status").Text()); got != "Needs attention" {
			t.Errorf("Items status = %q, want Needs attention", got)
		}
		if text := norm(sec.Text()); !strings.Contains(text, msg) {
			t.Errorf("Items section = %q, want it to list %q", text, msg)
		}
		if got := norm(checkSection(page.Selection, "Details").Find(".check-status").Text()); got != "Complete" {
			t.Errorf("Details status = %q, want Complete", got)
		}
	})
	t.Run("one line", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		sec := checkSection(uitest.Doc(t, e.get(t, doc42+"/edit/check")).Selection, "Items")
		if got := norm(sec.Find(".check-status").Text()); got != "Complete" {
			t.Errorf("Items status = %q, want Complete", got)
		}
		if strings.Contains(norm(sec.Text()), msg) {
			t.Errorf("Items section with one line lists %q", msg)
		}
	})
}

// Spec R8: DOC's no-lines error (Field "lines", Line 0) sends the user to
// the Items step, and its error summary link lands on an element there.
func TestSubmit_LinesErrorLinksToLineEditor(t *testing.T) {
	const msg = "Add at least one line."
	for _, hx := range []bool{true, false} {
		t.Run(map[bool]string{true: "htmx", false: "plain"}[hx], func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			rec := sampleRecord()
			rec.Lines = nil
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
			invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.",
				fields: []doctype.FieldError{{Field: "lines", Line: 0, Message: msg}}}
			e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{}, invalid)

			var hdr []string
			if hx {
				hdr = []string{"HX-Request", "true"}
			}
			res := e.post(t, doc42+"/submit", nil, hdr...)
			if res.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", res.Code, res.Body)
			}
			page := uitest.Doc(t, res)
			if got := norm(page.Find("h1#step-title").Text()); got != "Items" {
				t.Errorf("heading = %q, want Items", got)
			}
			link := page.Find(".error-summary a").FilterFunction(func(_ int, s *goquery.Selection) bool {
				return norm(s.Text()) == msg
			})
			if link.Length() != 1 {
				t.Fatalf("error summary has %d links reading %q, want 1", link.Length(), msg)
			}
			href, _ := link.Attr("href")
			id, ok := strings.CutPrefix(href, "#")
			if !ok || id == "" {
				t.Fatalf("link href = %q, want a #fragment", href)
			}
			dest := page.Find("#wizard").Find(`[id="` + id + `"]`)
			if dest.Length() != 1 {
				t.Fatalf("link href %q: %d elements in #wizard have id %q, want 1", href, dest.Length(), id)
			}
			if dest.Closest(".error-summary").Length() != 0 {
				t.Errorf("link href %q points into the error summary itself", href)
			}
		})
	}
}
