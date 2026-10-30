package doc_test

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// CD-205: step-scoped draft saves and Engine.Problems, on the real engine.

const (
	osWireBlocked = "Binding Wire 18G is blocked for purchase."
	osRajkotHold  = "Rajkot Castings is on hold. Ask accounts before ordering."
	osHookText    = "Supplier ref doesn't match the quote."
	osFormText    = "This order can't be saved yet."
	osMissing     = "99999"
)

// osOrder is a draft from RAJKOT with line 1 CEMENT-OPC and line 2 WIRE-18G.
func osOrder(t *testing.T, h *fixturetest.H) doc.Doc {
	t.Helper()
	res := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": lkID(h.M.Party["RAJKOT"])},
		fixturetest.OL{Item: "CEMENT-OPC", Qty: "10"}, fixturetest.OL{Item: "WIRE-18G", Qty: "5"})
	return loadDoc(t, h, fixture.Order, res.ID)
}

// osInput is d as an input, scoped to only.
func osInput(h *fixturetest.H, d doc.Doc, only ...string) doc.ActionInput {
	in := h.In(d)
	in.Only = only
	return in
}

// osUnwritten fails unless the document is d and the audit log has count rows.
func osUnwritten(t *testing.T, h *fixturetest.H, d doc.Doc, count int) {
	t.Helper()
	fixturetest.Eq(t, loadDoc(t, h, d.DocType, d.ID), d)
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d, want %d (nothing written)", n, count)
	}
}

// osHasField fails unless err's Fields hold want.
func osHasField(t *testing.T, err error, want doc.FieldError) {
	t.Helper()
	de := wantDocErr(t, err, doc.KindInvalid, lkMsgFixFields)
	if !slices.Contains(de.Fields, want) {
		t.Errorf("Fields = %#v, want them to hold %#v", de.Fields, want)
	}
}

// osSorted orders field errors by line, field and message, for comparing two
// lists whose order isn't the point.
func osSorted(errs []doc.FieldError) []doc.FieldError {
	out := slices.Clone(errs)
	slices.SortFunc(out, func(a, b doc.FieldError) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Field, b.Field), cmp.Compare(a.Message, b.Message))
	})
	return out
}

// osProblems runs Engine.Problems in a read transaction, as a page does.
func osProblems(t *testing.T, h *fixturetest.H, dt string, id int64) ([]doc.FieldError, error) {
	t.Helper()
	var (
		got []doc.FieldError
		err error
	)
	if rerr := h.DB.Read(t.Context(), func(tx *db.Tx) error {
		got, err = h.Engine.Problems(t.Context(), tx, dt, id)
		return nil
	}); rerr != nil {
		t.Fatalf("read transaction: %v", rerr)
	}
	return got, err
}

// osBroken lists the ways a stored value of osOrder goes bad after it was
// saved, with the error DOC gives for it.
var osBroken = []struct {
	name    string
	breakIt func(t *testing.T, h *fixturetest.H)
	want    doc.FieldError
}{
	{
		name:    "disabled supplier",
		breakIt: func(t *testing.T, h *fixturetest.H) { fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true) },
		want:    doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled},
	},
	{
		name: "blocked supplier",
		breakIt: func(t *testing.T, h *fixturetest.H) {
			fixture.SetBlocked(t, h.DB, "fixture_party", "RAJKOT", osRajkotHold)
		},
		want: doc.FieldError{Field: "supplier_id", Message: osRajkotHold},
	},
	{
		name:    "disabled item on line 1",
		breakIt: func(t *testing.T, h *fixturetest.H) { fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true) },
		want:    doc.FieldError{Field: "item_id", Line: 1, Message: lkMsgItemDisabled},
	},
	{
		name: "blocked item on line 2",
		breakIt: func(t *testing.T, h *fixturetest.H) {
			fixture.SetBlocked(t, h.DB, "fixture_item", "WIRE-18G", osWireBlocked)
		},
		want: doc.FieldError{Field: "item_id", Line: 2, Message: "Line 2: " + osWireBlocked},
	},
}

// A master problem on a field outside Only doesn't stop the write: the typed
// value is stored, the version bumps, and the problem returns in
// Result.Problems with DOC's exact text.
func TestDoc_Only_OutOfScopeMasterCommits(t *testing.T) {
	for _, c := range osBroken {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := osOrder(t, h)
			c.breakIt(t, h)

			in := osInput(h, d, "remarks")
			in.Values["remarks"] = "typed on this step"
			res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
			if err != nil {
				t.Fatalf("scoped save: %v, want nil", err)
			}
			if res.ID != d.ID || res.State != doc.StateDraft || res.Version != d.Version+1 {
				t.Errorf("Result = {ID:%d State:%s Version:%d}, want {%d draft %d}", res.ID, res.State, res.Version, d.ID, d.Version+1)
			}
			fixturetest.Eq(t, res.Problems, []doc.FieldError{c.want})

			after := loadDoc(t, h, fixture.Order, d.ID)
			if after.Version != d.Version+1 {
				t.Errorf("stored Version = %d, want %d", after.Version, d.Version+1)
			}
			if got := h.Record(t, fixture.Order, d.ID).Values["remarks"]; got != "typed on this step" {
				t.Errorf("stored remarks = %q, want the typed value", got)
			}
			fixturetest.Eq(t, after.Header["supplier_id"], d.Header["supplier_id"])
			if len(after.Lines) != len(d.Lines) {
				t.Errorf("stored lines = %d, want %d", len(after.Lines), len(d.Lines))
			}
		})
	}
}

// Every out-of-scope problem returns, not just the first.
func TestDoc_Only_AllOutOfScopeProblemsReturn(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	for _, c := range osBroken[2:] {
		c.breakIt(t, h)
	}
	osBroken[0].breakIt(t, h)
	probe := &fixture.Probe{ValidateErrors: []doc.FieldError{{Field: "supplier_ref", Message: osHookText}}}
	in := osInput(h, d, "remarks")
	in.Values["remarks"] = "typed"
	res, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("scoped save: %v, want nil", err)
	}
	want := []doc.FieldError{
		{Field: "supplier_id", Message: lkMsgSupplierDisabled},
		{Field: "supplier_ref", Message: osHookText},
		{Field: "item_id", Line: 1, Message: lkMsgItemDisabled},
		{Field: "item_id", Line: 2, Message: "Line 2: " + osWireBlocked},
	}
	fixturetest.Eq(t, osSorted(res.Problems), osSorted(want))
}

