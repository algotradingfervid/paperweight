package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The masters' read paths (Get, CheckRole, Defaults, Factor, Snapshot,
// PartySnapshot, ForParty) use only the tx, so the hooks call them over a
// zero Env, as mstfixture's hooks do.
var (
	parties   = party.New(mstkit.Env{})
	addresses = address.New(mstkit.Env{})
	items     = item.New(mstkit.Env{})
	uoms      = uom.New(mstkit.Env{})
	taxCodes  = taxcode.New(mstkit.Env{})
	sites     = site.New(mstkit.Env{})
	projects  = project.New(mstkit.Env{})
	branches  = company.New(mstkit.Env{})
	termsList = paymentterms.New(mstkit.Env{})
)

// must panics on an error Validate can't return as a field error; the
// engine's recover turns it into its plain internal error (see mustWired).
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// fieldErrors are a master's field errors (*mstkit.ValidationError) put on
// field, the PO's own name for the link; any other error panics (must).
func fieldErrors(err error, field string) []doctype.FieldError {
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		panic(err)
	}
	out := make([]doctype.FieldError, len(ve.Fields))
	for i, fe := range ve.Fields {
		out[i] = doctype.FieldError{Field: field, Line: fe.Line, Message: fe.Message}
	}
	return out
}

// inputError is gstcalc's or types' *types.InputError as a field error on
// field; any other error panics (must).
func inputError(err error, field string, line int) doctype.FieldError {
	var ie *types.InputError
	if !errors.As(err, &ie) {
		panic(err)
	}
	msg := ie.Msg
	if line > 0 {
		msg = fmt.Sprintf("Line %d: ", line) + msg
	}
	return doctype.FieldError{Field: field, Line: line, Message: msg}
}

// validateOrder is the Order's Validate hook (PO spec §4, §8). It runs on
// every save, submit and approve (D-APR-15) and reads inside the tx (CD-94).
// A blank order date on a draft is today (PO spec §3.1, as for a request).
// It checks the dates, the vendor, the site and project and each line, and
// while the PO is a draft writes the defaults, snapshots, totals and
// receipt_status. Each part is written from the current inputs or cleared,
// never left stale (CD-302), even when Validate returns field errors: amend
// keeps Validate's writes and ignores its errors (doc/terminal.go; spec §15
// item 8), and a step save commits with out-of-scope errors as Problems
// (CD-205, CD-207). On a full save, submit and approve the errors roll
// everything back. copied_from_no and copy_notes are never
// touched. The request-row check (P6) runs last.
func validateOrder(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	deps, err := wiring.Get(tx)
	must(err)
	q := dbq.New(tx.SQL())
	h, err := q.OrderForValidate(ctx, id)
	must(err)
	rows, err := q.OrderLinesForValidate(ctx, id)
	must(err)

	if h.State == string(doctype.StateDraft) && h.OrderDate == "" {
		h.OrderDate = types.Today(deps.Clock).ISO()
		must(q.SetOrderDate(ctx, dbq.SetOrderDateParams{ID: id, OrderDate: sql.NullString{String: h.OrderDate, Valid: true}}))
	}
	errs := checkDates(h, types.Today(deps.Clock))
	// A refused vendor leaves its part blank (cleared), never another
	// party's; the payment terms stay as the user set them.
	vendor := dbq.SetOrderVendorParams{ID: id, PaymentTermsID: nullID(h.PaymentTermsID)}
	category, supplierState, fe := checkVendor(ctx, tx, h, &vendor)
	errs = append(errs, fe...)
	if vendor.PaymentTermsID.Valid {
		t, err := termsList.Get(ctx, tx, vendor.PaymentTermsID.Int64)
		must(err)
		vendor.SnapTermsName = t.Name
	}
	site := dbq.SetOrderSiteParams{ID: id, ProjectID: nullID(h.ProjectID)}
	placeState, fe := checkSite(ctx, tx, h, &site)
	errs = append(errs, fe...)
	lines, lineOK, fe := checkLines(ctx, tx, h, site.ProjectID.Int64, rows)
	errs = append(errs, fe...)
	t := lineTotals(lines, lineOK, category, supplierState, placeState, h.ReverseCharge != 0)
	over := overLimit(t)
	if over {
		errs = append(errs, doctype.FieldError{Field: "lines", Message: fmt.Sprintf(msgTotalTooLarge, types.FormatINR(types.MaxMoney))})
	}
	errs = append(errs, checkEarlierPO(h, rows)...)

	if h.State == string(doctype.StateDraft) {
		must(q.SetOrderVendor(ctx, vendor))
		must(q.SetOrderSite(ctx, site))
		writeOrder(ctx, q, id, h, rows, lines, lineOK, t, over)
	}
	return append(errs, checkRequestRows(ctx, tx, id)...)
}

