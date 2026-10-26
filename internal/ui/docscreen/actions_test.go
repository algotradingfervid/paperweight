package docscreen_test

import (
	"context"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// pageRights are the rights Rights asks about.
var pageRights = []auth.Right{"write", "submit", "approve", "cancel", "amend", "delete", "print"}

// rightsOf is a Rights-shaped map with exactly the given rights true.
func rightsOf(rs ...auth.Right) map[auth.Right]bool {
	m := map[auth.Right]bool{}
	for _, r := range pageRights {
		m[r] = slices.Contains(rs, r)
	}
	return m
}

func viewIn(s doctype.State, needed bool) docscreen.DocView {
	info := sampleInfo(42, s)
	return docscreen.DocView{
		Info:     info,
		Version:  3,
		Record:   auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy},
		Approval: docscreen.ApprovalView{Needed: needed},
	}
}

func barLabels(bar components.ActionBarProps) (primary, secondary string) {
	if bar.Primary != nil {
		primary = bar.Primary.Label
	}
	if bar.Secondary != nil {
		secondary = bar.Secondary.Label
	}
	return primary, secondary
}

func moreLabels(more []components.MoreAction) []string {
	out := []string{}
	for _, m := range more {
		out = append(out, m.Label)
	}
	return out
}

func moreItem(more []components.MoreAction, label string) (components.MoreAction, bool) {
	i := slices.IndexFunc(more, func(m components.MoreAction) bool { return m.Label == label })
	if i < 0 {
		return components.MoreAction{}, false
	}
	return more[i], true
}

func TestActions_PerStateAndRights(t *testing.T) {
	all := rightsOf(pageRights...)
	cases := []struct {
		name               string
		state              doctype.State
		needed             bool
		rights             map[auth.Right]bool
		primary, secondary string
		more               []string
	}{
		// DOC-094 with every right.
		{"draft", doctype.StateDraft, false, all, "Submit", "", []string{"Delete draft", "Print"}},
		{"draft needing approval", doctype.StateDraft, true, all, "Submit for approval", "", []string{"Delete draft", "Print"}},
		{"pending, approver", doctype.StatePending, true, all, "Approve", "Reject", []string{"Withdraw", "Print"}},
		{"pending, submitter only", doctype.StatePending, true, rightsOf("submit", "print"), "", "Withdraw", []string{"Print"}},
		{"rejected", doctype.StateRejected, true, all, "Revise", "", []string{"Print"}},
		{"approved", doctype.StateApproved, true, all, "", "", []string{"Close", "Cancel", "Print"}},
		{"closed", doctype.StateClosed, true, all, "", "", []string{"Print"}},
		{"cancelled", doctype.StateCancelled, true, all, "Amend", "", []string{"Print"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bar, more := docscreen.Actions(viewIn(c.state, c.needed), true, c.rights)
			if p, s := barLabels(bar); p != c.primary || s != c.secondary {
				t.Errorf("bar = primary %q, secondary %q; want %q, %q", p, s, c.primary, c.secondary)
			}
			if bar.Note != "" {
				t.Errorf("bar note = %q, want none", bar.Note)
			}
			if got := moreLabels(more); !slices.Equal(got, c.more) {
				t.Errorf("more actions = %q, want %q", got, c.more)
			}
		})
	}

	// The approve and reject buttons of a pending document.
	bar, more := docscreen.Actions(viewIn(doctype.StatePending, true), true, all)
	if bar.Primary == nil || bar.Primary.Href != "" || bar.Primary.Variant != components.Primary {
		t.Errorf("Approve = %+v, want a primary submit (direct POST)", bar.Primary)
	}
	if bar.Secondary == nil || bar.Secondary.Href != doc42+"/reject" || bar.Secondary.Variant != components.Secondary {
		t.Errorf("Reject = %+v, want a secondary (not danger) link to %s/reject", bar.Secondary, doc42)
	}
	if w, ok := moreItem(more, "Withdraw"); !ok || !w.Post || w.Href != doc42+"/withdraw" || w.Danger {
		t.Errorf("Withdraw = %+v, want a POST to %s/withdraw", w, doc42)
	}

	// AUTH-084: Priya and Ravi on the same pending document, rights from authfake.
	az := authfake.New()
	for _, r := range []auth.Right{"read", "submit", "write", "delete", "print"} {
		az.Grant("Buyer", docscreentest.Sample, r, auth.ScopeAll)
	}
	for _, r := range []auth.Right{"read", "approve", "cancel", "amend", "print"} {
		az.Grant("Purchase Manager", docscreentest.Sample, r, auth.ScopeAll)
	}
	az.AddRole(priya.ID, "Buyer")
	az.AddRole(ravi.ID, "Purchase Manager")
	v := viewIn(doctype.StatePending, true)
	ctx := context.Background()

	pr := docscreen.Rights(ctx, az, priya, docscreentest.Sample, &v.Record)
	if want := rightsOf("submit", "write", "delete", "print"); !reflect.DeepEqual(pr, want) {
		t.Errorf("Rights(Priya) = %v, want %v", pr, want)
	}
	bar, more = docscreen.Actions(v, true, pr)
	if p, s := barLabels(bar); p != "" || s != "Withdraw" {
		t.Errorf("Priya: bar = %q, %q; want only Withdraw", p, s)
	}
	for _, l := range moreLabels(more) {
		if l == "Approve" || l == "Reject" {
			t.Errorf("Priya: More has %q", l)
		}
	}

	rr := docscreen.Rights(ctx, az, ravi, docscreentest.Sample, &v.Record)
	if want := rightsOf("approve", "cancel", "amend", "print"); !reflect.DeepEqual(rr, want) {
		t.Errorf("Rights(Ravi) = %v, want %v", rr, want)
	}
	bar, more = docscreen.Actions(v, true, rr)
	if p, s := barLabels(bar); p != "Approve" || s != "Reject" {
		t.Errorf("Ravi: bar = %q, %q; want Approve, Reject", p, s)
	}
	labels := append(moreLabels(more), barLabels2(bar)...)
	for _, l := range labels {
		if l == "Withdraw" || strings.Contains(l, "Edit") || strings.Contains(l, "Delete") || l == "Revise" {
			t.Errorf("Ravi: has %q", l)
		}
	}
}

