// Package fixturetest holds DOC's own test doubles of the AUTH, NAM, APR and
// FILE services. Each double implements the full §5 interface and records its
// calls into the fixture probe carried in the context (fixture.ProbeFrom).
package fixturetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/platform/errs"
	"github.com/vardhan/internaltools/internal/types"
)

var (
	_ auth.Service     = (*Auth)(nil)
	_ naming.Service   = (*Naming)(nil)
	_ approval.Service = (*Approvals)(nil)
	_ files.Service    = (*Files)(nil)
)

// Auth is an auth.Service that allows everything unless told otherwise.
type Auth struct {
	Deny       func(p auth.Principal, doctype string, r auth.Right) bool
	Hide       func(p auth.Principal, doctype string, id int64) bool
	DenyValues func(doctype string, values map[string][]int64) error
	DenyLink   func(target string, id int64) error
	Users      map[types.UserID]auth.UserInfo
	Calls      []string

	mu    sync.Mutex
	roles map[types.UserID][]string
}

// NewAuth returns an Auth with Users filled from 00-fixtures (ids 1–8), at
// the version a new user starts at (CD-189).
func NewAuth() *Auth {
	a := &Auth{Users: map[types.UserID]auth.UserInfo{}}
	for _, u := range []auth.UserInfo{
		{ID: 1, Email: "admin@vardhan.test", Name: "Anil Kapoor", Enabled: true, Version: 1},
		{ID: 2, Email: "meena@vardhan.test", Name: "Meena Iyer", Enabled: true, Version: 1},
		{ID: 3, Email: "ravi@vardhan.test", Name: "Ravi Kulkarni", Enabled: true, Version: 1},
		{ID: 4, Email: "priya@vardhan.test", Name: "Priya Shah", Enabled: true, Version: 1},
		{ID: 5, Email: "suresh@vardhan.test", Name: "Suresh Patil", Enabled: true, Version: 1},
		{ID: 6, Email: "kavya@vardhan.test", Name: "Kavya Rao", Enabled: true, Version: 1},
		{ID: 7, Email: "deepak@vardhan.test", Name: "Deepak Nair", Enabled: true, Version: 1},
		{ID: 8, Email: "old@vardhan.test", Name: "Former Staff", Enabled: false, Version: 1},
	} {
		a.Users[u.ID] = u
	}
	return a
}

// Remember registers p's roles for HasRole.
func (a *Auth) Remember(p auth.Principal) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.roles == nil {
		a.roles = map[types.UserID][]string{}
	}
	a.roles[p.ID] = slices.Clone(p.Roles)
}

func (a *Auth) record(ctx context.Context, call string) {
	a.mu.Lock()
	a.Calls = append(a.Calls, call)
	a.mu.Unlock()
	fixture.ProbeFrom(ctx).Add(call)
}

// applies refuses like the real service before any grant: an unknown DocType
// is a plain error, a right that doesn't apply to it a *auth.DeniedError (CD-171, CD-173).
func applies(name string, r auth.Right) error {
	dt, ok := doctype.Lookup(name)
	if !ok {
		return fmt.Errorf("auth: unknown DocType %q", name)
	}
	if !auth.Applies(dt, r) {
		return &auth.DeniedError{Right: r, Label: dt.Label, Plural: dt.PluralLabel}
	}
	return nil
}

// Can refuses like the real service an unknown DocType or a right that
// doesn't apply, then returns auth.ErrNotFound for a hidden record, then
// auth.ErrForbidden when denied.
func (a *Auth) Can(ctx context.Context, p auth.Principal, dt string, r auth.Right, rec *auth.RecordRef) error {
	a.record(ctx, "AUTH.Can:"+string(r))
	if err := applies(dt, r); err != nil {
		return err
	}
	if rec != nil && a.Hide != nil && a.Hide(p, dt, rec.ID) {
		return auth.ErrNotFound
	}
	if a.Deny != nil && a.Deny(p, dt, r) {
		return auth.ErrForbidden
	}
	return nil
}

// CanTx is Can: the double has no transactions.
func (a *Auth) CanTx(ctx context.Context, _ *db.Tx, p auth.Principal, dt string, r auth.Right, rec *auth.RecordRef) error {
	return a.Can(ctx, p, dt, r, rec)
}

// Scope refuses like Can, otherwise returns an empty predicate (no filter).
func (a *Auth) Scope(_ context.Context, _ auth.Principal, dt string, r auth.Right) (auth.Predicate, error) {
	return auth.Predicate{}, applies(dt, r)
}

