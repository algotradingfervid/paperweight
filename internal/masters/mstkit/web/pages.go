package mstweb

import (
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// HandlerFunc answers a request or returns the error the kit renders.
type HandlerFunc func(w http.ResponseWriter, r *http.Request, p auth.Principal) error

// Form is a form's state: its values, its version and what the last save was
// refused with.
type Form = form

// Pages is what Screen.Mount gets of the kit, for routes of the master's own.
type Pages struct {
	// Handle runs fn as the signed-in user and renders its error.
	Handle func(fn HandlerFunc) http.HandlerFunc
	// Input is fd's control at path, as the Screen's own forms draw it.
	Input func(fd doctype.Field, path string, f Form) templ.Component
	// Refused shows err on record id's page, or returns it when the page can't.
	Refused func(w http.ResponseWriter, r *http.Request, p auth.Principal, id int64, err error) error
}

func (h *handlers[R, In]) pages() Pages {
	return Pages{
		Handle: h.handle,
		Input:  h.input,
		Refused: func(w http.ResponseWriter, r *http.Request, p auth.Principal, id int64, err error) error {
			return h.refused(w, r, p, id, form{}, err)
		},
	}
}

// Refused is f with err shown: errors on names by their fields and the rest in
// the banner, a stale version with Reload. It returns the status, and false for
// an err a form can't show (403, 404, 503).
func (f form) Refused(err error, names []string) (Form, int, bool) {
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != http.StatusConflict && he.Status() != http.StatusUnprocessableEntity {
		return f, 0, false
	}
	errs := he.FieldErrors()
	f.Errs = stepErrors(docscreen.FormStep{Fields: names}, errs)
	f.Problem = strings.Join(messages(slices.DeleteFunc(slices.Clone(errs), func(e doctype.FieldError) bool {
		return slices.Contains(names, e.Field)
	})), " ")
	if len(f.Errs) == 0 && f.Problem == "" {
		f.Problem = he.Error()
	}
	f.Reload = he.Status() == http.StatusConflict
	return f, he.Status(), true
}

// SubForm is a form of the master's own on the record page's template: an
// address under its party. ID 0 posts no version. Confirm shows the delete
// confirm step instead, which posts to Action + "/delete" and cancels to Back:
// Action's form may need a right the step doesn't.
type SubForm struct {
	ID            int64
	Title, Action string
	Back          components.Link
	Form          Form
	Inputs        []templ.Component
	Confirm       bool
}

// RenderSubForm renders s with status.
func RenderSubForm(w http.ResponseWriter, r *http.Request, status int, s SubForm) {
	ui.Render(w, r, status, recordPageCancelTo(recordView{
		ID: s.ID, Title: s.Title, Action: s.Action, Back: s.Back, CanWrite: true, Width: layout.Form,
		Form: s.Form, Inputs: s.Inputs, Confirm: s.Confirm,
	}, s.Back.Href), nil)
}
