package docscreen

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// A viewer who can't read the DocType gets no recent picks and no error:
// the form still renders (fail closed).
func TestRecentPicks_NoReadIsNoPicks(t *testing.T) {
	authtest.RegisterDocTypes()
	w := authtest.NewWorld(t)
	w.AddUser(t, "rp-nobody@vardhan.test", "Nora Nobody")
	dt, _ := doctype.Lookup(authtest.DTOrder)
	site, _ := dt.Field("site_id")
	d := Deps{DB: w.DB, Auth: w.Auth, LinkRead: func(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error { return nil }}
	got, err := d.withRecent(dt, w.P(t, "rp-nobody@vardhan.test")).recentPicks(t.Context(), site)
	if err != nil || got != nil {
		t.Errorf("recentPicks = %v, %v; want nil, nil", got, err)
	}
}

var registerRecentDocLink sync.Once

// A header link to a document (no Master) gets no recent picks: they are
// masters only, and LinkRead is never asked.
func TestRecentPicks_DocumentLinkIsNoPicks(t *testing.T) {
	authtest.RegisterDocTypes()
	const name = "docscreen.RecentDocLink"
	registerRecentDocLink.Do(func() {
		base, _ := doctype.Lookup(authtest.DTOrder)
		dt := *base
		dt.Name = name
		dt.Fields = append(append([]doctype.Field(nil), base.Fields...), doctype.Field{Name: "source_id", Label: "Source", Column: "source_id", Type: doctype.Link, Link: authtest.DTOrder})
		doctype.Register(dt)
	})
	w := authtest.NewWorld(t)
	suresh := w.P(t, "suresh@vardhan.test")
	if err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `INSERT INTO probe_order (state, created_by, created_at, site_id) VALUES ('draft', ?, '2026-10-20T10:00:00Z', ?)`, int64(suresh.ID), w.Sites["PUNE-WH"])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	dt, _ := doctype.Lookup(name)
	f, _ := dt.Field("source_id")
	asked := false
	d := Deps{DB: w.DB, Auth: w.Auth, LinkRead: func(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error {
		asked = true
		return nil
	}}
	got, err := d.withRecent(dt, suresh).recentPicks(t.Context(), f)
	if err != nil || got != nil || asked {
		t.Errorf("recentPicks = %v, %v (LinkRead asked %v); want nil, nil, not asked", got, err, asked)
	}
}

// A stored value that isn't an id fails the read: it is an error, never
// "no recent picks".
func TestRecentPicks_BadStoredValueIsAnError(t *testing.T) {
	authtest.RegisterDocTypes()
	w := authtest.NewWorld(t)
	suresh := w.P(t, "suresh@vardhan.test")
	if err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `INSERT INTO probe_order (state, created_by, created_at, site_id) VALUES ('draft', ?, '2026-10-20T10:00:00Z', 'PUNE')`, int64(suresh.ID))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	w.Restrict(t, "suresh@vardhan.test", nil, nil) // the text site would fail the scope otherwise
	dt, _ := doctype.Lookup(authtest.DTOrder)
	site, _ := dt.Field("site_id")
	d := Deps{DB: w.DB, Auth: w.Auth, LinkRead: func(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error { return nil }}
	if got, err := d.withRecent(dt, suresh).recentPicks(t.Context(), site); err == nil {
		t.Errorf("recentPicks = %v, nil; want the scan error", got)
	}
}

var registerRecentRole sync.Once

// recentOrders makes an authtest world where Suresh (no restrictions) owns
// one order per site code, the last one newest, and returns Suresh.
func recentOrders(t *testing.T, w *authtest.World, sites ...string) auth.Principal {
	t.Helper()
	w.Restrict(t, "suresh@vardhan.test", nil, nil)
	suresh := w.P(t, "suresh@vardhan.test")
	if err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		for i, code := range sites {
			id, ok := w.Sites[code]
			if !ok {
				if err := tx.SQL().QueryRowContext(t.Context(), `INSERT INTO probe_site (code, name) VALUES (?, ?) RETURNING id`, code, code).Scan(&id); err != nil {
					return err
				}
				w.Sites[code] = id
			}
			if _, err := tx.SQL().ExecContext(t.Context(), `INSERT INTO probe_order (state, created_by, created_at, site_id) VALUES ('draft', ?, ?, ?)`,
				int64(suresh.ID), fmt.Sprintf("2026-10-%02dT10:00:00Z", 10+i), id); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return suresh
}

func readAll(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error { return nil }

// The read stops at components.MaxRecent kept picks: six readable sites on
// six orders give the five newest.
func TestRecentPicks_StopsAtMaxRecent(t *testing.T) {
	authtest.RegisterDocTypes()
	w := authtest.NewWorld(t)
	suresh := recentOrders(t, w, "S-1", "S-2", "S-3", "S-4", "S-5", "S-6")
	dt, _ := doctype.Lookup(authtest.DTOrder)
	site, _ := dt.Field("site_id")
	got, err := Deps{DB: w.DB, Auth: w.Auth, LinkRead: readAll}.withRecent(dt, suresh).recentPicks(t.Context(), site)
	var codes []string
	for _, r := range got {
		codes = append(codes, r.Code)
	}
	if err != nil || strings.Join(codes, ",") != "S-6,S-5,S-4,S-3,S-2" {
		t.Errorf("recentPicks = %q, %v; want the 5 newest", codes, err)
	}
}

// A field with a LinkRole keeps only picks LinkRole says hold the role
// (CD-218); with no LinkRole hook it offers none, and the hook's error is the
// read's.
func TestRecentPicks_LinkRole(t *testing.T) {
	authtest.RegisterDocTypes()
	const name = "docscreen.RecentRole"
	registerRecentRole.Do(func() {
		base, _ := doctype.Lookup(authtest.DTOrder)
		dt := *base
		dt.Name = name
		dt.Fields = append([]doctype.Field(nil), base.Fields...)
		for i := range dt.Fields {
			if dt.Fields[i].Name == "site_id" {
				dt.Fields[i].LinkRole = "depot"
			}
		}
		doctype.Register(dt)
	})
	w := authtest.NewWorld(t)
	suresh := recentOrders(t, w, "PUNE-WH", "METRO-SITE")
	dt, _ := doctype.Lookup(name)
	site, _ := dt.Field("site_id")
	broken := errors.New("roles on fire")
	for _, c := range []struct {
		name string
		role func(context.Context, *db.Tx, *doctype.DocType, int64, string) (bool, error)
		want string
		err  error
	}{
		{"no hook", nil, "", nil},
		{"only PUNE-WH holds it", func(_ context.Context, _ *db.Tx, target *doctype.DocType, id int64, role string) (bool, error) {
			return target.Name == authtest.DTSite && role == "depot" && id == w.Sites["PUNE-WH"], nil
		}, "PUNE-WH", nil},
		{"hook fails", func(context.Context, *db.Tx, *doctype.DocType, int64, string) (bool, error) { return false, broken }, "", broken},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := Deps{DB: w.DB, Auth: w.Auth, LinkRead: readAll, LinkRole: c.role}.withRecent(dt, suresh).recentPicks(t.Context(), site)
			var codes []string
			for _, r := range got {
				codes = append(codes, r.Code)
			}
			if strings.Join(codes, ",") != c.want || !errors.Is(err, c.err) {
				t.Errorf("recentPicks = %q, %v; want %q, %v", codes, err, c.want, c.err)
			}
		})
	}
}
