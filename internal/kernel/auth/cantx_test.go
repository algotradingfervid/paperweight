package auth_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213: CanTx and its siblings read in the caller's transaction.

const (
	ctxUser      = "cantx@vardhan.test"
	ctxRole      = "Stores" // read on the probe Purchase Order, nothing else
	ctxNoWrite   = "You don't have permission to change Purchase Order. Ask your administrator for a role that allows it."
	ctxOwnChange = "You can only change Expense Claims you created."
)

var errCtxRollback = errors.New("cantx test: roll back")

// ctxWorld is a World with an unrestricted user holding only ctxRole.
func ctxWorld(t *testing.T) (*authtest.World, auth.Principal) {
	t.Helper()
	w := authtest.NewWorld(t)
	w.AddUser(t, ctxUser, "CanTx User", ctxRole)
	return w, w.P(t, ctxUser)
}

// ctxGrant adds r to role's row on doctype and bumps perm_version inside tx.
func ctxGrant(ctx context.Context, tx *db.Tx, role, doctype string, r auth.Right) error {
	res, err := tx.SQL().ExecContext(ctx,
		`INSERT INTO role_rights (role_id, doctype, right_name, scope) SELECT id, ?, ?, 'all' FROM roles WHERE name_key = lower(?)`,
		doctype, string(r), role)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n != 1 {
		return errors.Join(err, errors.New("cantx test: no role "+role))
	}
	_, err = tx.SQL().ExecContext(ctx, `UPDATE auth_meta SET perm_version = perm_version + 1 WHERE id = 1`)
	return err
}

// ctxVersion reads perm_version in tx.
func ctxVersion(ctx context.Context, tx *db.Tx) (int64, error) {
	var v int64
	err := tx.SQL().QueryRowContext(ctx, `SELECT perm_version FROM auth_meta WHERE id = 1`).Scan(&v)
	return v, err
}

// ctxPO is a probe Purchase Order the Administrator created on projects.
func ctxPO(id int64, projects ...int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: authtest.DTOrder, ID: id, CreatedBy: 1, ProjectIDs: projects}
}

// ctxOneConn runs fn inside a write that commits, on the pool of one.
func ctxOneConn(t *testing.T, w *authtest.World, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
		return w.DB.Write(ctx, func(tx *db.Tx) error { return fn(ctx, tx) })
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// A grant made in the caller's write is what CanTx answers from; Can, on its
// own connection, can't see it, and after the rollback nobody can.
func TestCanTx_ReadsGrantInCallersTx(t *testing.T) {
	if db.NestCheck {
		t.Skip("calls plain Can inside a Write on purpose; the nestcheck gate would end the run")
	}
	w, p := ctxWorld(t)
	ctx := t.Context()
	rec := ctxPO(1)
	t5WantDenied(t, "Can before the grant", w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightWrite, rec), auth.RightWrite, false, false, ctxNoWrite)

	var withRec, withNil, onCan error
	err := w.DB.Write(ctx, func(tx *db.Tx) error {
		if err := ctxGrant(ctx, tx, ctxRole, authtest.DTOrder, auth.RightWrite); err != nil {
			return err
		}
		withRec = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, auth.RightWrite, rec)
		withNil = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, auth.RightWrite, nil)
		onCan = w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightWrite, rec)
		return errCtxRollback
	})
	if !errors.Is(err, errCtxRollback) {
		t.Fatalf("Write: %v, want the rollback", err)
	}
	if withRec != nil {
		t.Errorf("CanTx(record) in the granting tx = %v, want nil", withRec)
	}
	if withNil != nil {
		t.Errorf("CanTx(nil) in the granting tx = %v, want nil", withNil)
	}
	t5WantDenied(t, "Can during the grant", onCan, auth.RightWrite, false, false, ctxNoWrite)
	t5WantDenied(t, "Can after the rollback", w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightWrite, rec), auth.RightWrite, false, false, ctxNoWrite)
}

