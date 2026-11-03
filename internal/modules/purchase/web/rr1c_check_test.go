package purchaseweb

import (
	"errors"
	"slices"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// rr1cNoState is the submit refusal for a supplier with no GSTIN and no
// state on its address (gstcalc.SupplierState).
const rr1cNoState = "Set the state on the supplier's address so GST can be worked out."

// RR1c 3 (CD-397 (4)): a PO whose vendor has no GSTIN and no state on its
// address shows Terms as Needs attention on Check, listing Submit's own
// refusal, and Submit refuses it with that text. The place note claims
// neither inter- nor intra-state; with the state set again the note is
// back and Terms is Complete.
func TestRR1c_CheckSupplierState(t *testing.T) {
	w := newP15aWeb(t, purchasetest.New(t), nil)
	env := w.env
	ok := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	path := p15dDoc(purchase.DocType, ok.ID) + "/edit/check"
	terms := func() (string, []string, []string) {
		check := w.get(t, env.Priya, path)
		sec := r7bSection(check.doc, "Terms")
		return p15dCollapse(sec.Find(".check-status").Text()),
			sec.Find("ul > li").Map(func(_ int, s *goquery.Selection) string { return p15dCollapse(s.Text()) }),
			check.find("main .banner--info")
	}
	if status, _, notes := terms(); status != "Complete" || !r13Has(notes, "-state:") {
		t.Fatalf("with a state: Terms = %q and notes %q, want Complete and the place reason", status, notes)
	}
	var vendor string
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT vendor_id FROM purchase_order WHERE id = ?`, ok.ID).Scan(&vendor); err != nil {
		t.Fatal(err)
	}
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE address SET gstin = '', state_code = NULL WHERE party_id = ?`, vendor); err != nil {
		t.Fatal(err)
	}
	// As a save since then has left it (Validate writes the snapshots).
	if _, err := env.DB.SQL().ExecContext(t.Context(), `UPDATE purchase_order SET snap_vendor_state = '' WHERE id = ?`, ok.ID); err != nil {
		t.Fatal(err)
	}
	status, listed, notes := terms()
	if status != "Needs attention" || !slices.Equal(listed, []string{rr1cNoState}) {
		t.Errorf("no state: Terms = %q listing %q, want Needs attention listing %q", status, listed, rr1cNoState)
	}
	if r13Has(notes, "-state:") || r13Has(notes, "is in and") || !slices.Contains(notes, "Information: GST can't be worked out until the supplier's address has a state.") {
		t.Errorf("no state: notes %q, want the no-state note and no inter- or intra-state claim", notes)
	}
	_, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, ok.ID, doctype.ActionSubmit, doc.ActionInput{Version: rr1cVersion(t, w, ok.ID)})
	var he doctype.HTTPError
	if !errors.As(err, &he) || !slices.ContainsFunc(he.FieldErrors(), func(f doctype.FieldError) bool { return f.Message == rr1cNoState }) {
		t.Errorf("submit: %v, want refused with %q", err, rr1cNoState)
	}
}

// rr1cVersion is PO id's stored version.
func rr1cVersion(t *testing.T, w *p15dWeb, id int64) int {
	t.Helper()
	var v int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}
