package app_test

import (
	"net/http"
	"testing"
)

// UIV2-C3b (UI v2 §5.6): each setup step's × goes to the inbox, where home
// sends an administrator once setup is done, through "Leave without saving?".
func TestSetupWizard_Close(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(adminEmail)
	b.toCheck(goodSetup())
	for _, path := range []string{setupStep1, setupStep2, setupCheck} {
		r := b.get(path)
		if r.status != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", path, r.status)
		}
		x := r.doc.Find(".wizard-head > a.wizard-close")
		if x.Length() != 1 || x.AttrOr("href", "") != "/approvals" || x.AttrOr("data-open-dialog", "") != "leave-wizard" {
			t.Errorf("%s: ×: %d, href %q, opens %q; want /approvals through leave-wizard", path, x.Length(), x.AttrOr("href", ""), x.AttrOr("data-open-dialog", ""))
		}
		if r.doc.Find("dialog#leave-wizard a[href='/approvals']").Length() != 1 {
			t.Errorf("%s: no Leave link to the inbox in the dialog", path)
		}
	}
}