// A restriction added in the caller's write hides the record from CanTx; Can,
// on its own connection, can't see it.
func TestCanTx_ReadsRestrictionInCallersTx(t *testing.T) {
	if db.NestCheck {
		t.Skip("calls plain Can inside a Write on purpose; the nestcheck gate would end the run")
	}
	w, p := ctxWorld(t)
	ctx := t.Context()
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]

	var inside, outside, onCan error
	err := w.DB.Write(ctx, func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO user_restrictions (user_id, dimension, value_id) VALUES (?, ?, ?)`, int64(p.ID), string(auth.DimProject), metro); err != nil {
			return err
		}
		inside = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, auth.RightRead, ctxPO(1, metro))
		outside = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, auth.RightRead, ctxPO(2, techpark))
		onCan = w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightRead, ctxPO(2, techpark))
		return errCtxRollback
	})
	if !errors.Is(err, errCtxRollback) {
		t.Fatalf("Write: %v, want the rollback", err)
	}
	if inside != nil {
		t.Errorf("CanTx(PRJ-METRO) = %v, want nil", inside)
	}
	t5WantNotFound(t, "CanTx(PRJ-TECHPARK) in the restricting tx", outside)
	if onCan != nil {
		t.Errorf("Can(PRJ-TECHPARK) during the restriction = %v, want nil: it can't see the uncommitted row", onCan)
	}
}

// A rolled-back write can't poison the matrix cache: its perm_version is reused
// by the next committed change, whose grant is the one Can must see. No Can
// runs between the two writes: it would reload at the old version and hide the
// bad entry.
func TestCanTx_RollbackCantPoisonMatrixCache(t *testing.T) {
	w, p := ctxWorld(t)
	ctx := t.Context()
	t5WantDenied(t, "Can before", w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightWrite, nil), auth.RightWrite, false, false, ctxNoWrite)

	var rolledBack, committed int64
	var inTx error
	err := w.DB.Write(ctx, func(tx *db.Tx) (err error) {
		if err = ctxGrant(ctx, tx, ctxRole, authtest.DTOrder, auth.RightWrite); err != nil {
			return err
		}
		if rolledBack, err = ctxVersion(ctx, tx); err != nil {
			return err
		}
		inTx = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, auth.RightWrite, nil)
		return errCtxRollback
	})
	if !errors.Is(err, errCtxRollback) {
		t.Fatalf("rolled-back Write: %v", err)
	}
	if inTx != nil {
		t.Fatalf("CanTx in the rolled-back tx = %v, want nil (it must have read the matrix there)", inTx)
	}

	if err := w.DB.Write(ctx, func(tx *db.Tx) (err error) {
		if err = ctxGrant(ctx, tx, ctxRole, authtest.DTOrder, auth.RightSubmit); err != nil {
			return err
		}
		committed, err = ctxVersion(ctx, tx)
		return err
	}); err != nil {
		t.Fatalf("committed Write: %v", err)
	}
	if committed != rolledBack {
		t.Fatalf("perm_version committed %d, rolled back %d: the two writes must land on the same version", committed, rolledBack)
	}

	if err := w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightSubmit, nil); err != nil {
		t.Errorf("Can(submit), the committed grant = %v, want nil", err)
	}
	t5WantDenied(t, "Can(write), the rolled-back grant", w.Auth.Can(ctx, p, authtest.DTOrder, auth.RightWrite, nil), auth.RightWrite, false, false, ctxNoWrite)
}

// An Administrator's Can needs no connection at all, on a record or without one.
func TestCan_AdministratorInsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	admin := w.P(t, "admin@vardhan.test")
	var withNil, withRec error
	ctxOneConn(t, w, func(ctx context.Context, _ *db.Tx) error {
		withNil = w.Auth.Can(ctx, admin, authtest.DTOrder, auth.RightApprove, nil)
		withRec = w.Auth.Can(ctx, admin, authtest.DTOrder, auth.RightApprove, ctxPO(1, w.Projects["PRJ-METRO"]))
		return nil
	})
	if withNil != nil || withRec != nil {
		t.Errorf("Can = (%v, %v), want (nil, nil)", withNil, withRec)
	}
}

// ctxClaim is a probe Expense Claim of Deepak's on project.
func ctxClaim(w *authtest.World, t *testing.T, id, project int64) *auth.RecordRef {
	t.Helper()
	return &auth.RecordRef{
		DocType: authtest.DTClaim, ID: id, CreatedBy: w.P(t, "deepak@vardhan.test").ID,
		EmployeeID: w.Employees["EMP-007"], ProjectIDs: []int64{project},
	}
}

// CanTx on a non-Administrator: the employee path (Priya manages Deepak) and
// the restrictions (Priya is on PRJ-METRO) both read in the tx.
func TestCanTx_NonAdminInsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	priya := w.P(t, "priya@vardhan.test")
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	inProject, outProject := ctxClaim(w, t, 1, metro), ctxClaim(w, t, 2, techpark)
	var team, ownOnly, hidden, nilRec error
	ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) error {
		team = w.Auth.CanTx(ctx, tx, priya, authtest.DTClaim, auth.RightRead, inProject)
		ownOnly = w.Auth.CanTx(ctx, tx, priya, authtest.DTClaim, auth.RightWrite, inProject)
		hidden = w.Auth.CanTx(ctx, tx, priya, authtest.DTClaim, auth.RightRead, outProject)
		nilRec = w.Auth.CanTx(ctx, tx, priya, authtest.DTOrder, auth.RightCreate, nil)
		return nil
	})
	if team != nil {
		t.Errorf("CanTx(read, report's claim) = %v, want nil", team)
	}
	t5WantDenied(t, "CanTx(write, report's claim)", ownOnly, auth.RightWrite, true, false, ctxOwnChange)
	t5WantNotFound(t, "CanTx(read, claim outside the project)", hidden)
	if nilRec != nil {
		t.Errorf("CanTx(create, nil) = %v, want nil", nilRec)
	}
}

// CheckValuesTx on a restricted user, inside a write on one connection.
func TestCheckValuesTx_NonAdminInsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	priya := w.P(t, "priya@vardhan.test")
	var allowed, refused error
	ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) error {
		allowed = w.Auth.CheckValuesTx(ctx, tx, priya, authtest.DTOrder, map[string][]int64{"project_id": {w.Projects["PRJ-METRO"]}})
		refused = w.Auth.CheckValuesTx(ctx, tx, priya, authtest.DTOrder, map[string][]int64{"project_id": {w.Projects["PRJ-TECHPARK"]}})
		return nil
	})
	if allowed != nil {
		t.Errorf("CheckValuesTx(PRJ-METRO) = %v, want nil", allowed)
	}
	var ve *auth.ValueError
	if !errors.As(refused, &ve) || refused.Error() != "You can only use Project PRJ-METRO. Choose one of your allowed projects." {
		t.Errorf("CheckValuesTx(PRJ-TECHPARK) = %v (%T), want the *auth.ValueError listing PRJ-METRO", refused, refused)
	}
}

// CheckLinkTx on a non-Administrator, inside a write on one connection: a
// visible record, one outside the restriction, a missing one and a report's
// claim through the employee path. The rows are inserted in the same tx: the
// target has to be read there.
func TestCheckLinkTx_NonAdminInsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	priya := w.P(t, "priya@vardhan.test")
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	t5InsertPO(t, w, 1, "PO-1", 1, metro, 0)
	t5InsertPO(t, w, 2, "PO-2", 1, techpark, 0)
	deepak := w.P(t, "deepak@vardhan.test").ID
	var visible, hidden, missing, claim error
	ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(ctx,
			`INSERT INTO probe_claim (id, number, state, created_by, created_at, employee_id, project_id) VALUES (1, 'EX-1', 'draft', ?, ?, ?, ?)`,
			int64(deepak), auth.FormatTime(authtest.T0), w.Employees["EMP-007"], metro); err != nil {
			return err
		}
		visible = w.Auth.CheckLinkTx(ctx, tx, priya, authtest.DTOrder, 1)
		hidden = w.Auth.CheckLinkTx(ctx, tx, priya, authtest.DTOrder, 2)
		missing = w.Auth.CheckLinkTx(ctx, tx, priya, authtest.DTOrder, 999)
		claim = w.Auth.CheckLinkTx(ctx, tx, priya, authtest.DTClaim, 1)
		return nil
	})
	if visible != nil {
		t.Errorf("CheckLinkTx(PO-1) = %v, want nil", visible)
	}
	var le *auth.LinkError
	if !errors.As(hidden, &le) || hidden.Error() != "You can't use Purchase Order PO-2. It isn't available to you." {
		t.Errorf("CheckLinkTx(PO-2) = %v (%T), want the *auth.LinkError", hidden, hidden)
	}
	t5WantNotFound(t, "CheckLinkTx(missing)", missing)
	if claim != nil {
		t.Errorf("CheckLinkTx(report's claim) = %v, want nil", claim)
	}
}

// UsersWithRightTx inside a write on one connection, with the employee path and
// a restriction deciding who is listed: Deepak's claim on PRJ-METRO goes to the
// Administrator, Meena, his manager Priya and Deepak himself; on PRJ-TECHPARK
// Priya's restriction drops her.
func TestUsersWithRightTx_InsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	metro, techpark := ctxClaim(w, t, 1, w.Projects["PRJ-METRO"]), ctxClaim(w, t, 2, w.Projects["PRJ-TECHPARK"])
	id := func(email string) auth.UserID { return w.P(t, email).ID }
	admin, meena, priya, deepak := id("admin@vardhan.test"), id("meena@vardhan.test"), id("priya@vardhan.test"), id("deepak@vardhan.test")
	var inMetro, inTechpark []auth.UserID
	ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) (err error) {
		if inMetro, err = w.Auth.UsersWithRightTx(ctx, tx, authtest.DTClaim, auth.RightApprove, metro); err != nil {
			return err
		}
		inTechpark, err = w.Auth.UsersWithRightTx(ctx, tx, authtest.DTClaim, auth.RightApprove, techpark)
		return err
	})
	if want := []auth.UserID{admin, meena, priya, deepak}; !slices.Equal(inMetro, want) {
		t.Errorf("UsersWithRightTx(PRJ-METRO) = %v, want %v", inMetro, want)
	}
	if want := []auth.UserID{admin, meena, deepak}; !slices.Equal(inTechpark, want) {
		t.Errorf("UsersWithRightTx(PRJ-TECHPARK) = %v, want %v", inTechpark, want)
	}
}

// HasRoleTx inside a write on one connection.
func TestHasRoleTx_InsideWriteNoSecondConn(t *testing.T) {
	w := authtest.NewWorld(t)
	priya := w.P(t, "priya@vardhan.test").ID
	var holds, lacks bool
	ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) (err error) {
		if holds, err = w.Auth.HasRoleTx(ctx, tx, priya, "Purchase User"); err != nil {
			return err
		}
		lacks, err = w.Auth.HasRoleTx(ctx, tx, priya, "Director")
		return err
	})
	if !holds || lacks {
		t.Errorf("HasRoleTx(Purchase User, Director) = (%v, %v), want (true, false)", holds, lacks)
	}
}

// An unknown right and an unknown DocType are refused by CanTx for everyone,
// the Administrator included (CD-171).
func TestCanTx_UnknownRefusedForEveryone(t *testing.T) {
	w := authtest.NewWorld(t)
	const noDocType = "authtest.NoSuchDocType"
	for _, email := range []string{"priya@vardhan.test", "admin@vardhan.test"} {
		t.Run(email, func(t *testing.T) {
			p := w.P(t, email)
			var right, rightRec, doctype, doctypeRec error
			ctxOneConn(t, w, func(ctx context.Context, tx *db.Tx) error {
				right = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, "foo", nil)
				rightRec = w.Auth.CanTx(ctx, tx, p, authtest.DTOrder, "foo", ctxPO(1, w.Projects["PRJ-METRO"]))
				doctype = w.Auth.CanTx(ctx, tx, p, noDocType, auth.RightRead, nil)
				doctypeRec = w.Auth.CanTx(ctx, tx, p, noDocType, auth.RightRead, &auth.RecordRef{DocType: noDocType, ID: 1})
				return nil
			})
			const noFoo = "You don't have permission to foo Purchase Order. Ask your administrator for a role that allows it."
			t5WantDenied(t, "CanTx(foo, nil)", right, "foo", false, false, noFoo)
			t5WantDenied(t, "CanTx(foo, record)", rightRec, "foo", false, false, noFoo)
			const plain = `auth: unknown DocType "authtest.NoSuchDocType"`
			for _, err := range []error{doctype, doctypeRec} {
				if err == nil || err.Error() != plain || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrNotFound) {
					t.Errorf("CanTx(unknown DocType) = %v, want the plain error %q", err, plain)
				}
			}
		})
	}
}