// lineTotals are the order's totals over its lines (a line whose own check
// failed counts as 0).
func lineTotals(lines []dbq.SetOrderLineValidatedParams, lineOK []bool, category, supplierState, placeState string, reverseCharge bool) gstcalc.DocTotals {
	amounts, rates := make([]types.Money, len(lines)), make([]types.Percent, len(lines))
	for i, l := range lines {
		if lineOK[i] {
			amounts[i], rates[i] = types.Money(l.Amount.Int64), types.Percent(l.SnapGstRate)
		}
	}
	return orderTotals(category, supplierState, placeState, reverseCharge, amounts, rates)
}

// overLimit: a header total is above the money maximum (CD-308).
func overLimit(t gstcalc.DocTotals) bool {
	for _, m := range []types.Money{t.Net, t.CGST, t.SGST, t.IGST, t.Grand, t.Rounded} {
		if m > types.MaxMoney {
			return true
		}
	}
	return false
}

// writeOrder writes every line, with its GST from t (a line whose own check
// failed has its amount and GST cleared), and the header's terms default,
// totals (t, the sums of the lines as written; all 0 when over the maximum,
// CD-308) and receipt_status.
func writeOrder(ctx context.Context, q *dbq.Queries, id int64, h dbq.OrderForValidateRow, rows []dbq.OrderLinesForValidateRow,
	lines []dbq.SetOrderLineValidatedParams, lineOK []bool, t gstcalc.DocTotals, over bool) {
	tot := dbq.SetOrderTotalsParams{ID: id, Terms: h.Terms}
	if h.Terms == "" {
		var err error
		tot.Terms, err = q.DefaultPOTerms(ctx)
		must(err)
	}
	receipt := make([]ReceiptLine, len(lines))
	for i := range lines {
		receipt[i] = ReceiptLine{Qty: types.Qty(rows[i].Qty), Already: types.Qty(rows[i].AlreadyReceived), Received: types.Qty(rows[i].ReceivedQty)}
	}
	for i := range lines {
		if lineOK[i] {
			lines[i].Cgst, lines[i].Sgst, lines[i].Igst = valid(int64(t.Lines[i].CGST)), valid(int64(t.Lines[i].SGST)), valid(int64(t.Lines[i].IGST))
		} else {
			lines[i].Amount = sql.NullInt64{}
		}
		must(q.SetOrderLineValidated(ctx, lines[i]))
	}
	if !over {
		tot.NetTotal, tot.CgstTotal, tot.SgstTotal, tot.IgstTotal = int64(t.Net), int64(t.CGST), int64(t.SGST), int64(t.IGST)
		tot.GrandTotal, tot.RoundedTotal = int64(t.Grand), int64(t.Rounded)
	}
	tot.ReceiptStatus = receiptStatusOf(receipt)
	must(q.SetOrderTotals(ctx, tot))
}

// checkDates: the order date is not after today (the business date), and
// required-by is not before the order date (PO spec §3.1, §4).
func checkDates(h dbq.OrderForValidateRow, today types.Date) []doctype.FieldError {
	if h.OrderDate == "" {
		return nil
	}
	on, err := types.DateFromISO(h.OrderDate)
	must(err)
	var errs []doctype.FieldError
	if on.Compare(today) > 0 {
		errs = append(errs, doctype.FieldError{Field: "order_date", Message: msgOrderDateFuture})
	}
	if h.RequiredBy != "" {
		by, err := types.DateFromISO(h.RequiredBy)
		must(err)
		if by.Compare(on) < 0 {
			errs = append(errs, doctype.FieldError{Field: "required_by", Message: msgRequiredByEarly})
		}
	}
	return errs
}

