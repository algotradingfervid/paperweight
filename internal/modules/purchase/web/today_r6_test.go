package purchaseweb

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// PO spec §3.2: a new order opens on today's business date, from the app's
// clock. At 18:31 UTC on 24 October it is already 25 October in India, so a
// UTC date would show the day before.
func TestOrderNew_OrderDateDefaultsToToday(t *testing.T) {
	env := purchasetest.New(t)
	env.Clock.Set(time.Date(2026, 10, 24, 18, 31, 0, 0, time.UTC))
	w := newP15aWeb(t, env, nil)
	doc := w.get(t, env.Priya, "/doc/"+purchase.DocType+"/new").doc
	in := doc.Find("input#" + components.FieldID("order_date"))
	if got := in.AttrOr("value", ""); got != "25-10-2026" {
		t.Errorf("new order's Order date = %q, want 25-10-2026 (the IST date)", got)
	}
	if got := doc.Find(`button[data-set-date="25-10-2026"]`).AttrOr("aria-pressed", ""); got != "true" {
		t.Errorf("Today chip aria-pressed = %q, want true", got)
	}
	if got := doc.Find("input#"+components.FieldID("required_by")).AttrOr("value", ""); got != "" {
		t.Errorf("Required by = %q, want it left blank", got)
	}
}
