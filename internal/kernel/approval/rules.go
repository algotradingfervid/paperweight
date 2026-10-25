package approval

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/vardhan/internaltools/internal/kernel/approval/dbq"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// ruleDocType is the audit DocType of approval rules.
const ruleDocType = "approval.Rule"

// NormalizeRule trims the DocType and role names.
func NormalizeRule(in RuleInput) RuleInput {
	in.DocType = strings.TrimSpace(in.DocType)
	in.Role = strings.TrimSpace(in.Role)
	in.FallbackRole = strings.TrimSpace(in.FallbackRole)
	return in
}

// ValidateRule checks NormalizeRule(in): DocType, amount and approver fields.
// Production passes doctype.Lookup.
func ValidateRule(in RuleInput, lookup func(string) (*doctype.DocType, bool)) error {
	return validateRule(in, lookup, doctype.All)
}

// validateRule is ValidateRule; all lists the DocTypes named in the
// manager-only message.
func validateRule(in RuleInput, lookup func(string) (*doctype.DocType, bool), all func() []*doctype.DocType) error {
	in = NormalizeRule(in)
	dt, ok := lookup(in.DocType)
	if !ok {
		return noDocType()
	}
	if dt.Master != nil || !dt.Approvable {
		return masterDocType(dt.Label)
	}
	if in.FromAmount < 0 {
		return invalidFromAmount()
	}
	switch in.ApproverType {
	case ApproverManager:
		if dt.EmployeeField == "" {
			var labels []string
			for _, d := range approvable(all) {
				if d.EmployeeField != "" {
					labels = append(labels, d.Label)
				}
			}
			return managerNotAvailable(labels)
		}
		if in.FallbackRole == "" {
			return noFallbackRole()
		}
	case ApproverRole:
		if in.Role == "" {
			return noApproverRole()
		}
	default: // empty or unknown type: refuse here, not at the DB CHECK
		return noApproverRole()
	}
	return nil
}

// approvable is all() filtered to approvable non-masters, sorted by Label.
func approvable(all func() []*doctype.DocType) []*doctype.DocType {
	var out []*doctype.DocType
	for _, d := range all() {
		if d.Approvable && d.Master == nil {
			out = append(out, d)
		}
	}
	slices.SortFunc(out, func(a, b *doctype.DocType) int { return strings.Compare(a.Label, b.Label) })
	return out
}

// ApprovableDocTypes lists the DocTypes a rule may name, sorted by Label.
func (a *Approvals) ApprovableDocTypes() []*doctype.DocType { return approvable(a.all) }

// CreateRule saves a new enabled rule.
func (a *Approvals) CreateRule(ctx context.Context, p auth.Principal, in RuleInput) (Rule, error) {
	in, err := a.checkRule(ctx, p, in)
	if err != nil {
		return Rule{}, err
	}
	var r Rule
	err = a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		if err := a.checkDuplicate(ctx, q, in.DocType, in.FromAmount, 0); err != nil {
			return err
		}
		now := ts(a.clock.Now())
		id, err := q.InsertRule(ctx, dbq.InsertRuleParams{
			DocType: in.DocType, FromAmount: int64(in.FromAmount), ApproverType: string(in.ApproverType),
			ApproverRole: in.Role, FallbackRole: in.FallbackRole, AllowSelfApproval: boolInt(in.AllowSelfApproval),
			CreatedAt: now, CreatedBy: int64(p.ID),
		})
		if err != nil {
			return a.mapUnique(err, in)
		}
		r = Rule{ID: id, RuleInput: in, Enabled: true, Version: 1}
		return a.writeAudit(ctx, tx, p, id, "create", ruleChanges(RuleInput{}, in))
	})
	if err != nil {
		return Rule{}, err
	}
	return r, nil
}

