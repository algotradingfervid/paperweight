// Package employeeweb is the Employee master's screens (MST Task 22, CD-50,
// CD-89): the list, disable, enable and delete under /m/employee, the create
// wizard at /m/employee/new, a record's per-section edit, and the picker search
// at /pick/Employee. The screens themselves are mstweb's; this package gives
// them the employee service, its copy and the functions below.
package employeeweb

import (
	"context"
	"errors"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Routes mounts the employee screens under /m/employee and the picker at
// /pick/Employee. The wizard's values wait in sm's session.
func Routes(svc *employee.Service, env mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
	steps := mstweb.Steps(employee.DocType)
	return mstweb.Screen[employee.Employee, employee.Input]{
		DocType: employee.DocType,
		Base:    "/m/employee",
		Copy: mstweb.Copy{
			Plural: "Employees", Noun: "employees", One: "employee", New: "New employee",
			Search: "Search employees", Empty: "No employees yet.",
			Saved: "Employee saved.", Disabled: "Employee disabled.", Enabled: "Employee enabled.", Deleted: "Employee deleted.",
		},
		Svc:      svc,
		Env:      env,
		ToInput:  toInput,
		Validate: employee.Validate,
		Values:   values,
		Head:     head,
		Ref:      ref,
		Defaults: map[string]string{"status": string(employee.Active)},
		Sections: steps,
		Wizard:   mstweb.CreateWizard[employee.Input]{Sessions: sm, Steps: steps, ToInput: toInput, Validate: employee.Validate, Save: svc.Create},
		Lookups:  map[string]mstweb.Lookup{"user_id": userLookup(env.Auth)},
	}.Routes()
}

// userLookup is the user box (CD-218): the email they sign in with, because
// users are not a DocType a picker could search. A disabled user is not found,
// so the box can't tell which emails exist; the service's own check stays
// authoritative inside the write. Readers see Masked, never the email (CD-222).
func userLookup(a auth.Service) mstweb.Lookup {
	return mstweb.Lookup{
		Msg:    employee.MsgNoUser,
		Hint:   "The email they sign in with.",
		Masked: "Linked", // CD-222: the email is for writers
		Find: func(ctx context.Context, tx *db.Tx, email string) (string, bool, error) {
			u, ok, err := a.UserByEmail(ctx, tx, email)
			return strconv.FormatInt(int64(u.ID), 10), ok && u.Enabled, err
		},
		Show: func(ctx context.Context, tx *db.Tx, stored string) (string, error) {
			id, err := strconv.ParseInt(stored, 10, 64)
			if err != nil {
				return "", err
			}
			u, _, err := a.User(ctx, tx, auth.UserID(id)) // the foreign key keeps the user
			return u.Email, err
		},
	}
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank link or date is none. A link that can't be parsed gets the service's
// refusal of one that doesn't exist, and a date that can't be parsed NUM's
// message; neither reaches the service. user_id is already an id (Lookups).
func toInput(v map[string]string) (employee.Input, []doctype.FieldError) {
	var errs []doctype.FieldError
	link := func(field, notFound string) int64 {
		s := v[field]
		if s == "" {
			return 0
		}
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			errs = append(errs, doctype.FieldError{Field: field, Message: notFound})
		}
		return id
	}
	date := func(field string) types.Date {
		d, err := types.ParseDate(v[field])
		if err != nil && !errors.Is(err, types.ErrEmpty) {
			errs = append(errs, doctype.FieldError{Field: field, Message: err.Error()})
		}
		return d
	}
	return employee.Input{
		Code: v["code"], Name: v["name"], Status: employee.Status(v["status"]),
		BranchID:  link("branch_id", employee.MsgBranchNotFound),
		ManagerID: link("manager_id", employee.MsgManagerNotFound),
		UserID:    auth.UserID(link("user_id", employee.MsgNoUser)),
		Joining:   date("joining_date"),
		Birth:     date("birth_date"),
		Relieving: date("relieving_date"),
	}, errs
}

// values is a stored employee as the form's values: links the id in decimal,
// dates the stored form or blank, no user blank.
func values(e employee.Employee) map[string]string {
	user := ""
	if e.UserID != 0 {
		user = strconv.FormatInt(int64(e.UserID), 10)
	}
	return map[string]string{
		"code": e.Code, "name": e.Name, "status": string(e.Status), "user_id": user,
		"branch_id": strconv.FormatInt(e.BranchID, 10), "manager_id": strconv.FormatInt(e.ManagerID, 10),
		"joining_date": isoDate(e.Joining), "birth_date": isoDate(e.Birth), "relieving_date": isoDate(e.Relieving),
	}
}

func isoDate(d types.Date) string {
	if d == (types.Date{}) {
		return ""
	}
	return d.ISO()
}

func head(e employee.Employee) mstweb.Head {
	return mstweb.Head{Code: e.Code, Name: e.Name, Disabled: e.Disabled, Version: e.Version}
}

// ref is the record AUTH checks, as employee.Service builds it: Employee
// declares no project, site or employee field.
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: employee.DocType, ID: id}
}
