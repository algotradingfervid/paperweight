package web

import (
	"net/url"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/types"
)

// The inbox card's "why it is with me" line and the filter by type
// (design-system §8.5, §4 rule 12 for showing the filter only when it helps;
// wp/polish plan P2). Page text is compared with goquery (lessons §10).

const (
	whyLabel      = "Why it is with you"
	emptyInbox    = "Nothing is waiting for your approval."
	emptyOfType   = "Nothing of this type is waiting for your approval."
	filterBar     = "form.filterbar"
	chipSelector  = ".filterbar-chips a.chip"
	orderTypePath = "/approvals?type=" + at.Order
	claimTypePath = "/approvals?type=" + at.Claim
)

// squash is s with its runs of whitespace reduced to one space.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// whyOf is the value of the Why pair on the card titled title. The card must
// have exactly one such pair.
func whyOf(t *testing.T, doc *goquery.Document, title string) string {
	t.Helper()
	card := doc.Find("section.card").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return strings.TrimSpace(s.Find(".card-title").Text()) == title
	})
	if card.Length() != 1 {
		t.Fatalf("%d cards titled %s, want 1", card.Length(), title)
	}
	var vals []string
	card.Find("dt").Each(func(_ int, s *goquery.Selection) {
		if strings.TrimSpace(s.Text()) == whyLabel {
			vals = append(vals, strings.TrimSpace(s.Next().Text()))
		}
	})
	if len(vals) != 1 {
		t.Fatalf("card %s has %d %q pairs, want 1; card text %q", title, len(vals), whyLabel, squash(card.Text()))
	}
	return vals[0]
}

// typeLink is one per-type filter link: its ?type= value, label and count.
type typeLink struct{ Type, Label, Count string }

// linkText splits a filter link's text. The separator between label and count
// is the page's own choice ("Purchase Order 3", "Purchase Order (3)").
var linkText = regexp.MustCompile(`^(.+?)[^\p{L}\d]*(\d+)[^\p{L}\d]*$`)

// typeLinks are the plain GET filter links in page order. Pagination links
// carry rel and the chip and Clear all links have no type, so neither counts.
func typeLinks(t *testing.T, doc *goquery.Document) []typeLink {
	t.Helper()
	var out []typeLink
	doc.Find(`a[href*="type="]`).Not("[rel]").Each(func(_ int, s *goquery.Selection) {
		href := s.AttrOr("href", "")
		u, err := url.Parse(href)
		if err != nil || u.Path != "/approvals" {
			t.Errorf("type link href %q, want a link to /approvals", href)
			return
		}
		m := linkText.FindStringSubmatch(squash(s.Text()))
		if m == nil {
			t.Errorf("type link %q text %q has no label and count", href, squash(s.Text()))
			return
		}
		out = append(out, typeLink{u.Query().Get("type"), m[1], m[2]})
	})
	return out
}

// pagerLink is the query of the pagination link rel on doc; ok is false when
// the page has none.
func pagerLink(t *testing.T, doc *goquery.Document, rel string) (href string, q url.Values, ok bool) {
	t.Helper()
	a := doc.Find(`nav.pagination a[rel="` + rel + `"]`)
	if a.Length() == 0 {
		return "", nil, false
	}
	href = a.AttrOr("href", "")
	u, err := url.Parse(href)
	if err != nil || u.Path != "/approvals" {
		t.Fatalf("%s link %q, want a link to /approvals", rel, href)
	}
	return href, u.Query(), true
}

func wantRange(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()
	if got := squash(doc.Find(".pagination-range").Text()); got != want {
		t.Errorf("page range %q, want %q", got, want)
	}
}

func wantEmptyState(t *testing.T, doc *goquery.Document, want string) {
	t.Helper()
	if got := squash(doc.Find(".empty-state p").Text()); got != want {
		t.Errorf("empty state %q, want %q", got, want)
	}
}

