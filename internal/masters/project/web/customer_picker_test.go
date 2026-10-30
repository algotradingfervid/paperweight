package projectweb_test

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// pickerSearch is the URL the picker of field searches: the combobox tied to
// its listbox.
func pickerSearch(t *testing.T, r webtest.Response, field string) string {
	t.Helper()
	box := r.Doc.Find(`input[role="combobox"][aria-controls="` + components.FieldID(field) + `-list"]`)
	if box.Length() != 1 {
		t.Fatalf("%d comboboxes for %s; want 1\nbody: %s", box.Length(), field, webtest.Squash(r.Body))
	}
	got, _ := box.Attr("hx-get")
	return got
}

// CD-218: Field.LinkRole makes the customer picker search customers, on the new
// page, on the record's form and on a refused form; a link without one searches
// as before.
func TestScreens_CustomerPickerSearchesCustomers(t *testing.T) {
	const want = "/pick/Party?role=customer"
	w := newWorld(t)
	metro, _ := w.seed(t)
	h := w.h

	refused := h.Post(t, h.Env.Kavya, "/m/project/new", w.form("PRJ-NEW", ""))
	webtest.WantStatus(t, refused, http.StatusUnprocessableEntity)
	for name, page := range map[string]webtest.Response{
		"new":     h.Get(t, h.Env.Kavya, "/m/project/new"),
		"record":  h.Get(t, h.Env.Kavya, "/m/project/"+strconv.FormatInt(metro.ID, 10)),
		"refused": refused,
	} {
		if got := pickerSearch(t, page, "customer_id"); got != want {
			t.Errorf("%s: customer picker searches %q; want %q", name, got, want)
		}
		if got := pickerSearch(t, page, "branch_id"); got != "/pick/Branch" {
			t.Errorf("%s: branch picker searches %q; want /pick/Branch", name, got)
		}
	}
}
