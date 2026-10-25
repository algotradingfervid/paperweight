package auth

import (
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Dimension is a restriction dimension.
type Dimension string

const (
	DimProject Dimension = "project"
	DimSite    Dimension = "site"
)

// Applies reports whether right r applies to dt at all. Masters are not
// Submittable (CD-21), so submit, cancel, amend and approve never apply to them.
func Applies(dt *doctype.DocType, r Right) bool {
	switch r {
	case RightSubmit, RightCancel, RightAmend:
		return dt.Submittable
	case RightApprove:
		return dt.Approvable
	case RightImport:
		return dt.Importable
	}
	return true
}

// ScopeOptions lists the scopes a right on dt can have; team needs an
// employee field (CD-11).
func ScopeOptions(dt *doctype.DocType) []Scope {
	if dt.EmployeeField != "" {
		return []Scope{ScopeOwn, ScopeTeam, ScopeAll}
	}
	return []Scope{ScopeOwn, ScopeAll}
}

// ownerColumn is the column own scope compares with the user (§4.3).
func ownerColumn(dt *doctype.DocType) string {
	if dt.OwnerField != "" {
		return dt.OwnerField
	}
	return "created_by"
}

// dimFields returns dt's declared columns for dimension d (CD-09).
func dimFields(dt *doctype.DocType, d Dimension) []string {
	if d == DimProject {
		return dt.ProjectFields
	}
	return dt.SiteFields
}

// dimColumns splits dt's columns for d into header columns and line columns
// (declared as "lines.<col>", CD-09).
func dimColumns(dt *doctype.DocType, d Dimension) (header, lines []string) {
	for _, f := range dimFields(dt, d) {
		if col, ok := strings.CutPrefix(f, "lines."); ok {
			lines = append(lines, col)
		} else {
			header = append(header, f)
		}
	}
	return header, lines
}

// masterFor returns the registered master that holds dimension d's values:
// the one whose own id is its d column (CD-64).
func masterFor(d Dimension) (*doctype.DocType, bool) {
	for _, dt := range doctype.All() {
		if dt.Master != nil && slices.Contains(dimFields(dt, d), "id") {
			return dt, true
		}
	}
	return nil, false
}