func barLabels2(bar components.ActionBarProps) []string {
	p, s := barLabels(bar)
	return []string{p, s}
}

func TestActions_ApproveBlockedShowsNote(t *testing.T) {
	const blocked = "You can't approve your own Sample Order. Ask another Purchase Manager to approve it."
	e := newDocEnv(t)
	for _, r := range []auth.Right{"read", "approve", "print"} {
		e.az.Grant("Purchase Manager", docscreentest.Sample, r, auth.ScopeAll)
	}
	e.az.AddRole(ravi.ID, "Purchase Manager")
	e.as = ravi
	e.seed(doctype.StatePending)
	ctx := context.Background()

	v, err := e.d.View(ctx, ravi, docscreentest.Sample, 42)
	if err != nil {
		t.Fatal(err)
	}
	if v.ApproveBlocked != "" {
		t.Errorf("an eligible approver: ApproveBlocked = %q, want empty", v.ApproveBlocked)
	}

	e.apr.block = &approval.UserError{Kind: approval.ErrSelfApproval, Msg: blocked}
	v, err = e.d.View(ctx, ravi, docscreentest.Sample, 42)
	if err != nil {
		t.Fatal(err)
	}
	if v.ApproveBlocked != blocked {
		t.Errorf("ApproveBlocked = %q, want APR's text %q", v.ApproveBlocked, blocked)
	}
	bar, more := docscreen.Actions(v, true, docscreen.Rights(ctx, e.az, ravi, docscreentest.Sample, &v.Record))
	if bar.Note != blocked {
		t.Errorf("Note = %q, want %q", bar.Note, blocked)
	}
	if bar.Primary != nil || bar.Secondary != nil {
		t.Errorf("bar buttons %q shown with the note", barLabels2(bar))
	}
	for _, l := range moreLabels(more) {
		if l == "Approve" || l == "Reject" {
			t.Errorf("More has %q while approval is blocked", l)
		}
	}

	rec := e.get(t, doc42)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	doc := uitest.Doc(t, rec)
	if got := norm(doc.Find(".actionbar-note").Text()); got != blocked {
		t.Errorf("page note = %q, want %q", got, blocked)
	}
	for _, l := range []string{"Approve", "Reject"} {
		if buttonsNamed(doc.Selection, l).Length() != 0 {
			t.Errorf("page shows %q while approval is blocked", l)
		}
	}
}

func TestActions_ReadOnlyUserGetsNoBar(t *testing.T) {
	for _, s := range []doctype.State{doctype.StatePending, doctype.StateApproved, doctype.StateRejected, doctype.StateClosed, doctype.StateCancelled} {
		t.Run(string(s), func(t *testing.T) {
			e := newDocEnv(t, "read")
			e.seed(s)
			v := viewIn(s, true)
			r := docscreen.Rights(context.Background(), e.az, priya, docscreentest.Sample, &v.Record)
			if want := rightsOf(); !reflect.DeepEqual(r, want) {
				t.Errorf("Rights with only read = %v, want every right false", r)
			}
			bar, more := docscreen.Actions(v, true, r)
			if !reflect.DeepEqual(bar, components.ActionBarProps{}) {
				t.Errorf("bar = %+v, want empty", bar)
			}
			if len(more) != 0 {
				t.Errorf("more = %q, want none", moreLabels(more))
			}
			if html := uitest.Render(t, components.ActionBar(bar)).Find("body").Children().Length(); html != 0 {
				t.Error("ActionBar renders something for an empty bar")
			}
			if uitest.Render(t, components.MoreActions(more)).Find("body").Children().Length() != 0 {
				t.Error("MoreActions renders something with no items")
			}

			rec := e.get(t, doc42)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body %.300s", rec.Code, rec.Body)
			}
			doc := uitest.Doc(t, rec)
			if doc.Find(".actionbar, .actionbar-note").Length() != 0 {
				t.Error("page has an action bar for a read-only user")
			}
			if doc.Find(".more-actions").Length() != 0 || strings.Contains(doc.Text(), "More actions") {
				t.Error("page has a \"More actions\" section for a read-only user")
			}
			if doc.Find(`.app-page form[method="post"]`).Length() != 0 {
				t.Error("page has a POST form for a read-only user")
			}
		})
	}
}

