// Package mstfixture holds mst.Order, the test-only document type WP2's tests
// use to call the masters exactly as a real module will (M8). Its tables are
// test-only migrations (package migrations) that msttest.New always applies.
package mstfixture

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name; msttest.New seeds its number series.
const DocType = "mst.Order"

// Hooks has no Env, so these services run over a zero one: their Get,
// CheckRole, Factor and Snapshot paths use only the transaction.
var (
	parties   = party.New(mstkit.Env{})
	addresses = address.New(mstkit.Env{})
	items     = item.New(mstkit.Env{})
	uoms      = uom.New(mstkit.Env{})
)

func init() {
	all := map[string]string{"read": "all", "create": "all", "write": "all", "submit": "all", "cancel": "all", "amend": "all", "delete": "all"}
	doctype.Register(doctype.DocType{
		Name: DocType, Label: "Purchase Order", PluralLabel: "Purchase Orders",
		Table: "mst_order", LinesTable: "mst_order_line", Series: "PO/{FY}/{#####}",
		Submittable:   true, // not Approvable: msttest's engine has no approval service
		ProjectFields: []string{"project_id"},
		SiteFields:    []string{"site_id"},
		DefaultRights: map[string]map[string]string{"Masters Manager": all, "Purchase User": all},
		Fields: []doctype.Field{
			{Name: "vendor_id", Label: "Vendor", Type: doctype.Link, Link: party.DocType, Required: true},
			{Name: "address_id", Label: "Address", Type: doctype.Link, Link: address.DocType},
			{Name: "branch_id", Label: "Branch", Type: doctype.Link, Link: company.DocType},
			{Name: "project_id", Label: "Project", Type: doctype.Link, Link: project.DocType},
			{Name: "site_id", Label: "Site", Type: doctype.Link, Link: site.DocType},
			{Name: "terms_id", Label: "Payment Terms", Type: doctype.Link, Link: paymentterms.DocType},
			{Name: "employee_id", Label: "Employee", Type: doctype.Link, Link: employee.DocType},
			{Name: "order_date", Label: "Order date", Type: doctype.Date, Required: true},

			{Name: "item_id", Label: "Item", Type: doctype.Link, Link: item.DocType, OnLine: true},
			{Name: "uom_id", Label: "UoM", Type: doctype.Link, Link: uom.DocType, OnLine: true},
			{Name: "tax_code_id", Label: "Tax Code", Type: doctype.Link, Link: taxcode.DocType, OnLine: true},
			{Name: "expense_type_id", Label: "Expense Type", Type: doctype.Link, Link: expensetype.DocType, OnLine: true},
			{Name: "qty", Label: "Quantity", Type: doctype.Qty, Required: true, Positive: true, OnLine: true},
			{Name: "rate", Label: "Rate", Type: doctype.Rate, NonNegative: true, OnLine: true},
		},
		Hooks: orderHooks{},
	})
}

// Header is an order's header; a zero id is left blank.
type Header struct {
	VendorID, AddressID, BranchID, ProjectID, SiteID, TermsID, EmployeeID int64
	Date                                                                  types.Date
}

// Line is an order line; a zero id is left blank.
type Line struct {
	ItemID, UoMID, TaxCodeID, ExpenseTypeID int64
	Qty                                     types.Qty
	Rate                                    types.Rate
}

// Snap is what the order stored from its masters when last saved.
type Snap struct {
	VendorName, VendorGSTIN string
	Lines                   []SnapLine
}

// SnapLine is one line's stored snapshot.
type SnapLine struct {
	ItemCode string
	Factor   types.Factor
	StockQty types.Qty
}

func idText(id int64) string {
	if id == 0 {
		return ""
	}
	return strconv.FormatInt(id, 10)
}

