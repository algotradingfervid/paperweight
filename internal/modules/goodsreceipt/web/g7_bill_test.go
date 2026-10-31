package grnweb_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G7 Do 4: POST /modules/goodsreceipt/receipts/{id}/bill. The
// route's write guard is internal/app's (Guard, TestGRNWeb_Guards); here
// the handler is called with the principal in the context, as the app's
// guard leaves it. The Bill details card's markup is S3's panel, so these
// rows assert the status, the redirect, that htmx answers carry the
// card (the element id "bill", UX §5.7) with the warning or error texts
// inside it, and that no answer is a toast (no HX-Trigger notice).
const (
	g7wInvoice     = "SST/2627/0457"
	g7wAccepted    = "Accepted value before tax is ₹6,250.00."
	g7wNoRequired  = "Vendor invoice no. is required."
	g7wNotApproved = "Add bill details after the receipt is approved."
	g7wStale       = "Someone else changed Bill details after you opened it. Reload it and make your changes again."
	g7wNoWrite     = "You don't have permission to change Goods Receipt. Ask your administrator for a role that allows it."
)

// g7wPost posts form to GRN id's bill route as p; htmx sets HX-Request.
func g7wPost(t *testing.T, r chi.Router, p auth.Principal, id int64, form url.Values, htmx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), p), http.MethodPost,
		"/modules/goodsreceipt/receipts/"+strconv.FormatInt(id, 10)+"/bill", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if htmx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

// g7wForm is the card's form: version and the three fields. A version of
// "" leaves it out.
func g7wForm(version, no, date, amount string) url.Values {
	f := url.Values{"invoice_no": {no}, "invoice_date": {date}, "invoice_amount": {amount}}
	if version != "" {
		f.Set("version", version)
	}
	return f
}

// g7wText is the answer's text with runs of white space made one space.
func g7wText(t *testing.T, html string, sel string) string {
	t.Helper()
	d, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse the answer: %v", err)
	}
	s := d.Selection
	if sel != "" {
		s = d.Find(sel)
	}
	return strings.Join(strings.Fields(s.Text()), " ")
}

// g7wWant asserts the answer's status, that it isn't a toast, and that the
// text of sel (the whole answer when sel is "") carries each of texts.
func g7wWant(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, sel string, texts ...string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d (body %q)", what, rec.Code, status, rec.Body.String())
	}
	if h := rec.Header().Get("HX-Trigger"); h != "" {
		t.Errorf("%s: HX-Trigger %q, want none (never a toast, design system §7)", what, h)
	}
	got := g7wText(t, rec.Body.String(), sel)
	for _, s := range texts {
		if !strings.Contains(got, s) {
			t.Errorf("%s: text of %q is %q, want it to carry %q", what, sel, got, s)
		}
	}
}

// g7wVersion is GRN id's bill version through goodsreceipt.Bill.
func g7wVersion(t *testing.T, env *grntest.Env, id int64) int {
	t.Helper()
	var v goodsreceipt.BillView
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		v, err = goodsreceipt.Bill(t.Context(), tx, id)
		return err
	}); err != nil {
		t.Fatalf("Bill(GRN %d): %v", id, err)
	}
	return v.Version
}

// g7wGRN is an approved GRN as p receiving 100 kg of po's line 1 (₹6,250.00
// accepted at ₹62.50).
func g7wGRN(t *testing.T, env *grntest.Env, p auth.Principal, po doc.Doc, stop doctype.State) doc.Doc {
	t.Helper()
	return env.GRN(t, p, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "100"}}, stop)
}

