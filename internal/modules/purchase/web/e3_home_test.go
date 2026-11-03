package purchaseweb

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// e3Rows are the titles of the rows in the home section headed title, in
// page order.
func e3Rows(p p15ePage, title string) []string {
	var out []string
	p.doc.Find("section.homesect").Each(func(_ int, s *goquery.Selection) {
		if squash(s.Find("h2").First().Text()) != title {
			return
		}
		s.Find(".datalist-link .datalist-title").Each(func(_ int, c *goquery.Selection) {
			out = append(out, squash(c.Find("dd").Text()))
		})
	})
	return out
}

// e3Exec runs one statement in a write, for setups no service offers
// (moving a document out of a user's restriction, back-dating a change).
func e3Exec(t *testing.T, env *purchasetest.Env, q string, args ...any) {
	t.Helper()
	if err := env.DB.Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	}); err != nil {
		t.Fatalf("setup %q: %v", q, err)
	}
}

// e3MoveToTechPark puts a document of table on PRJ-TECHPARK / TECH-SITE,
// outside Priya's and Suresh's fixture restrictions.
func e3MoveToTechPark(t *testing.T, env *purchasetest.Env, table string, id int64) {
	t.Helper()
	e3Exec(t, env, `UPDATE `+table+` SET project_id = (SELECT id FROM project WHERE code = 'PRJ-TECHPARK'),
		site_id = (SELECT id FROM site WHERE code = 'TECH-SITE') WHERE id = ?`, id)
}

// e3Restrict adds project code to the user's project restriction.
func e3Restrict(t *testing.T, env *purchasetest.Env, id auth.UserID, code string) {
	t.Helper()
	var pid int64
	if err := env.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(), `SELECT id FROM project WHERE code = ?`, code).Scan(&pid)
	}); err != nil {
		t.Fatalf("setup: project %s: %v", code, err)
	}
	if err := env.Auth.ChangeRestrictions(context.Background(), env.Admin, id, auth.DimProject, []int64{pid}, nil, nil); err != nil {
		t.Fatalf("setup: restrict to %s: %v", code, err)
	}
}

func draftTitle(id int64) string { return "Draft " + strconv.FormatInt(id, 10) }

// e3Total is purchasetest.NewOrder's rounded total: 100 kg at ₹62.50 plus
// 18% GST.
const e3Total = "₹7,375.00"

// UIV2-E3 (UI v2 §5.12, CD-233/240/248 family): Priya (restricted to
// PRJ-METRO) has two drafts; the one moved to PRJ-TECHPARK is neither
// counted in My drafts nor named in Recent. Positive row: once she may also
// read PRJ-TECHPARK, the same draft is counted and listed.
func TestPurchaseWeb_HomeRestrictedCountsAndRecent(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	seen := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	hidden := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	e3MoveToTechPark(t, env, "purchase_order", hidden.ID)

	page := w.get(t, env.Priya, homePath)
	drafts := slices.IndexFunc(homeCards(page), func(c homeCard) bool { return c.title == "My drafts" })
	if drafts < 0 || homeCards(page)[drafts].figure != "1" {
		t.Errorf("restricted: cards = %v, want My drafts 1", homeCards(page))
	}
	if got := e3Rows(page, "Recent"); !slices.Equal(got, []string{draftTitle(seen.ID)}) {
		t.Errorf("restricted: Recent = %q, want only %s", got, draftTitle(seen.ID))
	}
	if strings.Contains(page.text, draftTitle(hidden.ID)) {
		t.Errorf("restricted: the page names the unreadable %s: %q", draftTitle(hidden.ID), page.text)
	}

	e3Restrict(t, env, env.Priya.ID, "PRJ-TECHPARK")
	page = w.get(t, env.Priya, homePath)
	drafts = slices.IndexFunc(homeCards(page), func(c homeCard) bool { return c.title == "My drafts" })
	if drafts < 0 || homeCards(page)[drafts].figure != "2" {
		t.Errorf("may read both: cards = %v, want My drafts 2", homeCards(page))
	}
	if got := e3Rows(page, "Recent"); !slices.Equal(got, []string{draftTitle(hidden.ID), draftTitle(seen.ID)}) {
		t.Errorf("may read both: Recent = %q, want %s then %s", got, draftTitle(hidden.ID), draftTitle(seen.ID))
	}
}

