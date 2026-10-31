package purchase_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-PO P5 fix 1: Validate reads and writes only the request it is saving,
// keeps the stored project when the site and project check refuses on a
// step save (CD-302, CD-205/207), and reports line errors in line order.

// MOD-PO P5 "the project default from the site": saving one draft writes its
// own project only. A second draft on TECH-SITE/PRJ-TECHPARK keeps
// PRJ-TECHPARK when a METRO-SITE draft with a blank project is saved and its
// project defaulted to PRJ-METRO.
func TestRequest_ProjectDefaultOnlyOwnRequest(t *testing.T) {
	env := purchasetest.New(t)
	metro, techpark := masterID(t, env.DB, "project", "PRJ-METRO"), masterID(t, env.DB, "project", "PRJ-TECHPARK")
	a := p5Must(t, "Ravi's METRO-SITE draft")(p5Create(t, env, env.Ravi, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), p5Lines(t, env)))
	b := p5Must(t, "Ravi's TECH-SITE draft")(p5Create(t, env, env.Ravi, p5Header(t, env, "TECH-SITE", "PRJ-TECHPARK"), p5Lines(t, env)))
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, b.ID).Header["project_id"]); got != techpark {
		t.Fatalf("TECH-SITE draft project_id = %s before the save, want PRJ-TECHPARK %s", got, techpark)
	}
	p5Must(t, "clear the METRO-SITE draft's project")(p5Save(t, env, env.Ravi, a.ID, map[string]string{"project_id": ""}, nil))
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, a.ID).Header["project_id"]); got != metro {
		t.Errorf("METRO-SITE draft project_id = %s, want PRJ-METRO %s from its site", got, metro)
	}
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, b.ID).Header["project_id"]); got != techpark {
		t.Errorf("TECH-SITE draft project_id = %s after saving the other draft, want PRJ-TECHPARK %s unchanged", got, techpark)
	}
}

// MOD-PO P5 "uom.CheckLineQty" (CD-278): Validate checks only the saved
// request's lines. Another (approved) request's BOLT-M12 Box line loses its
// conversion in the item master; saving a draft with a valid line then
// succeeds with no error for that line, and the other line is unchanged.
func TestRequest_ValidateOnlyOwnLines(t *testing.T) {
	env := purchasetest.New(t)
	other := purchasetest.NewRequest(t, env, purchasetest.RequestLines(p5Line(t, env, "BOLT-M12", "BOX", "2")))
	if other.State != doctype.StateApproved {
		t.Fatalf("other request = %s, want approved", other.State)
	}
	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	beforeLine := p5Load(t, env, purchase.RequestDocType, other.ID).Lines
	if len(beforeLine) != 1 {
		t.Fatalf("other request has %d lines, want 1", len(beforeLine))
	}
	p2DropBox(t, env)

	if _, err := p5Save(t, env, env.Suresh, draft.ID, map[string]string{"purpose": "Angles for the deck"}, nil); err != nil {
		t.Errorf("save the MS-ANGLE-50 draft after BOLT-M12's Box went: %v %+v, want saved (the Box line is another request's)", err, p5Fields(err))
	}
	after := p5Load(t, env, purchase.RequestDocType, other.ID).Lines
	if len(after) != 1 {
		t.Fatalf("other request has %d lines after the save, want 1", len(after))
	}
	for _, k := range []string{"item_id", "description", "uom_id", "qty"} {
		if g, w := p5Plain(after[0].Values[k]), p5Plain(beforeLine[0].Values[k]); g != w {
			t.Errorf("other request's line %s = %s after the save, want %s unchanged", k, g, w)
		}
	}
}

