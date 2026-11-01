package siteweb_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// No case ID: UI v2 B1b, CD-359 (6). Every field of a site's delivery address
// is optional (blank removes it), so its form has no required note.
func TestScreens_SiteDeliveryHasNoRequiredNote(t *testing.T) {
	w := newWorld(t)
	wh, _ := w.seed(t)
	page := w.h.Get(t, w.h.Env.Kavya, path(wh.ID, "/delivery"))
	webtest.WantStatus(t, page, http.StatusOK)
	if page.Doc.Find(`input[name="delivery_line1"]`).Length() != 1 {
		t.Fatal("no delivery form")
	}
	if strings.Contains(webtest.Squash(page.Doc.Find("form").Text()), "Fields marked * are required.") {
		t.Error("the delivery form shows the required note")
	}
}
