package purchase

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// validateRequest is the request's Validate hook (PO spec §20.2). It runs on
// every save and on submit (D-APR-15) and reads inside the tx (CD-94). It
// checks the dates, the site and project (the PO's rule and texts, §4) and
// each line's unit and quantity. While the request is a draft it writes the
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

	errs := checkRequestDates(h, types.Today(deps.Clock))
	projectID, fe, err := checkSiteProject(ctx, tx, h.SiteID, h.ProjectID)
	must(err)
	if fe != nil {
		errs = append(errs, *fe)
		projectID = h.ProjectID
	}
	draft := h.State == string(doctype.StateDraft)
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
			l.UomID = it.StockUoMID
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

// beforeSubmitRequest adds no rule: the kernel runs Validate again on submit
// (D-APR-15), and submit moves a request straight to approved (§20.3).
func beforeSubmitRequest(context.Context, *db.Tx, int64) error { return nil }
