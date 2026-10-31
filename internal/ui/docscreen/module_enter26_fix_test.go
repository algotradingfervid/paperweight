package docscreen_test

import (
	"net/http"
	"testing"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// M0-26 R4 (CD-286), AD ruling: a Body that returns no component gets the
// generic body, so its step form has no Enter copy, on the Items step and on
// a header step, in the full page and the htmx frame.
func TestModule_EnterPrimaryNilBody(t *testing.T) {
	e, _, id := m02Env(t)
	base := m02URL(id)
	none := func(docscreen.Ctx, docscreen.StepState) (templ.Component, error) { return nil, nil }
	m02Mount(e, docscreen.Module{Steps: map[string]docscreen.Step{"items": {Body: none}, "terms": {Body: none}}})
	for _, hx := range []bool{false, true} {
		for _, u := range []string{base + "/edit/items", base + "/edit/terms"} {
			what := m17What("GET "+u+" with a Body giving no component", hx)
			m26WantNoCopy(t, m02Page(t, e.get(t, u, m26Hdr[hx]...), http.StatusOK, what), "Save and continue", what)
		}
	}
}

// M0-26 R4 (CD-286): EnterSubmit copies the primary's Attrs except form (it
// sits in the form it submits), and its Name and Value.
func TestEnterSubmit_CopiesAttrsExceptForm(t *testing.T) {
	d := uitest.Render(t, components.EnterSubmit(components.ButtonProps{
		Label: "Add selected", Name: "nav", Value: "stay",
		Attrs: templ.Attributes{"form": "wizard-form", "data-m26": "x"},
	}))
	b := d.Find("button.enter-submit")
	if b.Length() != 1 {
		t.Fatalf("%d enter-submit buttons, want 1", b.Length())
	}
	if _, ok := b.Attr("form"); ok {
		t.Errorf("the Enter copy has a form attribute, want the primary's Attrs without it")
	}
	if got := m02Attr(b, "data-m26"); got != "x" {
		t.Errorf("the Enter copy's data-m26 = %q, want the primary's x", got)
	}
	if n, v := m02Attr(b, "name"), m02Attr(b, "value"); n != "nav" || v != "stay" {
		t.Errorf("the Enter copy posts %s=%s, want nav=stay", n, v)
	}
}
