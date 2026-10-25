package auth_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

const (
	t13Priya  = "priya@vardhan.test"
	t13Suresh = "suresh@vardhan.test"
)

func t13PermVersion(t *testing.T, w *authtest.World) int {
	t.Helper()
	return t7Count(t, w, `SELECT perm_version FROM auth_meta WHERE id = 1`)
}

func t13Version(t *testing.T, w *authtest.World, email string) int64 {
	t.Helper()
	return int64(t7Count(t, w, `SELECT version FROM users WHERE id = ?`, int64(w.Users[email])))
}

func TestListUsers_AdminOnlyAscending(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()

	_, err := w.Auth.ListUsers(ctx, w.P(t, t13Priya))
	t7WantProblem(t, "priya lists users", err, auth.ErrAdminOnlyUsers)

	got, err := w.Auth.ListUsers(ctx, w.P(t, t7Admin))
	if err != nil {
		t.Fatalf("admin lists users: %v", err)
	}
	if len(got) != len(w.Users) {
		t.Fatalf("admin lists %d users, want %d", len(got), len(w.Users))
	}
	if !slices.IsSortedFunc(got, func(a, b auth.UserInfo) int { return int(a.ID - b.ID) }) {
		t.Errorf("users not in ascending ID order: %+v", got)
	}
	for _, u := range got {
		if w.Users[u.Email] != u.ID {
			t.Errorf("user %+v: ID of %s is %d", u, u.Email, w.Users[u.Email])
		}
	}
}

func TestChangeRestrictions_AddRemove(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	suresh := w.Users[t13Suresh]
	pune, metro, tech := w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]

	pv := t13PermVersion(t, w)
	entries := len(w.Audit.Entries())
	if err := w.Auth.ChangeRestrictions(ctx, admin, suresh, auth.DimSite, []int64{tech}, []int64{pune}, nil); err != nil {
		t.Fatalf("admin changes suresh's sites: %v", err)
	}
	got, err := w.Auth.UserRestrictions(ctx, suresh)
	if err != nil {
		t.Fatalf("UserRestrictions: %v", err)
	}
	want := map[auth.Dimension][]int64{auth.DimSite: {metro, tech}}
	slices.Sort(want[auth.DimSite])
	if !reflect.DeepEqual(got, want) {
		t.Errorf("suresh restrictions = %v, want %v", got, want)
	}
	if n := t13PermVersion(t, w); n != pv+1 {
		t.Errorf("perm_version = %d, want %d", n, pv+1)
	}
	es := w.Audit.Entries()
	if len(es) != entries+1 {
		t.Fatalf("audit entries = %d, want %d", len(es), entries+1)
	}
	if e := es[len(es)-1]; e.DocType != "User" || e.DocID != int64(suresh) || e.Action != "update" || e.By != admin.ID ||
		len(e.Changes) != 1 || e.Changes[0].Field != "sites" {
		t.Errorf("audit entry = %+v, want a User update of sites by admin", e)
	}

	// No change: no perm_version bump and no audit entry.
	if err := w.Auth.ChangeRestrictions(ctx, admin, suresh, auth.DimSite, []int64{metro}, []int64{pune}, nil); err != nil {
		t.Fatalf("admin repeats suresh's sites: %v", err)
	}
	if n := t13PermVersion(t, w); n != pv+1 {
		t.Errorf("perm_version after a no-op = %d, want %d", n, pv+1)
	}
	if n := len(w.Audit.Entries()); n != entries+1 {
		t.Errorf("audit entries after a no-op = %d, want %d", n, entries+1)
	}

	err = w.Auth.ChangeRestrictions(ctx, admin, auth.UserID(9999), auth.DimSite, []int64{tech}, nil, nil)
	t7WantProblem(t, "unknown user", err, auth.ErrNotFound)
}

func TestChangeRestrictions_AdminOnly(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	suresh := w.Users[t13Suresh]
	before, err := w.Auth.UserRestrictions(ctx, suresh)
	if err != nil {
		t.Fatalf("UserRestrictions: %v", err)
	}
	pv := t13PermVersion(t, w)

	err = w.Auth.ChangeRestrictions(ctx, w.P(t, t13Suresh), suresh, auth.DimSite, []int64{w.Sites["TECH-SITE"]}, nil, nil)
	t7WantProblem(t, "suresh changes own sites", err, auth.ErrAdminOnlyAccess)

	if got, _ := w.Auth.UserRestrictions(ctx, suresh); !reflect.DeepEqual(got, before) {
		t.Errorf("restrictions = %v, want unchanged %v", got, before)
	}
	if n := t13PermVersion(t, w); n != pv {
		t.Errorf("perm_version = %d, want unchanged %d", n, pv)
	}
}

func TestUpdateUser_Version(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	priya := w.Users[t13Priya]
	mobile := func(m string) *string { return &m }
	stored := func() string {
		return t7Strings(t, w, `SELECT mobile FROM users WHERE id = ?`, int64(priya))[0]
	}

	v := t13Version(t, w, t13Priya)
	if _, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Mobile: mobile("+91 1"), Version: &v}); err != nil {
		t.Fatalf("current version: %v", err)
	}
	if got := stored(); got != "+91 1" {
		t.Fatalf("mobile after current-version save = %q, want +91 1", got)
	}

	_, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Mobile: mobile("+91 2"), Version: &v})
	t7WantText(t, "stale version", err, "Someone else changed Priya Shah after you opened it. Reload it and make your changes again.")
	if got := stored(); got != "+91 1" {
		t.Errorf("mobile after stale save = %q, want unchanged +91 1", got)
	}

	if _, err := w.Auth.UpdateUser(ctx, admin, priya, auth.UserPatch{Mobile: mobile("+91 3")}); err != nil {
		t.Fatalf("no version: %v", err)
	}
	if got := stored(); got != "+91 3" {
		t.Errorf("mobile after save without version = %q, want +91 3", got)
	}
}

