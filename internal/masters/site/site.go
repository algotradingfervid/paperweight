// Package site is the Site/Warehouse master (MST Task 13): a code, a branch,
// an optional project and a type, warehouse or site (D-MST-20, MST-105).
package site

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// DocType is the registry name.
const DocType = "Site"

// Kind is the Site master.
var Kind = mstkit.Kind{DocType: DocType, Label: "Site", Table: "site", HasCode: true}

// SiteType is a site's type (MST-105).
type SiteType string

// The two types, in display order (M13).
const (
	Warehouse SiteType = "warehouse"
	SiteKind  SiteType = "site"
)

var siteTypes = []SiteType{Warehouse, SiteKind}

const (
	msgType      = "Type must be Warehouse or Site." // MST-105 (06 wins over CD-166)
	msgBranch    = "Branch is required."
	msgNoBranch  = "Branch not found. Pick one from the list."                                            // CD-170
	msgNoProject = "Project not found. Pick one from the list."                                           // CD-170
	msgRestrict  = "You can only work with your assigned sites. Ask your administrator to add this site." // CD-172
	msgLocked    = "Branch can't change after the site is used."                                          // MST-107

	msgRestrictProject = "You can only link sites to your assigned projects. Ask your administrator to add this project." // CD-214
	msgReadOnly        = "This site is on a project you aren't assigned to. Ask your administrator to change it."         // CD-216(6)
)

func init() {
	options := make([]string, 0, len(siteTypes))
	for _, t := range siteTypes {
		options = append(options, string(t))
	}
	mstkit.RegisterMaster(doctype.DocType{
		Name:        DocType,
		Label:       "Site",
		PluralLabel: "Sites",
		Table:       Kind.Table,
		Importable:  true,
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text, Required: true, Importable: true, ListPriority: 4, MaxLen: 20},
			{Name: "name", Label: "Name", Column: "name", Type: doctype.Text, Required: true, Importable: true, ListPriority: 1},
			{Name: "branch_id", Label: "Branch", Column: "branch_id", Type: doctype.Link, Link: company.DocType, Required: true, Importable: true, ListPriority: 6},
			{Name: "type", Label: "Type", Column: "type", Type: doctype.Select, Required: true, Importable: true, ListPriority: 5, Options: options},
			{Name: "project_id", Label: "Project", Column: "project_id", Type: doctype.Link, Link: project.DocType, Importable: true},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check, Importable: true, Filterable: true, ListPriority: 3},
		},
		SearchFields: []string{"code", "name"},
		DefaultSort:  []doctype.SortKey{{Field: "name"}}, // list adds id as the tie-breaker
		SiteFields:   []string{"id"},                     // CD-09, CD-64
	}, mstkit.LookupFunc(lookup))
}

// lookup is the Master lookup.
func lookup(ctx context.Context, tx *db.Tx, id int64) (doctype.MasterInfo, bool, error) {
	r, err := dbq.New(tx.SQL()).GetSite(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.MasterInfo{}, false, nil
	}
	if err != nil {
		return doctype.MasterInfo{}, false, err
	}
	return doctype.MasterInfo{Code: r.Code, Name: r.Name, Disabled: r.Disabled != 0}, true, nil
}

// Site is one Site record.
type Site struct {
	ID         int64
	Code, Name string
	BranchID   int64
	ProjectID  int64 // 0 = none
	Type       SiteType
	Disabled   bool
	Version    int
}

// Input is what Create and Update accept.
type Input struct {
	Code, Name string
	BranchID   int64
	ProjectID  int64
	Type       SiteType
}

// Validate normalises code and name and checks branch presence and type.
func Validate(in Input) (Input, error) {
	var es mstkit.Errors
	add := func(err error) {
		var ve *mstkit.ValidationError
		if errors.As(err, &ve) {
			for _, f := range ve.Fields {
				es.Add(f.Field, f.Message)
			}
		}
	}
	code, err := mstkit.NormalizeCode(in.Code)
	add(err)
	name, err := mstkit.NormalizeName(in.Name)
	add(err)
	if in.BranchID == 0 {
		es.Add("branch_id", msgBranch)
	}
	if !validType(in.Type) {
		es.Add("type", msgType) // CD-166
	}
	if err := es.Err(); err != nil {
		return Input{}, err
	}
	in.Code, in.Name = code, name
	return in, nil
}

