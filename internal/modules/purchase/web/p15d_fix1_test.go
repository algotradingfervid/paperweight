package purchaseweb

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// P15d fix round 1 (AD-13 rulings): the Create PO state check, read before
// create, SetRequestDate's WHERE, rejected holders, a closed linked order's
// receipt word and the Create PO page's vendor and needed-by lines.

// p15dNotSubmitted is the Create PO refusal on a draft request (AD-13).
func p15dNotSubmitted(display string) string {
	return display + " isn't submitted yet. Submit it before you create a purchase order from it."
}

// p15dCancelledText is the Create PO refusal on a cancelled request (AD-13).
func p15dCancelledText(display string) string {
	return display + " is cancelled. You can't create a purchase order from it."
}

// orderRows is the number of purchase orders and of their lines.
func (w *p15dWeb) orderRows(t *testing.T) (orders, lines int) {
	t.Helper()
	db := w.env.DB.SQL()
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM purchase_order`).Scan(&orders); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(t.Context(), `SELECT count(*) FROM purchase_order_line`).Scan(&lines); err != nil {
		t.Fatal(err)
	}
	return orders, lines
}

// PUX-025's state check (AD-13, as P11's MarkSent refuses a wrong state:
// doc.KindInvalid, 422): GET and POST Create PO on a draft or a cancelled
// request answer 422 with the request's own text and no form, and the POST
// writes nothing.
func TestPUX025_CreatePOWrongState(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	draft := w.prA(t, purchasetest.StopAtDraft())
	cancelled := w.prA(t)
	w.act(t, env.Suresh, purchase.RequestDocType, cancelled.ID, doctype.ActionCancel, "Raised twice")
	shree := w.id(t, "party", "SHREE")
	for _, c := range []struct {
		name string
		id   int64
		want string
	}{
		{"draft", draft.ID, p15dNotSubmitted("Draft " + strconv.FormatInt(draft.ID, 10))},
		{"cancelled", cancelled.ID, p15dCancelledText(cancelled.Number)},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := p15dOrderPath(c.id)
			page := w.get(t, env.Priya, path)
			if page.status != http.StatusUnprocessableEntity || !strings.Contains(page.text(), c.want) {
				t.Errorf("GET = %d %q, want 422 with %q", page.status, page.text(), c.want)
			}
			if page.doc.Find(`form[action="`+path+`"]`).Length() != 0 {
				t.Error("GET: the page has the Create PO form")
			}
			orders, lines := w.orderRows(t)
			a := w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {w.version(t, c.id)}, "vendor_id": {shree}})
			if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text(), c.want) {
				t.Errorf("POST = %d %q, want 422 with %q", a.status, a.text(), c.want)
			}
			if a.doc.Find(`form[action="`+path+`"]`).Length() != 0 {
				t.Error("POST: the redraw has the Create PO form")
			}
			if o, l := w.orderRows(t); o != orders || l != lines {
				t.Errorf("POST wrote orders %d to %d, lines %d to %d; want nothing written", orders, o, lines, l)
			}
		})
	}
}

// PUX-025's check order (CD-259; AD-13): load, read, create right, version
// (POST only), then state. Suresh (no create on Purchase Order) gets AUTH's
// 403 on his own draft, not the state text; Priya posting a stale version on
// the draft gets DOC's stale text, not the state text.
func TestPUX025_CreatePOWrongStateOrder(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	draft := w.prA(t, purchasetest.StopAtDraft())
	display := "Draft " + strconv.FormatInt(draft.ID, 10)
	path := p15dOrderPath(draft.ID)
	denied := (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	shree := w.id(t, "party", "SHREE")
	v, _ := strconv.Atoi(w.version(t, draft.ID))

	if a := w.get(t, env.Suresh, path); a.status != http.StatusForbidden || !strings.Contains(a.text(), denied) || strings.Contains(a.text(), p15dNotSubmitted(display)) {
		t.Errorf("Suresh GET on his draft = %d %q, want 403 with %q only", a.status, a.text(), denied)
	}
	if a := w.do(t, env.Suresh, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v)}, "vendor_id": {shree}}); a.status != http.StatusForbidden || !strings.Contains(a.text(), denied) || strings.Contains(a.text(), p15dNotSubmitted(display)) {
		t.Errorf("Suresh POST on his draft = %d %q, want 403 with %q only", a.status, a.text(), denied)
	}
	stale := "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
	a := w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v + 1)}, "vendor_id": {shree}})
	if a.status != http.StatusConflict || !strings.Contains(a.text(), stale) || strings.Contains(a.text(), p15dNotSubmitted(display)) {
		t.Errorf("Priya stale POST on the draft = %d %q, want 409 with %q only", a.status, a.text(), stale)
	}
}

// PUX-025's read before create (CD-245, CD-259): Suresh, who reads purchase
// orders but has no create on them, gets AUTH's 404, not 403, on a request
// his restriction hides, on the GET and the POST, and nothing is written.
func TestPUX025_CreatePOHiddenBeforeCreate(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	hidden := p15dTechRequest(t, w)
	if got := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, hidden)).status; got != http.StatusNotFound {
		t.Fatalf("setup: Suresh GET the TECH-SITE request = %d, want 404 (hidden by his restriction)", got)
	}
	ordered := w.hold(t, purchasetest.StopAtApproved(), w.prA(t).Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "5")
	if got := w.get(t, env.Suresh, p15dDoc(purchase.DocType, ordered.ID)).status; got != http.StatusOK {
		t.Fatalf("setup: Suresh GET a METRO-SITE order = %d, want 200 (he reads orders)", got)
	}
	path := p15dOrderPath(hidden)
	if a := w.get(t, env.Suresh, path); a.status != http.StatusNotFound || !strings.Contains(a.text(), auth.MsgNotFound) {
		t.Errorf("Suresh GET hidden = %d %q, want 404", a.status, a.text())
	}
	orders, lines := w.orderRows(t)
	a := w.do(t, env.Suresh, http.MethodPost, path, url.Values{"version": {w.version(t, hidden)}, "vendor_id": {w.id(t, "party", "SHREE")}})
	if a.status != http.StatusNotFound || !strings.Contains(a.text(), auth.MsgNotFound) {
		t.Errorf("Suresh POST hidden = %d %q, want 404", a.status, a.text())
	}
	if o, l := w.orderRows(t); o != orders || l != lines {
		t.Errorf("POST wrote orders %d to %d, lines %d to %d; want nothing written", orders, o, lines, l)
	}
}

// PUX-021's Request date fill (CD-258 Q8 (a)) touches only the saved draft:
// a back-dated submitted request keeps its request_date after a new draft is
// saved with a blank one.
func TestPUX021_RequestDateFillOnlyThisDraft(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	back := purchasetest.NewRequest(t, env, purchasetest.RequestHeader(map[string]string{"request_date": "20-10-2026"}))
	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft(), purchasetest.RequestHeader(map[string]string{"request_date": ""}))
	date := func(id int64) string {
		t.Helper()
		var s string
		if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT COALESCE(request_date, '') FROM purchase_request WHERE id = ?`, id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if got := date(back.ID); got != "2026-10-20" {
		t.Errorf("back-dated request's request_date after another draft's save = %q, want 2026-10-20", got)
	}
	if got, want := date(draft.ID), types.Today(env.Clock).ISO(); got != want {
		t.Errorf("blank draft's request_date = %q, want today %s", got, want)
	}
}