// CheckValues refuses an unknown DocType like the real service, otherwise
// returns DenyValues' error when set.
func (a *Auth) CheckValues(ctx context.Context, _ auth.Principal, dt string, values map[string][]int64) error {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		ids := make([]string, 0, len(values[k]))
		for _, id := range values[k] {
			ids = append(ids, strconv.FormatInt(id, 10))
		}
		parts = append(parts, k+"="+strings.Join(ids, ","))
	}
	a.record(ctx, "AUTH.CheckValues:"+strings.Join(parts, " "))
	if _, ok := doctype.Lookup(dt); !ok {
		return fmt.Errorf("auth: unknown DocType %q", dt)
	}
	if a.DenyValues != nil {
		return a.DenyValues(dt, values)
	}
	return nil
}

// CheckValuesTx is CheckValues: the double has no transactions.
func (a *Auth) CheckValuesTx(ctx context.Context, _ *db.Tx, p auth.Principal, dt string, values map[string][]int64) error {
	return a.CheckValues(ctx, p, dt, values)
}

// CheckLink returns DenyLink's error when set.
func (a *Auth) CheckLink(ctx context.Context, _ auth.Principal, target string, id int64) error {
	a.record(ctx, "AUTH.CheckLink:"+target+":"+strconv.FormatInt(id, 10))
	if a.DenyLink != nil {
		return a.DenyLink(target, id)
	}
	return nil
}

// CheckLinkTx is CheckLink: the double has no transactions.
func (a *Auth) CheckLinkTx(ctx context.Context, _ *db.Tx, p auth.Principal, target string, id int64) error {
	return a.CheckLink(ctx, p, target, id)
}

// UsersWithRight returns no users; an unknown DocType is an error, as in the
// real service.
func (a *Auth) UsersWithRight(_ context.Context, dt string, r auth.Right, _ *auth.RecordRef) ([]auth.UserID, error) {
	if err := applies(dt, r); err != nil && !errors.Is(err, auth.ErrForbidden) {
		return nil, err
	}
	return nil, nil
}

// UsersWithRightTx is UsersWithRight: the double has no transactions.
func (a *Auth) UsersWithRightTx(ctx context.Context, _ *db.Tx, dt string, r auth.Right, rec *auth.RecordRef) ([]auth.UserID, error) {
	return a.UsersWithRight(ctx, dt, r, rec)
}

// Restricted reports no restriction on any dimension and refuses an unknown
// one, like the real service (CD-174).
func (a *Auth) Restricted(_ context.Context, _ *db.Tx, _ auth.Principal, dimension string) (bool, error) {
	return false, auth.ValidDimension(dimension)
}

// ValueAllowedTx allows every value, as nobody here is restricted, and refuses
// an unknown dimension like the real service (CD-216 item 7).
func (a *Auth) ValueAllowedTx(_ context.Context, _ *db.Tx, _ auth.Principal, dimension string, _ int64) (bool, error) {
	err := auth.ValidDimension(dimension)
	return err == nil, err
}

// DefaultValue has no value, as nobody here is restricted, and refuses an
// unknown dimension like the real service (AUTH-100).
func (a *Auth) DefaultValue(_ context.Context, _ auth.Principal, dim auth.Dimension) (int64, bool, error) {
	return 0, false, auth.ValidDimension(string(dim))
}

// HasRole reads the roles registered with Remember.
func (a *Auth) HasRole(_ context.Context, u auth.UserID, role string) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Contains(a.roles[u], role), nil
}

// HasRoleTx is HasRole: the double has no transactions.
func (a *Auth) HasRoleTx(ctx context.Context, _ *db.Tx, u auth.UserID, role string) (bool, error) {
	return a.HasRole(ctx, u, role)
}

// User returns the user from Users.
func (a *Auth) User(_ context.Context, _ *db.Tx, u auth.UserID) (auth.UserInfo, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	info, ok := a.Users[u]
	return info, ok, nil
}

// UserByEmail returns the user from Users, comparing normalised emails; a
// blank email finds nobody (CD-199).
func (a *Auth) UserByEmail(_ context.Context, _ *db.Tx, email string) (auth.UserInfo, bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	email = auth.NormalizeEmail(email)
	if email == "" {
		return auth.UserInfo{}, false, nil
	}
	for _, info := range a.Users {
		if auth.NormalizeEmail(info.Email) == email {
			return info, true, nil
		}
	}
	return auth.UserInfo{}, false, nil
}

// DisableUser marks the user disabled in Users.
func (a *Auth) DisableUser(_ context.Context, _ *db.Tx, u auth.UserID, _ string) error {
	return a.setEnabled(u, false)
}

// EnableUser marks the user enabled in Users.
func (a *Auth) EnableUser(_ context.Context, _ *db.Tx, u auth.UserID) error {
	return a.setEnabled(u, true)
}

