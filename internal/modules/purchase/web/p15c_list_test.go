package purchaseweb

import (
	"bytes"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// PO-052 and PUX-012 (PO spec §20.4 item 2; UX §5.6, §5.7; CD-280 D5, D6):
// with PR-A (METRO-SITE / PRJ-METRO: 40 BAG CEMENT-OPC, 25 KGS WIRE-18G and
// 2 BOX BOLT-M12, the last fully held by Priya's draft H), PR-B on PUNE-WH /
// PRJ-TECHPARK and PR-C on PUNE-WH / PRJ-METRO (same project, other site), Priya (restricted to
// PRJ-METRO, real AUTH) opens Items of her METRO-SITE / PRJ-METRO draft: the
// link reads Add from requests (2 open) and goes to ?from=requests. There,
// only PR-A is listed, in one card titled with its number, with requester
// and needed-by; both open rows are tickable with Pending {qty} {unit} and a
// Quantity to order input set to the pending amount; the held row reads
// On {H} (Priya Shah) with no checkbox or input. The bar is Add selected
// (nav=stay) and a Back to items link to the Items URL. Units show as the
// UoM code, as everywhere else in the app (PUX-021's "40 Bag" is 40 BAG).
func TestRequest_AddFromRequests(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	prA := w.prA(t, w.row(t, "BOLT-M12", "BOX", "2"))
	prB := w.request(t, "PUNE-WH", "PRJ-TECHPARK", w.row(t, "CEMENT-OPC", "BAG", "7"))
	prC := w.request(t, "PUNE-WH", "PRJ-METRO", w.row(t, "CEMENT-OPC", "BAG", "9"))
	held := w.hold(t, purchasetest.StopAtDraft(), prA.Lines[2].ID, "BOLT-M12", "BOX", "7318", "2")
	d := w.draft(t)

	items := w.get(t, env.Priya, p15cItems(d.ID))
	if items.status != 200 {
		t.Fatalf("Priya GET items = %d: %s", items.status, items.text())
	}
	if got := items.link(p15cLink(2)); !slices.Equal(got, []string{p15cFrom(d.ID)}) {
		t.Errorf("Items link %q hrefs = %q, want one to %s; page: %s", p15cLink(2), got, p15cFrom(d.ID), items.text())
	}
	if got := items.find(".lines-card"); len(got) != 1 {
		t.Errorf("Items step shows %d line cards, want the generic Items body under the link (1 line)", len(got))
	}

	page := w.get(t, env.Priya, p15cFrom(d.ID))
	if page.status != 200 {
		t.Fatalf("Priya GET ?from=requests = %d: %s", page.status, page.text())
	}
	if got := page.find("h2"); !slices.Contains(got, p15cHeading) {
		t.Errorf("h2s = %q, want %q", got, p15cHeading)
	}
	text := page.text()
	for _, other := range []string{prB.Number, prC.Number} {
		if strings.Contains(text, other) {
			t.Errorf("view lists %s (another site or project): %s", other, text)
		}
	}
	card := p15cCard(page, prA.Number)
	if card.Length() == 0 {
		t.Fatalf("no card titled %s: %s", prA.Number, text)
	}
	if n := page.doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return s.Find(`input[type="checkbox"]`).Length() > 0
	}).Length(); n != 1 {
		t.Errorf("%d cards with rows to tick, want PR-A's only", n)
	}
	cardText := p15cCollapse(card.Text())
	if !strings.Contains(cardText, "Suresh Patil · Needed by 07-11-2026") {
		t.Errorf("PR-A card = %q, want Suresh Patil · Needed by 07-11-2026", cardText)
	}
	rows := p15cRows(t, card)
	if len(rows) != 2 {
		t.Fatalf("PR-A has %d tickable rows, want 2 (BOLT-M12 is held)", len(rows))
	}
	for _, c := range []struct{ code, hint, qty string }{
		{"CEMENT-OPC", "Pending 40 BAG", "40"},
		{"WIRE-18G", "Pending 25 KGS", "25"},
	} {
		r := p15cFind(t, rows, c.code)
		if r.hint() != c.hint || r.value() != c.qty || r.ticked() {
			t.Errorf("%s row: hint %q, quantity %q, ticked %v; want %q, %q, unticked", c.code, r.hint(), r.value(), r.ticked(), c.hint, c.qty)
		}
		id, _ := r.qty.Attr("id")
		if got := p15cCollapse(page.doc.Find(`label[for="` + id + `"]`).Text()); got != p15cQtyLabel {
			t.Errorf("%s quantity label = %q, want %q", c.code, got, p15cQtyLabel)
		}
	}
	for _, r := range rows {
		if strings.Contains(r.label(), "BOLT-M12") {
			t.Errorf("held BOLT-M12 row has a checkbox")
		}
	}
	if want := "On " + p15cDisplay(held) + " (Priya Shah)"; !strings.Contains(cardText, "BOLT-M12") || !strings.Contains(cardText, want) {
		t.Errorf("PR-A card = %q, want the held BOLT-M12 row reading %q", cardText, want)
	}
	if got := p15cPrimaryNav(page, p15cPrimary); got != "nav=stay" {
		t.Errorf("%q button posts %q, want nav=stay (G6)", p15cPrimary, got)
	}
	if got := page.link(p15cBack); !slices.Equal(got, []string{p15cItems(d.ID)}) {
		t.Errorf("%q links = %q, want one to %s", p15cBack, got, p15cItems(d.ID))
	}
	// The generic Items view keeps docscreen's bar (Bar returns StepBar{}).
	if got := p15cPrimaryNav(items, p15cPrimary); got != "missing" {
		t.Errorf("generic Items view has an %q button (%s)", p15cPrimary, got)
	}
}