func validType(t SiteType) bool {
	for _, v := range siteTypes {
		if t == v {
			return true
		}
	}
	return false
}

// Service writes and reads sites.
type Service struct{ env mstkit.Env }

// New returns a Service over env.
func New(env mstkit.Env) *Service { return &Service{env: env} }

// ref carries the site's own id as its site value (SiteFields {"id"}, CD-64).
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: DocType, ID: id, SiteIDs: []int64{id}}
}

// checkUnrestricted refuses a principal restricted on Site (CD-172),
// reading inside tx (CD-174).
func (s *Service) checkUnrestricted(ctx context.Context, tx *db.Tx, p auth.Principal) error {
	restricted, err := s.env.Auth.Restricted(ctx, tx, p, string(auth.DimSite))
	if err != nil {
		return err
	}
	if restricted {
		return auth.Forbidden(msgRestrict)
	}
	return nil
}

// checkProjectAllowed refuses a project outside p's project restriction on
// every save that posts one, reading inside tx (CD-213, CD-214). It runs before
// checkLinks, whose messages name the project's and branch's codes.
func (s *Service) checkProjectAllowed(ctx context.Context, tx *db.Tx, p auth.Principal, projectID int64) error {
	allowed, err := s.projectAllowed(ctx, tx, p, projectID)
	if err != nil || allowed {
		return err
	}
	return auth.Forbidden(msgRestrictProject)
}

// checkNotReadOnly refuses any write of the stored site st when its project is
// outside p's project restriction (CD-216(6)). On Update it runs after Validate
// and before the checks that read other rows for what was posted, so its text
// wins over CD-214's and over any link check. It reads the stored row, so a
// stale page gets the stale error first, and reloads to the refusal (CD-94).
func (s *Service) checkNotReadOnly(ctx context.Context, tx *db.Tx, p auth.Principal, st Site, version int) error {
	allowed, err := s.projectAllowed(ctx, tx, p, st.ProjectID)
	if err != nil || allowed {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, st.ID, version); err != nil {
		return err
	}
	return auth.Forbidden(msgReadOnly)
}

// projectAllowed reports whether projectID is none, or p isn't restricted on
// projects, or it is in p's list, reading inside tx.
func (s *Service) projectAllowed(ctx context.Context, tx *db.Tx, p auth.Principal, projectID int64) (bool, error) {
	if projectID == 0 {
		return true, nil
	}
	restricted, err := s.env.Auth.Restricted(ctx, tx, p, string(auth.DimProject))
	if err != nil {
		return false, err
	}
	if !restricted {
		return true, nil
	}
	return dbq.New(tx.SQL()).UserHasRestrictionValue(ctx, dbq.UserHasRestrictionValueParams{
		UserID: int64(p.ID), Dimension: string(auth.DimProject), ValueID: projectID,
	})
}

func userID(p auth.Principal) sql.NullInt64 {
	return sql.NullInt64{Int64: int64(p.ID), Valid: true}
}

func nullID(id int64) sql.NullInt64 { return sql.NullInt64{Int64: id, Valid: id != 0} }

// checkLinks refuses an unknown or disabled branch or project and a project of
// another branch, reading inside tx (CD-94, CD-170, MST-106).
func (s *Service) checkLinks(ctx context.Context, tx *db.Tx, in Input) error {
	var es mstkit.Errors
	b, err := company.New(s.env).Get(ctx, tx, in.BranchID)
	switch {
	case errors.Is(err, doctype.ErrNotFound):
		es.Add("branch_id", msgNoBranch)
	case err != nil:
		return err
	case b.Disabled:
		es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
	}
	if in.ProjectID != 0 {
		pr, err := project.New(s.env).Get(ctx, tx, in.ProjectID)
		switch {
		case errors.Is(err, doctype.ErrNotFound):
			es.Add("project_id", msgNoProject)
		case err != nil:
			return err
		case pr.Disabled:
			es.Add("project_id", "Project "+pr.Code+" is disabled. Pick another project.")
		case pr.BranchID != in.BranchID:
			pb, err := company.New(s.env).Get(ctx, tx, pr.BranchID)
			if err != nil {
				return err
			}
			es.Add("project_id", "The site's branch must match the project's branch ("+pb.Code+").")
		}
	}
	return es.Err()
}

