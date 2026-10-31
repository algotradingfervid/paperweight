package purchase_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
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

// msgSentUnreadable is DOC's unreadable-value wording for Sent on (AD-13).
const msgSentUnreadable = "Sent on isn't a valid value. Check it and try again."

// PO-063 (PO spec §23; AD-13 fix-1 ruling; CD-259): the handler passes the
// zero date for a Sent on that doesn't parse. MarkSent refuses it with DOC's
// unreadable-value wording on sent_on (422) after load, right, version and
// state, and before the future, before-order-date and method rules; never
// the before-order-date text. Nothing changes.
func TestPO_MarkSentZeroDate(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	po := purchasetest.NewOrder(t, env)
	display := naming.Label(po.Number, po.ID)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	rec := sentRef(t, env, po)
	var zero types.Date

	for _, c := range []struct {
		name   string
		id     int64
		in     purchase.SentInput
		status int
		msg    string
		field  string
	}{
		{"approved", po.ID, purchase.SentInput{SentOn: zero, SentVia: "email"}, http.StatusUnprocessableEntity, msgSentUnreadable, "sent_on"},
		{"approved, no method: date first", po.ID, purchase.SentInput{SentOn: zero}, http.StatusUnprocessableEntity, msgSentUnreadable, "sent_on"},
		{"draft: state first", draft.ID, purchase.SentInput{SentOn: zero, SentVia: "email"}, http.StatusUnprocessableEntity, msgSentNotApproved, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			before, row := sentCountsOf(t, env, c.id), sentRow(t, env, c.id)
			err := env.Service.MarkSent(ctx, env.Priya, c.id, 0, c.in)
			sentWantRefused(t, c.name, err, c.status, c.msg)
			if c.field != "" {
				sentWantField(t, c.name, err, c.field, c.msg)
			}
			if err != nil && err.Error() == msgSentBeforeOrder {
				t.Errorf("%s = the before-order-date text, want the unreadable-value text", c.name)
			}
			if after := sentCountsOf(t, env, c.id); after != before {
				t.Errorf("counts after the refusal = %+v, want %+v", after, before)
			}
			if after := sentRow(t, env, c.id); after != row {
				t.Errorf("row after the refusal = %+v, want %+v", after, row)
			}
		})
	}

	t.Run("stale: version first", func(t *testing.T) {
		err := env.Service.MarkSent(ctx, env.Priya, po.ID, 5, purchase.SentInput{SentOn: zero, SentVia: "email"})
		sentWantRefused(t, "MarkSent stale with the zero date", err, http.StatusConflict, sentStale(display))
	})
	t.Run("no right: right first", func(t *testing.T) {
		for _, p := range []auth.Principal{env.Arjun, env.Suresh} {
			sentWantDenied(t, env, "MarkSent as "+p.Name+" with the zero date", env.Service.MarkSent(ctx, p, po.ID, 0, purchase.SentInput{SentOn: zero, SentVia: "email"}), p, rec)
		}
	})
	if r := sentRow(t, env, po.ID); r.ok {
		t.Errorf("row after the refusals = %+v, want none", r)
	}
}
