package auth_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// MOD-0 M0-24 (CD-280 D2): auth.Applies(dt, approve) is also true when
// dt.CloseRight is "approve" (a non-approvable DocType whose close is guarded
// by approve, as purchase.Request, CD-253/255). Everything that follows
// Applies follows: ValidateRow and seeding, Can/CanTx, Scope, WhoCan,
// UsersWithRight and AllowedRights.
const (
	m024Close = "m024.CloseApprove" // Submittable, not Approvable, CloseRight "approve"
	m024Twin  = "m024.CloseTwin"    // the same without CloseRight
	m024Seed  = "m024.CloseSeed"    // m024Close with approve in DefaultRights
	m024Amend = "m024.CloseAmend"   // the same with CloseRight "amend" (as purchase.Order)
	m024None  = "m024.CloseNone"    // the same with CloseRight "none" (as Expense, CD-253)

	m024CloseLabel = "Close request"
	m024TwinLabel  = "Twin request"

	m024HolderRole = "m024 Closer"    // read and approve on m024Close
	m024OtherRole  = "m024 Submitter" // read and submit on m024Close
	m024Holder     = "m024-holder@vardhan.test"
	m024Other      = "m024-other@vardhan.test"

	// AUTH's existing refusal of approve on a DocType it doesn't apply to.
	m024TwinNotApplicable = "Approve does not apply to Twin request. Clear it."
)

// m024Others are non-approvable DocTypes whose CloseRight is set to
// something other than approve: approve applies to none of them.
var m024Others = []struct{ name, closeRight, label, plural, notApplicable string }{
	{m024Amend, "amend", "Amend request", "Amend requests", "Approve does not apply to Amend request. Clear it."},
	{m024None, "none", "None request", "None requests", "Approve does not apply to None request. Clear it."},
}

// m024RegisterOthers registers m024Others once and returns them in order.
func m024RegisterOthers() []*doctype.DocType {
	out := make([]*doctype.DocType, 0, len(m024Others))
	for _, o := range m024Others {
		t3Register(doctype.DocType{
			Name: o.name, Label: o.label, PluralLabel: o.plural,
			Submittable: true, CloseRight: o.closeRight,
		})
		dt, _ := doctype.Lookup(o.name)
		out = append(out, dt)
	}
	return out
}

// m024Register registers the M0-24 DocTypes once (the registry is global).
func m024Register() (closeDT, twin *doctype.DocType) {
	t3Register(doctype.DocType{
		Name: m024Close, Label: m024CloseLabel, PluralLabel: "Close requests",
		Submittable: true, CloseRight: "approve",
	})
	t3Register(doctype.DocType{
		Name: m024Twin, Label: m024TwinLabel, PluralLabel: "Twin requests",
		Submittable: true,
	})
	closeDT, _ = doctype.Lookup(m024Close)
	twin, _ = doctype.Lookup(m024Twin)
	return closeDT, twin
}

// m024Applies is the expected Applies table: every right except import
// applies to each m024 DocType (submittable, not importable); approve only to
// m024Close, never to the twin or to m024Others (CloseRight amend, none).
func m024Applies(dtName string, r auth.Right) bool {
	switch r {
	case auth.RightImport:
		return false
	case auth.RightApprove:
		return dtName == m024Close
	}
	return slices.Contains(auth.AllRights, r)
}

func TestCloseRight_Applies(t *testing.T) {
	closeDT, twin := m024Register()
	if closeDT.Approvable || !closeDT.Submittable || closeDT.CloseRight != "approve" || twin.Approvable || twin.CloseRight != "" {
		t.Fatalf("setup: %s must be submittable, not approvable, CloseRight approve; %s the same without CloseRight", m024Close, m024Twin)
	}
	for _, dt := range []*doctype.DocType{closeDT, twin} {
		for _, r := range append(slices.Clone(auth.AllRights), "foo") {
			if got, want := auth.Applies(dt, r), m024Applies(dt.Name, r); got != want {
				t.Errorf("Applies(%s, %s) = %v, want %v", dt.Name, r, got, want)
			}
		}
	}
}