// Save saves an order as p: a new one with id 0, otherwise the draft id. This
// test helper reads the draft's current version itself, so a stale save can't
// be expressed through it. Lines are sent without ids, which replaces them, so
// every save of a draft runs Hooks.Validate.
func Save(ctx context.Context, env *msttest.Env, p auth.Principal, id int64, h Header, lines []Line) (doc.Result, error) {
	in := doc.ActionInput{Values: map[string]string{
		"vendor_id": idText(h.VendorID), "address_id": idText(h.AddressID), "branch_id": idText(h.BranchID),
		"project_id": idText(h.ProjectID), "site_id": idText(h.SiteID), "terms_id": idText(h.TermsID),
		"employee_id": idText(h.EmployeeID), "order_date": h.Date.ISO(),
	}}
	for _, l := range lines {
		in.Lines = append(in.Lines, map[string]string{
			"item_id": idText(l.ItemID), "uom_id": idText(l.UoMID), "tax_code_id": idText(l.TaxCodeID),
			"expense_type_id": idText(l.ExpenseTypeID), "qty": l.Qty.Plain(), "rate": l.Rate.Plain(),
		})
	}
	if id != 0 {
		var err error
		if in.Version, err = version(ctx, env, id); err != nil {
			return doc.Result{}, err
		}
	}
	return env.Kit.Engine.Do(ctx, p, DocType, id, doc.ActionSave, in)
}

func version(ctx context.Context, env *msttest.Env, id int64) (int, error) {
	var v int
	err := env.DB.Read(ctx, func(tx *db.Tx) error {
		rec, err := env.Kit.Engine.Load(ctx, tx, DocType, id)
		v = rec.Version
		return err
	})
	return v, err
}

// do runs action a on id as Admin at the current version.
func do(t testing.TB, env *msttest.Env, id int64, a doc.Action, reason string) doc.Result {
	t.Helper()
	ctx := context.Background()
	v, err := version(ctx, env, id)
	if err != nil {
		t.Fatalf("mstfixture: load order %d: %v", id, err)
	}
	res, err := env.Kit.Engine.Do(ctx, env.Admin, DocType, id, a, doc.ActionInput{Version: v, Reason: reason})
	if err != nil {
		t.Fatalf("mstfixture: %s order %d: %v", a, id, err)
	}
	return res
}

// Approve submits the order, which has no approval step, and returns its number.
func Approve(t testing.TB, env *msttest.Env, id int64) string {
	t.Helper()
	return do(t, env, id, doc.ActionSubmit, "").Number
}

// Cancel cancels the approved order.
func Cancel(t testing.TB, env *msttest.Env, id int64) {
	t.Helper()
	do(t, env, id, doc.ActionCancel, "Test cancel")
}

// Snapshot reads the order's stored snapshot columns.
func Snapshot(t testing.TB, env *msttest.Env, id int64) Snap {
	t.Helper()
	ctx := context.Background()
	var s Snap
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		if err := q.QueryRowContext(ctx, `SELECT snap_vendor_name, snap_vendor_gstin FROM mst_order WHERE id = ?`, id).
			Scan(&s.VendorName, &s.VendorGSTIN); err != nil {
			return err
		}
		rows, err := q.QueryContext(ctx, `SELECT snap_item_code, snap_factor, snap_stock_qty FROM mst_order_line WHERE parent_id = ? ORDER BY line_no`, id)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var l SnapLine
			if err := rows.Scan(&l.ItemCode, &l.Factor, &l.StockQty); err != nil {
				return err
			}
			s.Lines = append(s.Lines, l)
		}
		return rows.Err()
	}); err != nil {
		t.Fatalf("mstfixture: snapshot of order %d: %v", id, err)
	}
	return s
}

type orderHooks struct{}

// Validate checks the vendor's role and each line's quantity, and stores the
// snapshots. It runs on save and submit, while the order is a draft.
func (orderHooks) Validate(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	errs := validateVendor(ctx, tx, id)
	lines, err := readLines(ctx, tx, id)
	if err != nil {
		return fieldErrors(err)
	}
	for _, l := range lines {
		errs = append(errs, validateLine(ctx, tx, l)...)
	}
	return errs
}

func validateVendor(ctx context.Context, tx *db.Tx, id int64) []doctype.FieldError {
	var vendorID, addressID int64
	if err := tx.SQL().QueryRowContext(ctx, `SELECT COALESCE(vendor_id, 0), COALESCE(address_id, 0) FROM mst_order WHERE id = ?`, id).
		Scan(&vendorID, &addressID); err != nil {
		return fieldErrors(err)
	}
	if vendorID == 0 {
		return nil // a draft may have no vendor; submit reports the required field
	}
	if err := parties.CheckRole(ctx, tx, vendorID, party.Vendor); err != nil {
		return fieldErrors(err)
	}
	snap, err := addresses.PartySnapshot(ctx, tx, vendorID, addressID)
	if err != nil {
		return fieldErrors(err)
	}
	if _, err := tx.SQL().ExecContext(ctx, `UPDATE mst_order SET snap_vendor_name = ?, snap_vendor_gstin = ? WHERE id = ?`,
		snap.Name, snap.GSTIN, id); err != nil {
		return fieldErrors(err)
	}
	return nil
}

