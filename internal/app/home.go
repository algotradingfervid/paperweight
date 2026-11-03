package app

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

const (
	homeTitle = "Home"
	// homeWaitingSize and homeRecentSize are how many rows Home shows (UI v2
	// §5.12).
	homeWaitingSize = 3
	homeRecentSize  = 5
	homeMetaSep     = " · "
)

// homeTotals names the header Money field a Home row shows for each document
// DocType that has one.
var homeTotals = map[string]string{
	purchase.DocType:       "rounded_total",
	expense.ClaimDocType:   "total",
	expense.AdvanceDocType: "amount",
}

// homeColumns are Home's rows, as on the module home.
var homeColumns = []components.Column{
	{Key: "title", Label: "Number", Priority: components.PTitle, Code: true, NoWrap: true},
	{Key: "total", Label: "Total", Priority: components.PFigure, Num: true},
	{Key: "m0", Label: "Details", Priority: components.PMeta},
	{Key: "state", Label: "State", Priority: components.PState},
}

// greeting is the home pages' eyebrow by the IST hour (UI v2 §5.12 item 1).
func greeting(now time.Time, name string) string {
	part := "evening"
	switch h := now.In(types.IST).Hour(); {
	case h < 12:
		part = "morning"
	case h < 17:
		part = "afternoon"
	}
	first, _, _ := strings.Cut(name, " ")
	return "Good " + part + ", " + first
}

// homeOwn is one DocType's part of Home, read within p's read scope, so a
// document p can't read is never counted or named. drafts and redone count
// every draft and rejected row p may read, as the list the card opens shows
// them; rejected and recent are p's own.
type homeOwn struct {
	dt             *doctype.DocType
	label          string // the sidebar's plural label
	icon           string
	drafts, redone int // the draft and rejected rows, and how many are rejected
	rejected       []homeDoc
	recent         []homeDoc
	hide           func(field string) bool
}

// homeDoc is one of p's documents on Home; total is NULL when Validate
// cleared it (over the money limit, CD-302, CD-308).
type homeDoc struct {
	info    doctype.Info
	total   sql.Null[types.Money]
	reason  string
	updated string
}

// homeDocTypes is what Home covers: each docNav document DocType p may read.
func (a *App) homeDocTypes(r *http.Request, p auth.Principal) ([]homeOwn, []auth.Predicate, error) {
	deps := a.docDeps()
	var out []homeOwn
	var scopes []auth.Predicate
	for _, place := range docNav {
		if place.group == groupMasters {
			continue
		}
		dt, _ := doctype.Lookup(place.name)
		scope, err := a.auth.Scope(r.Context(), p, dt.Name, auth.RightRead)
		if errors.Is(err, auth.ErrForbidden) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		out = append(out, homeOwn{dt: dt, label: place.label, icon: place.icon, hide: deps.ListHidden(r, dt)})
		scopes = append(scopes, scope)
	}
	return out, scopes, nil
}

// scopeWhere is AUTH's read scope as a WHERE clause, the list's own filter;
// with no scope it matches every row.
func scopeWhere(scope auth.Predicate) (string, []any) {
	if scope.SQL == "" {
		return " WHERE 1", nil
	}
	return " WHERE (" + scope.SQL + ")", scope.Args
}

// readOwn fills o's counts from the rows p may read, and its rows from p's
// own documents among them.
func (a *App) readOwn(ctx context.Context, tx *db.Tx, o *homeOwn, p auth.Principal, scope auth.Predicate) error {
	where, args := scopeWhere(scope)
	count := `SELECT COUNT(*), COUNT(CASE WHEN state = 'rejected' THEN 1 END) FROM "` + o.dt.Table + `"` + where + ` AND state IN ('draft', 'rejected')`
	if err := tx.SQL().QueryRowContext(ctx, count, args...).Scan(&o.drafts, &o.redone); err != nil { //nolint:gosec // G202: a registered table and AUTH's predicate, with its args
		return err
	}
	where += " AND " + o.dt.OwnerField + " = ?"
	args = append(slices.Clip(args), int64(p.ID))
	var err error
	if o.rejected, err = a.ownRows(ctx, tx, o.dt, where+` AND state = 'rejected'`, args, homeWaitingSize); err != nil {
		return err
	}
	o.recent, err = a.ownRows(ctx, tx, o.dt, where, args, homeRecentSize)
	return err
}

