package authfake

import (
	"cmp"
	"context"
	"slices"
	"sync"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

var _ auth.Directory = (*Directory)(nil)

// Directory is the one shared auth.Directory fake (CD-13, CD-56). The tx
// argument of every method may be nil.
type Directory struct {
	mu            sync.Mutex
	employees     map[int64]bool // every employee a seeding helper has named
	employeeOf    map[auth.UserID]int64
	userOf        map[int64]auth.UserID
	manager       map[int64]int64
	left          map[int64]bool
	employeeCodes map[int64]string
	projectCodes  map[int64]string
	siteCodes     map[int64]string
}

func NewDirectory() *Directory {
	return &Directory{
		employees:     map[int64]bool{},
		employeeOf:    map[auth.UserID]int64{},
		userOf:        map[int64]auth.UserID{},
		manager:       map[int64]int64{},
		left:          map[int64]bool{},
		employeeCodes: map[int64]string{},
		projectCodes:  map[int64]string{},
		siteCodes:     map[int64]string{},
	}
}

// Link makes employeeID the employee record of user u.
func (d *Directory) Link(u auth.UserID, employeeID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.employees[employeeID] = true
	d.unlinkLocked(employeeID)
	if old, ok := d.employeeOf[u]; ok {
		delete(d.userOf, old)
	}
	d.employeeOf[u] = employeeID
	d.userOf[employeeID] = u
}

// Unlink removes the user link of employeeID (APR-048).
func (d *Directory) Unlink(employeeID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.unlinkLocked(employeeID)
}

func (d *Directory) unlinkLocked(employeeID int64) {
	if u, ok := d.userOf[employeeID]; ok {
		delete(d.employeeOf, u)
		delete(d.userOf, employeeID)
	}
}

// SetManager sets the employee's manager; managerID 0 means none.
func (d *Directory) SetManager(employeeID, managerID int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.employees[employeeID] = true
	if managerID == 0 {
		delete(d.manager, employeeID)
		return
	}
	d.employees[managerID] = true
	d.manager[employeeID] = managerID
}

// SetActive marks an employee as still employed (the default) or left.
func (d *Directory) SetActive(employeeID int64, active bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.employees[employeeID] = true
	d.left[employeeID] = !active
}

// SetEmployeeCode sets the code DirectReports orders by.
func (d *Directory) SetEmployeeCode(employeeID int64, code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.employees[employeeID] = true
	d.employeeCodes[employeeID] = code
}

func (d *Directory) SetProjectCode(id int64, code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.projectCodes[id] = code
}

func (d *Directory) SetSiteCode(id int64, code string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.siteCodes[id] = code
}

// EmployeeForUser returns (0, false, nil) for a user whose employee has left.
func (d *Directory) EmployeeForUser(ctx context.Context, tx *db.Tx, u auth.UserID) (int64, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	emp, ok := d.employeeOf[u]
	if !ok || d.left[emp] {
		return 0, false, nil
	}
	return emp, true, nil
}

// UserForEmployee returns (0, false, nil) for an employee who has left (CD-54).
func (d *Directory) UserForEmployee(ctx context.Context, tx *db.Tx, employeeID int64) (auth.UserID, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u, ok := d.userOf[employeeID]
	if !ok || d.left[employeeID] {
		return 0, false, nil
	}
	return u, true, nil
}

// ManagerChain lists the direct manager first. An employee no seeding helper
// has named is auth.ErrNotFound.
func (d *Directory) ManagerChain(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.employees[employeeID] {
		return nil, auth.ErrNotFound
	}
	var chain []int64
	for m, ok := d.manager[employeeID]; ok && m != employeeID && !slices.Contains(chain, m); m, ok = d.manager[m] {
		chain = append(chain, m)
	}
	return chain, nil
}

// DirectReports returns active employees only, ordered by code, then id (for
// employees seeded with no code).
func (d *Directory) DirectReports(ctx context.Context, tx *db.Tx, employeeID int64) ([]int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []int64
	for e, m := range d.manager {
		if m == employeeID && !d.left[e] {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b int64) int {
		return cmp.Or(cmp.Compare(d.employeeCodes[a], d.employeeCodes[b]), cmp.Compare(a, b))
	})
	return out, nil
}

func (d *Directory) ProjectCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	return d.codes(d.projectCodes, ids), nil
}

func (d *Directory) SiteCodes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	return d.codes(d.siteCodes, ids), nil
}

func (d *Directory) codes(from map[int64]string, ids []int64) map[int64]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := map[int64]string{}
	for _, id := range ids {
		if code, ok := from[id]; ok {
			out[id] = code
		}
	}
	return out
}
