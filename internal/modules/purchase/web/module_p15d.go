package purchaseweb

import (
	"cmp"
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15d's Module functions: the Request screens (UX §5.14 to §5.18).

// Request page and close-page texts (UX §5.16, §5.17; spec §20.3, §20.7).
const (
	createPOLabel     = "Create PO"
	linkedOrdersTitle = "Linked orders"
	noLinkedOrders    = "Not on any order yet."
	// lineNoteFormat is "Requested {q} · Ordered {o} · Pending to order {p}",
	// each "{qty} {unit}".
	lineNoteFormat = "Requested %s · Ordered %s · Pending to order %s"
	// noticeDraft is "{name} is still writing this request. Only {name} can
	// change or submit it. Add a comment below."
	noticeDraft = "%[1]s is still writing this request. Only %[1]s can change or submit it. Add a comment below."
	// noticeCantClose is "Raised by {name}. Only {name} or a Purchase
	// Manager can close this request."
	noticeCantClose = "Raised by %[1]s. Only %[1]s or a Purchase Manager can close this request."
	// noticeCanClose is "Raised by {name}. You or {name} can close this
	// request."
	noticeCanClose = "Raised by %[1]s. You or %[1]s can close this request."
	// closeHeldOne is "1 row is on an order not yet approved: {list}. That
	// order can't be approved until the row is removed."
	closeHeldOne = "1 row is on an order not yet approved: %s. That order can't be approved until the row is removed."
	// closeHeldMany is "{n} rows are on orders not yet approved: {list}.
	// Those orders can't be approved until the rows are removed."
	closeHeldMany  = "%d rows are on orders not yet approved: %s. Those orders can't be approved until the rows are removed."
	closeHint      = "Say why it's no longer needed."
	unreadableOne  = "another order" // the held note reads On another order
	unreadableNoun = "order"
)

// receiptLabels are a linked order's receipt status words (UX §5.16).
var receiptLabels = map[string]string{"not_received": "Not received", "part_received": "Part received", "fully_received": "Fully received"}

// requestCardsOnly: a request's Items step is cards at every width (CD-315).
const requestCardsOnly = true

// requestHide hides Request date only as an input, on a new request or a
// draft: Validate fills today and submit sets it again (CD-258 Q8 (a),
// CD-327, CD-330 (1)). A nil View is a new record. When Ctx gains Listing
// (M0-47), the condition becomes `!c.Listing && (...)`.
func (m *module) requestHide(c docscreen.Ctx, f doctype.Field) bool {
	return f.Name == "request_date" && (c.View == nil || c.View.Info.State == doctype.StateDraft)
}

// requestLineCard is a request line's card: the quantity with its unit and
// the line's note, with no rate (CD-258 G8).
func (m *module) requestLineCard(_ docscreen.Ctx, l docscreen.LineView) components.LineCard {
	card := l.Card
	card.QtyRate = strings.TrimSpace(l.Display["qty"] + " " + unitOf(l))
	card.Note = l.Display["note"]
	return card
}

// requestCheck adds the Comments card to the check page (G2).
func (m *module) requestCheck(c docscreen.Ctx) (docscreen.CheckExtra, error) {
	card, err := m.commentsCard(c, purchase.RequestDocType, c.View.Info.ID)
	if err != nil {
		return docscreen.CheckExtra{}, err
	}
	return docscreen.CheckExtra{Cards: []templ.Component{card}}, nil
}

// requestRows is what the page and the close page read about a request's
// rows: each row's pending to order, the PO lines holding them and the
// linked orders.
type requestRows struct {
	pending map[int64]types.Qty
	holders []purchase.Holder
	linked  []purchase.LinkedOrder
}

func readRequestRows(c docscreen.Ctx) (requestRows, error) {
	var rr requestRows
	ctx := c.Req.Context()
	ids := make([]int64, 0, len(c.View.Lines))
	for _, l := range c.View.Lines {
		id, err := strconv.ParseInt(l.ID, 10, 64)
		if err != nil {
			return rr, err
		}
		ids = append(ids, id)
	}
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if rr.pending, err = purchase.PendingToOrder(ctx, tx, ids); err != nil {
			return err
		}
		if rr.holders, err = purchase.HeldBy(ctx, tx, c.View.Info.ID); err != nil {
			return err
		}
		rr.linked, err = purchase.LinkedOrders(ctx, tx, c.View.Info.ID)
		return err
	})
	return rr, err
}

