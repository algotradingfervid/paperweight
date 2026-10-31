package purchase_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Helpers for P3's tests (MOD-PO P3: approval amount, numbering, amend,
// owner rights), all on the real auth.Service the purchasetest env builds.
// Every document row is written through Engine.Do.

// p3Status is err's HTTP status (0 for nil, -1 for an error with none).
func p3Status(err error) int {
	if err == nil {
		return 0
	}
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.Status()
	}
	return -1
}

// p3Open is the document page's read check (docscreen View): the record as
// DOC reports it, then AUTH's read on that RecordRef. nil means p opens it.
func p3Open(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) error {
	t.Helper()
	var ref auth.RecordRef
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		info, err := env.Engine.Info(t.Context(), tx, purchase.DocType, id)
		if err != nil {
			return err
		}
		ref = auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
			EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
		return nil
	}); err != nil {
		t.Fatalf("info of order %d: %v", id, err)
	}
	return env.Auth.Can(t.Context(), p, purchase.DocType, auth.RightRead, &ref)
}

// p3Display is DOC's display name of order id ("PO/26-27/00001" or "Draft 42").
func p3Display(t *testing.T, env *purchasetest.Env, id int64) string {
	t.Helper()
	var info doctype.Info
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		info, err = env.Engine.Info(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("info of order %d: %v", id, err)
	}
	return info.Display
}

// p3ListIDs are the order ids the generic list shows p (no filters, first
// page), sorted.
func p3ListIDs(t *testing.T, env *purchasetest.Env, p auth.Principal) []int64 {
	t.Helper()
	dt, _ := doctype.Lookup(purchase.DocType)
	svc := &list.Service{DB: env.DB, Auth: env.Auth}
	res, err := svc.List(t.Context(), p, dt, list.Query{Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("list orders as %s: %v", p.Name, err)
	}
	var ids []int64
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

// p3Order saves an order as p with header and lines and, unless draft,
// submits it as p. It returns the stored id.
func p3Order(t *testing.T, env *purchasetest.Env, p auth.Principal, header map[string]string, lines []map[string]string, draft bool) int64 {
	t.Helper()
	res := p2Must(t, "save order as "+p.Name)(p2Create(t, env, p, header, lines))
	if !draft {
		p2Must(t, "submit order as "+p.Name)(p2Do(t, env, p, res.ID, doc.ActionSubmit, ""))
	}
	return res.ID
}

// p3SmallLine is one line below every approval rule: 100 kg MS-ANGLE-50 at
// ₹62.50, 18% (₹7,375.00).
func p3SmallLine(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p2Line(t, env, "MS-ANGLE-50", "KGS", "100", "62.5")}
}

// p3Rule is the env's purchase.Order rule starting at from.
func p3Rule(t *testing.T, env *purchasetest.Env, from int64) approval.Rule {
	t.Helper()
	rules, err := env.Approvals.Rules(t.Context(), env.Admin)
	if err != nil {
		t.Fatalf("approval rules: %v", err)
	}
	for _, r := range rules {
		if r.DocType == purchase.DocType && int64(r.FromAmount) == from {
			return r
		}
	}
	t.Fatalf("no purchase.Order rule from %d paise in %+v", from, rules)
	return approval.Rule{}
}

// p3Latest is order id's latest approval request.
func p3Latest(t *testing.T, env *purchasetest.Env, id int64) (approval.Request, bool) {
	t.Helper()
	var req approval.Request
	var ok bool
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		req, ok, err = env.Approvals.LatestRequest(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("latest request of order %d: %v", id, err)
	}
	return req, ok
}

// p3History is order id's approval history, oldest first.
func p3History(t *testing.T, env *purchasetest.Env, id int64) []approval.HistoryRow {
	t.Helper()
	var rows []approval.HistoryRow
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		rows, err = env.Approvals.History(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("approval history of order %d: %v", id, err)
	}
	return rows
}

// p3Namer is a naming admin over the env's database, for the cutover
// counter (the env's own Namer isn't exported; Namer holds no state).
func p3Namer(env *purchasetest.Env) *naming.Namer {
	return naming.New(env.DB, env.Clock, env.Auth, audit.Default)
}
