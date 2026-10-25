package party

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
)

// Choice is one party offered by a picker.
type Choice struct {
	ID         int64
	Code, Name string
	Disabled   bool
}

// Search lists the parties marked for role r that match text, through the
// kernel list (read right and scope, search fields, default sort, first
// page). Disabled parties are hidden unless includeDisabled (NUM-123).
func (s *Service) Search(ctx context.Context, p auth.Principal, r Role, text string, includeDisabled bool) ([]Choice, error) {
	dt, ok := doctype.Lookup(DocType)
	if !ok {
		return nil, fmt.Errorf("party: DocType %q is not registered", DocType)
	}
	roleField := "is_vendor"
	if r == Customer {
		roleField = "is_customer"
	}
	filters := []list.Filter{{Field: roleField, Op: list.OpEq, Values: []string{"1"}}}
	if !includeDisabled {
		filters = append(filters, list.DefaultFilters(dt)...)
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