type storedLine struct {
	id, itemID, uomID int64
	lineNo            int
	qty               types.Qty
}

func readLines(ctx context.Context, tx *db.Tx, id int64) ([]storedLine, error) {
	rows, err := tx.SQL().QueryContext(ctx,
		`SELECT id, line_no, COALESCE(item_id, 0), COALESCE(uom_id, 0), COALESCE(qty, 0) FROM mst_order_line WHERE parent_id = ? ORDER BY line_no`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []storedLine
	for rows.Next() {
		var l storedLine
		if err := rows.Scan(&l.id, &l.lineNo, &l.itemID, &l.uomID, &l.qty); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func validateLine(ctx context.Context, tx *db.Tx, l storedLine) []doctype.FieldError {
	if l.itemID == 0 || l.uomID == 0 {
		return nil // an expense line has no item to snapshot
	}
	_, ok, err := items.Factor(ctx, tx, l.itemID, l.uomID)
	if err != nil {
		return fieldErrors(err)
	}
	if !ok {
		return noConversion(ctx, tx, l)
	}
	snap, err := items.Snapshot(ctx, tx, l.itemID, l.uomID)
	if err != nil {
		return fieldErrors(err)
	}
	lineUoM := uom.UoM{Code: snap.UoMCode, WholeNumber: snap.LineWhole}
	stockUoM := uom.UoM{Code: snap.StockUoMCode, WholeNumber: snap.StockWhole}
	if fe := uom.CheckLineQty(l.lineNo, l.qty, lineUoM, stockUoM, snap.Factor); fe != nil {
		return []doctype.FieldError{*fe}
	}
	if _, err := tx.SQL().ExecContext(ctx, `UPDATE mst_order_line SET snap_item_code = ?, snap_factor = ?, snap_stock_qty = ? WHERE id = ?`,
		snap.Code, snap.Factor, uom.ToStock(l.qty, snap.Factor), l.id); err != nil {
		return fieldErrors(err)
	}
	return nil
}

// noConversion is CD-199's refusal for a line whose UoM the item no longer
// converts from.
func noConversion(ctx context.Context, tx *db.Tx, l storedLine) []doctype.FieldError {
	it, err := items.Get(ctx, tx, l.itemID)
	if err != nil {
		return fieldErrors(err)
	}
	u, err := uoms.Get(ctx, tx, l.uomID)
	if err != nil {
		return fieldErrors(err)
	}
	return []doctype.FieldError{{Field: "uom_id", Line: l.lineNo,
		Message: fmt.Sprintf("Row %d: Item %s has no conversion for UoM %s. Pick another UoM.", l.lineNo, it.Code, u.Code)}}
}

// fieldErrors maps a master's *mstkit.ValidationError to its field errors.
// Validate can't return any other error, so this test-only hook panics; the
// engine turns that into a failed action.
func fieldErrors(err error) []doctype.FieldError {
	var ve *mstkit.ValidationError
	if errors.As(err, &ve) {
		return ve.Fields
	}
	panic(err)
}

func (orderHooks) BeforeSubmit(context.Context, *db.Tx, int64) error   { return nil }
func (orderHooks) ApplyEffects(context.Context, *db.Tx, int64) error   { return nil }
func (orderHooks) ReverseEffects(context.Context, *db.Tx, int64) error { return nil }

// Amount is 0: only an approvable DocType's approval rules read it.
func (orderHooks) Amount(context.Context, *db.Tx, int64) (types.Money, error) { return 0, nil }

func (orderHooks) Date(ctx context.Context, tx *db.Tx, id int64) (types.Date, error) {
	var d types.Date
	err := tx.SQL().QueryRowContext(ctx, `SELECT order_date FROM mst_order WHERE id = ?`, id).Scan(&d)
	return d, err
}
