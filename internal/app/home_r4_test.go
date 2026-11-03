package app_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/app"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/layout"
)

// homeEnv is a canonical world with purchase documents in reach.
type homeEnv struct {
	*world
	admin auth.Principal
	priya auth.Principal
}

func newHomeEnv(t *testing.T, mods ...func(*app.Config)) homeEnv {
	t.Helper()
	w := newWorld(t, mods...)
	admin, _, _ := w.canonical()
	return homeEnv{world: w, admin: admin, priya: w.principal(priyaID)}
}

func (e homeEnv) id(table, code string) string {
	e.t.Helper()
	return e.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code)
}

// order saves a PO of qty × ₹96 as p on PRJ-METRO: 1,000 of them wait for a
// Purchase Manager under the rule homeRule adds.
func (e homeEnv) order(p auth.Principal, qty string) doc.Result {
	e.t.Helper()
	r, err := e.app.Engine().Do(context.Background(), p, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": e.id("party", "SHREE"), "site_id": e.id("site", "METRO-SITE"), "project_id": e.id("project", "PRJ-METRO"), "order_date": "2026-10-24",
		},
		Lines: []map[string]string{{
			"item_id": e.id("item", "MS-ANGLE-50"), "uom_id": e.id("uom", "KGS"), "qty": qty, "price": "96",
			"tax_code_id": e.id("tax_code", "GST18"), "hsn_sac": "7216",
		}},
	})
	if err != nil {
		e.t.Fatalf("save order as %s: %v", p.Name, err)
	}
	return r
}

func (e homeEnv) submit(p auth.Principal, r doc.Result, want doctype.State) doc.Result {
	e.t.Helper()
	out, err := e.app.Engine().Do(context.Background(), p, purchase.DocType, r.ID, doctype.ActionSubmit, doc.ActionInput{Version: r.Version})
	if err != nil || out.State != want {
		e.t.Fatalf("submit order %d = %s, %v; want %s", r.ID, out.State, err, want)
	}
	return out
}

// homeRule makes a PO of ₹50,000 or more wait for a Purchase Manager.
func (e homeEnv) homeRule() {
	e.t.Helper()
	r := e.signedIn(adminEmail).post("/admin/approval-rules", url.Values{
		"doctype": {purchase.DocType}, "from_amount": {"50000.00"}, "approver_type": {"role"}, "role": {"Purchase Manager"},
	})
	if r.status >= http.StatusBadRequest {
		e.t.Fatalf("POST rule = %d (%q)", r.status, r.pageText())
	}
}

func (e homeEnv) exec(q string, args ...any) {
	e.t.Helper()
	if err := e.app.DB().Write(context.Background(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(context.Background(), q, args...)
		return err
	}); err != nil {
		e.t.Fatalf("setup %q: %v", q, err)
	}
}

// moveOut puts order id on PRJ-TECHPARK, outside Priya's restriction.
func (e homeEnv) moveOut(id int64) {
	e.t.Helper()
	e.exec(`UPDATE purchase_order SET project_id = ? WHERE id = ?`, e.id("project", "PRJ-TECHPARK"), id)
}

