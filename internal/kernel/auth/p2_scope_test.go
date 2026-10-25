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

// AllowedRights must agree with Can for every right (AUTH's share of AUTH-084).
func TestAllowedRights_MatchesCan(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	ref := t18PendingPO(t, w)

	allowed := func(email string) map[auth.Right]bool {
		t.Helper()
		got, err := w.Auth.AllowedRights(ctx, w.P(t, email), authtest.DTOrder, ref)
		if err != nil {
			t.Fatalf("AllowedRights(%s, PO Draft 6): %v", email, err)
		}
		return got
	}

	priya := allowed("priya@vardhan.test")
	if priya[auth.RightApprove] {
		t.Errorf("AllowedRights(priya)[approve] = true, want false (Priya has no approve)")
	}
	if !priya[auth.RightSubmit] {
		t.Errorf("AllowedRights(priya)[submit] = false, want true (Withdraw needs submit)")
	}

	ravi := allowed("ravi@vardhan.test")
	if !ravi[auth.RightApprove] {
		t.Errorf("AllowedRights(ravi)[approve] = false, want true")
	}
	for _, r := range []auth.Right{auth.RightWrite, auth.RightDelete, auth.RightSubmit} {
		if ravi[r] {
			t.Errorf("AllowedRights(ravi)[%s] = true, want false", r)
		}
	}

	var granted, refused int
	for _, email := range []string{
		"admin@vardhan.test", "meena@vardhan.test", "ravi@vardhan.test", "priya@vardhan.test",
		"suresh@vardhan.test", "kavya@vardhan.test", "deepak@vardhan.test",
	} {
		p := w.P(t, email)
		got := allowed(email)
		for _, r := range auth.AllRights {
			canErr := w.Auth.Can(ctx, p, authtest.DTOrder, r, ref)
			if got[r] != (canErr == nil) {
				t.Errorf("%s: AllowedRights[%s] = %v, but Can(%s) = %v", email, r, got[r], r, canErr)
			}
			if canErr == nil {
				granted++
			} else {
				refused++
			}
		}
		for r := range got {
			if !slices.Contains(auth.AllRights, r) {
				t.Errorf("%s: AllowedRights has unknown right %q", email, r)
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