// UIV2-E3: Recent holds requests too, through the request read scope:
// Suresh (restricted to PUNE-WH and METRO-SITE) sees his METRO request and
// not the one moved to TECH-SITE, although orders with that id are his to
// read; a request row has no total. A requester without read on orders
// sees their request and no link to the PO list.
func TestPurchaseWeb_HomeRecentRequests(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	order := purchasetest.NewOrder(t, env)
	purchasetest.NewOrder(t, env)
	seen := purchasetest.NewRequest(t, env)
	hidden := purchasetest.NewRequest(t, env)
	e3MoveToTechPark(t, env, "purchase_request", hidden.ID)
	var seenTitle, hiddenTitle string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		for id, title := range map[int64]*string{seen.ID: &seenTitle, hidden.ID: &hiddenTitle} {
			info, err := env.Engine.Info(t.Context(), tx, purchase.RequestDocType, id)
			if err != nil {
				return err
			}
			*title = info.Display
		}
		return nil
	}); err != nil {
		t.Fatalf("setup: info: %v", err)
	}

	page := w.get(t, env.Suresh, homePath)
	if got := e3Rows(page, "Recent"); !slices.Equal(got, []string{seenTitle}) {
		t.Errorf("Suresh's Recent = %q, want only %s", got, seenTitle)
	}
	if strings.Contains(page.text, hiddenTitle) {
		t.Errorf("Suresh's home names the unreadable %s", hiddenTitle)
	}
	row := page.doc.Find("section.homesect:has(h2:contains('Recent')) .datalist-link").First()
	if href, _ := row.Attr("href"); href != "/doc/"+purchase.RequestDocType+"/"+strconv.FormatInt(seen.ID, 10) {
		t.Errorf("request row href = %q", href)
	}
	if got := squash(row.Find(".badge").Text()); got != "Submitted" {
		t.Errorf("request row badge = %q, want the request's word Submitted", got)
	}
	if strings.Contains(squash(row.Text()), "₹") {
		t.Errorf("request row shows a total: %q", squash(row.Text()))
	}

	// A requester without read on orders gets no "All purchase orders" link,
	// and an order she created (made hers in SQL) isn't named: without the
	// read right the order scope matches nothing.
	looker := p15eUser(t, env, "Rekha Nair", map[string][]auth.Right{purchase.RequestDocType: {auth.RightRead, auth.RightCreate}})
	e3Exec(t, env, `UPDATE purchase_request SET created_by = ? WHERE id = ?`, looker.ID, seen.ID)
	e3Exec(t, env, `UPDATE purchase_order SET created_by = ? WHERE id = ?`, looker.ID, order.ID)
	page = w.get(t, looker, homePath)
	if got := e3Rows(page, "Recent"); !slices.Equal(got, []string{seenTitle}) {
		t.Errorf("Rekha's Recent = %q, want %s", got, seenTitle)
	}
	if n := page.doc.Find("section.homesect:has(h2:contains('Recent')) .homesect-head a").Length(); n != 0 {
		t.Errorf("Rekha (no read on orders) has %d Recent links, want none", n)
	}
	if strings.Contains(page.text, order.Number) {
		t.Errorf("Rekha (no read on orders) is shown her order %s", order.Number)
	}
}

// UIV2-E3: Recent is the latest 5 by last change, newest first: with six
// drafts, the oldest-made one changed last comes first, a tie on the change
// time puts the higher id first, and the draft changed longest ago drops
// out. Each row has the total, the vendor and the state.
func TestPurchaseWeb_HomeRecentLimitAndOrder(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	var ids []int64
	for range 6 {
		ids = append(ids, purchasetest.NewOrder(t, env, purchasetest.StopAtDraft()).ID)
	}
	t0 := time.Date(2026, time.October, 26, 18, 31, 0, 0, time.UTC)
	for i, h := range []int{5, 0, 1, 3, 3, 2} {
		e3Exec(t, env, `UPDATE purchase_order SET updated_at = ? WHERE id = ?`, types.TimestampOf(t0.Add(time.Duration(h)*time.Hour)).String(), ids[i])
	}
	page := w.get(t, env.Priya, homePath)
	want := []string{draftTitle(ids[0]), draftTitle(ids[4]), draftTitle(ids[3]), draftTitle(ids[5]), draftTitle(ids[2])}
	if got := e3Rows(page, "Recent"); !slices.Equal(got, want) {
		t.Errorf("Recent = %q, want %q", got, want)
	}
	row := squash(page.doc.Find("section.homesect:has(h2:contains('Recent')) .datalist-link").First().Text())
	for _, part := range []string{e3Total, "Shree", "Draft"} {
		if !strings.Contains(row, part) {
			t.Errorf("first Recent row %q lacks %q", row, part)
		}
	}
	if got, _ := page.attr("section.homesect:has(h2:contains('Recent')) .homesect-head a", "href"); got != "/doc/"+purchase.DocType {
		t.Errorf("Recent link = %q, want the PO list", got)
	}
}

