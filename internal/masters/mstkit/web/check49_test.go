package mstweb_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// M0-49 (CD-322): a master form's Check switch keeps posting "1", which the
// masters' ToInput reads (`== "1"`), not DOC's "true"; Item's is_stock is
// the generic Check here, new and stored.
func TestScreens_CheckPostsOne(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"is_stock": "1"}})
	for _, target := range []string{base + "/new", record} {
		resp := h.Get(t, h.Env.Kavya, target)
		webtest.WantStatus(t, resp, http.StatusOK)
		in := resp.Doc.Find(`input[type="checkbox"][role="switch"][name="is_stock"]`)
		if in.Length() != 1 {
			t.Fatalf("%s: no is_stock switch", target)
		}
		if v, _ := in.Attr("value"); v != "1" {
			t.Errorf("%s: is_stock posts %q, want 1", target, v)
		}
		if _, on := in.Attr("checked"); target == record && !on {
			t.Errorf("%s: stored is_stock 1 drawn off", target)
		}
	}
}