func TestChangeRestrictions_Version(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	suresh := w.Users[t13Suresh]
	tech := w.Sites["TECH-SITE"]
	name := t7Strings(t, w, `SELECT name FROM users WHERE id = ?`, int64(suresh))[0]
	changed := auth.UserChangedError(name).Error()

	old := t13Version(t, w, t13Suresh)
	w.Clock.Advance(time.Hour)
	if err := w.Auth.ChangeRestrictions(ctx, admin, suresh, auth.DimSite, []int64{tech}, nil, &old); err != nil {
		t.Fatalf("current version: %v", err)
	}
	if got := t7Strings(t, w, `SELECT updated_at FROM users WHERE id = ?`, int64(suresh))[0]; got != auth.FormatTime(w.Clock.Now()) {
		t.Errorf("updated_at after ChangeRestrictions = %q, want %q", got, auth.FormatTime(w.Clock.Now()))
	}
	if v := t13Version(t, w, t13Suresh); v != old+1 {
		t.Errorf("version after ChangeRestrictions = %d, want %d", v, old+1)
	}
	if n := t7Count(t, w, `SELECT count(*) FROM users WHERE id = ? AND updated_by = ?`, int64(suresh), int64(admin.ID)); n != 1 {
		t.Errorf("updated_by not set to the admin")
	}
	before, _ := w.Auth.UserRestrictions(ctx, suresh)

	_, err := w.Auth.UpdateUser(ctx, admin, suresh, auth.UserPatch{Sites: &[]int64{}, Version: &old})
	t7WantText(t, "UpdateUser sites at the old version", err, changed)
	err = w.Auth.ChangeRestrictions(ctx, admin, suresh, auth.DimSite, nil, []int64{tech}, &old)
	t7WantText(t, "ChangeRestrictions at the old version", err, changed)
	if got, _ := w.Auth.UserRestrictions(ctx, suresh); !reflect.DeepEqual(got, before) {
		t.Errorf("restrictions = %v, want unchanged %v", got, before)
	}
}

func TestChangeRestrictions_BadDimension(t *testing.T) {
	w := authtest.NewWorld(t)
	err := w.Auth.ChangeRestrictions(context.Background(), w.P(t, t7Admin), w.Users[t13Suresh], auth.Dimension("team"), []int64{1}, nil, nil)
	var p *auth.Problem
	if !errors.As(err, &p) || p.Status() != 400 {
		t.Errorf("unknown dimension: err = %v, want a 400 Problem", err)
	}
}

func TestDeleteUser_Version(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	admin := w.P(t, t7Admin)
	kiran := w.AddUser(t, t7Kiran, "Kiran Joshi", "Employee")
	stale := t13Version(t, w, t7Kiran) - 1
	err := w.Auth.DeleteUser(ctx, admin, kiran, &stale)
	t7WantText(t, "stale version", err, "Someone else changed Kiran Joshi after you opened it. Reload it and make your changes again.")
	if n := t7Count(t, w, `SELECT count(*) FROM users WHERE id = ?`, int64(kiran)); n != 1 {
		t.Errorf("stale delete removed the user")
	}
	current := stale + 1
	if err := w.Auth.DeleteUser(ctx, admin, kiran, &current); err != nil {
		t.Errorf("current version: %v", err)
	}
}

func TestUserForEdit(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	suresh := w.Users[t13Suresh]
	want, err := w.Auth.UserRestrictions(ctx, suresh)
	if err != nil {
		t.Fatalf("UserRestrictions: %v", err)
	}
	for _, actor := range []string{t7Admin, t13Suresh} {
		u, v, rs, err := w.Auth.UserForEdit(ctx, w.P(t, actor), suresh)
		if err != nil {
			t.Fatalf("%s reads suresh: %v", actor, err)
		}
		if u.ID != suresh || u.Email != t13Suresh || u.Name != t7Strings(t, w, `SELECT name FROM users WHERE id = ?`, int64(suresh))[0] || !u.Enabled {
			t.Errorf("%s reads suresh: user = %+v", actor, u)
		}
		if wv := t13Version(t, w, t13Suresh); v != wv {
			t.Errorf("%s reads suresh: version = %d, want %d", actor, v, wv)
		}
		if !reflect.DeepEqual(rs, want) {
			t.Errorf("%s reads suresh: restrictions = %v, want %v", actor, rs, want)
		}
	}
	_, _, _, err = w.Auth.UserForEdit(ctx, w.P(t, t13Priya), suresh)
	t7WantProblem(t, "priya reads suresh", err, auth.ErrAdminOnlyField)
	_, _, _, err = w.Auth.UserForEdit(ctx, w.P(t, t7Admin), auth.UserID(9999))
	t7WantProblem(t, "unknown user", err, auth.ErrNotFound)
}
