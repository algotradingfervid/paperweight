//go:build e2e

package e2e

import (
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// M0-50 (CD-323 (1)): a module Save refused with CD-240's 403 on a field
// redraws the wizard in place in the browser, with the error summary linking
// to the field and the typed value kept. htmx swaps only 422 and 409
// (layout.go's htmx-config), so a 403 redraw would leave the page unchanged
// and every wait below would time out.

const (
	m50Typed   = "M50 typed remarks"
	m50NotHers = "You can't use this Purchase Order. It isn't available to you." // CD-240
	m50Marker  = "window.m50Marker"                                              // survives an htmx swap, not a page load
)

// m50Refusal is a module's 403 putting AUTH's refusal on remarks.
type m50Refusal struct{ err error }

func (e m50Refusal) Error() string { return e.err.Error() }
func (e m50Refusal) Unwrap() error { return e.err }
func (e m50Refusal) Status() int   { return http.StatusForbidden }
func (e m50Refusal) FieldErrors() []doctype.FieldError {
	return []doctype.FieldError{{Field: "remarks", Message: e.err.Error()}}
}

func TestE2E_Forbid50_SaveRefusedOnField(t *testing.T) {
	var other atomic.Int64 // the Order Priya can't read, set once the app runs
	save := func(c docscreen.Ctx) (docscreen.Save, error) {
		if err := c.Deps.Auth.CheckLink(c.Req.Context(), c.Principal, fixture.Order, other.Load()); err != nil {
			return docscreen.Save{}, m50Refusal{err: err}
		}
		return docscreen.Save{}, nil
	}
	h := StartModules(t, map[string]docscreen.Module{
		fixture.Order: {Steps: map[string]docscreen.Step{"details": {Save: save}}},
	})
	id := func(m map[string]int64, code string) string { return strconv.FormatInt(m[code], 10) }
	techpark := h.Do("admin@vardhan.test", fixture.Order, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": id(h.M.Party, "SHREE"), "project_id": id(h.M.Project, "PRJ-TECHPARK"), "order_date": "2026-10-24",
	}})
	other.Store(techpark.ID)
	own := h.Do(priya, fixture.Order, 0, doctype.ActionSave, doc.ActionInput{Values: map[string]string{
		"supplier_id": id(h.M.Party, "SHREE"), "project_id": id(h.M.Project, "PRJ-METRO"), "order_date": "2026-10-24",
	}})

	for _, c := range []struct{ name, path string }{
		{"new", "/doc/" + fixture.Order + "/new"},
		{"step", DocPath(fixture.Order, own.ID) + "/edit/details"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := h.SignIn(priya, desktop)
			p.Open(c.path)
			p.WaitText("Step 1 of 3")
			p.Fill("remarks", m50Typed)
			p.Do("mark the page", chromedp.Evaluate[chromedp.Void](m50Marker+" = true"))
			p.Press(saveContinue)
			p.Until("the error summary links to remarks",
				`!!document.querySelector('.error-summary a[href="#f-remarks"]')`)
			p.WaitText(m50NotHers)
			p.Until("the wizard redrew in place", m50Marker+` === true && location.pathname === `+strconv.Quote(c.path))
			if got := Eval[string](p, `document.getElementById("f-remarks").value`); got != m50Typed {
				t.Errorf("remarks after the refusal = %q, want the typed %q", got, m50Typed)
			}
		})
	}
}
