package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/web"
)

// Browsers read header bytes as Latin-1, so HX-Trigger must be pure ASCII and
// still decode to the exact text.
func TestWeb_HTMXTriggerIsASCII(t *testing.T) {
	const text = "Amount exceeds ₹5,000 for सप्लायर 😀"
	rec := httptest.NewRecorder()
	web.Error(rec, htmxRequest(), http.StatusForbidden, text)

	trigger := rec.Header().Get("HX-Trigger")
	for i := 0; i < len(trigger); i++ {
		if trigger[i] > 0x7F {
			t.Fatalf("HX-Trigger = %q has non-ASCII byte 0x%X at %d", trigger, trigger[i], i)
		}
	}
	var got struct {
		Notice web.Notice `json:"notice"`
	}
	if err := json.Unmarshal([]byte(trigger), &got); err != nil {
		t.Fatalf("HX-Trigger = %q is not JSON: %v", trigger, err)
	}
	if got.Notice.Level != "error" || got.Notice.Text != text {
		t.Errorf("HX-Trigger decodes to %+v, want level error and text %q", got.Notice, text)
	}
}

func TestWeb_StaticOnlyGetAndHead(t *testing.T) {
	needURL(t, "HTMXURL", web.HTMXURL)
	h := web.Static()
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if rec := serve(t, h, httptest.NewRequest(method, web.HTMXURL, nil)); rec.Code != http.StatusOK {
			t.Errorf("%s %q status = %d, want 200", method, web.HTMXURL, rec.Code)
		}
	}
	rec := serve(t, h, httptest.NewRequest(http.MethodPost, web.HTMXURL, nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %q status = %d, want 405", web.HTMXURL, rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("POST %q Allow = %q, want %q", web.HTMXURL, got, "GET, HEAD")
	}
}
