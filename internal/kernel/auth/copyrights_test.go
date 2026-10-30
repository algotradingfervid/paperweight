package auth_test

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Copy rights from another role: design-system §8.6, wp/polish plan P1,
// CD-200. Refusal texts are the plan's, copied here on purpose: messages are
// tested by exact text, never through the catalogue's own constants.
const (
	p1Source = "Copy Source"
	p1Target = "Copy Target"

	p1Admin = "admin@vardhan.test"
	p1Priya = "priya@vardhan.test"

	p1MsgAdminOnly  = "Only an administrator can change roles."
	p1MsgFixed      = "The Administrator role can't be changed."
	p1MsgChanged    = "Someone else changed Copy Target after you opened it. Reload it and make your changes again."
	p1MsgNoSource   = "Role not found. Pick one from the list."
	p1MsgSameRole   = "Pick a different role to copy from."
	p1MsgFromAdmin  = "Administrator has every right that applies and can't be copied."
	p1MsgNotApplies = "Submit does not apply to Item. Clear it."

	// p1NoRole is a role id no fixture reaches.
	p1NoRole = int64(999999)

	// p1Hold is how long a blocked copy gets to run its reads before the
	// competing write commits. A correct copy passes whatever this is; a copy
	// that reads before db.Write is only caught when it has run by then.
	p1Hold = 200 * time.Millisecond
)

// p1Fixture is the World with two roles whose rights differ on every kind of
// DocType the copy has to handle:
//
//	Order    both have rights, different: replaced
//	Receipt  only the target has rights: cleared
//	Claim    only the source has rights, target has no row: added, team kept
//	Item     both have the same rights: untouched
//	Party    neither has anything: untouched
//
// customised starts at 0 on all the target's rows, so a mark is the copy's.
type p1Fixture struct {
	w                  *authtest.World
	admin              auth.Principal
	sourceID, targetID int64
	version            int64
}

func p1Rights(scope auth.Scope, rights ...auth.Right) map[auth.Right]auth.Scope {
	out := make(map[auth.Right]auth.Scope, len(rights))
	for _, r := range rights {
		out[r] = scope
	}
	return out
}

func p1New(t *testing.T) p1Fixture {
	t.Helper()
	w := authtest.NewWorld(t)
	same := p1Rights(auth.ScopeAll, auth.RightRead, auth.RightExport)
	w.SetRights(t, p1Source, authtest.DTOrder, p1Rights(auth.ScopeAll, auth.RightRead, auth.RightPrint))
	w.SetRights(t, p1Source, authtest.DTClaim, p1Rights(auth.ScopeTeam, auth.RightRead, auth.RightApprove))
	w.SetRights(t, p1Source, authtest.DTItem, same)
	w.SetRights(t, p1Target, authtest.DTOrder, p1Rights(auth.ScopeOwn, auth.RightRead, auth.RightCreate, auth.RightWrite))
	w.SetRights(t, p1Target, authtest.DTReceipt, p1Rights(auth.ScopeAll, auth.RightRead, auth.RightPrint))
	w.SetRights(t, p1Target, authtest.DTItem, same)
	target := t3RoleID(t, w.DB, p1Target)
	t7Exec(t, w, `UPDATE role_doctypes SET customised = 0 WHERE role_id = ?`, target)
	return p1Fixture{
		w: w, admin: w.P(t, p1Admin),
		sourceID: t3RoleID(t, w.DB, p1Source), targetID: target,
		version: p1RoleVersion(t, w, p1Target),
	}
}

func (f p1Fixture) doCopy(t *testing.T) {
	t.Helper()
	if err := f.w.Auth.CopyRightsID(context.Background(), f.admin, f.targetID, f.sourceID, f.version); err != nil {
		t.Fatalf("CopyRightsID(target, source, current version) = %v, want nil", err)
	}
}

func p1RoleVersion(t *testing.T, w *authtest.World, role string) int64 {
	t.Helper()
	return int64(t7Count(t, w, `SELECT version FROM roles WHERE name = ?`, role))
}

