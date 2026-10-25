package testkit

import (
	"context"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// Seed inserts the Wave 1 stand-in rows (testkit_employee, testkit_project,
// testkit_site) and returns the fixtures with their database ids filled in.
// It creates no users, rights or sessions; AUTH owns those.
func Seed(t testing.TB, d *db.DB) Fixtures {
	t.Helper()
	fx := canonical()
	ctx := context.Background()
	err := d.Write(ctx, func(tx *db.Tx) error {
		q := tx.SQL()
		empIDs := map[string]int64{}
		for i := range fx.Employees {
			e := &fx.Employees[i]
			var manager any // NULL when there is no manager
			if e.ManagerCode != "" {
				manager = empIDs[e.ManagerCode] // managers come earlier in the fixtures table
			}
			if err := q.QueryRowContext(ctx,
				`INSERT INTO testkit_employee (code, user_id, manager_id, status) VALUES (?, ?, ?, ?) RETURNING id`,
				e.Code, int64(e.UserID), manager, e.Status).Scan(&e.ID); err != nil {
				return err
			}
			empIDs[e.Code] = e.ID
		}
		prjIDs := map[string]int64{}
		for i := range fx.Projects {
			p := &fx.Projects[i]
			if err := q.QueryRowContext(ctx,
				`INSERT INTO testkit_project (code, name, enabled) VALUES (?, ?, 1) RETURNING id`,
				p.Code, p.Name).Scan(&p.ID); err != nil {
				return err
			}
			prjIDs[p.Code] = p.ID
		}
		for i := range fx.Sites {
			s := &fx.Sites[i]
			var project any // NULL when the site has no project
			if s.ProjectCode != "" {
				s.ProjectID = prjIDs[s.ProjectCode]
				project = s.ProjectID
			}
			// The fixtures doc gives sites no name, so the code is the name.
			if err := q.QueryRowContext(ctx,
				`INSERT INTO testkit_site (code, name, branch_code, project_id, enabled) VALUES (?, ?, ?, ?, 1) RETURNING id`,
				s.Code, s.Code, s.BranchCode, project).Scan(&s.ID); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("testkit.Seed: insert stand-in masters: %v", err)
	}
	return fx
}