// checkVendor checks the vendor's role (MST's text), refuses an overseas
// vendor, settles the address (CD-176, CD-280 D7 (d)) and the payment terms,
// and fills the vendor snapshots into out. It returns the vendor's GST
// category and state (NUM-038 when unknown: the snapshots are still filled).
// No vendor, or a refused one, leaves out blank.
func checkVendor(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow, out *dbq.SetOrderVendorParams) (category, state string, errs []doctype.FieldError) {
	if h.VendorID == 0 {
		return "", "", nil // a draft may have no vendor yet; submit reports it
	}
	if err := parties.CheckRole(ctx, tx, h.VendorID, party.Vendor); err != nil {
		return "", "", fieldErrors(err, "vendor_id")
	}
	pt, err := parties.Get(ctx, tx, h.VendorID)
	must(err)
	if pt.GSTCategory == gstin.Overseas {
		return "", "", []doctype.FieldError{{Field: "vendor_id", Message: msgOverseasVendor}}
	}
	addr, err := vendorAddress(ctx, tx, h)
	must(err)
	snap, err := addresses.PartySnapshot(ctx, tx, h.VendorID, addr)
	if err != nil {
		return "", "", fieldErrors(err, "vendor_address_id")
	}
	if state, err = gstcalc.SupplierState(snap.GSTIN, snap.StateCode); err != nil {
		errs = append(errs, inputError(err, "vendor_address_id", 0))
	}
	out.SnapVendorState = state
	termsID, err := paymentTerms(ctx, tx, h)
	must(err)
	out.VendorAddressID, out.PaymentTermsID, out.SnapVendorID = nullID(addr), nullID(termsID), nullID(h.VendorID)
	out.SnapVendorCode, out.SnapVendorName, out.SnapVendorGstin = snap.Code, snap.Name, snap.GSTIN
	out.SnapVendorCategory, out.SnapVendorAddress = snap.GSTCategory, snap.AddressText
	return snap.GSTCategory, state, errs
}

// checkSite copies the branch from the site, fills the site and branch
// snapshots into out and applies the site-and-project rule; when the rule
// refuses only the project default is skipped (CD-302). It returns the place
// state, the site's branch's (PO spec §7). No site leaves out blank.
func checkSite(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow, out *dbq.SetOrderSiteParams) (string, []doctype.FieldError) {
	if h.SiteID == 0 {
		return "", nil
	}
	st, err := sites.Get(ctx, tx, h.SiteID)
	must(err)
	br, err := branches.Get(ctx, tx, st.BranchID)
	must(err)
	// A branch always has a state (branch.state_code is NOT NULL and the
	// branch service requires it), so NUM-039 can't be reached here.
	place, err := gstcalc.BranchState(br.Name, br.GSTIN, br.StateCode)
	if err != nil {
		panic(fmt.Errorf("purchase: place state of site %d (branch %d): %w", h.SiteID, st.BranchID, err)) // engine.Do turns it into its plain internal error
	}
	out.BranchID = nullID(st.BranchID)
	out.SnapSiteName, out.SnapBranchName, out.SnapBranchGstin, out.SnapPlaceState = st.Name, br.Name, br.GSTIN, place
	projectID, fe, err := checkSiteProject(ctx, tx, h.SiteID, h.ProjectID)
	must(err)
	if fe != nil {
		return place, []doctype.FieldError{*fe}
	}
	out.ProjectID = nullID(projectID)
	return place, nil
}

