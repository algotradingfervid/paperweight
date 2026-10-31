//go:build e2e

package e2e

import (
	"context"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// Purchase samples: three lines whose grand total, with the long tax code's
// 28%, is ₹99,99,97,440.00, just under the largest amount; 500 small lines.
const (
	poLineQty   = "1000.000"
	poLinePrice = "2604166.0000"
	poOrderDate = "2026-10-01"
	poDueDate   = "2026-10-10" // before the fixture clock, so the lines are overdue
)

// purchaseSamples are the sweep's purchase documents: orders and requests in
// every state docscreen.Screens visits, an approved order marked as sent,
// with comments.
type purchaseSamples struct {
	order, request docscreen.SampleIDs
	sent           int64
}

// purchaseSamples seeds Priya's long-value requests and orders on
// METRO-SITE / PRJ-METRO, from the long vendor and item (UX §4's longest
// values). The draft order's first line holds the submitted request's first
// row, so that request shows a held row. Orders get the fixture's approval
// rules first (00-fixtures.md): past 5 lakh Meena, the Director, approves.
// It runs after grnSamples, which set the PO series past the fixture's.
func (h *Harness) purchaseSamples(long testkit.LongRows) purchaseSamples {
	h.t.Helper()
	code := func(table, c string) string {
		return strconv.FormatInt(Scalar[int64](h, `SELECT id FROM `+table+` WHERE code = ?`, c), 10) //nolint:gosec // G202: fixed table names
	}
	id := func(v int64) string { return strconv.FormatInt(v, 10) }
	site, project, item, uom := code("site", "METRO-SITE"), code("project", "PRJ-METRO"), id(long.Item), id(h.Fx.Units[0].ID)

	request := func(lines int) doc.Result {
		in := doc.ActionInput{Values: map[string]string{
			"site_id": site, "project_id": project, "request_date": plainDate, "purpose": uitest.LongError[:longRemarks],
		}}
		for range lines {
			in.Lines = append(in.Lines, map[string]string{
				"item_id": item, "uom_id": uom, "qty": maxQty, "description": uitest.LongName100, "note": uitest.LongName100,
			})
		}
		return h.Do(priya, purchase.RequestDocType, 0, doctype.ActionSave, in)
	}
	submitted := func() doc.Result {
		r := request(longLines)
		return h.Do(priya, purchase.RequestDocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	}
	var s purchaseSamples
	s.request = docscreen.SampleIDs{CloseAs: priya, Draft: request(longLines).ID, Lines500: request(manyLines).ID}
	held := submitted().ID
	s.request.Approved = held
	r := submitted()
	s.request.Cancelled = h.Do(priya, purchase.RequestDocType, r.ID, doctype.ActionCancel, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID
	r = submitted()
	s.request.Closed = h.Do(priya, purchase.RequestDocType, r.ID, doctype.ActionClose, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID

	admin := h.Principal(admin)
	ctx := auth.WithPrincipal(context.Background(), admin)
	for _, rule := range []approval.RuleInput{
		{DocType: purchase.DocType, FromAmount: 50_000_00, ApproverType: approval.ApproverRole, Role: "Purchase Manager"},
		{DocType: purchase.DocType, FromAmount: 5_00_000_00, ApproverType: approval.ApproverRole, Role: "Director"},
	} {
		if _, err := h.App.Approvals().CreateRule(ctx, admin, rule); err != nil {
			h.t.Fatalf("approval rule for %s from %d: %v", rule.DocType, rule.FromAmount, err)
		}
	}
	order := func(lines int, qty, price, requestRow string) doc.Result {
		in := doc.ActionInput{Values: map[string]string{
			"vendor_id": id(long.Party), "site_id": site, "project_id": project, "order_date": poOrderDate, "required_by": poDueDate,
			"vendor_quote_ref": uitest.LongCode60, "terms": uitest.LongError, "remarks": uitest.LongError[:longRemarks],
		}}
		for i := range lines {
			l := map[string]string{"item_id": item, "uom_id": uom, "qty": qty, "price": price, "description": uitest.LongName100}
			if i == 0 && requestRow != "" {
				l["request_line_id"] = requestRow
			}
			in.Lines = append(in.Lines, l)
		}
		return h.Do(priya, purchase.DocType, 0, doctype.ActionSave, in)
	}
	pending := func() doc.Result {
		r := order(longLines, poLineQty, poLinePrice, "")
		return h.Do(priya, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	}
	approved := func() doc.Result {
		r := pending()
		return h.Do(meena, purchase.DocType, r.ID, doctype.ActionApprove, doc.ActionInput{Version: r.Version})
	}
	row := id(Scalar[int64](h, `SELECT id FROM purchase_request_line WHERE parent_id = ? ORDER BY line_no LIMIT 1`, held))
	s.order = docscreen.SampleIDs{CloseAs: priya,
		Draft: order(longLines, poLineQty, poLinePrice, row).ID, Lines500: order(manyLines, "1.000", "1", "").ID,
	}
	s.order.Pending = pending().ID
	s.order.Approved = approved().ID
	r = pending()
	s.order.Rejected = h.Do(meena, purchase.DocType, r.ID, doctype.ActionReject, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID
	r = approved()
	s.order.Closed = h.Do(priya, purchase.DocType, r.ID, doctype.ActionClose, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID
	r = approved()
	s.order.Cancelled = h.Do(ravi, purchase.DocType, r.ID, doctype.ActionCancel, doc.ActionInput{Version: r.Version, Reason: uitest.LongError}).ID

	// P11's Sent record, written as the expense samples mark an advance paid.
	s.sent = approved().ID
	pr := h.Principal(priya)
	if _, err := h.App.DB().SQL().ExecContext(context.Background(),
		`INSERT INTO purchase_order_sent (po_id, sent_on, sent_via, note, sent_by, sent_at, version) VALUES (?, ?, 'whatsapp', ?, ?, ?, 1)`,
		s.sent, plainDate, uitest.LongName100, pr.ID, types.TimestampOf(h.Clock.Now()).String()); err != nil {
		h.t.Fatalf("mark PO %d sent: %v", s.sent, err)
	}

	// Comments through the real service: an unbroken 1,000-character body and
	// a long name's, on the approved order and the submitted request.
	comments := modkit.NewComments(modkit.CommentsDeps{DB: h.App.DB(), Auth: h.App.Auth(), Info: h.App.Engine(), Clock: h.Clock})
	for _, c := range []struct {
		dt string
		id int64
	}{{purchase.DocType, s.order.Approved}, {purchase.RequestDocType, s.request.Approved}} {
		for _, body := range []string{strings.Repeat(uitest.LongCode60, modkit.MaxCommentRunes/len(uitest.LongCode60)), uitest.LongName100} {
			if _, err := comments.AddComment(context.Background(), pr, c.dt, c.id, body); err != nil {
				h.t.Fatalf("comment on %s %d: %v", c.dt, c.id, err)
			}
		}
	}
	return s
}

// purchaseScreens are both DocTypes' docscreen pages as Priya, who raises
// them; the reject page as Meena, the Director the rule picks past 5 lakh,
// and the order's cancel page as Ravi, the Purchase Manager, who holds
// cancel. Then the module's pages in the states UX §6.1 lists.
func (h *Harness) purchaseScreens(s purchaseSamples, pr int64) []screens.Screen {
	var out []screens.Screen
	for _, sc := range docscreen.Screens(purchase.DocType, s.order) {
		switch {
		case strings.HasSuffix(sc.Name, " reject"):
			sc.As = meena
		case strings.HasSuffix(sc.Name, " cancel"):
			sc.As = ravi
		}
		out = append(out, sc)
	}
	out = append(out, docscreen.Screens(purchase.RequestDocType, s.request)...)

	order := func(id int64) string { return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) }
	const orderRoute = "/modules/purchase/orders/{id}"
	const page, step = "/doc/{doctype}/{id}", "/doc/{doctype}/{id}/edit/{step}"
	const fromRoute = "/modules/purchase/requests/{id}/order"
	fromURL := func(id int64) string { return "/modules/purchase/requests/" + strconv.FormatInt(id, 10) + "/order" }
	cement := strconv.FormatInt(Scalar[int64](h, `SELECT id FROM item WHERE code = 'CEMENT-OPC'`), 10)
	out = append(out,
		screens.Screen{Name: "purchase home manager", Route: "/modules/purchase", URL: "/modules/purchase", As: ravi},
		screens.Screen{Name: "purchase home stores", Route: "/modules/purchase", URL: "/modules/purchase", As: suresh},
		screens.Screen{Name: "purchase home empty", Route: "/modules/purchase", URL: "/modules/purchase", As: deepak},

		screens.Screen{Name: "purchase order draft reader", Route: page, URL: DocPath(purchase.DocType, s.order.Draft), As: ravi},
		screens.Screen{Name: "purchase order sent", Route: page, URL: DocPath(purchase.DocType, s.sent), As: priya},
		screens.Screen{Name: "purchase order add from requests", Route: step, URL: DocPath(purchase.DocType, s.order.Draft) + "/edit/items?from=requests", As: priya},
		screens.Screen{Name: "purchase order comment refused", Route: page, URL: DocPath(purchase.DocType, s.order.Approved), As: priya, Setup: refuseComment},
		screens.Screen{Name: "purchase request draft reader", Route: page, URL: DocPath(purchase.RequestDocType, s.request.Draft), As: ravi},
		screens.Screen{Name: "purchase request as manager", Route: page, URL: DocPath(purchase.RequestDocType, s.request.Approved), As: ravi},

		screens.Screen{Name: "purchase hand over pending", Route: orderRoute + "/handover", URL: order(s.order.Pending) + "/handover", As: priya},
		screens.Screen{Name: "purchase send approved", Route: orderRoute + "/send", URL: order(s.order.Approved) + "/send", As: priya},
		screens.Screen{Name: "purchase send sent", Route: orderRoute + "/send", URL: order(s.sent) + "/send", As: priya},
		screens.Screen{Name: "purchase send refused", Route: orderRoute + "/send", URL: order(s.order.Approved) + "/send", As: priya,
			Setup: refuse(`input[name=sent_on]`, `
  f.elements.sent_on.value = "";
  f.elements.sent_via.forEach((e) => { e.checked = false; });`)},
		screens.Screen{Name: "purchase send stale", Route: orderRoute + "/send", URL: order(s.sent) + "/send", As: priya, Setup: stale(`input[name=sent_on]`)},
		screens.Screen{Name: "purchase expected on approved", Route: orderRoute + "/expected", URL: order(s.order.Approved) + "/expected", As: priya},
		screens.Screen{Name: "purchase expected on refused", Route: orderRoute + "/expected", URL: order(s.order.Approved) + "/expected", As: priya,
			Setup: refuse(`input[name=expected_on]`, `f.elements.expected_on.value = "x";`)},

		screens.Screen{Name: "create PO from long request", Route: fromRoute, URL: fromURL(s.request.Approved), As: priya},
		screens.Screen{Name: "create PO from request refused", Route: fromRoute, URL: fromURL(pr), As: priya, Setup: refuse(`form[action$='/order'] input[name=version]`, "")},
		screens.Screen{Name: "create PO from request stale", Route: fromRoute, URL: fromURL(pr), As: priya, Setup: stale(`form[action$='/order'] input[name=version]`)},

		screens.Screen{Name: "purchase pending report overdue", Route: "/reports/purchase/pending", URL: "/reports/purchase/pending?overdue=1", As: priya},
		screens.Screen{Name: "purchase pending report filtered empty", Route: "/reports/purchase/pending", URL: "/reports/purchase/pending?item=" + cement, As: priya},
	)
	for _, q := range []string{
		purchase.QueueDrafts, purchase.QueueWaiting, purchase.QueueToSend, purchase.QueueToClose,
		purchase.QueueAwaitingDelivery, purchase.QueueRequestsToOrder, purchase.QueueMyRequests,
	} {
		out = append(out, screens.Screen{Name: "purchase queue " + q, Route: "/modules/purchase/q/{queue}", URL: "/modules/purchase/q/" + q, As: priya})
	}
	return out
}

// refuseComment posts a comment one character too long, so the box shows
// its error with the typed text kept (UX §5.10).
func refuseComment(ctx context.Context) error {
	return submit(ctx, `(() => {
  const box = document.querySelector('form[action^="/modules/comments/"] textarea');
  box.value = "`+strings.Repeat("x", modkit.MaxCommentRunes+1)+`";
  box.form.requestSubmit();
})()`, `form[action^="/modules/comments/"] textarea[aria-invalid="true"]`)
}

// stale saves the form of field with the version before the one it has, so
// the page shows its 409 banner with Reload.
func stale(field string) func(context.Context) error {
	return func(ctx context.Context) error {
		return submit(ctx, `(() => {
  const f = document.querySelector(`+strconv.Quote(field)+`).form;
  f.elements.version.value = String(Number(f.elements.version.value) - 1);
  f.noValidate = true;
  f.requestSubmit();
})()`, ".banner--danger a[href]")
	}
}
