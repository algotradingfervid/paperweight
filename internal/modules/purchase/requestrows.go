package purchase

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// lineOrdered is what one PO line counts towards its request row (PO spec
// §20.2): draft, pending and rejected hold qty; approved orders qty; closed
// orders min(qty, already + received), so a short close frees the rest;
// cancelled counts nothing.
func lineOrdered(state doctype.State, qty, already, received types.Qty) (ordered, held types.Qty) {
	switch state {
	case doctype.StateDraft, doctype.StatePending, doctype.StateRejected:
		return 0, qty
	case doctype.StateApproved:
		return qty, 0
	case doctype.StateClosed:
		return min(qty, already+received), 0
	}
	return 0, 0
}

// linkedOrdered is lineOrdered of the stored PO line l. For the PO poID the
// received quantity is accepted's (a missing line counts 0), never the
// received_qty column (spec §20.3, CD-276); poID 0 reads the column for all.
func linkedOrdered(l dbq.RequestLinkedLinesRow, poID int64, accepted map[int64]types.Qty) (ordered, held types.Qty) {
	received := types.Qty(l.ReceivedQty)
	if l.PoID == poID {
		received = accepted[l.ID]
	}
	return lineOrdered(doctype.State(l.State), types.Qty(l.Qty), types.Qty(l.AlreadyReceived), received)
}

// pendingOf is request row rowID's pending to order from its request's
// linked lines: max(0, qty − ordered − held) summed over the row's lines.
func pendingOf(rowID int64, qty types.Qty, links []dbq.RequestLinkedLinesRow) types.Qty {
	var on types.Qty
	for _, l := range links {
		if l.RowID == rowID {
			o, h := linkedOrdered(l, 0, nil)
			on += o + h
		}
	}
	return max(0, qty-on)
}

// holderOf is the linked line l as a Holder.
func holderOf(l dbq.RequestLinkedLinesRow) Holder {
	return Holder{RequestLineID: l.RowID, POID: l.PoID, PODisplay: naming.Label(l.PoNumber, l.PoID),
		State: doctype.State(l.State), Owner: types.UserID(l.CreatedBy), Qty: types.Qty(l.Qty)}
}

// checkRequestRows is the §20.5 post-state check Validate calls last, so the
// kernel runs it on every save, submit and approve (D-APR-15). It reads the
// other POs inside the write (CD-94). Each refused line gets one error on
// request_line_id with Line = the PO line. Rows of a request that isn't
// approved or closed are DOC's (its link texts) and skipped here.
func checkRequestRows(ctx context.Context, tx *db.Tx, poID int64) []doctype.FieldError {
	q := dbq.New(tx.SQL())
	rows, err := q.OrderRequestRows(ctx, poID)
	must(err)
	var errs []doctype.FieldError
	first := map[int64]int64{} // request row -> the PO line that takes it first
	for _, r := range rows {
		k, dup := first[r.RowID]
		if !dup {
			first[r.RowID] = r.LineNo
		}
		if st := doctype.State(r.RequestState); st != doctype.StateApproved && st != doctype.StateClosed {
			continue
		}
		if msg := requestRowError(ctx, tx, q, poID, r, dup, k); msg != "" {
			errs = append(errs, doctype.FieldError{Field: "request_line_id", Line: int(r.LineNo), Message: msg})
		}
	}
	return errs
}

