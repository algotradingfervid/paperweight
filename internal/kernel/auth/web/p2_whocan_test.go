package authweb_test

import (
	"context"
	"html"
	"net/http"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
)

const (
	t19Path          = "/rights/who-can"
	t19NotApplicable = "Approve does not apply to Item. Clear it."
	t19AdminOnly     = "Only an administrator can see who has which rights."
)

// t19Names are the seeded users' names (00-fixtures.md).
var t19Names = []string{
	"Anil Kapoor", "Meena Iyer", "Ravi Kulkarni", "Priya Shah",
	"Suresh Patil", "Kavya Rao", "Deepak Nair", "Former Staff",
}

var (
	t19TitleRe  = regexp.MustCompile(`<h2 class="card-title">([^<]*)</h2>`)
	t19SectRe   = regexp.MustCompile(`</?section\b`)
	t19RightRe  = regexp.MustCompile(`(?s)<select\b[^>]*\bname="right"[^>]*>(.*?)</select>`)
	t19OptionRe = regexp.MustCompile(`(?s)<option\b[^>]*\bvalue="([^"]*)"[^>]*>(.*?)</option>`)
)

// t19Card is one rendered role Card: its title and the seeded names it
// lists, in page order.
type t19Card struct {
	Title string
	Names []string
	Body  string
}

// t19Cards returns the role Cards of a Who can… page: each
// <section class="card…"> (matched to its own </section>) whose card-title
// has " · ". Names are listed in order of first appearance.
func t19Cards(body string) []t19Card {
	var out []t19Card
	rest := body
	for {
		i := strings.Index(rest, `<section class="card`)
		if i < 0 {
			return out
		}
		rest = rest[i:]
		end, depth := len(rest), 0
		for _, m := range t19SectRe.FindAllStringIndex(rest, -1) {
			if strings.HasPrefix(rest[m[0]:], "</") {
				depth--
			} else {
				depth++
			}
			if depth == 0 {
				end = m[1]
				break
			}
		}
		seg := rest[:end]
		rest = rest[len(`<section`):]
		tm := t19TitleRe.FindStringSubmatch(seg)
		if tm == nil || !strings.Contains(html.UnescapeString(tm[1]), " · ") {
			continue
		}
		text := html.UnescapeString(seg)
		c := t19Card{Title: strings.TrimSpace(html.UnescapeString(tm[1])), Body: text}
		type at struct {
			name string
			pos  int
		}
		var found []at
		for _, n := range t19Names {
			if p := strings.Index(text, n); p >= 0 {
				found = append(found, at{n, p})
			}
		}
		for i := 1; i < len(found); i++ {
			for j := i; j > 0 && found[j].pos < found[j-1].pos; j-- {
				found[j], found[j-1] = found[j-1], found[j]
			}
		}
		for _, f := range found {
			c.Names = append(c.Names, f.name)
		}
		out = append(out, c)
	}
}

// t19Plain drops empty restriction maps, so an unrestricted user compares
// equal whether WhoCan gives nil or an empty map.
func t19Plain(groups []auth.WhoCanRole) []auth.WhoCanRole {
	for i := range groups {
		for j := range groups[i].Users {
			if len(groups[i].Users[j].Restrictions) == 0 {
				groups[i].Users[j].Restrictions = nil
			}
		}
	}
	return groups
}

// t19WantGroups checks the role Cards of a 200 page against want
// ({title, names...}).
func t19WantGroups(t *testing.T, what string, r webtest.Response, want []t19Card) []t19Card {
	t.Helper()
	wantStatus(t, what, r, http.StatusOK)
	if strings.Contains(r.Body, "<table") {
		t.Fatalf("%s: body has <table markup (design system §8.6)", what)
	}
	cards := t19Cards(r.Body)
	var got, exp []t19Card
	for _, c := range cards {
		got = append(got, t19Card{Title: c.Title, Names: c.Names})
	}
	for _, c := range want {
		exp = append(exp, t19Card{Title: c.Title, Names: c.Names})
	}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("%s: role cards = %+v, want %+v (body %q)", what, got, exp, r.Body)
	}
	return cards
}