// PUX-012's empty view and the link's zero case (UX §5.6, §5.7): with PR-A's
// rows all held, Items has no link; ?from=requests still lists the held rows
// with nothing to tick, and only Back to items. With no request on the PO's
// site and project the view reads No open request lines for METRO-SITE ·
// PRJ-METRO.
func TestPUX012_NoOpenLines(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	d := w.draft(t)

	page := w.get(t, env.Priya, p15cFrom(d.ID))
	if page.status != 200 {
		t.Fatalf("GET ?from=requests = %d: %s", page.status, page.text())
	}
	if !strings.Contains(page.text(), "No open request lines for METRO-SITE · PRJ-METRO.") {
		t.Errorf("empty view = %q, want No open request lines for METRO-SITE · PRJ-METRO.", page.text())
	}
	if got := p15cPrimaryNav(page, p15cPrimary); got != "missing" {
		t.Errorf("empty view has %q (%s), want only %q", p15cPrimary, got, p15cBack)
	}
	if got := page.link(p15cBack); !slices.Equal(got, []string{p15cItems(d.ID)}) {
		t.Errorf("%q links = %q, want one to %s", p15cBack, got, p15cItems(d.ID))
	}

	pr := w.prA(t)
	w.hold(t, purchasetest.StopAtDraft(), pr.Lines[0].ID, "CEMENT-OPC", "BAG", "2523", "40")
	w.hold(t, purchasetest.StopAtApproved(), pr.Lines[1].ID, "WIRE-18G", "KGS", "7217", "25")
	items := w.get(t, env.Priya, p15cItems(d.ID))
	if strings.Contains(items.text(), p15cHeading) {
		t.Errorf("Items with nothing open shows the link: %s", items.text())
	}
	page = w.get(t, env.Priya, p15cFrom(d.ID))
	card := p15cCard(page, pr.Number)
	if card.Length() == 0 || len(p15cRows(t, card)) != 0 {
		t.Errorf("all-held PR-A: card found %v, want it listed with no row to tick: %s", card.Length() > 0, page.text())
	}
}

