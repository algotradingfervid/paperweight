package goodsreceipt_test

import (
	"errors"
	"maps"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// g1CloseText is G0a's close refusal for the GRN: "{PluralLabel} can't be
// closed." (CD-279 (2), CD-294).
const g1CloseText = "Goods Receipts can't be closed."

// g1Scoped builds a role's rights row at one scope.
func g1Scoped(scope string, rights ...string) map[string]string {
	m := map[string]string{}
	for _, r := range rights {
		m[r] = scope
	}
	return m
}

// GRN-034 (GRN spec §5, §10, §14 item 4; 00-fixtures.md Goods Receipt
// rights; CD-264 (7), CD-277 (1), CD-279 (2), CD-294). goodsreceipt.Receipt's
// DefaultRights are spec §10's rows with no Administrator key, and the real
// auth.Service seeds and enforces exactly them: every fixture role × every
// right in auth.AllRights, and every fixture user on a GRN they own and one
// they don't (Stores deletes only its own). The Administrator holds every
// applicable right. CloseRight is doctype.CloseNone, and close of an approved
// GRN through the engine is refused with G0a's text for Stores, Purchase
// Manager, two read-only roles (Director, Purchase User: the refusal needs
// only read, never a right a close would need) and the Administrator, with
// nothing written.
func TestGRN_DefaultRights(t *testing.T) {
	dt := g1DocType(t)
	stores := g1Scoped("all", "read", "create", "write", "submit", "amend", "print")
	stores["delete"] = "own"
	want := map[string]map[string]string{
		"Stores":           stores,
		"Purchase User":    g1Scoped("all", "read", "print", "export"),
		"Purchase Manager": g1Scoped("all", "read", "cancel", "print", "export"),
		"Director":         g1Scoped("all", "read", "print", "export"),
	}
	if !maps.EqualFunc(dt.DefaultRights, want, maps.Equal) {
		t.Errorf("%s DefaultRights = %v\nwant %v (GRN spec §10; no Administrator key)", dt.Name, dt.DefaultRights, want)
	}
	if dt.CloseRight != doctype.CloseNone {
		t.Errorf("%s CloseRight = %q, want %q (CD-264 (7), CD-277 (1))", dt.Name, dt.CloseRight, doctype.CloseNone)
	}

	env := grntest.New(t)
	ctx := t.Context()

	// Every fixture role × every right, as the real AUTH stores it. Roles
	// with no GRN row hold nothing.
	for _, role := range []string{"Director", "Purchase Manager", "Purchase User", "Stores", "Masters Manager", "Employee", "Fallback Approver"} {
		got, err := env.Auth.RoleRights(ctx, role, goodsreceipt.DocType)
		if err != nil {
			t.Fatalf("RoleRights(%s, %s): %v", role, goodsreceipt.DocType, err)
		}
		for _, r := range auth.AllRights {
			if g, w := got[r], auth.Scope(want[role][string(r)]); g != w {
				t.Errorf("real auth: %s %s on %s = %q, want %q", role, r, goodsreceipt.DocType, g, w)
			}
		}
	}

	// Every fixture user × every right through Can, on a METRO-SITE /
	// PRJ-METRO GRN they created and on one the Administrator created.
	site, err := strconv.ParseInt(g1MasterID(t, env.DB, "site", "METRO-SITE"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	project, err := strconv.ParseInt(g1MasterID(t, env.DB, "project", "PRJ-METRO"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(owner auth.UserID) *auth.RecordRef {
		return &auth.RecordRef{DocType: goodsreceipt.DocType, ID: 1, CreatedBy: owner, ProjectIDs: []int64{project}, SiteIDs: []int64{site}}
	}
	all := func(rights ...auth.Right) map[auth.Right]bool {
		m := map[auth.Right]bool{}
		for _, r := range rights {
			m[r] = true
		}
		return m
	}
	storesAll := all(auth.RightRead, auth.RightCreate, auth.RightWrite, auth.RightSubmit, auth.RightAmend, auth.RightPrint)
	storesOwn := maps.Clone(storesAll)
	storesOwn[auth.RightDelete] = true
	admin := all(auth.RightRead, auth.RightCreate, auth.RightWrite, auth.RightSubmit, auth.RightCancel, auth.RightAmend,
		auth.RightDelete, auth.RightApprove, auth.RightPrint, auth.RightExport)
	if auth.Applies(dt, auth.RightImport) {
		admin[auth.RightImport] = true
	}
	for _, c := range []struct {
		who        string
		p          auth.Principal
		own, other map[auth.Right]bool
	}{
		{"admin (Administrator)", env.Admin, admin, admin},
		{"suresh (Stores)", env.Suresh, storesOwn, storesAll},
		{"priya (Purchase User)", env.Priya, all(auth.RightRead, auth.RightPrint, auth.RightExport), all(auth.RightRead, auth.RightPrint, auth.RightExport)},
		{"ravi (Purchase Manager)", env.Ravi, all(auth.RightRead, auth.RightCancel, auth.RightPrint, auth.RightExport), all(auth.RightRead, auth.RightCancel, auth.RightPrint, auth.RightExport)},
		{"meena (Director)", env.Meena, all(auth.RightRead, auth.RightPrint, auth.RightExport), all(auth.RightRead, auth.RightPrint, auth.RightExport)},
		{"kavya (Masters Manager)", env.Kavya, all(), all()},
		{"deepak (Employee)", env.Deepak, all(), all()},
	} {
		for _, rec := range []struct {
			what string
			ref  *auth.RecordRef
			want map[auth.Right]bool
		}{
			{"own GRN", ref(c.p.ID), c.own},
			{"another user's GRN", ref(env.Admin.ID), c.other},
		} {
			if c.p.ID == env.Admin.ID && rec.what == "another user's GRN" {
				rec.ref = ref(env.Suresh.ID)
			}
			for _, r := range auth.AllRights {
				err := env.Auth.Can(ctx, c.p, goodsreceipt.DocType, r, rec.ref)
				var de *auth.DeniedError
				switch {
				case rec.want[r] && err != nil:
					t.Errorf("real auth: %s %s on %s: %v, want allowed", c.who, r, rec.what, err)
				case !rec.want[r] && !errors.As(err, &de):
					t.Errorf("real auth: %s %s on %s: err = %v, want AUTH's 403 (*auth.DeniedError)", c.who, r, rec.what, err)
				}
			}
		}
	}

	// Close through the engine: an approved GRN by Suresh on an approved PO.
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := g1Receipt(t, env, po, doctype.StateApproved, g1Line{poLine: 0, received: "40"})
	auditRows := g1Count(t, env.DB, "audit_log", "1 = 1")
	for _, c := range []struct {
		who string
		p   auth.Principal
	}{
		{"suresh (Stores)", env.Suresh},
		{"ravi (Purchase Manager)", env.Ravi},
		{"meena (Director, read-only)", env.Meena},
		{"priya (Purchase User, read-only)", env.Priya},
		{"admin (Administrator)", env.Admin},
	} {
		_, err := env.Engine.Do(ctx, c.p, goodsreceipt.DocType, grn.ID, doctype.ActionClose,
			doc.ActionInput{Version: grn.Version, Reason: "Vendor stopped supply"})
		var de *doc.Error
		if !errors.As(err, &de) || de.Kind != doc.KindRefused || de.Status() != 409 || de.Error() != g1CloseText {
			t.Errorf("close GRN as %s: err = %v (%T); want doc.Refused 409 %q", c.who, err, err, g1CloseText)
		}
		after := g1Load(t, env, grn.ID)
		if after.State != doctype.StateApproved || after.Version != grn.Version || !after.UpdatedAt.Equal(grn.UpdatedAt) || after.StateReason != grn.StateReason {
			t.Errorf("after close as %s: state %s version %d updated %v reason %q; want unchanged %s %d %v %q",
				c.who, after.State, after.Version, after.UpdatedAt, after.StateReason, grn.State, grn.Version, grn.UpdatedAt, grn.StateReason)
		}
		if got := g1Count(t, env.DB, "audit_log", "1 = 1"); got != auditRows {
			t.Errorf("audit_log rows after close as %s = %d, want %d (nothing written)", c.who, got, auditRows)
		}
	}
}
