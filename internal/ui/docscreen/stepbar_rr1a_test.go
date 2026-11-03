package docscreen_test

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// rr1aStep is the stepper item labelled label: whether it is done and
// whether it needs attention.
func rr1aStep(t *testing.T, d *goquery.Document, label string) (done, attention bool) {
	t.Helper()
	li := d.Find("nav.stepper li").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Find(".stepper__label").Contents().Not("span.visually-hidden").Text()) == label
	})
	if li.Length() != 1 {
		t.Fatalf("%d stepper items labelled %q, want 1", li.Length(), label)
	}
	return li.HasClass("is-done"), li.HasClass("is-attention")
}

// RR1a 3 (CD-387 (2)): a draft saved with a required field blank shows
// that field's step as needing attention in the step bar, not Completed,
// on the later steps and on Check, as Check's section does; with every
// required value it is done. On Check a module problem marks its step too,
// the same as the section.
func TestRR1a_StepBarAttention(t *testing.T) {
	for _, c := range []struct {
		name      string
		blank     string // the header value left out
		attention bool
	}{
		{"complete", "", false},
		{"order date blank", "order_date", true},
		{"supplier blank", "supplier_id", true},
		{"remarks blank, not required", "remarks", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newFormEnv(t, allRights...)
			rec := sampleRecord()
			if c.blank != "" {
				rec.Values[c.blank] = ""
			}
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
			for _, step := range []string{"items", "check"} {
				d := uitest.Doc(t, e.get(t, doc42+"/edit/"+step))
				done, attention := rr1aStep(t, d, "Details")
				if done == c.attention || attention != c.attention {
					t.Errorf("on %s: Details done %v, attention %v; want attention %v", step, done, attention, c.attention)
				}
				if step == "check" {
					want := "Complete"
					if c.attention {
						want = "Needs attention"
					}
					if got := norm(checkSection(d.Selection, "Details").Find(".check-status").Text()); got != want {
						t.Errorf("Check's Details = %q, want %q", got, want)
					}
				}
			}
		})
	}
	t.Run("a module problem on Check", func(t *testing.T) {
		e := newFormEnv(t, allRights...)
		e.seed(doctype.StateDraft)
		e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {Check: func(docscreen.Ctx) (docscreen.CheckExtra, error) {
			return docscreen.CheckExtra{Problems: []doctype.FieldError{{Field: "qty", Line: 1, Message: "Line 1: Tax code not found for this item."}}}, nil
		}}}
		e.remount()
		d := uitest.Doc(t, e.get(t, doc42+"/edit/check"))
		if done, attention := rr1aStep(t, d, "Items"); done || !attention {
			t.Errorf("Items done %v, attention %v; want attention", done, attention)
		}
		if done, attention := rr1aStep(t, d, "Details"); !done || attention {
			t.Errorf("Details done %v, attention %v; want done", done, attention)
		}
	})
}

// RR1a 3: a blank required field the module hides is no problem in the
// step bar either (CD-256 (1)): Details stays done.
func TestRR1a_StepBarHiddenField(t *testing.T) {
	e := newFormEnv(t, allRights...)
	rec := sampleRecord()
	rec.Values["order_date"] = ""
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
	e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {Hide: m01bHide("order_date")}}
	e.remount()
	for _, step := range []string{"items", "check"} {
		d := uitest.Doc(t, e.get(t, doc42+"/edit/"+step))
		if done, attention := rr1aStep(t, d, "Details"); !done || attention {
			t.Errorf("on %s: Details done %v, attention %v; want done", step, done, attention)
		}
	}
}

// RR1a 2 (R7b, CD-395): a submit refused on lines of a cards module links
// each line error to its card, #line-{n}, when the Items step draws it; a
// line past the first 20 (no card without Show all) links as Check does,
// to the Items step with every line shown (?all=1) at its card (RR1a fix
// 3); the lines as a whole, and a line past the last, link to the heading.
func TestRR1a_RefusalLinksLineCards(t *testing.T) {
	e := newFormEnv(t, allRights...)
	rec := sampleRecord()
	for n := 2; n <= 22; n++ {
		rec.Lines = append(rec.Lines, map[string]string{"id": itoa(int64(n)), "item_id": "3", "qty": "5.000", "rate": "62.5000"})
	}
	docscreentest.Seed(e.docs, sampleInfo(42, doctype.StateDraft), rec)
	e.d.Modules = map[string]docscreen.Module{docscreentest.Sample: {CardsOnly: true}}
	e.remount()
	invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Fix the highlighted fields and save again.", fields: []doctype.FieldError{
		{Field: "lines", Message: "Add the freight line."},
		{Field: "qty", Line: 1, Message: "Line 1: Quantity must be more than zero."},
		{Field: "qty", Line: 20, Message: "Line 20: Quantity must be more than zero."},
		{Field: "qty", Line: 21, Message: "Line 21: Quantity must be more than zero."},
		{Field: "qty", Line: 22, Message: "Line 22: Quantity must be more than zero."},
		{Field: "qty", Line: 23, Message: "Line 23: Quantity must be more than zero."},
	}}
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionSubmit, doc.Result{}, invalid)
	for _, c := range []struct {
		what string
		hdr  []string
		want []string
	}{
		{"from Check", nil, []string{"#step-title", "#line-1", "#line-20", doc42 + "/edit/items?all=1#line-reveal", doc42 + "/edit/items?all=1#line-22", "#step-title"}},
		// With every line shown (CD-395) the 21st card is #line-reveal.
		{"every line shown", []string{"HX-Request", "true", "HX-Current-URL", "http://x" + doc42 + "/edit/items?all=1"}, []string{"#step-title", "#line-1", "#line-20", "#line-reveal", "#line-22", "#step-title"}},
	} {
		page := uitest.Doc(t, e.post(t, doc42+"/submit", nil, c.hdr...))
		var hrefs []string
		page.Find(".error-summary a").Each(func(_ int, s *goquery.Selection) { hrefs = append(hrefs, s.AttrOr("href", "")) })
		if !slices.Equal(hrefs, c.want) {
			t.Errorf("%s: summary links %q, want %q", c.what, hrefs, c.want)
		}
		for _, h := range c.want[1:] {
			if strings.HasPrefix(h, "#line-") && page.Find(h).Length() != 1 {
				t.Errorf("%s: no %s on the page", c.what, h)
			}
		}
	}
}
