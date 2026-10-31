package grnweb_test

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

const s1MsgDeleteToChange = "Delete this draft to receive against another order."

// MOD-GRN S1d (CD-332): step 1 of a draft with lines posts its stored
// order as a hidden input; a posted order other than that one, blank or
// missing, is refused with 422 on purchase_order_id and nothing is written.
// Validate refuses another order's id too, so "not an id" is the case only
// step 1's own check answers with this text.
// A stale page or a draft no longer in draft gets DOC's answer before
// anything posted is parsed, even a bad receipt date (CD-259).
func TestGRNWeb_FixedPOPostsStored(t *testing.T) {
	w := newS1World(t)
	po := w.po("SHREE", "10")
	other := w.po("BHARAT", "5")
	id := w.draft(po)
	path := stepPath(id, "details")
	stored := strconv.FormatInt(po.ID, 10)

	r := w.get(w.Suresh, path)
	if got := r.doc.Find(`input[type="hidden"][name="purchase_order_id"]`); got.Length() != 1 || got.AttrOr("value", "") != stored {
		t.Errorf("fixed step 1 posts %d purchase_order_id inputs, value %q, want one hidden with %s", got.Length(), got.AttrOr("value", ""), stored)
	}

	g := w.Load(t, goodsreceipt.DocType, id)
	version := strconv.Itoa(g.Version)
	for name, f := range map[string]url.Values{
		"crafted":   {"version": {version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {strconv.FormatInt(other.ID, 10)}},
		"blank":     {"version": {version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {""}},
		"not an id": {"version": {version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {"x"}},
		"missing":   {"version": {version}, "receipt_date": {"23-10-2026"}},
	} {
		r := w.post(w.Suresh, path, f)
		if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.errorSummary(), s1MsgDeleteToChange) {
			t.Errorf("%s PO = %d %q, want 422 %s", name, r.status, r.errorSummary(), s1MsgDeleteToChange)
		}
		if after := w.Load(t, goodsreceipt.DocType, id); after.Version != g.Version || after.Header["purchase_order_id"] != po.ID {
			t.Errorf("%s PO wrote: version %d PO %v", name, after.Version, after.Header["purchase_order_id"])
		}
	}

	for name, f := range map[string]url.Values{
		"another PO": {"version": {strconv.Itoa(g.Version - 1)}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {strconv.FormatInt(other.ID, 10)}},
		"bad date":   {"version": {strconv.Itoa(g.Version - 1)}, "receipt_date": {"x"}, "purchase_order_id": {stored}},
	} {
		r := w.post(w.Suresh, path, f)
		if r.status != http.StatusConflict {
			t.Errorf("stale page with %s = %d %q, want DOC's 409 first (CD-259)", name, r.status, r.errorSummary())
		}
		if after := w.Load(t, goodsreceipt.DocType, id); after.Version != g.Version || after.Header["purchase_order_id"] != po.ID ||
			after.Header["receipt_date"] != g.Header["receipt_date"] {
			t.Errorf("stale page with %s wrote: version %d PO %v date %v", name, after.Version, after.Header["purchase_order_id"], after.Header["receipt_date"])
		}
	}
	// docscreen reads the version with strconv.Atoi, so "+N" is the current
	// page too and gets the PO refusal, not DOC's conflict.
	r = w.post(w.Suresh, path, url.Values{"version": {"+" + version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {strconv.FormatInt(other.ID, 10)}})
	if r.status != http.StatusUnprocessableEntity || !strings.Contains(r.errorSummary(), s1MsgDeleteToChange) {
		t.Errorf("version +%s with another PO = %d %q, want 422 %s", version, r.status, r.errorSummary(), s1MsgDeleteToChange)
	}
	if r = w.post(w.Suresh, path, url.Values{"version": {version}, "receipt_date": {"x"}, "purchase_order_id": {stored}}); r.status != http.StatusUnprocessableEntity {
		t.Errorf("current page with a bad date = %d, want 422", r.status)
	}

	if r = w.post(w.Suresh, path, url.Values{"version": {version}, "receipt_date": {"23-10-2026"}, "purchase_order_id": {stored}}); r.status != http.StatusSeeOther {
		t.Errorf("save with the stored PO = %d %q, want 303", r.status, r.errorSummary())
	}
}

// CD-332 with CD-259: on a GRN past draft (approved, cancelled, pending)
// step 1's POST with the current version gets DOC's state answer before
// anything posted is parsed: neither the bad date nor the PO refusal, and
// nothing is written.
func TestGRNWeb_FixedPONotDraft(t *testing.T) {
	w := newS1World(t)
	approved := w.receiveAll(w.po("SHREE", "10"))
	cancelled := w.receiveAll(w.po("SHREE", "10"))
	w.Cancel(t, w.Ravi, cancelled.ID, "Counted twice")
	cancelled = w.Load(t, goodsreceipt.DocType, cancelled.ID)
	w.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
	po := w.po("SHREE", "10")
	pending := w.GRN(t, w.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StatePending)
	for name, g := range map[string]doc.Doc{"approved": approved, "cancelled": cancelled, "pending": pending} {
		r := w.post(w.Suresh, stepPath(g.ID, "details"), url.Values{"version": {strconv.Itoa(g.Version)}, "receipt_date": {"x"}, "purchase_order_id": {"x"}})
		if r.status != http.StatusConflict || strings.Contains(r.errorSummary(), s1MsgDeleteToChange) {
			t.Errorf("step 1 POST on a %s GRN = %d %q, want DOC's 409 state answer, not a parse or PO refusal", name, r.status, r.errorSummary())
		}
		if after := w.Load(t, goodsreceipt.DocType, g.ID); after.Version != g.Version || after.State != g.State ||
			after.Header["purchase_order_id"] != g.Header["purchase_order_id"] || after.Header["receipt_date"] != g.Header["receipt_date"] {
			t.Errorf("the refused post on the %s GRN wrote: version %d state %s PO %v date %v", name, after.Version, after.State, after.Header["purchase_order_id"], after.Header["receipt_date"])
		}
	}
}

// CD-332: Hide hides rest_not_coming only on a document past draft whose
// tick is off; a draft, a ticked document and the list (no View) show it.
func TestGRNWeb_HideRestNotComing(t *testing.T) {
	w := newS1World(t)
	hide := grnweb.Modules(grnweb.Deps{Service: w.Service})[goodsreceipt.DocType].Hide
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	f, _ := dt.Field("rest_not_coming")
	view := func(s doctype.State, tick string) *docscreen.DocView {
		return &docscreen.DocView{Info: doctype.Info{State: s}, Inputs: map[string]string{"rest_not_coming": tick}}
	}
	for _, tc := range []struct {
		name string
		view *docscreen.DocView
		want bool
	}{
		{"draft off", view(doctype.StateDraft, "false"), false},
		{"draft on", view(doctype.StateDraft, "true"), false},
		{"pending off", view(doctype.StatePending, "false"), true},
		{"approved off", view(doctype.StateApproved, "false"), true},
		{"approved on", view(doctype.StateApproved, "true"), false},
		{"closed off", view(doctype.StateClosed, "false"), true},
		{"rejected off", view(doctype.StateRejected, "false"), true},
		{"cancelled off", view(doctype.StateCancelled, "false"), true},
		{"no view", nil, false},
	} {
		if got := hide(docscreen.Ctx{DT: dt, View: tc.view}, f); got != tc.want {
			t.Errorf("%s: Hide(rest_not_coming) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// CD-332 through the public path: a reader (read only, not the creator)
// sees a draft's off tick as "No" in Details.
func TestGRNWeb_ReaderSeesDraftTickOff(t *testing.T) {
	w := newS1World(t)
	id := w.draft(w.po("SHREE", "10"))
	uid, _, err := w.Auth.CreateUser(t.Context(), w.Admin, auth.NewUser{ //nolint:gosec // G101: a test user's password
		Email: "buyer9@vardhan.test", Name: "Buyer Nine", Password: "Read the receipts 49", Roles: []string{"Purchase User"},
	})
	if err != nil {
		t.Fatal(err)
	}
	reader, _, err := w.Auth.LoadPrincipal(t.Context(), uid)
	if err != nil {
		t.Fatal(err)
	}
	if h := w.Load(t, goodsreceipt.DocType, id).Header; h["rest_not_coming"] == true {
		t.Fatalf("new draft has the tick on")
	}
	r := w.get(reader, "/doc/"+goodsreceipt.DocType+"/"+strconv.FormatInt(id, 10))
	if r.status != http.StatusOK {
		t.Fatalf("reader's draft page = %d", r.status)
	}
	if r.doc.Find(`a[href*="/edit/"]`).Length() != 0 {
		t.Errorf("the reader's page links to a step: not a read-only page")
	}
	if rows := r.summaryRows(); !slices.Contains(rows, "Vendor says the rest won't come: No") {
		t.Errorf("reader's draft page rows %q, want Vendor says the rest won't come: No", rows)
	}
}