func squashed(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

// homeRows are the row titles of the section headed title.
func homeRows(r *response, title string) []string {
	out := []string{}
	r.doc.Find("section.homesect").Each(func(_ int, s *goquery.Selection) {
		if squashed(s.Find("h2").First()) != title {
			return
		}
		s.Find(".datalist-link .datalist-title dd").Each(func(_ int, c *goquery.Selection) { out = append(out, squashed(c)) })
	})
	return out
}

// homeCard is one count card as drawn.
type homeCard struct{ label, count, detail, href string }

func homeCards(r *response) []homeCard {
	var out []homeCard
	r.doc.Find("ul.statcards a.statcard").Each(func(_ int, s *goquery.Selection) {
		out = append(out, homeCard{squashed(s.Find(".statcard-label")), squashed(s.Find(".statcard-count")), squashed(s.Find(".statcard-detail")), s.AttrOr("href", "")})
	})
	return out
}

func homeTiles(r *response) []string {
	out := []string{}
	r.doc.Find("main a.newtile").Each(func(_ int, s *goquery.Selection) { out = append(out, squashed(s)+" "+s.AttrOr("href", "")) })
	return out
}

func draftWord(id int64) string { return "Draft " + strconv.FormatInt(id, 10) }

func draftsHref(dt string) string {
	return "/doc/" + dt + "?f=state%3Ain%3Adraft&f=state%3Ain%3Arejected"
}

// CD-387 (1): an all-rights user's Home. A real page at `/`, greeted by
// first name, a tile for every create, and their draft on a count card and
// in Recent, with its amount, party and state.
func TestHome_AllRights(t *testing.T) {
	e := newHomeEnv(t)
	d := e.order(e.admin, "100")
	r := e.signedIn(adminEmail).get("/")
	if r.status != http.StatusOK {
		t.Fatalf("GET / = %d to %q, want 200", r.status, r.location())
	}
	if got := squashed(r.doc.Find("main h1")); got != "Home" {
		t.Errorf("h1 = %q, want Home", got)
	}
	if got := squashed(r.doc.Find("main p.eyebrow")); got != "Good morning, Anil" {
		t.Errorf("greeting = %q", got)
	}
	want := []string{
		"New purchase order /doc/purchase.Order/new",
		"New purchase request /doc/purchase.Request/new",
		"Receive goods /doc/goodsreceipt.Receipt/new",
		"New expense claim /doc/expense.Claim/new",
		"New advance request /doc/expense.Advance/new",
		"New party /m/party/new",
	}
	if got := homeTiles(r); !slices.Equal(got, want) {
		t.Errorf("quick actions = %q, want %q", got, want)
	}
	card := homeCard{"Purchase orders", "1", "1 draft", draftsHref(purchase.DocType)}
	if got := homeCards(r); !slices.Equal(got, []homeCard{card}) {
		t.Errorf("cards = %v, want %v", got, card)
	}
	if got := homeRows(r, "Recent"); !slices.Equal(got, []string{draftWord(d.ID)}) {
		t.Errorf("Recent = %q", got)
	}
	row := r.doc.Find("section.homesect:has(h2:contains('Recent')) .datalist-link").First()
	if got := row.AttrOr("href", ""); got != "/doc/purchase.Order/"+strconv.FormatInt(d.ID, 10) {
		t.Errorf("row href = %q", got)
	}
	text := squashed(row)
	for _, w := range []string{"₹11,328.00", "Shree Steel Traders", "Purchase Order", "Draft"} {
		if !strings.Contains(text, w) {
			t.Errorf("Recent row %q lacks %q", text, w)
		}
	}
	// The link each card makes opens, so a card never ends in a refusal.
	if got := e.signedIn(adminEmail).get(card.href); got.status != http.StatusOK {
		t.Errorf("GET %s = %d", card.href, got.status)
	}
}

// Every DocType the Home covers reads: a saved row of each is on Recent, newest
// change first across DocTypes, at most 5, with its own amount field.
func TestHome_RecentAcrossModulesNewestFirst(t *testing.T) {
	e := newHomeEnv(t)
	var ids []int64
	for range 6 {
		ids = append(ids, e.order(e.admin, "100").ID)
	}
	// Ids run against the order of change, so ORDER BY updated_at decides.
	for i, id := range ids {
		e.exec(`UPDATE purchase_order SET updated_at = ? WHERE id = ?`, "2026-10-1"+strconv.Itoa(i)+"T00:00:00.000Z", id)
	}
	e.exec(`UPDATE purchase_order SET updated_at = '2026-10-23T00:00:00.000Z' WHERE id = ?`, ids[0])
	res, err := e.app.Engine().Do(context.Background(), e.admin, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"site_id": e.id("site", "METRO-SITE"), "project_id": e.id("project", "PRJ-METRO"), "request_date": "2026-10-24"},
		Lines:  []map[string]string{{"item_id": e.id("item", "MS-ANGLE-50"), "uom_id": e.id("uom", "KGS"), "qty": "100"}},
	})
	if err != nil {
		t.Fatalf("save request: %v", err)
	}
	e.exec(`UPDATE purchase_request SET updated_at = '2026-10-22T00:00:00.000Z' WHERE id = ?`, res.ID)
	info := e.text(`SELECT 'Draft ' || id FROM purchase_request WHERE id = ?`, res.ID)
	r := e.signedIn(adminEmail).get("/")
	want := []string{draftWord(ids[0]), info, draftWord(ids[5]), draftWord(ids[4]), draftWord(ids[3])}
	if got := homeRows(r, "Recent"); !slices.Equal(got, want) {
		t.Errorf("Recent = %q, want %q", got, want)
	}
	reqRow := r.doc.Find(`section.homesect a.datalist-link[href="/doc/purchase.Request/` + strconv.FormatInt(res.ID, 10) + `"]`)
	if strings.Contains(squashed(reqRow), "₹") {
		t.Errorf("a request has no amount: %q", squashed(reqRow))
	}
	if !strings.Contains(squashed(reqRow), "Purchase Request") {
		t.Errorf("request row = %q, want its document label", squashed(reqRow))
	}
}

