package employee

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
)

// Choice is one employee offered by a picker.
type Choice struct {
	ID         int64
	Code, Name string
	Disabled   bool
}

// Search lists the employees that match text, through the kernel list (read
// right and scope, search fields, default sort, first page). Left employees
// are disabled, so they are hidden unless includeDisabled (MST-120).
func (s *Service) Search(ctx context.Context, p auth.Principal, text string, includeDisabled bool) ([]Choice, error) {
	dt, _ := doctype.Lookup(DocType) // this package's init registers it
	var filters []list.Filter
	if !includeDisabled {
		filters = list.DefaultFilters(dt)
	}
	ls := list.Service{DB: s.env.DB, Auth: s.env.Auth}
	res, err := ls.List(ctx, p, dt, list.Query{Filters: filters, Search: text, Page: 1})
	if err != nil {
		return nil, err
	}
	out := make([]Choice, len(res.Rows))
	for i, row := range res.Rows {
		code, _ := row.Values["code"].(string)
		name, _ := row.Values["name"].(string)
		disabled, _ := row.Values["disabled"].(bool)
		out[i] = Choice{ID: row.ID, Code: code, Name: name, Disabled: disabled}
	}
	return out, nil
}
