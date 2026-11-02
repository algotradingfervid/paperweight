package grnweb_test

import (
	"net/http"
	"strconv"
	"strings"
	"testing"

	grnweb "github.com/vardhan/internaltools/internal/modules/goodsreceipt/web"
)

// GRN-029, the S1 rows of TestGRN_RestrictedCreateReal (CD-283, CD-312;
// GUX-006). They live here because the preselect is grnweb's. Suresh, on
// the real auth.Service, opens step 1 with ?po= the reference PO
// (METRO-SITE, which he may read) and finds it chosen among two, labelled
// PO/26-27/00001, and ReceiveLink gives the link; with ?po= a TECH-SITE PO
// nothing is chosen, nothing names it, no error, and ReceiveLink is false.
// The check is auth.CheckLinkTx on the PO plus create on the GRN: Priya
// reads the PO but can't create a GRN, so she gets no link. A fully
// received PO is chosen in the widened list.
func TestGRN_RestrictedCreateRealPreselect(t *testing.T) {
	w := newS1World(t)
	ref := w.po("SHREE", "1200", "50")
	other := w.po("BHARAT", "5")
	tech := w.techPO()
	full := w.po("RAJKOT", "7")
	w.receiveAll(full)
	links := grnweb.Deps{Service: w.Service}
	refID := strconv.FormatInt(ref.ID, 10)

	r := w.get(w.Suresh, s1New+"?po="+refID)
	got := r.radios("purchase_order_id")
	if len(got) != 2 || got[0].value != strconv.FormatInt(other.ID, 10) || got[0].checked ||
		got[1].value != refID || !got[1].checked || !strings.HasPrefix(got[1].text, "PO/26-27/00001 · Shree Steel Traders") {
		t.Errorf("?po= the reference PO: radios %+v, want it checked as PO/26-27/00001 beside Bharat's", got)
	}
	if href, ok := links.ReceiveLink(t.Context(), w.Suresh, ref.ID); !ok || href != s1New+"?po="+refID {
		t.Errorf("ReceiveLink(suresh, reference PO) = %q %v, want %s?po=%s true", href, ok, s1New, refID)
	}

	r = w.get(w.Suresh, s1New+"?po="+strconv.FormatInt(tech.ID, 10))
	if r.status != http.StatusOK || r.errorSummary() != "" {
		t.Errorf("?po= TECH-SITE PO = %d %q, want 200 with no error", r.status, r.errorSummary())
	}
	for _, o := range r.radios("purchase_order_id") {
		if o.checked {
			t.Errorf("?po= TECH-SITE PO checks %+v", o)
		}
	}
	if strings.Contains(r.body, tech.Number) {
		t.Errorf("?po= TECH-SITE PO names %s on the page (CD-233)", tech.Number)
	}
	if href, ok := links.ReceiveLink(t.Context(), w.Suresh, tech.ID); ok || href != "" {
		t.Errorf("ReceiveLink(suresh, TECH-SITE PO) = %q %v, want false", href, ok)
	}
	if href, ok := links.ReceiveLink(t.Context(), w.Priya, ref.ID); ok || href != "" {
		t.Errorf("ReceiveLink(priya, who can't create a GRN) = %q %v, want false", href, ok)
	}
	if _, ok := links.ReceiveLink(t.Context(), w.Suresh, 999999); ok {
		t.Errorf("ReceiveLink of a missing PO is true")
	}

	r = w.get(w.Suresh, s1New+"?po="+strconv.FormatInt(full.ID, 10))
	var checked []s1Radio
	for _, o := range r.radios("purchase_order_id") {
		if o.checked {
			checked = append(checked, o)
		}
	}
	if len(checked) != 1 || checked[0].text != full.Number+" · Rajkot Castings METRO-SITE Fully received · ordered 24-10-2026" {
		t.Errorf("?po= a fully received PO: checked %+v, want it alone, in the widened list", checked)
	}

	// A ?po= of no PO is ignored like an unreadable one: 200, no error,
	// nothing chosen (Receivable's not-found arm).
	r = w.get(w.Suresh, s1New+"?po=999999")
	if r.status != http.StatusOK || r.errorSummary() != "" {
		t.Errorf("?po=999999 = %d %q, want 200 with no error", r.status, r.errorSummary())
	}
	for _, o := range r.radios("purchase_order_id") {
		if o.checked {
			t.Errorf("?po=999999 checks %+v", o)
		}
	}

	for _, q := range []string{"abc", "0"} {
		for _, o := range w.get(w.Suresh, s1New+"?po="+q).radios("purchase_order_id") {
			if o.checked {
				t.Errorf("?po=%s checks %+v", q, o)
			}
		}
	}
}
