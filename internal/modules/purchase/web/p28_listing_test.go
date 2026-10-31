package purchaseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// MOD-PO P28 (CD-327 (1), CD-334): on the list and search pages (Listing
// true, View nil) requestHide hides nothing, so Request date stays a sort
// and a column there; a nil View without Listing is still a new record.
func TestP28_RequestHideListing(t *testing.T) {
	var m module
	if m.requestHide(docscreen.Ctx{Listing: true}, doctype.Field{Name: "request_date"}) {
		t.Error("requestHide(Listing, nil View, request_date) = true, want shown on the list and search pages")
	}
	if !m.requestHide(docscreen.Ctx{}, doctype.Field{Name: "request_date"}) {
		t.Error("requestHide(nil View, request_date) = false, want hidden on a new record")
	}
}
