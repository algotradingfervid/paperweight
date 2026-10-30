// Package siteweb is the Site master's screens (MST Task 22, CD-89): the list,
// new and record pages, disable, enable and delete under /m/site, and the
// picker search at /pick/Site. The screens themselves are mstweb's; this
// package gives them the site service, its copy and the functions below.
package siteweb

import (
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/site"
)

// Routes mounts the site screens under /m/site and the picker at /pick/Site.
func Routes(svc *site.Service, env mstkit.Env) func(chi.Router) {
	return mstweb.Screen[site.Site, site.Input]{
		DocType: site.DocType,
		Base:    "/m/site",
		Copy: mstweb.Copy{
			Plural: "Sites", Noun: "sites", One: "site", New: "New site", Search: "Search sites", Empty: "No sites yet.",
			Saved: "Site saved.", Disabled: "Site disabled.", Enabled: "Site enabled.", Deleted: "Site deleted.",
		},
		Svc:          svc,
		Env:          env,
		ToInput:      toInput,
		Validate:     site.Validate,
		Values:       values,
		Head:         head,
		Ref:          ref,
		CanCreate:    svc.CheckUnrestricted,
		OptionLabels: map[string]map[string]string{"type": {string(site.Warehouse): "Warehouse", string(site.SiteKind): "Site"}},
	}.Routes()
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank link is none; an id that can't be parsed never reaches the service,
// so it gets the service's refusal of one that doesn't exist.
func toInput(v map[string]string) (site.Input, []doctype.FieldError) {
	in := site.Input{Code: v["code"], Name: v["name"], Type: site.SiteType(v["type"])}
	var errs []doctype.FieldError
	in.BranchID, errs = link(v, "branch_id", site.MsgBranchNotFound, errs)
	in.ProjectID, errs = link(v, "project_id", site.MsgProjectNotFound, errs)
	return in, errs
}

// link is the id in the form's field, 0 when blank; one that can't be parsed
// adds msg to errs.
func link(v map[string]string, field, msg string, errs []doctype.FieldError) (int64, []doctype.FieldError) {
	s := v[field]
	if s == "" {
		return 0, errs
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, append(errs, doctype.FieldError{Field: field, Message: msg})
	}
	return id, errs
}

// values is a stored site as the form's values: the links the ids in decimal,
// the type its stored value.
func values(s site.Site) map[string]string {
	return map[string]string{
		"code": s.Code, "name": s.Name, "type": string(s.Type),
		"branch_id":  strconv.FormatInt(s.BranchID, 10),
		"project_id": strconv.FormatInt(s.ProjectID, 10),
	}
}

func head(s site.Site) mstweb.Head {
	return mstweb.Head{Code: s.Code, Name: s.Name, Disabled: s.Disabled, Version: s.Version}
}

// ref is the record AUTH checks, as site.Service builds it: the site's own id
// is its site value (SiteFields {"id"}, CD-64).
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: site.DocType, ID: id, SiteIDs: []int64{id}}
}
