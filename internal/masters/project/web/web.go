// Package projectweb is the Project master's screens (MST Task 22, CD-89): the
// list, new and record pages, disable, enable and delete under /m/project, and
// the picker search at /pick/Project. The screens themselves are mstweb's;
// this package gives them the project service, its copy and the functions
// below.
package projectweb

import (
	"errors"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/types"
)

// Routes mounts the project screens under /m/project and the picker at
// /pick/Project.
func Routes(svc *project.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[project.Project, project.Input]{
		DocType: project.DocType,
		Base:    "/m/project",
		Copy: mstweb.Copy{
			Plural: "Projects", Noun: "projects", One: "project", New: "New project",
			Search: "Search projects", Empty: "No projects yet.",
			Saved: "Project saved.", Disabled: "Project disabled.", Enabled: "Project enabled.", Deleted: "Project deleted.",
		},
		Svc:       svc,
		Env:       env,
		ToInput:   toInput,
		Validate:  project.Validate,
		Values:    values,
		Head:      head,
		Ref:       ref,
		CanCreate: svc.CheckUnrestricted,
		Defaults:  map[string]string{"status": string(project.Open)}, // MST-101
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank link or date is none. A link that can't be parsed gets the service's
// refusal of one that doesn't exist, and a date that can't be parsed NUM's
// message; neither reaches the service.
func toInput(v map[string]string) (project.Input, []doctype.FieldError) {
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
	return project.Input{
		Code: v["code"], Name: v["name"], Status: project.Status(v["status"]),
		BranchID:   link("branch_id", project.MsgBranchNotFound),
		CustomerID: link("customer_id", project.MsgCustomerNotFound),
		Start:      date("start_date"),
		End:        date("end_date"),
	}, errs
}

// values is a stored project as the form's values: links the id in decimal,
// dates the stored form or blank.
func values(p project.Project) map[string]string {
	return map[string]string{
		"code": p.Code, "name": p.Name, "status": string(p.Status),
		"branch_id": strconv.FormatInt(p.BranchID, 10), "customer_id": strconv.FormatInt(p.CustomerID, 10),
		"start_date": isoDate(p.Start), "end_date": isoDate(p.End),
	}
}

func isoDate(d types.Date) string {
	if d == (types.Date{}) {
		return ""
	}
	return d.ISO()
}

func head(p project.Project) mstweb.Head {
	return mstweb.Head{Code: p.Code, Name: p.Name, Disabled: p.Disabled, Version: p.Version}
}

// ref is the record AUTH checks, as project.Service builds it: the project's
// own id is its project value (ProjectFields {"id"}, CD-64).
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: project.DocType, ID: id, ProjectIDs: []int64{id}}
}
