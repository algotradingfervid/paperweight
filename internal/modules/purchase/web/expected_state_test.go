package purchaseweb

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-080 handlers, AD-13 ruling on P18 fix round 1 (spec §27.1): POST
// …/expected/clear on a closed PO redraws through renderExpected with 422
// and the not-approved text, with and without a record, and changes
// nothing. An expected_on of only spaces is blank: 422 with "Enter the
// expected date.", not the invalid-value text.
func TestPurchaseWeb_ExpectedClosedAndBlank(t *testing.T) {
	w := newP18Web(t)
	env := w.env

	closeAsPriya := func(t *testing.T, po doc.Doc) {
		t.Helper()
		if _, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, po.ID, doc.ActionClose,
			doc.ActionInput{Version: po.Version, Reason: "Balance not needed"}); err != nil {
			t.Fatalf("close PO %d as Priya: %v", po.ID, err)
		}
	}
	withRow := purchasetest.NewOrder(t, env)
	if a := w.post(t, env.Arjun, expectedPath(withRow.ID), setForm("0", "05-11-2026", "")); a.status != http.StatusSeeOther {
		t.Fatalf("set on the approved PO = %d (%q), want 303", a.status, a.text)
	}
	closeAsPriya(t, withRow)
	noRow := purchasetest.NewOrder(t, env)
	closeAsPriya(t, noRow)
	open := purchasetest.NewOrder(t, env)

	for _, c := range []struct {
		name    string
		id      int64
		form    url.Values
		status  int
		msg     string
		wantRow bool
	}{
		{"clear a closed PO with a record", withRow.ID, url.Values{"version": {"1"}}, http.StatusUnprocessableEntity, p18NotApproved, true},
		{"clear a closed PO with no record", noRow.ID, url.Values{"version": {"0"}}, http.StatusUnprocessableEntity, p18NotApproved, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, audits := w.rows(t), w.audits(t, c.id)
			a := w.post(t, env.Arjun, clearPath(c.id), c.form)
			if a.status != c.status || !strings.Contains(a.text, c.msg) {
				t.Errorf("POST = %d %q, want %d with %q", a.status, a.text, c.status, c.msg)
			}
			if len(w.renders) != 1 || w.renders[0].status != c.status || !slices.Equal(w.renders[0].msgs, []string{c.msg}) {
				t.Errorf("renderExpected calls = %+v, want one %d with %q", w.renders, c.status, c.msg)
			}
			if got := w.rows(t); got != n {
				t.Errorf("rows after the refusal = %d, want %d", got, n)
			}
			if _, v, ok := w.row(t, c.id); ok != c.wantRow || (ok && v != 1) {
				t.Errorf("row after the refusal = v%d %v, want there %v (v1)", v, ok, c.wantRow)
			}
			if got := w.audits(t, c.id); got != audits {
				t.Errorf("PO audit entries after the refusal = %d, want %d", got, audits)
			}
		})
	}

	t.Run("expected_on of only spaces is blank", func(t *testing.T) {
		a := w.post(t, env.Arjun, expectedPath(open.ID), setForm("0", "   ", ""))
		if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text, p18Blank) || strings.Contains(a.text, p18Unparseable) {
			t.Errorf("POST = %d %q, want 422 with %q and not %q", a.status, a.text, p18Blank, p18Unparseable)
		}
		if len(w.renders) != 1 || w.renders[0].status != http.StatusUnprocessableEntity || !slices.Equal(w.renders[0].msgs, []string{p18Blank}) {
			t.Errorf("renderExpected calls = %+v, want one 422 with %q", w.renders, p18Blank)
		}
		if _, _, ok := w.row(t, open.ID); ok {
			t.Error("row after the blank set, want none")
		}
	})
}