// A Hooks.Validate field error outside Only is a problem, not a refusal.
func TestDoc_Only_OutOfScopeHookErrorCommits(t *testing.T) {
	for _, c := range []struct {
		name string
		fe   doc.FieldError
	}{
		{"header field", doc.FieldError{Field: "supplier_ref", Message: osHookText}},
		{"line field", doc.FieldError{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."}},
		{"the lines as a whole", doc.FieldError{Field: "lines", Message: "Add at least one line."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := osOrder(t, h)
			probe := &fixture.Probe{ValidateErrors: []doc.FieldError{c.fe}}
			in := osInput(h, d, "remarks")
			in.Values["remarks"] = "typed"
			res, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
			if err != nil {
				t.Fatalf("scoped save: %v, want nil", err)
			}
			fixturetest.Eq(t, res.Problems, []doc.FieldError{c.fe})
			if res.Version != d.Version+1 {
				t.Errorf("Result.Version = %d, want %d", res.Version, d.Version+1)
			}
			if got := h.Record(t, fixture.Order, d.ID).Values["remarks"]; got != "typed" {
				t.Errorf("stored remarks = %q, want the typed value", got)
			}
		})
	}
}

// An error on a field in Only refuses as a save does, and nothing is written,
// also when an out-of-scope problem is there too.
func TestDoc_Only_InScopeErrorRefuses(t *testing.T) {
	longRemarks := strings.Repeat("x", 501)
	cases := []struct {
		name    string
		only    []string
		disable bool
		edit    func(in *doc.ActionInput)
		hook    []doc.FieldError
		want    doc.FieldError
	}{
		{
			name: "stored disabled master", only: []string{"supplier_id", "remarks"}, disable: true,
			edit: func(in *doc.ActionInput) { in.Values["remarks"] = "typed" },
			want: doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled},
		},
		{
			name: "unchanged disabled master", only: []string{"supplier_id"}, disable: true,
			edit: func(*doc.ActionInput) {},
			want: doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled},
		},
		{
			name: "range", only: []string{"credit_days"},
			edit: func(in *doc.ActionInput) { in.Values["credit_days"] = "400" },
			want: doc.FieldError{Field: "credit_days", Message: "Credit days must be between 0 and 365."},
		},
		{
			name: "length", only: []string{"remarks"},
			edit: func(in *doc.ActionInput) { in.Values["remarks"] = longRemarks },
			want: doc.FieldError{Field: "remarks", Message: "Remarks can be at most 500 characters."},
		},
		{
			name: "hook error", only: []string{"supplier_ref", "remarks"},
			edit: func(in *doc.ActionInput) { in.Values["remarks"] = "typed" },
			hook: []doc.FieldError{{Field: "supplier_ref", Message: osHookText}},
			want: doc.FieldError{Field: "supplier_ref", Message: osHookText},
		},
		{
			name: "in scope and out of scope together", only: []string{"credit_days"}, disable: true,
			edit: func(in *doc.ActionInput) { in.Values["credit_days"] = "400" },
			want: doc.FieldError{Field: "credit_days", Message: "Credit days must be between 0 and 365."},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := osOrder(t, h)
			if c.disable {
				fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
			}
			count := h.AuditCount(t)
			in := osInput(h, d, c.only...)
			c.edit(&in)
			probe := &fixture.Probe{ValidateErrors: c.hook}
			_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
			osHasField(t, err, c.want)
			osUnwritten(t, h, d, count)
		})
	}
}

// CD-199's no-change branch follows the scope: unchanged values with only
// out-of-scope problems are no refusal and no write (DOC-029), and the
// problems still return, so Next on an untouched step moves.
func TestDoc_Only_NoChangeReturnsProblems(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	count := h.AuditCount(t)

	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, osInput(h, d, "remarks"))
	if err != nil {
		t.Fatalf("scoped no-change save: %v, want nil", err)
	}
	if res.Version != d.Version {
		t.Errorf("Result.Version = %d, want %d (no write)", res.Version, d.Version)
	}
	fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
	osUnwritten(t, h, d, count)
}

// Parse errors, read-only errors, a missing link and any error with no field
// name refuse with Only set, even when they sit outside it, and nothing is
// written.
func TestDoc_Only_StillRefuses(t *testing.T) {
	rows := []struct {
		name string
		edit func(h *fixturetest.H, in *doc.ActionInput)
		hook []doc.FieldError
		fail map[string]error
		want doc.FieldError
	}{
		{
			name: "parse error on a header field",
			edit: func(_ *fixturetest.H, in *doc.ActionInput) { in.Values["credit_days"] = "abc" },
			want: doc.FieldError{Field: "credit_days", Message: "Credit days isn't a valid value. Check it and try again."},
		},
		{
			name: "parse error on a line field",
			edit: func(_ *fixturetest.H, in *doc.ActionInput) { in.Lines[0]["qty"] = "abc" },
			want: doc.FieldError{Field: "qty", Line: 1, Message: "Line 1: Quantity isn't a valid value. Check it and try again."},
		},
		{
			name: "unreadable line id",
			edit: func(_ *fixturetest.H, in *doc.ActionInput) { in.Lines[0]["id"] = "x" },
			want: doc.FieldError{Field: "id", Line: 1, Message: "Line 1: Line id isn't a valid value. Check it and try again."},
		},
		{
			name: "missing master on the header",
			edit: func(_ *fixturetest.H, in *doc.ActionInput) { in.Values["project_id"] = osMissing },
			want: doc.FieldError{Field: "project_id", Message: "Project not found. Pick one from the list."},
		},
		{
			name: "missing master on a line",
			edit: func(_ *fixturetest.H, in *doc.ActionInput) { in.Lines[1]["item_id"] = osMissing },
			want: doc.FieldError{Field: "item_id", Line: 2, Message: "Line 2: Item not found. Pick one from the list."},
		},
		{
			name: "hook error with no field name",
			hook: []doc.FieldError{{Message: osFormText}},
			want: doc.FieldError{Message: osFormText},
		},
		{
			name: "hook error with a line but no field name",
			hook: []doc.FieldError{{Line: 1, Message: osFormText}},
			want: doc.FieldError{Line: 1, Message: osFormText},
		},
		{
			name: "failed hook surfaces with no field name",
			fail: map[string]error{"Validate": errors.New(osFormText)},
			want: doc.FieldError{Message: osFormText},
		},
	}
	for _, c := range rows {
		for _, only := range [][]string{{"remarks"}, {"lines"}} {
			if len(only) == 1 && only[0] == "lines" && c.want.Line > 0 {
				continue // a line error is in scope there
			}
			t.Run(c.name+"/"+only[0], func(t *testing.T) {
				h := fixturetest.New(t)
				d := osOrder(t, h)
				// Without the row's error this save commits past the
				// out-of-scope supplier, so the refusal below is the row's.
				fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
				in := osInput(h, d, only...)
				in.Values["remarks"] = "typed"
				if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in); err != nil {
					t.Fatalf("scoped save without the row's error: %v, want nil", err)
				}
				d = loadDoc(t, h, fixture.Order, d.ID)
				count := h.AuditCount(t)
				in = osInput(h, d, only...)
				in.Values["remarks"] = "typed again"
				if c.edit != nil {
					c.edit(h, &in)
				}
				probe := &fixture.Probe{ValidateErrors: c.hook, Fail: c.fail}
				_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
				osHasField(t, err, c.want)
				osUnwritten(t, h, d, count)
			})
		}
	}

	t.Run("read-only error", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewClaim(t, h.Deepak, "EMP-007", fixturetest.CL{Type: "TRAVEL", Amount: "1250.00"})
		d := loadDoc(t, h, fixture.Claim, res.ID)
		count := h.AuditCount(t)
		in := osInput(h, d, "remarks")
		in.Values["remarks"] = "typed"
		in.Values["total"] = "999.00"
		_, err := h.Do(t, t.Context(), h.Deepak, fixture.Claim, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "total", Message: "Total is calculated and can't be entered."})
		osUnwritten(t, h, d, count)
	})

	t.Run("foreign line id", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		in := osInput(h, d, "remarks")
		in.Values["remarks"] = "typed"
		in.Lines[0]["id"] = osMissing
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		fixturetest.ErrIs(t, err, doc.KindRefused, "Line 1 doesn't belong to this document. Reload and try again.")
		osUnwritten(t, h, d, count)
	})
}