// requestRowError is the first §20.5 condition PO line r breaks, or "". dup
// says an earlier line k of this PO takes the same row.
func requestRowError(ctx context.Context, tx *db.Tx, q *dbq.Queries, poID int64, r dbq.OrderRequestRowsRow, dup bool, k int64) string {
	pr := naming.Label(r.RequestNumber, r.RequestID)
	switch {
	case r.SiteID != 0 && r.RequestSiteID != r.SiteID:
		return fmt.Sprintf(msgRequestSite, r.LineNo, pr, siteCode(ctx, tx, r.RequestSiteID), siteCode(ctx, tx, r.SiteID))
	case r.RequestProjectID != r.ProjectID:
		return fmt.Sprintf(msgRequestProject, r.LineNo, pr, projectCode(ctx, tx, r.RequestProjectID), projectCode(ctx, tx, r.ProjectID))
	case r.ItemID != 0 && r.ItemID != r.RowItemID:
		it, err := items.Get(ctx, tx, r.RowItemID)
		must(err)
		return fmt.Sprintf(msgRequestItem, r.LineNo, pr, r.RowNo, it.Code)
	case r.UomID != r.RowUomID:
		u, err := uoms.Get(ctx, tx, r.RowUomID)
		must(err)
		return fmt.Sprintf(msgRequestUoM, r.LineNo, pr, r.RowNo, u.Code)
	case dup:
		return fmt.Sprintf(msgRequestSameRow, r.LineNo, pr, r.RowNo, k)
	}
	links, err := q.RequestLinkedLines(ctx, r.RequestID)
	must(err)
	var others types.Qty
	oldest := ""
	for _, l := range links {
		if l.RowID != r.RowID || l.PoID == poID {
			continue
		}
		o, h := linkedOrdered(l, 0, nil)
		if o+h > 0 && oldest == "" {
			oldest = naming.Label(l.PoNumber, l.PoID)
		}
		others += o + h
	}
	if others >= types.Qty(r.RowQty) {
		return fmt.Sprintf(msgRequestAlreadyOn, r.LineNo, pr, r.RowNo, oldest)
	}
	return ""
}

func siteCode(ctx context.Context, tx *db.Tx, id int64) string {
	s, err := sites.Get(ctx, tx, id)
	must(err)
	return s.Code
}

func projectCode(ctx context.Context, tx *db.Tx, id int64) string {
	p, err := projects.Get(ctx, tx, id)
	must(err)
	return p.Code
}

