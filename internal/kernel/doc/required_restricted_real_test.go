package doc_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/testkit"
)

// rqRealPassword is the users' password (testkit's fixture password).
const rqRealPassword = "Correct-Horse-42"

// rqRealEngine returns an engine for rq.Restricted over the real auth
// service on h's database, and two users set up through AUTH's own admin
// calls: the first is restricted to project PRJ-METRO, the second is not;
// both have read, create and write on rq.Restricted.
func rqRealEngine(t *testing.T, h *fixturetest.H) (doc.Engine, auth.Principal, auth.Principal) {
	t.Helper()
	ctx := t.Context()
	rqRestrictedEngine(t, h) // creates the rq_restricted tables
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	adminID, err := a.CreateFirstAdmin(ctx, "rq-admin@vardhan.test", "Rq Admin", rqRealPassword)
	if err != nil {
		t.Fatalf("CreateFirstAdmin: %v", err)
	}
	admin := rqPrincipal(t, a, adminID)
	if err := a.CreateRole(ctx, admin, "Drafter"); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	all := map[auth.Right]auth.Scope{"read": auth.ScopeAll, "create": auth.ScopeAll, "write": auth.ScopeAll}
	if err := a.SetRights(ctx, admin, "Drafter", rqRestricted, all); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	user := func(email string) auth.UserID {
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: rqRealPassword, Roles: []string{"Drafter"}})
		if err != nil {
			t.Fatalf("CreateUser(%s): %v", email, err)
		}
		return id
	}
	restricted, open := user("rq-restricted@vardhan.test"), user("rq-open@vardhan.test")
	if err := a.ChangeRestrictions(ctx, admin, restricted, auth.DimProject, []int64{h.M.Project["PRJ-METRO"]}, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
	e := doc.NewEngine(doc.Deps{
		DB: h.DB, Clock: h.Clock, Auth: a, Naming: h.Naming, Numbers: fixturetest.Numbers(),
		Approval: h.Approvals, Files: h.Files,
	})
	return e, rqPrincipal(t, a, restricted), rqPrincipal(t, a, open)
}

// rqPrincipal loads user u's principal from AUTH.
func rqPrincipal(t *testing.T, a *auth.Auth, u auth.UserID) auth.Principal {
	t.Helper()
	p, _, err := a.LoadPrincipal(t.Context(), u)
	if err != nil {
		t.Fatalf("LoadPrincipal(%d): %v", u, err)
	}
	return p
}

// CD-178 exception on the real auth service: a restricted user's draft
// create or save with the Required project empty gets the 422 Required
// error, not AUTH's refusal, and nothing is written.
func TestDoc_Draft_RequiredRestrictedRealAuth(t *testing.T) {
	h := fixturetest.New(t)
	e, restricted, open := rqRealEngine(t, h)
	ctx := t.Context()
	wantProject := []doc.FieldError{{Field: "project_id", Line: 0, Message: "Project is required."}}
	empty := doc.ActionInput{Values: map[string]string{"project_id": "", "note": "step 1"}}

	// Restricted user, project empty: 422 Required, nothing written.
	count := h.AuditCount(t)
	_, err := e.Do(ctx, restricted, rqRestricted, 0, doc.ActionSave, empty)
	de := wantDocErr(t, err, doc.KindInvalid, rqFixFields)
	if diff := diffOf(wantProject, de.Fields); diff != "" {
		t.Errorf("restricted create Fields (want, got):\n%s", diff)
	}
	if de.Status() != 422 {
		t.Errorf("restricted create status = %d, want 422", de.Status())
	}
	if n := rqRestrictedRows(t, h); n != 0 {
		t.Errorf("rows after a refused create = %d, want 0", n)
	}
	if n := h.AuditCount(t); n != count {
		t.Errorf("AuditCount = %d after a refused create, want %d", n, count)
	}

	// Unrestricted user, project empty: saves.
	if _, err := e.Do(ctx, open, rqRestricted, 0, doc.ActionSave, empty); err != nil {
		t.Errorf("unrestricted create with project empty: %v, want ok", err)
	}

	// Restricted user, project set: create and save succeed.
	set := func(note string) map[string]string {
		return map[string]string{"project_id": fmt.Sprint(h.M.Project["PRJ-METRO"]), "note": note}
	}
	res, err := e.Do(ctx, restricted, rqRestricted, 0, doc.ActionSave, doc.ActionInput{Values: set("step 1")})
	if err != nil {
		t.Fatalf("restricted create with project set: %v, want ok", err)
	}
	res, err = e.Do(ctx, restricted, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{Version: res.Version, Values: set("step 2")})
	if err != nil {
		t.Fatalf("restricted save with project set: %v, want ok", err)
	}

	// Restricted user, a save that empties the project: 422 Required, nothing written.
	before := loadDoc(t, h, rqRestricted, res.ID)
	count = h.AuditCount(t)
	_, err = e.Do(ctx, restricted, rqRestricted, res.ID, doc.ActionSave, doc.ActionInput{
		Version: res.Version, Values: map[string]string{"project_id": "", "note": "step 3"},
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