// t19Under checks that label appears after name and before next (or the
// card's end when next is "") in the card's text, exactly once.
func t19Under(t *testing.T, c t19Card, name, label, next string) {
	t.Helper()
	if n := strings.Count(c.Body, label); n != 1 {
		t.Fatalf("%s card: %q appears %d times, want once (under %s): %q", c.Title, label, n, name, c.Body)
	}
	pos, from := strings.Index(c.Body, label), strings.Index(c.Body, name)
	to := len(c.Body)
	if next != "" {
		to = strings.Index(c.Body, next)
	}
	if pos < from || pos > to {
		t.Fatalf("%s card: %q is not under %s (before %q): %q", c.Title, label, name, next, c.Body)
	}
}

// AUTH-120
func TestWhoCan_RolesAndUsers(t *testing.T) {
	ctx := context.Background()
	w := authtest.NewWorld(t)
	id := func(email string) auth.UserID { return w.Users[email] }
	anil := auth.WhoCanUser{ID: id("admin@vardhan.test"), Name: "Anil Kapoor"}
	meena := auth.WhoCanUser{ID: id("meena@vardhan.test"), Name: "Meena Iyer"}
	ravi := auth.WhoCanUser{ID: id("ravi@vardhan.test"), Name: "Ravi Kulkarni"}

	// Service: Purchase Order, approve.
	got, err := w.Auth.WhoCan(ctx, authtest.DTOrder, auth.RightApprove)
	if err != nil {
		t.Fatalf("WhoCan(Order, approve): %v", err)
	}
	want := []auth.WhoCanRole{
		{Role: "Administrator", Scope: auth.ScopeAll, Users: []auth.WhoCanUser{anil}},
		{Role: "Director", Scope: auth.ScopeAll, Users: []auth.WhoCanUser{meena}},
		{Role: "Purchase Manager", Scope: auth.ScopeAll, Users: []auth.WhoCanUser{ravi}},
	}
	if got = t19Plain(got); !reflect.DeepEqual(got, want) {
		t.Fatalf("WhoCan(Order, approve) = %+v, want %+v", got, want)
	}

	// Service: Expense Claim, approve. Former Staff (Employee, disabled) is
	// not listed.
	got, err = w.Auth.WhoCan(ctx, authtest.DTClaim, auth.RightApprove)
	if err != nil {
		t.Fatalf("WhoCan(Claim, approve): %v", err)
	}
	want = []auth.WhoCanRole{
		{Role: "Administrator", Scope: auth.ScopeAll, Users: []auth.WhoCanUser{anil}},
		{Role: "Director", Scope: auth.ScopeAll, Users: []auth.WhoCanUser{meena}},
		{Role: "Employee", Scope: auth.ScopeTeam, Users: []auth.WhoCanUser{
			{ID: id("deepak@vardhan.test"), Name: "Deepak Nair"},
			{ID: id("kavya@vardhan.test"), Name: "Kavya Rao"},
			{ID: id("priya@vardhan.test"), Name: "Priya Shah", Restrictions: map[auth.Dimension][]string{auth.DimProject: {"PRJ-METRO"}}},
			ravi,
			{ID: id("suresh@vardhan.test"), Name: "Suresh Patil", Restrictions: map[auth.Dimension][]string{auth.DimSite: {"PUNE-WH", "METRO-SITE"}}},
		}},
	}
	if got = t19Plain(got); !reflect.DeepEqual(got, want) {
		t.Fatalf("WhoCan(Claim, approve) = %+v, want %+v", got, want)
	}

	// HTTP, as the administrator.
	h := webtest.New(t, w)
	admin := signedIn(t, h, "admin@vardhan.test")

	t19WantGroups(t, "GET who-can Order approve", admin.Get(t19Path+"?doctype=authtest.Order&right=approve"), []t19Card{
		{Title: "Administrator · every right", Names: []string{"Anil Kapoor"}},
		{Title: "Director · All", Names: []string{"Meena Iyer"}},
		{Title: "Purchase Manager · All", Names: []string{"Ravi Kulkarni"}},
	})

	claim := admin.Get(t19Path + "?doctype=authtest.Claim&right=approve")
	cards := t19WantGroups(t, "GET who-can Claim approve", claim, []t19Card{
		{Title: "Administrator · every right", Names: []string{"Anil Kapoor"}},
		{Title: "Director · All", Names: []string{"Meena Iyer"}},
		{Title: "Employee · Team", Names: []string{"Deepak Nair", "Kavya Rao", "Priya Shah", "Ravi Kulkarni", "Suresh Patil"}},
	})
	t19Under(t, cards[2], "Priya Shah", "Projects: PRJ-METRO", "Ravi Kulkarni")
	t19Under(t, cards[2], "Suresh Patil", "Sites: PUNE-WH, METRO-SITE", "")
	if p, s := strings.Count(cards[2].Body, "Projects:"), strings.Count(cards[2].Body, "Sites:"); p != 1 || s != 1 {
		t.Fatalf("Employee card: %d \"Projects:\" and %d \"Sites:\" lines, want one each (unrestricted users show none): %q", p, s, cards[2].Body)
	}
	if strings.Contains(html.UnescapeString(claim.Body), "Former Staff") {
		t.Fatalf("GET who-can Claim approve: lists Former Staff (disabled): %q", claim.Body)
	}

	// The right select offers only the rights that apply to the DocType.
	item := admin.Get(t19Path + "?doctype=authtest.Item")
	wantStatus(t, "GET who-can Item", item, http.StatusOK)
	sel := t19RightRe.FindStringSubmatch(item.Body)
	if sel == nil {
		t.Fatalf("GET who-can Item: no <select name=\"right\"> in %q", item.Body)
	}
	var values []string
	for _, o := range t19OptionRe.FindAllStringSubmatch(sel[1], -1) {
		values = append(values, o[1])
		label := strings.ToLower(html.UnescapeString(o[2]))
		for _, bad := range []string{"submit", "cancel", "amend", "approve"} {
			if o[1] == bad || strings.Contains(label, bad) {
				t.Fatalf("GET who-can Item: right select offers %q (%q); it doesn't apply to Item", o[1], o[2])
			}
		}
	}
	if !strings.Contains(strings.Join(values, ","), "read") {
		t.Fatalf("GET who-can Item: right select values = %q, want read among them", values)
	}

	// An inapplicable right asked for directly: 400 in the ErrorSummary.
	bad := admin.Get(t19Path + "?doctype=authtest.Item&right=approve")
	wantStatus(t, "GET who-can Item approve", bad, http.StatusBadRequest)
	body := html.UnescapeString(bad.Body)
	i := strings.Index(body, "error-summary")
	if i < 0 || !strings.Contains(body[i:], t19NotApplicable) {
		t.Fatalf("GET who-can Item approve: want %q inside the ErrorSummary, body %q", t19NotApplicable, bad.Body)
	}
	if strings.Contains(bad.Body, "<table") {
		t.Fatalf("GET who-can Item approve: body has <table markup")
	}

	// Refused: a non-admin gets 403.
	priya := signedIn(t, h, t10Priya)
	r := priya.Get(t19Path + "?doctype=authtest.Claim&right=approve")
	wantStatus(t, "priya GET who-can", r, http.StatusForbidden)
	if msg := html.UnescapeString(messageOf(r)); !strings.Contains(msg, t19AdminOnly) {
		t.Fatalf("priya GET who-can: body %q, want %q", r.Body, t19AdminOnly)
	}
	if strings.Contains(r.Body, "Meena Iyer") {
		t.Fatalf("priya GET who-can: the 403 leaks the result: %q", r.Body)
	}
}
