package authweb

import (
	"net/http"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	authscreens "github.com/vardhan/internaltools/internal/kernel/auth/web/screens"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// getWhoCan shows the read-only "Who can…" view (Task 19): pick a DocType
// and one of the rights that apply to it (D-AUTH-11), see the roles and
// users holding it. An inapplicable right is 400 in the ErrorSummary; an
// unknown right is errBadForm.
func (s *Server) getWhoCan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	all := slices.Clone(doctype.All())
	slices.SortStableFunc(all, func(a, b *doctype.DocType) int { return strings.Compare(a.Label, b.Label) })
	page := authscreens.WhoCanPage{DocTypes: all, DocType: r.URL.Query().Get("doctype"), Right: r.URL.Query().Get("right")}
	if page.Right != "" && !slices.Contains(auth.AllRights, auth.Right(page.Right)) {
		s.WriteError(w, r, errBadForm)
		return
	}
	var dt *doctype.DocType
	for _, d := range all {
		if d.Name == page.DocType || (page.DocType == "" && dt == nil) {
			dt = d
		}
	}
	if dt == nil {
		s.WriteError(w, r, auth.ErrNotFound)
		return
	}
	page.DocType = dt.Name
	for _, rt := range auth.AllRights {
		if auth.Applies(dt, rt) {
			page.Rights = append(page.Rights, rt)
		}
	}
	status := http.StatusOK
	if page.Right != "" {
		roles, err := s.a.WhoCan(ctx, dt.Name, auth.Right(page.Right))
		if code, text, ok := userError(err); ok {
			status, page.Error = code, text
		} else if err != nil {
			s.WriteError(w, r, err)
			return
		}
		page.Roles, page.Shown = roles, err == nil
	}
	render(w, r, status, authscreens.WhoCan(page))
}
