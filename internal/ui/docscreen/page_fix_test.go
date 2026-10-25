package docscreen_test

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

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
	const blocked = "You submitted this, so someone else must approve it."
	const version = 5
	for _, c := range []struct {
		name  string
		block error
	}{
		{"approver who also holds submit", nil},
		{"self-approval-blocked submitter", errors.New(blocked)},
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

// A direct action without a usable version is a bad request: it never
// reaches DOC with version 0 (CD-94).
func TestDocAction_MissingOrBadVersionIs400(t *testing.T) {
	results := map[string]doc.Result{
		"withdraw": {ID: 42, State: doctype.StateDraft, Version: 4},
		"submit":   {ID: 42, State: doctype.StatePending, Version: 4},
		"approve":  {ID: 42, State: doctype.StateApproved, Number: "SO/26-27/00001", Version: 4},
	}
	for _, action := range []string{"withdraw", "submit", "approve"} {
		for _, c := range []struct {
			name string
			form url.Values
		}{
			{"missing", url.Values{"csrf_token": {csrfToken}}},
			{"abc", url.Values{"csrf_token": {csrfToken}, "version": {"abc"}}},
		} {
			t.Run(action+"/"+c.name, func(t *testing.T) {
				e := newDocEnv(t, allRights...)
				e.seed(doctype.StatePending)
				e.docs.SetResult(docscreentest.Sample, 42, doctype.Action(action), results[action], nil)
				rec := e.do(t, http.MethodPost, doc42+"/"+action, c.form)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400", rec.Code)
				}
				if calls := e.docs.Calls(); len(calls) != 0 {
					t.Errorf("engine called: %+v", calls)
				}
			})
		}
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
