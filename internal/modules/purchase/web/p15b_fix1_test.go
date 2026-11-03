package purchaseweb

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	printweb "github.com/vardhan/internaltools/internal/kernel/print/web"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// P15b fix round 1: PO-062's panel in every state on PO and Request pages
// and never in print, the quantity guards one smallest unit (0.001) either
// side, ids of two digits, and PUX-020's no Attachments on cancelled.

// p15bComments are the Comments region's entries of page, each its
// "{name} · {time}" line then its body.
func p15bComments(p p15dPage) []string {
	var out []string
	p.doc.Find("#comments ol > li > p").Each(func(_ int, s *goquery.Selection) {
		out = append(out, p15dCollapse(s.Text()))
	})
	return out
}

// p15bAddComments adds two comments to dt id: first by a at 03:00 UTC on
// 31-10-2026 (08:30 IST), then by b at 18:31 UTC (00:01 IST on 01-11-2026,
// lessons: a clock just after IST midnight), and returns the entries the
// panel must show, oldest first.
func (w *p15dWeb) p15bAddComments(t *testing.T, dt string, id int64, a, b auth.Principal) []string {
	t.Helper()
	env := w.env
	env.SetNow(t, p15bAt(3, 0))
	if _, err := env.Comments.AddComment(t.Context(), a, dt, id, "First comment"); err != nil {
		t.Fatalf("AddComment as %s: %v", a.Name, err)
	}
	env.SetNow(t, p15bAt(18, 31))
	if _, err := env.Comments.AddComment(t.Context(), b, dt, id, "Second comment"); err != nil {
		t.Fatalf("AddComment as %s: %v", b.Name, err)
	}
	return []string{
		a.Name + " · 31-10-2026 08:30", "First comment",
		b.Name + " · 01-11-2026 00:01", "Second comment",
	}
}

// p15bCheckPanel fails unless page's Comments card has the region id
// #comments (the home's links land on it), the entries want in order, the
// box and Post comment.
func p15bCheckPanel(t *testing.T, what string, page p15dPage, want []string) {
	t.Helper()
	region := page.doc.Find(`section.card #comments`)
	if region.Length() != 1 {
		t.Fatalf("%s: no Comments card with id=\"comments\" (cards %q)", what, page.cards())
	}
	if got := p15bComments(page); !slices.Equal(got, want) {
		t.Errorf("%s: comments %q, want %q (oldest first, name and time)", what, got, want)
	}
	if region.Find(`textarea[name="body"]`).Length() != 1 {
		t.Errorf("%s: no comment box", what)
	}
	if got := p15dCollapse(region.Find(`button[type="submit"]`).Text()); !strings.Contains(got, "Post comment") {
		t.Errorf("%s: button %q, want Post comment", what, got)
	}
}

// p15bPrint is the print page of PO id as p through the real print handler,
// which must answer 200.
func (w *p15dWeb) p15bPrint(t *testing.T, p auth.Principal, id int64) string {
	t.Helper()
	env := w.env
	lh := allmasters.New(mstkit.Env{DB: env.DB, Auth: env.Auth, Engine: env.Engine, Auditor: audit.Default, Clock: env.Clock}).Letterhead
	h := printweb.Routes(printweb.Config{DB: env.DB, Auth: env.Auth, Info: env.Engine, Letterhead: lh})
	req := httptest.NewRequest(http.MethodGet, "/print/"+purchase.DocType+"/"+strconv.FormatInt(id, 10), nil)
	req = req.WithContext(auth.WithPrincipal(req.Context(), p))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("print PO %d as %s = %d: %s", id, p.Name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// PO-062 (spec §22, §26; UX §5.10): the PO and the Request document pages
// show the Comments card in every state, cancelled included: the comments
// oldest first, each with its name and time, the box and Post comment, in
// the region id="comments". The PO print has no comments.
func TestComments_Panel(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	for _, s := range p15bStates {
		t.Run("order/"+string(s), func(t *testing.T) {
			po := w.order(t, s)
			want := w.p15bAddComments(t, purchase.DocType, po.ID, env.Ravi, env.Arjun)
			p15bCheckPanel(t, "PO "+string(s), w.page(t, env.Suresh, po.ID), want)
			body := w.p15bPrint(t, env.Priya, po.ID)
			for _, bad := range []string{"First comment", "Second comment", "Comments", "Post comment", `id="comments"`} {
				if strings.Contains(body, bad) {
					t.Errorf("%s PO print has %q", s, bad)
				}
			}
		})
	}
	requests := map[doctype.State]func(t *testing.T) doc.Doc{
		doctype.StateDraft:    func(t *testing.T) doc.Doc { return w.prA(t, purchasetest.StopAtDraft()) },
		doctype.StateApproved: func(t *testing.T) doc.Doc { return w.prA(t) },
		doctype.StateClosed: func(t *testing.T) doc.Doc {
			pr := w.prA(t)
			w.act(t, env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionClose, "Bought locally")
			return pr
		},
		doctype.StateCancelled: func(t *testing.T) doc.Doc {
			pr := w.prA(t)
			w.act(t, env.Suresh, purchase.RequestDocType, pr.ID, doctype.ActionCancel, "Raised twice")
			return pr
		},
	}
	for _, s := range []doctype.State{doctype.StateDraft, doctype.StateApproved, doctype.StateClosed, doctype.StateCancelled} {
		t.Run("request/"+string(s), func(t *testing.T) {
			pr := requests[s](t)
			want := w.p15bAddComments(t, purchase.RequestDocType, pr.ID, env.Suresh, env.Priya)
			page := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, pr.ID))
			if page.status != http.StatusOK {
				t.Fatalf("Priya GET request %d = %d: %s", pr.ID, page.status, page.text())
			}
			p15bCheckPanel(t, "request "+string(s), page, want)
		})
	}
}

