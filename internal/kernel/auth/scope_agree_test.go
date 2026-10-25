package auth_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// t6aRef builds the RecordRef of a probe row from authtest.Info, as
// CheckLink does.
func t6aRef(t *testing.T, w *authtest.World, dt string, id int64) *auth.RecordRef {
	t.Helper()
	ctx := context.Background()
	var ref auth.RecordRef
	if err := w.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := w.Info.Info(ctx, tx, dt, id)
		if err != nil {
			return err
		}
		ref = auth.RecordRef{
			DocType: dt, ID: id, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
			ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
		}
		return nil
	}); err != nil {
		t.Fatalf("ref %s %d: %v", dt, id, err)
	}
	return &ref
}

// t6aRestrictSuresh restricts Suresh as in TestRestriction_DimensionsAnd.
func t6aRestrictSuresh(t *testing.T, w *authtest.World) {
	t.Helper()
	w.Restrict(t, "suresh@vardhan.test", []string{"PRJ-METRO"}, []string{"PUNE-WH", "METRO-SITE"})
}

// Header site NULL: the line sites alone decide the strict site rule.
func TestScope_NullHeaderSiteUsesLines(t *testing.T) {
	w := authtest.NewWorld(t)
	t6aRestrictSuresh(t, w)
	priya := w.Users["priya@vardhan.test"]
	metro := w.Projects["PRJ-METRO"]

	t6InsertDoc(t, w, "probe_order", 1, priya, 0, metro, 0) // visible: line at METRO-SITE
	t6InsertLine(t, w, "probe_order_line", 11, 1, 0, w.Sites["METRO-SITE"])
	t6InsertDoc(t, w, "probe_order", 2, priya, 0, metro, 0) // hidden: line at TECH-SITE
	t6InsertLine(t, w, "probe_order_line", 21, 2, 0, w.Sites["TECH-SITE"])

	pred := t6Scope(t, w, "suresh@vardhan.test", authtest.DTOrder, auth.RightRead)
	visible := t6Visible(t, w, "probe_order", pred)
	if !slices.Contains(visible, 1) {
		t.Errorf("PO 1 (header site NULL, line at METRO-SITE) hidden; visible = %v, SQL %q", visible, pred.SQL)
	}
	if slices.Contains(visible, 2) {
		t.Errorf("PO 2 (header site NULL, line at TECH-SITE) visible; visible = %v, SQL %q", visible, pred.SQL)
	}
}

// Scope is exactly Can's flattened strict rule (plan Task 6): for every row,
// row ∈ Scope(p, dt, read) ⇔ Can(p, dt, read, ref) == nil.
func TestScope_AgreesWithCan(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	t6aRestrictSuresh(t, w)
	u := w.Users
	metro, techpark := w.Projects["PRJ-METRO"], w.Projects["PRJ-TECHPARK"]
	puneWH, metroSite, techSite := w.Sites["PUNE-WH"], w.Sites["METRO-SITE"], w.Sites["TECH-SITE"]

	// Orders: header project × header site × lines (each line is {project, site}).
	var orderIDs []int64
	id, lineID := int64(0), int64(1000)
	lineSets := [][][2]int64{
		nil,
		{{0, metroSite}},
		{{techpark, techSite}},
		{{metro, 0}},
		{{0, metroSite}, {metro, techSite}},
		{{techpark, puneWH}, {0, 0}},
	}
	for _, prj := range []int64{metro, techpark, 0} {
		for _, site := range []int64{puneWH, metroSite, techSite, 0} {
			for _, lines := range lineSets {
				id++
				t6InsertDoc(t, w, "probe_order", id, u["priya@vardhan.test"], 0, prj, site)
				for _, l := range lines {
					lineID++
					t6InsertLine(t, w, "probe_order_line", lineID, id, l[0], l[1])
				}
				orderIDs = append(orderIDs, id)
			}
		}
	}

	// Claims: creator × employee × project.
	var claimIDs []int64
	id = 0
	for _, by := range []string{"deepak@vardhan.test", "priya@vardhan.test", "suresh@vardhan.test", "ravi@vardhan.test", "kavya@vardhan.test"} {
		for _, emp := range []string{"", "EMP-003", "EMP-004", "EMP-005", "EMP-006", "EMP-007"} {
			for _, prj := range []int64{metro, techpark, 0} {
				id++
				t6InsertDoc(t, w, "probe_claim", id, u[by], w.Employees[emp], prj, 0)
				claimIDs = append(claimIDs, id)
			}
		}
	}

	tables := []struct {
		dt, table string
		ids       []int64
	}{
		{authtest.DTOrder, "probe_order", orderIDs},
		{authtest.DTClaim, "probe_claim", claimIDs},
	}
	var compared, visibleSomewhere, hiddenSomewhere int
	for _, email := range []string{"suresh@vardhan.test", "priya@vardhan.test", "ravi@vardhan.test", "deepak@vardhan.test"} {
		p := w.P(t, email)
		for _, tb := range tables {
			pred, scopeErr := w.Auth.Scope(ctx, p, tb.dt, auth.RightRead)
			var de *auth.DeniedError
			if scopeErr != nil && !errors.As(scopeErr, &de) {
				t.Fatalf("Scope(%s, %s, read) error = %v, want nil or *auth.DeniedError", email, tb.dt, scopeErr)
			}
			var visible []int64
			if scopeErr == nil {
				visible = t6Visible(t, w, tb.table, pred)
			}
			for _, rid := range tb.ids {
				inScope := scopeErr == nil && slices.Contains(visible, rid)
				ref := t6aRef(t, w, tb.dt, rid)
				canErr := w.Auth.Can(ctx, p, tb.dt, auth.RightRead, ref)
				if inScope != (canErr == nil) {
					t.Errorf("%s %s %d %+v: in Scope = %v (SQL %q, err %v), Can(read) = %v",
						email, tb.dt, rid, *ref, inScope, pred.SQL, scopeErr, canErr)
				}
				if inScope {
					visibleSomewhere++
				} else {
					hiddenSomewhere++
				}
				compared++
			}
		}
	}
	if compared == 0 || visibleSomewhere == 0 || hiddenSomewhere == 0 {
		t.Fatalf("agreement check is vacuous: compared %d, visible %d, hidden %d", compared, visibleSomewhere, hiddenSomewhere)
	}
}

// AUTH-102 shape (CD-64): a site restriction on the Site master filters its id.
func TestScope_SiteMasterIDIn(t *testing.T) {
	w := authtest.NewWorld(t)
	w.AddRole(t, "suresh@vardhan.test", "Masters Manager") // read on the Site master
	pred := t6Scope(t, w, "suresh@vardhan.test", authtest.DTSite, auth.RightRead)
	t6WantContains(t, "Suresh Site scope", pred, `"probe_site"."id" IN (`)
	want := []int64{w.Sites["PUNE-WH"], w.Sites["METRO-SITE"]}
	slices.Sort(want)
	if got := t6Visible(t, w, "probe_site", pred); !reflect.DeepEqual(got, want) {
		t.Errorf("Suresh visible sites = %v, want %v (PUNE-WH, METRO-SITE)", got, want)
	}
}

// An inapplicable right is refused even for the Administrator, as Can does.
func TestScope_AdminInapplicableRightDenied(t *testing.T) {
	w := authtest.NewWorld(t)
	_, err := w.Auth.Scope(context.Background(), w.P(t, "admin@vardhan.test"), authtest.DTItem, auth.RightSubmit)
	t5WantDenied(t, "admin Scope(Item, submit)", err, auth.RightSubmit, false, false,
		"You don't have permission to submit Item. Ask your administrator for a role that allows it.")
}