func wantNoFilter(t *testing.T, label string, doc *goquery.Document) {
	t.Helper()
	if n := doc.Find(filterBar).Length(); n != 0 {
		t.Errorf("%s: %d filter bars, want none", label, n)
	}
	if got := typeLinks(t, doc); len(got) != 0 {
		t.Errorf("%s: type links %+v, want none", label, got)
	}
	if n := doc.Find(chipSelector).Length(); n != 0 {
		t.Errorf("%s: %d chips, want none", label, n)
	}
}

// wantChip checks the one removable chip names label and that it and Clear
// all go back to the whole inbox.
func wantChip(t *testing.T, doc *goquery.Document, label string) {
	t.Helper()
	chip := doc.Find(chipSelector)
	if chip.Length() != 1 {
		t.Fatalf("%d chips, want 1", chip.Length())
	}
	if txt := squash(chip.Text()); !strings.Contains(txt, label) {
		t.Errorf("chip text %q does not name %q", txt, label)
	}
	if got := chip.AttrOr("href", ""); got != "/approvals" {
		t.Errorf("chip removes to %q, want /approvals", got)
	}
	clearAll := doc.Find(".filterbar-chips a").FilterFunction(func(_ int, s *goquery.Selection) bool {
		return squash(s.Text()) == "Clear all"
	})
	if got := clearAll.AttrOr("href", ""); clearAll.Length() != 1 || got != "/approvals" {
		t.Errorf("%d Clear all links to %q, want one to /approvals", clearAll.Length(), got)
	}
	// Works without JavaScript: a GET form and plain links.
	if n := doc.Find(filterBar + `[method="get"][action="/approvals"]`).Length(); n != 1 {
		t.Errorf("%d GET filter bars on /approvals, want 1", n)
	}
}

// ordersAndClaim gives Meena, oldest first, Draft 41 and Draft 42 (Orders) and
// Draft 10 (a Claim); Ravi sees the two orders only.
func ordersAndClaim(t *testing.T) *at.Env {
	t.Helper()
	e := at.New(t)
	for i, id := range []int64{41, 42} {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, id, at.Rs(60000), e.Priya)))
	}
	e.Clock.Set(time.Date(2026, 10, 24, 5, 2, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Deepak, e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)))
	return e
}

var meenaLinks = []typeLink{{at.Claim, "Expense Claim", "1"}, {at.Order, "Purchase Order", "2"}}

func TestInboxCard_ShowsWhyItIsWithYou(t *testing.T) {
	e := ordersAndClaim(t)
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 50, at.Rs(6_00_000), e.Priya)))
	doc := page(t, e, e.Meena, "/approvals")
	for title, want := range map[string]string{
		"Draft 41": "₹50,000 or more",
		"Draft 50": "₹5,00,000 or more",
		"Draft 10": "Any amount",
	} {
		if got := whyOf(t, doc, title); got != want {
			t.Errorf("%s: %s = %q, want %q", title, whyLabel, got, want)
		}
	}
}

func TestInboxCard_WhyKeepsThresholdAfterRuleEdit(t *testing.T) {
	e := at.New(t)
	at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)))
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{
		DocType: at.Order, FromAmount: at.Rs(40_000), ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	})
	at.MustOK(t, err)
	if got, want := whyOf(t, page(t, e, e.Ravi, "/approvals"), "Draft 42"), "₹50,000 or more"; got != want {
		t.Errorf("%s = %q after the rule edit, want %q", whyLabel, got, want)
	}
}

func TestInboxFilter_OneLinkPerTypeWithCount(t *testing.T) {
	e := ordersAndClaim(t)
	doc := page(t, e, e.Meena, "/approvals")
	if got := cardTitles(doc); !slices.Equal(got, []string{"Draft 41", "Draft 42", "Draft 10"}) {
		t.Errorf("cards %q, want every request", got)
	}
	if got := typeLinks(t, doc); !slices.Equal(got, meenaLinks) {
		t.Errorf("type links %+v, want %+v", got, meenaLinks)
	}
	if n := doc.Find(chipSelector).Length(); n != 0 {
		t.Errorf("%d chips with no filter on, want none", n)
	}
}

