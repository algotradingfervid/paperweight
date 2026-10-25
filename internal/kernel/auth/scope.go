package auth

import (
	"context"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Scope returns the filter p's list of doctype needs for right r (CD-16):
// columns are qualified with the DocType's table, lines are reached with
// EXISTS on parent_id, and SQL "" means no filter. A missing grant for r or
// read, or a right that does not apply to the DocType, is a *DeniedError.
func (a *Auth) Scope(ctx context.Context, p Principal, doctypeName string, r Right) (Predicate, error) {
	dt, err := lookup(doctypeName)
	if err != nil {
		return Predicate{}, err
	}
	if !Applies(dt, r) {
		return Predicate{}, denied(dt, r)
	}
	if slices.Contains(p.Roles, RoleAdministrator) {
		return Predicate{}, nil
	}
	readScope, err := a.grant(ctx, p.Roles, dt.Name, RightRead)
	if err != nil {
		return Predicate{}, err
	}
	rScope, err := a.grant(ctx, p.Roles, dt.Name, r)
	if err != nil {
		return Predicate{}, err
	}
	if readScope == "" || rScope == "" {
		return Predicate{}, denied(dt, r)
	}
	scopes := []Scope{readScope}
	if rScope != readScope {
		scopes = append(scopes, rScope)
	}
	scoped := slices.ContainsFunc(scopes, func(s Scope) bool { return s == ScopeOwn || s == ScopeTeam })

	t := dt.Table
	var clauses []string
	var args []any
	err = a.db.Read(ctx, func(tx *db.Tx) error {
		var me int64
		var reports []int64
		var found bool
		if scoped && dt.EmployeeField != "" {
			var err error
			if me, reports, found, err = a.employee(ctx, tx, p.ID); err != nil {
				return err
			}
		}
		for _, s := range scopes {
			if s != ScopeOwn && s != ScopeTeam {
				continue
			}
			parts := []string{col(t, ownerColumn(dt)) + " = ?"}
			args = append(args, int64(p.ID))
			if found {
				parts = append(parts, col(t, dt.EmployeeField)+" = ?")
				args = append(args, me)
				if s == ScopeTeam && len(reports) > 0 {
					parts = append(parts, col(t, dt.EmployeeField)+" IN "+placeholders(len(reports)))
					args = appendIDs(args, reports)
				}
			}
			clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
		}

		allowed, err := restrictions(ctx, dbq.New(tx.SQL()), p.ID)
		if err != nil {
			return err
		}
		for _, d := range []Dimension{DimProject, DimSite} {
			ids, restricted := allowed[d]
			header, lines := dimColumns(dt, d)
			if !restricted || len(header)+len(lines) == 0 {
				continue
			}
			for _, h := range header {
				clauses = append(clauses, "("+col(t, h)+" IS NULL OR "+col(t, h)+" IN "+placeholders(len(ids))+")")
				args = appendIDs(args, ids)
			}
			for _, l := range lines {
				clauses = append(clauses, "NOT EXISTS ("+linesFrom(dt.LinesTable, t)+" AND "+
					col(dt.LinesTable, l)+" IS NOT NULL AND "+col(dt.LinesTable, l)+" NOT IN "+placeholders(len(ids))+")")
				args = appendIDs(args, ids)
			}
			var present []string
			for _, h := range header {
				present = append(present, col(t, h)+" IS NOT NULL")
			}
			if len(lines) > 0 {
				var lp []string
				for _, l := range lines {
					lp = append(lp, col(dt.LinesTable, l)+" IS NOT NULL")
				}
				present = append(present, "EXISTS ("+linesFrom(dt.LinesTable, t)+" AND ("+strings.Join(lp, " OR ")+"))")
			}
			clauses = append(clauses, "("+strings.Join(present, " OR ")+")")
		}
		return nil
	})
	if err != nil {
		return Predicate{}, err
	}
	if len(clauses) == 0 {
		return Predicate{}, nil
	}
	return Predicate{SQL: strings.Join(clauses, " AND "), Args: args}, nil
}

// col returns "table"."column".
func col(table, column string) string {
	return `"` + table + `"."` + column + `"`
}

// linesFrom opens a subquery over lines rows that belong to the current row
// of table.
func linesFrom(lines, table string) string {
	return `SELECT 1 FROM "` + lines + `" WHERE ` + col(lines, "parent_id") + " = " + col(table, "id")
}

// placeholders returns "(?,?,…)" with n marks.
func placeholders(n int) string {
	return "(" + strings.TrimSuffix(strings.Repeat("?,", n), ",") + ")"
}

func appendIDs(args []any, ids []int64) []any {
	for _, id := range ids {
		args = append(args, id)
	}
	return args
}
