package employee

import (
	"context"
	"strings"

	"github.com/vardhan/internaltools/internal/masters/employee/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// checkLoop refuses managerID for employee id (saved with code) when the
// chain above managerID reaches id (MST-114). It walks up inside tx, at most
// count(employee) steps, building the path for the message.
func (s *Service) checkLoop(ctx context.Context, tx *db.Tx, id int64, code string, managerID int64) error {
	n, err := dbq.New(tx.SQL()).CountEmployees(ctx)
	if err != nil {
		return err
	}
	path := []string{code}
	cur := managerID
	for range n {
		if cur == id {
			path = append(path, code)
			return fieldErr("manager_id", "This would make a reporting loop ("+strings.Join(path, " → ")+"). Pick another manager.")
		}
		e, err := s.Get(ctx, tx, cur)
		if err != nil {
			return err
		}
		path = append(path, e.Code)
		if e.ManagerID == 0 {
			return nil
		}
		cur = e.ManagerID
	}
	return nil
}

// ManagerChain is employeeID's managers, direct manager first. MST-114 keeps
// it acyclic; the walk stops after count(employee) steps regardless.
func (s *Service) ManagerChain(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	n, err := dbq.New(tx.SQL()).CountEmployees(ctx)
	if err != nil {
		return nil, err
	}
	chain := []int64{}
	e, err := s.Get(ctx, tx, employeeID)
	if err != nil {
		return nil, err
	}
	for range n {
		if e.ManagerID == 0 {
			break
		}
		chain = append(chain, e.ManagerID)
		if e, err = s.Get(ctx, tx, e.ManagerID); err != nil {
			return nil, err
		}
	}
	return chain, nil
}

// DirectReports is the active employees whose manager is employeeID, by code.
func (s *Service) DirectReports(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	reps, err := dbq.New(tx.SQL()).ActiveReports(ctx, nullID(employeeID))
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(reps))
	for _, r := range reps {
		ids = append(ids, r.ID)
	}
	return ids, nil
}
