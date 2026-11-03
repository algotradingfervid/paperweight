package grnweb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S2: Quantities (UX doc §5.4). One card per PO line, keyed by its
// PO line number; a card with a quantity above 0 is a posted line, and its
// inputs carry docscreen.LineFieldID of its posted position, the Line the
// engine's errors give (CD-286 R1). Save sends the posted lines in PO line
// order, so the Body numbers positions the same way.

// Step 3 copy (UX doc §5.4 copy table).
const (
	labelReceived     = "Received now"
	labelRejected     = "Rejected"
	labelReason       = "Reason for rejecting"
	hintReason        = "For example, Bent pieces or Wrong size. Printed on the GRN."
	labelSomeRejected = "Some rejected"
	labelFillAll      = "Receive all pending"
	busyFillAll       = "Filling…"
	labelClearAll     = "Clear all"
	busyClearAll      = "Clearing…"
)

// The fill query values of the two Body buttons (UX doc §5.4).
const (
	fillAll   = "all"
	fillClear = "clear"
)

// itemsStep is step 3: the cards and their save.
func (m *module) itemsStep() docscreen.Step {
	return docscreen.Step{Body: m.itemsBody, Save: m.itemsSave, Bar: itemsBar}
}

// itemsBar puts {n} of {m} lines entered in the footer (CD-387 (7)): m is the
// lines still pending and n those of them Save would post (CD-391), as the
// page's own script counts while typing (counter.js, the inputs marked
// data-line-entry). A viewer who can't see Received now gets no count (CD-319),
// nor does a PO with nothing pending.
func itemsBar(c docscreen.Ctx, s docscreen.StepState) (docscreen.StepBar, error) {
	if !shown(c, "received_qty") {
		return docscreen.StepBar{}, nil
	}
	lines, err := poLines(c)
	if err != nil {
		return docscreen.StepBar{}, err
	}
	es, n, m := entries(c, lines, s.Posted), 0, 0
	for _, l := range lines {
		if l.Pending <= 0 {
			continue
		}
		m++
		if nonZero(es[l.ID].received) {
			n++
		}
	}
	if m == 0 {
		return docscreen.StepBar{}, nil // nothing pending: no count (CD-391)
	}
	return docscreen.StepBar{Summary: linesEntered(n, m)}, nil
}

// noLinePages: the lines come only from step 3's cards (CD-256 (7)).
func (m *module) noLinePages() bool { return true }

// entry is one card's values as its inputs show them, the stored line's id
// when it has one, and its posted position (0: not a posted line).
type entry struct {
	received, rejected, reason string
	id                         string
	pos                        int
}

// lineInputs are the card fields, each drawn and posted only when Hide
// shows it (CD-319).
var lineInputs = []string{"received_qty", "rejected_qty", "reject_reason"}

// shownInputs reports, by field name, whether Hide shows each card field.
func shownInputs(c docscreen.Ctx) map[string]bool {
	show := map[string]bool{}
	for _, name := range lineInputs {
		show[name] = shown(c, name)
	}
	return show
}

// entries are the cards' values by PO line id: posted (with the fill the
// pressed button asked for) when posted, else the stored lines'; a field
// Hide hides keeps its stored value. A posted card's position counts the
// cards with a quantity in PO line order; a stored line's is its line number.
func entries(c docscreen.Ctx, lines []goodsreceipt.POLine, posted bool) map[int64]entry {
	out := map[int64]entry{}
	for _, lv := range c.View.Lines {
		id, _ := strconv.ParseInt(lv.Inputs["po_line_id"], 10, 64) // stored by Validate's rules: an id
		out[id] = entry{received: lv.Inputs["received_qty"], rejected: lv.Inputs["rejected_qty"], reason: lv.Inputs["reject_reason"], id: lv.ID, pos: lv.No}
	}
	if !posted {
		return out
	}
	fill, pos, show := c.Req.URL.Query().Get("fill"), 0, shownInputs(c)
	for _, l := range lines {
		n, st := strconv.Itoa(l.No), out[l.ID]
		read := func(name, key, stored string) string {
			if show[name] {
				return c.Req.PostFormValue(key + "-" + n)
			}
			return stored
		}
		e := entry{received: read("received_qty", "received", st.received), rejected: read("rejected_qty", "rejected", st.rejected),
			reason: read("reject_reason", "reason", st.reason), id: st.id}
		switch {
		case fill == fillClear:
			e.received = ""
		case fill == fillAll && l.Pending > 0:
			e.received = types.FormatQty(l.Pending)
		}
		if nonZero(e.received) {
			pos++
			e.pos = pos
		}
		out[l.ID] = e
	}
	return out
}

