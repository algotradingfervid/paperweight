package auth_test

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// CD-157 (C): authfake's forbidden text is the real service's DeniedError
// text for the same right and DocType.
func TestAuthfake_DeniedTextMatchesReal(t *testing.T) {
	const clerk = "clerk@vardhan.test"
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.AddUser(t, clerk, "Clerk User")
	p := w.P(t, clerk)
	f := authfake.New()
	for _, r := range []auth.Right{auth.RightRead, auth.RightWrite} {
		t.Run(string(r), func(t *testing.T) {
			real, fake := w.Auth.Can(ctx, p, authtest.DTOrder, r, nil), f.Can(ctx, p, authtest.DTOrder, r, nil)
			var rd, fd *auth.DeniedError
			if !errors.As(real, &rd) || !errors.As(fake, &fd) {
				t.Fatalf("real = %v, authfake = %v; want *auth.DeniedError from both", real, fake)
			}
			if rd.Error() != fd.Error() {
				t.Errorf("authfake text = %q, real = %q", fd.Error(), rd.Error())
			}
		})
	}
}

// CD-162: authfake's CheckValues text is the real service's ValueError text
// for the same site restriction and a disallowed site.
func TestAuthfake_ValueTextMatchesReal(t *testing.T) {
	const clerk = "clerk@vardhan.test"
	ctx := context.Background()
	w := authtest.NewWorld(t)
	w.AddUser(t, clerk, "Clerk User")
	p := w.P(t, clerk)
	codes := slices.Sorted(maps.Keys(w.Sites))
	if len(codes) < 3 {
		t.Fatalf("world has %d sites; want at least 3", len(codes))
	}
	allowed, other := []string{codes[2], codes[0]}, codes[1]
	w.Restrict(t, clerk, nil, allowed)

	dir := authfake.NewDirectory()
	f := authfake.New()
	f.UseDirectory(dir)
	var ids []int64
	for _, c := range allowed {
		dir.SetSiteCode(w.Sites[c], c)
		ids = append(ids, w.Sites[c])
	}
	f.Restrict(p.ID, nil, ids)

	values := map[string][]int64{"site_id": {w.Sites[other]}}
	real, fake := w.Auth.CheckValues(ctx, p, authtest.DTOrder, values), f.CheckValues(ctx, p, authtest.DTOrder, values)
	var rv, fv *auth.ValueError
	if !errors.As(real, &rv) || !errors.As(fake, &fv) {
		t.Fatalf("real = %v, authfake = %v; want *auth.ValueError from both", real, fake)
	}
	if !errors.Is(fake, auth.ErrForbidden) {
		t.Errorf("authfake error %v is not ErrForbidden", fake)
	}
	if rv.Error() != fv.Error() {
		t.Errorf("authfake text = %q, real = %q", fv.Error(), rv.Error())
	}
}

// CD-157 fallback: an unregistered DocType's denial uses its name as the label.
func TestAuthfake_DeniedUnregisteredUsesName(t *testing.T) {
	err := authfake.New().Can(context.Background(), auth.Principal{ID: 1}, "nosuch.Type", auth.RightRead, nil)
	var d *auth.DeniedError
	if !errors.As(err, &d) || !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Can = %v; want a *auth.DeniedError that is ErrForbidden", err)
	}
	if d.Label != "nosuch.Type" || d.Plural != "nosuch.Type" {
		t.Errorf("label, plural = %q, %q; want the name for both", d.Label, d.Plural)
	}
}