// An unknown name in Only is a caller bug (CD-147): a plain internal error
// that Classify can't place, so 500, never a FieldError or a user message,
// and it stops the save whether or not the draft has any error.
func TestDoc_Only_UnknownNameIsInternal(t *testing.T) {
	for _, c := range []struct {
		name string
		only []string
	}{
		{"unknown", []string{"nope"}},
		{"unknown after a known name", []string{"remarks", "nope"}},
		{"empty name", []string{""}},
		{"case matters", []string{"Lines"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			d := osOrder(t, h)
			count := h.AuditCount(t)
			in := osInput(h, d, c.only...)
			in.Values["remarks"] = "typed"
			_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
			if err == nil {
				t.Fatal("save with an unknown Only name succeeded, want an internal error")
			}
			var he doctype.HTTPError
			if errors.As(err, &he) {
				t.Errorf("err = %v (%T) carries HTTP status %d, want a plain error (500 by Classify)", err, err, he.Status())
			}
			var de *doc.Error
			if errors.As(err, &de) {
				t.Errorf("err = %#v, want no *doc.Error: this is no user message", de)
			}
			if errors.Is(err, doctype.ErrNotFound) {
				t.Errorf("err = %v is not found, want an internal error", err)
			}
			osUnwritten(t, h, d, count)
		})
	}

	t.Run("no change still refused", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, osInput(h, d, "nope")); err == nil {
			t.Fatal("no-change save with an unknown Only name succeeded, want an internal error")
		}
	})
}

// CD-205 gives Only as "field names, plus lines" and calls anything else
// unknown. A line field is a field of the DocType, so its name is known and
// not an internal error; like any other name it scopes the errors on that
// field, and like "lines" only on the lines this save changes.
func TestDoc_Only_LineFieldNameIsKnown(t *testing.T) {
	t.Run("in scope on a changed line", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		count := h.AuditCount(t)
		in := osInput(h, d, "item_id")
		in.Lines[0]["description"] = "typed"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "item_id", Line: 1, Message: lkMsgItemDisabled})
		osUnwritten(t, h, d, count)
	})
	t.Run("out of scope on an unchanged line", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "item_id")
		in.Lines[1]["description"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("scoped save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 1, Message: lkMsgItemDisabled}})
	})
	t.Run("another line field is out of scope", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "qty")
		in.Lines[0]["qty"] = "12.000"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("scoped save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 1, Message: lkMsgItemDisabled}})
	})
}