// nonZero reports whether a typed quantity is there and not 0; one NUM
// can't read counts, so its error has a card to sit on.
func nonZero(s string) bool {
	if strings.TrimSpace(s) == "" {
		return false
	}
	q, err := types.ParseQty(s)
	return err != nil || q != 0
}

// poLines are the draft's PO lines.
func poLines(c docscreen.Ctx) ([]goodsreceipt.POLine, error) {
	var lines []goodsreceipt.POLine
	err := c.Deps.DB.Read(c.Req.Context(), func(tx *db.Tx) (err error) {
		lines, err = goodsreceipt.POLines(c.Req.Context(), tx, c.View.Info.ID)
		return err
	})
	return lines, err
}

// itemsSave is step 3's save (UX doc §5.4 Saving): one line per card with
// a quantity above 0, in PO line order, with the stored line's id when
// there is one (CD-256 (3)); a non-nil empty set on Clear all. Each
// quantity is parsed and checked against its units here, so the texts
// number the PO line (AD-12 P2, CD-277 (4)); any error is a 422 with
// nothing written.
func (m *module) itemsSave(c docscreen.Ctx) (docscreen.Save, error) {
	lines, err := poLines(c)
	if err != nil {
		return docscreen.Save{}, err
	}
	es := entries(c, lines, true)
	out := []map[string]string{}
	var errs []doctype.FieldError
	for _, l := range lines {
		e := es[l.ID]
		if e.pos == 0 {
			continue
		}
		received, fe := checkQty(l, e.pos, "received_qty", e.received)
		if fe != nil {
			errs = append(errs, *fe)
		}
		var rejected types.Qty
		if strings.TrimSpace(e.rejected) != "" {
			if rejected, fe = checkQty(l, e.pos, "rejected_qty", e.rejected); fe != nil {
				errs = append(errs, *fe)
			}
		}
		line := map[string]string{"received_qty": received.Plain(), "rejected_qty": rejected.Plain(), "reject_reason": e.reason}
		if e.id != "" {
			line["id"] = e.id
		} else {
			line["po_line_id"] = strconv.FormatInt(l.ID, 10)
		}
		out = append(out, line)
	}
	if len(errs) > 0 {
		return docscreen.Save{Errors: errs}, nil
	}
	return docscreen.Save{Lines: out}, nil
}

// checkQty is s read as a quantity of PO line l, posted at pos, with NUM's
// text or uom.CheckLineQty's when it can't be one, starting with l's number.
func checkQty(l goodsreceipt.POLine, pos int, field, s string) (types.Qty, *doctype.FieldError) {
	q, err := types.ParseQty(s)
	if err != nil {
		return 0, &doctype.FieldError{Field: field, Line: pos, Message: "Line " + strconv.Itoa(l.No) + ": " + err.Error()}
	}
	if fe := uom.CheckLineQty(l.No, q, l.UoM, l.StockUoM, l.Factor); fe != nil {
		return 0, &doctype.FieldError{Field: field, Line: pos, Message: fe.Message}
	}
	return q, nil
}

// itemCard is one PO line's card.
type itemCard struct {
	Title, Code, Hint          string
	Received, Rejected, Reason components.FieldProps
	Show                       map[string]bool // the fields Hide shows
	Whole                      bool
	UoM                        string
	Open                       bool   // Some rejected
	Accepted                   string // after a save with rejects
	Warnings                   []string
}

// itemsForm is step 3's Body.
type itemsForm struct {
	Summary        string
	Warnings       []string // G11's duplicate challan, if any
	FillAll, Clear components.ButtonProps
	Cards, Full    []itemCard // Full: fully received, when some lines are pending
	FullLabel      string
	FullOpen       bool
}

