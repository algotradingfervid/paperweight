package expense_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E13: the form audit's claim-line rules (CD-337 (5)): a line's project
// comes from its site, its HSN/SAC is checked like the expense type's
// default, and a supplier GSTIN that is one of our branch GSTINs warns.

// e13Exec runs q in a write tx, for setups no service offers (a master
// moved under a submitted claim, a branch disabled, a bad type default).
func e13Exec(t *testing.T, e *expensetest.Env, q string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := e.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, q, args...)
		return err
	}); err != nil {
		t.Fatalf("%s %v: %v", q, args, err)
	}
}

// e13LineProject is line lineNo of table's document id: its project id (0
// for none) and its project snapshot.
func e13LineProject(t *testing.T, e *expensetest.Env, table string, id int64, lineNo int) (int64, string) {
	t.Helper()
	var project int64
	var snap string
	e2Scan(t, e, `SELECT COALESCE(project_id, 0), snap_project_code FROM `+table+` WHERE parent_id = ? AND line_no = ?`,
		[]any{id, lineNo}, &project, &snap)
	return project, snap
}

// e13WantFieldError fails unless err carries exactly one field error, text on
// field and line.
func e13WantFieldError(t *testing.T, err error, field string, line int, text string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v, want field error %q", err, text)
	}
	want := []doctype.FieldError{{Field: field, Line: line, Message: text}}
	if got := he.FieldErrors(); !slices.Equal(got, want) {
		t.Errorf("field errors = %+v, want %+v", got, want)
	}
}

// TestExpE13_LineProjectFromSite: on a draft claim line, a site on a project
// fills a blank project and refuses a different one (PO's rule, line
// prefixed); a site with no project, or no site, keeps the line's project.
// The defaulted project reaches the snapshot.
func TestExpE13_LineProjectFromSite(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	metro, tech := e.E8Project(t, "PRJ-METRO"), e.E8Project(t, "PRJ-TECHPARK")
	metroSite, techSite, wh := e2ID(e.E8Site(t, "METRO-SITE")), e2ID(e.E8Site(t, "TECH-SITE")), e2ID(e.E8Site(t, "PUNE-WH"))

	c := e.DraftClaim(t, deepak, nil,
		V{"site_id": metroSite},                            // blank: defaulted
		V{"site_id": metroSite, "project_id": e2ID(metro)}, // same: kept
		V{"project_id": e2ID(tech)},                        // no site: kept
		V{"site_id": wh, "project_id": e2ID(tech)},         // site without a project: kept
		V{"site_id": wh},                                   // site without a project: stays blank
		V{"site_id": techSite},                             // a second blank: defaulted too
	)
	for _, w := range []struct {
		line    int
		project int64
		snap    string
	}{{1, metro, "PRJ-METRO"}, {2, metro, "PRJ-METRO"}, {3, tech, "PRJ-TECHPARK"}, {4, tech, "PRJ-TECHPARK"}, {5, 0, ""}, {6, tech, "PRJ-TECHPARK"}} {
		if p, s := e13LineProject(t, e, "expense_claim_line", c.ID, w.line); p != w.project || s != w.snap {
			t.Errorf("line %d project = %d %q, want %d %q", w.line, p, s, w.project, w.snap)
		}
	}

	// A different project is refused on its line.
	const wantMetro = "Line 2: Site METRO-SITE is on project PRJ-METRO. Pick that project or another site."
	var err error
	e.WantRefused(t, wantMetro, func() error {
		_, err = e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil,
			V{"site_id": wh}, V{"site_id": metroSite, "project_id": e2ID(tech)}))
		return err
	})
	e13WantFieldError(t, err, "project_id", 2, wantMetro)

	// Stale: the project defaulted from METRO-SITE, then the site changes to
	// TECH-SITE. The stored project is now a different one and is refused;
	// cleared, it defaults to TECH-SITE's project.
	s := e.DraftClaim(t, deepak, nil, V{"site_id": metroSite})
	const wantTech = "Line 1: Site TECH-SITE is on project PRJ-TECHPARK. Pick that project or another site."
	e.WantRefused(t, wantTech, func() error {
		_, err = e.Edit(t, deepak, expense.ClaimDocType, s.ID, nil, V{"site_id": techSite})
		return err
	})
	e13WantFieldError(t, err, "project_id", 1, wantTech)
	e2Must(t, "change the site and clear the project")(e.Edit(t, deepak, expense.ClaimDocType, s.ID, nil, V{"site_id": techSite, "project_id": ""}))
	if p, snap := e13LineProject(t, e, "expense_claim_line", s.ID, 1); p != tech || snap != "PRJ-TECHPARK" {
		t.Errorf("after the site change line 1 project = %d %q, want %d PRJ-TECHPARK", p, snap, tech)
	}
}