// checkSiteProject is the site-and-project rule (PO spec §4), which P5's
// request Validate reuses: a site on a project needs that project, and a
// blank project defaults to it (PO-011). A site with no project, or no site,
// keeps projectID.
func checkSiteProject(ctx context.Context, tx *db.Tx, siteID, projectID int64) (defaulted int64, fe *doc.FieldError, err error) {
	if siteID == 0 {
		return projectID, nil, nil
	}
	st, err := sites.Get(ctx, tx, siteID)
	if err != nil || st.ProjectID == 0 || st.ProjectID == projectID {
		return projectID, nil, err
	}
	if projectID == 0 {
		return st.ProjectID, nil, nil
	}
	pr, err := projects.Get(ctx, tx, st.ProjectID)
	if err != nil {
		return 0, nil, err
	}
	return 0, &doc.FieldError{Field: "project_id", Message: fmt.Sprintf(msgSiteProject, st.Code, pr.Code)}, nil
}

// checkLines applies each line's defaults, checks its HSN/SAC and its UoM (a conversion,
// CD-278; whole numbers through uom.CheckLineQty, CD-277 (4)) and works out
// its net rate, amount and snapshots. ok[i] is false for a line whose own
// check failed. The GST columns are filled from the totals by the caller.
func checkLines(ctx context.Context, tx *db.Tx, h dbq.OrderForValidateRow, projectID int64, rows []dbq.OrderLinesForValidateRow) ([]dbq.SetOrderLineValidatedParams, []bool, []doctype.FieldError) {
	out, ok := make([]dbq.SetOrderLineValidatedParams, len(rows)), make([]bool, len(rows))
	var errs []doctype.FieldError
	for i := range rows {
		l := &rows[i]
		n := int(l.LineNo)
		p := dbq.SetOrderLineValidatedParams{ID: l.ID}
		ok[i] = true
		if l.ItemID != 0 {
			it, err := items.Get(ctx, tx, l.ItemID)
			must(err)
			must(lineDefaults(ctx, tx, h, projectID, it, l))
			if fe := checkLineHSN(n, it.Type, l.HsnSac); fe != nil {
				errs = append(errs, *fe)
			}
			if fe := lineSnapshot(ctx, tx, l, it, &p); fe != nil {
				errs, ok[i] = append(errs, *fe), false
			}
		}
		if l.TaxCodeID != 0 {
			tc, err := taxCodes.Snapshot(ctx, tx, l.TaxCodeID)
			must(err)
			p.SnapTaxCode, p.SnapGstRate = tc.Code, int64(tc.Rate)
		}
		// DOC refuses a discount outside 0 to 100% (NUM-015) before Validate
		// runs, so NetRate's own refusal (rate 0) never reaches the user.
		rate, _ := types.NetRate(types.Rate(l.Price.Int64), types.Percent(l.DiscountPct.Int64))
		amount, err := gstcalc.LineAmount(types.Qty(l.Qty), rate)
		if err != nil {
			errs, ok[i] = append(errs, inputError(err, "qty", n)), false
		}
		p.Description, p.HsnSac, p.UomID, p.TaxCodeID = l.Description, l.HsnSac, nullID(l.UomID), nullID(l.TaxCodeID)
		p.Price, p.DiscountPct, p.AlreadyReceived = l.Price, l.DiscountPct, valid(l.AlreadyReceived)
		p.Rate, p.Amount = valid(int64(rate)), valid(int64(amount))
		out[i] = p
	}
	return out, ok, errs
}

// checkLineHSN checks line n's HSN/SAC code with the item master's rule for
// the item's type (CD-337 (2); PO spec:94): gstin's goods HSN check or its
// SAC check, with gstin's text behind the line prefix.
func checkLineHSN(n int, typ item.ItemType, code string) *doctype.FieldError {
	check := gstin.ValidateGoodsHSN
	if typ == item.TypeService {
		check = gstin.ValidateSAC
	}
	if _, err := check(code); err != nil {
		return &doctype.FieldError{Field: "hsn_sac", Line: n, Message: fmt.Sprintf("Line %d: ", n) + err.Error()}
	}
	return nil
}