// ownRows reads the latest changed rows of dt that match where, at most
// limit. Reading up to limit per DocType is enough: the merged Home list is
// no longer than that.
func (a *App) ownRows(ctx context.Context, tx *db.Tx, dt *doctype.DocType, where string, args []any, limit int) ([]homeDoc, error) {
	total := "0"
	if f, ok := homeTotals[dt.Name]; ok {
		total = `"` + f + `"`
	}
	q := `SELECT id, ` + total + `, state_reason, updated_at FROM "` + dt.Table + `"` + where +
		` ORDER BY updated_at DESC, id DESC LIMIT ` + strconv.Itoa(limit)
	rows, err := tx.SQL().QueryContext(ctx, q, args...) //nolint:gosec // G202: a registered table and field and AUTH's predicate, with its args
	if err != nil {
		return nil, err
	}
	var out []homeDoc
	for rows.Next() {
		var d homeDoc
		if err := rows.Scan(&d.info.ID, &d.total, &d.reason, &d.updated); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, d)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	// The rows are closed: Info reads on the same connection.
	for i := range out {
		info, err := a.engine.Info(ctx, tx, dt.Name, out[i].info.ID)
		if err != nil {
			return nil, err
		}
		out[i].info = info
	}
	return out, nil
}

// homeRow is a document as a Home row with meta, leaving out each value whose
// field the registered Hide hides (CD-368 (3)): the amount and the party;
// homeProps drops the state column when state is hidden. A DocType with no
// amount field, or a NULL amount, shows none.
func homeRow(dt *doctype.DocType, id int64, title, party string, total sql.Null[types.Money], state doctype.State, meta string, hide func(string) bool) components.Row {
	row := components.Row{Href: recordURL(dt.Name, id), Values: map[string]string{"title": title, "m0": meta}}
	if f, ok := homeTotals[dt.Name]; ok && total.Valid && !hide(f) {
		row.Values["total"] = types.FormatINR(total.V)
	}
	if f, ok := doc.PartyField(dt); !ok || !hide(f.Name) {
		row.Party = party
	}
	row.State, row.StateLabel = state, dt.StateLabels[state]
	return row
}

