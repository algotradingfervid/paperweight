package purchaseweb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// P15b's Module functions: the Order document page and its close page (UX
// §5.9, §5.11).

// Order page and close-page texts (UX §5.9, §5.11; spec §6.4, §21.1, §23,
// §27.1; approved in CD-258).
const (
	pairPlace      = "Place of supply"
	pairReverse    = "Reverse charge"
	pairCopiedFrom = "Copied from"
	// noticeOwned is "Owned by {name}. Only {name} can change this draft.
	// Add a comment below, or ask {name} to hand it over." (spec §21.1).
	noticeOwned  = "Owned by %[1]s. Only %[1]s can change this draft. Add a comment below, or ask %[1]s to hand it over."
	copyLabel    = "Copy"
	sendLabel    = "Send to vendor"
	receiveLabel = "Receive goods"

	notePending       = "Pending %s"
	noteAlready       = "Already received %s before this app · Pending %s"
	notePendingClosed = "Pending %s (closed)"
	noteOverBy        = "Over by %s"

	sentTitle     = "Sent to vendor"
	notSentYet    = "Not sent yet"
	sentLine      = "Sent to vendor on %s by %s (%s)"
	expectedTitle = "Expected delivery"
	noExpected    = "No date from the vendor yet."
	expectedOn    = "Expected on %s"
	expectedSetBy = "Set by %s on %s"
	setDateLabel  = "Set date"
	changeLabel   = "Change"
	receiptsTitle = "Goods receipts"
	noReceipts    = "No goods receipts yet."
	receiptNoun   = "goods receipt"

	// closePendingOne and closePendingMany are spec §6.4's close note, with
	// {x} the pending quantities per unit (CD-258 Q12).
	closePendingOne  = "1 line still has %s pending. Closing means no more goods can be received against this order."
	closePendingMany = "%d lines still have %s pending. Closing means no more goods can be received against this order."
	orderCloseHint   = "Say why nothing more is coming. If you still need the rest, raise a new order after closing."
)

// sentWords are the Sent card's how words in the sentence (UX §5.9), so By
// hand reads "by hand".
var sentWords = map[string]string{"email": "email", "whatsapp": "WhatsApp", "courier": "courier", "by_hand": "hand", "other": "other means"}

// receiptColumns are the Goods receipts rows: the title (the receipt's
// number), its date and its state (UX §5.9).
var receiptColumns = []components.Column{
	{Key: "date", Label: "Date", Priority: components.PMeta, NoWrap: true},
	{Key: "state", Label: "State", Priority: components.PState},
}

// orderLine is one PO line's figures for its note and the close note.
type orderLine struct {
	no                              int
	unit, own                       string // the unit code; the line's own description unless it is the item's name
	qty, already, received, pending types.Qty
}

// orderLines reads each line of v's quantities from its input values.
func orderLines(v *docscreen.DocView) ([]orderLine, error) {
	out := make([]orderLine, 0, len(v.Lines))
	for _, l := range v.Lines {
		ol := orderLine{no: l.No, unit: unitOf(l)}
		if d := l.Display["description"]; d != l.Card.Item {
			ol.own = d
		}
		for name, q := range map[string]*types.Qty{"qty": &ol.qty, "already_received": &ol.already, "received_qty": &ol.received} {
			if s := l.Inputs[name]; s != "" {
				var err error
				if *q, err = types.ParseQty(s); err != nil {
					return nil, err
				}
			}
		}
		ol.pending = purchase.Pending(ol.qty, ol.already, ol.received)
		out = append(out, ol)
	}
	return out, nil
}

