package docscreen_test

import (
	"context"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// A POST under More actions saves with the version the page rendered, like
// the bar's hidden forms (CD-94).
func TestActions_MoreActionsPostsCarryVersion(t *testing.T) {
	const blocked = "You can't approve your own Sample Order. Ask another Purchase Manager to approve it."
	const version = 5
	for _, c := range []struct {
		name  string
		block error
	}{
		{"approver who also holds submit", nil},
		{"self-approval-blocked submitter", &approval.UserError{Kind: approval.ErrSelfApproval, Msg: blocked}},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			rec := sampleRecord()
			rec.Version = version
			docscreentest.Seed(e.docs, sampleInfo(42, doctype.StatePending), rec)
			e.apr.block = c.block

			res := e.get(t, doc42)
			if res.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
			}
			page := uitest.Doc(t, res)
			if c.block != nil {
				if got := norm(page.Find(".actionbar-note").Text()); got != blocked {
					t.Fatalf("note = %q, want %q", got, blocked)
				}
			}
			form := page.Find(`.more-actions form[action="` + doc42 + `/withdraw"]`)
			if form.Length() != 1 {
				t.Fatalf("found %d Withdraw forms under More actions, want 1", form.Length())
			}
			if m, _ := form.Attr("method"); !strings.EqualFold(m, "post") {
				t.Errorf("Withdraw form method = %q, want post", m)
			}
			v := form.Find(`input[type="hidden"][name="version"]`)
			if v.Length() != 1 {
				t.Fatalf("Withdraw form has %d version inputs, want 1", v.Length())
			}
			if got, _ := v.Attr("value"); got != strconv.Itoa(version) {
				t.Errorf("Withdraw form version = %q, want the rendered record's %d", got, version)
			}
		})
	}
}

// badFormText is CD-146's wording for a POST without a usable version
// (CD-148 applies it beyond AUTH).
const badFormText = "The form could not be read. Reload the page and try again."

// A direct action, a reason page POST or the delete confirm POST without a
// usable version is a bad request with CD-146's wording: it never reaches
// DOC with version 0 (CD-94).
func TestDocAction_MissingOrBadVersionIs400(t *testing.T) {
	cases := []struct {
		action string
		state  doctype.State
		reason string
		result doc.Result
	}{
		{"withdraw", doctype.StatePending, "", doc.Result{ID: 42, State: doctype.StateDraft, Version: 4}},
		{"submit", doctype.StatePending, "", doc.Result{ID: 42, State: doctype.StatePending, Version: 4}},
		{"approve", doctype.StatePending, "", doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}},
		{"cancel", doctype.StateApproved, "Supplier closed", doc.Result{ID: 42, State: doctype.StateCancelled, Version: 4}},
		{"delete", doctype.StateDraft, "", doc.Result{ID: 42}},
	}
	for _, a := range cases {
		for _, c := range []struct {
			name    string
			version []string
		}{
			{"missing", nil},
			{"abc", []string{"abc"}},
		} {
			t.Run(a.action+"/"+c.name, func(t *testing.T) {
				e := newDocEnv(t, allRights...)
				e.seed(a.state)
				e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(a.action), a.result, nil)
				form := url.Values{"csrf_token": {csrfToken}}
				if c.version != nil {
					form["version"] = c.version
				}
				if a.reason != "" {
					form.Set("reason", a.reason)
				}
				rec := e.do(t, http.MethodPost, doc42+"/"+a.action, form)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", rec.Code)
				}
				if calls := e.docs.Calls(); len(calls) != 0 {
					t.Errorf("engine called: %+v", calls)
				}
				page, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
				if err != nil {
					t.Fatal(err)
				}
				if got := norm(page.Text()); !strings.Contains(got, badFormText) {
					t.Errorf("body text = %.300q, want it to contain %q", got, badFormText)
				}
			})
		}
	}
}

