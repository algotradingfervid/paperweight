package purchase_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// msgExpectedUnreadable is DOC's unreadable-value text for Expected on
// (plan P18), copied exactly (CD-147).
const msgExpectedUnreadable = "Expected on isn't a valid value. Check it and try again."

// MOD-PO P15e fix round 1, CD-259 as ruled for Expected on (AD-13):
// SetExpectedPosted, the form's path, takes the posted date as typed and
// parses it inside the write after SetExpected's load, right, version and
// state checks, as MarkSent does: a stale version,
// a missing right or a closed PO each win over a bad date. Blank (also only
// spaces) and a date that doesn't parse keep their own messages (P18), after
// those checks.
func TestPO_ExpectedOnDateAfterChecks(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	svc := env.Service
	po := purchasetest.NewOrder(t, env)
	display := naming.Label(po.Number, po.ID)

	expWantRefused(t, "stale (3, no row) and 31-02-2026",
		svc.SetExpectedPosted(ctx, env.Priya, po.ID, 3, "31-02-2026", ""), http.StatusConflict, expStale(display))
	expWantRefused(t, "stale (3, no row) and blank",
		svc.SetExpectedPosted(ctx, env.Priya, po.ID, 3, "  ", ""), http.StatusConflict, expStale(display))
	expWantDenied(t, "Suresh and 31-02-2026", svc.SetExpectedPosted(ctx, env.Suresh, po.ID, 0, "31-02-2026", ""))
	expWantNotFound(t, "missing PO and 31-02-2026", svc.SetExpectedPosted(ctx, env.Priya, 999_999, 0, "31-02-2026", ""))

	closed := purchasetest.NewOrder(t, env)
	expClose(t, env, closed)
	expWantRefused(t, "closed and 31-02-2026",
		svc.SetExpectedPosted(ctx, env.Priya, closed.ID, 0, "31-02-2026", ""), http.StatusUnprocessableEntity, msgExpectedNotApproved)
	expWantRefused(t, "closed and blank",
		svc.SetExpectedPosted(ctx, env.Priya, closed.ID, 0, "", ""), http.StatusUnprocessableEntity, msgExpectedNotApproved)

	for _, s := range []string{"", "   "} {
		expWantRefused(t, fmt.Sprintf("blank %q", s), svc.SetExpectedPosted(ctx, env.Priya, po.ID, 0, s, ""), http.StatusUnprocessableEntity, msgExpectedBlank)
	}
	expWantRefused(t, "31-02-2026", svc.SetExpectedPosted(ctx, env.Priya, po.ID, 0, "31-02-2026", ""), http.StatusUnprocessableEntity, msgExpectedUnreadable)
	if r := expectedRow(t, env, po.ID); r.ok {
		t.Errorf("row after the refusals = %+v, want none", r)
	}
	if err := svc.SetExpectedPosted(ctx, env.Priya, po.ID, 0, " 05-11-2026 ", ""); err != nil {
		t.Fatalf("SetExpectedPosted(\" 05-11-2026 \") = %v, want it saved", err)
	}
	if got, ok := expectedInfo(t, env, po.ID); !ok || got.ExpectedOn != expDate(2026, 11, 5) {
		t.Errorf("ExpectedInfo = %+v, %v; want 05-11-2026", got, ok)
	}
}
