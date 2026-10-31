package app_test

import (
	"encoding/csv"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// CD-327 (3): the app gives the approval inbox docscreen's party check. A
// fixture.Order whose Module hides its party field (supplier_id) on the
// list, asked as the list asks (Listing, no View, the viewer), shows no
// party on the inbox card or in My requests (CD-334); the same Order with a Module that hides it only
// off the list, and with no Module, shows it.
func TestApp_InboxPartyFollowsListHide(t *testing.T) {
	const party = "SHREE Shree Steel Traders"
	var admin auth.Principal
	listHide := func(c docscreen.Ctx, f doctype.Field) bool {
		return c.Listing && c.View == nil && c.Principal.ID == admin.ID && f.Name == "supplier_id"
	}
	offListHide := func(c docscreen.Ctx, f doctype.Field) bool { return !c.Listing && f.Name == "supplier_id" }
	for _, c := range []struct {
		name string
		mods map[string]docscreen.Module
		want string
	}{
		{"no module", nil, party},
		{"hidden off the list only", map[string]docscreen.Module{fixture.Order: {Hide: offListHide}}, party},
		{"hidden on the list", map[string]docscreen.Module{fixture.Order: {Hide: listHide}}, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, func(cfg *app.Config) { cfg.Modules = c.mods })
			admin = w.firstAdmin()
			ctx := auth.WithPrincipal(t.Context(), admin)
			m := fixture.SeedMasters(t, w.app.DB())
			if _, err := w.app.Approvals().CreateRule(ctx, admin, approval.RuleInput{
				DocType: fixture.Order, ApproverType: approval.ApproverRole, Role: "Administrator", AllowSelfApproval: true,
			}); err != nil {
				t.Fatalf("approval rule on %s: %v", fixture.Order, err)
			}
			res, err := w.app.Engine().Do(ctx, admin, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
				Values: map[string]string{"supplier_id": strconv.FormatInt(m.Party["SHREE"], 10), "order_date": "2026-10-24", "amount": "60000"},
				Lines:  []map[string]string{{"item_id": strconv.FormatInt(m.Item["CEMENT-OPC"], 10), "qty": "10", "rate": "350"}},
			})
			if err != nil {
				t.Fatalf("saving an Order draft: %v", err)
			}
			if _, err := w.app.Engine().Do(ctx, admin, fixture.Order, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
				t.Fatalf("submitting the Order: %v", err)
			}
			inbox, err := w.app.Approvals().Inbox(ctx, admin, types.Page{})
			if err != nil {
				t.Fatalf("Inbox: %v", err)
			}
			if len(inbox.Rows) != 1 || inbox.Rows[0].DocID != res.ID {
				t.Fatalf("admin's inbox = %+v, want the Order %d only", inbox.Rows, res.ID)
			}
			if got := inbox.Rows[0].Party; got != c.want {
				t.Errorf("inbox card Party = %q, want %q", got, c.want)
			}
			mine, err := w.app.Approvals().MyRequests(ctx, admin, types.Page{})
			if err != nil {
				t.Fatalf("MyRequests: %v", err)
			}
			if len(mine.Rows) != 1 || mine.Rows[0].Party != c.want {
				t.Errorf("My requests = %+v, want the Order with Party %q", mine.Rows, c.want)
			}
		})
	}
}

// CD-334 (3), (4): the CSV export asks the list's Hide question (Listing,
// no View). A field it hides has no column, and a URL filter on it is
// dropped before the query runs; the control run without the hook keeps the
// column and the filter, which leaves out the draft.
func TestApp_ExportFollowsListHide(t *testing.T) {
	hideState := func(c docscreen.Ctx, f doctype.Field) bool { return c.Listing && c.View == nil && f.Name == "state" }
	for _, c := range []struct {
		name   string
		mods   map[string]docscreen.Module
		column bool
		rows   int
	}{
		{"no module", nil, true, 0},
		{"state hidden on the list", map[string]docscreen.Module{fixture.Order: {Hide: hideState}}, false, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, func(cfg *app.Config) { cfg.Modules = c.mods })
			admin := w.firstAdmin()
			ctx := auth.WithPrincipal(t.Context(), admin)
			m := fixture.SeedMasters(t, w.app.DB())
			if _, err := w.app.Engine().Do(ctx, admin, fixture.Order, 0, doc.ActionSave, doc.ActionInput{
				Values: map[string]string{"supplier_id": strconv.FormatInt(m.Party["SHREE"], 10), "order_date": "2026-10-24"},
			}); err != nil {
				t.Fatalf("saving an Order draft: %v", err)
			}
			r := w.signedIn(adminEmail).get(orderPath + "/export.csv?f=state:=:approved")
			wantStatus(t, "export", r, http.StatusOK)
			all, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(r.body, byteOrderMark))).ReadAll()
			if err != nil || len(all) == 0 {
				t.Fatalf("export CSV %q: %v", r.body, err)
			}
			if !slices.Contains(all[0], "Supplier") {
				t.Fatalf("export header %q, want a Supplier column", all[0])
			}
			if got := slices.Contains(all[0], "State"); got != c.column {
				t.Errorf("export header %q: a State column = %v, want %v", all[0], got, c.column)
			}
			if got := len(all) - 1; got != c.rows {
				t.Errorf("export rows = %d, want %d", got, c.rows)
			}
		})
	}
}
