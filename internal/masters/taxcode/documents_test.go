package taxcode_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstfixture"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Integration tests use the canonical fixtures (M7) and mstfixture.Order.

const (
	rateLockedMsg      = "Rate can't change after this tax code is used. Disable it and create a new one."
	treatmentLockedMsg = "Treatment can't change after this tax code is used. Disable it and create a new one."
)

// world is a seeded Env.
type world struct {
	env *msttest.Env
	m   msttest.Masters
	svc *taxcode.Service
}

func newWorld(t *testing.T) world {
	t.Helper()
	env := msttest.New(t)
	return world{env: env, m: msttest.SeedMasters(t, env), svc: taxcode.New(env.Kit)}
}

// draft saves a one-line order of BOLT-M12 taxed with code.
func (w world) draft(t *testing.T, code string) int64 {
	t.Helper()
	res, err := mstfixture.Save(context.Background(), w.env, w.env.Kavya, 0, mstfixture.Header{
		VendorID: w.m.Parties["SHREE"], AddressID: w.m.Addresses["Mumbai"], BranchID: w.m.Branches["PUNE"],
		Date: types.Date{Year: 2026, Month: time.October, Day: 24},
	}, []mstfixture.Line{{ItemID: w.m.Items["BOLT-M12"], UoMID: w.m.UoMs["NOS"], TaxCodeID: w.m.TaxCodes[code], Qty: 1000, Rate: 62_5000}})
	if err != nil {
		t.Fatalf("Save(order with %s): %v", code, err)
	}
	return res.ID
}

// get reads the tax code with code.
func (w world) get(t *testing.T, code string) taxcode.TaxCode {
	t.Helper()
	ctx := context.Background()
	var tc taxcode.TaxCode
	if err := w.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		tc, err = w.svc.Get(ctx, tx, w.m.TaxCodes[code])
		return err
	}); err != nil {
		t.Fatalf("Get(%s): %v", code, err)
	}
	return tc
}

// input is the stored tax code as the page posts it back.
func input(tc taxcode.TaxCode) taxcode.Input {
	return taxcode.Input{Code: tc.Code, Name: tc.Name, Treatment: tc.Treatment, Rate: tc.Rate}
}

func wantStale(t *testing.T, what string, err error) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindStale {
		t.Errorf("%s = %v (%T); want DOC's stale error", what, err, err)
	}
}

func TestTaxCode_RateLocked(t *testing.T) { // MST-099
	w := newWorld(t)
	ctx := context.Background()
	mstfixture.Approve(t, w.env, w.draft(t, "GST12"))

	before := w.get(t, "GST12")
	in := input(before)
	in.Rate = pct(t, "5")
	_, err := w.svc.Update(ctx, w.env.Kavya, before.ID, before.Version, in)
	wantOneError(t, "Update(rate 12 → 5)", err, "rate", rateLockedMsg)
	if got := w.get(t, "GST12"); got != before {
		t.Errorf("GST12 after the refusal = %+v; want %+v", got, before)
	}

	// A stale page gets the conflict, not the lock (CD-180 D4 pattern).
	_, err = w.svc.Update(ctx, w.env.Kavya, before.ID, before.Version-1, in)
	wantStale(t, "Update(rate 12 → 5, stale version)", err)

	// A name change stays allowed.
	named := input(before)
	named.Name = "GST at 12 percent"
	if _, err := w.svc.Update(ctx, w.env.Kavya, before.ID, before.Version, named); err != nil {
		t.Fatalf("Update(name only): %v", err)
	}
	if got := w.get(t, "GST12"); got.Name != named.Name || got.Rate != before.Rate {
		t.Errorf("GST12 after the rename = %+v; want name %q, rate unchanged", got, named.Name)
	}

	// A cancelled order still counts.
	cancelled := w.draft(t, "GST28")
	mstfixture.Approve(t, w.env, cancelled)
	mstfixture.Cancel(t, w.env, cancelled)
	gst28 := w.get(t, "GST28")
	in = input(gst28)
	in.Rate = pct(t, "18")
	_, err = w.svc.Update(ctx, w.env.Kavya, gst28.ID, gst28.Version, in)
	wantOneError(t, "Update(GST28, cancelled order)", err, "rate", rateLockedMsg)

	// Only a draft uses it: the change is allowed.
	w.draft(t, "GST5")
	gst5 := w.get(t, "GST5")
	in = input(gst5)
	in.Rate = pct(t, "12")
	if _, err := w.svc.Update(ctx, w.env.Kavya, gst5.ID, gst5.Version, in); err != nil {
		t.Errorf("Update(GST5, only a draft): %v", err)
	}
}

// TestTaxCode_TreatmentLocked (MST-099's title, CD-199 item 3): Exempted to
// Nil-Rated keeps rate 0, so only the treatment itself shows the change.
func TestTaxCode_TreatmentLocked(t *testing.T) {
	w := newWorld(t)
	ctx := context.Background()
	mstfixture.Approve(t, w.env, w.draft(t, "EXEMPT"))

	before := w.get(t, "EXEMPT")
	in := input(before)
	in.Treatment = taxcode.NilRated
	_, err := w.svc.Update(ctx, w.env.Kavya, before.ID, before.Version, in)
	wantOneError(t, "Update(Exempted → Nil-Rated)", err, "treatment", treatmentLockedMsg)
	if got := w.get(t, "EXEMPT"); got != before {
		t.Errorf("EXEMPT after the refusal = %+v; want %+v", got, before)
	}

	_, err = w.svc.Update(ctx, w.env.Kavya, before.ID, before.Version-1, in)
	wantStale(t, "Update(Exempted → Nil-Rated, stale version)", err)

	// Changing both reports both.
	mstfixture.Approve(t, w.env, w.draft(t, "GST12"))
	gst12 := w.get(t, "GST12")
	in = input(gst12)
	in.Treatment = taxcode.Exempted
	_, err = w.svc.Update(ctx, w.env.Kavya, gst12.ID, gst12.Version, in)
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("Update(Taxable 12 → Exempted) = %v (%T); want *mstkit.ValidationError", err, err)
	}
	want := []doctype.FieldError{{Field: "rate", Message: rateLockedMsg}, {Field: "treatment", Message: treatmentLockedMsg}}
	if got := ve.FieldErrors(); !reflect.DeepEqual(got, want) {
		t.Errorf("FieldErrors() = %+v; want %+v", got, want)
	}

	// Only a draft uses it: the change is allowed.
	w.draft(t, "NIL")
	nilRated := w.get(t, "NIL")
	in = input(nilRated)
	in.Treatment = taxcode.Exempted
	if _, err := w.svc.Update(ctx, w.env.Kavya, nilRated.ID, nilRated.Version, in); err != nil {
		t.Errorf("Update(NIL, only a draft): %v", err)
	}
}
