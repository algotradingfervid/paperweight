package purchase_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

const p2MsgStateWarning = "Site PUNE-WH is under branch PUNE in Maharashtra, but project PRJ-TECHPARK belongs to branch BLR in Karnataka. Check the tax with your CA."

// PO-032 (PO spec §3.2, §4, §18; CD-234, CD-280 D1) and PO-030's first-save
// part (spec §6.3): Already received above qty saves and OrderWarnings returns
// `Line {n}: Already received is more than the quantity.` per line; equal to
// qty warns nothing. A line with Already received needs Earlier PO no., and
// an Earlier PO date needs it too, each with its own text. receipt_status
// follows already_received from the first draft save, and back (undo).
func TestPO_AlreadyReceivedRules(t *testing.T) {
	env := purchasetest.New(t)
	cutover := purchasetest.OrderHeader(map[string]string{"earlier_po_no": "P-17", "earlier_po_date": "2026-09-15"})
	line := func(qty, already string) map[string]string {
		l := p2Line(t, env, "MS-ANGLE-50", "KGS", qty, "62.5")
		l["already_received"] = already
		return l
	}
	status := func(id int64) string {
		t.Helper()
		return p2Snap(t, env, id, "receipt_status")["receipt_status"]
	}

	for _, c := range []struct {
		what     string
		lines    []map[string]string
		warnings []string
		status   string
	}{
		{"above qty on line 1", []map[string]string{line("10", "12")},
			[]string{"Line 1: Already received is more than the quantity."}, "fully_received"},
		{"above qty on line 2 only", []map[string]string{line("10", "3"), line("4", "6")},
			[]string{"Line 2: Already received is more than the quantity."}, "part_received"},
		{"above qty on both lines", []map[string]string{line("10", "11"), line("4", "6")},
			[]string{"Line 1: Already received is more than the quantity.", "Line 2: Already received is more than the quantity."}, "fully_received"},
		{"equal to qty", []map[string]string{line("10", "10")}, nil, "fully_received"},
		{"part", []map[string]string{line("10", "3")}, nil, "part_received"},
	} {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), cutover, purchasetest.OrderLines(c.lines...))
		if got := p2Warnings(t, env, env.Priya, po.ID); !slices.Equal(got, c.warnings) {
			t.Errorf("%s: OrderWarnings = %q, want %q", c.what, got, c.warnings)
		}
		if got := status(po.ID); got != c.status {
			t.Errorf("%s: receipt_status after the first save = %q, want %q", c.what, got, c.status)
		}
	}

	// Earlier PO no. rules.
	h := p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	before := p2CountAll(t, env)
	_, err := p2Create(t, env, env.Priya, h, []map[string]string{line("10", "3")})
	p2WantInvalid(t, "already received, no earlier PO no.", err,
		doctype.FieldError{Field: "earlier_po_no", Message: "Earlier PO no. is required when a line has Already received."})
	p2SameCounts(t, "already received, no earlier PO no.", before, env)
	h["earlier_po_date"] = "2026-09-15"
	_, err = p2Create(t, env, env.Priya, h, p2RefLines(t, env))
	p2WantInvalid(t, "earlier PO date, no earlier PO no.", err,
		doctype.FieldError{Field: "earlier_po_no", Message: "Earlier PO no. is required with an earlier PO date."})
	p2SameCounts(t, "earlier PO date, no earlier PO no.", before, env)

	// receipt_status follows every draft save: none, part, none.
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), cutover, purchasetest.OrderLines(line("10", "0")))
	if got := status(po.ID); got != "not_received" {
		t.Errorf("nothing already received: receipt_status = %q, want not_received", got)
	}
	for _, s := range []struct{ already, want string }{{"3", "part_received"}, {"10", "fully_received"}, {"0", "not_received"}} {
		p2Must(t, "already "+s.already)(p2Save(t, env, env.Priya, po.ID, nil, []map[string]string{line("10", s.already)}))
		if got := status(po.ID); got != s.want {
			t.Errorf("already received %s: receipt_status = %q, want %q", s.already, got, s.want)
		}
	}
}

// PO-084 (PO spec §27.3; CD-272 Gap 14, CD-276 (2), CD-280 D1), on the real
// auth.Service: a PO with site PUNE-WH (branch PUNE, Maharashtra) and project
// PRJ-TECHPARK (branch BLR, Karnataka) saves, and OrderWarnings returns §27.3's
// text; METRO-SITE with PRJ-METRO, and TECH-SITE with PRJ-TECHPARK, warn
// nothing, and moving the project to PRJ-METRO removes it (undo). Already
// received warnings come first. A user who can't read the PO gets no
// warnings, only AUTH's not-found.
func TestPO_SiteProjectStateWarning(t *testing.T) {
	env := purchasetest.New(t)
	res := p2Must(t, "save PUNE-WH on PRJ-TECHPARK")(p2Create(t, env, env.Arjun,
		p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-TECHPARK"), p2RefLines(t, env)))
	if got := p2Warnings(t, env, env.Arjun, res.ID); !slices.Equal(got, []string{p2MsgStateWarning}) {
		t.Errorf("PUNE-WH on PRJ-TECHPARK: OrderWarnings = %q, want [%q]", got, p2MsgStateWarning)
	}

	// Priya (PRJ-METRO only) can't read it.
	if got, err := env.Service.OrderWarnings(t.Context(), env.Priya, res.ID); err == nil || got != nil ||
		(!errors.Is(err, auth.ErrNotFound) && !errors.Is(err, doctype.ErrNotFound)) {
		t.Errorf("OrderWarnings for Priya on a PRJ-TECHPARK PO = %q, %v; want nil and not found", got, err)
	}

	for _, c := range []struct{ site, project string }{{"METRO-SITE", "PRJ-METRO"}, {"TECH-SITE", "PRJ-TECHPARK"}} {
		r := p2Must(t, c.site)(p2Create(t, env, env.Arjun, p2Header(t, env, "SHREE", c.site, c.project), p2RefLines(t, env)))
		if got := p2Warnings(t, env, env.Arjun, r.ID); len(got) != 0 {
			t.Errorf("%s on %s: OrderWarnings = %q, want none", c.site, c.project, got)
		}
	}

	p2Must(t, "project to PRJ-METRO")(p2Save(t, env, env.Arjun, res.ID, map[string]string{"project_id": masterID(t, env.DB, "project", "PRJ-METRO")}, nil))
	if got := p2Warnings(t, env, env.Arjun, res.ID); len(got) != 0 {
		t.Errorf("PUNE-WH on PRJ-METRO: OrderWarnings = %q, want none", got)
	}

	// Order: per-line Already received first, then the state warning.
	h := p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-TECHPARK")
	h["earlier_po_no"] = "P-17"
	l := p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5")
	l["already_received"] = "12"
	both := p2Must(t, "cutover PUNE-WH on PRJ-TECHPARK")(p2Create(t, env, env.Arjun, h, []map[string]string{l}))
	want := []string{"Line 1: Already received is more than the quantity.", p2MsgStateWarning}
	if got := p2Warnings(t, env, env.Arjun, both.ID); !slices.Equal(got, want) {
		t.Errorf("OrderWarnings = %q, want %q in that order", got, want)
	}
}
