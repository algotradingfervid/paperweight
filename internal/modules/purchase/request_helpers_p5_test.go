package purchase_test

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers for P5's tests (MOD-PO P5, request rules). Every document row is
// written through Engine.Do or the purchasetest builders; the SQL here only
// reads. All names carry p5 so P2's and P6's helpers never clash.

// p5Today is the fixture clock's business date (authtest.T0, 24-10-2026 IST).
const p5Today = "2026-10-24"

// AUTH's and DOC's texts the P5 tests expect (CD-142: the producing WP's
// text, never a copy that drifts).
const (
	p5NotFound     = "This record doesn't exist or you don't have access to it."
	p5OwnOnlyEdit  = "You can only change Purchase Requests you created."
	p5OwnOnlyClose = "You can only approve Purchase Requests you created."
	p5SureshSites  = "You can only use Sites METRO-SITE, PUNE-WH. Choose one of your allowed sites."
	p5Blocked      = "Can't cancel %s because %s (%s) is linked to it. Cancel %s first." // DOC-052
)

// The request texts (PO spec §20.2, §4), copied exactly.
const (
	p5MsgDateFuture   = "Request date can't be in the future."
	p5MsgNeededBy     = "Needed by can't be before the request date."
	p5MsgSiteProject  = "Site %s is on project %s. Pick that project or another site." // site code, project code
	p5MsgWholeNumber  = "Line %d: Quantity %s can't be a fraction for UoM %s. Enter a whole number."
	p5CloseReason     = "No longer needed on site"
	p5CancelReason    = "Raised by mistake"
	p5RequestFieldSet = "site_id|project_id" // §4 names neither field; P2's checkSiteProject decides
)

// p5Header is a request header for site and project (codes; "" leaves the
// field blank), dated today.
func p5Header(t *testing.T, env *purchasetest.Env, site, project string) map[string]string {
	t.Helper()
	h := map[string]string{"site_id": "", "project_id": "", "request_date": p5Today}
	if site != "" {
		h["site_id"] = masterID(t, env.DB, "site", site)
	}
	if project != "" {
		h["project_id"] = masterID(t, env.DB, "project", project)
	}
	return h
}

// p5Line is one request line of qty of item in unit.
func p5Line(t *testing.T, env *purchasetest.Env, item, unit, qty string) map[string]string {
	t.Helper()
	l := map[string]string{"item_id": masterID(t, env.DB, "item", item), "qty": qty, "uom_id": ""}
	if unit != "" {
		l["uom_id"] = masterID(t, env.DB, "uom", unit)
	}
	return l
}

// p5Lines is PR-A's default single line: 100 kg MS-ANGLE-50.
func p5Lines(t *testing.T, env *purchasetest.Env) []map[string]string {
	t.Helper()
	return []map[string]string{p5Line(t, env, "MS-ANGLE-50", "KGS", "100")}
}

// p5Create creates a request draft as p through the engine.
func p5Create(t *testing.T, env *purchasetest.Env, p auth.Principal, values map[string]string, lines []map[string]string) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{Values: values, Lines: lines})
}

// p5Version is the stored version of request id.
func p5Version(t *testing.T, env *purchasetest.Env, id int64) int {
	t.Helper()
	var v int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_request WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of request %d: %v", id, err)
	}
	return v
}

// p5Save saves draft request id as p at its stored version. nil lines post
// the stored lines as a page would, with their ids and values (DOC deletes
// lines a save leaves out, and drops a line that posts only its id).
func p5Save(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, values map[string]string, lines []map[string]string) (doc.Result, error) {
	t.Helper()
	if lines == nil {
		for _, l := range p5Load(t, env, purchase.RequestDocType, id).Lines {
			m := map[string]string{"id": strconv.FormatInt(l.ID, 10)}
			for _, k := range []string{"item_id", "description", "uom_id", "qty", "note"} {
				if v := p5Plain(l.Values[k]); v != "<nil>" {
					m[k] = v
				}
			}
			lines = append(lines, m)
		}
	}
	return env.Engine.Do(t.Context(), p, purchase.RequestDocType, id, doctype.ActionSave,
		doc.ActionInput{Version: p5Version(t, env, id), Values: values, Lines: lines})
}

// p5Do runs action a on request id as p at its stored version.
func p5Do(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, a doctype.Action, reason string) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, purchase.RequestDocType, id, a, doc.ActionInput{Version: p5Version(t, env, id), Reason: reason})
}

// p5DoOrder runs action a on PO id as p at its stored version.
func p5DoOrder(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, a doctype.Action, reason string) (doc.Result, error) {
	t.Helper()
	var v int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_order WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of order %d: %v", id, err)
	}
	return env.Engine.Do(t.Context(), p, purchase.DocType, id, a, doc.ActionInput{Version: v, Reason: reason})
}

