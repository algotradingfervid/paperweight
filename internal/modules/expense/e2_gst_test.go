package expense_test

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Spec §5's GST texts.
const (
	msgGSTINRequired   = "Line 1: Supplier GSTIN is required for a bill in the company GSTIN."
	msgInvoiceRequired = "Line 1: Invoice no. is required for a bill in the company GSTIN."
	msgGSTAboveAmount  = "Line 1: GST can't be more than the amount."
)

// TestExp_GSTFields: the company-bill tick needs a supplier GSTIN and an
// invoice no. at submit, not on a draft save; a bad GSTIN is refused on save
// with MST-031's text prefixed `Line 1: ` (CD-142, CD-264 (8)); a good one
// with an invoice no. and Billed to submits.
func TestExp_GSTFields(t *testing.T) { // EXP-011
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	// Both missing: the draft saves, the submit is refused with both texts.
	c := e.DraftClaim(t, deepak, nil, V{"company_bill": "true"})
	var submitErr error
	e.WantRefused(t, msgGSTINRequired, func() error {
		_, submitErr = e.Act(t, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, "")
		return submitErr
	})
	if !e2Has(submitErr, msgInvoiceRequired) {
		t.Errorf("submit messages = %q, want also %q", e2Messages(submitErr), msgInvoiceRequired)
	}

	// A bad GSTIN is refused on a draft save, with MST-031's text.
	const bad = "27AABCS1429B1Z5"
	wantBad := "Line 1: GSTIN 27AABCS1429B1Z5 is not valid: the last character doesn't match. Check for typing mistakes."
	if err := gstin.ValidateGSTIN(bad); err == nil || "Line 1: "+err.Error() != wantBad {
		t.Fatalf("gstin.ValidateGSTIN(%s) = %v; the case expects MST-031's text %q", bad, err, wantBad)
	}
	draft := e.DraftClaim(t, deepak, nil, V{"company_bill": "true"})
	e.WantRefused(t, wantBad, func() error {
		_, err := e.Edit(t, deepak, expense.ClaimDocType, draft.ID, nil, V{"supplier_gstin": bad, "invoice_no": "INV-778"})
		return err
	})
	e.WantRefused(t, wantBad, func() error {
		_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil,
			V{"company_bill": "true", "supplier_gstin": bad, "invoice_no": "INV-778"}))
		return err
	})

	// A good GSTIN, an invoice no. and Billed to PUNE submit.
	e2Must(t, "save the good GSTIN")(e.Edit(t, deepak, expense.ClaimDocType, draft.ID, nil, V{
		"supplier_gstin": "27AABCS1429B1ZU", "invoice_no": "INV-778", "billed_to_id": e2ID(e2Branch(t, e, "PUNE")),
	}))
	res := e2Must(t, "submit with GSTIN, invoice no. and Billed to")(e.Act(t, deepak, expense.ClaimDocType, draft.ID, doc.ActionSubmit, ""))
	if res.State != doc.StatePending {
		t.Errorf("after submit the claim is %q, want pending", res.State)
	}
}

// TestExp_GSTAboveAmount runs through the public save path (no pure function
// is named): gst_amount above the line amount is refused on save; equal and
// below pass.
func TestExp_GSTAboveAmount(t *testing.T) { // EXP-012
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values

	e.WantRefused(t, msgGSTAboveAmount, func() error {
		_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, V{"amount": "1500.00", "gst_amount": "1500.01"}))
		return err
	})
	// The same on an edit of a stored draft.
	c := e.DraftClaim(t, deepak, nil, V{"amount": "1500.00"})
	e.WantRefused(t, msgGSTAboveAmount, func() error {
		_, err := e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil, V{"gst_amount": "1500.01"})
		return err
	})
	for _, gst := range []struct {
		value string
		paise int64
	}{{"1500.00", 150000}, {"160.71", 16071}} {
		res := e2Must(t, "GST "+gst.value+" on ₹1,500.00")(e.Create(deepak, expense.ClaimDocType,
			e.ClaimInput(t, deepak, nil, V{"amount": "1500.00", "gst_amount": gst.value})))
		if l := e2ReadLine(t, e, res.ID, 1); e2Int(l.gstAmount) != gst.paise {
			t.Errorf("stored gst_amount = %d, want %d", e2Int(l.gstAmount), gst.paise)
		}
	}
}