// note is line l's note on an approved or closed PO (UX §5.9): its own
// description, the pending figure and, when over-received, Over by (CD-277),
// joined with " · " (CD-330 (2)).
func (l orderLine) note(closed bool) string {
	var parts []string
	if l.own != "" {
		parts = append(parts, l.own)
	}
	switch {
	case closed:
		parts = append(parts, fmt.Sprintf(notePendingClosed, types.FormatQtyUoM(0, l.unit)))
	case l.already > 0:
		parts = append(parts, fmt.Sprintf(noteAlready, types.FormatQtyUoM(l.already, l.unit), types.FormatQtyUoM(l.pending, l.unit)))
	default:
		parts = append(parts, fmt.Sprintf(notePending, types.FormatQtyUoM(l.pending, l.unit)))
	}
	if over := l.already + l.received - l.qty; over > 0 {
		parts = append(parts, fmt.Sprintf(noteOverBy, types.FormatQtyUoM(over, l.unit)))
	}
	return strings.Join(parts, metaSep)
}

// orderRecords is what the page reads about a PO besides its view: the Sent
// and Expected on records, its place of supply and reverse-charge texts, and
// the documents linking it.
type orderRecords struct {
	sent           purchase.Sent
	isSent         bool
	expected       purchase.Expected
	isExpected     bool
	place, reverse string
	blockers       []doc.Blocker
}

func readOrderRecords(c docscreen.Ctx) (orderRecords, error) {
	var r orderRecords
	ctx, id := c.Req.Context(), c.View.Info.ID
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if r.sent, r.isSent, err = purchase.SentInfo(ctx, tx, id); err != nil {
			return err
		}
		if r.expected, r.isExpected, err = purchase.ExpectedInfo(ctx, tx, id); err != nil {
			return err
		}
		if r.place, err = purchase.PlaceReason(ctx, tx, id); err != nil {
			return err
		}
		if r.reverse, err = purchase.ReverseChargeNote(ctx, tx, id); err != nil {
			return err
		}
		r.blockers, err = doc.LinkedBlockers(ctx, tx, purchase.DocType, id)
		return err
	})
	return r, err
}

// orderPage is the Order's document page additions (UX §5.9): the pairs,
// the owner notice, line notes, the Owner, Sent to vendor, Expected
// delivery, Goods receipts and Comments cards, More actions and the primary.
func (m *module) orderPage(c docscreen.Ctx) (docscreen.PageExtra, error) {
	v := c.View
	id, state := v.Info.ID, v.Info.State
	owner, err := ownerName(c, v.Info.CreatedBy)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	rec, err := readOrderRecords(c)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	var x docscreen.PageExtra
	if x.Pairs, err = orderPairs(c, owner, rec); err != nil {
		return docscreen.PageExtra{}, err
	}
	isOwner := c.Principal.ID == v.Info.CreatedBy
	canApprove, err := allowed(c, purchase.DocType, auth.RightApprove, &v.Record)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	draftLike := state == doctype.StateDraft || state == doctype.StateRejected
	handover := orderPage(id, "/handover")
	if draftLike && !isOwner {
		x.Notice = &components.BannerProps{Kind: components.Info, Message: fmt.Sprintf(noticeOwned, owner)}
		if canApprove {
			x.Notice.Link = &components.Link{Label: handOverLabel, Href: handover}
		}
	}
	lines, err := orderLines(v)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	live := state == doctype.StateApproved || state == doctype.StateClosed
	if live {
		x.LineNotes = map[int]string{}
		for _, l := range lines {
			x.LineNotes[l.no] = l.note(state == doctype.StateClosed)
		}
	}
	canWrite, err := allowed(c, purchase.DocType, auth.RightWrite, &v.Record)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	canSend := canWrite || canApprove // MarkSent's right: owner or approve (spec §23)
	if state == doctype.StateRejected {
		if oc := ownerCard(c, id); oc != nil {
			x.Cards = append(x.Cards, oc)
		}
	}
	if live || (state == doctype.StateCancelled && rec.isSent) {
		card, err := sentCard(c, rec, canSend && state == doctype.StateApproved)
		if err != nil {
			return docscreen.PageExtra{}, err
		}
		x.Cards = append(x.Cards, card)
	}
	if state == doctype.StateApproved || (rec.isExpected && (state == doctype.StateClosed || state == doctype.StateCancelled)) {
		canAmend, err := allowed(c, purchase.DocType, auth.RightAmend, &v.Record)
		if err != nil {
			return docscreen.PageExtra{}, err
		}
		card, err := expectedCard(c, rec, (canAmend || canApprove) && state == doctype.StateApproved)
		if err != nil {
			return docscreen.PageExtra{}, err
		}
		x.Cards = append(x.Cards, card)
	}
	// A cancelled PO has no live receipt: cancel is refused while one links
	// it (D-DOC-05), so the card is on approved and closed POs only.
	if live {
		card, err := receiptsCard(c, rec.blockers)
		if err != nil {
			return docscreen.PageExtra{}, err
		}
		x.Cards = append(x.Cards, card)
	}
	comments, err := m.commentsCard(c, purchase.DocType, id)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	x.Cards = append(x.Cards, comments)

	if state == doctype.StateApproved && !rec.isSent && canSend {
		x.Primary = &components.ButtonProps{Label: sendLabel, Href: orderPage(id, "/send")}
	}
	if state == doctype.StateApproved && m.d.ReceiveLink != nil {
		if href, ok := m.d.ReceiveLink(c.Req.Context(), c.Principal, id); ok {
			if x.Primary == nil && !fullyReceived(lines) {
				x.Primary = &components.ButtonProps{Label: receiveLabel, Href: href}
			} else {
				x.More = append(x.More, components.MoreAction{Label: receiveLabel, Href: href})
			}
		}
	}
	canCreate, err := allowed(c, purchase.DocType, auth.RightCreate, nil)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	if canCreate {
		x.More = append(x.More, components.MoreAction{Label: copyLabel, Href: orderPage(id, "/copy"), Post: true})
	}
	if draftLike && (isOwner || canApprove) {
		x.More = append(x.More, components.MoreAction{Label: handOverLabel, Href: handover})
	}
	return x, nil
}