// CD-387 (1), CD-233 family: Priya is restricted to PRJ-METRO. A draft she
// owns on another project is neither counted nor named; another user's draft
// she can read isn't hers, so it isn't either. Positive row: once she may read
// the project, the same draft shows and counts.
func TestHome_RestrictedReader(t *testing.T) {
	e := newHomeEnv(t)
	seen := e.order(e.priya, "100")
	hidden := e.order(e.priya, "100")
	e.moveOut(hidden.ID)
	other := e.order(e.admin, "100")

	priya := e.signedIn(priyaEmail)
	r := priya.get("/")
	// The card counts every draft she may read (hers and the admin's on
	// PRJ-METRO), as its list shows them; Recent names only hers.
	cards := homeCards(r)
	if len(cards) != 1 || cards[0].count != "2" || cards[0].detail != "2 drafts" {
		t.Fatalf("restricted: cards = %v, want one with 2 drafts", cards)
	}
	if n := listRows(t, priya, cards[0].href); strconv.Itoa(n) != cards[0].count {
		t.Errorf("restricted: card says %s, its list shows %d rows", cards[0].count, n)
	}
	if got := homeRows(r, "Recent"); !slices.Equal(got, []string{draftWord(seen.ID)}) {
		t.Errorf("restricted: Recent = %q", got)
	}
	for _, id := range []int64{hidden.ID, other.ID} {
		if strings.Contains(r.pageText(), draftWord(id)) {
			t.Errorf("Home names %s", draftWord(id))
		}
	}
	if err := e.app.Auth().ChangeRestrictions(context.Background(), e.admin, priyaID, auth.DimProject, []int64{mustInt(t, e.id("project", "PRJ-TECHPARK"))}, nil, nil); err != nil {
		t.Fatalf("widen: %v", err)
	}
	r = priya.get("/")
	if got := homeCards(r); len(got) != 1 || got[0].count != "3" || got[0].detail != "3 drafts" {
		t.Errorf("may read both: cards = %v", got)
	}
	if got := homeRows(r, "Recent"); !slices.Contains(got, draftWord(hidden.ID)) {
		t.Errorf("may read both: Recent = %q, want %s", got, draftWord(hidden.ID))
	}
}

