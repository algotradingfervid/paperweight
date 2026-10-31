package purchase_test

import (
	"context"
	"maps"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// The P14 signatures (plan P14): assigning the methods makes any change a
// compile error.
var (
	p14Home  func(*purchase.Service, context.Context, auth.Principal) (purchase.HomeView, error)               = (*purchase.Service).Home
	p14Queue func(*purchase.Service, context.Context, auth.Principal, string, int) (purchase.QueueView, error) = (*purchase.Service).Queue
)

const (
	p14Shree       = "Shree Steel Traders"                // SHREE's name, the vendor meta
	p14RestMeta    = "Vendor says the rest won't come · " // + dd-mm-yyyy (spec §27.2, UX §5.2)
	p14CloseReason = "Vendor won't send the rest"
	p14HrefQueue   = "/modules/purchase/q/"
	p14HrefApprove = "/approvals"
	p14HrefOverdue = "/reports/purchase/pending?overdue=1"
)

// p14At is d-m-2026 at hh:mm IST.
func p14At(m time.Month, d, hh, mm int) time.Time {
	return time.Date(2026, m, d, hh, mm, 0, 0, types.IST)
}

// p14Order saves an order as owner (vendor SHREE, site, project, ordered
// today, extra merged over the header) and takes it to stop: a draft, pending
// after owner's submit, or approved (Ravi approves when the submit leaves it
// pending). It returns the id.
func p14Order(t *testing.T, env *purchasetest.Env, owner auth.Principal, site, project string, lines []map[string]string, stop doctype.State, extra map[string]string) int64 {
	t.Helper()
	h := map[string]string{
		"vendor_id":  masterID(t, env.DB, "party", "SHREE"),
		"site_id":    masterID(t, env.DB, "site", site),
		"project_id": masterID(t, env.DB, "project", project),
		"order_date": types.Today(env.Clock).ISO(),
	}
	maps.Copy(h, extra)
	res := p2Must(t, "save order as "+owner.Name)(p2Create(t, env, owner, h, lines))
	if stop == doctype.StateDraft {
		return res.ID
	}
	res = p2Must(t, "submit order as "+owner.Name)(p2Do(t, env, owner, res.ID, doc.ActionSubmit, ""))
	if stop == doctype.StateApproved && res.State == doctype.StatePending {
		res = p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, res.ID, doc.ActionApprove, ""))
	}
	if res.State != stop {
		t.Fatalf("order %d is %s, want %s", res.ID, res.State, stop)
	}
	return res.ID
}

// p14Metro is p14Order on METRO-SITE / PRJ-METRO.
func p14Metro(t *testing.T, env *purchasetest.Env, owner auth.Principal, lines []map[string]string, stop doctype.State, extra map[string]string) int64 {
	t.Helper()
	return p14Order(t, env, owner, "METRO-SITE", "PRJ-METRO", lines, stop, extra)
}

// p14Tech is p14Order on TECH-SITE / PRJ-TECHPARK.
func p14Tech(t *testing.T, env *purchasetest.Env, owner auth.Principal, lines []map[string]string, stop doctype.State, extra map[string]string) int64 {
	t.Helper()
	return p14Order(t, env, owner, "TECH-SITE", "PRJ-TECHPARK", lines, stop, extra)
}

// p14Request saves a request as owner for site and project (requested today,
// extra merged over the header) with lines (nil: one line of 100 kg
// MS-ANGLE-50) and, when submit, submits it. It returns the id.
func p14Request(t *testing.T, env *purchasetest.Env, owner auth.Principal, site, project string, lines []map[string]string, submit bool, extra map[string]string) int64 {
	t.Helper()
	h := p5Header(t, env, site, project)
	h["request_date"] = types.Today(env.Clock).ISO()
	maps.Copy(h, extra)
	if lines == nil {
		lines = p5Lines(t, env)
	}
	res := p5Must(t, "save request as "+owner.Name)(p5Create(t, env, owner, h, lines))
	if submit {
		p5Must(t, "submit request as "+owner.Name)(p5Do(t, env, owner, res.ID, doctype.ActionSubmit, ""))
	}
	return res.ID
}

