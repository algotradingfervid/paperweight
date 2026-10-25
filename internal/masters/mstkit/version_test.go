package mstkit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// staleParty is DOC's stale wording (doc/messages.go msgStale) with the
// master display "{Label} {code}" as the record (M2, CD-82).
const staleParty = "Someone else changed Party V-SHARMA after you opened it. Reload it and make your changes again."

// bumpVersion sets a kit_thing row's version.
func (k *kit) bumpVersion(t *testing.T, id int64, version int) {
	t.Helper()
	k.write(t, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), `UPDATE kit_thing SET version = ? WHERE id = ?`, version, id)
		return err
	})
}

// wantStale checks err is DOC's stale error: *doc.Error, KindStale, 409,
// a doctype.HTTPError, with msg.
func wantStale(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) {
		t.Errorf("%s = %T %v; want *doc.Error", what, err, err)
		return
	}
	if de.Kind != doc.KindStale {
		t.Errorf("%s Kind = %q; want %q", what, de.Kind, doc.KindStale)
	}
	if err.Error() != msg {
		t.Errorf("%s message = %q; want %q", what, err.Error(), msg)
	}
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Errorf("%s is not a doctype.HTTPError", what)
		return
	}
	if he.Status() != 409 {
		t.Errorf("%s Status() = %d; want 409", what, he.Status())
	}
}

// TestKit_CheckVersionStale has no case ID (CD-94: a write uses the version
// the page posted; DOC's stale error is 409).
func TestKit_CheckVersionStale(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
	k.bumpVersion(t, id, 2)
	k.write(t, func(tx *db.Tx) error {
		wantStale(t, "CheckVersion(stale 1, row at 2)", mstkit.CheckVersion(ctx, tx, party, id, 1), staleParty)
		if err := mstkit.CheckVersion(ctx, tx, party, id, 2); err != nil {
			t.Errorf("CheckVersion(same version 2) = %v; want nil", err)
		}
		if err := mstkit.CheckVersion(ctx, tx, party, 999, 1); !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("CheckVersion(missing id) = %v; want doctype.ErrNotFound", err)
		}
		return nil
	})

	// SetDisabledTx checks the posted version before it writes.
	t.Run("SetDisabledTx stale", func(t *testing.T) {
		k := newKit(t)
		id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
		k.bumpVersion(t, id, 2)
		err := k.db.Write(ctx, func(tx *db.Tx) error {
			return k.env.SetDisabledTx(ctx, tx, k.kavya, party, id, 1, true)
		})
		wantStale(t, "SetDisabledTx(stale)", err, staleParty)
		if disabled, version, ok := k.thing(t, id); !ok || disabled != 0 || version != 2 {
			t.Errorf("after stale disable: disabled=%d version=%d exists=%v; want 0, 2, true", disabled, version, ok)
		}
		if es := k.audit.For("Party", id); len(es) != 0 {
			t.Errorf("entries after stale disable = %+v; want none", es)
		}
	})

	// DeleteTx checks the posted version before it deletes.
	t.Run("DeleteTx stale", func(t *testing.T) {
		k := newKit(t)
		id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
		k.bumpVersion(t, id, 2)
		err := k.db.Write(ctx, func(tx *db.Tx) error {
			return k.env.DeleteTx(ctx, tx, k.kavya, party, id, 1)
		})
		wantStale(t, "DeleteTx(stale)", err, staleParty)
		if _, version, ok := k.thing(t, id); !ok || version != 2 {
			t.Errorf("after stale delete: version=%d exists=%v; want 2, true", version, ok)
		}
		if es := k.audit.For("Party", id); len(es) != 0 {
			t.Errorf("entries after stale delete = %+v; want none", es)
		}
	})

	// The version is checked before references: a stale page gets 409, not
	// the "Cannot delete" refusal.
	t.Run("DeleteTx stale and referenced", func(t *testing.T) {
		k := newKit(t)
		id := k.insertThing(t, "V-SHARMA", "Sharma Steel Traders")
		k.bumpVersion(t, id, 2)
		k.docs.AddReference("kit_thing", id, doc.Reference{DocType: "Purchase Order", Field: "party", ID: 1, Display: "PO/26-27/00001", State: doc.StateApproved})
		err := k.db.Write(ctx, func(tx *db.Tx) error {
			return k.env.DeleteTx(ctx, tx, k.kavya, party, id, 1)
		})
		var refused *mstkit.DeleteRefusedError
		if errors.As(err, &refused) {
			t.Fatalf("DeleteTx(stale, referenced) = refusal %q; want the stale error (version checked first)", err)
		}
		wantStale(t, "DeleteTx(stale, referenced)", err, staleParty)
		if _, version, ok := k.thing(t, id); !ok || version != 2 {
			t.Errorf("after stale referenced delete: version=%d exists=%v; want 2, true", version, ok)
		}
		if es := k.audit.For("Party", id); len(es) != 0 {
			t.Errorf("entries after stale referenced delete = %+v; want none", es)
		}
	})
}
