package app_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
)

// No case ID: UI v2 D2 (CD-375). POST /m/party/quick sits behind AUTH's
// CSRF check like every master write: without the token it is AUTH's 403
// and nothing is saved; with it the vendor is saved.
func TestApp_QuickCreateNeedsCSRF(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	admin := w.signedIn(adminEmail)
	form := url.Values{
		"role": {"vendor"}, "quick_code": {"SAIKRUPA"}, "quick_name": {"Sai Krupa Sand Suppliers"},
		"quick_line1": {"Survey 42, Hadapsar"}, "quick_city": {"Pune"}, "quick_state_code": {"27"},
		"pick_id": {"f-vendor_id"}, "pick_label": {"Vendor"}, "pick_search": {"/pick/Party?role=vendor"},
		"pick_name": {"vendor_id"}, "pick_list": {"f-vendor_id-list"},
	}
	htmx := map[string]string{"HX-Request": "true"}
	noToken := *admin
	noToken.csrf = ""
	if r := noToken.post("/m/party/quick", form); r.status != http.StatusForbidden || !strings.Contains(r.pageText(), auth.ErrCSRFSignIn.Msg) {
		t.Errorf("POST without CSRF = %d %q, want 403 %q", r.status, r.pageText(), auth.ErrCSRFSignIn.Msg)
	}
	if n := w.count(`SELECT count(*) FROM party WHERE code = 'SAIKRUPA'`); n != 0 {
		t.Errorf("parties saved without a token = %d, want 0", n)
	}
	if r := admin.postHeaders("/m/party/quick", form, htmx); r.status != http.StatusOK {
		t.Fatalf("POST with CSRF = %d %q, want 200", r.status, r.alert())
	}
	if n := w.count(`SELECT count(*) FROM party p JOIN address a ON a.party_id = p.id AND a.is_primary_billing = 1 WHERE p.code = 'SAIKRUPA' AND p.is_vendor = 1`); n != 1 {
		t.Errorf("vendors with a primary billing address = %d, want 1", n)
	}
}
