package doc_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc"
)

// rqRestricted is a test-only DocType whose Required project field is its
// ProjectFields entry. Its tables are created by rqRestrictedEngine.
const rqRestricted = "rq.Restricted"

var _ = func() bool {
	doc.Register(doc.DocType{
		Name: rqRestricted, Label: "Restricted document", PluralLabel: "Restricted documents",
		Table: "rq_restricted", LinesTable: "rq_restricted_line", Hooks: p2lHooks{},
		ProjectFields: []string{"project_id"},
		Fields: []doc.Field{
			{Name: "project_id", Label: "Project", Type: doc.Link, Link: fixture.Project, Required: true},
			{Name: "note", Label: "Note", Type: doc.Text},
		},
	})
	return true
}()

// rqRestrictedEngine creates the rq_restricted tables on h's database and
// returns an engine on an authfake that allows every right on rqRestricted
// to Priya and Ravi and restricts Priya to project PRJ-METRO.
func rqRestrictedEngine(t *testing.T, h *fixturetest.H) doc.Engine {
	t.Helper()
	h.Exec(t, `CREATE TABLE rq_restricted (
		id INTEGER PRIMARY KEY, number TEXT UNIQUE, state TEXT NOT NULL DEFAULT 'draft',
		state_reason TEXT NOT NULL DEFAULT '', amended_from_id INTEGER, amendment_root_id INTEGER,
		amendment_no INTEGER NOT NULL DEFAULT 0, version INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL, created_by INTEGER NOT NULL, updated_at TEXT NOT NULL,
		updated_by INTEGER NOT NULL, project_id INTEGER, note TEXT NOT NULL DEFAULT '')`)
	h.Exec(t, `CREATE TABLE rq_restricted_line (
		id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL, line_no INTEGER NOT NULL)`)
	a := authfake.New()
	a.Allow(h.Priya.ID, rqRestricted, auth.AllRights...)
	a.Allow(h.Ravi.ID, rqRestricted, auth.AllRights...)
	a.Restrict(h.Priya.ID, []int64{h.M.Project["PRJ-METRO"]}, nil)
	return doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
}

// rqRestrictedRows counts the rq_restricted rows.
func rqRestrictedRows(t *testing.T, h *fixturetest.H) int {
	t.Helper()
	var n int
	if err := h.DB.SQL().QueryRowContext(t.Context(), "SELECT count(*) FROM rq_restricted").Scan(&n); err != nil {
		t.Fatalf("count rq_restricted: %v", err)
	}
	return n
}

// CD-178 exception: a Required ProjectFields field stays required on a draft
// save for a user restricted on projects, with the normal Required message;
// an unrestricted user saves the draft with it empty.
func TestDoc_Draft_RequiredRestrictedDimension(t *testing.T) {
	h := fixturetest.New(t)
	e := rqRestrictedEngine(t, h)
	ctx := t.Context()
	wantProject := []doc.FieldError{{Field: "project_id", Line: 0, Message: "Project is required."}}
	empty := doc.ActionInput{Values: map[string]string{"project_id": "", "note": "step 1"}}

	// Restricted user, project empty: refused, nothing written.
	count := h.AuditCount(t)
	_, err := e.Do(ctx, h.Priya, rqRestricted, 0, doc.ActionSave, empty)
	de := wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	if diff := diffOf(wantProject, de.Fields); diff != "" {
		t.Errorf("restricted create Fields (want, got):\n%s", diff)
	}
	if n := rqRestrictedRows(t, h); n != 0 {
		t.Errorf("rows after a refused create = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused create, want %d", n, count)
	}

	// Unrestricted user, project empty: saves.
	if _, err := e.Do(ctx, h.Ravi, rqRestricted, 0, doc.ActionSave, empty); err != nil {
		t.Errorf("unrestricted create with project empty: %v, want ok", err)
	}

	// Restricted user, project set: saves.
	project := fmt.Sprint(h.M.Project["PRJ-METRO"])
	res, err := e.Do(ctx, h.Priya, rqRestricted, 0, doc.ActionSave,
		doc.ActionInput{Values: map[string]string{"project_id": project, "note": "step 1"}})
	if err != nil {
		t.Fatalf("restricted create with project set: %v, want ok", err)
	}
	if _, err := e.Do(ctx, h.Priya, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{
		Version: res.Version, Values: map[string]string{"project_id": project, "note": "step 2"},
	}); err != nil {
		t.Fatalf("restricted save with project set: %v, want ok", err)
	}

	// Restricted user, a save that empties the project: refused, nothing written.
	before := loadDoc(t, h, rqRestricted, res.ID)
	count = h.AuditCount(t)
	_, err = e.Do(ctx, h.Priya, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{
		Version: before.Version, Values: map[string]string{"project_id": "", "note": "step 3"},
	})
	de = wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	if diff := diffOf(wantProject, de.Fields); diff != "" {
		t.Errorf("restricted save Fields (want, got):\n%s", diff)
	}
	if diff := diffOf(before, loadDoc(t, h, rqRestricted, res.ID)); diff != "" {
		t.Errorf("document changed by a refused save (before, after):\n%s", diff)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused save, want %d", n, count)
	}
}