// fullyReceived reports whether no line has anything pending.
func fullyReceived(lines []orderLine) bool {
	for _, l := range lines {
		if l.pending > 0 {
			return false
		}
	}
	return true
}

// orderPairs are Details' additions (UX §5.9): Raised by, Place of supply,
// Reverse charge when ticked, and Copied from when the viewer may read the
// source (CD-248 family, CD-312).
func orderPairs(c docscreen.Ctx, owner string, rec orderRecords) ([]components.Pair, error) {
	pairs := []components.Pair{{Label: pairRaisedBy, Value: owner}}
	if rec.place != "" {
		pairs = append(pairs, components.Pair{Label: pairPlace, Value: rec.place})
	}
	if rec.reverse != "" {
		pairs = append(pairs, components.Pair{Label: pairReverse, Value: rec.reverse})
	}
	if s := c.View.Inputs["copied_from_id"]; s != "" {
		src, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		label, ok, err := readableLabel(c, purchase.DocType, src)
		if err != nil {
			return nil, err
		}
		if ok {
			pairs = append(pairs, components.Pair{Label: pairCopiedFrom, Value: label})
		}
	}
	return pairs, nil
}

// sentCard is the Sent to vendor card (UX §5.9, spec §23): Not sent yet, or
// the sentence and the note, with Change to the Send page when change is set
// and it was sent.
func sentCard(c docscreen.Ctx, rec orderRecords, change bool) (templ.Component, error) {
	if !rec.isSent {
		return recordView(sentTitle, []string{notSentYet}, nil), nil
	}
	by, err := ownerName(c, rec.sent.SentBy)
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf(sentLine, types.FormatDate(rec.sent.SentOn), sentWords[rec.sent.SentVia], by)}
	if rec.sent.Note != "" {
		lines = append(lines, rec.sent.Note)
	}
	var link *components.Link
	if change {
		link = &components.Link{Label: changeLabel, Href: orderPage(c.View.Info.ID, "/send")}
	}
	return recordView(sentTitle, lines, link), nil
}

