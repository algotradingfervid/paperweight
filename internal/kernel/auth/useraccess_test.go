package auth_test

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// t20Sites returns the restrictions on codes, in id order, labelled by code.
func t20Sites(w *authtest.World, codes ...string) []auth.Restriction {
	var out []auth.Restriction
	for _, c := range codes {
		out = append(out, auth.Restriction{ID: w.Sites[c], Label: c})
	}
	slices.SortFunc(out, func(a, b auth.Restriction) int { return int(a.ID - b.ID) })
	return out
}

// Task 20: UserAccess returns the user, the version and each restricted
// value's id with its label, in id order; a disabled value is marked.
func TestUserAccess(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	w.Restrict(t, t13Suresh, nil, []string{"PUNE-WH", "METRO-SITE", "TECH-SITE"})
	suresh := w.Users[t13Suresh]

	u, v, rs, err := w.Auth.UserAccess(ctx, w.P(t, t7Admin), suresh)
	if err != nil {
		t.Fatalf("UserAccess(suresh): %v", err)
	}
	if u.ID != suresh || u.Email != t13Suresh || u.Name != t7Strings(t, w, `SELECT name FROM users WHERE id = ?`, int64(suresh))[0] || !u.Enabled {
		t.Errorf("UserAccess(suresh): user = %+v", u)
	}
	if wv := t13Version(t, w, t13Suresh); v != wv {
		t.Errorf("UserAccess(suresh): version = %d, want %d", v, wv)
	}
	want := map[auth.Dimension][]auth.Restriction{auth.DimSite: t20Sites(w, "PUNE-WH", "METRO-SITE", "TECH-SITE")}
	if !reflect.DeepEqual(rs, want) {
		t.Fatalf("UserAccess(suresh): restrictions = %+v, want %+v", rs, want)
	}

	w.SetMasterEnabled(t, authtest.DTSite, "METRO-SITE", false)
	_, _, rs, err = w.Auth.UserAccess(ctx, w.P(t, t7Admin), suresh)
	if err != nil {
		t.Fatalf("UserAccess(suresh) after disabling METRO-SITE: %v", err)
	}
	for i, r := range want[auth.DimSite] {
		if r.Label == "METRO-SITE" {
			want[auth.DimSite][i].Label = "METRO-SITE (disabled)"
		}
	}
	if !reflect.DeepEqual(rs, want) {
		t.Fatalf("UserAccess(suresh) after disabling METRO-SITE: restrictions = %+v, want %+v", rs, want)
	}
}

// Task 20: a value the Directory has no code for shows as its id, and the
// values after it keep their own ids and labels; RestrictionLabels agrees.
func TestUserAccess_MissingCode(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	w.Restrict(t, t13Suresh, nil, []string{"PUNE-WH", "METRO-SITE", "TECH-SITE"})
	want := t20Sites(w, "PUNE-WH", "METRO-SITE", "TECH-SITE")
	// Blank the code of the middle value.
	w.Dir.SetSiteCode(want[1].ID, "")
	want[1].Label = strconv.FormatInt(want[1].ID, 10)

	_, _, rs, err := w.Auth.UserAccess(ctx, w.P(t, t7Admin), w.Users[t13Suresh])
	if err != nil {
		t.Fatalf("UserAccess(suresh): %v", err)
	}
	if !reflect.DeepEqual(rs[auth.DimSite], want) {
		t.Fatalf("UserAccess(suresh)[site] = %+v, want %+v", rs[auth.DimSite], want)
	}
	labels, err := w.Auth.RestrictionLabels(ctx, w.Users[t13Suresh])
	if err != nil {
		t.Fatalf("RestrictionLabels(suresh): %v", err)
	}
	if got, wl := labels[auth.DimSite], []string{want[0].Label, want[1].Label, want[2].Label}; !reflect.DeepEqual(got, wl) {
		t.Fatalf("RestrictionLabels(suresh)[site] = %q, want %q", got, wl)
	}
}

// Task 20: only an Administrator may read a user's access, themselves
// included.
func TestUserAccess_AdminOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	for _, actor := range []string{t13Suresh, t13Priya} {
		_, _, rs, err := w.Auth.UserAccess(ctx, w.P(t, actor), w.Users[t13Suresh])
		t7WantProblem(t, actor+" reads suresh's access", err, auth.ErrAdminOnlyAccess)
		if rs != nil {
			t.Errorf("%s reads suresh's access: restrictions = %+v, want none", actor, rs)
		}
	}
	_, _, _, err := w.Auth.UserAccess(ctx, w.P(t, t7Admin), auth.UserID(9999))
	t7WantProblem(t, "unknown user", err, auth.ErrNotFound)
}

// Task 20: MasterFor names the master holding a dimension's values.
func TestMasterFor(t *testing.T) {
	_ = authtest.NewWorld(t)
	for d, name := range map[auth.Dimension]string{auth.DimSite: authtest.DTSite, auth.DimProject: authtest.DTProject} {
		dt, ok := auth.MasterFor(d)
		if !ok || dt.Name != name {
			t.Errorf("MasterFor(%s) = (%v, %v), want %s", d, dt, ok, name)
		}
	}
}
