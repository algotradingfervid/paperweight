package auth_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
)

// A stale session and a cancelled context: Touch's read fails, Touch returns
// that error and writes nothing. db.Write refuses a cancelled context too, so
// TestTouch_ReadFailureStopsTheWrite is the case that needs Touch's own check.
func TestTouch_ReadFailsOnCancelledContext(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	p9LogUpdates(t, e)
	before := e.p9Session(t, hash)

	e.w.Clock.Set(authtest.T0.Add(p9Stale))
	ctx, cancel := context.WithCancel(p9Meta(hash, p9IP2, p9UA2))
	cancel()
	if err := e.st.Touch(ctx, hash); !errors.Is(err, context.Canceled) {
		t.Errorf("Touch = %v, want context.Canceled", err)
	}

	if got := e.p9Session(t, hash); !bytes.Equal(got.data, before.data) || got.lastSeen != before.lastSeen || got.ip != p9IP1 || got.userAg != p9UA1 {
		t.Errorf("row after Touch = %+v, want unchanged %+v", got, before)
	}
	if n := e.p9Updates(t); n != 0 {
		t.Errorf("updates = %d, want 0", n)
	}
}

// A read that fails while the database still accepts writes: a NULL last_seen_at
// can't scan into the read's string. The column is rebuilt nullable to get one.
// Without Touch's check the failure is dropped and the UPDATE matches no row, so
// Touch would answer nil.
func TestTouch_ReadFailureStopsTheWrite(t *testing.T) {
	e := newSessionEnv(t)
	hash := p9Signed(t, e)
	raceExec(t, e.w, `ALTER TABLE sessions DROP COLUMN last_seen_at`)
	raceExec(t, e.w, `ALTER TABLE sessions ADD COLUMN last_seen_at TEXT`)

	e.w.Clock.Set(authtest.T0.Add(p9Stale))
	err := e.st.Touch(p9Meta(hash, p9IP2, p9UA2), hash)
	if err == nil || !strings.Contains(err.Error(), "NULL") {
		t.Errorf("Touch = %v, want the read's NULL scan error", err)
	}
	if n := e.count(t, `SELECT COUNT(*) FROM sessions WHERE token_hash = ? AND ip = ? AND last_seen_at IS NULL`, hash, p9IP1); n != 1 {
		t.Errorf("unchanged rows = %d, want 1", n)
	}
}
