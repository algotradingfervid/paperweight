package files_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// latest calls Latest and LatestURL as p (nil: no principal on ctx)
// in one read tx, with the pool at one connection: a second connection would
// wait on the tx's own until the deadline, and fail.
func latest(t *testing.T, env *filestest.Env, p *auth.Principal, parent auth.RecordRef) (files.Meta, bool, string, error) {
	t.Helper()
	pool := env.DB.SQL()
	pool.SetMaxOpenConns(1)
	defer pool.SetMaxOpenConns(8)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	if p != nil {
		ctx = auth.WithPrincipal(ctx, *p)
	}
	var m files.Meta
	var ok bool
	var url string
	err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if m, ok, err = env.Store.Latest(ctx, tx, parent); err != nil {
			return err
		}
		url, err = env.Store.LatestURL(ctx, tx, parent)
		return err
	})
	return m, ok, url, err
}

func TestLatest_LogoForAnySignedInUser(t *testing.T) {
	env, id := logoEnv(t)
	logo := auth.RecordRef{DocType: "company", ID: 1}
	kavya, deepak := env.User("kavya@vardhan.test"), env.User("deepak@vardhan.test")

	newer, err := env.Attach(kavya, logo, "newer.png", filestest.PNG(2048))
	if err != nil {
		t.Fatal(err)
	}
	if newer <= id {
		t.Fatalf("second attachment id %d is not after %d", newer, id)
	}

	// Deepak has no right on company: ReadAnySignedIn is what lets him in.
	m, ok, url, err := latest(t, env, &deepak, logo)
	if err != nil || !ok {
		t.Fatalf("Latest(deepak) = %v, %v", ok, err)
	}
	if m.ID != newer || m.Name != "newer.png" || url != "/files/"+strconv.FormatInt(newer, 10) {
		t.Errorf("Latest(deepak) = %+v, %q; want attachment %d", m, url, newer)
	}

	if _, ok, url, err := latest(t, env, nil, logo); ok || url != "" || files.KindOf(err) != files.KindForbidden {
		t.Errorf("Latest with no principal = %v, %q, %v; want KindForbidden", ok, url, err)
	}
	var fe *files.Error
	if _, _, _, err := latest(t, env, nil, logo); !errors.As(err, &fe) || fe.Code != http.StatusForbidden || !errors.Is(err, auth.ErrForbidden) {
		t.Errorf("Latest with no principal = %v; want 403 wrapping auth.ErrForbidden", err)
	}
}

func TestLatest_NoAttachmentAndNoParent(t *testing.T) {
	env, _ := logoEnv(t)
	kavya := env.User("kavya@vardhan.test")
	env.Doc("company", 2, "", "Empty", kavya.ID)

	if _, ok, url, err := latest(t, env, &kavya, auth.RecordRef{DocType: "company", ID: 2}); ok || url != "" || err != nil {
		t.Errorf("Latest with no attachment = %v, %q, %v; want none", ok, url, err)
	}
	if _, _, _, err := latest(t, env, &kavya, auth.RecordRef{DocType: "company", ID: 3}); !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("Latest of a missing parent = %v; want not found", err)
	}
}

// Without ReadAnySignedIn the read right is needed, as in List.
func TestLatest_OtherPoliciesNeedTheReadRight(t *testing.T) {
	env := filestest.New(t)
	kavya, deepak := env.User("kavya@vardhan.test"), env.User("deepak@vardhan.test")
	env.Auth.Allow(kavya.ID, "expense", "read", "write")
	ref := env.Doc("expense", 1, "Draft", "EXP-1", kavya.ID)
	id, err := env.Attach(kavya, ref, "bill.pdf", filestest.PDF(1024))
	if err != nil {
		t.Fatal(err)
	}
	if m, ok, _, err := latest(t, env, &kavya, ref); err != nil || !ok || m.ID != id {
		t.Errorf("Latest(kavya) = %+v, %v, %v", m, ok, err)
	}
	if _, _, _, err := latest(t, env, &deepak, ref); files.KindOf(err) != files.KindForbidden {
		t.Errorf("Latest(deepak) = %v; want KindForbidden", err)
	}
}

// Latest sees what the caller's own tx has done.
func TestLatest_ReadsTheCallersTx(t *testing.T) {
	env, id := logoEnv(t)
	kavya := env.User("kavya@vardhan.test")
	logo := auth.RecordRef{DocType: "company", ID: 1}
	ctx := auth.WithPrincipal(t.Context(), kavya)
	err := env.DB.Write(ctx, func(tx *db.Tx) error {
		if err := env.Store.Remove(ctx, tx, kavya, id); err != nil {
			return err
		}
		if _, ok, err := env.Store.Latest(ctx, tx, logo); err != nil || ok {
			t.Errorf("Latest after Remove in tx = %v, %v; want none", ok, err)
		}
		return errors.New("roll back")
	})
	if err == nil {
		t.Fatal("tx did not roll back")
	}
}
