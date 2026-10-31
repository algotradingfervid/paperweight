package expensetest

import (
	"context"
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E2's helpers: inputs for saves a case expects to be refused, and an edit
// of a stored draft that returns the engine's error instead of failing.

// ClaimInput is the input DraftClaim saves, with the same defaults, for a
// case that expects the save to be refused.
func (e *Env) ClaimInput(t testing.TB, p auth.Principal, header Values, lines ...Values) doc.ActionInput {
	t.Helper()
	in := doc.ActionInput{Values: merge(Values{
		"employee_id": e.employeeOf(p), "claim_date": Date, "purpose": "Pune Metro site visit",
	}, header)}
	if len(lines) == 0 {
		lines = []Values{nil}
	}
	for _, l := range lines {
		in.Lines = append(in.Lines, merge(Values{
			"expense_date": Date, "expense_type_id": e.expenseType(t, "TRAVEL"), "amount": "1000.00",
			"no_receipt_reason": "Paid in cash.",
		}, l))
	}
	return in
}

// AdvanceInput is the input DraftAdvance saves, with the same defaults.
func (e *Env) AdvanceInput(t testing.TB, p auth.Principal, header Values, lines ...Values) doc.ActionInput {
	t.Helper()
	in := doc.ActionInput{Values: merge(Values{
		"employee_id": e.employeeOf(p), "advance_date": Date, "purpose": "Pune Metro site visit",
	}, header)}
	if len(lines) == 0 {
		lines = []Values{nil}
	}
	for _, l := range lines {
		in.Lines = append(in.Lines, merge(Values{"expense_type_id": e.expenseType(t, "TRAVEL"), "line_amount": "5000.00"}, l))
	}
	return in
}

// Create saves in as a new document of dt as p and returns the engine's
// result and error.
func (e *Env) Create(p auth.Principal, dt string, in doc.ActionInput) (doc.Result, error) {
	return e.Engine.Do(context.Background(), p, dt, 0, doc.ActionSave, in)
}

// Edit saves stored draft id of dt as p with its stored version: header
// values are sent only for the names in header (the engine keeps the
// others), and every stored line is sent in full with lines[i] merged over
// line i. It returns the engine's result and error.
func (e *Env) Edit(t testing.TB, p auth.Principal, dt string, id int64, header Values, lines ...Values) (doc.Result, error) {
	t.Helper()
	rec := e.Load(t, dt, id)
	in := doc.ActionInput{Version: rec.Version, Values: maps.Clone(header)}
	if in.Values == nil {
		in.Values = Values{}
	}
	for i, l := range rec.Lines {
		m := maps.Clone(l) // each line in full: a line sent with only its id is blank
		if i < len(lines) {
			maps.Copy(m, lines[i])
		}
		in.Lines = append(in.Lines, m)
	}
	return e.Engine.Do(context.Background(), p, dt, id, doc.ActionSave, in)
}

// Act runs action a on document id of dt as p with the document's stored
// version and reason, and returns the engine's result and error.
func (e *Env) Act(t testing.TB, p auth.Principal, dt string, id int64, a doc.Action, reason string) (doc.Result, error) {
	t.Helper()
	rec := e.Load(t, dt, id)
	return e.Engine.Do(context.Background(), p, dt, id, a, doc.ActionInput{Version: rec.Version, Reason: reason})
}

// Load is document id of dt as the engine reads it.
func (e *Env) Load(t testing.TB, dt string, id int64) doc.Record {
	t.Helper()
	var rec doc.Record
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		rec, err = e.Engine.Load(context.Background(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: load %s %d: %v", dt, id, err)
	}
	return rec
}