// TestExp_TypeDefaults: a line's blank HSN/SAC and tax code are copied from
// its expense type (expensetype.Defaults); typed values are kept; a disabled
// default tax code gives none (CD-216 item 4). Also the line's type
// snapshots.
func TestExp_TypeDefaults(t *testing.T) { // EXP-013
	ctx := context.Background()
	e := expensetest.New(t)
	deepak, admin := e.P(t, e2Deepak), e.P(t, e2Admin)
	type V = expensetest.Values
	lodging := e.F.ExpenseTypeIDs["LODGING"]

	var gst18, gst5 taxcode.TaxCode
	var et expensetype.ExpenseType
	if err := e.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		var ok5, ok18 bool
		if gst18, ok18, err = e.Masters.TaxCode.ByCode(ctx, tx, "GST18"); err != nil {
			return err
		}
		if gst5, ok5, err = e.Masters.TaxCode.ByCode(ctx, tx, "GST5"); err != nil {
			return err
		}
		if !ok5 || !ok18 {
			t.Fatalf("fixture tax codes GST5 %v, GST18 %v; want both", ok5, ok18)
		}
		et, err = e.Masters.ExpenseType.Get(ctx, tx, lodging)
		return err
	}); err != nil {
		t.Fatalf("read the masters: %v", err)
	}
	// MST-123's setup: LODGING defaults to SAC 996311 and GST18.
	if _, err := e.Masters.ExpenseType.Update(ctx, admin, lodging, et.Version, expensetype.Input{
		Code: et.Code, Name: et.Name, DefaultHSNSAC: "996311", DefaultTaxCodeID: gst18.ID,
	}); err != nil {
		t.Fatalf("set LODGING's defaults: %v", err)
	}

	lodgingLine := V{"expense_type_id": e2Type(t, e, "LODGING"), "hsn_sac": "", "tax_code_id": ""}
	c := e.DraftClaim(t, deepak, nil, lodgingLine)
	if l := e2ReadLine(t, e, c.ID, 1); l.hsnSAC != "996311" || e2Int(l.taxCodeID) != gst18.ID {
		t.Errorf("blank LODGING line = SAC %q, tax code %d; want 996311, GST18 (%d)", l.hsnSAC, e2Int(l.taxCodeID), gst18.ID)
	} else if l.typeCode != "LODGING" || l.typeName != "Lodging" {
		t.Errorf("line type snapshot = %q %q, want LODGING Lodging", l.typeCode, l.typeName)
	}

	// Typed values are kept, on a new line and on an edit.
	typed := e.DraftClaim(t, deepak, nil, V{"expense_type_id": e2Type(t, e, "LODGING"), "hsn_sac": "996332", "tax_code_id": e2ID(gst5.ID)})
	if l := e2ReadLine(t, e, typed.ID, 1); l.hsnSAC != "996332" || e2Int(l.taxCodeID) != gst5.ID {
		t.Errorf("typed LODGING line = SAC %q, tax code %d; want 996332, GST5 (%d)", l.hsnSAC, e2Int(l.taxCodeID), gst5.ID)
	}
	e2Must(t, "edit the defaulted line's values")(e.Edit(t, deepak, expense.ClaimDocType, c.ID, nil,
		V{"hsn_sac": "996332", "tax_code_id": e2ID(gst5.ID)}))
	if l := e2ReadLine(t, e, c.ID, 1); l.hsnSAC != "996332" || e2Int(l.taxCodeID) != gst5.ID {
		t.Errorf("edited LODGING line = SAC %q, tax code %d; want 996332, GST5 (%d)", l.hsnSAC, e2Int(l.taxCodeID), gst5.ID)
	}

	// GST18 disabled: a new line gets no tax code; the SAC is still copied.
	if err := e.Masters.TaxCode.SetDisabled(ctx, admin, gst18.ID, gst18.Version, true); err != nil {
		t.Fatalf("disable GST18: %v", err)
	}
	later := e.DraftClaim(t, deepak, nil, lodgingLine)
	if l := e2ReadLine(t, e, later.ID, 1); l.hsnSAC != "996311" || l.taxCodeID != nil {
		t.Errorf("LODGING line with GST18 disabled = SAC %q, tax code %d; want 996311, none (NULL)", l.hsnSAC, e2Int(l.taxCodeID))
	}
}