// UIV2-E3: Waiting for you is the purchase approvals waiting for the viewer,
// at most 3, oldest first, with "See all {n}" to the inbox narrowed to POs;
// each row says why it is theirs.
func TestPurchaseWeb_HomeWaitingRows(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	var ids []int64
	for range 4 {
		ids = append(ids, purchasetest.NewOrder(t, env, purchasetest.StopAtPending()).ID)
	}
	page := w.get(t, env.Ravi, homePath)
	want := []string{draftTitle(ids[0]), draftTitle(ids[1]), draftTitle(ids[2])}
	if got := e3Rows(page, "Waiting for you"); !slices.Equal(got, want) {
		t.Errorf("Ravi's Waiting = %q, want %q", got, want)
	}
	if href, _ := page.attr("section.homesect:has(h2:contains('Waiting for you')) .homesect-head a", "href"); href != "/approvals?type="+purchase.DocType {
		t.Errorf("See all href = %q", href)
	}
	if got := page.texts("section.homesect:has(h2:contains('Waiting for you')) .homesect-head a"); !slices.Equal(got, []string{"See all 4"}) {
		t.Errorf("See all = %q, want See all 4", got)
	}
	row := squash(page.doc.Find("section.homesect:has(h2:contains('Waiting for you')) .datalist-link").First().Text())
	for _, part := range []string{e3Total, "Shree", "Needs your approval"} {
		if !strings.Contains(row, part) {
			t.Errorf("first waiting row %q lacks %q", row, part)
		}
	}
	if href, _ := page.attr("section.homesect:has(h2:contains('Waiting for you')) .datalist-link", "href"); href != "/doc/"+purchase.DocType+"/"+strconv.FormatInt(ids[0], 10) {
		t.Errorf("first waiting row href = %q", href)
	}
	if strings.Contains(page.text, layout.HomeCaughtUp) {
		t.Error("Ravi has work waiting but the home says all caught up")
	}

	// Priya submitted them: nothing of hers is in an approval queue.
	if got := e3Rows(w.get(t, env.Priya, homePath), "Waiting for you"); len(got) != 0 {
		t.Errorf("Priya's Waiting = %q, want none", got)
	}
}

// UIV2-E3: a Purchase Manager restricted to PRJ-TECHPARK isn't shown or
// counted the METRO POs waiting for approval; positive row: the PO moved to
// PRJ-TECHPARK is listed and counted.
func TestPurchaseWeb_HomeWaitingRestricted(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	moved := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	id, _, err := env.Auth.CreateUser(t.Context(), env.Admin, auth.NewUser{
		Email: "tara@vardhan.test", Name: "Tara Iyer", Password: "Correct-Horse-42", Roles: []string{"Purchase Manager"},
	})
	if err != nil {
		t.Fatalf("setup: user: %v", err)
	}
	e3Restrict(t, env, id, "PRJ-TECHPARK")
	tara, _, err := env.Auth.LoadPrincipal(t.Context(), id)
	if err != nil {
		t.Fatalf("setup: principal: %v", err)
	}

	page := w.get(t, tara, homePath)
	if got := e3Rows(page, "Waiting for you"); len(got) != 0 {
		t.Errorf("before the move: Waiting = %q, want none", got)
	}
	for _, c := range homeCards(page) {
		if c.title == "To approve" {
			t.Errorf("before the move: To approve card %v, want none (0 is hidden)", c)
		}
	}

	e3MoveToTechPark(t, env, "purchase_order", moved.ID)
	page = w.get(t, tara, homePath)
	if got := e3Rows(page, "Waiting for you"); !slices.Equal(got, []string{draftTitle(moved.ID)}) {
		t.Errorf("after the move: Waiting = %q, want %s", got, draftTitle(moved.ID))
	}
	cards := homeCards(page)
	if len(cards) == 0 || cards[0] != (homeCard{"To approve", "1", "1 waiting for your approval", "/approvals"}) {
		t.Errorf("after the move: cards = %v, want To approve 1 first", cards)
	}
	if got := page.texts("section.homesect:has(h2:contains('Waiting for you')) .homesect-head a"); !slices.Equal(got, []string{"See all 1"}) {
		t.Errorf("after the move: See all = %q, want See all 1", got)
	}
	if !page.doc.Find("a.statcard").First().HasClass("statcard--hot") {
		t.Error("To approve above 0 isn't the hot card")
	}
}