// §4 rule 12: no filter when there is nothing to choose between.
func TestInboxFilter_HiddenWithOneTypeAndNoFilter(t *testing.T) {
	e := ordersAndClaim(t)
	ravi := page(t, e, e.Ravi, "/approvals")
	if got := cardTitles(ravi); !slices.Equal(got, []string{"Draft 41", "Draft 42"}) {
		t.Fatalf("Ravi's cards %q, want the two orders", got)
	}
	wantNoFilter(t, "one type", ravi)

	nothing := page(t, e, e.Kavya, "/approvals")
	wantNoFilter(t, "empty inbox", nothing)
	wantEmptyState(t, nothing, emptyInbox)

	// The same inbox data with two types to choose between does show it.
	if n := page(t, e, e.Meena, "/approvals").Find(filterBar).Length(); n != 1 {
		t.Errorf("Meena's two types: %d filter bars, want 1", n)
	}
}

func TestInboxFilter_ActiveTypeHasRemovableChip(t *testing.T) {
	e := ordersAndClaim(t)
	doc := page(t, e, e.Meena, claimTypePath)
	if got := cardTitles(doc); !slices.Equal(got, []string{"Draft 10"}) {
		t.Errorf("cards %q, want the claim only", got)
	}
	wantRange(t, doc, "1–1 of 1")
	wantChip(t, doc, "Expense Claim")
	if got := typeLinks(t, doc); !slices.Equal(got, meenaLinks) {
		t.Errorf("type links with a filter on %+v, want %+v", got, meenaLinks)
	}

	// A filter on keeps the bar even when the inbox holds one type.
	ravi := page(t, e, e.Ravi, orderTypePath)
	if got := cardTitles(ravi); !slices.Equal(got, []string{"Draft 41", "Draft 42"}) {
		t.Errorf("Ravi's filtered cards %q, want the two orders", got)
	}
	wantChip(t, ravi, "Purchase Order")
	if got, want := typeLinks(t, ravi), []typeLink{{at.Order, "Purchase Order", "2"}}; !slices.Equal(got, want) {
		t.Errorf("Ravi's type links %+v, want %+v", got, want)
	}
}

func TestInboxFilter_PaginationKeepsType(t *testing.T) {
	e := at.New(t)
	at.MustOK(t, e.Submit(e.Deepak, e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)))
	for i := range 26 {
		e.Clock.Set(time.Date(2026, 10, 24, 5, 1+i, 0, 0, time.UTC))
		at.MustOK(t, e.Submit(e.Priya, e.NewDoc(t, at.Order, int64(100+i), at.Rs(60000), e.Priya)))
	}
	links := []typeLink{{at.Claim, "Expense Claim", "1"}, {at.Order, "Purchase Order", "26"}}

	first := page(t, e, e.Meena, orderTypePath)
	if got := len(cardTitles(first)); got != 25 {
		t.Errorf("first filtered page has %d cards, want 25", got)
	}
	wantRange(t, first, "1–25 of 26") // Total follows the filter: 27 requests in all
	href, q, ok := pagerLink(t, first, "next")
	if !ok || q.Get("type") != at.Order || q.Get("page") != "2" {
		t.Fatalf("next link %q, want type=%s and page=2", href, at.Order)
	}

	second := page(t, e, e.Meena, href)
	if got := cardTitles(second); !slices.Equal(got, []string{"Draft 125"}) {
		t.Errorf("second filtered page cards %q, want [Draft 125]", got)
	}
	wantRange(t, second, "26–26 of 26")
	if got := typeLinks(t, second); !slices.Equal(got, links) {
		t.Errorf("type links on page 2 %+v, want %+v", got, links)
	}
	href, q, ok = pagerLink(t, second, "prev")
	if !ok || q.Get("type") != at.Order || q.Get("page") != "1" {
		t.Errorf("previous link %q, want type=%s and page=1", href, at.Order)
	}

	// With no filter on the links don't invent one.
	whole := page(t, e, e.Meena, "/approvals")
	wantRange(t, whole, "1–25 of 27")
	href, q, ok = pagerLink(t, whole, "next")
	if !ok || q.Has("type") || q.Get("page") != "2" {
		t.Errorf("unfiltered next link %q, want page=2 and no type", href)
	}
}