// expectedCard is the Expected delivery card (UX §5.9, §5.20, spec §27.1):
// No date from the vendor yet., or the date, the note and who set it on
// which IST day, with Set date or Change to the Expected on page when link
// is set.
func expectedCard(c docscreen.Ctx, rec orderRecords, link bool) (templ.Component, error) {
	lines, label := []string{noExpected}, setDateLabel
	if rec.isExpected {
		by, err := ownerName(c, rec.expected.SetBy)
		if err != nil {
			return nil, err
		}
		lines, label = []string{fmt.Sprintf(expectedOn, types.FormatDate(rec.expected.ExpectedOn))}, changeLabel
		if rec.expected.Note != "" {
			lines = append(lines, rec.expected.Note)
		}
		lines = append(lines, fmt.Sprintf(expectedSetBy, by, types.FormatDate(types.BusinessDate(rec.expected.SetAt))))
	}
	var change *components.Link
	if link {
		change = &components.Link{Label: label, Href: orderPage(c.View.Info.ID, "/expected")}
	}
	return recordView(expectedTitle, lines, change), nil
}

// receiptsCard is the Goods receipts card (UX §5.9): a row per linked
// document the viewer may read (CD-312), dated by its DocType's Date hook
// after that check, then one line counting the others with no state
// (CD-248, CD-280 D6).
func receiptsCard(c docscreen.Ctx, bs []doc.Blocker) (templ.Component, error) {
	ctx := c.Req.Context()
	var rows []components.Row
	hidden := 0
	for _, b := range bs {
		label, ok, err := readableLabel(c, b.DocType, b.ID)
		if err != nil {
			return nil, err
		}
		if !ok {
			hidden++
			continue
		}
		dt, _ := doctype.Lookup(b.DocType) // LinkedBlockers reads registered DocTypes only
		var on types.Date
		if err := c.Deps.DB.Read(ctx, func(tx *db.Tx) error {
			var err error
			on, err = dt.Hooks.Date(ctx, tx, b.ID)
			return err
		}); err != nil {
			return nil, err
		}
		rows = append(rows, components.Row{
			Href: "/doc/" + b.DocType + "/" + strconv.FormatInt(b.ID, 10), Title: label,
			Values: map[string]string{"date": types.FormatDate(on)}, State: b.State,
		})
	}
	return receiptsView(rows, otherCount(hidden, receiptNoun)), nil
}

// orderReason adds the pending warning and the hint to the close page (UX
// §5.11, spec §6.4): the lines with anything pending and the pending
// quantities per unit, in the order the units first appear (CD-258 Q12).
// The note is docscreen's info banner; CD-330 (3) moves it to
// ReasonExtra.NoteWarn once M0-53 is on this branch.
func (m *module) orderReason(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
	if a != doctype.ActionClose {
		return docscreen.ReasonExtra{}, nil
	}
	x := docscreen.ReasonExtra{Hint: orderCloseHint}
	lines, err := orderLines(c.View)
	if err != nil {
		return docscreen.ReasonExtra{}, err
	}
	var units []string
	sum := map[string]types.Qty{}
	n := 0
	for _, l := range lines {
		if l.pending <= 0 {
			continue
		}
		n++
		if _, ok := sum[l.unit]; !ok {
			units = append(units, l.unit)
		}
		sum[l.unit] += l.pending
	}
	if n == 0 {
		return x, nil
	}
	parts := make([]string, len(units))
	for i, u := range units {
		parts[i] = types.FormatQtyUoM(sum[u], u)
	}
	if n == 1 {
		x.Note = fmt.Sprintf(closePendingOne, joinAnd(parts))
	} else {
		x.Note = fmt.Sprintf(closePendingMany, n, joinAnd(parts))
	}
	return x, nil
}
