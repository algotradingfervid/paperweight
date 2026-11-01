package purchaseweb

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

const homePath = "/modules/purchase"

// cardTitles are each home card's title, by Card.Key (UX §5.1, CD-258 Q4).
var cardTitles = map[string]string{
	purchase.CardToApprove:         "To approve",
	purchase.QueueDrafts:           "My drafts",
	purchase.QueueToSend:           "To send",
	purchase.QueueToClose:          "To close",
	purchase.QueueRequestsToOrder:  "Requests to order",
	purchase.QueueWaiting:          "Waiting for approval",
	purchase.QueueMyRequests:       "My purchase requests",
	purchase.QueueAwaitingDelivery: "Awaiting delivery",
	purchase.CardOverdue:           "Overdue deliveries",
}

// counted is "1 {one}" or "{n} {many}".
func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return types.FormatCount(int64(n)) + " " + many
}

// cardLine is a home card's one line (UX §5.1 copy), singular for 1.
func cardLine(c purchase.Card) string {
	switch c.Key {
	case purchase.CardToApprove:
		return types.FormatCount(int64(c.Count)) + " waiting for your approval"
	case purchase.QueueDrafts:
		s := counted(c.Count, "order", "orders")
		if c.NeedsChanges > 0 {
			s += metaSep + counted(c.NeedsChanges, "needs changes", "need changes")
		}
		return s
	case purchase.QueueWaiting:
		s := counted(c.Count, "order", "orders")
		if c.WaitingDays > 0 {
			s += metaSep + "oldest waiting " + strconv.Itoa(c.WaitingDays) + " days"
		}
		return s
	case purchase.QueueToSend:
		return counted(c.Count, "approved order not sent yet", "approved orders not sent yet")
	case purchase.QueueToClose:
		return counted(c.Count, "order the vendor won't complete", "orders the vendor won't complete")
	case purchase.QueueRequestsToOrder:
		return counted(c.Count, "request with items to order", "requests with items to order")
	case purchase.QueueMyRequests:
		return types.FormatCount(int64(c.Count)) + " open"
	case purchase.QueueAwaitingDelivery:
		return counted(c.Count, "order not fully received", "orders not fully received")
	}
	return counted(c.Count, "line past its due date", "lines past their due date")
}

// commentCut is how much of a recent comment the home shows (UX §5.1).
const commentCut = 200

// cutComment is body cut at a word to commentCut characters with "…"; a
// single longer word is cut at commentCut.
func cutComment(body string) string {
	runes := []rune(body)
	if len(runes) <= commentCut {
		return body
	}
	cut := string(runes[:commentCut])
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ") + "…"
}

// commentHref is the comment's document page, at its Comments card.
func commentHref(c modkit.Comment) string {
	return "/doc/" + c.DocType + "/" + strconv.FormatInt(c.RecordID, 10) + "#comments"
}

// homeView is the Purchase home (UI v2 §5.12): the non-zero cards, the
// approvals waiting for p (the first homeWaitingSize and their total), p's
// rejected drafts (changes) and orders to send, p's recent documents, the
// quick actions, whether p reads orders (the Recent link), the recent
// comments, and hidden, the registered Hide as the list asks it (nil hides
// nothing).
type homeView struct {
	cards           []purchase.Card
	waiting         []approval.InboxItem
	waitingTotal    int
	changes, toSend []purchase.HomeRow
	recent          []purchase.HomeRow
	quick           []components.QuickActionProps
	readOrders      bool
	comments        []modkit.Comment
	hidden          func(dt, field string) bool
}

// homeWaitingSize is how many waiting rows the home shows (UI v2 §5.12).
const homeWaitingSize = 3

// cardIcons are each home card's icon, by Card.Key.
var cardIcons = map[string]string{
	purchase.CardToApprove:         "check-check",
	purchase.QueueDrafts:           "pencil-line",
	purchase.QueueToSend:           "send",
	purchase.QueueToClose:          "circle-stop",
	purchase.QueueRequestsToOrder:  "clipboard-list",
	purchase.QueueWaiting:          "hourglass",
	purchase.QueueMyRequests:       "file-text",
	purchase.QueueAwaitingDelivery: "truck",
	purchase.CardOverdue:           "calendar-clock",
}

