package purchaseweb

import (
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// homeCard is one card the home page shows: its title, figure, line and the
// Open link's href.
type homeCard struct{ title, figure, line, href string }

// homeCards reads the count cards of the home page, in page order.
func homeCards(p p15ePage) []homeCard {
	var out []homeCard
	p.doc.Find("section.card").Each(func(_ int, s *goquery.Selection) {
		if s.Find("p.num").Length() == 0 {
			return // Recent comments
		}
		href, _ := s.Find(".card-head a").Attr("href")
		out = append(out, homeCard{
			title: squash(s.Find("h2.card-title").Text()), figure: squash(s.Find("p.num").Text()),
			line: squash(s.Find("p.num").Next().Text()), href: href,
		})
	})
	return out
}

// barButtons are the action bar's buttons as "label href variant".
func barButtons(p p15ePage) []string {
	var out []string
	p.doc.Find(".actionbar .btn").Each(func(_ int, s *goquery.Selection) {
		v := "secondary"
		if s.HasClass("btn-primary") {
			v = "primary"
		}
		href, _ := s.Attr("href")
		out = append(out, squash(s.Text())+" "+href+" "+v)
	})
	return out
}

const (
	newOrderPrimary     = "New purchase order /doc/purchase.Order/new primary"
	newRequestSecondary = "New purchase request /doc/purchase.Request/new secondary"
	newRequestPrimary   = "New purchase request /doc/purchase.Request/new primary"
)

// MOD-PO P15e, PUX-001 (UX §5.1, spec §25, CD-258 Q4/Q5/Q10): Priya's home
// shows only her non-zero cards, in the action-first order, each count line
// in the singular for 1, the Open links to each queue (Overdue deliveries to
// the overdue report), Ravi's comment on her draft under Recent comments,
// and the bar `New purchase order` (primary) and `New purchase request`.
// The clock is just after IST midnight on 27-10 (18:31 UTC on 26-10), so a
// PO due 26-10 is overdue only by the IST date.
func TestPurchaseWeb_HomePriya(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	ctx := t.Context()

	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	pending := purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	if _, err := env.Engine.Do(ctx, env.Ravi, purchase.DocType, pending.ID, doctype.ActionReject, doc.ActionInput{Version: pending.Version, Reason: "Wrong vendor"}); err != nil {
		t.Fatalf("setup: reject: %v", err)
	}
	purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"required_by": "26-10-2026"}))
	purchasetest.NewRequest(t, env)
	if _, err := env.Comments.AddComment(ctx, env.Ravi, purchase.DocType, draft.ID, "Use Bharat Tools for the bolts"); err != nil {
		t.Fatalf("setup: comment: %v", err)
	}
	env.SetNow(t, time.Date(2026, time.October, 26, 18, 31, 0, 0, time.UTC))

	home, err := env.Service.Home(ctx, env.Priya)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	waiting := "1 order"
	for _, c := range home.Cards {
		if c.Key == purchase.QueueWaiting && c.WaitingDays > 0 {
			waiting += fmt.Sprintf(" · oldest waiting %d days", c.WaitingDays)
		}
	}

	page := w.get(t, env.Priya, "/modules/purchase")
	if page.status != http.StatusOK {
		t.Fatalf("GET home = %d %q", page.status, page.text)
	}
	want := []homeCard{
		{"My drafts", "3", "3 orders · 1 needs changes", "/modules/purchase/q/drafts"},
		{"To send", "1", "1 approved order not sent yet", "/modules/purchase/q/to-send"},
		{"Requests to order", "1", "1 request with items to order", "/modules/purchase/q/requests-to-order"},
		{"Waiting for approval", "1", waiting, "/modules/purchase/q/waiting"},
		{"Awaiting delivery", "1", "1 order not fully received", "/modules/purchase/q/awaiting-delivery"},
		{"Overdue deliveries", "1", "1 line past its due date", "/reports/purchase/pending?overdue=1"},
	}
	if got := homeCards(page); !slices.Equal(got, want) {
		t.Errorf("cards =\n%v\nwant\n%v", got, want)
	}
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"Purchase"}) {
		t.Errorf("h1 = %q, want Purchase", h1)
	}
	comments := page.texts("section.card:has(h2:contains('Recent comments')) li p")
	if want := []string{"Ravi Kulkarni on Draft " + strconv.FormatInt(draft.ID, 10), "24-10-2026 10:30", "Use Bharat Tools for the bolts"}; !slices.Equal(comments, want) {
		t.Errorf("recent comments = %q, want %q", comments, want)
	}
	if href, _ := page.attr("section.card:has(h2:contains('Recent comments')) li a", "href"); href != "/doc/purchase.Order/"+strconv.FormatInt(draft.ID, 10)+"#comments" {
		t.Errorf("recent comment link = %q, want the draft's comments", href)
	}
	if got := barButtons(page); !slices.Equal(got, []string{newOrderPrimary, newRequestSecondary}) {
		t.Errorf("bar = %q, want %q and %q", got, newOrderPrimary, newRequestSecondary)
	}
}

