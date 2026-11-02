package expenseweb

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-381: on the paid pages only a stale version is the ConflictBanner; a
// refused 409 is a danger banner with no Reload and no keep-your-entries line.
func TestPaid_Refused409StaysDanger(t *testing.T) {
	const msg = "Can't do this now."
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/modules/expense/advances/7/unpaid", nil)
	pg := expense.PaidPage{Label: "EA/26-27/00001"}
	render := func(err error) string {
		t.Helper()
		var b bytes.Buffer
		if err := paidBanners(paidScreenOf(r, pg, paidView{AdvanceID: 7, Version: 2}, err)).Render(t.Context(), &b); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	got := render(doc.Refused(msg))
	if !strings.Contains(got, "banner--danger") || strings.Contains(got, "banner--warning") || strings.Contains(got, "Reload") || strings.Contains(got, "Your entries stay") {
		t.Errorf("refused 409 = %s, want only the danger banner", got)
	}
	got = render(&doc.Error{Kind: doc.KindStale, Message: msg})
	if !strings.Contains(got, "banner--warning") || !strings.Contains(got, "Reload EA/26-27/00001") {
		t.Errorf("stale = %s, want the ConflictBanner", got)
	}
}
