package grnweb

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3c: the GRN's line card on the check and document pages (UX doc §5.5
// P1, GUX-017). docscreen calls it with each line's own Ctx (CD-339) and
// the default card already without the fields Hide hides (CD-319 (9)).

// S3c copy (UX doc §5.5).
const (
	cardReceived = "Received "
	cardAccepted = "Accepted "
	cardRejected = "Rejected "
)

// lineCache holds the last request's Lines, as factsCache holds Facts:
// docscreen asks for one card per line, and one read per request is enough.
type lineCache struct {
	mu    sync.Mutex
	req   *http.Request
	id    int64
	lines map[int64]goodsreceipt.ReceiptLine
}

// receiptLines is Service.Lines for c's GRN, read once per request.
func (m *module) receiptLines(c docscreen.Ctx) (map[int64]goodsreceipt.ReceiptLine, error) {
	lc := &m.lines
	lc.mu.Lock()
	defer lc.mu.Unlock()
	if lc.req == c.Req && lc.id == c.View.Info.ID {
		return lc.lines, nil
	}
	lines, err := m.d.Service.Lines(c.Req.Context(), c.Principal, c.View.Info.ID)
	if err != nil {
		return nil, err
	}
	lc.req, lc.id, lc.lines = c.Req, c.View.Info.ID, lines
	return lines, nil
}

// lineCard is line l's card: numbered by its PO line, titled by the item,
// "Received {qty}" and "Accepted {qty}" in the line's snapshot unit, the first
// in QtyRate's slot and the second in Tax's, two parts that each stay whole
// and start at the card's edge when they wrap (GRN review M11), the accepted
// value as its amount, and "Rejected {qty}: {reason}" when anything was
// rejected. Each part is left out when Hide, asked with the line's Ctx c,
// hides its field (CD-319); the accepted value needs both accepted and the
// header's accepted_value shown (CD-319 (8)), and a value over the money
// maximum is left out. Module.LineCard can't return an error, so a failed
// read is logged and keeps the default card without its item.
func (m *module) lineCard(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
	lines, err := m.receiptLines(c)
	id, _ := strconv.ParseInt(l.ID, 10, 64) // a stored line's id
	rl, ok := lines[id]
	if err != nil {
		slog.ErrorContext(c.Req.Context(), "grnweb: line card lines not read, default card shown", "grn", c.View.Info.ID, "err", err)
	}
	if err != nil || !ok {
		card := l.Card
		card.Item, card.Code = "", "" // the default card's item is the line's first Link, the UoM
		return card
	}
	unit := ""
	if shown(c, "uom_id") {
		unit = rl.UoM
	}
	qty := func(name string) (types.Qty, bool) {
		if !shown(c, name) {
			return 0, false
		}
		q, err := types.ParseQty(l.Inputs[name])
		return q, err == nil
	}
	text := func(q types.Qty) string { return strings.TrimSpace(types.FormatQtyUoM(q, unit)) }

	card := l.Card
	card.No, card.Item, card.Code = rl.PONo, rl.ItemName, rl.ItemCode
	var parts []string
	if q, ok := qty("received_qty"); ok {
		parts = append(parts, cardReceived+text(q))
	}
	if q, ok := qty("accepted_qty"); ok {
		parts = append(parts, cardAccepted+text(q))
		if v, err := gstcalc.LineAmount(q, rl.Rate); err == nil && shown(c, "accepted_value") {
			card.Amount = types.FormatINR(v)
		}
	}
	parts = append(parts, "", "") // a part Hide hides leaves its slot blank, the rest first
	card.QtyRate, card.Tax = parts[0], parts[1]
	if q, ok := qty("rejected_qty"); ok && q > 0 {
		card.Note = cardRejected + text(q)
		if reason := l.Inputs["reject_reason"]; reason != "" && shown(c, "reject_reason") {
			card.Note += ": " + reason
		}
	}
	return card
}
