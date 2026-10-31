package purchase_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-048 (PO spec §20.2, §20.3, §26; MOD-PO P5 "Submit goes straight to
// approved with a number", "dateRequest returns request_date"): Suresh
// submits draft PR-A (2 rows) and it moves straight to approved, with no
// approval request or history, numbered PR/26-27/00001. The FY of the number
// comes from request_date, not the clock (24-10-2026): a request dated
// 31-03-2026 is PR/25-26/00001, one dated 01-04-2026 PR/26-27/00002.
func TestRequest_SubmitNumbers(t *testing.T) {
	env := purchasetest.New(t)
	for _, c := range []struct {
		date, want string
	}{
		{p5Today, "PR/26-27/00001"},
		{"2026-03-31", "PR/25-26/00001"},
		{"2026-04-01", "PR/26-27/00002"},
	} {
		h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
		h["request_date"] = c.date
		draft := p5Must(t, "create PR-A dated "+c.date)(p5Create(t, env, env.Suresh, h, []map[string]string{
			p5Line(t, env, "MS-ANGLE-50", "KGS", "100"),
			p5Line(t, env, "BOLT-M12", "NOS", "40"),
		}))
		if draft.Number != "" || draft.State != doctype.StateDraft {
			t.Fatalf("draft dated %s = %s %q, want an unnumbered draft", c.date, draft.State, draft.Number)
		}
		before := p5CountAll(t, env)
		res := p5Must(t, "submit PR-A dated "+c.date)(p5Do(t, env, env.Suresh, draft.ID, doctype.ActionSubmit, ""))
		if res.State != doctype.StateApproved || res.Number != c.want {
			t.Errorf("submit dated %s = %s %q, want approved %q", c.date, res.State, res.Number, c.want)
		}
		d := p5Load(t, env, purchase.RequestDocType, draft.ID)
		if d.State != doctype.StateApproved || d.Number != c.want || len(d.Lines) != 2 {
			t.Errorf("stored request dated %s = %s %q with %d lines, want approved %q with 2", c.date, d.State, d.Number, len(d.Lines), c.want)
		}
		after := p5CountAll(t, env)
		if after.approvals != before.approvals || after.history != before.history {
			t.Errorf("submit dated %s: approval requests %d → %d, history %d → %d; want none (not approvable)",
				c.date, before.approvals, after.approvals, before.history, after.history)
		}
	}
}

// MOD-PO P5 "Request date and needed-by rules, with their texts" (PO spec
// §20.2): on the frozen clock (24-10-2026 IST) a request date after today is
// refused with "Request date can't be in the future." and a needed-by date
// before the request date with "Needed by can't be before the request
// date.", on create and on a saved draft, writing nothing. Past dates and a
// needed-by on or after the request date save. Today is the IST date: at
// 00:30 IST on 25-10-2026 that date saves and 26-10-2026 is refused.
func TestRequest_DateRules(t *testing.T) {
	env := purchasetest.New(t)
	future := doctype.FieldError{Field: "request_date", Message: p5MsgDateFuture}
	early := doctype.FieldError{Field: "required_by", Message: p5MsgNeededBy}
	create := func(requestDate, neededBy string) error {
		t.Helper()
		h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
		h["request_date"], h["required_by"] = requestDate, neededBy
		_, err := p5Create(t, env, env.Suresh, h, p5Lines(t, env))
		return err
	}
	refused := func(what string, err error, want doctype.FieldError, before p5Counts) {
		t.Helper()
		p5WantInvalid(t, what, err, want)
		p5SameCounts(t, what, before, env)
	}

	refused("request date 25-10-2026", create("2026-10-25", ""), future, p5CountAll(t, env))
	for _, d := range []string{p5Today, "2026-04-01", "2026-03-31", "2025-04-01"} {
		if err := create(d, ""); err != nil {
			t.Errorf("request date %s: %v %+v, want saved (past dates are allowed)", d, err, p5Fields(err))
		}
	}
	refused("needed by 23-10-2026", create(p5Today, "2026-10-23"), early, p5CountAll(t, env))
	for _, d := range []string{p5Today, "2026-11-30"} {
		if err := create(p5Today, d); err != nil {
			t.Errorf("needed by %s: %v %+v, want saved", d, err, p5Fields(err))
		}
	}

	// A saved draft: each refusal leaves the stored dates as they were.
	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	for _, c := range []struct {
		what   string
		values map[string]string
		want   doctype.FieldError
	}{
		{"save request date 25-10-2026", map[string]string{"request_date": "2026-10-25"}, future},
		{"save needed by 23-10-2026", map[string]string{"request_date": p5Today, "required_by": "2026-10-23"}, early},
	} {
		before := p5CountAll(t, env)
		version := p5Version(t, env, draft.ID)
		_, err := p5Save(t, env, env.Suresh, draft.ID, c.values, nil)
		refused(c.what, err, c.want, before)
		d := p5Load(t, env, purchase.RequestDocType, draft.ID)
		if got := p5Plain(d.Header["request_date"]); got != p5Today {
			t.Errorf("%s: stored request_date %s, want %s kept", c.what, got, p5Today)
		}
		if got := p5Plain(d.Header["required_by"]); got != "<nil>" && got != "" {
			t.Errorf("%s: stored required_by %s, want blank kept", c.what, got)
		}
		if d.Version != version {
			t.Errorf("%s: version %d, want %d unchanged", c.what, d.Version, version)
		}
	}
	// Undo: the corrected dates save on the same draft.
	p5Must(t, "save corrected dates")(p5Save(t, env, env.Suresh, draft.ID, map[string]string{"request_date": p5Today, "required_by": "2026-10-30"}, nil))
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["required_by"]); got != "2026-10-30" {
		t.Errorf("required_by after the corrected save = %s, want 2026-10-30", got)
	}

	// Submit re-runs the rules (D-APR-15): a draft dated today submits.
	p5Must(t, "submit the corrected draft")(p5Do(t, env, env.Suresh, draft.ID, doctype.ActionSubmit, ""))

	// 19:00 UTC on 24 Oct is 00:30 IST on 25 Oct.
	env.SetNow(t, time.Date(2026, 10, 24, 19, 0, 0, 0, time.UTC))
	if err := create("2026-10-25", ""); err != nil {
		t.Errorf("request date 25-10-2026 after midnight IST: %v %+v, want saved", err, p5Fields(err))
	}
	refused("request date 26-10-2026 after midnight IST", create("2026-10-26", ""), future, p5CountAll(t, env))
}

