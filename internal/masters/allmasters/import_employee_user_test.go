package allmasters_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/masters/employee"
)

// CD-199: the master's MST-112 check skips a Left row, so only importEmployee's
// own check refuses this one. old@vardhan.test is the disabled fixture user.
func TestImport_EmployeeLeftDisabledUser(t *testing.T) {
	env, svc := othersFresh(t)
	if _, ok := env.Kit.Auth.(*auth.Auth); !ok {
		t.Fatalf("Kit.Auth is %T, want the real *auth.Auth", env.Kit.Auth)
	}
	body := othersCSV(othersEmployeeHeader, "E-1,One,PUNE,old@vardhan.test,,01-04-2024,,Left,31-03-2026")
	othersRefused(t, othersPreview(t, env, employee.DocType, body), "Row 2: "+othersNoUser)
	othersRefused(t, othersImport(t, env, employee.DocType, body), "Row 2: "+othersNoUser)
	othersAbsent(t, env, svc.Employee.ByCode, "E-1")
}

// MST-111 through the import. The message names the stored email, whatever
// case the file used.
func TestImport_EmployeeUserAlreadyLinked(t *testing.T) {
	env, svc := othersFresh(t)
	if _, ok := env.Kit.Auth.(*auth.Auth); !ok {
		t.Fatalf("Kit.Auth is %T, want the real *auth.Auth", env.Kit.Auth)
	}

	t.Run("same file", func(t *testing.T) {
		body := othersCSV(othersEmployeeHeader,
			othersEmployeeRow("E-1", "One", "deepak@vardhan.test", ""),
			othersEmployeeRow("E-2", "Two", "deepak@vardhan.test", ""),
		)
		const want = "Row 3: User deepak@vardhan.test is already linked to employee E-1."
		preview := othersPreview(t, env, employee.DocType, body)
		othersRefused(t, preview, want)
		if preview.Ready != 1 {
			t.Errorf("Preview Ready = %d, want 1", preview.Ready)
		}
		othersRefused(t, othersImport(t, env, employee.DocType, body), want)
		othersAbsent(t, env, svc.Employee.ByCode, "E-1")
		othersAbsent(t, env, svc.Employee.ByCode, "E-2")
	})

	t.Run("stored employee, email in different case", func(t *testing.T) {
		othersLoaded(t, othersImport(t, env, employee.DocType,
			othersCSV(othersEmployeeHeader, othersEmployeeRow("E-3", "Three", "priya@vardhan.test", ""))), 1)
		body := othersCSV(othersEmployeeHeader, othersEmployeeRow("E-4", "Four", "PRIYA@Vardhan.TEST", ""))
		const want = "Row 2: User priya@vardhan.test is already linked to employee E-3."
		othersRefused(t, othersPreview(t, env, employee.DocType, body), want)
		othersRefused(t, othersImport(t, env, employee.DocType, body), want)
		othersAbsent(t, env, svc.Employee.ByCode, "E-4")
	})
}