// homeProps is p's Home (CD-387 (1), UI v2 §5.12): To approve and a card per
// DocType with draft or rejected rows p may read, Waiting for you's three sources in CD-368 (1)'s
// order, p's recent documents across the DocTypes, and a tile per create.
func (a *App) homeProps(r *http.Request, p auth.Principal) (layout.HomeProps, error) {
	ctx := r.Context()
	inbox, err := a.approvals.Inbox(ctx, p, types.Page{Number: 1, Size: homeWaitingSize})
	if err != nil {
		return layout.HomeProps{}, err
	}
	own, scopes, err := a.homeDocTypes(r, p)
	if err != nil {
		return layout.HomeProps{}, err
	}
	err = a.db.Read(ctx, func(tx *db.Tx) error {
		for i := range own {
			if err := a.readOwn(ctx, tx, &own[i], p, scopes[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return layout.HomeProps{}, err
	}
	_, toSend, err := a.purchase.Waiting(ctx, p)
	if err != nil {
		return layout.HomeProps{}, err
	}

	cols := homeColumns
	if slices.ContainsFunc(own, func(o homeOwn) bool { return o.hide("state") }) {
		cols = cols[:3] // the list leaves a hidden state's column out (CD-368 (3))
	}
	hp := layout.HomeProps{
		Title:   homeTitle,
		Waiting: components.DataListProps{Label: "Waiting for you", Columns: cols},
		Recent:  components.DataListProps{Label: "Your recent documents", Columns: cols},
	}
	if inbox.Total > 0 {
		n := types.FormatCount(int64(inbox.Total))
		hp.Counts = append(hp.Counts, components.StatCardProps{
			Count: inbox.Total, Label: "To approve", Detail: n + " waiting for your approval", Icon: "check-check", Href: inboxPath, Hot: true,
		})
		hp.WaitingAll = &components.Link{Label: "See all " + n, Href: inboxPath}
	}
	deps := a.docDeps()
	for _, it := range inbox.Rows {
		dt, _ := doctype.Lookup(it.DocType)
		why := layout.HomeWhyApproval + homeMetaSep + it.Why // approval.WhyText is never empty
		hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(dt, it.DocID, it.Title, it.Party, sql.Null[types.Money]{V: it.Amount, Valid: true}, doctype.StatePending, why, deps.ListHidden(r, dt)))
	}
	for _, o := range own {
		if o.hide("state") {
			continue // the card and a Needs changes row would name the state
		}
		if o.drafts > 0 {
			hp.Counts = append(hp.Counts, draftCard(o))
		}
		for _, d := range o.rejected {
			why := layout.HomeWhyChanges
			if d.reason != "" {
				why += homeMetaSep + `"` + d.reason + `"`
			}
			hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(o.dt, d.info.ID, d.info.Display, d.info.Party, d.total, d.info.State, why, o.hide))
		}
	}
	for _, s := range toSend {
		dt, _ := doctype.Lookup(s.DocType)
		hide := deps.ListHidden(r, dt)
		if hide("state") {
			continue // Not sent to the vendor yet would name the state (CD-368 (3))
		}
		hp.Waiting.Rows = append(hp.Waiting.Rows, homeRow(dt, s.ID, s.Title, s.Party, sql.Null[types.Money]{V: s.Total, Valid: true}, s.State, layout.HomeWhyNotSent, hide))
	}
	hp.Waiting.Rows = hp.Waiting.Rows[:min(len(hp.Waiting.Rows), homeWaitingSize)]

	type recent struct {
		o *homeOwn
		d homeDoc
	}
	var rows []recent
	for i := range own {
		for _, d := range own[i].recent {
			rows = append(rows, recent{&own[i], d})
		}
	}
	slices.SortStableFunc(rows, func(x, y recent) int { return strings.Compare(y.d.updated, x.d.updated) })
	for _, x := range rows[:min(len(rows), homeRecentSize)] {
		hp.Recent.Rows = append(hp.Recent.Rows, homeRow(x.o.dt, x.d.info.ID, x.d.info.Display, x.d.info.Party, x.d.total, x.d.info.State, x.d.info.DocLabel, x.o.hide))
	}

	for _, it := range ui.ShellFrom(ctx).New {
		hp.Quick = append(hp.Quick, components.QuickActionProps{Icon: it.Icon, Label: quickLabel(it), Href: it.Href})
	}
	return hp, nil
}

// draftCard counts the draft and rejected rows of one DocType p may read,
// and opens the list of exactly those (a card's count is what its link
// opens).
func draftCard(o homeOwn) components.StatCardProps {
	detail := countWord(o.drafts, "draft", "drafts")
	if o.redone > 0 {
		detail += homeMetaSep + countWord(o.redone, "needs changes", "need changes")
	}
	return components.StatCardProps{
		Count: o.drafts, Label: o.label, Detail: detail, Icon: o.icon,
		Href: listURL(o.dt.Name) + "?f=state%3Ain%3Adraft&f=state%3Ain%3Arejected",
	}
}

// countWord is "1 {one}" or "{n} {many}".
func countWord(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return types.FormatCount(int64(n)) + " " + many
}

// quickLabel is a create tile's words on Home (UI v2 §5.12 item 5).
func quickLabel(it ui.NavItem) string {
	if it.Href == listURL(goodsreceipt.DocType)+"/new" {
		return layout.HomeReceiveGoods
	}
	return "New " + strings.ToLower(it.Label)
}