// homeProps is v as the shared module home: To approve is the hot card.
func homeProps(v homeView) layout.HomeProps {
	hp := layout.HomeProps{Title: homeTitle, Quick: v.quick}
	for _, c := range v.cards {
		hp.Counts = append(hp.Counts, components.StatCardProps{
			Count: c.Count, Label: cardTitles[c.Key], Detail: cardLine(c), Icon: cardIcons[c.Key], Href: c.Href, Hot: c.Key == purchase.CardToApprove,
		})
	}
	hide := v.hidden
	if hide == nil {
		hide = func(string, string) bool { return false }
	}
	cols := []components.Column{queueTitleCol, queueTotalCol, queueMetaCols[0], queueStateCol}
	if hide(purchase.DocType, "state") || hide(purchase.RequestDocType, "state") {
		cols = cols[:3] // the list leaves a hidden state's column out (CD-368 (3))
	}
	hp.Waiting = components.DataListProps{Label: waitingLabel, Columns: cols}
	for _, it := range v.waiting {
		why := waitingWhy
		if it.Why != "" {
			why += metaSep + it.Why
		}
		r := purchase.HomeRow{DocType: it.DocType, ID: it.DocID, Title: it.Title, Party: it.Party, Total: it.Amount, State: doctype.StatePending}
		hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(r, why, hide))
	}
	for _, r := range v.changes {
		meta := metaChanges
		if r.Reason != "" {
			meta += metaSep + `"` + r.Reason + `"`
		}
		hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(r, meta, hide))
	}
	for _, r := range v.toSend {
		hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(r, metaNotSent, hide))
	}
	hp.Waiting.Rows = hp.Waiting.Rows[:min(len(hp.Waiting.Rows), homeWaitingSize)]
	if v.waitingTotal > 0 {
		hp.WaitingAll = &components.Link{Label: fmt.Sprintf(seeAllLabel, v.waitingTotal), Href: "/approvals?type=" + purchase.DocType}
	}
	hp.Recent = components.DataListProps{Label: recentDocsLabel, Columns: cols}
	for _, r := range v.recent {
		hp.Recent.Rows = append(hp.Recent.Rows, homeRow(r, "", hide))
	}
	if v.readOrders {
		hp.RecentAll = &components.Link{Label: allOrdersLabel, Href: "/doc/" + purchase.DocType}
	}
	if len(v.comments) > 0 {
		hp.Side = recentComments(v.comments)
	}
	return hp
}

// homeRow is a Recent or Waiting row (approvals included) for r with meta, leaving out each
// value whose field the registered Hide hides (CD-368 (3)): the total and
// the party; homeProps drops the state column when state is hidden.
func homeRow(r purchase.HomeRow, meta string, hide func(dt, field string) bool) components.Row {
	row := components.Row{Href: docHref(r.DocType, r.ID), Values: map[string]string{"title": r.Title, "m0": meta}}
	if r.DocType == purchase.DocType && !hide(r.DocType, "rounded_total") {
		row.Values["total"] = types.FormatINR(r.Total)
	}
	d, _ := doctype.Lookup(r.DocType)
	if f, ok := doc.PartyField(d); !ok || !hide(r.DocType, f.Name) {
		row.Party = r.Party
	}
	row.State, row.StateLabel = r.State, stateLabel(r.DocType, r.State)
	return row
}

// docHref is a document's page.
func docHref(dt string, id int64) string { return fmt.Sprintf("/doc/%s/%d", dt, id) }

// stateLabel is purchase DocType dt's word for st.
func stateLabel(dt string, st doctype.State) string {
	d, _ := doctype.Lookup(dt)
	return d.StateLabels[st]
}

// can reports whether p holds right on dt (Service.Can); AUTH's refusal is
// false, any other error is returned.
func (h *handlers) can(ctx context.Context, p auth.Principal, dt string, right auth.Right) (bool, error) {
	err := h.d.Service.Can(ctx, p, dt, right)
	if errors.Is(err, auth.ErrForbidden) {
		return false, nil
	}
	return err == nil, err
}