// MOD-PO P15e, PUX-002, PUX-003 and the To approve card (UX §5.1; CD-258
// Q10 (c)): Suresh has no My drafts, To send or Requests to order card and
// only `New purchase request`, as primary; Ravi (approve) gets To approve,
// linking to /approvals; a user with every count 0 and no comments sees
// `Nothing waiting for you.` and no cards; a user with neither read gets
// AUTH's 403.
func TestPurchaseWeb_HomeOthers(t *testing.T) {
	env := p15eRuleEnv(t)
	w := newP15eWeb(t, env)
	purchasetest.NewOrder(t, env, purchasetest.StopAtPending())
	purchasetest.NewOrder(t, env)

	t.Run("Suresh", func(t *testing.T) {
		page := w.get(t, env.Suresh, "/modules/purchase")
		for _, c := range homeCards(page) {
			if slices.Contains([]string{"My drafts", "To send", "Requests to order", "To approve", "To close"}, c.title) {
				t.Errorf("Suresh sees the %s card", c.title)
			}
		}
		if got := barButtons(page); !slices.Equal(got, []string{newRequestPrimary}) {
			t.Errorf("Suresh's bar = %q, want only %q", got, newRequestPrimary)
		}
	})

	t.Run("Ravi", func(t *testing.T) {
		page := w.get(t, env.Ravi, "/modules/purchase")
		cards := homeCards(page)
		if len(cards) == 0 || cards[0] != (homeCard{"To approve", "1", "1 waiting for your approval", "/approvals"}) {
			t.Errorf("Ravi's cards = %v, want To approve first with 1 waiting", cards)
		}
		if got := barButtons(page); !slices.Equal(got, []string{newRequestPrimary}) {
			t.Errorf("Ravi's bar = %q, want only %q (no create on Order)", got, newRequestPrimary)
		}
	})

	t.Run("nothing waiting", func(t *testing.T) {
		page := w.get(t, env.Kavya, "/modules/purchase")
		if page.status != http.StatusOK || len(homeCards(page)) != 0 || !strings.Contains(page.text, "Nothing waiting for you.") {
			t.Errorf("Kavya's home = %d, cards %v, text %q; want 200, no cards and the empty text", page.status, homeCards(page), page.text)
		}
	})

	t.Run("create rights", func(t *testing.T) {
		orders := p15eUser(t, env, "Order Maker", map[string][]auth.Right{purchase.DocType: {auth.RightRead, auth.RightCreate}})
		if got := barButtons(w.get(t, orders, "/modules/purchase")); !slices.Equal(got, []string{newOrderPrimary}) {
			t.Errorf("create on Order only: bar = %q, want only %q", got, newOrderPrimary)
		}
		readers := p15eUser(t, env, "Order Looker", map[string][]auth.Right{purchase.DocType: {auth.RightRead}})
		if got := barButtons(w.get(t, readers, "/modules/purchase")); len(got) != 0 {
			t.Errorf("read only: bar = %q, want none", got)
		}
	})

	t.Run("no read is 403", func(t *testing.T) {
		nobody := p15eUser(t, env, "Nina Bose", nil)
		page := w.get(t, nobody, "/modules/purchase")
		if page.status != http.StatusForbidden {
			t.Errorf("GET home with no read = %d %q, want 403", page.status, page.text)
		}
	})
}

