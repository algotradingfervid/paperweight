package purchaseweb

import (
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p11Unparseable is DOC's unreadable-value wording for Sent on (AD-13).
const p11Unparseable = "Sent on isn't a valid value. Check it and try again."

// PO-063 handler (AD-13 ruling 3; CD-142): a Sent on that doesn't parse is
// refused in the handler with DOC's wording, 422 through renderSend, and
// nothing is written.
func TestPurchaseWeb_SendPostUnparseableDate(t *testing.T) {
	w := newP11Web(t)
	env := w.env
	po := purchasetest.NewOrder(t, env)

	for _, on := range []string{"soon", "31-02-2026", "2026-10-24x"} {
		t.Run(on, func(t *testing.T) {
			n := w.rows(t)
			a := w.post(t, env.Priya, sendPath(po.ID), sendForm("0", on, "email", ""))
			if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text, p11Unparseable) {
				t.Errorf("POST = %d %q, want 422 with %q", a.status, a.text, p11Unparseable)
			}
			if len(w.renders) != 1 || w.renders[0].status != http.StatusUnprocessableEntity || !slices.Equal(w.renders[0].msgs, []string{p11Unparseable}) {
				t.Errorf("renderSend calls = %+v, want one {422 [%q]}", w.renders, p11Unparseable)
			}
			if got := w.rows(t); got != n {
				t.Errorf("rows after the refusal = %d, want %d", got, n)
			}
		})
	}
}
