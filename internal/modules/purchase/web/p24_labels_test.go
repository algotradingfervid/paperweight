package purchaseweb

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// p24Detail is the collapsed text of the dd beside the Details dt label.
func p24Detail(p p15dPage, label string) string {
	return p15dCollapse(p.doc.Find("dt").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return p15dCollapse(s.Text()) == label
	}).First().Next().Text())
}

// MOD-PO P24 (CD-325, CD-330 (5); UX §5.2, §5.14): the request and order
// pages' Details show the Select values' UX labels, never the codes. Lists,
// chips and the CSV are TestApp_PurchaseSelectLabels.
func TestP24_SelectLabels(t *testing.T) {
	w := newP15dWeb(t)
	env := w.env
	h := w.heldPRA(t)

	page := w.get(t, env.Priya, p15dDoc(purchase.RequestDocType, h.pr.ID))
	if got := p24Detail(page, "Ordered"); got != "Part ordered" {
		t.Errorf("request Details Ordered = %q, want Part ordered", got)
	}
	if strings.Contains(page.text(), "part_ordered") {
		t.Errorf("request page shows the raw part_ordered: %s", page.text())
	}

	page = w.get(t, env.Priya, p15dDoc(purchase.DocType, h.ordered.ID))
	if got := p24Detail(page, "Receipt"); got != "Not received" {
		t.Errorf("order Details Receipt = %q, want Not received", got)
	}
	if strings.Contains(page.text(), "not_received") {
		t.Errorf("order page shows the raw not_received: %s", page.text())
	}
}

// MOD-PO P24 (spec §23; UX §5.13): the Sent banner words each way for a
// sentence, so By hand reads "by hand", never "by By hand".
func TestP24_SentBannerWords(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	path := orderPath(po.ID, "/send")
	env.SetNow(t, time.Date(2026, time.October, 24, 18, 31, 0, 0, time.UTC))
	wantFlash(t, "mark", w.post(t, env.Priya, path, url.Values{"version": {"0"}, "sent_on": {"25-10-2026"}, "sent_via": {"by_hand"}}), poURL(po.ID), po.Number+" marked as sent.")
	if page := w.get(t, env.Priya, path); page.status != http.StatusOK || !hasBanner(page, "info", "Sent to vendor on 25-10-2026 by hand (Priya Shah)") {
		t.Errorf("banners = %d %q, want by hand", page.status, banners(page))
	}

	want := map[string]string{"email": "email", "whatsapp": "WhatsApp", "courier": "courier", "by_hand": "hand", "other": "other means"}
	for _, o := range sentVia {
		if got := viaSentence(o.Value); got != want[o.Value] {
			t.Errorf("viaSentence(%q) = %q, want %q", o.Value, got, want[o.Value])
		}
	}
	if len(sentVia) != len(want) {
		t.Errorf("sentVia has %d ways, want %d", len(sentVia), len(want))
	}
}
