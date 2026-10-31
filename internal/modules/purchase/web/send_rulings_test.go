package purchaseweb

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p11Unparseable is DOC's unreadable-value wording for Sent on (AD-13).
const p11Unparseable = "Sent on isn't a valid value. Check it and try again."

// PO-063 handler (AD-13 rulings 3 and fix-1; CD-142, CD-259): a Sent on that
// doesn't parse, blank included, reaches MarkSent as the zero date, so load,
// right, version and state are checked first: AUTH's 403 uses the error
// page, a stale version is the 409 stale text, a draft is the not-approved
// 422. On an approved PO at the right version it is DOC's unreadable-value
// wording, 422 through renderSend. Nothing is written.
func TestPurchaseWeb_SendPostUnparseableDate(t *testing.T) {
	w := newP11Web(t)
	env := w.env
	po := purchasetest.NewOrder(t, env)
	display := naming.Label(po.Number, po.ID)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	bad := []string{"soon", "31-02-2026", "2026-10-24x", ""}
	redraws := []struct {
		name   string
		p      auth.Principal
		id     int64
		ver    string
		status int
		msg    string
	}{
		{"approved", env.Priya, po.ID, "0", http.StatusUnprocessableEntity, p11Unparseable},
		{"stale", env.Priya, po.ID, "5", http.StatusConflict, p11Stale(display)},
		{"draft", env.Priya, draft.ID, "0", http.StatusUnprocessableEntity, p11NotApproved},
	}
	for _, c := range redraws {
		for _, on := range bad {
			t.Run(c.name+"/"+on, func(t *testing.T) {
				n := w.rows(t)
				a := w.post(t, c.p, sendPath(c.id), sendForm(c.ver, on, "email", ""))
				if a.status != c.status || !strings.Contains(a.text, c.msg) {
					t.Errorf("POST = %d %q, want %d with %q", a.status, a.text, c.status, c.msg)
				}
				if len(w.renders) != 1 || w.renders[0].status != c.status || !slices.Equal(w.renders[0].msgs, []string{c.msg}) {
					t.Errorf("renderSend calls = %+v, want one {%d [%q]}", w.renders, c.status, c.msg)
				}
				if got := w.rows(t); got != n {
					t.Errorf("rows after the refusal = %d, want %d", got, n)
				}
			})
		}
	}

	// No right: AUTH's 403 on the error page, before the date (CD-259).
	denied := []string{
		(&auth.DeniedError{Right: auth.RightWrite, Label: "Purchase Order", Plural: "Purchase Orders"}).Error(),
		(&auth.DeniedError{Right: auth.RightWrite, Label: "Purchase Order", Plural: "Purchase Orders", OwnOnly: true}).Error(),
		(&auth.DeniedError{Right: auth.RightApprove, Label: "Purchase Order", Plural: "Purchase Orders"}).Error(),
	}
	for _, p := range []auth.Principal{env.Arjun, env.Suresh} {
		for _, on := range bad {
			t.Run("no right "+p.Name+"/"+on, func(t *testing.T) {
				n := w.rows(t)
				a := w.post(t, p, sendPath(po.ID), sendForm("0", on, "email", ""))
				found := slices.ContainsFunc(denied, func(m string) bool { return strings.Contains(a.text, m) })
				if a.status != http.StatusForbidden || !found {
					t.Errorf("POST = %d %q, want 403 with one of %q", a.status, a.text, denied)
				}
				if len(w.renders) != 0 {
					t.Errorf("renderSend called for a 403: %+v (403 uses the error page)", w.renders)
				}
				if got := w.rows(t); got != n {
					t.Errorf("rows after the refusal = %d, want %d", got, n)
				}
			})
		}
	}
}
