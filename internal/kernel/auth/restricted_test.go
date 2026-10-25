package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/platform/web"
)

// restrictedService is the CD-174 method; TestService_HasRestricted pins that
// auth.Service has it.
type restrictedService interface {
	Restricted(ctx context.Context, tx *db.Tx, p auth.Principal, dimension string) (bool, error)
}

// restrictedIn calls svc.Restricted inside a read transaction of w's database.
func restrictedIn(t *testing.T, w *authtest.World, svc restrictedService, p auth.Principal, dim string) (bool, error) {
	t.Helper()
	ctx := context.Background()
	var got bool
	var callErr error
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		got, callErr = svc.Restricted(ctx, tx, p, dim)
		return nil
	}); err != nil {
		t.Fatalf("Read: %v", err)
	}
	return got, callErr
}

// wantCallerBug checks that err is a plain error naming dim: not a 403, not a
// 404, not any user-facing HTTPError, and not the stub's ErrNotImplemented.
func wantCallerBug(t *testing.T, what string, err error, dim string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: err = nil, want an error (unknown dimension is a caller bug)", what)
		return
	}
	var he doctype.HTTPError
	if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) || errors.As(err, &he) || errors.Is(err, errs.ErrNotImplemented) {
		t.Errorf("%s: err = %v (%T), want a plain internal error (not ErrForbidden, ErrNotFound, an HTTPError or ErrNotImplemented)", what, err, err)
	}
	if q := fmt.Sprintf("%q", dim); !strings.Contains(err.Error(), q) {
		t.Errorf("%s: message %q, want it to name the dimension %s", what, err.Error(), q)
	}
}

// CD-174: Restricted is part of the §5 Service with the CD's signature.
func TestService_HasRestricted(t *testing.T) {
	m, ok := reflect.TypeFor[auth.Service]().MethodByName("Restricted")
	if !ok {
		t.Fatal("auth.Service has no Restricted method (CD-174)")
	}
	want := reflect.TypeOf((func(context.Context, *db.Tx, auth.Principal, string) (bool, error))(nil))
	if m.Type != want {
		t.Errorf("auth.Service.Restricted type = %v, want %v", m.Type, want)
	}
}

// CD-174: true only on a dimension the principal has a restriction on.
// Canonical World: Priya is restricted to PRJ-METRO, Suresh to PUNE-WH and
// METRO-SITE, Ravi is unrestricted.
func TestRestricted_ByDimension(t *testing.T) {
	w := authtest.NewWorld(t)
	cases := []struct {
		email string
		dim   string
		want  bool
	}{
		{"priya@vardhan.test", string(auth.DimProject), true},
		{"priya@vardhan.test", string(auth.DimSite), false},
		{"suresh@vardhan.test", string(auth.DimSite), true},
		{"suresh@vardhan.test", string(auth.DimProject), false},
		{"ravi@vardhan.test", string(auth.DimProject), false},
		{"ravi@vardhan.test", string(auth.DimSite), false},
	}
	for _, c := range cases {
		got, err := restrictedIn(t, w, w.Auth, w.P(t, c.email), c.dim)
		if err != nil || got != c.want {
			t.Errorf("Restricted(%s, %s) = (%v, %v), want (%v, nil)", c.email, c.dim, got, err, c.want)
		}
	}
}

// CD-174: the Administrator bypass of Can: never restricted, even with
// restriction rows on both dimensions.
func TestRestricted_AdministratorBypass(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	const admin = "admin@vardhan.test"
	w.Restrict(t, admin, []string{"PRJ-METRO"}, []string{"PUNE-WH"})
	labels, err := w.Auth.RestrictionLabels(ctx, w.Users[admin])
	if err != nil || len(labels[auth.DimProject]) == 0 || len(labels[auth.DimSite]) == 0 {
		t.Fatalf("setup: admin's restrictions = (%v, %v), want both dimensions restricted", labels, err)
	}
	// Control: the same rows on a non-Administrator do restrict.
	const ravi = "ravi@vardhan.test"
	w.Restrict(t, ravi, []string{"PRJ-METRO"}, []string{"PUNE-WH"})
	p, control := w.P(t, admin), w.P(t, ravi)
	for _, dim := range []auth.Dimension{auth.DimProject, auth.DimSite} {
		got, err := restrictedIn(t, w, w.Auth, p, string(dim))
		if err != nil || got {
			t.Errorf("Restricted(admin, %s) = (%v, %v), want (false, nil): the Administrator is never restricted", dim, got, err)
		}
		if got, err := restrictedIn(t, w, w.Auth, control, string(dim)); err != nil || !got {
			t.Errorf("Restricted(ravi, %s) with the same rows = (%v, %v), want (true, nil)", dim, got, err)
		}
	}
}