// TestExpE13_AdvanceLineProjectFromSite: advance lines follow the same rule.
func TestExpE13_AdvanceLineProjectFromSite(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	metro, tech := e.E8Project(t, "PRJ-METRO"), e.E8Project(t, "PRJ-TECHPARK")
	metroSite, techSite := e2ID(e.E8Site(t, "METRO-SITE")), e2ID(e.E8Site(t, "TECH-SITE"))

	a := e.DraftAdvance(t, deepak, nil, V{"project_id": e2ID(tech)}, V{"site_id": metroSite}, V{"site_id": techSite})
	if p, snap := e13LineProject(t, e, "employee_advance_line", a.ID, 2); p != metro || snap != "PRJ-METRO" {
		t.Errorf("advance line 2 project = %d %q, want %d PRJ-METRO", p, snap, metro)
	}
	if p, snap := e13LineProject(t, e, "employee_advance_line", a.ID, 3); p != tech || snap != "PRJ-TECHPARK" {
		t.Errorf("advance line 3 project = %d %q, want %d PRJ-TECHPARK", p, snap, tech)
	}
	if p, _ := e13LineProject(t, e, "employee_advance_line", a.ID, 1); p != tech {
		t.Errorf("advance line 1 (no site) project = %d, want %d", p, tech)
	}

	const want = "Line 2: Site METRO-SITE is on project PRJ-METRO. Pick that project or another site."
	var err error
	e.WantRefused(t, want, func() error {
		_, err = e.Create(deepak, expense.AdvanceDocType, e.AdvanceInput(t, deepak, nil,
			V{}, V{"site_id": metroSite, "project_id": e2ID(tech)}))
		return err
	})
	e13WantFieldError(t, err, "project_id", 2, want)
}

// TestExpE13_SiteProjectDraftOnly: the rule reads the site master, so like
// the snapshots it runs only while the claim is a draft: a site moved to
// another project after submit neither blocks the approval nor rewrites the
// line.
func TestExpE13_SiteProjectDraftOnly(t *testing.T) {
	e := expensetest.New(t)
	deepak, priya := e.P(t, e2Deepak), e.P(t, e2Priya)
	type V = expensetest.Values
	metro, tech := e.E8Project(t, "PRJ-METRO"), e.E8Project(t, "PRJ-TECHPARK")
	metroSite := e.E8Site(t, "METRO-SITE")

	c := e.DraftClaim(t, deepak, nil, V{"site_id": e2ID(metroSite)})
	if res := e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Fatalf("submit: state %q, want pending", res.State)
	}
	e13Exec(t, e, `UPDATE site SET project_id = ? WHERE id = ?`, tech, metroSite)
	if res := e6Do(t, e, priya, expense.ClaimDocType, c.ID, doc.ActionApprove, ""); res.State != doc.StateApproved {
		t.Fatalf("approve: state %q, want approved", res.State)
	}
	if p, _ := e13LineProject(t, e, "expense_claim_line", c.ID, 1); p != metro {
		t.Errorf("approved line project = %d, want the submitted %d", p, metro)
	}
}

