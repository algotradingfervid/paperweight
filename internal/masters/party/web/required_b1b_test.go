package partyweb_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// No case ID: UI v2 B1b, CD-359 (6). A party's address and contact forms have
// required fields (Line 1, City; First name), so each starts with the
// required note.
func TestScreens_PartySubFormsShowRequiredNote(t *testing.T) {
	w := newWorld(t)
	b := w.kavya()
	id := w.get(t, "SHREE").ID
	for _, target := range []string{newAddressURL(id), newContactURL(id)} {
		page := b.Get(t, target)
		webtest.WantStatus(t, page, http.StatusOK)
		if !strings.Contains(webtest.Squash(page.Doc.Find("form").Text()), "Fields marked * are required.") {
			t.Errorf("%s: no required note", target)
		}
	}
}