// MOD-PO P15e, PUX-004 and PUX-021's queue part (UX §5.2; plan P14):
// each queue lists as many rows as its card counts, with the card's title
// as h1, the row's meta and a 25-row pager; an unknown queue is AUTH-108's
// 404; my-requests shows a submitted request's badge as `Submitted` and the
// `New purchase request` bar; to-close has no bar.
func TestPurchaseWeb_Queues(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	po := purchasetest.NewOrder(t, env)
	purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	home, err := env.Service.Home(t.Context(), env.Priya)
	if err != nil {
		t.Fatalf("Home: %v", err)
	}
	for _, c := range home.Cards {
		if c.Key == purchase.CardOverdue || c.Key == purchase.CardToApprove {
			continue
		}
		page := w.get(t, env.Priya, "/modules/purchase/q/"+c.Key)
		if page.status != http.StatusOK {
			t.Errorf("GET queue %s = %d %q", c.Key, page.status, page.text)
			continue
		}
		if rows := page.doc.Find(".datalist-rows > li").Length(); rows != c.Count {
			t.Errorf("queue %s lists %d rows, want the card's %d", c.Key, rows, c.Count)
		}
	}

	page := w.get(t, env.Priya, "/modules/purchase/q/to-send")
	if h1 := page.texts("h1"); !slices.Equal(h1, []string{"To send"}) {
		t.Errorf("to-send h1 = %q", h1)
	}
	row := page.texts(".datalist-rows > li")
	if len(row) != 1 || !strings.Contains(row[0], po.Number) || !strings.Contains(row[0], "Approved on 24-10-2026") || !strings.Contains(row[0], "₹7,375.00") {
		t.Errorf("to-send rows = %q, want %s approved on 24-10-2026 for ₹7,375.00", row, po.Number)
	}
	if !strings.Contains(page.text, "1 order") {
		t.Errorf("to-send page has no `1 order` count line: %q", page.text)
	}
	if got := barButtons(page); !slices.Equal(got, []string{newOrderPrimary}) {
		t.Errorf("to-send bar = %q, want %q", got, newOrderPrimary)
	}
	if got := barButtons(w.get(t, env.Ravi, "/modules/purchase/q/to-send")); len(got) != 0 {
		t.Errorf("Ravi's to-send bar = %q, want none (no create on Order)", got)
	}

	if page := w.get(t, env.Priya, "/modules/purchase/q/to-close"); page.status != http.StatusOK ||
		!strings.Contains(page.text, "No orders are waiting to be closed.") || len(barButtons(page)) != 0 {
		t.Errorf("to-close = %d %q, bar %q; want the empty text and no bar", page.status, page.text, barButtons(page))
	}

	if page := w.get(t, env.Priya, "/modules/purchase/q/nope"); page.status != http.StatusNotFound || !strings.Contains(page.text, auth.MsgNotFound) {
		t.Errorf("GET /q/nope = %d %q, want 404 with %q", page.status, page.text, auth.MsgNotFound)
	}

	t.Run("my-requests", func(t *testing.T) {
		pr := purchasetest.NewRequest(t, env)
		page := w.get(t, env.Suresh, "/modules/purchase/q/my-requests")
		row := page.texts(".datalist-rows > li")
		if h1 := page.texts("h1"); !slices.Equal(h1, []string{"My purchase requests"}) {
			t.Errorf("my-requests h1 = %q", h1)
		}
		if len(row) != 1 || !strings.Contains(row[0], pr.Number) || !strings.Contains(row[0], "Submitted") || strings.Contains(row[0], "Approved") {
			t.Errorf("my-requests rows = %q, want %s with the Submitted badge", row, pr.Number)
		}
		if got := barButtons(page); !slices.Equal(got, []string{newRequestPrimary}) {
			t.Errorf("my-requests bar = %q, want %q", got, newRequestPrimary)
		}
	})

	t.Run("pages of 25", func(t *testing.T) {
		for range purchase.QueuePageSize {
			purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		}
		page := w.get(t, env.Priya, "/modules/purchase/q/drafts")
		if n := page.doc.Find(".datalist-rows > li").Length(); n != purchase.QueuePageSize || !strings.Contains(page.text, "1–25 of 26") {
			t.Errorf("drafts page 1 = %d rows, text %q; want 25 and `1–25 of 26`", n, page.text)
		}
		if next, _ := page.attr(".pagination a[rel=next]", "href"); next != "/modules/purchase/q/drafts?page=2" {
			t.Errorf("Next = %q", next)
		}
		if n := w.get(t, env.Priya, "/modules/purchase/q/drafts?page=2").doc.Find(".datalist-rows > li").Length(); n != 1 {
			t.Errorf("drafts page 2 = %d rows, want 1", n)
		}
	})
}

