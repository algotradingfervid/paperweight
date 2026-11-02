package mstweb_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: UI v2 C5 (CD-369, §5.5 item 9). A master declares form-only
// fields, which its DocType doesn't, shown with components.Segmented on a
// section and a wizard step. A posted value that isn't an option reaches
// ToInput blank, and Renames moves the errors on a DocType field (here
// item_type) to the form-only field, from ToInput, Validate and the service.

const sizeMsg = "Pick a size."

var sizeChoice = mstweb.Choice{Label: "Size", Required: true, Options: []components.Option{
	{Value: "s", Label: "Small"}, {Value: "l", Label: "Large"},
}}

// onChoice gives the stub screen the form-only field size, with item_type's
// errors renamed to it, and a details section of code and size.
func onChoice(s *screen) {
	s.Choices = map[string]mstweb.Choice{"size": sizeChoice}
	s.Renames = map[string]string{"item_type": "size"}
	s.Sections = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code", "size"}}}
}

// wantSize checks that the form shows size as a segmented control of the two
// options, with checked the chosen one ("" for none).
func wantSize(t *testing.T, r webtest.Response, checked string) {
	t.Helper()
	if got := r.Doc.Find(`form .segmented input[type="radio"][name="size"]`).Length(); got != 2 {
		t.Fatalf("%d segmented size options, want 2\nbody: %s", got, webtest.Squash(r.Body))
	}
	want := []webtest.Choice{{Value: "s", Label: "Small"}, {Value: "l", Label: "Large"}}
	if got := r.Choices("size"); !slices.Equal(got, want) {
		t.Errorf("size options = %v; want %v", got, want)
	}
	got, _ := r.Doc.Find(`input[name="size"]:checked`).Attr("value")
	if got != checked {
		t.Errorf("checked size = %q; want %q", got, checked)
	}
	wantRequiredMark(t, r, "size")
}

// wantRequiredMark checks a required choice's mark: the legend's asterisk and
// aria-required on every option.
func wantRequiredMark(t *testing.T, r webtest.Response, name string) {
	t.Helper()
	opts := r.Doc.Find(`input[name="` + name + `"]`)
	if opts.Filter(`[aria-required="true"]`).Length() != opts.Length() {
		t.Errorf("%s: not every option is aria-required", name)
	}
	if opts.First().Closest("fieldset").Find("legend .field__req").Length() != 1 {
		t.Errorf("%s: the legend has no asterisk", name)
	}
}

// The section shows the stored value checked, and the summary its label.
func TestChoicesC5_SectionRender(t *testing.T) {
	for _, stored := range []string{"s", "l"} {
		h := harness(t, stubService{rec: stubRecord{"code": "X", "size": stored}}, onChoice)
		r := h.Get(t, h.Env.Kavya, record+"/edit/details")
		webtest.WantStatus(t, r, http.StatusOK)
		wantSize(t, r, stored)
		label := map[string]string{"s": "Small", "l": "Large"}[stored]
		if got := webtest.Squash(h.Get(t, h.Env.Kavya, record).Doc.Find("dd").Text()); got != "X"+label {
			t.Errorf("summary values = %q; want %q", got, "X"+label)
		}
	}
}

