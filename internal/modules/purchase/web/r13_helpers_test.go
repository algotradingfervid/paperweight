package purchaseweb

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
)

// r13Empty is a zero-line draft of dt owned by the fixtures' buyer.
func r13Empty(t *testing.T, w *p15dWeb, dt string) int64 {
	t.Helper()
	env := w.env
	values := map[string]string{"site_id": w.id(t, "site", "METRO-SITE"), "project_id": w.id(t, "project", "PRJ-METRO")}
	p := env.Suresh
	if dt == purchase.DocType {
		values["vendor_id"], p = w.id(t, "party", "SHREE"), env.Priya
	}
	res, err := env.Engine.Do(t.Context(), p, dt, 0, doctype.ActionSave, doc.ActionInput{Values: values})
	if err != nil {
		t.Fatalf("empty %s draft: %v", dt, err)
	}
	return res.ID
}

// r13Form is the Details post for an Order with these link ids, version 0
// meaning a new document.
func r13Form(version int, vendor, site, project string) url.Values {
	f := url.Values{"vendor_id": {vendor}, "site_id": {site}, "project_id": {project}, "order_date": {""}, "required_by": {""}}
	if version > 0 {
		f.Set("version", strconv.Itoa(version))
	}
	return f
}

// post sends form as p.
func (w *p15dWeb) post(t *testing.T, p auth.Principal, path string, form url.Values) p15dPage {
	t.Helper()
	return w.do(t, p, http.MethodPost, path, form)
}

// containsAll reports whether s holds every part.
func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

// r13Has reports whether any of list contains sub.
func r13Has(list []string, sub string) bool {
	for _, s := range list {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