// p1Snapshot is every row a copy may touch, as text.
func p1Snapshot(t *testing.T, w *authtest.World) []string {
	t.Helper()
	var out []string
	out = append(out, t7Strings(t, w, `SELECT 'rr|' || role_id || '|' || doctype || '|' || right_name || '|' || scope
		FROM role_rights ORDER BY role_id, doctype, right_name`)...)
	out = append(out, t7Strings(t, w, `SELECT 'rd|' || role_id || '|' || doctype || '|' || customised
		FROM role_doctypes ORDER BY role_id, doctype`)...)
	out = append(out, t7Strings(t, w, `SELECT 'r|' || id || '|' || name || '|' || version || '|' || updated_at
		FROM roles ORDER BY id`)...)
	out = append(out, t7Strings(t, w, `SELECT 'pv|' || perm_version FROM auth_meta`)...)
	return out
}

// p1WantRefused checks err's exact text and status; status 0 accepts any 4xx,
// for the refusals whose status the plan leaves open.
func p1WantRefused(t *testing.T, what string, err error, msg string, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: err = nil, want %q", what, msg)
	}
	if err.Error() != msg {
		t.Errorf("%s: err = %q, want %q", what, err.Error(), msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("%s: err %v is not a doctype.HTTPError", what, err)
	}
	if got := he.Status(); status == 0 && (got < 400 || got > 499) || status != 0 && got != status {
		t.Errorf("%s: status = %d, want %d (0 = any 4xx)", what, got, status)
	}
}

// p1WantNoWrite checks the database and the audit log are as before.
func p1WantNoWrite(t *testing.T, what string, w *authtest.World, before []string) {
	t.Helper()
	if after := p1Snapshot(t, w); !slices.Equal(after, before) {
		t.Errorf("%s: database changed:\n got %q\nwant %q", what, after, before)
	}
	if n := len(w.Audit.Entries()); n != 0 {
		t.Errorf("%s: audit entries = %d (%+v), want 0", what, n, w.Audit.Entries())
	}
}

func TestCopyRights_NonAdminRefusedBeforeAnyRead(t *testing.T) {
	f := p1New(t)
	ctx := context.Background()
	priya := f.w.P(t, p1Priya)
	before := p1Snapshot(t, f.w)

	err := f.w.Auth.CopyRightsID(ctx, priya, f.targetID, f.sourceID, f.version)
	p1WantRefused(t, "non-admin", err, p1MsgAdminOnly, 403)
	if !errors.Is(err, auth.ErrAdminOnlyRoles) {
		t.Errorf("non-admin: err = %v, want ErrAdminOnlyRoles", err)
	}

	// Before any read: a target that doesn't exist is the same refusal, not a
	// 404 that tells a non-admin which role ids exist.
	err = f.w.Auth.CopyRightsID(ctx, priya, p1NoRole, f.sourceID, f.version)
	p1WantRefused(t, "non-admin, no such target", err, p1MsgAdminOnly, 403)

	p1WantNoWrite(t, "non-admin", f.w, before)
}

func TestCopyRights_StaleVersionRefused(t *testing.T) {
	f := p1New(t)
	ctx := context.Background()
	before := p1Snapshot(t, f.w)

	err := f.w.Auth.CopyRightsID(ctx, f.admin, f.targetID, f.sourceID, f.version-1)
	p1WantRefused(t, "stale version", err, p1MsgChanged, 409)

	// The target is loaded first: a stale page hears that it is stale, even
	// when its source would be refused too.
	err = f.w.Auth.CopyRightsID(ctx, f.admin, f.targetID, f.targetID, f.version-1)
	p1WantRefused(t, "stale version, source == target", err, p1MsgChanged, 409)

	p1WantNoWrite(t, "stale version", f.w, before)
}

func TestCopyRights_MissingTargetNotFound(t *testing.T) {
	f := p1New(t)
	before := p1Snapshot(t, f.w)

	err := f.w.Auth.CopyRightsID(context.Background(), f.admin, p1NoRole, f.sourceID, 1)
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("missing target: err = %v, want ErrNotFound", err)
	}
	p1WantRefused(t, "missing target", err, auth.MsgNotFound, 404)
	p1WantNoWrite(t, "missing target", f.w, before)
}

func TestCopyRights_AdministratorTargetRefused(t *testing.T) {
	f := p1New(t)
	ctx := context.Background()
	admin := t3RoleID(t, f.w.DB, auth.RoleAdministrator)
	v := p1RoleVersion(t, f.w, auth.RoleAdministrator)
	before := p1Snapshot(t, f.w)

	err := f.w.Auth.CopyRightsID(ctx, f.admin, admin, f.sourceID, v)
	p1WantRefused(t, "Administrator target", err, p1MsgFixed, 400)

	// Checked before the source is loaded.
	err = f.w.Auth.CopyRightsID(ctx, f.admin, admin, p1NoRole, v)
	p1WantRefused(t, "Administrator target, no such source", err, p1MsgFixed, 400)

	p1WantNoWrite(t, "Administrator target", f.w, before)
}