// osApproved is an approved order with one line.
func osApproved(t *testing.T, h *fixturetest.H) doc.Doc {
	t.Helper()
	return h.ApprovedOrder(t, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10"})
}

// osCancel moves order o to cancelled, keeping its number.
func osCancel(t *testing.T, h *fixturetest.H, o doc.Doc) {
	t.Helper()
	h.ForceState(t, fixture.Order, o.ID, doc.StateCancelled, o.Number)
}

// osSourceCancelled is DOC's error on receipt line n pointing at a cancelled order.
func osSourceCancelled(n int, number string) doc.FieldError {
	return doc.FieldError{Field: "order_line_id", Line: n, Message: fmt.Sprintf(
		"Line %d: %s is cancelled. Remove the line or pick another purchase order.", n, number)}
}

// osBadReceipt is a draft receipt with both lines on orders since cancelled.
func osBadReceipt(t *testing.T, h *fixturetest.H) (d doc.Doc, first, second doc.Doc) {
	t.Helper()
	first, second = osApproved(t, h), osApproved(t, h)
	res := h.NewReceipt(t, h.Priya,
		fixturetest.RL{OrderLineID: first.Lines[0].ID, Qty: "1"}, fixturetest.RL{OrderLineID: second.Lines[0].ID, Qty: "1"})
	osCancel(t, h, first)
	osCancel(t, h, second)
	return loadDoc(t, h, fixture.Receipt, res.ID), first, second
}

// osDisabledItem is DOC's text for a disabled item on line n.
func osDisabledItem(n int, display string) string {
	return fmt.Sprintf("Line %d: Item %s is disabled. Pick an active item, or ask an admin to enable it.", n, display)
}

const (
	osCement = "CEMENT-OPC OPC Cement 53 Grade"
	osWire   = "WIRE-18G Binding Wire 18G"
	osBolt   = "BOLT-M12 Hex Bolt M12x60"
)

// "lines" in Only scopes errors to the lines this save changes: added, or with
// a changed field value. A line left unchanged or only moved keeps its stored
// errors out of scope, and they return in Problems; removing a line refuses
// nothing about the lines left. A header error is out of scope too.
func TestDoc_Only_Lines(t *testing.T) {
	t.Run("a changed line's own error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines[0]["description"] = "typed"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "item_id", Line: 1, Message: lkMsgItemDisabled})
		osUnwritten(t, h, d, count)
	})
	t.Run("a typed line error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines[1]["qty"] = "0"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "qty", Line: 2, Message: "Line 2: Quantity must be more than 0."})
		osUnwritten(t, h, d, count)
	})
	t.Run("a hook line error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		fe := doc.FieldError{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."}
		in := osInput(h, d, "lines")
		in.Lines[0]["description"] = "typed"
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, fe)
		osUnwritten(t, h, d, count)
	})
	t.Run("a hook error on the lines as a whole refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		fe := doc.FieldError{Field: "lines", Message: "Add at least one line."}
		in := osInput(h, d, "lines")
		in.Lines[0]["description"] = "typed"
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, fe)
		osUnwritten(t, h, d, count)
	})
	t.Run("a header error doesn't stop a line save", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		in := osInput(h, d, "lines")
		in.Lines[0]["description"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
		if got := h.Record(t, fixture.Order, d.ID).Lines[0]["description"]; got != "typed" {
			t.Errorf("stored description = %q, want the typed value", got)
		}
	})
	t.Run("a header error doesn't stop a reorder", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		in := osInput(h, d, "lines")
		in.Lines[0], in.Lines[1] = in.Lines[1], in.Lines[0]
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("reorder: %v, want nil", err)
		}
		if res.Version != d.Version+1 {
			t.Errorf("Result.Version = %d, want %d", res.Version, d.Version+1)
		}
		after := loadDoc(t, h, fixture.Order, d.ID)
		if after.Lines[0].ID != d.Lines[1].ID || after.Lines[1].ID != d.Lines[0].ID {
			t.Errorf("stored line ids = [%d %d], want them swapped from [%d %d]", after.Lines[0].ID, after.Lines[1].ID, d.Lines[0].ID, d.Lines[1].ID)
		}
	})
	t.Run("an added line's error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "BOLT-M12", true)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines = append(in.Lines, map[string]string{"item_id": lkID(h.M.Item["BOLT-M12"]), "qty": "1.000"})
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "item_id", Line: 3, Message: osDisabledItem(3, osBolt)})
		osUnwritten(t, h, d, count)
	})
	t.Run("an edit to another row commits past a stored line error", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "lines")
		in.Lines[1]["description"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 1, Message: lkMsgItemDisabled}})
		if res.Version != d.Version+1 {
			t.Errorf("Result.Version = %d, want %d", res.Version, d.Version+1)
		}
		if got := h.Record(t, fixture.Order, d.ID).Lines[1]["description"]; got != "typed" {
			t.Errorf("stored description = %q, want the typed value", got)
		}
	})
	t.Run("fixing one bad row commits past another", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		fixture.SetDisabled(t, h.DB, "fixture_item", "WIRE-18G", true)
		in := osInput(h, d, "lines")
		in.Lines[0]["item_id"] = lkID(h.M.Item["BOLT-M12"])
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 2, Message: osDisabledItem(2, osWire)}})
	})
	t.Run("an equal value in another format is no change", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "lines")
		in.Lines[0]["qty"] = "10" // stored as 10.000
		in.Lines[1]["description"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 1, Message: lkMsgItemDisabled}})
	})
	t.Run("a reorder commits past a stored line error", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "lines")
		in.Lines[0], in.Lines[1] = in.Lines[1], in.Lines[0]
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("reorder: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 2, Message: osDisabledItem(2, osCement)}})
		if res.Version != d.Version+1 {
			t.Errorf("Result.Version = %d, want %d", res.Version, d.Version+1)
		}
		after := loadDoc(t, h, fixture.Order, d.ID)
		if after.Lines[0].ID != d.Lines[1].ID || after.Lines[1].ID != d.Lines[0].ID {
			t.Errorf("stored line ids = [%d %d], want them swapped from [%d %d]", after.Lines[0].ID, after.Lines[1].ID, d.Lines[0].ID, d.Lines[1].ID)
		}
	})
	t.Run("a moved line that also changed refuses at its new number", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines[0]["description"] = "typed"
		in.Lines[0], in.Lines[1] = in.Lines[1], in.Lines[0]
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "item_id", Line: 2, Message: osDisabledItem(2, osCement)})
		osUnwritten(t, h, d, count)
	})
	t.Run("a removal commits past the lines left", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		fixture.SetDisabled(t, h.DB, "fixture_item", "WIRE-18G", true)
		in := osInput(h, d, "lines")
		in.Lines = in.Lines[1:]
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("removal: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "item_id", Line: 1, Message: osDisabledItem(1, osWire)}})
		if got := h.Record(t, fixture.Order, d.ID).Lines; len(got) != 1 {
			t.Errorf("stored lines = %d, want 1", len(got))
		}
	})
	t.Run("a hook error on an unchanged line is a problem", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fe := doc.FieldError{Field: "rate", Line: 1, Message: "Line 1: Rate must be more than 0."}
		in := osInput(h, d, "lines")
		in.Lines[1]["description"] = "typed"
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
		res, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{fe})
	})
	t.Run("a hook line error follows the saved line numbers", func(t *testing.T) {
		// Line 1 below is first moved to 2 and edited; the hook reads the saved
		// document, so its Line 2 is that edited line and its Line 1 isn't.
		for _, c := range []struct {
			line   int
			refuse bool
		}{{1, false}, {2, true}} {
			h := fixturetest.New(t)
			d := osOrder(t, h)
			count := h.AuditCount(t)
			fe := doc.FieldError{Field: "rate", Line: c.line, Message: fmt.Sprintf("Line %d: Rate must be more than 0.", c.line)}
			in := osInput(h, d, "lines")
			in.Lines[0]["description"] = "typed"
			in.Lines[0], in.Lines[1] = in.Lines[1], in.Lines[0]
			probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
			res, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
			if c.refuse {
				osHasField(t, err, fe)
				osUnwritten(t, h, d, count)
				continue
			}
			if err != nil {
				t.Fatalf("hook error on line %d: %v, want nil", c.line, err)
			}
			fixturetest.Eq(t, res.Problems, []doc.FieldError{fe})
		}
	})
	t.Run("a changed link to a disabled master refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_item", "BOLT-M12", true)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines[1]["item_id"] = lkID(h.M.Item["BOLT-M12"])
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		osHasField(t, err, doc.FieldError{Field: "item_id", Line: 2, Message: osDisabledItem(2, osBolt)})
		osUnwritten(t, h, d, count)
	})
	t.Run("a duplicate line id refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		in := osInput(h, d, "lines")
		in.Lines[1]["id"] = in.Lines[0]["id"]
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		fixturetest.ErrIs(t, err, doc.KindRefused, "Line 2 doesn't belong to this document. Reload and try again.")
		osUnwritten(t, h, d, count)
	})
	// F1: a stored source error on one line must not hide a changed line's,
	// whichever comes first.
	for _, c := range []struct {
		name          string
		stale, edited int // 0-based positions of the unchanged bad line and the changed line
	}{
		{"a stored source error on line 1 doesn't hide changed line 2", 0, 1},
		{"a stored source error on line 2 doesn't hide changed line 1", 1, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			cancelled, good, other := osApproved(t, h), osApproved(t, h), osApproved(t, h)
			rl := make([]fixturetest.RL, 2)
			rl[c.stale] = fixturetest.RL{OrderLineID: cancelled.Lines[0].ID, Qty: "1"}
			rl[c.edited] = fixturetest.RL{OrderLineID: good.Lines[0].ID, Qty: "1"}
			res := h.NewReceipt(t, h.Priya, rl...)
			osCancel(t, h, cancelled)
			osCancel(t, h, other)
			d := loadDoc(t, h, fixture.Receipt, res.ID)
			count := h.AuditCount(t)

			in := osInput(h, d, "lines")
			in.Lines[c.edited]["order_line_id"] = lkID(other.Lines[0].ID)
			_, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSave, in)
			osHasField(t, err, osSourceCancelled(c.edited+1, other.Number))
			osUnwritten(t, h, d, count)
		})
	}
	t.Run("a bad line can be replaced", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		in := osInput(h, d, "lines")
		in.Lines[0]["item_id"] = lkID(h.M.Item["BOLT-M12"])
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("line save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
		if got := h.Record(t, fixture.Order, d.ID).Lines[0]["item_id"]; got != lkID(h.M.Item["BOLT-M12"]) {
			t.Errorf("stored item_id = %q, want the replacement", got)
		}
	})
}