// After an approve the "Next request" link sits under the success banner
// (plan Task 13): nothing shows between the banner and the link, and the
// link comes before the state badge.
func TestDocPage_NextRequestUnderBanner(t *testing.T) {
	const want = "Approved. SO/26-27/00001 has been numbered."
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StatePending)
	e.docs.SetResult(docscreentest.Sample, 42, doctype.ActionApprove,
		doc.Result{ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4}, nil)
	rec := e.post(t, doc42+"/approve", nil)
	if got := location(t, rec); got != doc42 {
		t.Fatalf("303 to %q, want %q", got, doc42)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "next=1") {
		t.Fatalf("Location %q lacks next=1", loc)
	}
	e.docs.SetInfo(sampleInfo(42, doctype.StateApproved))

	res := e.get(t, loc)
	if res.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, body %.300s", loc, res.Code, res.Body)
	}
	main := uitest.Doc(t, res).Find("main#main")
	var banner *goquery.Selection
	main.Find(".banner--success").Each(func(_ int, b *goquery.Selection) {
		if b.ParentsFiltered("template").Length() == 0 && strings.Contains(norm(b.Text()), want) {
			banner = b
		}
	})
	if banner == nil {
		t.Fatalf("no success banner %q in main#main", want)
	}
	link := main.Find(`a[href="` + inboxURL + `"]`).FilterFunction(func(_ int, s *goquery.Selection) bool {
		return norm(s.Text()) == "Next request"
	})
	if link.Length() != 1 {
		t.Fatalf("found %d \"Next request\" links in main#main, want 1", link.Length())
	}
	badge := main.Find(".badge").First()
	if badge.Length() == 0 {
		t.Fatal("no state badge in main#main")
	}

	// main#main's text in document order: the link's text follows the
	// banner's with nothing between.
	if full, b := norm(main.Text()), norm(banner.Text()); !strings.Contains(full, b+" Next request") {
		i := strings.Index(full, b) + len(b)
		j := strings.Index(full, "Next request")
		gap := "?"
		if i <= j {
			gap = full[i:j]
		}
		t.Errorf("text between the success banner and \"Next request\" = %q, want none (the link sits straight under the banner)", strings.TrimSpace(gap))
	}
	all := main.Find("*")
	if li, gi := all.IndexOfSelection(link), all.IndexOfSelection(badge); gi < li {
		t.Errorf("the state badge (element %d) comes before \"Next request\" (element %d); the link must come first", gi, li)
	}
}

// The sample DocType has no roll-ups, so its document has no taxable total
// and the page shows no "Taxable total" row.
func TestDocView_NoTaxableTotalFromRollUps(t *testing.T) {
	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateApproved)
	v, err := e.d.View(context.Background(), priya, docscreentest.Sample, 42)
	if err != nil {
		t.Fatal(err)
	}
	// Checked by reflection so the test survives the field's removal.
	if f := reflect.ValueOf(v).FieldByName("TaxableTotal"); f.IsValid() && !f.IsZero() {
		t.Errorf("DocView.TaxableTotal = %v, want none: the sample has no roll-ups", f.Interface())
	}
	res := e.get(t, doc42)
	if res.Code != http.StatusOK {
		t.Fatalf("status = %d, body %.300s", res.Code, res.Body)
	}
	if got := norm(uitest.Doc(t, res).Find("main#main").Text()); strings.Contains(got, "Taxable total") {
		t.Errorf("the document page shows a \"Taxable total\" row; the sample has no roll-ups")
	}
}

