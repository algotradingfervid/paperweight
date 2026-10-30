package docscreen_test

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// packType has one editable Factor field: no DocType linked into this test
// binary has one (CD-65 prefix).
const packType = "docscreentest.Pack"

var registerPackOnce sync.Once

func newPackEnv(t *testing.T) *docEnv {
	t.Helper()
	registerPackOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: packType, Label: "Pack", PluralLabel: "Packs", Table: "docscreentest_pack", Submittable: true,
			Fields: []doctype.Field{{Name: "factor", Label: "Conversion factor", Type: doctype.Factor}},
		})
	})
	e := newDocEnv(t)
	for _, r := range allRights {
		e.az.Grant("Buyer", packType, r, auth.ScopeAll)
	}
	return e
}

func TestFieldInput_Factor(t *testing.T) {
	f := doctype.Field{Name: "factor", Label: "Conversion factor", Type: doctype.Factor}
	in := uitest.Render(t, docscreen.FieldInput(f, "f-factor", "1.5", "", docscreen.Deps{})).Find("input#f-factor")
	if in.Length() != 1 {
		t.Fatal("Factor is not one input#f-factor")
	}
	if hx, _ := in.Attr("hx-post"); hx != "/ui/validate/factor" {
		t.Errorf("hx-post = %q, want /ui/validate/factor (CD-48, CD-178)", hx)
	}
	if !in.HasClass("num") {
		t.Error("Factor input lacks class num (CD-169)")
	}
	if m, _ := in.Attr("inputmode"); m != "decimal" {
		t.Errorf("inputmode = %q, want decimal", m)
	}
	if v, _ := in.Attr("value"); v != "1.5" {
		t.Errorf("value = %q, want 1.5", v)
	}
}

func TestDocForm_FactorRoundTrip(t *testing.T) {
	const edit = "/doc/" + packType + "/70/edit/details"
	input := func(t *testing.T, e *docEnv) string {
		t.Helper()
		rec := e.get(t, edit)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, body %.300s", edit, rec.Code, rec.Body)
		}
		in := uitest.Doc(t, rec).Find(`input[name="factor"][hx-post="/ui/validate/factor"]`)
		if in.Length() != 1 {
			t.Fatal("the form has no Factor input checked by /ui/validate/factor")
		}
		v, _ := in.Attr("value")
		return v
	}

	e := newPackEnv(t)
	docscreentest.Seed(e.docs, draftInfo(packType, "Pack", 70), doc.Record{Version: 3, Values: map[string]string{"factor": "1.5"}})
	if got := input(t, e); got != "1.5" {
		t.Errorf("stored 1.5 shows as %q", got)
	}

	// The worst case, typed with its grouping, is saved in plain form...
	e.docs.SetResult(packType, 70, doctype.ActionSave, doc.Result{ID: 70, State: doctype.StateDraft, Version: 4}, nil)
	e.post(t, edit, url.Values{"factor": {"9,99,999.999999999"}}, "HX-Request", "true")
	calls := e.docs.Calls()
	if len(calls) != 1 || calls[0].Input.Values["factor"] != "999999.999999999" {
		t.Fatalf("engine calls = %+v, want one save with factor 999999.999999999", calls)
	}

	// ...and shows again in the same form.
	saved := newPackEnv(t)
	docscreentest.Seed(saved.docs, draftInfo(packType, "Pack", 70), doc.Record{Version: 4, Values: calls[0].Input.Values})
	if got := input(t, saved); got != "9,99,999.999999999" {
		t.Errorf("stored %s shows as %q", calls[0].Input.Values["factor"], got)
	}
}

func TestDocForm_FactorRefusedKeepsTypedValue(t *testing.T) {
	const edit = "/doc/" + packType + "/71/edit/details"
	e := newPackEnv(t)
	docscreentest.Seed(e.docs, draftInfo(packType, "Pack", 71), doc.Record{Version: 3, Values: map[string]string{"factor": "1.5"}})

	rec := e.post(t, edit, url.Values{"factor": {"1.1234567891"}}, "HX-Request", "true")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %.300s", rec.Code, rec.Body)
	}
	d := uitest.Doc(t, rec)
	if !strings.Contains(norm(d.Text()), "Conversion factor can have at most 9 decimal places.") {
		t.Error("the form does not show ParseFactor's message")
	}
	if v, _ := d.Find(`input[name="factor"]`).Attr("value"); v != "1.1234567891" {
		t.Errorf("typed value became %q", v)
	}
	if calls := e.docs.Calls(); len(calls) != 0 {
		t.Errorf("engine calls = %+v, want none", calls)
	}
}