// newButtons is a queue page's bar (UX §5.2, CD-258 Q5): `New purchase order`
// as primary with create on Order, then `New purchase request` (primary
// when it is alone) with create on Request.
func (h *handlers) newButtons(ctx context.Context, p auth.Principal, order, request bool) (components.ActionBarProps, error) {
	var bar components.ActionBarProps
	if order {
		ok, err := h.can(ctx, p, purchase.DocType, auth.RightCreate)
		if err != nil {
			return bar, err
		}
		if ok {
			bar.Primary = &components.ButtonProps{Label: newOrderLabel, Href: "/doc/" + purchase.DocType + "/new", Variant: components.Primary}
		}
	}
	if request {
		ok, err := h.can(ctx, p, purchase.RequestDocType, auth.RightCreate)
		if err != nil {
			return bar, err
		}
		b := &components.ButtonProps{Label: newReqLabel, Href: "/doc/" + purchase.RequestDocType + "/new", Variant: components.Primary}
		switch {
		case !ok:
		case bar.Primary == nil:
			bar.Primary = b
		default:
			b.Variant = components.Secondary
			bar.Secondary = b
		}
	}
	return bar, nil
}

// hidden is the registered Hide for r's viewer as the list asks it
// (Listing, no View, Deps.ListHidden).
func (h *handlers) hidden(r *http.Request) func(dt, field string) bool {
	return func(dt, field string) bool {
		d, _ := doctype.Lookup(dt)
		return h.d.ListHidden(r, d)(field)
	}
}

// quickActions are the home's quick actions p's rights allow (UI v2 §5.12):
// New purchase order with create on Order, New purchase request with create
// on Request, Pending items with read on Order.
func (h *handlers) quickActions(ctx context.Context, p auth.Principal, readOrders bool) ([]components.QuickActionProps, error) {
	var out []components.QuickActionProps
	for _, a := range []struct {
		dt   string
		item components.QuickActionProps
	}{
		{purchase.DocType, components.QuickActionProps{Icon: "shopping-cart", Label: newOrderLabel, Href: "/doc/" + purchase.DocType + "/new"}},
		{purchase.RequestDocType, components.QuickActionProps{Icon: "clipboard-list", Label: newReqLabel, Href: "/doc/" + purchase.RequestDocType + "/new"}},
	} {
		ok, err := h.can(ctx, p, a.dt, auth.RightCreate)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, a.item)
		}
	}
	if readOrders {
		out = append(out, components.QuickActionProps{Icon: "list-checks", Label: pendingTitle, Href: "/reports/purchase/pending"})
	}
	return out, nil
}

// home answers GET /modules/purchase (UI v2 §5.12, UX §5.1): the cards p
// has the right for whose count isn't 0, the purchase approvals waiting for
// p, p's recent documents p may still read, the quick actions and the
// recent comments. A user who reads neither purchase DocType gets AUTH's
// 403.
func (h *handlers) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	orderErr := h.d.Service.Can(ctx, p, purchase.DocType, auth.RightRead)
	if reqErr := h.d.Service.Can(ctx, p, purchase.RequestDocType, auth.RightRead); orderErr != nil && reqErr != nil {
		web.ErrorFrom(w, r, reqErr)
		return
	}
	readOrders := orderErr == nil
	hv, err := h.d.Service.Home(ctx, p)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := homeView{comments: hv.Comments, readOrders: readOrders}
	for _, c := range hv.Cards {
		if c.Count > 0 {
			v.cards = append(v.cards, c)
		}
	}
	inbox, _, err := h.d.Approvals.InboxOfType(ctx, p, purchase.DocType, types.Page{Number: 1, Size: homeWaitingSize})
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v.waiting, v.waitingTotal = inbox.Rows, inbox.Total
	if v.changes, v.toSend, err = h.d.Service.Waiting(ctx, p); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v.hidden = h.hidden(r)
	if v.recent, err = h.d.Service.Recent(ctx, p); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	if v.quick, err = h.quickActions(ctx, p, readOrders); err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, homePage(v), nil)
}

// queueSpec is a queue page's words (UX §5.2): its noun, empty text and
// which New button its bar has ("" for none).
type queueSpec struct {
	one, many, empty, newDocType string
}