// A nil Only keeps CD-199 with a typed change too: every error refuses and
// Problems stays empty. TestDoc_Save_NoOp_MasterRefused covers the no-change
// half; this adds the changed save and a hook error.
func TestDoc_Only_NilKeepsCD199(t *testing.T) {
	t.Run("typed change, disabled master", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		count := h.AuditCount(t)
		in := h.In(d)
		in.Values["remarks"] = "typed"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		lkInvalid(t, err, doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled})
		osUnwritten(t, h, d, count)
	})
	t.Run("no change, disabled master", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, h.In(d))
		lkInvalid(t, err, doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled})
		osUnwritten(t, h, d, count)
	})
	t.Run("hook field error", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		count := h.AuditCount(t)
		fe := doc.FieldError{Field: "supplier_ref", Message: osHookText}
		in := h.In(d)
		in.Values["remarks"] = "typed"
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		lkInvalid(t, err, fe)
		osUnwritten(t, h, d, count)
	})
	t.Run("a clean save has no Problems", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		in := h.In(d)
		in.Values["remarks"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("save: %v", err)
		}
		if len(res.Problems) != 0 {
			t.Errorf("Problems = %#v, want none", res.Problems)
		}
	})
}

// "Create, submit and revalidate are unchanged": those paths ignore Only, so
// it neither loosens their checks nor refuses them. The CD gives no refusal
// for them, and an ignored Only leaves them byte for byte as they were.
func TestDoc_Only_CreateIgnoresIt(t *testing.T) {
	t.Run("an out-of-scope error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		rows, count := orderRows(t, h), h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{"supplier_id": lkID(h.M.Party["RAJKOT"]), "remarks": "typed"},
			Only:   []string{"remarks"},
		})
		lkInvalid(t, err, doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled})
		if n := orderRows(t, h); n != rows {
			t.Errorf("order rows = %d, want %d", n, rows)
		}
		if n := h.AuditCount(t); n != count {
			t.Errorf("AuditCount = %d, want %d", n, count)
		}
	})
	t.Run("a hook field error refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		rows := orderRows(t, h)
		fe := doc.FieldError{Field: "supplier_ref", Message: osHookText}
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{fe}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{"supplier_id": lkID(h.M.Party["SHREE"]), "remarks": "typed"},
			Only:   []string{"remarks"},
		})
		lkInvalid(t, err, fe)
		if n := orderRows(t, h); n != rows {
			t.Errorf("order rows = %d, want %d", n, rows)
		}
	})
	t.Run("a clean create succeeds", func(t *testing.T) {
		h := fixturetest.New(t)
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
			Values: map[string]string{"supplier_id": lkID(h.M.Party["SHREE"])},
			Only:   []string{"remarks"},
		})
		if err != nil {
			t.Fatalf("create: %v, want nil", err)
		}
		if res.Version != 1 || len(res.Problems) != 0 {
			t.Errorf("Result = %+v, want version 1 and no Problems", res)
		}
	})
}

func TestDoc_Only_SubmitIgnoresIt(t *testing.T) {
	t.Run("an out-of-scope disabled master refuses", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit,
			doc.ActionInput{Version: d.Version, Only: []string{"remarks"}})
		lkInvalid(t, err, doc.FieldError{Field: "supplier_id", Message: lkMsgSupplierDisabled})
		osUnwritten(t, h, d, count)
	})
	t.Run("every error refuses together", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": ""}, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10"})
		d := loadDoc(t, h, fixture.Order, res.ID)
		fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
		hook := doc.FieldError{Field: "supplier_ref", Message: osHookText}
		probe := &fixture.Probe{ValidateErrors: []doc.FieldError{hook}}
		_, err := h.Do(t, fixture.WithProbe(t.Context(), probe), h.Priya, fixture.Order, d.ID, doc.ActionSubmit,
			doc.ActionInput{Version: d.Version, Only: []string{"remarks"}})
		de := wantDocErr(t, err, doc.KindInvalid, lkMsgFixFields)
		want := []doc.FieldError{
			{Field: "supplier_id", Message: lkMsgSupplierRequired},
			hook,
			{Field: "item_id", Line: 1, Message: lkMsgItemDisabled},
		}
		fixturetest.Eq(t, osSorted(de.Fields), osSorted(want))
	})
	t.Run("a clean draft submits", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSubmit,
			doc.ActionInput{Version: d.Version, Only: []string{"remarks"}})
		if err != nil {
			t.Fatalf("submit with Only: %v, want nil (ignored)", err)
		}
		if res.State == doc.StateDraft || res.Version != d.Version+1 {
			t.Errorf("Result = {State:%s Version:%d}, want it out of draft at %d", res.State, res.Version, d.Version+1)
		}
	})
	t.Run("approve revalidates in full", func(t *testing.T) {
		h := fixturetest.New(t)
		d := h.PendingOrder(t)
		fixture.SetDisabled(t, h.DB, "fixture_party", "SHREE", true)
		count := h.AuditCount(t)
		_, err := h.Do(t, t.Context(), h.Ravi, fixture.Order, d.ID, doc.ActionApprove,
			doc.ActionInput{Version: d.Version, Only: []string{"remarks"}})
		lkInvalid(t, err, doc.FieldError{Field: "supplier_id", Message: "Supplier SHREE Shree Steel Traders is disabled. Pick an active supplier, or ask an admin to enable it."})
		osUnwritten(t, h, d, count)
	})
}