// itemsBody draws step 3 from the post on a redraw, else from the stored
// lines, with LineWarnings on the accepted quantities shown (UX doc §5.4)
// and G11's duplicate challan.
func (m *module) itemsBody(c docscreen.Ctx, s docscreen.StepState) (templ.Component, error) {
	ctx := c.Req.Context()
	var (
		lines   []goodsreceipt.POLine
		es      map[int64]entry
		warn    map[int64]string
		challan string
	)
	err := c.Deps.DB.Read(ctx, func(tx *db.Tx) (err error) {
		if lines, err = goodsreceipt.POLines(ctx, tx, c.View.Info.ID); err != nil {
			return err
		}
		es = entries(c, lines, s.Posted)
		accepted := map[int64]types.Qty{}
		for _, l := range lines {
			if a, ok := acceptedOf(es[l.ID]); ok {
				accepted[l.ID] = a
			}
		}
		poID, _ := strconv.ParseInt(c.View.Inputs["purchase_order_id"], 10, 64) // a saved draft's PO: an id
		if warn, err = goodsreceipt.LineWarnings(ctx, tx, poID, c.View.Info.ID, accepted); err != nil {
			return err
		}
		// docscreen loaded c.View with p's read check (Deps.View).
		challan, err = goodsreceipt.DuplicateChallan(ctx, tx, c.Principal, c.View.Info.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	facts, err := m.receiptFacts(c)
	if err != nil {
		return nil, err
	}
	base := "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(c.View.Info.ID, 10) + "/edit/items?fill="
	f := itemsForm{
		Summary: facts.PONumber + " · " + facts.VendorName + " · " + facts.SiteName,
		FillAll: components.ButtonProps{Label: labelFillAll, Busy: busyFillAll, Name: "nav", Value: "stay",
			Variant: components.Secondary, Attrs: templ.Attributes{"formaction": base + fillAll}},
		Clear: components.ButtonProps{Label: labelClearAll, Busy: busyClearAll, Name: "nav", Value: "stay",
			Variant: components.Secondary, Attrs: templ.Attributes{"formaction": base + fillClear}},
	}
	rf, _ := c.DT.Field("reject_reason")
	reasonMax := rf.MaxLen
	somePending, show := false, shownInputs(c)
	for _, l := range lines {
		somePending = somePending || l.Pending > 0
	}
	for _, l := range lines {
		card := cardOf(c.DT, l, es[l.ID], s, warn[l.ID], reasonMax)
		card.Show = show
		if !somePending || l.Pending > 0 {
			f.Cards = append(f.Cards, card)
			continue
		}
		f.Full = append(f.Full, card)
		e := es[l.ID]
		f.FullOpen = f.FullOpen || strings.TrimSpace(e.received+e.rejected+e.reason) != "" // a value in any input; errors come with one
	}
	if challan != "" && shown(c, "challan_no") { // the text shows challan_no's value (CD-319)
		f.Warnings = []string{challan}
	}
	f.FullLabel = fmt.Sprintf("Fully received lines (%d)", len(f.Full))
	return itemsView(f), nil
}

// acceptedOf is a card's received less rejected, when both read.
func acceptedOf(e entry) (types.Qty, bool) {
	received, err := types.ParseQty(e.received)
	if err != nil {
		return 0, false
	}
	var rejected types.Qty
	if strings.TrimSpace(e.rejected) != "" {
		if rejected, err = types.ParseQty(e.rejected); err != nil {
			return 0, false
		}
	}
	return received - rejected, true
}

// cardOf is PO line l's card, its inputs marked as dt declares them, with
// its values e, the step's errors, its over-delivery warning, if any, and
// the reason's longest length.
func cardOf(dt *doctype.DocType, l goodsreceipt.POLine, e entry, s docscreen.StepState, warning string, reasonMax int) itemCard {
	n := strconv.Itoa(l.No)
	field := func(key, name, label, value string, attrs templ.Attributes) components.FieldProps {
		id := components.FieldID("po-line[" + n + "]." + name)
		if e.pos > 0 {
			id = docscreen.LineFieldID(e.pos, name)
		}
		attrs["aria-label"] = label + ", line " + n + ", " + l.ItemName
		return declared(dt, name, components.FieldProps{ID: id, Name: key + "-" + n, Label: label, Value: value,
			Error: lineError(s.Errors, e.pos, name), Attrs: attrs})
	}
	c := itemCard{
		Title: n + " · " + l.ItemName, Code: l.ItemCode, Whole: l.UoM.WholeNumber, UoM: l.UoM.Code,
		Received: field("received", "received_qty", labelReceived, e.received, templ.Attributes{"data-line-entry": l.Pending > 0}), // counted (CD-391)
		Rejected: field("rejected", "rejected_qty", labelRejected, e.rejected, templ.Attributes{}),
		Reason:   field("reason", "reject_reason", labelReason, e.reason, templ.Attributes{"maxlength": strconv.Itoa(reasonMax)}),
	}
	c.Hint = "Ordered " + types.FormatQty(l.Ordered) + " · Pending " + types.FormatQtyUoM(l.Pending, l.UoM.Code)
	c.Received.Attrs["aria-describedby"] = c.Received.ID + "-hint " + c.Received.ID + "-msg" // the hint sits with the item, not the field
	c.Reason.Hint = hintReason
	c.Open = nonZero(e.rejected) || c.Rejected.Error != "" || c.Reason.Error != ""
	if a, ok := acceptedOf(e); ok && !s.Posted && nonZero(e.rejected) {
		c.Accepted = "Accepted " + types.FormatQtyUoM(a, l.UoM.Code)
	}
	if warning != "" {
		c.Warnings = []string{warning}
	}
	return c
}

// lineError is the message of the error on posted line pos's field, if
// any; pos 0 matches none, line fields have no header errors.
func lineError(errs []doctype.FieldError, pos int, field string) string {
	for _, e := range errs {
		if e.Line == pos && e.Field == field {
			return e.Message
		}
	}
	return ""
}