// PUX-012's rights (CD-233, CD-248, CD-280 D6; real AUTH, lessons §5): a
// buyer restricted to METRO-SITE whose Order read is own and Request read
// own (Employee) counts and lists only her own request's open rows, never
// Suresh's PR-A; her held row names nothing about Priya's draft, reading On
// another order. Priya, on her own draft, sees both requests and the holder
// by name.
func TestPUX012_UnreadableRowsAndHolders(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	buyer := w.probe(t, "buyer")
	prA := w.prA(t)
	own := w.ownRequest(t, buyer)
	held := w.hold(t, purchasetest.StopAtDraft(), own.lines[1], "WIRE-18G", "KGS", "7217", "5")
	bd := w.saveAs(t, buyer, map[string]string{
		"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"),
	})

	items := w.get(t, buyer, p15cItems(bd))
	if items.status != 200 {
		t.Fatalf("buyer GET items = %d: %s", items.status, items.text())
	}
	if got := items.link(p15cLink(1)); len(got) != 1 {
		t.Errorf("buyer's link = %q in %s, want %q (her own CEMENT-OPC row only)", got, items.text(), p15cLink(1))
	}
	page := w.get(t, buyer, p15cFrom(bd))
	text := page.text()
	if strings.Contains(text, prA.Number) || strings.Contains(text, "Suresh Patil") {
		t.Errorf("buyer's view names PR-A or its requester: %s", text)
	}
	card := p15cCard(page, own.display)
	if card.Length() == 0 {
		t.Fatalf("buyer's view has no card %s: %s", own.display, text)
	}
	if rows := p15cRows(t, card); len(rows) != 1 || !strings.Contains(rows[0].label(), "CEMENT-OPC") {
		t.Errorf("buyer's tickable rows = %d, want her CEMENT-OPC row only", len(rows))
	}
	cardText := p15cCollapse(card.Text())
	if !strings.Contains(cardText, p15cUnreadOne) || strings.Contains(text, p15cDisplay(held)) || strings.Contains(text, "Priya Shah") {
		t.Errorf("buyer's card = %q, want the held row as %q with no order or owner named", cardText, p15cUnreadOne)
	}

	pd := w.draft(t)
	items = w.get(t, env.Priya, p15cItems(pd.ID))
	if got := items.link(p15cLink(3)); len(got) != 1 {
		t.Errorf("Priya's link: %s, want %q (PR-A's 2 and the buyer's 1)", items.text(), p15cLink(3))
	}
	page = w.get(t, env.Priya, p15cFrom(pd.ID))
	if c := p15cCollapse(p15cCard(page, own.display).Text()); !strings.Contains(c, "On "+p15cDisplay(held)+" (Priya Shah)") {
		t.Errorf("Priya's card for %s = %q, want On %s (Priya Shah)", own.display, c, p15cDisplay(held))
	}
}

// p15cOwn is a request saved and submitted by its requester.
type p15cOwn struct {
	id      int64
	display string
	lines   []int64
}

// ownRequest is p's own submitted METRO-SITE / PRJ-METRO request: 10 BAG of
// CEMENT-OPC and 5 KGS of WIRE-18G.
func (w *p15cWeb) ownRequest(t *testing.T, p auth.Principal) p15cOwn {
	t.Helper()
	ctx, env := t.Context(), w.env
	res, err := env.Engine.Do(ctx, p, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO"),
			"request_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{w.row(t, "CEMENT-OPC", "BAG", "10"), w.row(t, "WIRE-18G", "KGS", "5")},
	})
	if err != nil {
		t.Fatalf("save %s's request: %v", p.Name, err)
	}
	if _, err := env.Engine.Do(ctx, p, purchase.RequestDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit %s's request: %v", p.Name, err)
	}
	var out p15cOwn
	out.id = res.ID
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		d, err := doc.LoadDoc(ctx, tx, purchase.RequestDocType, res.ID)
		out.display = p15cDisplay(d)
		for _, l := range d.Lines {
			out.lines = append(out.lines, l.ID)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// PUX-012's error path (P15c Builds; as P15a's last-price note): when
// OpenRequestLines fails, Items still renders with no link and the error is
// logged. The same page with a working Service shows the link first, so the
// hidden link is the error's doing. The draft has no lines, so nothing else
// on the page (P15a's last-price note) reads the Service and logs.
func TestPUX012_LinkHiddenOnError(t *testing.T) {
	w := newP15cWeb(t)
	env := w.env
	w.prA(t)
	id := w.saveAs(t, env.Priya, map[string]string{
		"vendor_id": w.id(t, "party", "SHREE"), "site_id": w.id(t, "site", "METRO-SITE"),
		"project_id": w.id(t, "project", "PRJ-METRO"), "order_date": types.Today(env.Clock).ISO(),
	})
	d := doc.Doc{ID: id}
	if got := w.get(t, env.Priya, p15cItems(d.ID)).link(p15cLink(2)); len(got) != 1 {
		t.Fatalf("working Service: link %q missing", p15cLink(2))
	}

	broken := testkit.NewDB(t)
	if err := broken.Close(); err != nil {
		t.Fatal(err)
	}
	svc := purchase.New(purchase.ServiceDeps{DB: broken, Engine: env.Engine, Auth: env.Auth, Approvals: env.Approvals, Comments: env.Comments, Clock: env.Clock})
	var logs bytes.Buffer
	before := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(before) })

	page := p15cDo(t, p15cHandler(env, svc), env.Priya, "GET", p15cItems(d.ID), nil)
	if page.status != 200 {
		t.Fatalf("Items with a failing OpenRequestLines = %d, want 200: %s", page.status, page.text())
	}
	if strings.Contains(page.text(), p15cHeading) {
		t.Errorf("Items with a failing OpenRequestLines shows the link: %s", page.text())
	}
	if logs.Len() == 0 {
		t.Error("a failing OpenRequestLines logged nothing")
	}
}