// PUX-022 and PUX-023 with a rejected holder (spec §20.2: a rejected order
// still waits on approval): its row's note names it with its owner, and the
// close page warns about it, as for a draft or pending holder.
func TestPUX022_RejectedHolder(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtPending(), purchasetest.OrderLines(map[string]string{
		"item_id": w.id(t, "item", "CEMENT-OPC"), "uom_id": w.id(t, "uom", "BAG"), "hsn_sac": "2523", "qty": "40", "price": "1500",
		"request_line_id": strconv.FormatInt(pr.Lines[0].ID, 10),
	}))
	w.act(t, env.Ravi, purchase.DocType, po.ID, doctype.ActionReject, "Price too high")
	if got := p15dState(t, w, po.ID); got != doctype.StateRejected {
		t.Fatalf("the 40 Bag order is %s, want rejected", got)
	}
	named := p15dDisplay(po) + " (Priya Shah)"

	page := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, pr.ID))
	if page.status != http.StatusOK {
		t.Fatalf("Priya GET PR-A = %d: %s", page.status, page.text())
	}
	if got := page.notes(); len(got) != 2 || !strings.HasSuffix(got[0], " · On "+named) {
		t.Errorf("line notes = %q, want row 1 to end On %s", got, named)
	}
	note, _ := w.closeNote(t, env.Suresh, pr.ID)
	want := "1 row is on an order not yet approved: " + named + ". That order can't be approved until the row is removed."
	if note != want {
		t.Errorf("close note = %q, want %q", note, want)
	}
}

