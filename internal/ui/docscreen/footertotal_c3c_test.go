package docscreen_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// footerTotal is the Items footer's total as the page shows it, and how many
// #footer-total elements the page has.
func footerTotal(page *goquery.Document) (string, int) {
	s := page.Find("#footer-total")
	return norm(s.Text()), s.Length()
}

// itemsWith is draft 42's Items step with m as Sample's module.
func itemsWith(t *testing.T, rec doc.Record, m docscreen.Module) (*docEnv, *goquery.Document) {
	t.Helper()
	e := newLinesEnv(t, rec)
	e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: m}
	e.remount()
	res := e.get(t, doc42+"/edit/items")
	if res.Code != http.StatusOK {
		t.Fatalf("GET items: status %d, body %.300s", res.Code, res.Body)
	}
	return e, uitest.Doc(t, res)
}

// UI v2 A-22 (CD-376 (2)): Module.FooterTotal names the money field the Items
// footer shows as "{n} items · {label} ₹…", n the saved lines, inside the
// sticky footer's summary row. A field the module hides shows only the count;
// a module without FooterTotal shows no total.
func TestItemsFooter_Total(t *testing.T) {
	hideAmount := func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "amount" }
	one := threeLines()
	one.Lines = one.Lines[:1]
	blank := threeLines()
	delete(blank.Values, "amount")
	big := threeLines()
	big.Values["amount"] = "123456.00"
	for _, c := range []struct {
		name string
		rec  doc.Record
		m    docscreen.Module
		want string
	}{
		{"three lines", threeLines(), docscreen.Module{FooterTotal: "amount"}, "3 items · Amount ₹94,164.00"},
		{"lakh grouping", big, docscreen.Module{FooterTotal: "amount"}, "3 items · Amount ₹1,23,456.00"},
		{"one line", one, docscreen.Module{FooterTotal: "amount"}, "1 item · Amount ₹94,164.00"},
		{"hidden field", threeLines(), docscreen.Module{FooterTotal: "amount", Hide: hideAmount}, "3 items"},
		{"blank total", blank, docscreen.Module{FooterTotal: "amount"}, "3 items"}, // CD-378 (4)
	} {
		t.Run(c.name, func(t *testing.T) {
			_, page := itemsWith(t, c.rec, c.m)
			got, n := footerTotal(page)
			if n != 1 || got != c.want {
				t.Errorf("%d #footer-total %q, want one %q", n, got, c.want)
			}
			if page.Find("#wizard .actionbar .actionbar-summary #footer-total").Length() != 1 {
				t.Error("#footer-total isn't in the footer's summary row")
			}
			// The label is the part hidden on narrow phones (CD-378 (2)); no label without a value.
			label := page.Find("#footer-total .actionbar-total-label")
			if want := map[bool]string{true: "Amount", false: ""}[strings.Contains(c.want, "₹")]; norm(label.Text()) != want || (want == "") != (label.Length() == 0) {
				t.Errorf("label = %d %q, want %q", label.Length(), label.Text(), want)
			}
		})
	}
	t.Run("no FooterTotal", func(t *testing.T) {
		_, page := itemsWith(t, threeLines(), docscreen.Module{})
		if _, n := footerTotal(page); n != 0 || page.Find(".actionbar-summary").Length() != 0 {
			t.Errorf("%d #footer-total, %d summary rows, want none", n, page.Find(".actionbar-summary").Length())
		}
	})
	// Only the Items step shows it.
	t.Run("other steps", func(t *testing.T) {
		e, _ := itemsWith(t, threeLines(), docscreen.Module{FooterTotal: "amount"})
		page := uitest.Doc(t, e.get(t, doc42+"/edit/details"))
		if _, n := footerTotal(page); n != 0 {
			t.Errorf("details step: %d #footer-total, want none", n)
		}
	})
}

// A grid row save answers with an out-of-band #footer-total carrying the
// total after the save, and nothing else of the footer (A-22).
func TestItemsFooter_GridSaveSwapsTheTotal(t *testing.T) {
	e, page := itemsWith(t, threeLines(), docscreen.Module{FooterTotal: "amount"})
	if got, _ := footerTotal(page); got != "3 items · Amount ₹94,164.00" {
		t.Fatalf("before the save: %q", got)
	}
	after := threeLines()
	after.Version = 7
	after.Values["amount"] = "123456.00" // the engine's new total
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), after)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSave, doc.Result{ID: 42, State: doctype.StateDraft, Version: 7}, nil)
	rec := gridPost(t, e, page, 2, "6", map[string]string{"grid-2-qty": "25"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %.300s", rec.Code, rec.Body)
	}
	resp := uitest.Doc(t, rec)
	oob := resp.Find("#footer-total")
	if oob.Length() != 1 || oob.AttrOr("hx-swap-oob", "") != "true" || norm(oob.Text()) != "3 items · Amount ₹1,23,456.00" {
		t.Errorf("out-of-band #footer-total = %d %q (hx-swap-oob %q), want one swap of %q", oob.Length(), norm(oob.Text()), oob.AttrOr("hx-swap-oob", ""), "3 items · Amount ₹1,23,456.00")
	}
	if n := resp.Find(".actionbar, .actionbar-summary, input:not([type=hidden])").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.Closest("form.linegrid-form").Length() == 0
	}).Length(); n != 0 {
		t.Errorf("the response redraws %d footer or input elements outside the row, want only #footer-total", n)
	}
}

// A FooterTotal that isn't a header Money field of its DocType panics when
// the routes are built (lesson: every declared name checked at registration).
func TestRoutes_FooterTotalMustBeAMoneyField(t *testing.T) {
	docscreentest.RegisterSample()
	docscreentest.RegisterHooked()
	for _, c := range []struct {
		name  string
		mods  map[string]docscreen.Module
		panic bool
	}{
		{"unknown name", map[string]docscreen.Module{docscreentest.Sample: {FooterTotal: "nosuch"}}, true},
		{"not money", map[string]docscreen.Module{docscreentest.Sample: {FooterTotal: "remarks"}}, true},
		{"line money field", map[string]docscreen.Module{fixture.Claim: {FooterTotal: "amount"}}, true},
		{"header money field", map[string]docscreen.Module{fixture.Claim: {FooterTotal: "total"}}, false},
		{"unregistered DocType", map[string]docscreen.Module{"c3c.Nowhere": {FooterTotal: "amount"}}, true},
		{"money field", map[string]docscreen.Module{docscreentest.Sample: {FooterTotal: "amount"}, docscreentest.Hooked: {}}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if got := recover() != nil; got != c.panic {
					t.Errorf("Routes panicked = %v, want %v", got, c.panic)
				}
			}()
			docscreen.Routes(docscreen.Deps{Modules: c.mods})
		})
	}
}
