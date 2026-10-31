package purchase_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// msgSentNoteTooLong is AD-13's ruling text for a note over 200 characters.
const msgSentNoteTooLong = "Note can be at most 200 characters."

// PO-063 (PO spec §23 note max 200; AD-13 ruling 1): a note over 200
// characters is refused with its own message (422) and nothing changes;
// exactly 200 characters saves, counted in characters, not bytes.
func TestPO_MarkSentNoteLength(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env) // ordered and marked on 24-10-2026
	on := sentDate(2026, time.October, 24)

	for _, note := range []string{strings.Repeat("a", 201), strings.Repeat("₹", 201)} {
		before, row := sentCountsOf(t, env, po.ID), sentRow(t, env, po.ID)
		err := env.Service.MarkSent(ctx, env.Priya, po.ID, 0, purchase.SentInput{SentOn: on, SentVia: "email", Note: note})
		sentWantRefused(t, "MarkSent with a 201-character note", err, http.StatusUnprocessableEntity, msgSentNoteTooLong)
		if after := sentCountsOf(t, env, po.ID); after != before {
			t.Errorf("counts after the refused note = %+v, want %+v", after, before)
		}
		if after := sentRow(t, env, po.ID); after != row {
			t.Errorf("row after the refused note = %+v, want %+v", after, row)
		}
	}

	version := 0
	for _, note := range []string{strings.Repeat("a", 200), strings.Repeat("₹", 200)} {
		if err := env.Service.MarkSent(ctx, env.Priya, po.ID, version, purchase.SentInput{SentOn: on, SentVia: "email", Note: note}); err != nil {
			t.Errorf("MarkSent with a 200-character note (%d bytes): %v", len(note), err)
			continue
		}
		version++
		if r := sentRow(t, env, po.ID); !r.ok || r.note != note || r.version != version {
			t.Errorf("row after the 200-character note = version %d, note of %d bytes; want version %d with the note", r.version, len(r.note), version)
		}
	}
}

// PO-063 (PO spec §23; AD-13 ruling 2): Mark as not sent on an approved PO
// with no Sent row, posted version 0, is a no-op: nil, no row, no audit
// entry, and the PO row unchanged.
func TestPO_UnmarkSentNoRow(t *testing.T) {
	env := purchasetest.New(t)
	po := purchasetest.NewOrder(t, env)
	before := sentCountsOf(t, env, po.ID)
	pre := sentLoadPO(t, env, po.ID)

	if err := env.Service.UnmarkSent(t.Context(), env.Priya, po.ID, 0); err != nil {
		t.Errorf("UnmarkSent with no row, version 0 = %v, want nil (a no-op)", err)
	}
	if after := sentCountsOf(t, env, po.ID); after != before {
		t.Errorf("counts after the no-op = %+v, want %+v (no row, no audit entry)", after, before)
	}
	if r := sentRow(t, env, po.ID); r.ok {
		t.Errorf("row after the no-op = %+v, want none", r)
	}
	for _, e := range sentAudits(t, env, po.ID) {
		if e.Action == auditMarkedNotSent || e.Action == auditMarkedSent {
			t.Errorf("audit entry %q after the no-op, want none", e.Action)
		}
	}
	if post := sentLoadPO(t, env, po.ID); post.Version != pre.Version || !post.UpdatedAt.Equal(pre.UpdatedAt) {
		t.Errorf("PO after the no-op: version %d updated_at %v, want %d %v", post.Version, post.UpdatedAt, pre.Version, pre.UpdatedAt)
	}

	// A mark after the no-op still starts from version 0.
	if err := env.Service.MarkSent(t.Context(), env.Priya, po.ID, 0, purchase.SentInput{SentOn: sentDate(2026, time.October, 24), SentVia: "email"}); err != nil {
		t.Errorf("MarkSent after the no-op: %v", err)
	}
	if r := sentRow(t, env, po.ID); !r.ok || r.version != 1 {
		t.Errorf("row after marking = %+v, want version 1", r)
	}
}