// PUX-025's picker summary (CD-233): a 422 with no vendor posted shows no
// chosen row; a vendor the user can't read, or an unknown id, shows the raw
// id as the chosen row.
func TestPUX025_CreatePOVendorSummary(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	path := p15dOrderPath(pr.ID)
	shree := w.id(t, "party", "SHREE")
	v, _ := strconv.Atoi(w.version(t, pr.ID))

	a := w.do(t, env.Priya, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v)}, "vendor_id": {""}})
	chosen := a.doc.Find(`[data-picker-chosen]`)
	if _, hidden := chosen.Attr("hidden"); a.status != http.StatusUnprocessableEntity || !hidden || p15dCollapse(chosen.Find(".picker__summary").Text()) != "" {
		t.Errorf("no vendor: 422 = %d, chosen hidden %v, summary %q; want a hidden, empty chosen row", a.status, hidden, p15dCollapse(chosen.Find(".picker__summary").Text()))
	}

	creator := w.probe(t, "summary-no-party", auth.RightCreate)
	for _, c := range []struct {
		name   string
		p      auth.Principal
		vendor string
	}{
		{"no party read", creator, shree},
		{"unknown vendor", env.Priya, "999999"},
	} {
		a := w.do(t, c.p, http.MethodPost, path, url.Values{"version": {strconv.Itoa(v - 1)}, "vendor_id": {c.vendor}})
		chosen := a.doc.Find(`[data-picker-chosen]`)
		_, hidden := chosen.Attr("hidden")
		if got := p15dCollapse(chosen.Find(".picker__summary").Text()); a.status != http.StatusConflict || hidden || got != c.vendor {
			t.Errorf("%s: 409 = %d, chosen hidden %v, summary %q; want the raw id %s shown", c.name, a.status, hidden, got, c.vendor)
		}
	}
}

// PUX-025's summary line without a needed-by date (UX §5.18): no Needed by.
func TestPUX025_CreatePONoNeededBy(t *testing.T) {
	w := newP15dWeb(t)
	pr := w.prA(t, purchasetest.RequestHeader(map[string]string{"required_by": ""}))
	page := w.get(t, w.env.Priya, p15dOrderPath(pr.ID))
	if page.status != http.StatusOK {
		t.Fatalf("GET = %d: %s", page.status, page.text())
	}
	summary := "From " + pr.Number + " · Suresh Patil · METRO-SITE"
	if !strings.Contains(page.text(), summary) || strings.Contains(page.text(), "Needed by") {
		t.Errorf("page = %q, want the summary %q with no Needed by", page.text(), summary)
	}
	if got := page.find(".lines-card .num"); !slices.Equal(got, []string{"40 BAG", "25 KGS"}) {
		t.Errorf("rows = %q, want both rows pending", got)
	}
}