func TestCopyRights_SourceRefusals(t *testing.T) {
	f := p1New(t)
	before := p1Snapshot(t, f.w)
	for _, tc := range []struct {
		name   string
		source int64
		msg    string
	}{
		{"source missing", p1NoRole, p1MsgNoSource},
		{"source is the target", f.targetID, p1MsgSameRole},
		{"source is Administrator", t3RoleID(t, f.w.DB, auth.RoleAdministrator), p1MsgFromAdmin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := f.w.Auth.CopyRightsID(context.Background(), f.admin, f.targetID, tc.source, f.version)
			p1WantRefused(t, tc.name, err, tc.msg, 0)
			p1WantNoWrite(t, tc.name, f.w, before)
		})
	}
}

func TestCopyRights_ReplacesAndClearsEveryDocType(t *testing.T) {
	f := p1New(t)
	sourceBefore := map[string]map[string]string{}
	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt, authtest.DTClaim, authtest.DTItem} {
		sourceBefore[dt] = t3Rows(t, f.w.DB, p1Source, dt)
	}

	f.doCopy(t)

	for _, tc := range []struct {
		dt   string
		want map[string]string
	}{
		{authtest.DTOrder, map[string]string{"read": "all", "print": "all"}},     // replaced, nothing of the old row kept
		{authtest.DTReceipt, map[string]string{}},                                // the source has none: cleared
		{authtest.DTClaim, map[string]string{"read": "team", "approve": "team"}}, // added, with its scope
		{authtest.DTItem, map[string]string{"read": "all", "export": "all"}},     // same on both
		{authtest.DTParty, map[string]string{}},                                  // none on either
	} {
		if got := t3Rows(t, f.w.DB, p1Target, tc.dt); !maps.Equal(got, tc.want) {
			t.Errorf("role_rights(target, %s) = %v, want %v", tc.dt, got, tc.want)
		}
	}
	for dt, want := range sourceBefore {
		if got := t3Rows(t, f.w.DB, p1Source, dt); !maps.Equal(got, want) {
			t.Errorf("role_rights(source, %s) = %v, want unchanged %v", dt, got, want)
		}
	}
}

func TestCopyRights_MarksOnlyChangedDocTypesCustomised(t *testing.T) {
	f := p1New(t)
	f.doCopy(t)

	for _, dt := range []string{authtest.DTOrder, authtest.DTReceipt, authtest.DTClaim} {
		if c, ok := t3RoleDocType(t, f.w.DB, p1Target, dt); !ok || c != 1 {
			t.Errorf("role_doctypes(target, %s) = (customised %d, exists %v), want (1, true)", dt, c, ok)
		}
	}
	if c, ok := t3RoleDocType(t, f.w.DB, p1Target, authtest.DTItem); !ok || c != 0 {
		t.Errorf("role_doctypes(target, Item, unchanged) = (customised %d, exists %v), want (0, true)", c, ok)
	}
	if c, ok := t3RoleDocType(t, f.w.DB, p1Target, authtest.DTParty); ok {
		t.Errorf("role_doctypes(target, Party, unchanged) = (customised %d, exists), want no row", c)
	}
}

func TestCopyRights_BumpsVersionsOnce(t *testing.T) {
	f := p1New(t)
	perm := t3PermVersion(t, f.w.DB)
	source := p1RoleVersion(t, f.w, p1Source)

	f.doCopy(t) // three DocTypes change, and it is still one bump of each

	if got, want := p1RoleVersion(t, f.w, p1Target), f.version+1; got != want {
		t.Errorf("target version = %d, want %d", got, want)
	}
	if got, want := t3PermVersion(t, f.w.DB), perm+1; got != want {
		t.Errorf("perm_version = %d, want %d", got, want)
	}
	if got := p1RoleVersion(t, f.w, p1Source); got != source {
		t.Errorf("source version = %d, want unchanged %d", got, source)
	}
}