// From 80rem the side panel holds state, approval history and attachments
// (design-system §8.4). It sits inside main#main so the htmx fragment
// carries it too.
func TestDocPage_SidePanelInsideMain(t *testing.T) {
	for _, s := range []doctype.State{doctype.StatePending, doctype.StateApproved} {
		t.Run(string(s), func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(s)
			e.extras.SetApproval(docscreentest.Sample, 42, docscreen.ApprovalView{
				Needed: true, WaitingOn: "Ravi Kulkarni (Purchase Manager)",
				History: []docscreen.HistoryRow{{At: "24-10-2026 10:30", By: "Priya Shah", Action: "Submitted"}},
			})
			e.extras.SetAttachments(docscreentest.Sample, 42, []components.Attachment{
				{ID: 1, Name: "quote.jpg", Kind: "image", URL: "/files/1", UploadedBy: "Priya Shah", UploadedAt: "24-10-2026 10:00"},
			})

			for _, hx := range []bool{false, true} {
				var hdr []string
				if hx {
					hdr = []string{"HX-Request", "true"}
				}
				rec := e.get(t, doc42, hdr...)
				if rec.Code != http.StatusOK {
					t.Fatalf("htmx=%v: status = %d, body %.300s", hx, rec.Code, rec.Body)
				}
				main := uitest.Doc(t, rec).Find("main#main")
				if main.Length() != 1 {
					t.Fatalf("htmx=%v: found %d main#main", hx, main.Length())
				}
				side := main.Find(".doc-side")
				if side.Length() != 1 {
					t.Fatalf("htmx=%v: found %d .doc-side inside main#main, want 1", hx, side.Length())
				}
				if side.Find(".badge").Length() == 0 {
					t.Errorf("htmx=%v: .doc-side has no state badge", hx)
				}
				summaries := side.Find("details > summary").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
				if !slices.Contains(summaries, "Approval history") {
					t.Errorf("htmx=%v: .doc-side disclosures = %q, want \"Approval history\"", hx, summaries)
				}
				if side.Find(".attachments").Length() == 0 || side.Find(`img[alt="quote.jpg"]`).Length() == 0 {
					t.Errorf("htmx=%v: .doc-side does not hold the attachments", hx)
				}

				layout := main.Find(".doc-layout")
				if layout.Length() != 1 {
					t.Fatalf("htmx=%v: found %d .doc-layout inside main#main, want 1", hx, layout.Length())
				}
				if layout.ChildrenFiltered(".doc-side").Length() != 1 {
					t.Errorf("htmx=%v: .doc-side is not a child of .doc-layout", hx)
				}
				col := layout.Children().Not(".doc-side")
				if col.Length() == 0 {
					t.Fatalf("htmx=%v: .doc-layout has no main column beside .doc-side", hx)
				}
				if col.Find("dl.summary").Length() == 0 {
					t.Errorf("htmx=%v: the main column beside .doc-side lacks the header facts", hx)
				}
				if side.Find("dl.summary").Length() != 0 {
					t.Errorf("htmx=%v: the header facts are in the side panel", hx)
				}
			}
		})
	}
}

// A 422 whose field errors name other fields than the reason still lists
// them, so the user sees what is wrong.
func TestReasons_422FieldErrorsNotOnReasonShowSummary(t *testing.T) {
	const fieldMsg = "Amount must be more than zero."
	for _, p := range reasonPages {
		t.Run(p.action, func(t *testing.T) {
			e := newDocEnv(t, allRights...)
			e.seed(p.state)
			invalid := statusErr{status: http.StatusUnprocessableEntity, msg: "Check the document.", fields: []doctype.FieldError{{Field: "amount", Message: fieldMsg}}}
			e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(p.action), doc.Result{}, invalid)
			rec := e.post(t, doc42+"/"+p.action, url.Values{"reason": {"Wrong GSTIN"}})
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
			}
			if calls := e.docs.Calls(); len(calls) != 1 {
				t.Fatalf("engine calls = %+v, want 1", calls)
			}
			summary := uitest.Doc(t, rec).Find("#main .error-summary")
			if summary.Length() != 1 {
				t.Fatalf("found %d error summaries, want 1", summary.Length())
			}
			items := summary.Find("li").Map(func(_ int, s *goquery.Selection) string { return norm(s.Text()) })
			if !slices.Contains(items, fieldMsg) {
				t.Errorf("error summary items = %q, want one %q", items, fieldMsg)
			}
		})
	}
}
