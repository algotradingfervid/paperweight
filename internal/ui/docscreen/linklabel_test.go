package docscreen_test

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const llChosen = "7"

func llField() doctype.Field {
	return doctype.Field{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: docscreentest.Supplier}
}

// llRender is the Link's picker text for the posted id llChosen.
func llRender(t *testing.T, d docscreen.Deps) string {
	t.Helper()
	d.DB = &docscreentest.Reader{}
	return uitest.Render(t, docscreen.FieldInput(llField(), "f-supplier", llChosen, "", d)).Text()
}

func llCheck(err error) func(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error {
	return func(context.Context, *db.Tx, auth.Principal, *doctype.DocType, int64) error { return err }
}

// CD-233: a posted id shows its label only when LinkRead allows it. With no
// LinkRead it fails closed; a stored value keeps its label with none.
func TestLinkInput_PostedLabel(t *testing.T) {
	stored := map[string]string{"supplier_id": llChosen}
	docscreentest.RegisterSample()
	docscreentest.SetMaster(docscreentest.Supplier, 7, doctype.MasterInfo{Code: "SUP-7", Name: "Hidden Supplier"})
	const label = "SUP-7"
	for name, c := range map[string]struct {
		d    docscreen.Deps
		show bool
	}{
		"no hook, posted":        {docscreen.Deps{}, false},
		"no hook, stored":        {docscreen.Deps{}.WithStored(stored), true},
		"no hook, changed value": {docscreen.Deps{}.WithStored(map[string]string{"supplier_id": "8"}), false},
		"readable":               {docscreen.Deps{LinkRead: llCheck(nil)}, true},
		"hidden":                 {docscreen.Deps{LinkRead: llCheck(doctype.ErrNotFound)}, false},
		"no right":               {docscreen.Deps{LinkRead: llCheck(&auth.DeniedError{})}, false},
		"hidden, stored":         {docscreen.Deps{LinkRead: llCheck(doctype.ErrNotFound)}.WithStored(stored), true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := llRender(t, c.d); strings.Contains(got, label) != c.show {
				t.Errorf("show = %v, want %v: %q", !c.show, c.show, got)
			}
		})
	}
}

func TestLinkInput_ReadFailureStopsTheRender(t *testing.T) {
	broken := errors.New("disk on fire")
	d := docscreen.Deps{DB: &docscreentest.Reader{}, LinkRead: llCheck(broken)}
	var b strings.Builder
	if err := docscreen.FieldInput(llField(), "f-supplier", llChosen, "", d).Render(t.Context(), &b); !errors.Is(err, broken) {
		t.Errorf("err = %v, want %v", err, broken)
	}
}

// A draft's stored links keep their labels with no LinkRead, on the step and on
// the line page; a value the POST changed shows the raw id (CD-233).
func TestLinkInput_DraftForms(t *testing.T) {
	e := newFormEnv(t, allRights...)
	e.seed(doctype.StateDraft)
	if page := uitest.Doc(t, e.get(t, doc42+"/edit/details")).Text(); !strings.Contains(page, "SHREE") {
		t.Errorf("the stored supplier lost its label: %q", page)
	}
	if page := uitest.Doc(t, e.get(t, doc42+"/lines/1")).Text(); !strings.Contains(page, "MS-ANGLE") {
		t.Errorf("the stored item lost its label: %q", page)
	}

	refused := url.Values{"version": {"5"}, "supplier_id": {"3"}, "order_date": {"not a date"}}
	docscreentest.SetMaster(docscreentest.Supplier, 3, doctype.MasterInfo{Code: "OTHER", Name: "Other Supplier"})
	res := e.post(t, doc42+"/edit/details", refused)
	if res.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", res.Code)
	}
	if page := uitest.Doc(t, res).Text(); strings.Contains(page, "OTHER") {
		t.Errorf("a posted id got a label with no LinkRead: %q", page)
	}
}