// p5Must stops the test when an engine call it relies on fails.
func p5Must(t *testing.T, what string) func(doc.Result, error) doc.Result {
	t.Helper()
	return func(res doc.Result, err error) doc.Result {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v %+v", what, err, p5Fields(err))
		}
		return res
	}
}

// p5Fields are err's field errors, for a failure message.
func p5Fields(err error) []doctype.FieldError {
	var he doctype.HTTPError
	if errors.As(err, &he) {
		return he.FieldErrors()
	}
	return nil
}

// p5Load reads document id of dt as DOC stores it.
func p5Load(t *testing.T, env *purchasetest.Env, dt string, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("load %s %d: %v", dt, id, err)
	}
	return d
}

// p5Plain formats a stored value in its plain form; nil reads <nil>.
func p5Plain(v any) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case types.Qty:
		return x.Plain()
	case types.Date:
		return x.ISO()
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	}
	return fmt.Sprintf("%v", v)
}

// p5Counts are the row counts a refused request write must leave unchanged.
type p5Counts struct {
	requests, lines, orders, audits, approvals, history int
}

func p5CountAll(t *testing.T, env *purchasetest.Env) p5Counts {
	t.Helper()
	return p5Counts{
		requests:  countRows(t, env.DB, "purchase_request", "1 = 1"),
		lines:     countRows(t, env.DB, "purchase_request_line", "1 = 1"),
		orders:    countRows(t, env.DB, "purchase_order", "1 = 1"),
		audits:    countRows(t, env.DB, "audit_log", "1 = 1"),
		approvals: countRows(t, env.DB, "approval_requests", "1 = 1"),
		history:   countRows(t, env.DB, "approval_history", "1 = 1"),
	}
}

// p5SameCounts reports a refused write that changed a row count.
func p5SameCounts(t *testing.T, what string, before p5Counts, env *purchasetest.Env) {
	t.Helper()
	if after := p5CountAll(t, env); after != before {
		t.Errorf("%s: row counts %+v, want %+v unchanged (rolled back)", what, after, before)
	}
}

// p5WantInvalid asserts err is DOC's invalid error (422) whose field errors
// are exactly want, in any order. A want Field of "a|b" accepts either name.
func p5WantInvalid(t *testing.T, what string, err error, want ...doctype.FieldError) {
	t.Helper()
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindInvalid || de.Status() != 422 {
		t.Errorf("%s: err = %v, want an invalid error (422) with %+v", what, err, want)
		return
	}
	got := slices.Clone(de.Fields)
	for _, w := range want {
		i := slices.IndexFunc(got, func(g doctype.FieldError) bool {
			return g.Line == w.Line && g.Message == w.Message && slices.Contains(strings.Split(w.Field, "|"), g.Field)
		})
		if i < 0 {
			t.Errorf("%s: field errors %+v lack %+v", what, de.Fields, w)
			continue
		}
		got = slices.Delete(got, i, i+1)
	}
	if len(got) > 0 {
		t.Errorf("%s: unexpected field errors %+v (want exactly %+v)", what, got, want)
	}
}

// p5WantDenied asserts err is AUTH's 403 with exactly msg.
func p5WantDenied(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.Is(err, auth.ErrForbidden) || !errors.As(err, &he) || he.Status() != 403 || err.Error() != msg {
		t.Errorf("%s: err = %v, want AUTH's 403 %q", what, err, msg)
	}
}

// p5WantNotFound asserts err is AUTH's not-found (404): a record the user
// can't read answers like a missing one (CD-245).
func p5WantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) || err.Error() != p5NotFound {
		t.Errorf("%s: err = %v, want AUTH's not-found %q (CD-245)", what, err, p5NotFound)
	}
}

// p5Ref is the RecordRef AUTH decides on for request d: owner, project, site.
func p5Ref(t *testing.T, d doc.Doc) *auth.RecordRef {
	t.Helper()
	ref := &auth.RecordRef{DocType: purchase.RequestDocType, ID: d.ID, CreatedBy: d.CreatedBy}
	for col, dst := range map[string]*[]int64{"project_id": &ref.ProjectIDs, "site_id": &ref.SiteIDs} {
		if s := p5Plain(d.Header[col]); s != "<nil>" {
			n, err := strconv.ParseInt(s, 10, 64)
			if err != nil {
				t.Fatalf("request %d %s = %q: %v", d.ID, col, s, err)
			}
			*dst = []int64{n}
		}
	}
	return ref
}

// p5ID parses an id masterID returned.
func p5ID(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatalf("id %q: %v", s, err)
	}
	return n
}