func TestInboxFilter_NothingOfTypeShowsEmptyState(t *testing.T) {
	e := ordersAndClaim(t)
	for _, c := range []struct {
		label string
		p     auth.Principal
		path  string
	}{
		{"unknown type", e.Meena, "/approvals?type=no.such.Type"},
		{"type absent from the inbox", e.Ravi, claimTypePath},
		{"empty inbox", e.Kavya, orderTypePath},
		{"markup in the value", e.Meena, "/approvals?type=%3Cscript%3Ealert(1)%3C%2Fscript%3E"},
	} {
		doc := page(t, e, c.p, c.path)
		if got := cardTitles(doc); len(got) != 0 {
			t.Errorf("%s: cards %q, want none", c.label, got)
		}
		wantEmptyState(t, doc, emptyOfType)
		if n := doc.Find(chipSelector).Length(); n != 1 {
			t.Errorf("%s: %d chips, want 1 so the filter can be removed", c.label, n)
		}
		if n := doc.Find("script").Length(); n != 0 {
			t.Errorf("%s: %d script elements, want the value escaped", c.label, n)
		}
	}
}

// The header badge counts the whole inbox, filter or no filter.
func TestInboxCount_IgnoresTypeFilter(t *testing.T) {
	e := ordersAndClaim(t)
	// The filter does narrow the list, so the badge below really differs from it.
	if got := cardTitles(page(t, e, e.Meena, claimTypePath)); !slices.Equal(got, []string{"Draft 10"}) {
		t.Errorf("filtered cards %q, want the claim only", got)
	}
	for _, path := range []string{"/approvals/count", "/approvals/count?type=" + at.Claim, "/approvals/count?type=no.such.Type"} {
		if got := strings.TrimSpace(page(t, e, e.Meena, path).Text()); got != "3" {
			t.Errorf("GET %s: badge %q, want 3", path, got)
		}
	}
}

// realDirectors builds an Approvals on the real auth.Service over e's database
// and two new Directors on it, the first restricted to PRJ-METRO. Their ids (9
// and 10) are not those of the document creators used below: a shared id would
// make them the owner.
func realDirectors(t *testing.T, e *at.Env) (svc *approval.Approvals, restricted, open auth.Principal) {
	t.Helper()
	ctx := t.Context()
	a := auth.New(auth.Deps{
		DB: e.DB, Clock: e.Clock, Directory: e.Dir, Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{},
	})
	principal := func(u auth.UserID) auth.Principal {
		t.Helper()
		p, _, err := a.LoadPrincipal(ctx, u)
		at.MustOK(t, err)
		return p
	}
	admin := principal(e.Anil.ID)
	for _, dt := range []string{at.Order, at.Claim} {
		at.MustOK(t, a.SetRights(ctx, admin, "Director", dt, map[auth.Right]auth.Scope{"read": auth.ScopeAll, "approve": auth.ScopeAll}))
	}
	user := func(email string) auth.UserID {
		t.Helper()
		id, _, err := a.CreateUser(ctx, admin, auth.NewUser{Email: email, Name: email, Password: e.F.Password, Roles: []string{"Director"}})
		at.MustOK(t, err)
		return id
	}
	restrictedID, openID := user("inbox-restricted@vardhan.test"), user("inbox-open@vardhan.test")
	at.MustOK(t, a.ChangeRestrictions(ctx, admin, restrictedID, auth.DimProject, []int64{at.ProjMetro}, nil, nil))
	svc = approval.New(approval.Deps{DB: e.DB, Clock: e.Clock, Auth: a, Info: e.Docs, Directory: e.Dir, Audit: e.Audit})
	return svc, principal(restrictedID), principal(openID)
}

