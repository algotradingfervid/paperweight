package purchaseweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// MOD-PO P1 review focus (AD-13 ruling): a stub route answers 501 with
// msgNotBuilt, never success. P9 deletes this test with stubs_web_p9.go.
func TestPurchaseWeb_StubAnswers501(t *testing.T) {
	rec := httptest.NewRecorder()
	(&handlers{}).pendingExport(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/reports/purchase/pending/export.csv", nil))
	if rec.Code != http.StatusNotImplemented || strings.TrimSpace(rec.Body.String()) != msgNotBuilt {
		t.Errorf("pendingExport stub = %d %q, want 501 %q", rec.Code, rec.Body.String(), msgNotBuilt)
	}
}