var queueSpecs = map[string]queueSpec{
	purchase.QueueDrafts:           {"order", "orders", "You have no drafts.", purchase.DocType},
	purchase.QueueWaiting:          {"order", "orders", "Nothing of yours is waiting for approval.", purchase.DocType},
	purchase.QueueToSend:           {"order", "orders", "Every approved order has been sent.", purchase.DocType},
	purchase.QueueToClose:          {"order", "orders", "No orders are waiting to be closed.", ""},
	purchase.QueueAwaitingDelivery: {"order", "orders", "Nothing is waiting for delivery.", purchase.DocType},
	purchase.QueueRequestsToOrder:  {"request", "requests", "No requests are waiting to be ordered.", ""},
	purchase.QueueMyRequests:       {"request", "requests", "You have no open purchase requests.", purchase.RequestDocType},
}

// The queue DataList's columns: the title, the rounded total on the PO
// queues, up to three meta lines and the state.
var (
	queueTitleCol = components.Column{Key: "title", Label: "Number", Priority: components.PTitle, Code: true, NoWrap: true}
	queueTotalCol = components.Column{Key: "total", Label: "Total", Priority: components.PFigure, Num: true}
	queueMetaCols = []components.Column{
		{Key: "m0", Label: "Details", Priority: components.PMeta},
		{Key: "m1", Label: "More details", Priority: components.PMeta},
		{Key: "m2", Label: "Notes", Priority: components.PMeta},
	}
	queueStateCol = components.Column{Key: "state", Label: "State", Priority: components.PState}
)

// queueView is one queue page.
type queueView struct {
	title, count string
	list         components.DataListProps
	pages        components.PageInfo
	bar          components.ActionBarProps
}

// queue answers GET /modules/purchase/q/{queue} (UX §5.2): one page of the
// queue's rows; an unknown queue or one without its card's right is AUTH's
// 404 (the service's).
func (h *handlers) queue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	p, _ := auth.PrincipalFrom(ctx)
	name, page := chi.URLParam(r, "queue"), pageNumber(r)
	qv, err := h.d.Service.Queue(ctx, p, name, page)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	spec := queueSpecs[name]
	cols := []components.Column{queueTitleCol}
	if spec.one == "order" {
		cols = append(cols, queueTotalCol)
	}
	cols = append(append(cols, queueMetaCols...), queueStateCol)
	v := queueView{
		title: cardTitles[name],
		list:  components.DataListProps{Label: cardTitles[name], Columns: cols, Empty: components.EmptyState(spec.empty, nil)},
		pages: pageInfo(homePath+"/q/"+name+"?", spec.many, page, purchase.QueuePageSize, qv.Total),
	}
	if qv.Total > 0 {
		v.count = counted(qv.Total, spec.one, spec.many)
	}
	for _, row := range qv.Rows {
		values := map[string]string{"title": row.Title, "total": types.FormatINR(row.Total)}
		for i, m := range row.Meta {
			values["m"+strconv.Itoa(i)] = m
		}
		var label string
		if dt, ok := doctype.Lookup(row.DocType); ok {
			label = dt.StateLabels[row.State]
		}
		v.list.Rows = append(v.list.Rows, components.Row{
			Href: "/doc/" + row.DocType + "/" + strconv.FormatInt(row.ID, 10), State: row.State, StateLabel: label, Values: values,
		})
	}
	if spec.newDocType != "" {
		if v.bar, err = h.newButtons(ctx, p, spec.newDocType == purchase.DocType, spec.newDocType == purchase.RequestDocType); err != nil {
			web.ErrorFrom(w, r, err)
			return
		}
	}
	ui.Render(w, r, http.StatusOK, queuePage(v), nil)
}

// pageNumber is ?page=, 1 when missing or not a positive number.
func pageNumber(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || n < 1 {
		return 1
	}
	return n
}

// pageInfo builds the pager; list is the list's URL up to its page
// parameter, ending in "?" or "&".
func pageInfo(list, noun string, page, size, total int) components.PageInfo {
	pi := components.PageInfo{
		Page: page, From: (page-1)*size + 1, To: min(page*size, total), Total: total, Noun: noun,
		NextURL: list + "page=" + strconv.Itoa(page+1),
	}
	if page > 1 {
		pi.PrevURL = list + "page=" + strconv.Itoa(page-1)
	}
	return pi
}