// CD-174: Restricted reads through the caller's tx, so a restriction added
// earlier in the same uncommitted db.Write is seen; after rollback it is gone.
func TestRestricted_ReadsCallerTx(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	const ravi = "ravi@vardhan.test"
	p := w.P(t, ravi)
	rollback := errors.New("roll back")
	var inTx bool
	var inTxErr error
	err := w.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO user_restrictions (user_id, dimension, value_id) VALUES (?, ?, ?)`,
			int64(w.Users[ravi]), string(auth.DimSite), w.Sites["PUNE-WH"]); err != nil {
			return err
		}
		inTx, inTxErr = w.Auth.Restricted(ctx, tx, p, string(auth.DimSite))
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatalf("Write = %v, want the rollback sentinel", err)
	}
	if inTxErr != nil || !inTx {
		t.Errorf("Restricted(ravi, site) inside the Write after adding a site restriction = (%v, %v), want (true, nil)", inTx, inTxErr)
	}
	if got, err := restrictedIn(t, w, w.Auth, p, string(auth.DimSite)); err != nil || got {
		t.Errorf("Restricted(ravi, site) after rollback = (%v, %v), want (false, nil)", got, err)
	}
}

// CD-174: an unknown dimension is a caller bug, a plain error, for any
// principal (the Administrator included, as Can checks Applies first).
func TestRestricted_UnknownDimension(t *testing.T) {
	w := authtest.NewWorld(t)
	for _, email := range []string{"ravi@vardhan.test", "priya@vardhan.test", "admin@vardhan.test"} {
		for _, dim := range []string{"region", ""} {
			got, err := restrictedIn(t, w, w.Auth, w.P(t, email), dim)
			what := fmt.Sprintf("Restricted(%s, %q)", email, dim)
			if got {
				t.Errorf("%s = true, want false with the error", what)
			}
			wantCallerBug(t, what, err, dim)
		}
	}
}

// CD-174: Forbidden(msg) is a 403 that errors.Is ErrForbidden and shows
// exactly msg, also through web.ErrorFrom.
func TestForbidden_CustomMessage(t *testing.T) {
	const msg = "You can only work with your assigned projects. Ask your administrator to add this Project."
	err := auth.Forbidden(msg)
	if err == nil {
		t.Fatal("Forbidden(msg) = nil, want an error")
	}
	if !errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
		t.Errorf("Forbidden(msg) = %v, want errors.Is ErrForbidden and not ErrNotFound", err)
	}
	if !errors.Is(fmt.Errorf("create project: %w", err), auth.ErrForbidden) {
		t.Errorf("wrapped Forbidden(msg) is not ErrForbidden")
	}
	if got := err.Error(); got != msg {
		t.Errorf("Forbidden(msg).Error() = %q, want %q", got, msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("Forbidden(msg) = %v (%T), want a doctype.HTTPError", err, err)
	}
	if he.Status() != http.StatusForbidden || he.FieldErrors() != nil {
		t.Errorf("Forbidden(msg) Status() = %d, FieldErrors() = %v; want 403 and nil", he.Status(), he.FieldErrors())
	}
	if other := auth.Forbidden("Another refusal."); other.Error() != "Another refusal." {
		t.Errorf("Forbidden(\"Another refusal.\").Error() = %q, want %q", other.Error(), "Another refusal.")
	}

	rec := httptest.NewRecorder()
	web.ErrorFrom(rec, httptest.NewRequest(http.MethodPost, "/projects", nil), fmt.Errorf("create: %w", err))
	if rec.Code != http.StatusForbidden {
		t.Errorf("web.ErrorFrom(Forbidden(msg)) status = %d, want 403", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, `<div role="alert">`+msg+`</div>`) {
		t.Errorf("web.ErrorFrom(Forbidden(msg)) body = %q, want the alert to hold exactly %q", body, msg)
	}
}

// CD-174: authfake.Restricted answers like the real service. Each row seeds
// the same restrictions on both.
func TestAuthfake_RestrictedMatchesReal(t *testing.T) {
	const (
		priya  = "priya@vardhan.test"  // restricted to PRJ-METRO in the World
		suresh = "suresh@vardhan.test" // restricted to PUNE-WH, METRO-SITE
		ravi   = "ravi@vardhan.test"   // unrestricted
		admin  = "admin@vardhan.test"
	)
	cases := []struct {
		name    string
		email   string
		dim     string
		want    bool
		wantErr bool
	}{
		{"project restriction, project", priya, string(auth.DimProject), true, false},
		{"project restriction, site", priya, string(auth.DimSite), false, false},
		{"site restriction, site", suresh, string(auth.DimSite), true, false},
		{"site restriction, project", suresh, string(auth.DimProject), false, false},
		{"unrestricted, project", ravi, string(auth.DimProject), false, false},
		{"unrestricted, site", ravi, string(auth.DimSite), false, false},
		{"Administrator restricted, project", admin, string(auth.DimProject), false, false},
		{"Administrator restricted, site", admin, string(auth.DimSite), false, false},
		{"unknown dimension, unrestricted", ravi, "region", false, true},
		{"unknown dimension, restricted", priya, "region", false, true},
		{"unknown dimension, Administrator", admin, "region", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := authtest.NewWorld(t)
			w.Restrict(t, admin, []string{"PRJ-METRO"}, []string{"PUNE-WH"})
			f := authfake.New()
			f.Restrict(w.Users[priya], []int64{w.Projects["PRJ-METRO"]}, nil)
			f.Restrict(w.Users[suresh], nil, []int64{w.Sites["PUNE-WH"], w.Sites["METRO-SITE"]})
			f.Restrict(w.Users[admin], []int64{w.Projects["PRJ-METRO"]}, []int64{w.Sites["PUNE-WH"]})
			p := w.P(t, c.email)

			for _, side := range []struct {
				name string
				svc  restrictedService
			}{{"real", w.Auth}, {"authfake", f}} {
				got, err := restrictedIn(t, w, side.svc, p, c.dim)
				what := fmt.Sprintf("%s Restricted(%s, %q)", side.name, c.email, c.dim)
				if c.wantErr {
					if got {
						t.Errorf("%s = true, want false with the error", what)
					}
					wantCallerBug(t, what, err, c.dim)
					continue
				}
				if err != nil || got != c.want {
					t.Errorf("%s = (%v, %v), want (%v, nil)", what, got, err, c.want)
				}
			}
		})
	}
}