func TestCopyRights_PermVersionReachesCan(t *testing.T) {
	f := p1New(t)
	ctx := context.Background()
	f.w.AddUser(t, "copy.user@vardhan.test", "Copy User", p1Target)
	p := f.w.P(t, "copy.user@vardhan.test")

	// These load the matrix into the cache the copy has to invalidate.
	if err := f.w.Auth.Can(ctx, p, authtest.DTReceipt, auth.RightRead, nil); err != nil {
		t.Fatalf("Can(target role, Receipt, read) before = %v, want nil", err)
	}
	if err := f.w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightPrint, nil); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("Can(target role, Order, print) before = %v, want ErrForbidden", err)
	}

	f.doCopy(t)

	if err := f.w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightPrint, nil); err != nil {
		t.Errorf("Can(target role, Order, print) after = %v, want nil", err)
	}
	if err := f.w.Auth.Can(ctx, p, authtest.DTReceipt, auth.RightRead, nil); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Can(target role, Receipt, read) after = %v, want ErrForbidden", err)
	}
	if err := f.w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightCreate, nil); !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Can(target role, Order, create) after = %v, want ErrForbidden", err)
	}
}

func TestCopyRights_OneAuditEntryForEveryChangedDocType(t *testing.T) {
	f := p1New(t)
	f.doCopy(t)

	es := f.w.Audit.Entries()
	if len(es) != 1 {
		t.Fatalf("audit entries = %d (%+v), want 1", len(es), es)
	}
	got := es[0]
	if !got.At.Equal(f.w.Clock.Now()) {
		t.Errorf("audit At = %v, want %v", got.At, f.w.Clock.Now())
	}
	got.At = f.w.Clock.Now()
	// The plan fixes which changes, not their order across DocTypes.
	got.Changes = slices.Clone(got.Changes)
	slices.SortFunc(got.Changes, func(a, b audit.Change) int { return strings.Compare(a.Field, b.Field) })
	want := audit.Entry{
		DocType: "Role", DocID: f.targetID, Action: "rights", By: f.admin.ID, At: f.w.Clock.Now(),
		Changes: []audit.Change{
			{Field: authtest.DTClaim + ".approve", Before: "none", After: "team", Op: "set"},
			{Field: authtest.DTClaim + ".read", Before: "none", After: "team", Op: "set"},
			{Field: authtest.DTOrder + ".create", Before: "own", After: "none", Op: "set"},
			{Field: authtest.DTOrder + ".print", Before: "none", After: "all", Op: "set"},
			{Field: authtest.DTOrder + ".read", Before: "own", After: "all", Op: "set"},
			{Field: authtest.DTOrder + ".write", Before: "own", After: "none", Op: "set"},
			{Field: authtest.DTReceipt + ".print", Before: "all", After: "none", Op: "set"},
			{Field: authtest.DTReceipt + ".read", Before: "all", After: "none", Op: "set"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audit entry =\n %+v\nwant\n %+v", got, want)
	}
}

func TestCopyRights_NoChangeWritesNothing(t *testing.T) {
	w := authtest.NewWorld(t)
	ctx := context.Background()
	for _, role := range []string{p1Source, p1Target} {
		w.SetRights(t, role, authtest.DTOrder, p1Rights(auth.ScopeAll, auth.RightRead, auth.RightPrint))
		w.SetRights(t, role, authtest.DTClaim, p1Rights(auth.ScopeTeam, auth.RightRead))
	}
	t7Exec(t, w, `UPDATE role_doctypes SET customised = 0`)
	target, source := t3RoleID(t, w.DB, p1Target), t3RoleID(t, w.DB, p1Source)
	version := p1RoleVersion(t, w, p1Target)
	before := p1Snapshot(t, w)

	if err := w.Auth.CopyRightsID(ctx, w.P(t, p1Admin), target, source, version); err != nil {
		t.Fatalf("CopyRightsID(same rights) = %v, want nil", err)
	}
	p1WantNoWrite(t, "copy with nothing to change", w, before)

	// Nothing to change is no way round the version check.
	err := w.Auth.CopyRightsID(ctx, w.P(t, p1Admin), target, source, version-1)
	p1WantRefused(t, "stale copy with nothing to change", err, p1MsgChanged, 409)
}

// A row the source should not hold (a right that stopped applying) is refused
// by ValidateRow, and the DocTypes copied before it roll back. DocTypes run in
// name order, so Claim (changed, valid) comes before Item (invalid).
func TestCopyRights_InvalidSourceRowRefusedAndRolledBack(t *testing.T) {
	f := p1New(t)
	t7Exec(t, f.w, `INSERT INTO role_rights (role_id, doctype, right_name, scope) VALUES (?, ?, 'submit', 'all')`,
		f.sourceID, authtest.DTItem)
	before := p1Snapshot(t, f.w)

	err := f.w.Auth.CopyRightsID(context.Background(), f.admin, f.targetID, f.sourceID, f.version)
	p1WantRefused(t, "source row with an inapplicable right", err, p1MsgNotApplies, 400)
	p1WantNoWrite(t, "source row with an inapplicable right", f.w, before)
}

// p1DuringCopy starts CopyRightsID while a competing write holds the write
// lock with change uncommitted, lets the copy run up to that lock, and only
// then commits. A copy that reads anything before db.Write sees the database
// from before change; one that reads inside db.Write sees it after (CD-94).
func p1DuringCopy(t *testing.T, f p1Fixture, change func(ctx context.Context, q db.DBTX) error) error {
	t.Helper()
	ctx := context.Background()
	held, release := make(chan struct{}), make(chan struct{})
	changed := make(chan error, 1)
	go func() {
		changed <- f.w.DB.Write(ctx, func(tx *db.Tx) error {
			err := change(ctx, tx.SQL())
			close(held)
			<-release
			return err
		})
	}()
	select {
	case <-held:
	case err := <-changed:
		t.Fatalf("competing write never took the lock: %v", err)
	}
	copied := make(chan error, 1)
	go func() {
		copied <- f.w.Auth.CopyRightsID(ctx, f.admin, f.targetID, f.sourceID, f.version)
	}()
	time.Sleep(p1Hold)
	close(release)
	if err := <-changed; err != nil {
		t.Fatalf("competing write: %v", err)
	}
	return <-copied
}

func TestCopyRights_ReadsInsideTheWrite(t *testing.T) {
	t.Run("source rights saved while the copy waits are the ones copied", func(t *testing.T) {
		f := p1New(t)
		err := p1DuringCopy(t, f, func(ctx context.Context, q db.DBTX) error {
			if _, err := q.ExecContext(ctx, `INSERT INTO role_rights (role_id, doctype, right_name, scope) VALUES (?, ?, 'export', 'own')`,
				f.sourceID, authtest.DTOrder); err != nil {
				return err
			}
			// The source's version is not posted, so this refuses nothing.
			_, err := q.ExecContext(ctx, `UPDATE roles SET version = version + 1 WHERE id = ?`, f.sourceID)
			return err
		})
		if err != nil {
			t.Fatalf("CopyRightsID = %v, want nil", err)
		}
		want := map[string]string{"read": "all", "print": "all", "export": "own"}
		if got := t3Rows(t, f.w.DB, p1Target, authtest.DTOrder); !maps.Equal(got, want) {
			t.Errorf("role_rights(target, Order) = %v, want the committed source row %v", got, want)
		}
	})

	t.Run("source deleted while the copy waits is refused", func(t *testing.T) {
		f := p1New(t)
		order := t3Rows(t, f.w.DB, p1Target, authtest.DTOrder)
		err := p1DuringCopy(t, f, func(ctx context.Context, q db.DBTX) error {
			_, err := q.ExecContext(ctx, `DELETE FROM roles WHERE id = ?`, f.sourceID)
			return err
		})
		p1WantRefused(t, "source deleted", err, p1MsgNoSource, 0)
		if got := t3Rows(t, f.w.DB, p1Target, authtest.DTOrder); !maps.Equal(got, order) {
			t.Errorf("role_rights(target, Order) = %v, want unchanged %v", got, order)
		}
		if n := len(f.w.Audit.Entries()); n != 0 {
			t.Errorf("audit entries = %d, want 0", n)
		}
	})

	t.Run("target saved while the copy waits is stale", func(t *testing.T) {
		f := p1New(t)
		order := t3Rows(t, f.w.DB, p1Target, authtest.DTOrder)
		err := p1DuringCopy(t, f, func(ctx context.Context, q db.DBTX) error {
			_, err := q.ExecContext(ctx, `UPDATE roles SET version = version + 1 WHERE id = ?`, f.targetID)
			return err
		})
		p1WantRefused(t, "target saved", err, p1MsgChanged, 409)
		if got := t3Rows(t, f.w.DB, p1Target, authtest.DTOrder); !maps.Equal(got, order) {
			t.Errorf("role_rights(target, Order) = %v, want unchanged %v", got, order)
		}
		if n := len(f.w.Audit.Entries()); n != 0 {
			t.Errorf("audit entries = %d, want 0", n)
		}
	})
}
