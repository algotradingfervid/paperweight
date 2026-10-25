package doc_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/naming"
)

// msgClientNumber is NAM's refusal of a client-supplied number (CD-28).
const msgClientNumber = "Document numbers are set automatically and can't be entered."

// CD-144: with "number" declared as a display field, create refuses a posted
// number through the guard, save accepts the stored number echoed back and
// keeps it, and Load shows it.
func TestDoc_ConventionField_Engine(t *testing.T) {
	h := fixturetest.New(t)
	h.Exec(t, `CREATE TABLE t_doc_cd144 (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL,
		updated_by INTEGER NOT NULL, note TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE t_doc_cd144_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL)`)
	const dt, stored = "t.DocCD144", "CD/26-27/00001"

	_, err := h.Do(t, t.Context(), h.Priya, dt, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"number": "CD-HACK", "note": "hi"}})
	de := wantDocErr(t, err, doc.KindInvalid, msgClientNumber)
	if !errors.Is(de, naming.ErrClientNumber) || de.Status() != 422 {
		t.Errorf("create with number: err %v status %d, want naming.ErrClientNumber, 422", de, de.Status())
	}
	if len(de.Fields) != 1 || de.Fields[0].Field != "number" {
		t.Errorf("create with number: Fields = %+v, want one on \"number\"", de.Fields)
	}

	res := h.MustDo(t, t.Context(), h.Priya, dt, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"note": "hi"}})
	h.ForceState(t, dt, res.ID, doc.StateDraft, stored)

	in := h.In(h.Load(t, dt, res.ID))
	if in.Values["number"] != stored {
		t.Fatalf("In: number = %q, want %q", in.Values["number"], stored)
	}
	in.Values["note"] = "edited"
	h.MustDo(t, t.Context(), h.Priya, dt, res.ID, doc.ActionSave, in)

	d := h.Load(t, dt, res.ID)
	if d.Number != stored || d.Header["number"] != any(stored) || d.Header["note"] != any("edited") {
		t.Errorf("after save: Number %q, Header %v; want number %q, note \"edited\"", d.Number, d.Header, stored)
	}
	if got := h.Record(t, dt, res.ID).Values["number"]; got != stored {
		t.Errorf("Record.Values[number] = %q, want %q", got, stored)
	}
}