func TestCloseRight_ValidateRowAndSeed(t *testing.T) {
	closeDT, twin := m024Register()
	ctx := context.Background()

	for _, s := range []auth.Scope{auth.ScopeOwn, auth.ScopeAll} {
		row := map[auth.Right]auth.Scope{auth.RightRead: s, auth.RightApprove: s}
		if err := auth.ValidateRow(closeDT, row); err != nil {
			t.Errorf("ValidateRow(%s, {read: %s, approve: %s}) = %v, want nil (CD-280 D2)", m024Close, s, s, err)
		}
	}
	twinRow := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll}
	t3WantProblem(t, "ValidateRow(twin, {read: all, approve: all})", auth.ValidateRow(twin, twinRow), m024TwinNotApplicable)

	// Through SetRights (the roles screen's save): own and all are kept.
	a, d, _, _ := t3Auth(t)
	admin := testkit.Principal(t, "admin@vardhan.test")
	for _, s := range []auth.Scope{auth.ScopeOwn, auth.ScopeAll} {
		want := map[auth.Right]auth.Scope{auth.RightRead: s, auth.RightApprove: s}
		if err := a.SetRights(ctx, admin, auth.RoleEmployee, m024Close, want); err != nil {
			t.Errorf("SetRights(%s, {read: %s, approve: %s}) = %v, want nil (CD-280 D2)", m024Close, s, s, err)
			continue
		}
		if got, err := a.RoleRights(ctx, auth.RoleEmployee, m024Close); err != nil || !t3Equal(got, want) {
			t.Errorf("RoleRights(%s) = (%v, %v), want (%v, nil)", m024Close, got, err, want)
		}
	}
	before := t3Rows(t, d, auth.RoleEmployee, m024Twin)
	t3WantProblem(t, "SetRights(twin, {read: all, approve: all})",
		a.SetRights(ctx, admin, auth.RoleEmployee, m024Twin, twinRow), m024TwinNotApplicable)
	if after := t3Rows(t, d, auth.RoleEmployee, m024Twin); !maps.Equal(before, after) {
		t.Errorf("role_rights(%s, twin) after refused SetRights = %v, want unchanged %v", auth.RoleEmployee, after, before)
	}

	// Seeding: a DocType whose DefaultRights hold approve under CloseRight
	// approve seeds. Registered only once ValidateRow accepts it, so a
	// refusal never reaches every later SeedDefaults in this binary.
	if auth.ValidateRow(closeDT, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll}) != nil {
		t.Fatalf("seeding not tried: ValidateRow refuses approve on %s", m024Close)
	}
	t3Register(doctype.DocType{
		Name: m024Seed, Label: "Seed request", PluralLabel: "Seed requests",
		Submittable: true, CloseRight: "approve",
		DefaultRights: map[string]map[string]string{
			"Purchase Manager": {"read": "all", "approve": "all"},
			auth.RoleEmployee:  {"read": "own", "approve": "own"},
		},
	})
	sa, sd, _, _ := t3Auth(t)
	if err := sa.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults with approve on %s = %v, want nil", m024Seed, err)
	}
	for role, want := range map[string]map[string]string{
		"Purchase Manager": {"read": "all", "approve": "all"},
		auth.RoleEmployee:  {"read": "own", "approve": "own"},
	} {
		if got := t3Rows(t, sd, role, m024Seed); !maps.Equal(got, want) {
			t.Errorf("seeded role_rights(%s, %s) = %v, want %v", role, m024Seed, got, want)
		}
	}
}