// CD-94: Only changes no version rule. A stale scoped save is 409 and keeps
// the newer values, with or without a problem on the draft.
func TestDoc_Only_StaleIs409(t *testing.T) {
	for _, disable := range []bool{false, true} {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		if disable {
			fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		}
		first := osInput(h, d, "remarks")
		first.Values["remarks"] = "first"
		saveDo(t, h, h.Priya, fixture.Order, d.ID, first)
		fresh := loadDoc(t, h, fixture.Order, d.ID)
		count := h.AuditCount(t)

		late := osInput(h, d, "remarks")
		late.Values["remarks"] = "late"
		_, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, late)
		de := wantDocErr(t, err, doc.KindStale, fmt.Sprintf(lkMsgStale, lkDraft(d.ID)))
		if de.Status() != 409 {
			t.Errorf("Status = %d, want 409", de.Status())
		}
		osUnwritten(t, h, fresh, count)
	}
}

// A scoped save audits what changed, once; out-of-scope problems add nothing,
// and a scoped no-change save writes no entry.
func TestDoc_Only_AuditsWhatChanged(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	fixture.SetDisabled(t, h.DB, "fixture_item", "CEMENT-OPC", true)
	count := h.AuditCount(t)
	entries := len(h.Audit(t, fixture.Order, d.ID))

	in := osInput(h, d, "remarks")
	in.Values["remarks"] = "typed"
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("scoped save: %v", err)
	}
	if n := h.AuditCount(t); n != count+1 {
		t.Errorf("AuditCount = %d, want %d", n, count+1)
	}
	rows := h.Audit(t, fixture.Order, d.ID)
	if len(rows) != entries+1 {
		t.Fatalf("order audit entries = %d, want %d", len(rows), entries+1)
	}
	last := rows[len(rows)-1]
	if last.Action != string(doc.ActionSave) {
		t.Errorf("last entry action = %q, want save", last.Action)
	}
	var fields []string
	for _, c := range last.Changes {
		fields = append(fields, c.Field)
	}
	fixturetest.Eq(t, fields, []string{"remarks"})

	saved := loadDoc(t, h, fixture.Order, d.ID)
	if res.Version != saved.Version {
		t.Fatalf("Result.Version = %d, stored %d", res.Version, saved.Version)
	}
	if _, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, osInput(h, saved, "remarks")); err != nil {
		t.Fatalf("scoped no-change save: %v", err)
	}
	if n := len(h.Audit(t, fixture.Order, d.ID)); n != entries+1 {
		t.Errorf("after a no-change save: order audit entries = %d, want %d", n, entries+1)
	}
}

// Problems lists a stored draft's master problems on the header and the lines
// with the same text a refused save gives.
func TestDoc_Problems_ListsHeaderAndLines(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	for _, c := range osBroken[2:] {
		c.breakIt(t, h)
	}
	osBroken[0].breakIt(t, h)

	got, err := osProblems(t, h, fixture.Order, d.ID)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	want := []doc.FieldError{
		{Field: "supplier_id", Message: lkMsgSupplierDisabled},
		{Field: "item_id", Line: 1, Message: lkMsgItemDisabled},
		{Field: "item_id", Line: 2, Message: "Line 2: " + osWireBlocked},
	}
	fixturetest.Eq(t, got, want)

	_, saveErr := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, h.In(d))
	de := wantDocErr(t, saveErr, doc.KindInvalid, lkMsgFixFields)
	fixturetest.Eq(t, got, de.Fields)
}

func TestDoc_Problems_CleanDraftHasNone(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	fixture.SetWarning(t, h.DB, "fixture_party", "RAJKOT", "Rajkot Castings pays late.")
	got, err := osProblems(t, h, fixture.Order, d.ID)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Problems = %#v, want none: a master warning is no problem", got)
	}

	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	got, err = osProblems(t, h, fixture.Order, d.ID)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	fixturetest.Eq(t, got, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
}