// p14Techpark runs fn while Priya's restriction also allows PRJ-TECHPARK, so
// she can own records she can't read once it is taken away again.
func p14Techpark(t *testing.T, env *purchasetest.Env, fn func()) {
	t.Helper()
	tp := p5ID(t, masterID(t, env.DB, "project", "PRJ-TECHPARK"))
	if err := env.Auth.ChangeRestrictions(t.Context(), env.Admin, env.Priya.ID, auth.DimProject, []int64{tp}, nil, nil); err != nil {
		t.Fatalf("allow Priya PRJ-TECHPARK: %v", err)
	}
	fn()
	if err := env.Auth.ChangeRestrictions(t.Context(), env.Admin, env.Priya.ID, auth.DimProject, nil, []int64{tp}, nil); err != nil {
		t.Fatalf("take PRJ-TECHPARK from Priya: %v", err)
	}
}

// p14Sent marks order id as sent today by p.
func p14Sent(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) {
	t.Helper()
	if err := env.Service.MarkSent(t.Context(), p, id, 0, purchase.SentInput{SentOn: types.Today(env.Clock), SentVia: "email"}); err != nil {
		t.Fatalf("mark order %d sent as %s: %v", id, p.Name, err)
	}
}

// p14Receive receives qty on order id's first line as GRN would: received_qty
// (SetReceived) and receipt_status (RefreshReceiptStatus).
func p14Receive(t *testing.T, env *purchasetest.Env, id int64, qty string) {
	t.Helper()
	line := p2Load(t, env, id).Lines[0].ID
	q := p9Qty(t, qty)
	purchasetest.SetReceived(t, env, line, q)
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return purchase.RefreshReceiptStatus(t.Context(), tx, id, map[int64]types.Qty{line: q})
	}); err != nil {
		t.Fatalf("refresh receipt status of order %d: %v", id, err)
	}
}

// p14Rest records "the rest won't come" on order id from GRN grn, as Suresh,
// through P19's SetRestNotComing.
func p14Rest(t *testing.T, env *purchasetest.Env, id, grn int64) {
	t.Helper()
	if err := restWrite(t, env, id, grn, env.Suresh.ID, true); err != nil {
		t.Fatalf("SetRestNotComing(%d, %d): %v", id, grn, err)
	}
}

// p14Comment posts body on dt id as p.
func p14Comment(t *testing.T, env *purchasetest.Env, p auth.Principal, dt string, id int64, body string) int64 {
	t.Helper()
	c, err := env.Comments.AddComment(t.Context(), p, dt, id, body)
	if err != nil {
		t.Fatalf("comment on %s %d as %s: %v", dt, id, p.Name, err)
	}
	return c.ID
}

// p14HomeOf is p's home; an error stops the test.
func p14HomeOf(t *testing.T, env *purchasetest.Env, p auth.Principal) purchase.HomeView {
	t.Helper()
	v, err := p14Home(env.Service, t.Context(), p)
	if err != nil {
		t.Fatalf("Home(%s): %v", p.Name, err)
	}
	return v
}

// p14QueueOf is page page of queue name for p; an error stops the test.
func p14QueueOf(t *testing.T, env *purchasetest.Env, p auth.Principal, name string, page int) purchase.QueueView {
	t.Helper()
	v, err := p14Queue(env.Service, t.Context(), p, name, page)
	if err != nil {
		t.Fatalf("Queue(%s, %q, %d): %v", p.Name, name, page, err)
	}
	return v
}

// p14Keys are v's card keys in order.
func p14Keys(v purchase.HomeView) []string {
	keys := []string{}
	for _, c := range v.Cards {
		keys = append(keys, c.Key)
	}
	return keys
}

// p14Card is v's card key.
func p14Card(v purchase.HomeView, key string) (purchase.Card, bool) {
	for _, c := range v.Cards {
		if c.Key == key {
			return c, true
		}
	}
	return purchase.Card{}, false
}