// UpdateRule saves in over rule id if it is still at version (CD-94).
func (a *Approvals) UpdateRule(ctx context.Context, p auth.Principal, id, version int64, in RuleInput) (Rule, error) {
	in, err := a.checkRule(ctx, p, in)
	if err != nil {
		return Rule{}, err
	}
	var r Rule
	err = a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		old, err := getRule(ctx, q, id)
		if err != nil {
			return err
		}
		if old.Version != version {
			return ruleConflict()
		}
		if old.Enabled {
			if err := a.checkDuplicate(ctx, q, in.DocType, in.FromAmount, id); err != nil {
				return err
			}
		}
		n, err := q.UpdateRule(ctx, dbq.UpdateRuleParams{
			DocType: in.DocType, FromAmount: int64(in.FromAmount), ApproverType: string(in.ApproverType),
			ApproverRole: in.Role, FallbackRole: in.FallbackRole, AllowSelfApproval: boolInt(in.AllowSelfApproval),
			UpdatedAt: ts(a.clock.Now()), UpdatedBy: int64(p.ID), ID: id, Version: version,
		})
		if err != nil {
			return a.mapUnique(err, in)
		}
		if n == 0 {
			return ruleConflict()
		}
		r = Rule{ID: id, RuleInput: in, Enabled: old.Enabled, Version: version + 1}
		return a.writeAudit(ctx, tx, p, id, "save", ruleChanges(old.RuleInput, in))
	})
	if err != nil {
		return Rule{}, err
	}
	return r, nil
}

// SetRuleEnabled enables or disables rule id. Enabling checks for an enabled
// duplicate.
func (a *Approvals) SetRuleEnabled(ctx context.Context, p auth.Principal, id int64, enabled bool) error {
	if err := a.requireAdmin(ctx, p); err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		old, err := getRule(ctx, q, id)
		if err != nil {
			return err
		}
		if old.Enabled == enabled {
			return nil
		}
		if enabled {
			if err := a.checkDuplicate(ctx, q, old.DocType, old.FromAmount, id); err != nil {
				return err
			}
		}
		if _, err := q.SetRuleEnabled(ctx, dbq.SetRuleEnabledParams{
			Enabled: boolInt(enabled), UpdatedAt: ts(a.clock.Now()), UpdatedBy: int64(p.ID), ID: id,
		}); err != nil {
			return a.mapUnique(err, old.RuleInput)
		}
		action := "disable"
		if enabled {
			action = "enable"
		}
		return a.writeAudit(ctx, tx, p, id, action, []audit.Change{{
			Field: "enabled", Before: strconv.FormatBool(old.Enabled), After: strconv.FormatBool(enabled), Op: "set",
		}})
	})
}

// DeleteRule deletes rule id unless a document has used it.
func (a *Approvals) DeleteRule(ctx context.Context, p auth.Principal, id int64) error {
	if err := a.requireAdmin(ctx, p); err != nil {
		return err
	}
	return a.db.Write(ctx, func(tx *db.Tx) error {
		q := dbq.New(tx.SQL())
		old, err := getRule(ctx, q, id)
		if err != nil {
			return err
		}
		n, err := q.CountRuleUsage(ctx, id)
		if err != nil {
			return err
		}
		if n > 0 {
			return ruleInUse(n)
		}
		if err := q.DeleteRule(ctx, id); err != nil {
			return err
		}
		return a.writeAudit(ctx, tx, p, id, "delete", ruleChanges(old.RuleInput, RuleInput{}))
	})
}

// Rules lists every rule, ordered by DocType and from amount.
func (a *Approvals) Rules(ctx context.Context, p auth.Principal) ([]Rule, error) {
	if err := a.requireAdmin(ctx, p); err != nil {
		return nil, err
	}
	var out []Rule
	err := a.db.Read(ctx, func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ListRules(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			out = append(out, ruleOf(row))
		}
		return nil
	})
	return out, err
}