// The filter must not leak (lessons §5, an engine test that depends on
// restrictions has a case on the real auth.Service): a restricted approver
// sees no type link, count or card for a document they can't read.
func TestInboxFilter_RestrictedUserSeesNoUnreadableType_RealAuth(t *testing.T) {
	e := at.New(t)
	e.Auth.AddRole(e.Kavya.ID, "Purchase User")
	onFake := e.A
	at.MustOK(t, e.Submit(e.Kavya, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Kavya, at.Project(at.ProjTechPark))))
	e.Clock.Set(time.Date(2026, 10, 24, 5, 1, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Deepak, e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)))
	onReal, restricted, open := realDirectors(t, e)
	e.A = onReal
	claim := approval.InboxType{DocType: at.Claim, Label: "Expense Claim", Count: 1}

	// Every order is out of reach: the type doesn't exist for this user.
	if got, want := e.InboxTitles(t, restricted), []string{"Draft 10"}; !slices.Equal(got, want) {
		t.Fatalf("restricted inbox = %q, want %q", got, want)
	}
	if got, want := e.InboxTitles(t, open), []string{"Draft 42", "Draft 10"}; !slices.Equal(got, want) {
		t.Fatalf("unrestricted inbox = %q, want %q", got, want)
	}
	_, got, err := e.A.InboxOfType(t.Context(), restricted, "", types.Page{Number: 1, Size: 25})
	at.MustOK(t, err)
	if want := []approval.InboxType{claim}; !slices.Equal(got, want) {
		t.Errorf("restricted types = %+v, want %+v", got, want)
	}
	doc := page(t, e, restricted, "/approvals")
	wantNoFilter(t, "restricted, one readable type", doc)
	if txt := doc.Text(); strings.Contains(txt, "Purchase Order") || strings.Contains(txt, "Draft 42") {
		t.Errorf("restricted page names the unreadable order: %q", squash(txt))
	}

	// Asking for the type by URL gives nothing away either.
	doc = page(t, e, restricted, orderTypePath)
	if got := cardTitles(doc); len(got) != 0 {
		t.Errorf("restricted, filtered to orders: cards %q, want none", got)
	}
	wantEmptyState(t, doc, emptyOfType)
	if got, want := typeLinks(t, doc), []typeLink{{at.Claim, "Expense Claim", "1"}}; !slices.Equal(got, want) {
		t.Errorf("restricted, filtered to orders: type links %+v, want %+v", got, want)
	}

	if got, want := typeLinks(t, page(t, e, open, "/approvals")), []typeLink{{at.Claim, "Expense Claim", "1"}, {at.Order, "Purchase Order", "1"}}; !slices.Equal(got, want) {
		t.Errorf("unrestricted type links %+v, want %+v", got, want)
	}

	// One order in reach: the count is the readable one, never both.
	e.A = onFake
	e.Clock.Set(time.Date(2026, 10, 24, 5, 2, 0, 0, time.UTC))
	at.MustOK(t, e.Submit(e.Kavya, e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Kavya, at.Project(at.ProjMetro))))
	e.A = onReal

	doc = page(t, e, restricted, "/approvals")
	if got, want := typeLinks(t, doc), []typeLink{{at.Claim, "Expense Claim", "1"}, {at.Order, "Purchase Order", "1"}}; !slices.Equal(got, want) {
		t.Errorf("restricted type links %+v, want %+v", got, want)
	}
	doc = page(t, e, restricted, orderTypePath)
	if got := cardTitles(doc); !slices.Equal(got, []string{"Draft 43"}) {
		t.Errorf("restricted, filtered to orders: cards %q, want [Draft 43]", got)
	}
	wantRange(t, doc, "1–1 of 1")
	if got, want := typeLinks(t, page(t, e, open, "/approvals")), []typeLink{{at.Claim, "Expense Claim", "1"}, {at.Order, "Purchase Order", "2"}}; !slices.Equal(got, want) {
		t.Errorf("unrestricted type links %+v, want %+v", got, want)
	}
}
