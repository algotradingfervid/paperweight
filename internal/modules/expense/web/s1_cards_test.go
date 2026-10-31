package expenseweb

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
)

// A card's receipt is only ever one of the claim's own files: a hash none of
// them holds gives no thumbnail.
func TestReceiptOf_OwnFilesOnly(t *testing.T) {
	own := []files.Meta{{ID: 4, Name: "bill.jpg", MIME: "image/jpeg", SHA256: "aa"}}
	if a := ownReceipt(own, "aa"); a == nil || a.ID != 4 || a.URL != "/files/4" || a.Kind != "image" {
		t.Errorf("own hash: %+v, want file 4 as an image", a)
	}
	if a := ownReceipt(own, "bb"); a != nil {
		t.Errorf("hash on no own file: %+v, want none", a)
	}
}