// checkBranchLock refuses a branch change on a site a document other than a
// draft uses (MST-107).
func (s *Service) checkBranchLock(ctx context.Context, tx *db.Tx, old Site, in Input) error {
	if in.BranchID == old.BranchID {
		return nil
	}
	_, used, err := s.env.UsedOutsideDrafts(ctx, tx, Kind.Table, old.ID)
	if err != nil || !used {
		return err
	}
	var es mstkit.Errors
	es.Add("branch_id", msgLocked)
	return es.Err()
}

// Create adds a site.
func (s *Service) Create(ctx context.Context, p auth.Principal, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightCreate, func(tx *db.Tx) error {
		var err error
		saved, err = s.CreateTx(ctx, tx, p, in)
		return err
	})
	return saved, err
}

// CreateTx is Create inside tx.
func (s *Service) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightCreate, nil); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkUnrestricted(ctx, tx, p); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkProjectAllowed(ctx, tx, p, in.ProjectID); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLinks(ctx, tx, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, 0); err != nil {
		return mstkit.Saved{}, err
	}
	now := types.TimestampOf(s.env.Clock.Now()).String()
	id, err := dbq.New(tx.SQL()).InsertSite(ctx, dbq.InsertSiteParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, ProjectID: nullID(in.ProjectID), Type: string(in.Type),
		CreatedAt: now, CreatedBy: userID(p), UpdatedAt: now, UpdatedBy: userID(p),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "create", nil, auditFields(fromInput(in))); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: 1}, nil
}

// Update replaces the site's fields.
func (s *Service) Update(ctx context.Context, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	var saved mstkit.Saved
	err := s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		var err error
		saved, err = s.UpdateTx(ctx, tx, p, id, version, in)
		return err
	})
	return saved, err
}

// UpdateTx is Update inside tx.
func (s *Service) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in Input) (mstkit.Saved, error) {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return mstkit.Saved{}, err
	}
	in, err := Validate(in)
	if err != nil {
		return mstkit.Saved{}, err
	}
	old, err := s.Get(ctx, tx, id)
	if err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkNotReadOnly(ctx, tx, p, old, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkProjectAllowed(ctx, tx, p, in.ProjectID); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkLinks(ctx, tx, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckCodeFree(ctx, tx, Kind, in.Code, id); err != nil {
		return mstkit.Saved{}, err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.checkBranchLock(ctx, tx, old, in); err != nil {
		return mstkit.Saved{}, err
	}
	if err := s.env.CheckCodeChange(ctx, p, old.Code, in.Code); err != nil {
		return mstkit.Saved{}, err
	}
	before, after := auditFields(old), auditFields(fromInput(in))
	if mstkit.Unchanged(before, after) {
		return mstkit.Saved{ID: id, Version: version}, nil
	}
	n, err := dbq.New(tx.SQL()).UpdateSite(ctx, dbq.UpdateSiteParams{
		Code: in.Code, Name: in.Name, BranchID: in.BranchID, ProjectID: nullID(in.ProjectID), Type: string(in.Type),
		UpdatedAt: types.TimestampOf(s.env.Clock.Now()).String(), UpdatedBy: userID(p), ID: id, Version: int64(version),
	})
	if err != nil {
		return mstkit.Saved{}, err
	}
	if n == 0 {
		// The row moved off the posted version: CheckVersion names the stale case.
		if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
			return mstkit.Saved{}, err
		}
		return mstkit.Saved{}, doctype.ErrNotFound
	}
	if err := s.env.Audit(ctx, tx, p, Kind, id, "save", before, after); err != nil {
		return mstkit.Saved{}, err
	}
	return mstkit.Saved{ID: id, Version: old.Version + 1}, nil
}

// SetDisabled disables or enables the site.
func (s *Service) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return s.env.Write(ctx, p, Kind, auth.RightWrite, func(tx *db.Tx) error {
		return s.SetDisabledTx(ctx, tx, p, id, version, disabled)
	})
}