// MOD-GRN G7 Do 4 (UX §5.7; CD-94, CD-183, CD-245, CD-259; GRN-026, 027
// through the route): a no-JS save answers 303 to the GRN page at #bill; an
// htmx save answers 200 with the card, its duplicate warning and the
// information line inside it, never a toast; a refused field redraws the
// card with the error (422); a draft GRN is the state text (422); a stale
// version is 409 with DOC's text, also with an unreadable date (the values
// are parsed after the checks); Priya (no write) is AUTH's 403, also with an
// unreadable date; a GRN Suresh can't see is AUTH's 404; a missing version
// is 400. Refusals write nothing.
func TestGRNBillWeb_Save(t *testing.T) {
	env := grntest.New(t)
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: env.Service})(r)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{"qty": "1200", "price": "62.5"}))
	first := g7wGRN(t, env, env.Suresh, po, doctype.StateApproved)
	second := g7wGRN(t, env, env.Suresh, po, doctype.StateApproved)
	draft := g7wGRN(t, env, env.Suresh, po, doctype.StateDraft)
	tech := grntest.POAs(t, env, env.Admin, doctype.StateApproved, map[string]string{
		"site_id": g7wMasterID(t, env, "site", "TECH-SITE"), "project_id": g7wMasterID(t, env, "project", "PRJ-TECHPARK"),
	})
	hidden := g7wGRN(t, env, env.Admin, tech, doctype.StateApproved)

	rec := g7wPost(t, r, env.Suresh, first.ID, g7wForm("0", g7wInvoice, "24-10-2026", "7,375.00"), false)
	if want := "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(first.ID, 10) + "#bill"; rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Errorf("no-JS save: %d Location %q, want 303 %q", rec.Code, rec.Header().Get("Location"), want)
	}
	if v := g7wVersion(t, env, first.ID); v != 1 {
		t.Errorf("after the no-JS save: bill version %d, want 1", v)
	}

	rec = g7wPost(t, r, env.Suresh, second.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true)
	g7wWant(t, "htmx save", rec, http.StatusOK, "#bill", "Invoice "+g7wInvoice+" is also on "+first.Number+".", g7wAccepted)
	if v := g7wVersion(t, env, second.ID); v != 1 {
		t.Errorf("after the htmx save: bill version %d, want 1", v)
	}

	before := env.RowCounts(t)
	rec = g7wPost(t, r, env.Suresh, second.ID, g7wForm("1", "", "24-10-2026", ""), true)
	g7wWant(t, "htmx save with no invoice no.", rec, http.StatusUnprocessableEntity, "#bill", g7wNoRequired)
	rec = g7wPost(t, r, env.Suresh, draft.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true)
	g7wWant(t, "htmx save on a draft GRN", rec, http.StatusUnprocessableEntity, "", g7wNotApproved)
	rec = g7wPost(t, r, env.Suresh, second.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true)
	g7wWant(t, "htmx save at a stale version", rec, http.StatusConflict, "", g7wStale)
	rec = g7wPost(t, r, env.Suresh, second.ID, g7wForm("0", g7wInvoice, "31-02-2026", ""), true)
	g7wWant(t, "htmx save at a stale version with an unreadable date", rec, http.StatusConflict, "", g7wStale)

	rec = g7wPost(t, r, env.Priya, second.ID, g7wForm("1", g7wInvoice, "24-10-2026", ""), false)
	g7wWant(t, "Priya (no write)", rec, http.StatusForbidden, "", g7wNoWrite)
	rec = g7wPost(t, r, env.Priya, second.ID, g7wForm("1", g7wInvoice, "31-02-2026", ""), false)
	g7wWant(t, "Priya with an unreadable date", rec, http.StatusForbidden, "", g7wNoWrite)
	rec = g7wPost(t, r, env.Suresh, hidden.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), false)
	g7wWant(t, "a GRN Suresh can't see", rec, http.StatusNotFound, "", auth.MsgNotFound)
	rec = g7wPost(t, r, env.Suresh, second.ID, g7wForm("", g7wInvoice, "24-10-2026", ""), false)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("no version posted: status %d, want 400 (CD-183)", rec.Code)
	}
	if after := env.RowCounts(t); after != before {
		t.Errorf("the refused posts wrote: before %+v after %+v", before, after)
	}
	if v := g7wVersion(t, env, second.ID); v != 1 {
		t.Errorf("after the refused posts: bill version %d, want 1", v)
	}
}

// g7wMasterID is the id, as a form value, of the master row with code.
func g7wMasterID(t *testing.T, env *grntest.Env, table, code string) string {
	t.Helper()
	var id int64
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&id); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(id, 10)
}