// m024World is a World with a user holding read and approve on m024Close and
// one holding read and submit, both granted through AUTH's admin calls.
func m024World(t *testing.T) (w *authtest.World, holder, other auth.Principal) {
	t.Helper()
	m024Register()
	ctx := context.Background()
	w = authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")
	for role, r := range map[string]auth.Right{m024HolderRole: auth.RightApprove, m024OtherRole: auth.RightSubmit} {
		if err := w.Auth.CreateRole(ctx, admin, role); err != nil {
			t.Fatalf("CreateRole(%s): %v", role, err)
		}
		rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, r: auth.ScopeAll}
		if err := w.Auth.SetRights(ctx, admin, role, m024Close, rights); err != nil {
			t.Fatalf("SetRights(%s, %s, {read, %s}) = %v, want nil (CD-280 D2: approve applies under CloseRight approve)", role, m024Close, r, err)
		}
	}
	w.AddUser(t, m024Holder, "Close Holder", m024HolderRole)
	w.AddUser(t, m024Other, "Close Submitter", m024OtherRole)
	return w, w.P(t, m024Holder), w.P(t, m024Other)
}

// m024Denied fails unless err is AUTH's *DeniedError for approve on label.
func m024Denied(t *testing.T, what string, err error, label, plural string) {
	t.Helper()
	var de *auth.DeniedError
	want := &auth.DeniedError{Right: auth.RightApprove, Label: label, Plural: plural}
	if !errors.As(err, &de) || !errors.Is(err, auth.ErrForbidden) || de.Status() != 403 || err.Error() != want.Error() {
		t.Errorf("%s = %v, want *auth.DeniedError 403 %q", what, err, want.Error())
	}
}