func TestActions_PrintNeedsRightAndPrintable(t *testing.T) {
	all := rightsOf(pageRights...)
	noPrint := rightsOf("write", "submit", "approve", "cancel", "amend", "delete")
	for _, s := range []doctype.State{doctype.StateDraft, doctype.StatePending, doctype.StateRejected, doctype.StateApproved, doctype.StateClosed, doctype.StateCancelled} {
		v := viewIn(s, true)
		if _, more := docscreen.Actions(v, true, noPrint); slices.Contains(moreLabels(more), "Print") {
			t.Errorf("%s: Print without the print right", s)
		}
		if _, more := docscreen.Actions(v, false, all); slices.Contains(moreLabels(more), "Print") {
			t.Errorf("%s: Print on a DocType that isn't printable", s)
		}
		_, more := docscreen.Actions(v, true, all)
		p, ok := moreItem(more, "Print")
		if !ok {
			t.Errorf("%s: no Print with the right on a printable DocType", s)
			continue
		}
		if !p.NewTab || p.Post || p.Danger || p.Href == "" {
			t.Errorf("%s: Print = %+v, want a new-tab link", s, p)
		}
	}
}

// Danger items (design-system §7: a destructive action needs a confirm
// step) are GET links to their reason or confirm page, never one-tap POSTs.
func TestActions_DangerItemsLinkToConfirmPage(t *testing.T) {
	all := rightsOf(pageRights...)
	_, more := docscreen.Actions(viewIn(doctype.StateDraft, false), true, all)
	if d, ok := moreItem(more, "Delete draft"); !ok || d.Post || !d.Danger || d.Href != doc42+"/delete" {
		t.Errorf("Delete draft = %+v, want a danger GET link to %s/delete", d, doc42)
	}
	_, more = docscreen.Actions(viewIn(doctype.StateApproved, true), true, all)
	if c, ok := moreItem(more, "Cancel"); !ok || c.Post || !c.Danger || c.Href != doc42+"/cancel" {
		t.Errorf("Cancel = %+v, want a danger GET link to %s/cancel", c, doc42)
	}
	if c, ok := moreItem(more, "Close"); !ok || c.Post || c.Danger || c.Href != doc42+"/close" {
		t.Errorf("Close = %+v, want a GET link to %s/close", c, doc42)
	}

	e := newDocEnv(t, allRights...)
	e.seed(doctype.StateApproved)
	doc := uitest.Doc(t, e.get(t, doc42))
	for _, a := range []string{"cancel", "close"} {
		if doc.Find(`.more-actions a[href="`+doc42+"/"+a+`"]`).Length() != 1 {
			t.Errorf("no link to %s/%s under More actions", doc42, a)
		}
		if doc.Find(`form[action="`+doc42+"/"+a+`"]`).Length() != 0 {
			t.Errorf("the document page posts %s in one tap", a)
		}
	}
	if btn := buttonsNamed(uitest.Doc(t, e.get(t, doc42+"/cancel")).Selection, "Cancel sample order"); btn.Length() != 1 || !btn.HasClass("btn--danger") {
		t.Error("the cancel page has no danger \"Cancel sample order\" button")
	}

	d := newDocEnv(t, allRights...)
	d.seed(doctype.StateDraft)
	rec := d.get(t, doc42+"/delete")
	if rec.Code != http.StatusOK {
		t.Fatalf("delete page status = %d", rec.Code)
	}
	page := uitest.Doc(t, rec)
	if !strings.Contains(norm(page.Find("#main").Text()), "Delete Draft 42? This can't be undone.") {
		t.Errorf("delete page lacks the question; main = %q", norm(page.Find("#main").Text()))
	}
	btn := buttonsNamed(page.Selection, "Delete draft")
	if btn.Length() != 1 || goquery.NodeName(btn) != "button" || !btn.HasClass("btn--danger") {
		t.Fatal("no danger \"Delete draft\" submit button")
	}
	form := formOf(page, btn)
	if a, _ := form.Attr("action"); a != doc42+"/delete" {
		t.Errorf("delete form action = %q", a)
	}
	if form.Find(`input[name="csrf_token"][value="`+csrfToken+`"]`).Length() != 1 {
		t.Error("delete form has no csrf_token")
	}
	if len(d.docs.Calls()) != 0 {
		t.Error("GET of the confirm page called the engine")
	}
}