// MOD-PO P5 "Site and project rules through P2's checkSiteProject (the PO's
// texts)" (PO spec §20.2, §4; PO-010's rule on a request): a site on another
// project is refused with "Site {code} is on project {code}. Pick that
// project or another site.", on create and on a saved draft, writing
// nothing. A site with no project (PUNE-WH) takes any project (PR-B).
func TestRequest_SiteProjectMismatch(t *testing.T) {
	env := purchasetest.New(t)
	for _, c := range []struct {
		who            string
		site, project  string
		wantSite, proj string
	}{
		{"Suresh", "METRO-SITE", "PRJ-TECHPARK", "METRO-SITE", "PRJ-METRO"},
		{"Ravi", "TECH-SITE", "PRJ-METRO", "TECH-SITE", "PRJ-TECHPARK"},
	} {
		p := env.Suresh
		if c.who == "Ravi" {
			p = env.Ravi
		}
		what := fmt.Sprintf("%s creates %s on %s", c.who, c.site, c.project)
		before := p5CountAll(t, env)
		_, err := p5Create(t, env, p, p5Header(t, env, c.site, c.project), p5Lines(t, env))
		p5WantInvalid(t, what, err, doctype.FieldError{Field: p5RequestFieldSet, Message: fmt.Sprintf(p5MsgSiteProject, c.wantSite, c.proj)})
		p5SameCounts(t, what, before, env)
	}

	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	before := p5CountAll(t, env)
	_, err := p5Save(t, env, env.Suresh, draft.ID, map[string]string{"project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK")}, nil)
	p5WantInvalid(t, "save METRO-SITE on PRJ-TECHPARK", err,
		doctype.FieldError{Field: p5RequestFieldSet, Message: fmt.Sprintf(p5MsgSiteProject, "METRO-SITE", "PRJ-METRO")})
	p5SameCounts(t, "save METRO-SITE on PRJ-TECHPARK", before, env)
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["project_id"]); got != masterID(t, env.DB, "project", "PRJ-METRO") {
		t.Errorf("project_id after the refusal = %s, want PRJ-METRO kept", got)
	}

	// PR-B: PUNE-WH has no project, so any project is valid.
	if _, err := p5Create(t, env, env.Deepak, p5Header(t, env, "PUNE-WH", "PRJ-TECHPARK"), p5Lines(t, env)); err != nil {
		t.Errorf("PR-B (PUNE-WH on PRJ-TECHPARK): %v %+v, want saved", err, p5Fields(err))
	}
}

