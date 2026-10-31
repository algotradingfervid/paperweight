package docscreen

import (
	"net/http"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
)

// writeErr is AUTH's answer to the request's user changing v: rights, not
// created_by (CD-263 G1). Call it after View, outside any tx.
func (d Deps) writeErr(r *http.Request, dt *doctype.DocType, v DocView) error {
	return d.Auth.Can(r.Context(), d.principal(r), dt.Name, "write", &v.Record)
}

// canWrite reports whether the request's user may change draft v.
func (d Deps) canWrite(r *http.Request, dt *doctype.DocType, v DocView) bool {
	return d.writeErr(r, dt, v) == nil
}

// readOnly sends a reader of draft v from a step or line GET to its
// read-only document page and reports whether it did (CD-263 G1).
func (d Deps) readOnly(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, v DocView) bool {
	if d.canWrite(r, dt, v) {
		return false
	}
	http.Redirect(w, r, docURL(dt.Name, v.Info.ID), http.StatusSeeOther) //nolint:gosec // G710: a local /doc/ path from a registered DocType name and a numeric id
	return true
}

// readOnlyPage renders draft v's read-only document page as #main for an
// htmx action whose writer would land on the check page, with the action's
// banner and warnings, and HX-Push-Url to the document page (CD-263 G1).
func (d Deps) readOnlyPage(w http.ResponseWriter, r *http.Request, dt *doctype.DocType, v DocView, banner string, warnings []string) {
	pv, err := d.pageViewOf(r, d.principal(r), dt, v)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	pv.Success, pv.V.Warnings = banner, warnings
	w.Header().Set("HX-Push-Url", docURL(dt.Name, v.Info.ID))
	ui.Render(w, r, http.StatusOK, docPage(pv), docMain(pv))
}
