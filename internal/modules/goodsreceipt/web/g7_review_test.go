package grnweb_test

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// G7 review 3 (UX §5.7: the route answers with the card alone, 409 and 422
// included; never the platform's alert fragment): a stale version, a stale
// version with an unreadable date, and a draft GRN each answer htmx with
// the text inside the card (id "bill").
func TestGRNBillWeb_ReviewRefusalsInCard(t *testing.T) {
	env := grntest.New(t)
	r := chi.NewRouter()
	grnweb.Routes(grnweb.Deps{Service: env.Service})(r)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(map[string]string{"qty": "1200", "price": "62.5"}))
	grn := g7wGRN(t, env, env.Suresh, po, doctype.StateApproved)
	draft := g7wGRN(t, env, env.Suresh, po, doctype.StateDraft)
	if rec := g7wPost(t, r, env.Suresh, grn.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true); rec.Code != http.StatusOK {
		t.Fatalf("first save: %d %q, want 200", rec.Code, rec.Body.String())
	}

	rec := g7wPost(t, r, env.Suresh, grn.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true)
	g7wWant(t, "stale version", rec, http.StatusConflict, "#bill", g7wStale)
	rec = g7wPost(t, r, env.Suresh, grn.ID, g7wForm("0", g7wInvoice, "31-02-2026", ""), true)
	g7wWant(t, "stale version with an unreadable date", rec, http.StatusConflict, "#bill", g7wStale)
	rec = g7wPost(t, r, env.Suresh, draft.ID, g7wForm("0", g7wInvoice, "24-10-2026", ""), true)
	g7wWant(t, "draft GRN", rec, http.StatusUnprocessableEntity, "#bill", g7wNotApproved)
}
