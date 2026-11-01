package grnweb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3a: the check page, the document page and the cancel reason (UX doc
// §5.5, §5.6, §5.8). Everything taken from the PO is left out for a viewer
// who can't read it (goodsreceipt.ReceiptPage; CD-233, CD-248, CD-340).

// S3a copy (UX doc §5.6, §5.8 copy tables).
const (
	labelProject       = "Project"
	labelOpen          = "Open"
	labelReceiptStatus = "Receipt status"
	labelStillPending  = "Still pending"
	pendingNothing     = "Nothing"
	pendingOf          = "%d of %d lines"
	noteCancelClosed   = "%s is closed. After this cancel, only an amendment of this receipt can receive against it again."
	noteCancelBill     = "This receipt has bill details (%s). Correct your accounts too."
	hintCancel         = "Say what was wrong. Stores can then amend the receipt to correct it."
)

// receiptPage is the Service's ReceiptPage of c's GRN for c's viewer.
func (m *module) receiptPage(c docscreen.Ctx) (goodsreceipt.ReceiptPage, error) {
	return m.d.Service.Page(c.Req.Context(), c.Principal, c.View.Info.ID)
}

// check adds G5's notes and warnings, then G11's, to the check page (UX doc
// §5.5). The big figure is docscreen's grand figure, accepted_value.
func (m *module) check(c docscreen.Ctx) (docscreen.CheckExtra, error) {
	rp, err := m.receiptPage(c)
	if err != nil {
		return docscreen.CheckExtra{}, err
	}
	return docscreen.CheckExtra{Notes: rp.Notes, Warnings: withChallan(c, rp)}, nil
}

// page adds to the document page (UX doc §5.6): Site and Project from the
// GRN's snapshot, G5's and G11's warnings and the e-way hint while it isn't
// cancelled, then the Bill details card (S3b) and the Purchase order card.
// docscreen shows no Close action (CloseNone, CD-277 (1)).
func (m *module) page(c docscreen.Ctx) (docscreen.PageExtra, error) {
	rp, err := m.receiptPage(c)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	x := docscreen.PageExtra{Pairs: []components.Pair{{Label: labelSite, Value: rp.Site}, {Label: labelProject, Value: rp.Project}}}
	if c.View.Info.State != doctype.StateCancelled {
		x.Warnings = withChallan(c, rp)
		if rp.EWay != "" {
			x.Notice = &components.BannerProps{Kind: components.Info, Message: rp.EWay}
		}
	}
	bill, err := m.billPanel(c)
	if err != nil {
		return docscreen.PageExtra{}, err
	}
	if bill != nil {
		x.Cards = append(x.Cards, bill)
	}
	if rp.PO != nil {
		x.Cards = append(x.Cards, poCard(poRows(rp.PO)))
	}
	return x, nil
}

// withChallan is G5's warnings, then G11's duplicate challan, if any,
// unless Hide hides challan_no: the text shows its value (CD-319).
func withChallan(c docscreen.Ctx, rp goodsreceipt.ReceiptPage) []string {
	if rp.Challan == "" || !shown(c, "challan_no") {
		return rp.Warnings
	}
	return append(rp.Warnings, rp.Challan)
}

// poRows are the Purchase order card's link and lines.
func poRows(po *goodsreceipt.ReceiptPO) (components.Link, string, []components.Pair) {
	pending := pendingNothing
	if po.Pending > 0 {
		pending = fmt.Sprintf(pendingOf, po.Pending, po.Lines)
	}
	open := components.Link{Label: labelOpen, Href: "/doc/" + purchase.DocType + "/" + strconv.FormatInt(po.ID, 10)}
	return open, po.Number + " · " + po.OrderDate, []components.Pair{{Label: labelReceiptStatus, Value: po.ReceiptStatus}, {Label: labelStillPending, Value: pending}}
}

// reason adds to the cancel page (UX doc §5.8, spec §5): one warning,
// the closed-PO sentence then the bill sentence, each on its condition,
// and the hint. A canceller who can't read the PO gets no closed-PO
// sentence, as it would name the PO and its state (CD-233, CD-248).
func (m *module) reason(c docscreen.Ctx, a doctype.Action) (docscreen.ReasonExtra, error) {
	if a != doctype.ActionCancel {
		return docscreen.ReasonExtra{}, nil
	}
	rp, err := m.receiptPage(c)
	if err != nil {
		return docscreen.ReasonExtra{}, err
	}
	var note []string
	if rp.PO != nil && rp.PO.Closed {
		note = append(note, fmt.Sprintf(noteCancelClosed, rp.PO.Number))
	}
	if rp.Invoice != "" {
		note = append(note, fmt.Sprintf(noteCancelBill, rp.Invoice))
	}
	return docscreen.ReasonExtra{Note: strings.Join(note, " "), NoteWarn: true, Hint: hintCancel}, nil
}