// A posted option reaches the service; an unknown or missing one reaches it
// blank, and the service's refusal on item_type shows under Size.
func TestChoicesC5_SectionPost(t *testing.T) {
	for _, tc := range []struct{ posted, want string }{{"s", "s"}, {"l", "l"}, {"x", ""}, {"", ""}} {
		var in stubRecord
		h := harness(t, stubService{rec: stubRecord{"code": "X", "size": "l"}, in: &in}, onChoice)
		r := h.Post(t, h.Env.Kavya, record+"/edit/details", url.Values{"version": {"3"}, "code": {"X"}, "size": {tc.posted}})
		webtest.WantStatus(t, r, http.StatusSeeOther)
		if in["size"] != tc.want {
			t.Errorf("posted %q: service got size %q; want %q", tc.posted, in["size"], tc.want)
		}
	}
	h := harness(t, stubService{rec: stubRecord{"code": "X", "size": "l"}, err: &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "item_type", Message: sizeMsg}}}}, onChoice)
	r := h.Post(t, h.Env.Kavya, record+"/edit/details", url.Values{"version": {"3"}, "code": {"X"}, "size": {"x"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "size", sizeMsg)
	wantSize(t, r, "")
}

// ToInput's error on item_type shows under Size, and a refusal the rename has
// no field error to move keeps its own status and text.
func TestChoicesC5_SectionRefusals(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, onChoice, func(s *screen) {
		s.ToInput = func(v map[string]string) (stubRecord, []doctype.FieldError) {
			return v, []doctype.FieldError{{Field: "item_type", Message: sizeMsg}}
		}
	})
	r := h.Post(t, h.Env.Kavya, record+"/edit/details", url.Values{"version": {"3"}, "code": {"X"}, "size": {"s"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "size", sizeMsg)

	for _, tc := range []struct {
		err    error
		status int
	}{
		{&refusal{text: "Whole record refused.", status: http.StatusUnprocessableEntity}, http.StatusUnprocessableEntity},
		{&refusal{text: "Whole record refused.", status: http.StatusConflict, fields: []doctype.FieldError{{Field: "item_type", Message: "Changed."}}}, http.StatusConflict},
	} {
		h := harness(t, stubService{rec: stubRecord{"code": "X"}, err: tc.err}, onChoice)
		r := h.Post(t, h.Env.Kavya, record+"/edit/details", url.Values{"version": {"3"}, "code": {"X"}, "size": {"s"}})
		webtest.WantStatus(t, r, tc.status)
		if tc.status == http.StatusUnprocessableEntity {
			webtest.WantBanner(t, r, "Whole record refused.")
		}
	}
}

// choiceWizard is the stub wizard with size on its details step; Validate
// refuses Small on item_type, and draft gets ToInput's values.
func choiceWizard(draft *map[string]string) func(*screen) {
	return func(s *screen) {
		s.Choices = map[string]mstweb.Choice{"size": sizeChoice}
		s.Renames = map[string]string{"item_type": "size"}
		w := s.Wizard.(mstweb.CreateWizard[stubDraft])
		w.Steps = []docscreen.FormStep{
			{Slug: "details", Label: "Details", Fields: []string{"code", "size"}},
			{Slug: "more", Label: "More", Fields: []string{"name"}},
		}
		w.ToInput = func(v map[string]string) (stubDraft, []doctype.FieldError) {
			*draft = v
			return stubDraft{code: v["size"]}, nil
		}
		w.Validate = func(d stubDraft) (stubDraft, error) {
			if d.code == "s" {
				return d, &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "item_type", Message: "Small is out."}}}
			}
			return d, nil
		}
		s.Wizard = w
	}
}

// The step shows size; Validate's error and the save's refusal on item_type
// show under it, an unknown value is held blank, and Check shows the
// chosen option's label.
func TestChoicesC5_Wizard(t *testing.T) {
	var draft map[string]string
	h := wizardHarness(t, func(_ context.Context, _ auth.Principal, d stubDraft) (mstkit.Saved, error) {
		return mstkit.Saved{}, &mstkit.ValidationError{Fields: []doctype.FieldError{{Field: "item_type", Message: sizeMsg}}}
	}, choiceWizard(&draft))
	b := h.Browser(h.Env.Kavya)
	r := b.Get(t, wizardFirst)
	webtest.WantStatus(t, r, http.StatusOK)
	wantSize(t, r, "")

	// Required only marks the choice: a blank one passes Next, as no rule
	// refuses it, and isn't refused for being blank.
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)

	r = b.Post(t, wizardFirst, url.Values{"code": {"X"}, "size": {"s"}})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "size", "Small is out.")
	wantSize(t, r, "s")

	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}, "size": {"x"}}), http.StatusSeeOther)
	if v, ok := draft["size"]; !ok || v != "" {
		t.Errorf("unknown size reached ToInput as %q (held %v); want blank", v, ok)
	}
	wantSize(t, b.Get(t, wizardFirst), "")

	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}, "size": {"l"}}), http.StatusSeeOther)
	webtest.WantStatus(t, b.Post(t, base+"/new/more", url.Values{"name": {"N"}}), http.StatusSeeOther)
	check := b.Get(t, wizardCheck)
	if got := webtest.Squash(check.Doc.Find("dd").Text()); got != "XLargeN" {
		t.Errorf("check values = %q; want XLargeN", got)
	}
	r = b.Post(t, wizardCheck, url.Values{})
	webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
	webtest.WantFieldError(t, r, "size", sizeMsg)
}