// MOD-PO P5 "Site and project rules" on a step save (CD-302; CD-205, CD-207):
// METRO-SITE moves to PRJ-DEPOT after a draft on METRO-SITE/PRJ-METRO is
// saved. A save with Only naming purpose commits with the site and project
// error as a problem, and the stored project stays PRJ-METRO (the project
// default is skipped, not cleared).
func TestRequest_StepSaveSiteProjectKeepsProject(t *testing.T) {
	env := purchasetest.New(t)
	ctx := t.Context()
	draft := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	metro := masterID(t, env.DB, "project", "PRJ-METRO")
	if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["project_id"]); got != metro {
		t.Fatalf("draft project_id = %s, want PRJ-METRO %s", got, metro)
	}

	sites := site.New(p2Kit(env))
	var st site.Site
	if err := env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		st, err = sites.Get(ctx, tx, p2Int(t, masterID(t, env.DB, "site", "METRO-SITE")))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	depot, err := project.New(p2Kit(env)).Create(ctx, env.Admin, project.Input{Code: "PRJ-DEPOT", Name: "Pune Depot Extension", BranchID: st.BranchID})
	if err != nil {
		t.Fatalf("create PRJ-DEPOT: %v", err)
	}
	if _, err := sites.Update(ctx, env.Admin, st.ID, st.Version, site.Input{Code: st.Code, Name: st.Name, BranchID: st.BranchID,
		ProjectID: depot.ID, Type: st.Type}); err != nil {
		t.Fatalf("move METRO-SITE to PRJ-DEPOT: %v", err)
	}

	res, err := env.Engine.Do(ctx, env.Suresh, purchase.RequestDocType, draft.ID, doctype.ActionSave, doc.ActionInput{
		Version: p5Version(t, env, draft.ID), Values: map[string]string{"purpose": "Site moved"},
		Lines: p5StoredLines(t, env, draft.ID), Only: []string{"purpose"}})
	p2WantProblems(t, "purpose save after the site moved", res, err, doctype.FieldError{Field: p5RequestFieldSet,
		Message: fmt.Sprintf(p5MsgSiteProject, "METRO-SITE", "PRJ-DEPOT")})
	d := p5Load(t, env, purchase.RequestDocType, draft.ID)
	if got := p5Plain(d.Header["purpose"]); got != "Site moved" {
		t.Errorf("purpose = %s, want the step save committed", got)
	}
	if got := p5Plain(d.Header["project_id"]); got != metro {
		t.Errorf("project_id = %s after the refused check, want PRJ-METRO %s kept (CD-302)", got, metro)
	}
}

// MOD-PO P5 "uom.CheckLineQty": two failing lines report their errors in
// line order, Line 1 before Line 2.
func TestRequest_LineErrorsInOrder(t *testing.T) {
	env := purchasetest.New(t)
	_, err := p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "BOLT-M12", "NOS", "2.5"),
		p5Line(t, env, "BOLT-M12", "NOS", "1.5"),
	})
	want := []doctype.FieldError{
		{Field: "qty", Line: 1, Message: fmt.Sprintf(p5MsgWholeNumber, 1, "2.5", "NOS")},
		{Field: "qty", Line: 2, Message: fmt.Sprintf(p5MsgWholeNumber, 2, "1.5", "NOS")},
	}
	var de *doc.Error
	if !errors.As(err, &de) || de.Kind != doc.KindInvalid {
		t.Fatalf("two fractional Nos lines: err = %v, want an invalid error with %+v", err, want)
	}
	if len(de.Fields) != len(want) {
		t.Fatalf("field errors %+v, want %+v in this order", de.Fields, want)
	}
	for i, w := range want {
		if g := de.Fields[i]; g.Field != w.Field || g.Line != w.Line || g.Message != w.Message {
			t.Errorf("field error %d = %+v, want %+v (line order)", i, g, w)
		}
	}
}

// p5StoredLines are request id's stored lines as a page posts them (ids and
// values), as p5Save builds them for nil lines.
func p5StoredLines(t *testing.T, env *purchasetest.Env, id int64) []map[string]string {
	t.Helper()
	var lines []map[string]string
	for _, l := range p5Load(t, env, purchase.RequestDocType, id).Lines {
		m := map[string]string{"id": fmt.Sprint(l.ID)}
		for _, k := range []string{"item_id", "description", "uom_id", "qty", "note"} {
			if v := p5Plain(l.Values[k]); v != "<nil>" {
				m[k] = v
			}
		}
		lines = append(lines, m)
	}
	return lines
}