func (a *Auth) setEnabled(u auth.UserID, enabled bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	info, ok := a.Users[u]
	if !ok {
		return fmt.Errorf("fixturetest.Auth: no user %d", u)
	}
	info.Enabled = enabled
	info.Version++ // the real SetUserEnabled bumps users.version (CD-189)
	a.Users[u] = info
	return nil
}

// defaultPrefix is the series prefix per fixture DocType (CD-113, CD-115).
var defaultPrefix = map[string]string{fixture.Order: "PO", fixture.Receipt: "GR", fixture.Claim: "EX"}

// maxCounter is the last number a {#####} counter can render.
const maxCounter = 99999

// Naming is a naming.Service whose counter is a count of numbered rows read in
// the given tx, so a rolled-back number is never used up. The zero value works;
// a nil Prefix uses PO, GR and EX.
type Naming struct {
	Prefix   map[string]string
	Assigned int

	mu sync.Mutex
}

// Assign returns PREFIX/{FY short}/%05d, counting the numbered originals in dt's table.
func (n *Naming) Assign(ctx context.Context, tx *db.Tx, dtName string, _ int64, d types.Date) (string, error) {
	fixture.ProbeFrom(ctx).Add("NAM.Assign")
	prefixes := n.Prefix
	if prefixes == nil {
		prefixes = defaultPrefix
	}
	prefix, ok := prefixes[dtName]
	if !ok {
		return "", fmt.Errorf("fixturetest.Naming: no prefix for %s", dtName)
	}
	dt, ok := doctype.Lookup(dtName)
	if !ok {
		return "", fmt.Errorf("fixturetest.Naming: DocType %s is not registered", dtName)
	}
	series := prefix + "/" + types.FiscalYear(d).Short + "/"
	var count int
	if err := tx.SQL().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM "`+dt.Table+`" WHERE number LIKE ? AND amendment_no = 0`,
		series+"%").Scan(&count); err != nil {
		return "", fmt.Errorf("fixturetest.Naming: count %s: %w", dt.Table, err)
	}
	if count >= maxCounter { // NAM's render doesn't range-check the counter
		return "", fmt.Errorf("fixturetest.Naming: series %s is full at %05d", series, maxCounter)
	}
	n.mu.Lock()
	n.Assigned++
	n.mu.Unlock()
	return fmt.Sprintf("%s%05d", series, count+1), nil
}

// AssignAmendment returns root + "-" + n.
func (n *Naming) AssignAmendment(ctx context.Context, _ *db.Tx, _ string, _ int64, root string, k int) (string, error) {
	fixture.ProbeFrom(ctx).Add("NAM.AssignAmendment")
	return root + "-" + strconv.Itoa(k), nil
}

// Preview is not used by DOC.
func (n *Naming) Preview(context.Context, string, types.Date) (string, error) {
	return "", errs.ErrNotImplemented
}

// ValidateTemplate is not used by DOC.
func (n *Naming) ValidateTemplate(string) error { return errs.ErrNotImplemented }

// Approvals is an approval.Service that traces the DOC ↔ APR call protocol
// (CD-23). The zero value works: Needed then follows 00-fixtures.
type Approvals struct {
	Needed func(doctype string, amount types.Money) bool
	Deny   func(p auth.Principal) error
	Fail   map[string]error
	Calls  []string
	Opened []approval.Requirement
	// TB, when set, receives t.Errorf for a Record action outside the protocol.
	TB testing.TB

	mu sync.Mutex
}

// defaultNeeded is the 00-fixtures rule: orders from ₹50,000.00, claims always.
func defaultNeeded(dt string, amount types.Money) bool {
	switch dt {
	case fixture.Order:
		return amount >= 5000000
	case fixture.Claim:
		return true
	}
	return false
}

// call records "APR."+key and returns Fail[key].
func (a *Approvals) call(ctx context.Context, key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Calls = append(a.Calls, "APR."+key)
	fixture.ProbeFrom(ctx).Add("APR." + key)
	return a.Fail[key]
}

// Evaluate returns the requirement for amount.
func (a *Approvals) Evaluate(ctx context.Context, _ *db.Tx, dt string, _ int64, amount types.Money, requester auth.Principal) (approval.Requirement, error) {
	if err := a.call(ctx, "Evaluate"); err != nil {
		return approval.Requirement{}, err
	}
	needed := defaultNeeded
	if a.Needed != nil {
		needed = a.Needed
	}
	return approval.Requirement{
		Needed:        needed(dt, amount),
		RuleID:        1,
		ApproverRoles: []string{"Purchase Manager"},
		Amount:        amount,
		Requester:     requester.ID,
	}, nil
}

// Open appends req to Opened.
func (a *Approvals) Open(ctx context.Context, _ *db.Tx, _ string, _ int64, req approval.Requirement) error {
	if err := a.call(ctx, "Open"); err != nil {
		return err
	}
	a.mu.Lock()
	a.Opened = append(a.Opened, req)
	a.mu.Unlock()
	return nil
}

// CheckApprover returns Deny(p) when set.
func (a *Approvals) CheckApprover(ctx context.Context, _ *db.Tx, p auth.Principal, _ string, _ int64) error {
	if err := a.call(ctx, "CheckApprover"); err != nil {
		return err
	}
	if a.Deny != nil {
		return a.Deny(p)
	}
	return nil
}

// Record accepts only the protocol actions approve, reject, revise and
// withdraw. Like APR, it refuses a reject whose trimmed reason is empty or
// longer than 1,000 characters (CD-157).
func (a *Approvals) Record(ctx context.Context, _ *db.Tx, _ string, _ int64, action doctype.Action, _ auth.Principal, reason string) error {
	switch action {
	case doctype.ActionApprove, doctype.ActionRevise, doctype.ActionWithdraw:
		return a.call(ctx, "Record:"+string(action))
	case doctype.ActionReject:
		if err := a.call(ctx, "Record:"+string(action)); err != nil {
			return err
		}
		switch n := utf8.RuneCountInString(strings.TrimSpace(reason)); {
		case n == 0:
			return &approval.UserError{Kind: approval.ErrReasonRequired, Msg: "Enter a reason for rejecting.", Field: "reason"}
		case n > 1000:
			return &approval.UserError{Kind: approval.ErrReasonTooLong, Msg: "Reason must be 1,000 characters or fewer.", Field: "reason"}
		}
		return nil
	}
	_ = a.call(ctx, "Record:"+string(action)) // the protocol violation wins over an injected Fail
	if a.TB != nil {
		a.TB.Errorf("APR.Record called with action %q; only approve, reject, revise and withdraw are allowed (CD-23)", action)
	}
	return errors.New("fixturetest.Approvals: Record with action " + string(action))
}

// Inbox returns an empty result.
func (a *Approvals) Inbox(ctx context.Context, _ auth.Principal, _ types.Page) (types.Result[approval.InboxItem], error) {
	return types.Result[approval.InboxItem]{}, a.call(ctx, "Inbox")
}

// Files is a files.Service that records CopyAll and DeleteAll only.
type Files struct {
	Copied  [][2]auth.RecordRef
	Deleted []auth.RecordRef

	mu sync.Mutex
}

// Attach is not used by DOC.
func (f *Files) Attach(context.Context, *db.Tx, auth.Principal, auth.RecordRef, string, *files.Spooled) (int64, error) {
	return 0, errs.ErrNotImplemented
}

// Remove is not used by DOC.
func (f *Files) Remove(context.Context, *db.Tx, auth.Principal, int64) error {
	return errs.ErrNotImplemented
}

// Open is not used by DOC.
func (f *Files) Open(context.Context, auth.Principal, int64) (io.ReadCloser, files.Meta, error) {
	return nil, files.Meta{}, errs.ErrNotImplemented
}

// List is not used by DOC.
func (f *Files) List(context.Context, auth.Principal, auth.RecordRef) ([]files.Meta, error) {
	return nil, errs.ErrNotImplemented
}

// CopyAll records the pair.
func (f *Files) CopyAll(_ context.Context, _ *db.Tx, from, to auth.RecordRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Copied = append(f.Copied, [2]auth.RecordRef{from, to})
	return nil
}

// DeleteAll records parent.
func (f *Files) DeleteAll(_ context.Context, _ *db.Tx, parent auth.RecordRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Deleted = append(f.Deleted, parent)
	return nil
}

// StepClock is a clock.Clock that tests move by hand.
type StepClock struct {
	mu  sync.Mutex
	now time.Time
}

// NewStepClock returns a clock reading rfc3339.
func NewStepClock(rfc3339 string) *StepClock {
	c := &StepClock{}
	c.Set(rfc3339)
	return c
}

// Now returns the set time.
func (c *StepClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Set moves the clock to rfc3339; a malformed value panics.
func (c *StepClock) Set(rfc3339 string) {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		panic("fixturetest.StepClock: " + err.Error())
	}
	c.SetTime(t)
}

// SetTime moves the clock to t.
func (c *StepClock) SetTime(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = t
}

// Numbers returns NAM's number rules (CD-119).
func Numbers() doc.NumberFuncs {
	return doc.NumberFuncs{Guard: naming.GuardNumberField, CheckAmendment: naming.CheckAmendmentNo}
}