// p14IDs are v's row ids in order.
func p14IDs(v purchase.QueueView) []int64 {
	ids := []int64{}
	for _, r := range v.Rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// p14Sorted is ids sorted, for set comparisons.
func p14Sorted(ids ...int64) []int64 {
	out := slices.Clone(ids)
	if out == nil {
		out = []int64{}
	}
	slices.Sort(out)
	return out
}

// p14Display is DOC's display of dt id ("PO/26-27/00001", "Draft 42").
func p14Display(t *testing.T, env *purchasetest.Env, dt string, id int64) string {
	t.Helper()
	var info doctype.Info
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		info, err = env.Engine.Info(t.Context(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("info of %s %d: %v", dt, id, err)
	}
	return info.Display
}

// p14Rounded is order id's stored rounded_total.
func p14Rounded(t *testing.T, env *purchasetest.Env, id int64) types.Money {
	t.Helper()
	var m types.Money
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT rounded_total FROM purchase_order WHERE id = ?`, id).Scan(&m); err != nil {
		t.Fatalf("rounded_total of order %d: %v", id, err)
	}
	return m
}

// p14Row is the row a queue test expects.
type p14Row struct {
	DocType string
	ID      int64
	Title   string
	Total   types.Money
	State   doctype.State
	Meta    []string
}

// p14WantRows asserts v's rows are exactly want, in order.
func p14WantRows(t *testing.T, what string, v purchase.QueueView, want []p14Row) {
	t.Helper()
	got := make([]p14Row, 0, len(v.Rows))
	for _, r := range v.Rows {
		got = append(got, p14Row{DocType: r.DocType, ID: r.ID, Title: r.Title, Total: r.Total, State: r.State, Meta: r.Meta})
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s rows:\n got %+v\nwant %+v", what, got, want)
	}
	if v.Total != len(want) {
		t.Errorf("%s Total = %d, want %d", what, v.Total, len(want))
	}
}

// p14OrderRow is the row for order id in state st with meta.
func p14OrderRow(t *testing.T, env *purchasetest.Env, id int64, st doctype.State, meta ...string) p14Row {
	t.Helper()
	return p14Row{DocType: purchase.DocType, ID: id, Title: p14Display(t, env, purchase.DocType, id), Total: p14Rounded(t, env, id), State: st, Meta: meta}
}

// p14RequestRow is the row for request id in state st with meta.
func p14RequestRow(t *testing.T, env *purchasetest.Env, id int64, st doctype.State, meta ...string) p14Row {
	t.Helper()
	return p14Row{DocType: purchase.RequestDocType, ID: id, Title: p14Display(t, env, purchase.RequestDocType, id), State: st, Meta: meta}
}

// p14StatusLine is APR's status line for order id ("Waiting for approval
// from …"), the waiting queue's second meta (UX §5.2, inbox.go).
func p14StatusLine(t *testing.T, env *purchasetest.Env, id int64) string {
	t.Helper()
	var line string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		st, err := env.Approvals.Status(t.Context(), tx, purchase.DocType, id)
		line = st.Line
		return err
	}); err != nil {
		t.Fatalf("approval status of order %d: %v", id, err)
	}
	return line
}

// p14Probe is a new user whose only role holds rights on purchase.Order and
// read (all) on purchase.Request: one user per right a card names (lessons
// §5: a rights test needs a user holding only that right).
func p14Probe(t *testing.T, env *purchasetest.Env, name string, rights ...auth.Right) auth.Principal {
	t.Helper()
	ctx := t.Context()
	role := "P14 " + name
	if err := env.Auth.CreateRole(ctx, env.Admin, role); err != nil {
		t.Fatalf("CreateRole(%s): %v", role, err)
	}
	order := map[auth.Right]auth.Scope{}
	for _, r := range rights {
		order[r] = auth.ScopeAll
	}
	if err := env.Auth.SetRights(ctx, env.Admin, role, purchase.DocType, order); err != nil {
		t.Fatalf("SetRights(%s, order): %v", role, err)
	}
	if err := env.Auth.SetRights(ctx, env.Admin, role, purchase.RequestDocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatalf("SetRights(%s, request): %v", role, err)
	}
	id, _, err := env.Auth.CreateUser(ctx, env.Admin, auth.NewUser{Email: "p14-" + name + "@vardhan.test", Name: role, Password: "Correct-Horse-42", Roles: []string{role}})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", role, err)
	}
	p, _, err := env.Auth.LoadPrincipal(ctx, id)
	if err != nil {
		t.Fatalf("LoadPrincipal(%s): %v", role, err)
	}
	return p
}