// MOD-PO P5 "the project default from the site" (PO spec §20.2 "Defaults
// from the site, as on the PO", §3.1): a blank project is filled from the
// site's project, for an unrestricted user (Ravi) and for Suresh, who is
// restricted on sites only. A site with no project leaves it blank, a given
// project is kept, and clearing it on a saved draft fills it again.
func TestRequest_ProjectDefaultsFromSite(t *testing.T) {
	env := purchasetest.New(t)
	metro, techpark := masterID(t, env.DB, "project", "PRJ-METRO"), masterID(t, env.DB, "project", "PRJ-TECHPARK")
	for _, c := range []struct {
		who                 string
		site, project, want string
	}{
		{"Ravi", "METRO-SITE", "", metro},
		{"Ravi", "TECH-SITE", "", techpark},
		{"Ravi", "PUNE-WH", "", "<nil>"},
		{"Ravi", "PUNE-WH", "PRJ-TECHPARK", techpark},
		{"Suresh", "METRO-SITE", "", metro},
	} {
		p := env.Ravi
		if c.who == "Suresh" {
			p = env.Suresh
		}
		res, err := p5Create(t, env, p, p5Header(t, env, c.site, c.project), p5Lines(t, env))
		if err != nil {
			t.Errorf("%s: %s with project %q: %v %+v", c.who, c.site, c.project, err, p5Fields(err))
			continue
		}
		if got := p5Plain(p5Load(t, env, purchase.RequestDocType, res.ID).Header["project_id"]); got != c.want {
			t.Errorf("%s: %s with project %q: project_id = %s, want %s", c.who, c.site, c.project, got, c.want)
		}
	}

	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	p5Must(t, "clear the project")(p5Save(t, env, env.Suresh, draft.ID, map[string]string{"project_id": ""}, nil))
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["project_id"]); got != metro {
		t.Errorf("project_id after clearing it = %s, want PRJ-METRO from METRO-SITE", got)
	}
}

// MOD-PO P5 "Item defaults; uom.CheckLineQty" (PO spec §20.2 lines): a blank
// description takes the item's name and a blank unit the item's stock UoM,
// on the first save and again after clearing them; a given description is
// kept. A fractional quantity in a whole-number unit is refused with
// MST-086's text through uom.CheckLineQty (CD-264 (8), CD-277 (4)), writing
// nothing, and a whole quantity saves.
func TestRequest_LineDefaults(t *testing.T) {
	env := purchasetest.New(t)
	kgs := masterID(t, env.DB, "uom", "KGS")
	blank := p5Line(t, env, "MS-ANGLE-50", "", "10")
	blank["description"] = ""
	given := p5Line(t, env, "WIRE-18G", "KGS", "5")
	given["description"] = "Binding wire, 25 kg coils"
	given["note"] = "Tata brand"

	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft(), purchasetest.RequestLines(blank, given))
	check := func(what string) {
		t.Helper()
		d := p5Load(t, env, purchase.RequestDocType, draft.ID)
		if len(d.Lines) != 2 {
			t.Fatalf("%s: %d lines, want 2", what, len(d.Lines))
		}
		for i, want := range []map[string]string{
			{"description": "MS Angle 50x50x5", "uom_id": kgs},
			{"description": "Binding wire, 25 kg coils", "uom_id": kgs, "note": "Tata brand"},
		} {
			for k, w := range want {
				if got := p5Plain(d.Lines[i].Values[k]); got != w {
					t.Errorf("%s: line %d %s = %s, want %s", what, i+1, k, got, w)
				}
			}
		}
	}
	check("first save")
	p5Must(t, "clear the defaults")(p5Save(t, env, env.Suresh, draft.ID, nil, []map[string]string{blank, given}))
	check("after clearing")

	before := p5CountAll(t, env)
	_, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p5Line(t, env, "BOLT-M12", "NOS", "2.5")})
	p5WantInvalid(t, "2.5 Nos", err, doctype.FieldError{Field: "qty", Line: 1, Message: fmt.Sprintf(p5MsgWholeNumber, 1, "2.5", "NOS")})
	p5SameCounts(t, "2.5 Nos", before, env)
	if _, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
		[]map[string]string{p5Line(t, env, "BOLT-M12", "NOS", "3")}); err != nil {
		t.Errorf("3 Nos: %v %+v, want saved", err, p5Fields(err))
	}
}