// UIV2-E3 quick actions: New purchase order needs create on Order, New
// purchase request create on Request, Pending items read on Order; the home
// has no action bar (UI v2 §5.12).
func TestPurchaseWeb_HomeQuickActions(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	for _, c := range []struct {
		name   string
		rights map[string][]auth.Right
		want   []string
	}{
		{"Order maker", map[string][]auth.Right{purchase.DocType: {auth.RightRead, auth.RightCreate}}, []string{newOrderQuick, pendingQuick}},
		{"Order looker", map[string][]auth.Right{purchase.DocType: {auth.RightRead}}, []string{pendingQuick}},
		{"Request maker", map[string][]auth.Right{purchase.RequestDocType: {auth.RightRead, auth.RightCreate}}, []string{newRequestQuick}},
		{"Request looker", map[string][]auth.Right{purchase.RequestDocType: {auth.RightRead}}, nil},
		{"Both maker", map[string][]auth.Right{
			purchase.DocType: {auth.RightRead, auth.RightCreate}, purchase.RequestDocType: {auth.RightRead, auth.RightCreate},
		}, []string{newOrderQuick, newRequestQuick, pendingQuick}},
	} {
		page := w.get(t, p15eUser(t, env, c.name, c.rights), homePath)
		if got := quickActions(page); !slices.Equal(got, c.want) {
			t.Errorf("%s: quick actions = %q, want %q", c.name, got, c.want)
		}
		if page.doc.Find(".actionbar").Length() != 0 {
			t.Errorf("%s: the home has an action bar", c.name)
		}
	}
}

// A-24, UIV2-E3 empty state: a new user with read and create on requests
// only, and nothing of their own, sees `You're all caught up.`, no counts, no Recent, and still
// the quick actions their rights allow.
func TestPurchaseWeb_HomeNewUser(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	nu := p15eUser(t, env, "Neha Rao", map[string][]auth.Right{purchase.RequestDocType: {auth.RightRead, auth.RightCreate}})
	page := w.get(t, nu, homePath)
	if got := squash(page.doc.Find("main .empty-state-text").Text()); got != "You're all caught up." {
		t.Errorf("empty state = %q, want You're all caught up.", got)
	}
	// M11 (§5.12): the second line under it.
	if got := squash(page.doc.Find("main .empty-state-line").Text()); got != "New documents and approvals will show here." {
		t.Errorf("second line = %q", got)
	}
	if page.doc.Find("a.statcard").Length() != 0 || len(e3Rows(page, "Recent")) != 0 {
		t.Errorf("new user: counts %d, Recent %q; want none", page.doc.Find("a.statcard").Length(), e3Rows(page, "Recent"))
	}
	if got := quickActions(page); !slices.Equal(got, []string{newRequestQuick}) {
		t.Errorf("new user: quick actions = %q, want %q", got, newRequestQuick)
	}
	if strings.Contains(page.text, recentTitle) {
		t.Errorf("new user: an empty %s card shows", recentTitle)
	}
}

// UIV2-E3: a waiting row says why it is the viewer's: "Needs your approval",
// then the rule's reason when the request has one.
func TestPurchaseWeb_HomeWaitingWhy(t *testing.T) {
	hp := homeProps(homeView{waitingTotal: 2, waiting: []approval.InboxItem{
		{DocType: purchase.DocType, DocID: 12, Title: "Draft 12", Why: "₹50,000 or more"},
		{DocType: purchase.DocType, DocID: 13, Title: "Draft 13"},
	}})
	var got []string
	for _, r := range hp.Waiting.Rows {
		got = append(got, r.Values["m0"])
	}
	if want := []string{"Needs your approval · ₹50,000 or more", "Needs your approval"}; !slices.Equal(got, want) {
		t.Errorf("why = %q, want %q", got, want)
	}
	if hp.Waiting.Rows[0].Href != "/doc/purchase.Order/12" {
		t.Errorf("href = %q", hp.Waiting.Rows[0].Href)
	}
}
