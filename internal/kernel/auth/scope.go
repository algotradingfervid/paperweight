package auth

import (
	"context"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth/dbq"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
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
	acc := Access{User: p.ID, Read: readScope, Right: rScope}
	if readScope != "" && rScope != "" {
		scoped := slices.ContainsFunc([]Scope{readScope, rScope}, func(s Scope) bool { return s == ScopeOwn || s == ScopeTeam })
		if err := a.db.Read(ctx, func(tx *db.Tx) error {
			var err error
			if scoped && dt.EmployeeField != "" {
				if acc.Employee, acc.Reports, acc.HasEmployee, err = a.employee(ctx, tx, p.ID); err != nil {
					return err
				}
			}
			acc.Allowed, err = restrictions(ctx, dbq.New(tx.SQL()), p.ID)
			return err
		}); err != nil {
			return Predicate{}, err
		}
	}
	return acc.Scope(dt, r)
}

// Scope is auth.Service.Scope after the Applies and Administrator checks:
// a missing grant for read or r is a *DeniedError; otherwise the filter for
// the own and team scopes and the restrictions ("" = no filter).
func (a Access) Scope(dt *doctype.DocType, r Right) (Predicate, error) {
	if a.Read == "" || a.Right == "" {
		return Predicate{}, denied(dt, r)
	}
	scopes := []Scope{a.Read}
	if a.Right != a.Read {
		scopes = append(scopes, a.Right)
	}
	t := dt.Table
	var clauses []string
	var args []any
	for _, s := range scopes {
		if s != ScopeOwn && s != ScopeTeam {
			continue
		}
		parts := []string{col(t, ownerColumn(dt)) + " = ?"}
		args = append(args, int64(a.User))
		if a.HasEmployee && dt.EmployeeField != "" {
			parts = append(parts, col(t, dt.EmployeeField)+" = ?")
			args = append(args, a.Employee)
			if s == ScopeTeam && len(a.Reports) > 0 {
				parts = append(parts, col(t, dt.EmployeeField)+" IN "+placeholders(len(a.Reports)))
				args = appendIDs(args, a.Reports)
			}
		}
		clauses = append(clauses, "("+strings.Join(parts, " OR ")+")")
	}
	for _, d := range []Dimension{DimProject, DimSite} {
		ids, restricted := a.Allowed[d]
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