// TestExpE13_LineHSN: a non-blank line HSN/SAC is checked with the expense
// type master's rule (99… is a SAC, else a goods HSN), gstin's text behind
// the line prefix; blank passes. The type's default is filled first and is
// checked too.
func TestExpE13_LineHSN(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	misc := e2Type(t, e, "SITE-MISC") // no default HSN/SAC
	// The seed's types carry no defaults; TRAVEL gets the CSV's.
	e13Exec(t, e, `UPDATE expense_type SET default_hsn_sac = '996411' WHERE code = 'TRAVEL'`)

	c := e.DraftClaim(t, deepak, nil,
		V{"hsn_sac": "2710"},                      // goods
		V{"hsn_sac": "996411"},                    // SAC
		V{"hsn_sac": " 996311 "},                  // SAC with spaces
		V{"expense_type_id": misc, "hsn_sac": ""}, // blank, no default
		V{"hsn_sac": ""},                          // blank: TRAVEL's 996411
	)
	if l := e2ReadLine(t, e, c.ID, 5); l.hsnSAC != "996411" {
		t.Errorf("line 5 HSN/SAC = %q, want TRAVEL's 996411", l.hsnSAC)
	}

	for _, tc := range []struct{ code, want string }{
		{"271", "Line 2: HSN code must be 4, 6 or 8 digits."},
		{"99641", "Line 2: SAC code must be 6 digits starting with 99."},
	} {
		var err error
		e.WantRefused(t, tc.want, func() error {
			_, err = e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, V{}, V{"hsn_sac": tc.code}))
			return err
		})
		e13WantFieldError(t, err, "hsn_sac", 2, tc.want)
	}

	// A bad default (set past the master's own check) is filled, then refused.
	e13Exec(t, e, `UPDATE expense_type SET default_hsn_sac = '123' WHERE code = 'SITE-MISC'`)
	const wantDefault = "Line 1: HSN code must be 4, 6 or 8 digits."
	e.WantRefused(t, wantDefault, func() error {
		_, err := e.Create(deepak, expense.ClaimDocType, e.ClaimInput(t, deepak, nil, V{"expense_type_id": misc, "hsn_sac": ""}))
		return err
	})
}

// TestExpE13_OwnBranchGSTIN: a line whose supplier GSTIN is an enabled
// branch's GSTIN (as gstin.Normalize stores it: case and outer space don't
// matter) warns, in line order; another GSTIN, a disabled branch's, or none
// (even beside a branch with none) does not. The claim still submits.
func TestExpE13_OwnBranchGSTIN(t *testing.T) {
	e := expensetest.New(t)
	deepak := e.P(t, e2Deepak)
	type V = expensetest.Values
	// An enabled branch with no GSTIN: a line with none doesn't match it.
	e13Exec(t, e, `INSERT INTO branch (code, name, state_code, created_at, updated_at)
		VALUES ('NOGST', 'Unregistered Office', '27', '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')`)

	c := e.DraftClaim(t, deepak, nil,
		V{"supplier_gstin": " 27aafcv2449g1z7 "},                    // PUNE, case and space variant
		V{"supplier_gstin": "27AABCS1429B1ZU", "amount": "1001.00"}, // a vendor
		V{"supplier_gstin": "29AAFCV2449G1Z3", "amount": "1002.00"}, // BLR
		V{"amount": "1003.00"},                                      // none
	)
	want := []string{
		"Line 1: Supplier GSTIN 27AAFCV2449G1Z7 is our own branch's GSTIN.",
		"Line 3: Supplier GSTIN 29AAFCV2449G1Z3 is our own branch's GSTIN.",
	}
	if got := e6Warnings(t, e, deepak, c.ID); !slices.Equal(got, want) {
		t.Errorf("ClaimWarnings =\n  %q\nwant\n  %q", got, want)
	}

	// A disabled branch isn't ours for this check (BranchGSTINs counts
	// enabled branches only).
	e13Exec(t, e, `UPDATE branch SET disabled = 1 WHERE code = 'BLR'`)
	if got := e6Warnings(t, e, deepak, c.ID); !slices.Equal(got, want[:1]) {
		t.Errorf("with BLR disabled ClaimWarnings =\n  %q\nwant\n  %q", got, want[:1])
	}

	// A warning never refuses.
	if res := e6Do(t, e, deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, ""); res.State != doc.StatePending {
		t.Errorf("submit: state %q, want pending", res.State)
	}
}
