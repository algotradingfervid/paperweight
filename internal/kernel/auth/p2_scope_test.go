package auth_test

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// t18PendingPO inserts PO "Draft 6" (PRJ-METRO / METRO-SITE, created by
// Priya) in pending_approval and returns its RecordRef (AUTH-084's PO).
func t18PendingPO(t *testing.T, w *authtest.World) *auth.RecordRef {
	t.Helper()
	const id = 6
	t5InsertPO(t, w, id, "", w.Users["priya@vardhan.test"], w.Projects["PRJ-METRO"], w.Sites["METRO-SITE"])
	t6Exec(t, w, `UPDATE probe_order SET state = ? WHERE id = ?`, string(doctype.StatePending), id)
	return t6aRef(t, w, authtest.DTOrder, id)
}

// AllowedRights must agree with Can for every right and every record
// (AUTH's share of AUTH-084).
func TestAllowedRights_MatchesCan(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	ref := t18PendingPO(t, w)
	if w.Projects["PRJ-TECHPARK"] == 0 || w.Sites["TECH-SITE"] == 0 {
		t.Fatalf("fixture has no PRJ-TECHPARK / TECH-SITE")
	}
	// A PO at PRJ-TECHPARK created by Priya: outside her project restriction.
	t5InsertPO(t, w, 7, "", w.Users["priya@vardhan.test"], w.Projects["PRJ-TECHPARK"], w.Sites["TECH-SITE"])
	techpark := t6aRef(t, w, authtest.DTOrder, 7)
	// A PO at PRJ-METRO created by Ravi: Priya can see it but does not own it.
	t5InsertPO(t, w, 8, "", w.Users["ravi@vardhan.test"], w.Projects["PRJ-METRO"], w.Sites["METRO-SITE"])
	ravis := t6aRef(t, w, authtest.DTOrder, 8)

	allowed := func(email, what string, rec *auth.RecordRef) map[auth.Right]bool {
		t.Helper()
		got, err := w.Auth.AllowedRights(ctx, w.P(t, email), authtest.DTOrder, rec)
		if err != nil {
			t.Fatalf("AllowedRights(%s, %s): %v", email, what, err)
		}
		return got
	}

	priya := allowed("priya@vardhan.test", "PO Draft 6", ref)
	if priya[auth.RightApprove] {
		t.Errorf("AllowedRights(priya, PO Draft 6)[approve] = true, want false (Priya has no approve)")
	}
	if !priya[auth.RightSubmit] {
		t.Errorf("AllowedRights(priya, PO Draft 6)[submit] = false, want true (Withdraw needs submit)")
	}
	if !priya[auth.RightDelete] {
		t.Errorf("AllowedRights(priya, PO Draft 6)[delete] = false, want true (own scope, Priya created it)")
	}

	ravi := allowed("ravi@vardhan.test", "PO Draft 6", ref)
	if !ravi[auth.RightApprove] {
		t.Errorf("AllowedRights(ravi, PO Draft 6)[approve] = false, want true")
	}
	for _, r := range []auth.Right{auth.RightWrite, auth.RightDelete, auth.RightSubmit} {
		if ravi[r] {
			t.Errorf("AllowedRights(ravi, PO Draft 6)[%s] = true, want false", r)
		}
	}

	// (a) A record Priya can't see: every right is refused.
	for _, r := range auth.AllRights {
		if allowed("priya@vardhan.test", "PO 7 at PRJ-TECHPARK", techpark)[r] {
			t.Errorf("AllowedRights(priya, PO 7 at PRJ-TECHPARK)[%s] = true, want false (outside her PRJ-METRO restriction)", r)
		}
	}
	// Control: the same record is visible to an unrestricted user.
	if !allowed("ravi@vardhan.test", "PO 7 at PRJ-TECHPARK", techpark)[auth.RightRead] {
		t.Errorf("AllowedRights(ravi, PO 7 at PRJ-TECHPARK)[read] = false, want true (Ravi is unrestricted)")
	}

	// (b) A record Priya can see but didn't create: own-scope delete is
	// refused, all-scope submit is not.
	onRavis := allowed("priya@vardhan.test", "PO 8 by Ravi", ravis)
	if onRavis[auth.RightDelete] {
		t.Errorf("AllowedRights(priya, PO 8 by Ravi)[delete] = true, want false (delete is own scope; Ravi created it)")
	}
	if !onRavis[auth.RightSubmit] {
		t.Errorf("AllowedRights(priya, PO 8 by Ravi)[submit] = false, want true (submit is all scope)")
	}
	if !onRavis[auth.RightRead] {
		t.Errorf("AllowedRights(priya, PO 8 by Ravi)[read] = false, want true (PRJ-METRO is in her restriction)")
	}

	var granted, refused int
	for _, rec := range []struct {
		what string
		ref  *auth.RecordRef
	}{{"PO Draft 6", ref}, {"PO 7 at PRJ-TECHPARK", techpark}, {"PO 8 by Ravi", ravis}} {
		for _, email := range []string{
			"admin@vardhan.test", "meena@vardhan.test", "ravi@vardhan.test", "priya@vardhan.test",
			"suresh@vardhan.test", "kavya@vardhan.test", "deepak@vardhan.test",
		} {
			p := w.P(t, email)
			got := allowed(email, rec.what, rec.ref)
			for _, r := range auth.AllRights {
				canErr := w.Auth.Can(ctx, p, authtest.DTOrder, r, rec.ref)
				if got[r] != (canErr == nil) {
					t.Errorf("%s on %s: AllowedRights[%s] = %v, but Can(%s) = %v", email, rec.what, r, got[r], r, canErr)
				}
				if canErr == nil {
					granted++
				} else {
					refused++
				}
			}
			for r := range got {
				if !slices.Contains(auth.AllRights, r) {
					t.Errorf("%s on %s: AllowedRights has unknown right %q", email, rec.what, r)
				}
			}
		}
	}
	if granted == 0 || refused == 0 {
		t.Fatalf("agreement check is vacuous: granted %d, refused %d", granted, refused)
	}
}

// ReadableDocTypes returns exactly the DocTypes the principal can read
// (Task 5 review carry-over).
func TestReadableDocTypes_ReturnsReadable(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.AddUser(t, "store1@vardhan.test", "Store One", "Stores")

	got, err := w.Auth.ReadableDocTypes(ctx, w.P(t, "store1@vardhan.test"))
	if err != nil {
		t.Fatalf("ReadableDocTypes(Stores): %v", err)
	}
	var probeTypes []string
	for _, name := range got {
		if strings.HasPrefix(name, "authtest.") {
			probeTypes = append(probeTypes, name)
		}
	}
	slices.Sort(probeTypes)
	if want := []string{authtest.DTOrder, authtest.DTReceipt}; !reflect.DeepEqual(probeTypes, want) {
		t.Errorf("ReadableDocTypes(Stores) authtest DocTypes = %q, want %q (not %s or the masters)", probeTypes, want, authtest.DTClaim)
	}

	got, err = w.Auth.ReadableDocTypes(ctx, w.P(t, "admin@vardhan.test"))
	if err != nil {
		t.Fatalf("ReadableDocTypes(admin): %v", err)
	}
	var want []string
	for _, dt := range doctype.All() {
		want = append(want, dt.Name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ReadableDocTypes(admin) = %q, want every registered DocType %q", got, want)
	}
}