// lineSnapshot checks the line's UoM (never blank after lineDefaults: an
// item always has a stock UoM, MST-069) against its item and fills the item
// and UoM snapshots into p.
func lineSnapshot(ctx context.Context, tx *db.Tx, l *dbq.OrderLinesForValidateRow, it item.Item, p *dbq.SetOrderLineValidatedParams) *doctype.FieldError {
	_, ok, err := items.Factor(ctx, tx, l.ItemID, l.UomID)
	must(err)
	if !ok {
		u, err := uoms.Get(ctx, tx, l.UomID)
		must(err)
		return &doctype.FieldError{Field: "uom_id", Line: int(l.LineNo), Message: fmt.Sprintf(msgNoConversion, l.LineNo, it.Code, u.Code)}
	}
	snap, err := items.Snapshot(ctx, tx, l.ItemID, l.UomID)
	must(err)
	qty := types.Qty(l.Qty)
	if fe := uom.CheckLineQty(int(l.LineNo), qty, uom.UoM{Code: snap.UoMCode, WholeNumber: snap.LineWhole},
		uom.UoM{Code: snap.StockUoMCode, WholeNumber: snap.StockWhole}, snap.Factor); fe != nil {
		return fe
	}
	p.SnapItemCode, p.SnapItemName, p.SnapUomCode = snap.Code, snap.Name, snap.UoMCode
	p.SnapFactor, p.SnapStockQty = int64(snap.Factor), int64(uom.ToStock(qty, snap.Factor))
	return nil
}

// checkEarlierPO: Already received on any line, or an earlier PO date, needs
// the earlier PO no. (PO spec §4, §12).
func checkEarlierPO(h dbq.OrderForValidateRow, rows []dbq.OrderLinesForValidateRow) []doctype.FieldError {
	if h.EarlierPoNo != "" {
		return nil
	}
	var errs []doctype.FieldError
	for _, l := range rows {
		if l.AlreadyReceived > 0 {
			errs = append(errs, doctype.FieldError{Field: "earlier_po_no", Message: msgEarlierPONeeded})
			break
		}
	}
	if h.EarlierPoDate != "" {
		errs = append(errs, doctype.FieldError{Field: "earlier_po_no", Message: msgEarlierPOWithDate})
	}
	return errs
}

// beforeSubmitOrder refuses a UoM or tax code that didn't resolve
// (CD-387 (2)), and reverse charge with a Nil-Rated, Exempted or Non-GST line,
// naming each such line (PO spec §4; doc.Invalid, CD-264 (2)). The other rules
// need no second call: the kernel runs Validate again on submit and approve
// (D-APR-15).
func beforeSubmitOrder(ctx context.Context, tx *db.Tx, id int64) error {
	q := dbq.New(tx.SQL())
	h, err := q.OrderForValidate(ctx, id)
	if err != nil {
		return err
	}
	rows, err := q.OrderLinesForValidate(ctx, id)
	if err != nil {
		return err
	}
	dt, _ := doc.Lookup(DocType)
	errs := orderBlanks(dt, rows)
	for _, l := range rows {
		if h.ReverseCharge == 0 || l.TaxCodeID == 0 {
			continue
		}
		tc, err := taxCodes.Get(ctx, tx, l.TaxCodeID)
		if err != nil {
			return err
		}
		switch tc.Treatment {
		case taxcode.NilRated, taxcode.Exempted, taxcode.NonGST:
			errs = append(errs, doctype.FieldError{Field: "tax_code_id", Line: int(l.LineNo), Message: fmt.Sprintf(msgReverseCharge, l.LineNo)})
		}
	}
	if len(errs) > 0 {
		return doc.Invalid(errs)
	}
	return nil
}

// amountOrder is the approval amount (PO spec §5; D-APR-03): the rounded
// total, plus the tax under reverse charge, since the tax is still a cost.
func amountOrder(ctx context.Context, tx *db.Tx, id int64) (types.Money, error) {
	a, err := dbq.New(tx.SQL()).OrderAmount(ctx, id)
	if err != nil {
		return 0, err
	}
	if a.ReverseCharge != 0 {
		return types.Money(a.RoundedTotal + a.Tax), nil
	}
	return types.Money(a.RoundedTotal), nil
}
