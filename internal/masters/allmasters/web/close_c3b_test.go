package mastersweb_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
)

// UIV2-C3b (UI v2 §5.6): the import wizard's × goes home, through "Leave
// without saving?".
func TestImportClose(t *testing.T) {
	w := newWorld(t)
	r := w.browser().get(t, w.env.Admin, "/m/import/choose")
	webtest.WantStatus(t, r, http.StatusOK)
	x := r.Doc.Find(".wizard-head > a.wizard-close")
	if x.Length() != 1 || x.AttrOr("href", "") != "/" || x.AttrOr("data-open-dialog", "") != "leave-wizard" {
		t.Errorf("×: %d, href %q, opens %q; want / through leave-wizard", x.Length(), x.AttrOr("href", ""), x.AttrOr("data-open-dialog", ""))
	}
	if r.Doc.Find("dialog#leave-wizard a[href='/']").Length() != 1 {
		t.Error("no Leave link home in the dialog")
	}
}