// requestWarnings are the over-request warnings OrderWarnings adds (spec
// §20.5): per PO line ordering more than its request row's qty, the excess
// with no unit.
func requestWarnings(ctx context.Context, tx *db.Tx, poID int64) ([]string, error) {
	rows, err := dbq.New(tx.SQL()).OrderRequestRows(ctx, poID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, r := range rows {
		if r.Qty > r.RowQty {
			out = append(out, fmt.Sprintf(msgRequestOverRequest, r.LineNo, types.FormatQty(types.Qty(r.Qty-r.RowQty)),
				naming.Label(r.RequestNumber, r.RequestID)))
		}
	}
	return out, nil
}

// Holder is one non-cancelled PO line linking a request row (MOD-PO P6 Do 7).
type Holder struct {
	RequestLineID, POID int64
	PODisplay           string
	State               doctype.State
	Owner               types.UserID
	Qty                 types.Qty
}

// OpenRequest is one submitted request with its rows, as Add from requests
// lists it (MOD-PO P6 Do 7).
type OpenRequest struct {
	ID            int64
	Display       string
	RequesterID   types.UserID
	RequesterName string
	NeededBy      types.Date // zero when blank
	Rows          []OpenRequestRow
}

// OpenRequestRow is one request row with its pending to order and the
// holders whose lineOrdered gives ordered or held above 0.
type OpenRequestRow struct {
	RequestLineID int64
	LineNo        int
	ItemID        int64
	ItemCode      string
	Description   string
	UoMID         int64
	UoMCode       string
	Qty           types.Qty
	Pending       types.Qty
	Holders       []Holder
}

// PendingToOrder is each request row's pending to order (PO spec §20.2),
// keyed by request line id: max(0, qty − ordered − held) over the row's PO
// lines, and 0 on a closed request.
func PendingToOrder(ctx context.Context, tx *db.Tx, requestLineIDs []int64) (map[int64]types.Qty, error) {
	q := dbq.New(tx.SQL())
	out := make(map[int64]types.Qty, len(requestLineIDs))
	for _, id := range requestLineIDs {
		row, err := q.RequestRowOf(ctx, id)
		if err != nil {
			return nil, err
		}
		if doctype.State(row.State) == doctype.StateClosed {
			out[id] = 0
			continue
		}
		links, err := q.RequestLinkedLines(ctx, row.ParentID)
		if err != nil {
			return nil, err
		}
		out[id] = pendingOf(id, types.Qty(row.Qty), links)
	}
	return out, nil
}

// HeldBy is every non-cancelled PO line linking the request, oldest PO first
// and in line order. Callers filter by state.
func HeldBy(ctx context.Context, tx *db.Tx, requestID int64) ([]Holder, error) {
	links, err := dbq.New(tx.SQL()).RequestLinkedLines(ctx, requestID)
	if err != nil {
		return nil, err
	}
	var out []Holder
	for _, l := range links {
		if doctype.State(l.State) != doctype.StateCancelled {
			out = append(out, holderOf(l))
		}
	}
	return out, nil
}

// OpenRequestLines lists the submitted requests p may read for the site and
// project, oldest needed-by first, each with its rows' pending to order and
// the holders whose lineOrdered is above 0. Scope is asked before the read
// opens: it has no Tx twin.
func (s *Service) OpenRequestLines(ctx context.Context, p auth.Principal, siteID, projectID int64) ([]OpenRequest, error) {
	pred, err := s.d.Auth.Scope(ctx, p, RequestDocType, auth.RightRead)
	if err != nil {
		return nil, err
	}
	var out []OpenRequest
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		readable, err := scopedRequests(ctx, tx, pred)
		if err != nil {
			return err
		}
		q := dbq.New(tx.SQL())
		reqs, err := q.OpenRequests(ctx, dbq.OpenRequestsParams{SiteID: nullID(siteID), ProjectID: nullID(projectID)})
		if err != nil {
			return err
		}
		for _, r := range reqs {
			if !readable(r.ID) {
				continue
			}
			o, err := s.openRequest(ctx, tx, q, r)
			if err != nil {
				return err
			}
			out = append(out, o)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// openRequest is request r with its requester and rows.
func (s *Service) openRequest(ctx context.Context, tx *db.Tx, q *dbq.Queries, r dbq.OpenRequestsRow) (OpenRequest, error) {
	o := OpenRequest{ID: r.ID, Display: naming.Label(r.Number, r.ID), RequesterID: types.UserID(r.CreatedBy)}
	u, _, err := s.d.Auth.User(ctx, tx, auth.UserID(r.CreatedBy))
	if err != nil {
		return OpenRequest{}, err
	}
	o.RequesterName = u.Name
	if r.RequiredBy != "" {
		if o.NeededBy, err = types.DateFromISO(r.RequiredBy); err != nil {
			return OpenRequest{}, err
		}
	}
	lines, err := q.RequestLinesForValidate(ctx, r.ID)
	if err != nil {
		return OpenRequest{}, err
	}
	links, err := q.RequestLinkedLines(ctx, r.ID)
	if err != nil {
		return OpenRequest{}, err
	}
	for _, l := range lines {
		it, err := items.Get(ctx, tx, l.ItemID)
		if err != nil {
			return OpenRequest{}, err
		}
		u, err := uoms.Get(ctx, tx, l.UomID)
		if err != nil {
			return OpenRequest{}, err
		}
		row := OpenRequestRow{RequestLineID: l.ID, LineNo: int(l.LineNo), ItemID: l.ItemID, ItemCode: it.Code, Description: l.Description,
			UoMID: l.UomID, UoMCode: u.Code, Qty: types.Qty(l.Qty), Pending: pendingOf(l.ID, types.Qty(l.Qty), links)}
		for _, k := range links {
			if o, h := linkedOrdered(k, 0, nil); k.RowID == l.ID && o+h > 0 {
				row.Holders = append(row.Holders, holderOf(k))
			}
		}
		o.Rows = append(o.Rows, row)
	}
	return o, nil
}

// scopedRequests reports whether a request id is in pred, the viewer's read
// scope ("" is every request).
func scopedRequests(ctx context.Context, tx *db.Tx, pred auth.Predicate) (func(int64) bool, error) {
	if pred.SQL == "" {
		return func(int64) bool { return true }, nil
	}
	rows, err := tx.SQL().QueryContext(ctx, `SELECT "purchase_request".id FROM "purchase_request" WHERE `+pred.SQL, pred.Args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return func(id int64) bool { return ids[id] }, nil
}
