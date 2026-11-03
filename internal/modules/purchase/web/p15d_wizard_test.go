package purchaseweb

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PUX-021 (PO-048, CD-315, CD-258 G8; UX §5.15): the Request's Items step is
// cards at every width (CardsOnly), and each card reads the quantity with its
// unit and the line's note, with no rate.
func TestPUX021_RequestItemsCards(t *testing.T) {
	if !Modules(Deps{})[purchase.RequestDocType].CardsOnly {
		t.Error("Request Module CardsOnly = false, want the Items step without the desktop grid (CD-315)")
	}
	w := newP15dWeb(t)
	pr := w.prA(t, purchasetest.StopAtDraft())
	page := w.get(t, w.env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/items")
	if page.status != 200 {
		t.Fatalf("Suresh GET items = %d: %s", page.status, page.text())
	}
	for _, sel := range []string{".linegrid", ".linelist--grid", `[id^="grid-row-"]`} {
		if n := page.doc.Find(sel).Length(); n != 0 {
			t.Errorf("Items step has %d %s, want cards only", n, sel)
		}
	}
	if got := page.find(".lines-card .num"); !slices.Equal(got, []string{"40 BAG", "25 KGS"}) {
		t.Errorf("card quantities = %q, want 40 BAG and 25 KGS", got)
	}
	if got := page.notes(); !slices.Equal(got, []string{"UltraTech or ACC only"}) {
		t.Errorf("card notes = %q, want only row 1's note", got)
	}
}

// PUX-021's Details step (CD-258 Q8 (a); UX §5.15): Request date is hidden,
// and a request saved without it takes today's business date, so it can be
// submitted. The clock is just after IST midnight (18:31 UTC), so the date is
// the IST one (lessons: date tests cross the IST boundary).
func TestPUX021_RequestDateHidden(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	env.SetNow(t, time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC)) // 25-10-2026 00:01 IST

	page := w.get(t, env.Suresh, "/doc/"+purchase.RequestDocType+"/new")
	if page.status != 200 {
		t.Fatalf("Suresh GET new = %d: %s", page.status, page.text())
	}
	if page.doc.Find(`[name="request_date"]`).Length() != 0 || strings.Contains(page.text(), "Request date") {
		t.Errorf("Details shows Request date: %s", page.text())
	}
	a := w.do(t, env.Suresh, "POST", "/doc/"+purchase.RequestDocType+"/new", url.Values{
		"site_id": {w.id(t, "site", "METRO-SITE")}, "project_id": {w.id(t, "project", "PRJ-METRO")},
	})
	if a.status != 303 {
		t.Fatalf("POST Details = %d: %s", a.status, a.text())
	}
	var id int64
	var date string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT id, COALESCE(request_date, '') FROM purchase_request ORDER BY id DESC LIMIT 1`).Scan(&id, &date); err != nil {
		t.Fatal(err)
	}
	if date != "2026-10-25" {
		t.Errorf("request_date after a Details save without it = %q, want 2026-10-25 (IST today)", date)
	}
	pr := p15dDoc(purchase.RequestDocType, id)
	if a := w.do(t, env.Suresh, "POST", pr+"/lines/new", url.Values{
		"version": {w.version(t, id)}, "item_id": {w.id(t, "item", "CEMENT-OPC")}, "uom_id": {w.id(t, "uom", "BAG")}, "qty": {"40"},
	}); a.status != 303 {
		t.Fatalf("POST line = %d: %s", a.status, a.text())
	}
	if a := w.do(t, env.Suresh, "POST", pr+"/submit", url.Values{"version": {w.version(t, id)}}); a.status != 303 {
		t.Fatalf("submit = %d: %s", a.status, a.text())
	}
	var state string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT state FROM purchase_request WHERE id = ?`, id).Scan(&state); err != nil || state != string(doctype.StateApproved) {
		t.Errorf("state after submit = %q, %v; want approved", state, err)
	}
}

// PUX-021's check page (G2; UX §5.10, §5.15): the Comments card lists Priya's
// comment with the box.
func TestPUX021_RequestCheckComments(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t, purchasetest.StopAtDraft())
	if _, err := env.Comments.AddComment(t.Context(), env.Priya, purchase.RequestDocType, pr.ID, "Which brand do you want?"); err != nil {
		t.Fatal(err)
	}
	page := w.get(t, env.Suresh, p15dDoc(purchase.RequestDocType, pr.ID)+"/edit/check")
	if page.status != 200 {
		t.Fatalf("Suresh GET check = %d: %s", page.status, page.text())
	}
	card := p15dCard(page, "Comments")
	if !strings.Contains(card, "Which brand do you want?") || page.doc.Find(`textarea[name="body"]`).Length() != 1 {
		t.Errorf("check page Comments card = %q, want Priya's comment and the box", card)
	}
}

// PUX-022's Comments card on the document page (UX §5.10): every state,
// cancelled included.
func TestPUX022_RequestPageComments(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	pr := w.prA(t)
	if _, err := env.Comments.AddComment(t.Context(), env.Priya, purchase.RequestDocType, pr.ID, "Ordering the cement today."); err != nil {
		t.Fatal(err)
	}
	w.act(t, env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionCancel, "Raised twice")
	page := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, pr.ID))
	if card := p15dCard(page, "Comments"); !strings.Contains(card, "Ordering the cement today.") {
		t.Errorf("cancelled request's Comments card = %q, want Priya's comment", card)
	}
}

// version is request id's stored version, as a form value.
func (w *p15dWeb) version(t *testing.T, id int64) string {
	t.Helper()
	var v int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_request WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of request %d: %v", id, err)
	}
	return strconv.Itoa(v)
}
