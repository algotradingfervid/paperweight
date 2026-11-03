package purchase

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// validateRequest is the request's Validate hook (PO spec §20.2). It runs on
// every save and on submit (D-APR-15) and reads inside the tx (CD-94). It
// checks the dates, the site and project (the PO's rule and texts, §4) and
// each line's unit and quantity. While the request is a draft it writes
// today into a blank request date (hidden on Details, CD-258 Q8 (a)), the
// project default from the site and each line's description and unit
// defaults from its item, even when it returns field errors (CD-302); a
// refused site and project check skips only the project default.
func validateRequest(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	deps, err := wiring.Get(tx)
	must(err)
	q := dbq.New(tx.SQL())
	h, err := q.RequestForValidate(ctx, id)
	must(err)
	rows, err := q.RequestLinesForValidate(ctx, id)
	must(err)

	draft := h.State == string(doctype.StateDraft)
	if draft && h.RequestDate == "" {
		h.RequestDate = types.Today(deps.Clock).ISO()
		must(q.SetRequestDate(ctx, dbq.SetRequestDateParams{ID: id, RequestDate: sql.NullString{String: h.RequestDate, Valid: true}}))
	}
	errs := checkRequestDates(h, types.Today(deps.Clock))
	projectID, fe, err := checkSiteProject(ctx, tx, h.SiteID, h.ProjectID)
	must(err)
	if fe != nil {
		errs = append(errs, *fe)
		projectID = h.ProjectID
	}
	if draft {
		must(q.SetRequestProject(ctx, dbq.SetRequestProjectParams{ID: id, ProjectID: nullID(projectID)}))
	}
	for i := range rows {
		l := &rows[i]
		if l.ItemID == 0 {
			continue
		}
		it, err := items.Get(ctx, tx, l.ItemID)
		must(err)
		if l.Description == "" {
			l.Description = it.Name
		}
		if l.UomID == 0 {
			l.UomID = it.PurchaseUoMID
		}
		if fe := checkRequestQty(ctx, tx, l, it.Code); fe != nil {
			errs = append(errs, *fe)
		}
		if draft {
			must(q.SetRequestLineDefaults(ctx, dbq.SetRequestLineDefaultsParams{ID: l.ID, Description: l.Description, UomID: nullID(l.UomID)}))
		}
	}
	return errs
}

// checkRequestDates: the request date is not after today (the business
// date), and needed-by is not before the request date (PO spec §20.2).
func checkRequestDates(h dbq.RequestForValidateRow, today types.Date) []doctype.FieldError {
	if h.RequestDate == "" {
		return nil
	}
	on, err := types.DateFromISO(h.RequestDate)
	must(err)
	var errs []doctype.FieldError
	if on.Compare(today) > 0 {
		errs = append(errs, doctype.FieldError{Field: "request_date", Message: msgRequestDateFuture})
	}
	if h.RequiredBy != "" {
		by, err := types.DateFromISO(h.RequiredBy)
		must(err)
		if by.Compare(on) < 0 {
			errs = append(errs, doctype.FieldError{Field: "required_by", Message: msgNeededByEarly})
		}
	}
	return errs
}

// checkRequestQty checks the line's unit against its item (the PO's
// conversion text, CD-278) and its quantity's whole-number flags through
// uom.CheckLineQty (CD-277 (4)).
func checkRequestQty(ctx context.Context, tx *db.Tx, l *dbq.RequestLinesForValidateRow, itemCode string) *doctype.FieldError {
	_, ok, err := items.Factor(ctx, tx, l.ItemID, l.UomID)
	must(err)
	if !ok {
		u, err := uoms.Get(ctx, tx, l.UomID)
		must(err)
		return &doctype.FieldError{Field: "uom_id", Line: int(l.LineNo), Message: fmt.Sprintf(msgNoConversion, l.LineNo, itemCode, u.Code)}
	}
	snap, err := items.Snapshot(ctx, tx, l.ItemID, l.UomID)
	must(err)
	return uom.CheckLineQty(int(l.LineNo), types.Qty(l.Qty), uom.UoM{Code: snap.UoMCode, WholeNumber: snap.LineWhole},
		uom.UoM{Code: snap.StockUoMCode, WholeNumber: snap.StockWhole}, snap.Factor)
}

// beforeSubmitRequest sets the hidden request date to the submit's business
// date, so the number takes that date's FY (CD-330 (1)), and checks needed-by
// against it (§20.2) and that the units resolved (CD-387 (2)).
// The kernel has run Validate already (D-APR-15); submit then moves a request
// straight to approved (§20.3).
func beforeSubmitRequest(ctx context.Context, tx *db.Tx, id int64) error {
	deps, err := wiring.Get(tx)
	must(err)
	q := dbq.New(tx.SQL())
	h, err := q.RequestForValidate(ctx, id)
	must(err)
	today := types.Today(deps.Clock)
	h.RequestDate = today.ISO()
	must(q.SetRequestDate(ctx, dbq.SetRequestDateParams{ID: id, RequestDate: sql.NullString{String: h.RequestDate, Valid: true}}))
	rows, err := q.RequestLinesForValidate(ctx, id)
	must(err)
	dt, _ := doc.Lookup(RequestDocType)
	if errs := append(checkRequestDates(h, today), requestBlanks(dt, rows)...); len(errs) > 0 {
		return doc.Invalid(errs)
	}
	return nil
}