// Decided from the code: only a draft can be saved or sent to the wizard
// (guard's allowedActions, stepGet's redirect), and CD-205 says "a stored
// draft's problems". A document past draft has none and no error, whatever
// its masters look like; Load reads it, so an error would be wrong too.
func TestDoc_Problems_NonDraftHasNone(t *testing.T) {
	for _, s := range []doc.State{doc.StatePending, doc.StateApproved, doc.StateRejected, doc.StateClosed, doc.StateCancelled} {
		t.Run(string(s), func(t *testing.T) {
			h, d := guardOrder(t, s)
			draft := h.NewOrder(t, h.Priya, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10"}).ID
			fixture.SetDisabled(t, h.DB, "fixture_party", "SHREE", true)
			fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
			got, err := osProblems(t, h, fixture.Order, d.ID)
			if err != nil {
				t.Fatalf("Problems: %v", err)
			}
			if len(got) != 0 {
				t.Errorf("Problems = %#v, want none past draft", got)
			}
			// The same master on a draft is a problem.
			got, err = osProblems(t, h, fixture.Order, draft)
			if err != nil {
				t.Fatalf("Problems of the draft: %v", err)
			}
			if len(got) != 1 || got[0].Field != "supplier_id" {
				t.Errorf("Problems of the draft = %#v, want the supplier's", got)
			}
		})
	}
}

// Problems reads what Load reads: a missing document, an unknown DocType and
// a master are Load's not-found error.
func TestDoc_Problems_NotFoundAsLoad(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	for _, c := range []struct {
		name, dt string
		id       int64
	}{
		{"missing id", fixture.Order, d.ID + 1000},
		{"unknown DocType", "fixture.Nope", 1},
		{"master DocType", fixture.Party, h.M.Party["RAJKOT"]},
	} {
		t.Run(c.name, func(t *testing.T) {
			var loadErr error
			if err := h.DB.Read(t.Context(), func(tx *db.Tx) error {
				_, loadErr = h.Engine.Load(t.Context(), tx, c.dt, c.id)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if loadErr == nil {
				t.Fatal("Load found it, want an error to compare with")
			}
			_, err := osProblems(t, h, c.dt, c.id)
			if err == nil || err.Error() != loadErr.Error() || !errors.Is(err, doctype.ErrNotFound) {
				t.Errorf("Problems error = %v, want Load's %q matching doctype.ErrNotFound", err, loadErr)
			}
			var he, lhe doctype.HTTPError
			if !errors.As(err, &he) || !errors.As(loadErr, &lhe) || he.Status() != lhe.Status() {
				t.Errorf("Problems error %v: status differs from Load's %v", err, loadErr)
			}
		})
	}
}

// Problems takes no principal: like Load it checks no rights, so a record
// hidden from everyone reads the same, and no AUTH call is made. The caller
// (docscreen.View) applies read rights; its HTTP test covers that.
func TestDoc_Problems_NoRightsCheckAsLoad(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	h.Auth.Hide = func(auth.Principal, string, int64) bool { return true }
	h.Auth.Deny = func(auth.Principal, string, auth.Right) bool { return true }

	probe := &fixture.Probe{}
	ctx := fixture.WithProbe(t.Context(), probe)
	var (
		got []doc.FieldError
		err error
	)
	if rerr := h.DB.Read(ctx, func(tx *db.Tx) error {
		got, err = h.Engine.Problems(ctx, tx, fixture.Order, d.ID)
		return nil
	}); rerr != nil {
		t.Fatal(rerr)
	}
	if err != nil {
		t.Fatalf("Problems of a hidden record: %v, want what Load does: no error", err)
	}
	fixturetest.Eq(t, got, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
	for _, c := range probe.Calls {
		if strings.HasPrefix(c, "AUTH.") {
			t.Errorf("Problems called %s; rights are the caller's, as for Load", c)
		}
	}
}

// Problems runs in the caller's read transaction, so it writes nothing and
// calls no hook: Hooks.Validate may write (fixture.Claim's sets total, CD-79).
func TestDoc_Problems_WritesNothing(t *testing.T) {
	t.Run("order", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
		count := h.AuditCount(t)
		probe := &fixture.Probe{}
		ctx := fixture.WithProbe(t.Context(), probe)
		var got []doc.FieldError
		if err := h.DB.Read(ctx, func(tx *db.Tx) (err error) {
			got, err = h.Engine.Problems(ctx, tx, fixture.Order, d.ID)
			return err
		}); err != nil {
			t.Fatalf("Problems: %v", err)
		}
		fixturetest.Eq(t, got, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
		if hooks := probe.Hooks(); len(hooks) != 0 {
			t.Errorf("hooks called = %q, want none", hooks)
		}
		osUnwritten(t, h, d, count)
	})
	t.Run("claim, whose Validate writes", func(t *testing.T) {
		h := fixturetest.New(t)
		res := h.NewClaim(t, h.Deepak, "EMP-007", fixturetest.CL{Type: "TRAVEL", Amount: "1250.00"})
		d := loadDoc(t, h, fixture.Claim, res.ID)
		count := h.AuditCount(t)
		if _, err := osProblems(t, h, fixture.Claim, d.ID); err != nil {
			t.Fatalf("Problems: %v", err)
		}
		osUnwritten(t, h, d, count)
	})
}

// A non-nil empty Only is a scoped save of no field: a step that names none
// still mustn't fall back to CD-199's full refusal.
func TestDoc_Only_EmptyIsScoped(t *testing.T) {
	h := fixturetest.New(t)
	d := osOrder(t, h)
	fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true)
	in := h.In(d)
	in.Only = []string{}
	in.Values["remarks"] = "typed"
	res, err := h.Do(t, t.Context(), h.Priya, fixture.Order, d.ID, doc.ActionSave, in)
	if err != nil {
		t.Fatalf("save with an empty Only: %v, want nil", err)
	}
	if res.Version != d.Version+1 {
		t.Errorf("Result.Version = %d, want %d", res.Version, d.Version+1)
	}
	fixturetest.Eq(t, res.Problems, []doc.FieldError{{Field: "supplier_id", Message: lkMsgSupplierDisabled}})
}

// F1: every line's source problem returns, from a scoped save and from
// Engine.Problems; checkSources mustn't stop at the first.
func TestDoc_SourceProblems_AllLines(t *testing.T) {
	t.Run("Result.Problems", func(t *testing.T) {
		h := fixturetest.New(t)
		d, first, second := osBadReceipt(t, h)
		in := osInput(h, d, "remarks")
		in.Values["remarks"] = "typed"
		res, err := h.Do(t, t.Context(), h.Priya, fixture.Receipt, d.ID, doc.ActionSave, in)
		if err != nil {
			t.Fatalf("scoped save: %v, want nil", err)
		}
		fixturetest.Eq(t, res.Problems, []doc.FieldError{osSourceCancelled(1, first.Number), osSourceCancelled(2, second.Number)})
	})
	t.Run("Engine.Problems", func(t *testing.T) {
		h := fixturetest.New(t)
		d, first, second := osBadReceipt(t, h)
		got, err := osProblems(t, h, fixture.Receipt, d.ID)
		if err != nil {
			t.Fatalf("Problems: %v", err)
		}
		fixturetest.Eq(t, got, []doc.FieldError{osSourceCancelled(1, first.Number), osSourceCancelled(2, second.Number)})
	})
}

// An empty Required field is Submit's error, not a stored draft's problem
// (CD-178): the check page lists those itself.
func TestDoc_Problems_EmptyRequiredIsNone(t *testing.T) {
	h := fixturetest.New(t)
	res := h.NewOrderWith(t, h.Priya, map[string]string{"supplier_id": ""}, fixturetest.OL{Item: "CEMENT-OPC", Qty: "10"})
	got, err := osProblems(t, h, fixture.Order, res.ID)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Problems = %#v, want none", got)
	}
}

// Problems is in position order across the field and source checks: a source
// error on line 1 before a field error on line 2.
func TestDoc_Problems_SortedByPosition(t *testing.T) {
	h := fixturetest.New(t)
	cancelled, good := osApproved(t, h), osApproved(t, h)
	res := h.NewReceipt(t, h.Priya,
		fixturetest.RL{OrderLineID: cancelled.Lines[0].ID, Qty: "1"}, fixturetest.RL{OrderLineID: good.Lines[0].ID, Qty: "1"})
	osCancel(t, h, cancelled)
	h.Exec(t, `UPDATE fixture_receipt_line SET qty = 0 WHERE parent_id = ? AND line_no = 2`, res.ID)
	got, err := osProblems(t, h, fixture.Receipt, res.ID)
	if err != nil {
		t.Fatalf("Problems: %v", err)
	}
	fixturetest.Eq(t, got, []doc.FieldError{
		osSourceCancelled(1, cancelled.Number),
		{Field: "qty", Line: 2, Message: "Line 2: Quantity must be more than 0."},
	})
}

// A failed read is the error, never a partial list. No test double: renaming
// the table makes the real lookup and the real source read fail.
func TestDoc_Problems_ReadFailureIsAnError(t *testing.T) {
	t.Run("master lookup", func(t *testing.T) {
		h := fixturetest.New(t)
		d := osOrder(t, h)
		fixture.SetDisabled(t, h.DB, "fixture_party", "RAJKOT", true) // found before the failure
		h.Exec(t, `ALTER TABLE fixture_item RENAME TO fixture_item_gone`)
		got, err := osProblems(t, h, fixture.Order, d.ID)
		if err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Errorf("Problems error = %v, want the failed lookup", err)
		}
		if got != nil {
			t.Errorf("Problems = %#v, want no partial list", got)
		}
	})
	t.Run("source read", func(t *testing.T) {
		h := fixturetest.New(t)
		o := osApproved(t, h)
		res := h.NewReceipt(t, h.Priya, fixturetest.RL{OrderLineID: o.Lines[0].ID, Qty: "1"})
		h.Exec(t, `UPDATE fixture_receipt_line SET qty = 0 WHERE parent_id = ?`, res.ID) // a field problem found first
		h.Exec(t, `ALTER TABLE fixture_order_line RENAME TO fixture_order_line_gone`)
		got, err := osProblems(t, h, fixture.Receipt, res.ID)
		if err == nil || !strings.Contains(err.Error(), "no such table") {
			t.Errorf("Problems error = %v, want the failed source read", err)
		}
		if got != nil {
			t.Errorf("Problems = %#v, want no partial list", got)
		}
	})
}

// osTwoProjects has two ProjectFields, so a draft with the Required one empty
// is still visible to a restricted user through the other (auth.recordAllowed
// hides a record with no project at all). Its tables are made in the test.
const osTwoProjects = "os.TwoProjects"

var _ = func() bool {
	doc.Register(doc.DocType{
		Name: osTwoProjects, Label: "Two projects", PluralLabel: "Two projects",
		Table: "os_two", LinesTable: "os_two_line", Hooks: p2lHooks{},
		ProjectFields: []string{"project_id", "alt_project_id"},
		Fields: []doc.Field{
			{Name: "project_id", Label: "Project", Type: doc.Link, Link: fixture.Project, Required: true},
			{Name: "alt_project_id", Label: "Other project", Type: doc.Link, Link: fixture.Project},
			{Name: "note", Label: "Note", Type: doc.Text},
		},
	})
	return true
}()

// CD-178 on the no-change path: a scoped save changing nothing, on a visible
// draft with the Required field empty, still refuses for the restricted user
// and writes nothing.
func TestDoc_Only_RestrictedRequiredNoChangeRefuses(t *testing.T) {
	h := fixturetest.New(t)
	h.Exec(t, `CREATE TABLE os_two (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL,
		updated_by INTEGER NOT NULL, project_id INTEGER, alt_project_id INTEGER, note TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE os_two_line (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL)`)
	a := authfake.New()
	a.Allow(h.Priya.ID, osTwoProjects, auth.AllRights...)
	a.Allow(h.Ravi.ID, osTwoProjects, auth.AllRights...)
	a.Restrict(h.Priya.ID, []int64{h.M.Project["PRJ-METRO"]}, nil)
	e := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	values := map[string]string{"project_id": "", "alt_project_id": lkID(h.M.Project["PRJ-METRO"]), "note": "kept"}
	res, err := e.Do(t.Context(), h.Ravi, osTwoProjects, 0, doc.ActionSave, doc.ActionInput{Values: values})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	d := loadDoc(t, h, osTwoProjects, res.ID)
	count := h.AuditCount(t)
	_, err = e.Do(t.Context(), h.Priya, osTwoProjects, res.ID, doc.ActionSave,
		doc.ActionInput{Version: res.Version, Values: values, Only: []string{"note"}})
	osHasField(t, err, doc.FieldError{Field: "project_id", Message: "Project is required."})
	osUnwritten(t, h, d, count)
}

// CD-178's exception holds with Only set: a restricted user's save that
// empties the Required project refuses even when the project is out of scope,
// since the draft would vanish from them. On the real auth.Service and on
// authfake (lessons section 5). The same scoped save does commit past an
// ordinary out-of-scope problem, and for an unrestricted user.
func TestDoc_Only_RestrictedRequiredStillRefuses(t *testing.T) {
	const disabled = "Project PRJ-METRO Pune Metro Depot is disabled. Pick an active project, or ask an admin to enable it."
	for _, c := range []struct {
		name  string
		setup func(t *testing.T, h *fixturetest.H) (e doc.Engine, restricted, open auth.Principal)
	}{
		{"real auth.Service", func(t *testing.T, h *fixturetest.H) (doc.Engine, auth.Principal, auth.Principal) {
			return rqRealEngine(t, h)
		}},
		{"authfake", func(t *testing.T, h *fixturetest.H) (doc.Engine, auth.Principal, auth.Principal) {
			return rqRestrictedEngine(t, h), h.Priya, h.Ravi
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := fixturetest.New(t)
			e, restricted, open := c.setup(t, h)
			ctx := t.Context()
			project := lkID(h.M.Project["PRJ-METRO"])
			create := func(p auth.Principal) doc.Result {
				t.Helper()
				res, err := e.Do(ctx, p, rqRestricted, 0, doc.ActionSave, doc.ActionInput{Values: map[string]string{"project_id": project, "note": "step 1"}})
				if err != nil {
					t.Fatalf("create: %v", err)
				}
				return res
			}
			res := create(restricted)
			openDoc := create(open)
			fixture.SetDisabled(t, h.DB, "fixture_project", "PRJ-METRO", true)

			saved, err := e.Do(ctx, restricted, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{
				Version: res.Version, Values: map[string]string{"project_id": project, "note": "step 2"}, Only: []string{"note"},
			})
			if err != nil {
				t.Fatalf("scoped save past a disabled project: %v, want nil", err)
			}
			fixturetest.Eq(t, saved.Problems, []doc.FieldError{{Field: "project_id", Message: disabled}})

			before := loadDoc(t, h, rqRestricted, res.ID)
			count := h.AuditCount(t)
			_, err = e.Do(ctx, restricted, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{
				Version: saved.Version, Values: map[string]string{"project_id": "", "note": "step 3"}, Only: []string{"note"},
			})
			osHasField(t, err, doc.FieldError{Field: "project_id", Message: "Project is required."})
			osUnwritten(t, h, before, count)

			if _, err := e.Do(ctx, open, rqRestricted, openDoc.ID, doc.ActionSave, doc.ActionInput{
				Version: openDoc.Version, Values: map[string]string{"project_id": "", "note": "step 3"}, Only: []string{"note"},
			}); err != nil {
				t.Errorf("unrestricted scoped save with the project empty: %v, want nil", err)
			}
		})
	}
}
