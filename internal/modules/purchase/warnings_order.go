package purchase

import (
	"context"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/gstcalc"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// OrderWarnings are the PO's warnings, which never refuse a save (CD-280 D1):
// per line, Already received above the quantity (PO spec §3.2); then the
// site and project branches in different states (§27.3); then the
// over-request warnings (§20.5, P6). They are read in one read tx after the
// read check: a PO p can't read is AUTH's refusal, with no warnings.
func (s *Service) OrderWarnings(ctx context.Context, p auth.Principal, poID int64) ([]string, error) {
	var out []string
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.d.Engine.Info(ctx, tx, DocType, poID)
		if err != nil {
			return err
		}
		ref := auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
			EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, &ref); err != nil {
			return err
		}
		q := dbq.New(tx.SQL())
		rows, err := q.OrderLinesForValidate(ctx, poID)
		if err != nil {
			return err
		}
		for _, l := range rows {
			if types.Qty(l.AlreadyReceived) > types.Qty(l.Qty) {
				out = append(out, fmt.Sprintf(msgAlreadyAbove, l.LineNo))
			}
		}
		h, err := q.OrderForValidate(ctx, poID)
		if err != nil {
			return err
		}
		w, err := stateWarning(ctx, tx, h.SiteID, h.ProjectID)
		if err != nil {
			return err
		}
		if w != "" {
			out = append(out, w)
		}
		more, err := requestWarnings(ctx, tx, poID)
		out = append(out, more...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// stateWarning is §27.3's text when the site's branch and the project's
// branch are in different GST states, else "".
func stateWarning(ctx context.Context, tx *db.Tx, siteID, projectID int64) (string, error) {
	if siteID == 0 || projectID == 0 {
		return "", nil
	}
	st, err := sites.Get(ctx, tx, siteID)
	if err != nil {
		return "", err
	}
	pr, err := projects.Get(ctx, tx, projectID)
	if err != nil {
		return "", err
	}
	siteState, err := branches.BranchStateCode(ctx, tx, st.BranchID)
	if err != nil {
		return "", err
	}
	projectState, err := branches.BranchStateCode(ctx, tx, pr.BranchID)
	if err != nil || siteState == projectState {
		return "", err
	}
	sb, err := branches.Get(ctx, tx, st.BranchID)
	if err != nil {
		return "", err
	}
	pb, err := branches.Get(ctx, tx, pr.BranchID)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(msgSiteProjectState, st.Code, sb.Code, stateName(siteState), pr.Code, pb.Code, stateName(projectState)), nil
}

// stateName is the GST state's name from gstcalc's table, the code when
// unknown (CD-254).
func stateName(code string) string {
	if name, ok := gstcalc.StateName(code); ok {
		return name
	}
	return code
}