// SetDisabledTx is SetDisabled inside tx. Enabling needs an enabled branch, the
// site's side of MST-108 (CD-170).
func (s *Service) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightWrite, ref(id)); err != nil {
		return err
	}
	st, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := s.checkNotReadOnly(ctx, tx, p, st, version); err != nil {
		return err
	}
	if !disabled {
		if err := s.checkBranchEnabled(ctx, tx, id, version); err != nil {
			return err
		}
	}
	return s.env.SetDisabledTx(ctx, tx, p, Kind, id, version, disabled)
}

// checkBranchEnabled refuses a site at version whose branch is disabled; a
// stale page gets the stale error first (CD-94).
func (s *Service) checkBranchEnabled(ctx context.Context, tx *db.Tx, id int64, version int) error {
	st, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := mstkit.CheckVersion(ctx, tx, Kind, id, version); err != nil {
		return err
	}
	b, err := company.New(s.env).Get(ctx, tx, st.BranchID)
	if err != nil || !b.Disabled {
		return err
	}
	var es mstkit.Errors
	es.Add("branch_id", "Branch "+b.Code+" is disabled. Pick another branch.")
	return es.Err()
}

// Delete deletes the site unless it is referenced.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return s.env.Write(ctx, p, Kind, auth.RightDelete, func(tx *db.Tx) error {
		return s.DeleteTx(ctx, tx, p, id, version)
	})
}

// DeleteTx is Delete inside tx.
func (s *Service) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	if err := s.env.Auth.Can(ctx, p, DocType, auth.RightDelete, ref(id)); err != nil {
		return err
	}
	st, err := s.Get(ctx, tx, id)
	if err != nil {
		return err
	}
	if err := s.checkNotReadOnly(ctx, tx, p, st, version); err != nil {
		return err
	}
	return s.env.DeleteTx(ctx, tx, p, Kind, id, version)
}

// Get reads the site; a missing one is doctype.ErrNotFound.
func (s *Service) Get(ctx context.Context, tx *db.Tx, id int64) (Site, error) {
	r, err := dbq.New(tx.SQL()).GetSite(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, doctype.ErrNotFound
	}
	if err != nil {
		return Site{}, err
	}
	return fromRow(r), nil
}

// Codes maps each of ids to its site's code; ids with no site are left out
// (auth.Directory, M10).
func (s *Service) Codes(ctx context.Context, tx *db.Tx, ids []int64) (map[int64]string, error) {
	codes := make(map[int64]string, len(ids))
	for _, id := range ids {
		r, err := s.Get(ctx, tx, id)
		if errors.Is(err, doctype.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		codes[id] = r.Code
	}
	return codes, nil
}

// ByCode reads the site with code (upper-cased and trimmed first).
func (s *Service) ByCode(ctx context.Context, tx *db.Tx, code string) (Site, bool, error) {
	r, err := dbq.New(tx.SQL()).GetSiteByCode(ctx, strings.ToUpper(strings.TrimSpace(code)))
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, false, nil
	}
	if err != nil {
		return Site{}, false, err
	}
	return fromRow(dbq.GetSiteRow(r)), true, nil
}

func fromRow(r dbq.GetSiteRow) Site {
	return Site{
		ID: r.ID, Code: r.Code, Name: r.Name, BranchID: r.BranchID, ProjectID: r.ProjectID.Int64,
		Type: SiteType(r.Type), Disabled: r.Disabled != 0, Version: int(r.Version),
	}
}

func fromInput(in Input) Site {
	return Site{Code: in.Code, Name: in.Name, BranchID: in.BranchID, ProjectID: in.ProjectID, Type: in.Type}
}

// auditFields is the site as audited.
func auditFields(st Site) map[string]string {
	id := func(v int64) string {
		if v == 0 {
			return ""
		}
		return strconv.FormatInt(v, 10)
	}
	return map[string]string{
		"code": st.Code, "name": st.Name, "branch_id": id(st.BranchID), "project_id": id(st.ProjectID), "type": string(st.Type),
	}
}