// unapproved reports whether a holding PO in state s still waits on
// approval: its rows are held, not ordered (spec §20.2).
func unapproved(s doctype.State) bool {
	return s == doctype.StateDraft || s == doctype.StatePending || s == doctype.StateRejected
}

// holderNames is each readable PO's "{display} ({owner})" by PO id; a PO
// the viewer can't read has no entry (CD-312, CD-248 family).
type holderNames map[int64]string

// namesOf reads the label of each distinct PO in hs the viewer may read.
func namesOf(c docscreen.Ctx, hs []purchase.Holder) (holderNames, error) {
	names := holderNames{}
	seen := map[int64]bool{}
	for _, h := range hs {
		if seen[h.POID] {
			continue
		}
		seen[h.POID] = true
		label, ok, err := readableLabel(c, purchase.DocType, h.POID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		owner, err := ownerName(c, h.Owner)
		if err != nil {
			return nil, err
		}
		names[h.POID] = label + " (" + owner + ")"
	}
	return names, nil
}

// heldList is the POs holding hs, in order: each one the viewer may read as
// named, and how many others there are (UX §5.17, CD-280 D6).
func heldList(hs []purchase.Holder, names holderNames) (named []string, hidden int) {
	seen := map[int64]bool{}
	for _, h := range hs {
		if seen[h.POID] {
			continue
		}
		seen[h.POID] = true
		if n, ok := names[h.POID]; ok {
			named = append(named, n)
		} else {
			hidden++
		}
	}
	return named, hidden
}

// hiddenOrders is k unreadable orders in words: another order, or {k} other
// orders.
func hiddenOrders(k int) string {
	if k == 1 {
		return unreadableOne
	}
	return otherCount(k, unreadableNoun)
}

// requestPage is the Request's document page additions (UX §5.16): Raised
// by, the owner notice, each line's quantities and holders, the Linked
// orders and Comments cards, and Create PO.
func (m *module) requestPage(c docscreen.Ctx) (docscreen.PageExtra, error) {
	v := c.View
	requester, err := ownerName(c, v.Info.CreatedBy)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	x := docscreen.PageExtra{Pairs: []components.Pair{{Label: pairRaisedBy, Value: requester}}}
	if x.Notice, err = requestNotice(c, requester); err != nil {
		return docscreen.PageExtra{}, err
	}
	rr, err := readRequestRows(c)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	var held []purchase.Holder
	for _, h := range rr.holders {
		if unapproved(h.State) {
			held = append(held, h)
		}
	}
	names, err := namesOf(c, held)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	x.LineNotes = map[int]string{}
	anyPending := false
	for _, l := range v.Lines {
		id, _ := strconv.ParseInt(l.ID, 10, 64) // parsed by readRequestRows
		p := rr.pending[id]
		anyPending = anyPending || p > 0
		unit := unitOf(l)
		note := fmt.Sprintf(lineNoteFormat, l.Display["qty"]+" "+unit, cmp.Or(l.Display["ordered_qty"], "0")+" "+unit, types.FormatQtyUoM(p, unit))
		var mine []purchase.Holder
		for _, h := range held {
			if h.RequestLineID == id {
				mine = append(mine, h)
			}
		}
		named, hidden := heldList(mine, names)
		for _, n := range named {
			note += " · On " + n
		}
		if hidden > 0 {
			note += " · On " + hiddenOrders(hidden)
		}
		if own := l.Display["note"]; own != "" {
			note = own + " · " + note // the line's own note first (CD-330 (2))
		}
		x.LineNotes[l.No] = note
	}
	card, err := linkedOrdersCard(c, rr.linked)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	comments, err := m.commentsCard(c, purchase.RequestDocType, v.Info.ID)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	x.Cards = []templ.Component{card, comments}
	canCreate, err := allowed(c, purchase.DocType, auth.RightCreate, nil)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	if createPOShown(v.Info.State, canCreate, anyPending) {
		x.Primary = &components.ButtonProps{Label: createPOLabel, Href: fromRequestPath(v.Info.ID)}
	}
	return x, nil
}

// createPOShown is the Create PO primary's guard (UX §5.16, G5): a submitted
// request, a viewer with create on Purchase Order, and a row with pending to
// order above 0.
func createPOShown(s doctype.State, canCreate, pending bool) bool {
	return s == doctype.StateApproved && canCreate && pending
}

// fromRequestPath is request id's Create PO page (UX §5.18).
func fromRequestPath(id int64) string {
	return "/modules/purchase/requests/" + strconv.FormatInt(id, 10) + "/order"
}

// requestNotice is the owner notice (UX §5.16, G3): for anyone but the
// requester, on a draft or a submitted request; none on closed or cancelled.
func requestNotice(c docscreen.Ctx, requester string) (*components.BannerProps, error) {
	v := c.View
	if c.Principal.ID == v.Info.CreatedBy {
		return nil, nil
	}
	var format string
	switch v.Info.State {
	case doctype.StateDraft:
		format = noticeDraft
	case doctype.StateApproved:
		canClose, err := allowed(c, purchase.RequestDocType, auth.Right(c.DT.CloseRight), &v.Record)
		if err != nil {
			return nil, err
		}
		format = noticeCantClose
		if canClose {
			format = noticeCanClose
		}
	default:
		return nil, nil
	}
	return &components.BannerProps{Kind: components.Info, Message: fmt.Sprintf(format, requester)}, nil
}

// linkedOrdersCard is the Linked orders card: a row per order the viewer may
// read, then one line counting the others, with no state (CD-248, CD-280
// D6); with none, Not on any order yet.
func linkedOrdersCard(c docscreen.Ctx, orders []purchase.LinkedOrder) (templ.Component, error) {
	var rows []components.Row
	hidden := 0
	for _, o := range orders {
		label, ok, err := readableLabel(c, purchase.DocType, o.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			hidden++
			continue
		}
		row := components.Row{
			Href: "/doc/" + purchase.DocType + "/" + strconv.FormatInt(o.ID, 10), Title: label,
			Values: map[string]string{"vendor": o.VendorName}, State: o.State,
		}
		if o.State == doctype.StateApproved || o.State == doctype.StateClosed {
			row.Values["receipt"] = receiptLabels[o.ReceiptStatus]
		}
		rows = append(rows, row)
	}
	return linkedRowsView(linkedOrdersTitle, noLinkedOrders, linkedOrderColumns, rows, otherCount(hidden, unreadableNoun)), nil
}

// requestReason adds the held-rows warning and the hint to the close page
// (UX §5.17, PO-069): rows held by orders not yet approved, each order named
// with its owner when the viewer may read it.
func (m *module) requestReason(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
	if a != doctype.ActionClose {
		return docscreen.ReasonExtra{}, nil
	}
	x := docscreen.ReasonExtra{Hint: closeHint}
	var hs []purchase.Holder
	err := c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) error {
		var err error
		hs, err = purchase.HeldBy(c.Req.Context(), tx, c.View.Info.ID)
		return err
	})
	if err != nil {
		return docscreen.ReasonExtra{}, err
	}
	var held []purchase.Holder
	rows := map[int64]bool{}
	for _, h := range hs {
		if unapproved(h.State) {
			held = append(held, h)
			rows[h.RequestLineID] = true
		}
	}
	if len(rows) == 0 {
		return x, nil
	}
	names, err := namesOf(c, held)
	if err != nil {
		return docscreen.ReasonExtra{}, err
	}
	named, hidden := heldList(held, names)
	if hidden > 0 {
		named = append(named, hiddenOrders(hidden))
	}
	list := joinAnd(named)
	if len(rows) == 1 {
		x.Note = fmt.Sprintf(closeHeldOne, list)
	} else {
		x.Note = fmt.Sprintf(closeHeldMany, len(rows), list)
	}
	return x, nil
}