// MOD-PO P15e, PUX-034 (UX §5.1, §5.2; spec §27.2): two approved POs whose
// vendor won't send the rest give Priya the To close card after To send,
// `2 orders the vendor won't complete`, and its queue lists both with the
// meta; Suresh (no amend) has no To close card.
func TestPurchaseWeb_ToClose(t *testing.T) {
	env := purchasetest.New(t)
	w := newP15eWeb(t, env)
	for i, po := range []doc.Doc{purchasetest.NewOrder(t, env), purchasetest.NewOrder(t, env)} {
		if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return purchase.SetRestNotComing(t.Context(), tx, po.ID, int64(900+i), env.Suresh.ID, true)
		}); err != nil {
			t.Fatalf("setup: SetRestNotComing: %v", err)
		}
	}
	cards := homeCards(w.get(t, env.Priya, "/modules/purchase"))
	i := slices.IndexFunc(cards, func(c homeCard) bool { return c.title == "To close" })
	if i < 1 || cards[i-1].title != "To send" || cards[i].line != "2 orders the vendor won't complete" {
		t.Errorf("Priya's cards = %v, want To close after To send with `2 orders the vendor won't complete`", cards)
	}
	rows := w.get(t, env.Priya, "/modules/purchase/q/to-close").texts(".datalist-rows > li")
	if len(rows) != 2 || !strings.Contains(rows[0], "Vendor says the rest won't come · 24-10-2026") {
		t.Errorf("to-close rows = %q, want two with the meta", rows)
	}
	for _, c := range homeCards(w.get(t, env.Suresh, "/modules/purchase")) {
		if c.title == "To close" {
			t.Errorf("Suresh has the To close card")
		}
	}
}

// MOD-PO P15e (UX §5.1 copy): each card's line in the singular and plural,
// and the waiting line's oldest days only when WaitingDays is set.
func TestPurchaseWeb_HomeLines(t *testing.T) {
	for _, c := range []struct {
		card purchase.Card
		want string
	}{
		{purchase.Card{Key: purchase.CardToApprove, Count: 2}, "2 waiting for your approval"},
		{purchase.Card{Key: purchase.QueueDrafts, Count: 1}, "1 order"},
		{purchase.Card{Key: purchase.QueueDrafts, Count: 3, NeedsChanges: 2}, "3 orders · 2 need changes"},
		{purchase.Card{Key: purchase.QueueDrafts, Count: 3, NeedsChanges: 1}, "3 orders · 1 needs changes"},
		{purchase.Card{Key: purchase.QueueToSend, Count: 2}, "2 approved orders not sent yet"},
		{purchase.Card{Key: purchase.QueueToClose, Count: 1}, "1 order the vendor won't complete"},
		{purchase.Card{Key: purchase.QueueRequestsToOrder, Count: 2}, "2 requests with items to order"},
		{purchase.Card{Key: purchase.QueueWaiting, Count: 1, WaitingDays: 4}, "1 order · oldest waiting 4 days"},
		{purchase.Card{Key: purchase.QueueWaiting, Count: 2}, "2 orders"},
		{purchase.Card{Key: purchase.QueueMyRequests, Count: 3}, "3 open"},
		{purchase.Card{Key: purchase.QueueAwaitingDelivery, Count: 2}, "2 orders not fully received"},
		{purchase.Card{Key: purchase.CardOverdue, Count: 2}, "2 lines past their due date"},
		{purchase.Card{Key: purchase.CardOverdue, Count: 1}, "1 line past its due date"},
	} {
		if got := cardLine(c.card); got != c.want {
			t.Errorf("cardLine(%+v) = %q, want %q", c.card, got, c.want)
		}
	}
}

// MOD-PO P15e (UX §5.1 copy): a recent comment's body is cut at a word to
// 200 characters with `…`; a shorter one is whole.
func TestPurchaseWeb_CommentCut(t *testing.T) {
	short := "Use Bharat Tools"
	if got := cutComment(short); got != short {
		t.Errorf("cutComment(short) = %q", got)
	}
	long := strings.Repeat("abcd ", 50) // 250 characters
	got := cutComment(long)
	if !strings.HasSuffix(got, "abcd…") || len([]rune(got)) > 201 {
		t.Errorf("cutComment(250) = %q (%d), want at most 200 characters cut at a word, then …", got, len([]rune(got)))
	}
	if word := strings.Repeat("x", 250); cutComment(word) != strings.Repeat("x", 200)+"…" {
		t.Errorf("cutComment(one 250-character word) = %q, want 200 characters then …", cutComment(word))
	}
}
