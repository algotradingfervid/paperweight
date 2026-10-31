package expense

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense/dbq"
)

// A line's project from its site (CD-337 (5)), PO's site-and-project rule
// per line: a site on a project needs that project, and a blank project
// defaults to it. A site with no project, or no site, keeps the line's
// project. It reads the site master, so like the snapshots it runs only
// while the document is a draft, before the line snapshots are written. The
// queries return only the lines to default (blank project) or refuse
// (another project).

// claimLineProjects applies the rule to claim id's lines.
func claimLineProjects(ctx context.Context, q *dbq.Queries, id int64) ([]doctype.FieldError, error) {
	rows, err := q.E13ClaimLineSites(ctx, id)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	for _, r := range rows {
		if r.ProjectID.Valid {
			errs = append(errs, siteProjectError(r.LineNo, r.SiteCode, r.ProjectCode))
			continue
		}
		err := q.E13SetClaimLineProject(ctx, dbq.E13SetClaimLineProjectParams{ProjectID: r.SiteProjectID, ID: r.ID})
		if err != nil {
			return nil, err
		}
	}
	return errs, nil
}

// advanceLineProjects applies the rule to advance id's lines.
func advanceLineProjects(ctx context.Context, q *dbq.Queries, id int64) ([]doctype.FieldError, error) {
	rows, err := q.E13AdvanceLineSites(ctx, id)
	if err != nil {
		return nil, err
	}
	var errs []doctype.FieldError
	for _, r := range rows {
		if r.ProjectID.Valid {
			errs = append(errs, siteProjectError(r.LineNo, r.SiteCode, r.ProjectCode))
			continue
		}
		err := q.E13SetAdvanceLineProject(ctx, dbq.E13SetAdvanceLineProjectParams{ProjectID: r.SiteProjectID, ID: r.ID})
		if err != nil {
			return nil, err
		}
	}
	return errs, nil
}

// siteProjectError refuses line n's project: its site siteCode is on
// project projectCode.
func siteProjectError(n int64, siteCode, projectCode string) doctype.FieldError {
	return doctype.FieldError{Field: "project_id", Line: int(n), Message: fmt.Sprintf(msgLineSiteProject, n, siteCode, projectCode)}
}