// checkRule is the Administrator check, then validation of the normalized input.
func (a *Approvals) checkRule(ctx context.Context, p auth.Principal, in RuleInput) (RuleInput, error) {
	if err := a.requireAdmin(ctx, p); err != nil {
		return in, err
	}
	in = NormalizeRule(in)
	if err := validateRule(in, a.lookup, a.all); err != nil {
		return in, err
	}
	if a.roles != nil {
		field, role := "role", in.Role
		if in.ApproverType == ApproverManager {
			field, role = "fallback_role", in.FallbackRole
		}
		exists, active, err := a.roles.RoleStatus(ctx, role)
		if err != nil {
			return in, err
		}
		if !exists {
			return in, roleMissing(field, role)
		}
		if !active {
			return in, roleDisabled(field, role)
		}
	}
	return in, nil
}

// requireAdmin checks the Administrator role live.
func (a *Approvals) requireAdmin(ctx context.Context, p auth.Principal) error {
	ok, err := a.auth.HasRole(ctx, p.ID, "Administrator")
	if err != nil {
		return err
	}
	if !ok {
		return notAdmin()
	}
	return nil
}

// checkDuplicate refuses a second enabled rule for (dt, from), other than id.
func (a *Approvals) checkDuplicate(ctx context.Context, q *dbq.Queries, dt string, from types.Money, id int64) error {
	_, err := q.FindEnabledDuplicate(ctx, dbq.FindEnabledDuplicateParams{DocType: dt, FromAmount: int64(from), ID: id})
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil
	case err != nil:
		return err
	}
	return duplicateRule(a.label(dt), from)
}

// mapUnique turns the enabled-rule unique index violation into ErrDuplicateRule.
func (a *Approvals) mapUnique(err error, in RuleInput) error {
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE {
		return duplicateRule(a.label(in.DocType), in.FromAmount)
	}
	return err
}

func (a *Approvals) label(dt string) string {
	if d, ok := a.lookup(dt); ok {
		return d.Label
	}
	return dt
}

func (a *Approvals) writeAudit(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, action string, changes []audit.Change) error {
	return a.audit.Append(ctx, tx, audit.Entry{
		DocType: ruleDocType, DocID: id, Action: action, Changes: changes, By: p.ID, At: a.clock.Now(),
	})
}

// ruleChanges lists the fields that differ between before and after
// (from_amount as paise text).
func ruleChanges(before, after RuleInput) []audit.Change {
	names := [...]string{"doctype", "from_amount", "approver_type", "role", "fallback_role", "allow_self_approval"}
	values := func(in RuleInput) [len(names)]string {
		if in.DocType == "" { // no rule: before a create, after a delete
			return [len(names)]string{}
		}
		return [len(names)]string{in.DocType, strconv.FormatInt(int64(in.FromAmount), 10), string(in.ApproverType),
			in.Role, in.FallbackRole, strconv.FormatBool(in.AllowSelfApproval)}
	}
	b, af := values(before), values(after)
	var out []audit.Change
	for i, name := range names {
		if b[i] != af[i] {
			out = append(out, audit.Change{Field: name, Before: b[i], After: af[i], Op: "set"})
		}
	}
	return out
}

func getRule(ctx context.Context, q *dbq.Queries, id int64) (Rule, error) {
	row, err := q.GetRule(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return Rule{}, doctype.ErrNotFound
	}
	if err != nil {
		return Rule{}, err
	}
	return ruleOf(row), nil
}

func ruleOf(r dbq.ApprovalRule) Rule {
	return Rule{
		ID: r.ID,
		RuleInput: RuleInput{
			DocType: r.DocType, FromAmount: types.Money(r.FromAmount), ApproverType: ApproverType(r.ApproverType),
			Role: r.ApproverRole, FallbackRole: r.FallbackRole, AllowSelfApproval: r.AllowSelfApproval != 0,
		},
		Enabled: r.Enabled != 0,
		Version: r.Version,
	}
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}
