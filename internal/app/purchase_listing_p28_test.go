package app_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// MOD-PO P28 (CD-327 (1), CD-334): on the real app, the request list offers
// Request date as a sort both ways; Purchase's Hide answers false for it
// with Listing true, so the listing's Hide filter keeps it.
func TestApp_PurchaseRequestListSortsByDate(t *testing.T) {
	w := newWorld(t)
	w.canonical()
	b := w.signedIn(adminEmail)
	r := b.get("/doc/" + purchase.RequestDocType)
	if r.status != http.StatusOK {
		t.Fatalf("GET request list = %d %q", r.status, r.pageText())
	}
	for _, v := range []string{"request_date.desc", "request_date.asc"} {
		if r.doc.Find(`option[value="`+v+`"]`).Length() != 1 {
			t.Errorf("request list has no sort option %s, want Request date offered", v)
		}
	}
}