func mustInt(t *testing.T, s string) int64 {
	t.Helper()
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// A user with no create rights and no work: the page, no tiles, no New, and
// the caught-up state with its second line.
func TestHome_NoCreateRights(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	if _, _, err := w.app.Auth().CreateUser(context.Background(), admin, auth.NewUser{Email: nobodyEmail, Name: "Nina Bose", Password: testPassword, Roles: []string{}}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	r := w.signedIn(nobodyEmail).get("/")
	if r.status != http.StatusOK {
		t.Fatalf("GET / = %d to %q", r.status, r.location())
	}
	if got := homeTiles(r); len(got) != 0 {
		t.Errorf("quick actions = %q, want none", got)
	}
	if got := len(homeCards(r)); got != 0 {
		t.Errorf("%d cards, want none", got)
	}
	if got := squashed(r.doc.Find("main .empty-state")); got != layout.HomeCaughtUp+layout.HomeCaughtUpLine {
		t.Errorf("empty state = %q", got)
	}
	if got := r.doc.Find(`nav.bottomnav a[href="/new"]`).Length(); got != 0 {
		t.Error("a user who can't create has New")
	}
}

// Deepak (Employee) holds create on some DocTypes only: the tiles are exactly
// those, the same list the New sheet shows.
func TestHome_TilesFollowCreateRights(t *testing.T) {
	e := newHomeEnv(t)
	r := e.signedIn(deepakEmail).get("/")
	sheet := []string{}
	r.doc.Find("#new-sheet a.newtile").Each(func(_ int, s *goquery.Selection) { sheet = append(sheet, s.AttrOr("href", "")) })
	tiles := []string{}
	r.doc.Find("main a.newtile").Each(func(_ int, s *goquery.Selection) { tiles = append(tiles, s.AttrOr("href", "")) })
	if !slices.Equal(tiles, sheet) || len(tiles) == 0 || len(tiles) >= 6 {
		t.Errorf("tiles = %q, sheet = %q", tiles, sheet)
	}
}

// CD-368 (1) on Home: an approver's row and hot card, a rejected draft with
// its reason, and an approved order to send; each only for its own viewer.
func TestHome_WaitingSources(t *testing.T) {
	e := newHomeEnv(t)
	e.homeRule()
	ravi := e.signedIn(raviEmail)
	pending := e.submit(e.priya, e.order(e.priya, "1000"), doctype.StatePending)

	r := ravi.get("/")
	want := []homeCard{{"To approve", "1", "1 waiting for your approval", "/approvals"}}
	if got := homeCards(r); !slices.Equal(got, want) {
		t.Errorf("Ravi's cards = %v, want %v", got, want)
	}
	if r.doc.Find("a.statcard--hot").Length() != 1 {
		t.Error("To approve isn't the hot card")
	}
	if got := homeRows(r, "Waiting for you"); !slices.Equal(got, []string{draftWord(pending.ID)}) {
		t.Errorf("Ravi's Waiting = %q", got)
	}
	sect := r.doc.Find("section.homesect:has(h2:contains('Waiting for you'))")
	if got := squashed(sect.Find(`.homesect-head a[href="/approvals"]`)); got != "See all 1" {
		t.Errorf("See all = %q", got)
	}
	if !strings.Contains(squashed(sect), layout.HomeWhyApproval+" · ₹50,000 or more") {
		t.Errorf("Waiting = %q, want the why", squashed(sect))
	}
	if strings.Contains(r.pageText(), layout.HomeCaughtUp) {
		t.Error("work is waiting but Home says caught up")
	}

	if _, err := e.app.Engine().Do(context.Background(), e.principalOf(raviEmail), purchase.DocType, pending.ID, doctype.ActionReject, doc.ActionInput{Version: pending.Version, Reason: "Use the rate contract price"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	r = e.signedIn(priyaEmail).get("/")
	if got := homeRows(r, "Waiting for you"); !slices.Equal(got, []string{draftWord(pending.ID)}) {
		t.Errorf("Priya's Waiting = %q", got)
	}
	text := squashed(r.doc.Find("section.homesect:has(h2:contains('Waiting for you'))"))
	if !strings.Contains(text, layout.HomeWhyChanges+` · "Use the rate contract price"`) {
		t.Errorf("Priya's Waiting = %q", text)
	}
	if got := homeCards(r); len(got) != 1 || got[0].detail != "1 draft · 1 needs changes" {
		t.Errorf("Priya's cards = %v", got)
	}
	// A rejection with no reason left (older rows) shows the plain words.
	e.exec(`UPDATE purchase_order SET state_reason = '' WHERE id = ?`, pending.ID)
	text = squashed(e.signedIn(priyaEmail).get("/").doc.Find("section.homesect:has(h2:contains('Waiting for you'))"))
	if !strings.Contains(text, layout.HomeWhyChanges) || strings.Contains(text, `""`) {
		t.Errorf("no reason: Waiting = %q, want %q alone", text, layout.HomeWhyChanges)
	}
	// Ravi reads every order: the card counts Priya's rejected draft, as the
	// list it opens shows it, but Waiting names it only for her.
	r = ravi.get("/")
	if got := homeCards(r); len(got) != 1 || got[0].count != "1" || got[0].detail != "1 draft · 1 needs changes" {
		t.Errorf("Ravi after rejecting: cards = %v", got)
	}
	if got := homeRows(r, "Waiting for you"); len(got) != 0 {
		t.Errorf("Ravi after rejecting: Waiting = %q, want none", got)
	}

	sent := e.submit(e.admin, e.order(e.admin, "100"), doctype.StateApproved)
	r = e.signedIn(adminEmail).get("/")
	text = squashed(r.doc.Find("section.homesect:has(h2:contains('Waiting for you'))"))
	if !strings.Contains(text, layout.HomeWhyNotSent) || !strings.Contains(text, e.text(`SELECT number FROM purchase_order WHERE id = ?`, sent.ID)) {
		t.Errorf("Admin's Waiting = %q, want the approved order to send", text)
	}
}

func (e homeEnv) principalOf(email string) auth.Principal {
	e.t.Helper()
	return e.principal(auth.UserID(mustInt(e.t, e.text(`SELECT CAST(id AS TEXT) FROM users WHERE email = ?`, email))))
}

// More waiting work than rows: inbox rows first, cut to 3, See all with the
// whole count.
func TestHome_WaitingCutToThree(t *testing.T) {
	e := newHomeEnv(t)
	e.homeRule()
	var ids []string
	for range 4 {
		r := e.submit(e.priya, e.order(e.priya, "1000"), doctype.StatePending)
		ids = append(ids, draftWord(r.ID))
	}
	r := e.signedIn(raviEmail).get("/")
	if got := homeRows(r, "Waiting for you"); !slices.Equal(got, ids[:3]) {
		t.Errorf("Waiting = %q, want %q", got, ids[:3])
	}
	if got := squashed(r.doc.Find(`.homesect-head a[href="/approvals"]`)); got != "See all 4" {
		t.Errorf("See all = %q", got)
	}
}

// CD-368 (3): Home checks the registered Hide for each value it shows, and a
// state word never leaves on a card or a Needs changes row.
func TestHome_HideLeavesValuesOut(t *testing.T) {
	var hide map[string]bool
	e := newHomeEnv(t)
	e.app.SetHide(purchase.DocType, func(_ docscreen.Ctx, f doctype.Field) bool { return hide[f.Name] })
	e.homeRule()
	pending := e.submit(e.priya, e.order(e.priya, "1000"), doctype.StatePending)
	if _, err := e.app.Engine().Do(context.Background(), e.principalOf(raviEmail), purchase.DocType, pending.ID, doctype.ActionReject, doc.ActionInput{Version: pending.Version, Reason: "Too dear"}); err != nil {
		t.Fatalf("reject: %v", err)
	}
	priya := e.signedIn(priyaEmail)
	recent := func() string {
		return squashed(priya.get("/").doc.Find("section.homesect:has(h2:contains('Recent')) .datalist-link"))
	}
	if got := recent(); !strings.Contains(got, "₹1,13,280.00") || !strings.Contains(got, "Shree Steel Traders") || !strings.Contains(got, "Rejected") {
		t.Fatalf("control: Recent = %q", got)
	}
	hide = map[string]bool{"rounded_total": true}
	if got := recent(); strings.Contains(got, "₹") {
		t.Errorf("hidden total: Recent = %q", got)
	}
	hide = map[string]bool{"vendor_id": true}
	if got := recent(); strings.Contains(got, "Shree Steel Traders") {
		t.Errorf("hidden party: Recent = %q", got)
	}
	hide = map[string]bool{"state": true}
	r := priya.get("/")
	if got := squashed(r.doc.Find("section.homesect:has(h2:contains('Recent')) .datalist-link")); strings.Contains(got, "Rejected") {
		t.Errorf("hidden state: Recent = %q", got)
	}
	if strings.Contains(r.pageText(), layout.HomeWhyChanges) || len(homeCards(r)) != 0 {
		t.Errorf("hidden state: page still names the state: cards %v", homeCards(r))
	}
	// CD-368 (3) ruling: an order to send names the state too, so it leaves
	// with the state's Hide.
	hide = nil
	sent := e.submit(e.admin, e.order(e.admin, "100"), doctype.StateApproved)
	admin := e.signedIn(adminEmail)
	sendRow := `section.homesect:has(h2:contains('Waiting for you')) a.datalist-link[href$="/` + strconv.FormatInt(sent.ID, 10) + `"]`
	if got := squashed(admin.get("/").doc.Find(sendRow)); !strings.Contains(got, layout.HomeWhyNotSent) {
		t.Fatalf("control: to-send row = %q", got)
	}
	hide = map[string]bool{"state": true}
	if got := admin.get("/"); got.doc.Find(sendRow).Length() != 0 || strings.Contains(got.pageText(), layout.HomeWhyNotSent) {
		t.Errorf("hidden state: Home still shows the order to send")
	}
	// An approver's row follows Hide too.
	hide = nil
	again := e.submit(e.priya, e.order(e.priya, "1000"), doctype.StatePending)
	ravi := e.signedIn(raviEmail)
	row := `section.homesect a.datalist-link[href$="/` + strconv.FormatInt(again.ID, 10) + `"]`
	if got := squashed(ravi.get("/").doc.Find(row)); !strings.Contains(got, "1,13,280") || !strings.Contains(got, "Shree Steel Traders") {
		t.Fatalf("control: inbox row = %q", got)
	}
	hide = map[string]bool{"rounded_total": true, "vendor_id": true}
	got := squashed(ravi.get("/").doc.Find(row))
	if !strings.Contains(got, layout.HomeWhyApproval) || strings.Contains(got, "1,13,280") || strings.Contains(got, "Shree Steel Traders") {
		t.Errorf("hidden on an inbox row: %q", got)
	}
}

// §5.12 item 1: the greeting goes by the IST hour, also just after IST
// midnight, on Home and on the purchase home, by first name.
func TestHome_GreetingByISTHour(t *testing.T) {
	clk := testkit.Clock()
	e := newHomeEnv(t, func(c *app.Config) { c.Clock = clk })
	for _, c := range []struct {
		utc  string
		want string
	}{
		{"2026-10-24T18:31:00Z", "Good morning, Priya"},   // 00:01 IST the next day
		{"2026-10-24T06:29:00Z", "Good morning, Priya"},   // 11:59
		{"2026-10-24T06:30:00Z", "Good afternoon, Priya"}, // 12:00
		{"2026-10-24T11:29:00Z", "Good afternoon, Priya"}, // 16:59
		{"2026-10-24T11:30:00Z", "Good evening, Priya"},   // 17:00
		{"2026-10-24T18:29:00Z", "Good evening, Priya"},   // 23:59
	} {
		at, err := time.Parse(time.RFC3339, c.utc)
		if err != nil {
			t.Fatal(err)
		}
		clk.Set(at)
		b := e.signedIn(priyaEmail) // a session idle over 12 hours has ended
		for _, path := range []string{"/", "/modules/purchase"} {
			r := b.get(path)
			if got := squashed(r.doc.Find("main p.eyebrow")); got != c.want {
				t.Errorf("%s at %s: greeting = %q, want %q", path, c.utc, got, c.want)
			}
		}
	}
	if got := e.signedIn(priyaEmail).get("/approvals").doc.Find("main p.eyebrow").Length(); got != 0 {
		t.Errorf("Approvals has %d greetings, want none", got)
	}
}

// navRole gives a new user one role holding exactly rights on dt, signed in.
func navRole(t *testing.T, w *world, admin auth.Principal, email, dt string, rights map[auth.Right]auth.Scope) *browser {
	t.Helper()
	ctx := context.Background()
	role := "Only " + email
	if err := w.app.Auth().CreateRole(ctx, admin, role); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if rights != nil {
		if err := w.app.Auth().SetRights(ctx, admin, role, dt, rights); err != nil {
			t.Fatalf("SetRights: %v", err)
		}
	}
	if _, _, err := w.app.Auth().CreateUser(ctx, admin, auth.NewUser{Email: email, Name: "Test User", Password: testPassword, Roles: []string{role}}); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return w.signedIn(email)
}

func navHrefs(r *response, sel string) []string {
	out := []string{}
	r.doc.Find(sel + " a").Each(func(_ int, s *goquery.Selection) { out = append(out, s.AttrOr("href", "")) })
	return out
}

// H6, EXP high: each link shows only with the right its page needs, and opens
// for that holder. The read of one expense DocType never shows the other's.
func TestNav_ReportAndAdminLinksByRight(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	const (
		open, lines, advances = "/reports/expense/open", "/reports/expense/lines", "/reports/expense/advances"
		choose                = "/m/import/choose"
		stuck, overdue        = "/admin/approvals/stuck", "/admin/approvals/overdue"
	)
	links := []string{open, lines, advances, choose, stuck, overdue}
	read := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	for _, c := range []struct {
		name, email, dt string
		rights          map[auth.Right]auth.Scope
		want            []string
	}{
		{"claim read", "claims@vardhan.test", expense.ClaimDocType, read, []string{open, lines}},
		{"advance read", "advances@vardhan.test", expense.AdvanceDocType, read, []string{advances}},
		{"import", "import@vardhan.test", "Item", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightImport: auth.ScopeAll}, []string{choose}},
		{"read only on an importable master", "itemread@vardhan.test", "Item", read, nil},
		{"import on a master that isn't importable would be no link", "po@vardhan.test", purchase.DocType, read, nil},
		{"nothing", "none@vardhan.test", "", nil, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := navRole(t, w, admin, c.email, c.dt, c.rights)
			r := b.get("/approvals")
			for _, sel := range []string{"nav.app-sidebar", "#nav-sheet"} {
				got := navHrefs(r, sel)
				for _, l := range links {
					if slices.Contains(got, l) != slices.Contains(c.want, l) {
						t.Errorf("%s: link %s shown = %v, want %v", sel, l, slices.Contains(got, l), slices.Contains(c.want, l))
					}
				}
			}
			for _, l := range c.want {
				if p := b.get(l); p.status != http.StatusOK {
					t.Errorf("GET %s = %d, want 200", l, p.status)
				}
			}
		})
	}
	// Administrator has every link; a Director, with every document right but
	// not the role, has no stuck or overdue page.
	r := w.signedIn(adminEmail).get("/approvals")
	got := navHrefs(r, "nav.app-sidebar")
	for _, l := range links {
		if !slices.Contains(got, l) {
			t.Errorf("Administrator has no %s", l)
		}
		if p := w.signedIn(adminEmail).get(l); p.status != http.StatusOK {
			t.Errorf("Administrator GET %s = %d", l, p.status)
		}
	}
	got = navHrefs(w.signedIn("meena@vardhan.test").get("/approvals"), "nav.app-sidebar")
	if slices.Contains(got, stuck) || slices.Contains(got, overdue) {
		t.Errorf("a Director's nav has an Administrator page: %q", got)
	}
}

// Sidebar labels of the links the task adds.
func TestNav_NewLinkLabels(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	r := w.signedIn(adminEmail).get("/approvals")
	for href, want := range map[string]string{
		"/":                         "Home",
		"/reports/expense/open":     "Open claims",
		"/reports/expense/lines":    "Expense lines",
		"/reports/expense/advances": "Open advances",
		"/m/import/choose":          "Import",
		"/admin/approvals/stuck":    "Stuck approvals",
		"/admin/approvals/overdue":  "Overdue approvals",
	} {
		if got := squashed(r.doc.Find(`nav.app-sidebar a[href="` + href + `"]`)); got != want {
			t.Errorf("%s = %q, want %q", href, got, want)
		}
	}
}

// A-2: Home leads Work; H4: Home and Approvals are told apart in both navs.
func TestNav_HomeFirstAndCurrent(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(priyaEmail)
	first := b.get("/approvals").doc.Find(`nav.app-sidebar ul[aria-labelledby="side-work"] a`).First()
	if got := first.AttrOr("href", ""); got != "/" {
		t.Errorf("the first Work item = %q, want /", got)
	}
	for path, want := range map[string]string{"/": "/", "/approvals": "/approvals", "/approvals/mine": "/approvals/mine", "/search": "", "/new": ""} {
		r := b.get(path)
		if got := r.doc.Find(`nav.app-sidebar a[aria-current="page"]`).AttrOr("href", ""); got != want {
			t.Errorf("sidebar current on %s = %q, want %q", path, got, want)
		}
	}
	for path, want := range map[string]string{"/": "/", "/approvals": "/approvals", "/search": "/search"} {
		r := b.get(path)
		if got := r.doc.Find(`nav.bottomnav a[aria-current="page"]`).AttrOr("href", ""); got != want {
			t.Errorf("bottom nav current on %s = %q, want %q", path, got, want)
		}
	}
}

// H6: the bare wizard path goes to its first step.
func TestImport_BarePathRedirects(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	r := w.signedIn(adminEmail).get("/m/import")
	if r.status != http.StatusSeeOther || r.location() != "/m/import/choose" {
		t.Errorf("GET /m/import = %d to %q, want 303 to /m/import/choose", r.status, r.location())
	}
}

// M10: the empty search page focuses its box; a results page leaves the focus
// alone, so a phone's keyboard doesn't cover the results.
func TestSearch_BoxAutofocusOnlyWhenEmpty(t *testing.T) {
	w := newWorld(t)
	w.seedUsers()
	b := w.signedIn(priyaEmail)
	if b.get("/search").doc.Find(`main input#filter-q[autofocus]`).Length() != 1 {
		t.Error("GET /search: the box has no autofocus")
	}
	if b.get("/search?q=PO").doc.Find(`main input#filter-q[autofocus]`).Length() != 0 {
		t.Error("GET /search?q=PO: the box takes focus over the results")
	}
}

// §5.12 item 5: Receive goods on the purchase home needs create on receipts.
func TestPurchaseHome_ReceiveGoodsQuickAction(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	const tile = `main a.newtile[href="/doc/` + goodsreceipt.DocType + `/new"]`
	if got := squashed(w.signedIn(adminEmail).get("/modules/purchase").doc.Find(tile)); got != "Receive goods" {
		t.Errorf("all rights: tile = %q", got)
	}
	b := navRole(t, w, admin, "po2@vardhan.test", purchase.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
	if got := b.get("/modules/purchase").doc.Find(tile).Length(); got != 0 {
		t.Errorf("read on orders only: %d Receive goods tiles", got)
	}
}

// listRows is how many rows the list at href shows b.
func listRows(t *testing.T, b *browser, href string) int {
	t.Helper()
	r := b.get(href)
	if r.status != http.StatusOK {
		t.Fatalf("GET %s = %d", href, r.status)
	}
	return r.doc.Find("main a.datalist-link").Length()
}

// Director ruling (R4 review): a Home card's count is what its link opens.
// The admin sees another user's draft too; Deepak, whose claim read is his
// own, sees only his, though Priya's claim draft is there.
func TestHome_DraftCardCountIsItsList(t *testing.T) {
	e := newExpWorld(t)
	e.claim(0, "", true, "500.00", "2026-10-20")
	e.do(e.p(priyaID), expense.ClaimDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"claim_date": "2026-10-24", "purpose": "Site visit"},
		Lines:  []map[string]string{{"expense_type_id": e.travel, "expense_date": "2026-10-20", "amount": "300.00", "no_receipt_reason": "Auto rickshaw"}},
	})
	e.draftAdvance()
	for _, c := range []struct {
		email  string
		claims string
	}{{adminEmail, "2"}, {deepakEmail, "1"}} {
		b := e.signedIn(c.email)
		cards := homeCards(b.get("/"))
		if len(cards) == 0 {
			t.Fatalf("%s: no cards", c.email)
		}
		for _, card := range cards {
			if strconv.Itoa(listRows(t, b, card.href)) != card.count {
				t.Errorf("%s: card %v, its list shows %d rows", c.email, card, listRows(t, b, card.href))
			}
			if card.label == "Expense claims" && card.count != c.claims {
				t.Errorf("%s: claims card = %s, want %s", c.email, card.count, c.claims)
			}
		}
	}
}

// R4 review: a claim total and an advance amount Validate cleared (NULL, over
// the money limit, CD-302/308) show no amount; set ones show their own.
func TestHome_ExpenseAmountsAndNull(t *testing.T) {
	e := newExpWorld(t)
	claim := e.claim(0, "", true, "500.00", "2026-10-20")
	adv := e.draftAdvance()
	deepak := e.signedIn(deepakEmail)
	row := func(r *response, dt string, id int64) string {
		return squashed(r.doc.Find(`section.homesect:has(h2:contains('Recent')) a.datalist-link[href="/doc/` + dt + `/` + strconv.FormatInt(id, 10) + `"]`))
	}
	r := deepak.get("/")
	if got := row(r, expense.ClaimDocType, claim.ID); !strings.Contains(got, draftWord(claim.ID)) || !strings.Contains(got, "₹500.00") {
		t.Errorf("claim row = %q, want its title and ₹500.00", got)
	}
	if got := row(r, expense.AdvanceDocType, adv.ID); !strings.Contains(got, draftWord(adv.ID)) || !strings.Contains(got, "₹100.00") {
		t.Errorf("advance row = %q, want its title and ₹100.00", got)
	}
	he := homeEnv{world: e.world}
	he.exec(`UPDATE expense_claim SET total = NULL WHERE id = ?`, claim.ID)
	he.exec(`UPDATE employee_advance SET amount = NULL WHERE id = ?`, adv.ID)
	r = deepak.get("/")
	if r.status != http.StatusOK {
		t.Fatalf("GET / with NULL amounts = %d", r.status)
	}
	for _, c := range []struct {
		dt string
		id int64
	}{{expense.ClaimDocType, claim.ID}, {expense.AdvanceDocType, adv.ID}} {
		got := row(r, c.dt, c.id)
		if !strings.Contains(got, draftWord(c.id)) || strings.Contains(got, "₹") {
			t.Errorf("%s NULL amount: row = %q, want its title and no amount", c.dt, got)
		}
	}
}

// Recent breaks a tie on the change time by id, newest first.
func TestHome_RecentTieBreaksByID(t *testing.T) {
	e := newHomeEnv(t)
	a := e.order(e.admin, "100")
	b := e.order(e.admin, "100")
	e.exec(`UPDATE purchase_order SET updated_at = '2026-10-20T00:00:00.000Z' WHERE id IN (?, ?)`, a.ID, b.ID)
	if got, want := homeRows(e.signedIn(adminEmail).get("/"), "Recent"), []string{draftWord(b.ID), draftWord(a.ID)}; !slices.Equal(got, want) {
		t.Errorf("Recent = %q, want %q", got, want)
	}
}

// R4 review minor: Receive goods needs create on receipts: read alone gives no
// tile, read with create gives one.
func TestPurchaseHome_ReceiveGoodsCreateNotRead(t *testing.T) {
	w := newWorld(t)
	admin := w.seedUsers()
	const tile = `main a.newtile[href="/doc/` + goodsreceipt.DocType + `/new"]`
	for _, c := range []struct {
		email string
		grn   map[auth.Right]auth.Scope
		want  int
	}{
		{"grnread@vardhan.test", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}, 0},
		// AUTH needs read beside any other right.
		{"grncreate@vardhan.test", map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll}, 1},
	} {
		b := navRole(t, w, admin, c.email, purchase.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll})
		if err := w.app.Auth().SetRights(context.Background(), admin, "Only "+c.email, goodsreceipt.DocType, c.grn); err != nil {
			t.Fatalf("SetRights GRN: %v", err)
		}
		if got := b.get("/modules/purchase").doc.Find(tile).Length(); got != c.want {
			t.Errorf("%s: %d Receive goods tiles, want %d", c.email, got, c.want)
		}
	}
}