// p15bKGS is one line of qty KGS of MS-ANGLE-50 with extra.
func (w *p15dWeb) p15bKGS(t *testing.T, qty string, extra map[string]string) map[string]string {
	t.Helper()
	return w.line(t, "MS-ANGLE-50", "KGS", "7216", qty, extra)
}

// UX §5.9 line notes one smallest unit (0.001) above zero: already received
// 0.001 is a cutover line, and received 0.001 over the qty is Over by.
func TestPUX015_NoteBoundaries(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17", "earlier_po_date": "01-10-2026"}),
		purchasetest.OrderLines(
			w.p15bKGS(t, "10", map[string]string{"already_received": "0.001"}),
			w.p15bKGS(t, "10", nil),
		))
	purchasetest.SetReceived(t, env, po.Lines[1].ID, types.Qty(10001))
	want := []string{
		"Received 0 of 10 KGS", "Already received 0.001 KGS before this app · Pending 9.999 KGS",
		"Received 10.001 of 10 KGS", "Pending 0 KGS · Over by 0.001 KGS",
	}
	if got := w.page(t, env.Priya, po.ID).notes(); !slices.Equal(got, want) {
		t.Errorf("line notes = %q, want %q", got, want)
	}
}

// PUX-038 and PUX-016 one smallest unit short: a line received 0.001 below
// its qty is not fully received, so Receive goods stays the primary, and the
// close note counts it as pending.
func TestPUX038_PendingBoundary(t *testing.T) {
	w := newP15bWeb(t, true)
	env := w.env
	po := w.order(t, doctype.StateApproved, purchasetest.OrderLines(w.p15bKGS(t, "10", nil)))
	w.markSent(t, env.Priya, po.ID, "24-10-2026", "email", "")
	purchasetest.SetReceived(t, env, po.Lines[0].ID, types.Qty(9999))
	page := w.page(t, env.Suresh, po.ID)
	if label, href := page.primary(); label != "Receive goods" || href != p15bReceiveHref(po.ID) {
		t.Errorf("0.001 pending: primary %q %q, want Receive goods", label, href)
	}
	want := "1 line still has 0.001 KGS pending. Closing means no more goods can be received against this order."
	if note, _ := w.poCloseNote(t, env.Priya, po.ID); note != want {
		t.Errorf("close note %q, want %q", note, want)
	}
}

// Ids of two digits: a Goods receipts row links to its receipt's id in base
// 10, and the Copied from pair names the source read by its stored id.
func TestPO_TwoDigitIDs(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	filler := w.order(t, doctype.StateApproved)
	for range 9 {
		w.grn(t, filler.ID, "METRO-SITE", "29-10-2026")
	}
	po := w.order(t, doctype.StateApproved)
	g := w.grn(t, po.ID, "METRO-SITE", "30-10-2026")
	if g.ID < 10 {
		t.Fatalf("GRN id %d, want 10 or more", g.ID)
	}
	page := w.page(t, env.Priya, po.ID)
	link := page.doc.Find(`section.card a[href="` + p15dDoc(p15bGRN, g.ID) + `"]`)
	if link.Length() != 1 || !strings.Contains(p15dCollapse(link.Text()), g.Number) {
		t.Errorf("Goods receipts card %q: want one link to %s labelled %s", p15dCard(page, "Goods receipts"), p15dDoc(p15bGRN, g.ID), g.Number)
	}

	for last := po.ID; last < 9; {
		last = purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()).ID
	}
	src := w.techparkOrder(t)
	if src.ID < 10 {
		t.Fatalf("source PO id %d, want 10 or more", src.ID)
	}
	for range 3 { // ids the base-11 misreads of src land on exist
		purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	}
	cp := w.order(t, doctype.StateApproved, purchasetest.OrderHeader(map[string]string{
		"copied_from_id": strconv.FormatInt(src.ID, 10), "copied_from_no": src.Number,
	}))
	arjun, priya := w.page(t, env.Arjun, cp.ID).copiedFrom(), w.page(t, env.Priya, cp.ID).copiedFrom()
	if len(arjun) != len(priya)+1 || slices.ContainsFunc(arjun, func(v string) bool { return v != src.Number }) {
		t.Errorf("Copied from rows: Arjun %q, Priya %q; want Arjun's to add the pair %s", arjun, priya, src.Number)
	}
}

// PUX-020 and CD-243: the document page has the Attachments card in every
// state except cancelled.
func TestPUX020_NoAttachmentsCancelled(t *testing.T) {
	w := newP15bWeb(t, false)
	for _, s := range p15bStates {
		t.Run(string(s), func(t *testing.T) {
			po := w.order(t, s)
			page := w.page(t, w.env.Ravi, po.ID)
			if got, want := page.hasCard("Attachments"), s != doctype.StateCancelled; got != want {
				t.Errorf("%s: Attachments card %v, want %v (cards %q)", s, got, want, page.cards())
			}
		})
	}
}
