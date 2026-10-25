package list

import (
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// searchCond ORs a literal "contains" over dt.SearchFields; "" when the
// trimmed search is empty.
func searchCond(dt *doctype.DocType, search string) (string, []any) {
	s := strings.TrimSpace(search)
	if s == "" {
		return "", nil
	}
	var conds []string
	var args []any
	for _, name := range dt.SearchFields {
		i := slices.IndexFunc(dt.Fields, func(f doctype.Field) bool { return f.Name == name })
		if i < 0 {
			continue
		}
		conds = append(conds, column(dt, dt.Fields[i])+` LIKE ? ESCAPE '\'`)
		args = append(args, likePattern(s))
	}
	if len(conds) == 0 {
		return "", nil
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// likePattern is %s% with LIKE's own characters escaped, so s matches literally.
func likePattern(s string) string {
	s = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
	return "%" + s + "%"
}
