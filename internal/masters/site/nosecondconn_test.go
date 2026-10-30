package site_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213, CD-216(7): the project check on every site write reads in the write's
// tx, as Priya, who is restricted to PRJ-METRO and holds every right on Site.
// The Administrator bypass is TestSite_AdministratorPasses'.

// oneConn runs fn on the pool of one.
func (w projectWorld) oneConn(t *testing.T, fn func(ctx context.Context) error) error {
	t.Helper()
	return testkit.NoSecondConn(t, w.env.DB, fn)
}

// techSite is TECH-SITE, on a project Priya isn't assigned to, and its version.
func (w projectWorld) techSite(t *testing.T) (id int64, version int) {
	t.Helper()
	id = w.m.Sites["TECH-SITE"]
	return id, w.get(t, id).Version
}

func wantForbidden(t *testing.T, what string, err error, msg string) {
	t.Helper()
	if !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("%s: err = %v (%T), want auth.ErrForbidden", what, err, err)
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", what, err.Error(), msg)
	}
}

func TestSite_RestrictedWritesOnOneConnection(t *testing.T) {
	w := newProjectWorld(t)
	metro := w.m.Projects["PRJ-METRO"]

	var saved mstkit.Saved
	err := w.oneConn(t, func(ctx context.Context) (err error) {
		saved, err = w.svc.Create(ctx, w.env.Priya, w.input("METRO-2", "PUNE", "PRJ-METRO"))
		return err
	})
	if err != nil {
		t.Fatalf("Create on Priya's project: %v", err)
	}
	if got := w.get(t, saved.ID); got.ProjectID != metro || got.Version != 1 {
		t.Errorf("created site = %+v, want project %d at version 1", got, metro)
	}

	in := w.input("METRO-2", "PUNE", "PRJ-METRO")
	in.Name = "Renamed"
	err = w.oneConn(t, func(ctx context.Context) (err error) {
		saved, err = w.svc.Update(ctx, w.env.Priya, saved.ID, saved.Version, in)
		return err
	})
	if err != nil {
		t.Fatalf("Update on Priya's project: %v", err)
	}
	if got := w.get(t, saved.ID); got.Name != "Renamed" || got.Version != 2 {
		t.Errorf("updated site = %+v, want Renamed at version 2", got)
	}

	err = w.oneConn(t, func(ctx context.Context) error {
		return w.svc.SetDisabled(ctx, w.env.Priya, saved.ID, saved.Version, true)
	})
	if err != nil {
		t.Fatalf("SetDisabled on Priya's project: %v", err)
	}
	if got := w.get(t, saved.ID); !got.Disabled {
		t.Errorf("site = %+v, want disabled", got)
	}

	err = w.oneConn(t, func(ctx context.Context) error {
		return w.svc.Delete(ctx, w.env.Priya, saved.ID, saved.Version+1)
	})
	if err != nil {
		t.Fatalf("Delete on Priya's project: %v", err)
	}
}

func TestSite_RestrictedRefusalsOnOneConnection(t *testing.T) {
	w := newProjectWorld(t)
	id, version := w.techSite(t)
	onTech := w.input("TECH-SITE", "BLR", "PRJ-TECHPARK")

	err := w.oneConn(t, func(ctx context.Context) error {
		_, err := w.svc.Create(ctx, w.env.Priya, w.input("TECH-2", "BLR", "PRJ-TECHPARK"))
		return err
	})
	wantForbidden(t, "Create on another project", err, projectRefused)
	w.wantNoSite(t, "TECH-2")

	err = w.oneConn(t, func(ctx context.Context) error {
		_, err := w.svc.Update(ctx, w.env.Priya, id, version, onTech)
		return err
	})
	wantForbidden(t, "Update", err, readOnlyRefused)
	err = w.oneConn(t, func(ctx context.Context) error { return w.svc.SetDisabled(ctx, w.env.Priya, id, version, true) })
	wantForbidden(t, "SetDisabled", err, readOnlyRefused)
	err = w.oneConn(t, func(ctx context.Context) error { return w.svc.Delete(ctx, w.env.Priya, id, version) })
	wantForbidden(t, "Delete", err, readOnlyRefused)
	if got := w.get(t, id); got.Disabled || got.Version != version {
		t.Errorf("site = %+v after the refusals, want it unchanged at version %d", got, version)
	}
}

// CD-216(7): auth.Service answers the value check, so site keeps no read of
// AUTH's user_restrictions table.
func TestSite_NoOwnRestrictionQuery(t *testing.T) {
	const stopGap = "UserHasRestrictionValue"
	for _, pattern := range []string{"queries/*.sql", "dbq/*.go", "*.go"} {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		if len(paths) == 0 {
			t.Fatalf("no files match %s", pattern)
		}
		for _, path := range paths {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			body, err := os.ReadFile(path) //nolint:gosec // G304: this package's own files, from fixed globs
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), stopGap) {
				t.Errorf("%s still has %s", path, stopGap)
			}
		}
	}
}