func TestCloseRight_CanScopeWhoCanAllowed(t *testing.T) {
	ctx := context.Background()
	w, holder, other := m024World(t)
	rec := &auth.RecordRef{DocType: m024Close, ID: 1, CreatedBy: w.Users["admin@vardhan.test"]}

	// Can and CanTx.
	for _, r := range []*auth.RecordRef{nil, rec} {
		if err := w.Auth.Can(ctx, holder, m024Close, auth.RightApprove, r); err != nil {
			t.Errorf("Can(holder, approve, %v) = %v, want nil", r, err)
		}
		m024Denied(t, "Can(submitter, approve)", w.Auth.Can(ctx, other, m024Close, auth.RightApprove, r), m024CloseLabel, "Close requests")
	}
	var holderTx, otherTx error
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		holderTx = w.Auth.CanTx(ctx, tx, holder, m024Close, auth.RightApprove, rec)
		otherTx = w.Auth.CanTx(ctx, tx, other, m024Close, auth.RightApprove, rec)
		return nil
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	if holderTx != nil {
		t.Errorf("CanTx(holder, approve) = %v, want nil", holderTx)
	}
	m024Denied(t, "CanTx(submitter, approve)", otherTx, m024CloseLabel, "Close requests")

	// Scope.
	if pred, err := w.Auth.Scope(ctx, holder, m024Close, auth.RightApprove); err != nil || pred.SQL != "" {
		t.Errorf("Scope(holder, approve) = (%+v, %v), want no filter, nil", pred, err)
	}
	_, err := w.Auth.Scope(ctx, other, m024Close, auth.RightApprove)
	m024Denied(t, "Scope(submitter, approve)", err, m024CloseLabel, "Close requests")

	// WhoCan lists the holder's role at scope all with the holder, and not
	// the submitter's role.
	roles, err := w.Auth.WhoCan(ctx, m024Close, auth.RightApprove)
	if err != nil {
		t.Fatalf("WhoCan(%s, approve) = %v, want the holder listed", m024Close, err)
	}
	var found bool
	for _, r := range roles {
		if r.Role == m024OtherRole {
			t.Errorf("WhoCan(%s, approve) lists %s, which holds only submit", m024Close, m024OtherRole)
		}
		if r.Role == m024HolderRole {
			found = true
			ids := make([]auth.UserID, 0, len(r.Users))
			for _, u := range r.Users {
				ids = append(ids, u.ID)
			}
			if r.Scope != auth.ScopeAll || !slices.Equal(ids, []auth.UserID{holder.ID}) {
				t.Errorf("WhoCan(%s, approve) role %s = scope %s users %v, want all, [%d]", m024Close, r.Role, r.Scope, ids, holder.ID)
			}
		}
	}
	if !found {
		t.Errorf("WhoCan(%s, approve) = %+v, want role %s listed", m024Close, roles, m024HolderRole)
	}

	users, err := w.Auth.UsersWithRight(ctx, m024Close, auth.RightApprove, nil)
	if err != nil || !slices.Contains(users, holder.ID) || slices.Contains(users, other.ID) {
		t.Errorf("UsersWithRight(%s, approve) = %v, %v; want the holder %d, not the submitter %d", m024Close, users, err, holder.ID, other.ID)
	}

	// AllowedRights includes approve: true for the holder, false for the
	// submitter (it applies), and has no approve on the twin.
	for _, tc := range []struct {
		p    auth.Principal
		want bool
	}{{holder, true}, {other, false}} {
		got, err := w.Auth.AllowedRights(ctx, tc.p, m024Close, rec)
		if v, ok := got[auth.RightApprove]; err != nil || !ok || v != tc.want {
			t.Errorf("AllowedRights(user %d, %s)[approve] = %v (present %v), err %v; want %v", tc.p.ID, m024Close, v, ok, err, tc.want)
		}
	}
	admin := w.P(t, "admin@vardhan.test")
	got, err := w.Auth.AllowedRights(ctx, admin, m024Twin, nil)
	if _, ok := got[auth.RightApprove]; err != nil || ok {
		t.Errorf("AllowedRights(admin, %s) = %v, %v; want no approve (not applicable)", m024Twin, got, err)
	}
	if _, ok := got[auth.RightSubmit]; !ok {
		t.Errorf("AllowedRights(admin, %s) = %v, want submit listed", m024Twin, got)
	}

	// The twin still fails closed on approve, Administrator included.
	m024Denied(t, "Can(admin, twin, approve)", w.Auth.Can(ctx, admin, m024Twin, auth.RightApprove, nil), m024TwinLabel, "Twin requests")
	var p *auth.Problem
	if _, err := w.Auth.WhoCan(ctx, m024Twin, auth.RightApprove); !errors.As(err, &p) || p.Msg != m024TwinNotApplicable || p.Code != 400 {
		t.Errorf("WhoCan(%s, approve) = %v, want 400 %q", m024Twin, err, m024TwinNotApplicable)
	}
}

// CD-280 D2 is approve only: a non-approvable DocType whose CloseRight is
// anything else ("amend", "none") still refuses approve everywhere.
func TestCloseRight_OtherCloseRightRefusesApprove(t *testing.T) {
	ctx := context.Background()
	others := m024RegisterOthers()
	w := authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")
	for i, dt := range others {
		o := m024Others[i]
		if dt.Approvable || !dt.Submittable || dt.CloseRight != o.closeRight {
			t.Fatalf("setup: %s must be submittable, not approvable, CloseRight %q", o.name, o.closeRight)
		}
		for _, r := range append(slices.Clone(auth.AllRights), "foo") {
			if got, want := auth.Applies(dt, r), m024Applies(dt.Name, r); got != want {
				t.Errorf("Applies(%s, %s) = %v, want %v", dt.Name, r, got, want)
			}
		}
		row := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightApprove: auth.ScopeAll}
		t3WantProblem(t, "ValidateRow("+o.name+", {read: all, approve: all})", auth.ValidateRow(dt, row), o.notApplicable)
		m024Denied(t, "Can(admin, "+o.name+", approve)", w.Auth.Can(ctx, admin, o.name, auth.RightApprove, nil), o.label, o.plural)
	}
}
