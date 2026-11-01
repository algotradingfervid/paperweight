package expenseweb

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// more's two rules the pages can't isolate (MORE, AD-11): an error alone
// opens the group (every real error on these optional fields comes with a
// typed value), and a field that isn't Field.More gets its input bare.
func TestMoreExp_Helper(t *testing.T) {
	dt, ok := doctype.Lookup(expense.ClaimDocType)
	if !ok {
		t.Fatal("expense.Claim isn't registered")
	}
	c := docscreen.Ctx{Req: httptest.NewRequest("GET", "/", nil), DT: dt}
	in := templ.Raw(`<input name="probe">`)
	render := func(comp templ.Component) *goquery.Document {
		t.Helper()
		var buf bytes.Buffer
		if err := comp.Render(t.Context(), &buf); err != nil {
			t.Fatal(err)
		}
		d, err := goquery.NewDocumentFromReader(&buf)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	g := render(more(c, "remarks", "", "Wrong.", in)).Find("details")
	if _, open := g.Attr("open"); g.Length() != 1 || !open || g.Find(`[name="probe"]`).Length() != 1 {
		t.Errorf("an error on a blank Remarks: %d groups, open %v; want one open group holding the input", g.Length(), open)
	}
	d := render(more(c, "purpose", "Visit", "", in))
	if d.Find("details").Length() != 0 || d.Find(`[name="probe"]`).Length() != 1 || strings.Contains(d.Text(), "More details") {
		t.Error("Purpose isn't Field.More, but its input is in a group")
	}
}
