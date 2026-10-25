package components_test

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/icons"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func iconHTML(t *testing.T, name string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := icons.Icon(name).Render(context.Background(), &buf); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := doc.Find("svg").Html()
	return h
}

func text(s *goquery.Selection) string { return strings.Join(strings.Fields(s.Text()), " ") }

func TestStateBadge_WordIconForEveryState(t *testing.T) {
	cases := []struct {
		s          doctype.State
		word, icon string
	}{
		{doctype.StateDraft, "Draft", "pencil"},
		{doctype.StatePending, "Pending approval", "clock"},
		{doctype.StateApproved, "Approved", "check-circle"},
		{doctype.StateRejected, "Rejected", "x-circle"},
		{doctype.StateCancelled, "Cancelled", "ban"},
		{doctype.StateClosed, "Closed", "lock"},
	}
	for _, c := range cases {
		t.Run(string(c.s), func(t *testing.T) {
			if got := components.StateLabel(c.s); got != c.word {
				t.Errorf("StateLabel(%q) = %q, want %q", c.s, got, c.word)
			}
			doc := uitest.Render(t, components.StateBadge(c.s))
			b := doc.Find("span.badge")
			if b.Length() != 1 {
				t.Fatalf("want one span.badge, got %d", b.Length())
			}
			if !b.HasClass("badge--" + string(c.s)) {
				t.Errorf("class = %q, want badge--%s", b.AttrOr("class", ""), c.s)
			}
			if got := text(b); got != c.word {
				t.Errorf("text = %q, want %q", got, c.word)
			}
			svg, _ := b.Find("svg").Html()
			if svg != iconHTML(t, c.icon) {
				t.Errorf("icon is not %s", c.icon)
			}
		})
	}
	t.Run("unknown", func(t *testing.T) {
		if got := components.StateLabel("on_hold"); got != "on_hold" {
			t.Errorf("StateLabel = %q, want raw string", got)
		}
		b := uitest.Render(t, components.StateBadge("on_hold")).Find("span.badge")
		if !b.HasClass("badge--neutral") || text(b) != "on_hold" {
			t.Errorf("unknown state: class %q text %q", b.AttrOr("class", ""), text(b))
		}
	})
}

func pageNav(t *testing.T, p components.PageInfo) *goquery.Document {
	t.Helper()
	return uitest.Render(t, components.Pagination(p))
}

func TestPagination_TextAndEnds(t *testing.T) {
	first := pageNav(t, components.PageInfo{Page: 1, From: 1, To: 25, Total: 312, PrevURL: "/p?page=0", NextURL: "/p?page=2"})
	if got := text(first.Find("nav")); !strings.Contains(got, "1–25 of 312") {
		t.Errorf("text = %q, want 1–25 of 312", got)
	}
	if first.Find(`a[rel="prev"]`).Length() != 0 {
		t.Error("first page shows Previous")
	}
	next := first.Find(`a[rel="next"]`)
	if next.Length() != 1 || next.AttrOr("href", "") != "/p?page=2" || text(next) != "Next" {
		t.Errorf("first page Next = %d %q %q", next.Length(), next.AttrOr("href", ""), text(next))
	}

	last := pageNav(t, components.PageInfo{Page: 13, From: 301, To: 312, Total: 312, PrevURL: "/p?page=12", NextURL: "/p?page=14"})
	if last.Find(`a[rel="next"]`).Length() != 0 {
		t.Error("last page shows Next")
	}
	prev := last.Find(`a[rel="prev"]`)
	if prev.Length() != 1 || prev.AttrOr("href", "") != "/p?page=12" || text(prev) != "Previous" {
		t.Errorf("last page Previous = %d %q %q", prev.Length(), prev.AttrOr("href", ""), text(prev))
	}
	if got := text(last.Find("nav")); !strings.Contains(got, "301–312 of 312") {
		t.Errorf("text = %q", got)
	}
}

func TestPagination_PastEndShowsNoMoreResults(t *testing.T) {
	doc := pageNav(t, components.PageInfo{Page: 20, From: 476, To: 475, Total: 312, PrevURL: "/p?page=19"})
	if got := text(doc.Find("p")); got != "No more results." {
		t.Errorf("text = %q, want No more results.", got)
	}
	if strings.Contains(text(doc.Selection), "of 312") {
		t.Error("past-end page still shows a range")
	}
}

func TestFilterBar_URLState(t *testing.T) {
	p := components.FilterBarProps{
		Action: "/doc/po", Search: "cement", SearchLabel: "Search purchase orders", Sort: "date",
		Sorts:     []components.Option{{Value: "number", Label: "Number"}, {Value: "date", Label: "Date"}},
		Chips:     []components.Chip{{Label: "State: Draft", RemoveURL: "/doc/po?q=cement"}, {Label: "Site: Pune", RemoveURL: "/doc/po?state=draft"}},
		FilterURL: "/doc/po/filter", ClearURL: "/doc/po", Count: "312 purchase orders",
		New: &components.Link{Label: "New purchase order", Href: "/doc/po/new"}, Export: &components.Link{Label: "Export", Href: "/doc/po.csv"},
	}
	doc := uitest.Render(t, components.FilterBar(p))
	f := doc.Find("form")
	for attr, want := range map[string]string{
		"method": "get", "hx-get": "/doc/po", "hx-target": "#main", "hx-swap": "outerHTML", "hx-push-url": "true", "hx-trigger": "change, submit", "action": "/doc/po",
	} {
		if got := f.AttrOr(attr, ""); got != want {
			t.Errorf("form %s = %q, want %q", attr, got, want)
		}
	}
	in := f.Find(`input[type="search"]`)
	if in.AttrOr("value", "") != "cement" {
		t.Errorf("search value = %q", in.AttrOr("value", ""))
	}
	if id := in.AttrOr("id", ""); id == "" || text(doc.Find(`label[for="`+id+`"]`)) != "Search purchase orders" {
		t.Errorf("search input %q has no label with SearchLabel", id)
	}
	if got := f.Find("select option[selected]").AttrOr("value", ""); got != "date" {
		t.Errorf("selected sort = %q, want date", got)
	}
	if f.Find(`a[href="/doc/po/filter"]`).Length() != 1 {
		t.Error("no Filter link")
	}
	chips := f.Find("a.chip")
	if chips.Length() != 2 || chips.Eq(0).AttrOr("href", "") != "/doc/po?q=cement" || !strings.Contains(text(chips.Eq(1)), "Site: Pune") {
		t.Errorf("chips = %d", chips.Length())
	}
	clear := f.Find(`a[href="/doc/po"]`)
	if clear.Length() != 1 || text(clear) != "Clear all" {
		t.Errorf("Clear all = %d %q", clear.Length(), text(clear))
	}
	if !strings.Contains(text(f), "312 purchase orders") {
		t.Error("no count line")
	}
	if f.Find(`a[href="/doc/po/new"]`).Length() != 1 || f.Find(`a[href="/doc/po.csv"]`).Length() != 1 {
		t.Error("New or Export missing")
	}

	p.Chips = nil
	doc = uitest.Render(t, components.FilterBar(p))
	if strings.Contains(text(doc.Selection), "Clear all") || doc.Find("a.chip").Length() != 0 {
		t.Error("Clear all shown without chips")
	}
}

func TestFilterBar_KeepsParams(t *testing.T) {
	doc := uitest.Render(t, components.FilterBar(components.FilterBarProps{
		Action: "/doc/po", SearchLabel: "Search purchase orders",
		Keep: []components.Param{{Name: "f", Value: "state:=:draft"}, {Name: "f", Value: "site_id:=:3"}, {Name: "size", Value: "50"}},
	}))
	hidden := doc.Find(`form input[type="hidden"]`)
	var got []string
	hidden.Each(func(_ int, s *goquery.Selection) {
		got = append(got, s.AttrOr("name", "")+"="+s.AttrOr("value", ""))
	})
	want := []string{"f=state:=:draft", "f=site_id:=:3", "size=50"}
	if !slices.Equal(got, want) {
		t.Errorf("hidden inputs = %q, want %q", got, want)
	}
}

func TestActionBar_NoteReplacesButtons(t *testing.T) {
	doc := uitest.Render(t, components.ActionBar(components.ActionBarProps{
		Primary: &components.ButtonProps{Label: "Submit for approval", Variant: components.Primary},
		Note:    "Waiting for approval by Asha Rao.",
	}))
	bar := doc.Find("div.actionbar")
	if bar.Length() != 1 || text(bar) != "Waiting for approval by Asha Rao." {
		t.Errorf("bar = %d %q", bar.Length(), text(bar))
	}
	if doc.Find("button, a").Length() != 0 {
		t.Error("note bar has buttons")
	}

	doc = uitest.Render(t, components.ActionBar(components.ActionBarProps{
		Primary:   &components.ButtonProps{Label: "Save and continue", Name: "nav", Value: "next", Variant: components.Primary},
		Secondary: &components.ButtonProps{Label: "Back", Href: "/doc/po/1/edit?step=1", Variant: components.Secondary},
		Form:      "wizard", FinishLater: true,
	}))
	b := doc.Find("div.actionbar button.btn-primary")
	if b.Length() != 1 || text(b) != "Save and continue" || b.AttrOr("form", "") != "wizard" || b.AttrOr("name", "") != "nav" {
		t.Errorf("primary = %d %q form %q", b.Length(), text(b), b.AttrOr("form", ""))
	}
	if doc.Find(`div.actionbar a.btn-outline[href="/doc/po/1/edit?step=1"]`).Length() != 1 {
		t.Error("secondary link missing")
	}
	fl := doc.Find(`button[name="nav"][value="finish"]`)
	if fl.Length() != 1 || text(fl) != "Save and finish later" || fl.AttrOr("form", "") != "wizard" || fl.ParentsFiltered(".actionbar").Length() != 0 {
		t.Errorf("finish later = %d %q", fl.Length(), text(fl))
	}
}

func TestActionBar_SubmitsCarryBusyAndForm(t *testing.T) {
	doc := uitest.Render(t, components.ActionBar(components.ActionBarProps{
		Primary: &components.ButtonProps{Label: "Save and continue", Name: "nav", Value: "next", Variant: components.Primary},
		Form:    "wizard", FinishLater: true,
	}))
	for _, sel := range []string{"div.actionbar button.btn-primary", `button[name="nav"][value="finish"]`} {
		b := doc.Find(sel)
		if b.Length() != 1 || b.AttrOr("data-busy-label", "") == "" || b.AttrOr("form", "") != "wizard" {
			t.Errorf("%s: n %d busy %q form %q", sel, b.Length(), b.AttrOr("data-busy-label", ""), b.AttrOr("form", ""))
		}
	}

	doc = uitest.Render(t, components.ActionBar(components.ActionBarProps{
		Primary: &components.ButtonProps{Label: "Save", Variant: components.Primary},
	}))
	b := doc.Find("div.actionbar button.btn-primary")
	if b.AttrOr("data-busy-label", "") == "" {
		t.Error("primary submit has no data-busy-label")
	}
	if _, ok := b.Attr("form"); ok {
		t.Error("form attribute set without Form")
	}

	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-1"})
	doc = uitest.RenderCtx(t, ctx, components.MoreActions([]components.MoreAction{
		{Label: "Cancel order", Href: "/doc/po/1/cancel", Post: true, Danger: true},
	}))
	if doc.Find(`form button[type="submit"]`).AttrOr("data-busy-label", "") == "" {
		t.Error("MoreActions POST button has no data-busy-label")
	}
}

func TestActionBar_EmptyRendersNothing(t *testing.T) {
	doc := uitest.Render(t, components.ActionBar(components.ActionBarProps{Form: "f"}))
	if h, _ := doc.Find("body").Html(); strings.TrimSpace(h) != "" {
		t.Errorf("empty bar rendered %q", h)
	}
}

func TestWarnings_OneBanner(t *testing.T) {
	doc := uitest.Render(t, components.Warnings([]string{"Party P-1 is on hold.", "Item I-9 is inactive."}))
	b := doc.Find(".banner--warning")
	if b.Length() != 1 {
		t.Fatalf("want one warning banner, got %d", b.Length())
	}
	if b.AttrOr("role", "") != "alert" {
		t.Errorf("role = %q", b.AttrOr("role", ""))
	}
	got := text(b)
	i, j := strings.Index(got, "Party P-1 is on hold."), strings.Index(got, "Item I-9 is inactive.")
	if i < 0 || j < i {
		t.Errorf("messages not both shown in order: %q", got)
	}
	if b.Find("li").Length() != 2 {
		t.Errorf("want 2 items, got %d", b.Find("li").Length())
	}

	one := uitest.Render(t, components.Warnings([]string{"Party P-1 is on hold."}))
	if one.Find(".banner--warning").Length() != 1 || !strings.Contains(text(one.Selection), "Party P-1 is on hold.") {
		t.Error("single warning not shown")
	}

	doc = uitest.Render(t, components.Warnings(nil))
	if h, _ := doc.Find("body").Html(); strings.TrimSpace(h) != "" {
		t.Errorf("no warnings rendered %q", h)
	}
}

func TestBanner_RoleByKind(t *testing.T) {
	for kind, role := range map[components.BannerKind]string{
		components.Success: "status", components.Info: "status", components.Warning: "alert", components.Danger: "alert",
	} {
		b := uitest.Render(t, components.Banner(components.BannerProps{Kind: kind, Message: "Saved."})).Find(".alert")
		if b.AttrOr("role", "") != role || !b.HasClass("banner--"+string(kind)) || b.Find("svg").Length() != 1 {
			t.Errorf("%s: role %q class %q", kind, b.AttrOr("role", ""), b.AttrOr("class", ""))
		}
	}
}

func TestDialog_NativeWithClose(t *testing.T) {
	for name, c := range map[string]func(string, string) templ.Component{
		"dialog": components.Dialog, "sheet": components.Sheet,
	} {
		doc := uitest.Render(t, c("pick-party", "Choose a party"))
		d := doc.Find("dialog#pick-party")
		if d.Length() != 1 || !d.HasClass("dialog") {
			t.Fatalf("%s: no dialog.dialog", name)
		}
		if name == "sheet" && !d.HasClass("sheet") {
			t.Errorf("sheet lacks class sheet")
		}
		lb := d.AttrOr("aria-labelledby", "")
		if lb == "" || text(doc.Find("#"+lb)) != "Choose a party" {
			t.Errorf("%s: aria-labelledby %q does not name the title", name, lb)
		}
		cl := d.Find(`button[data-close-dialog][aria-label="Close"]`)
		if cl.Length() != 1 || cl.AttrOr("type", "") != "button" || !cl.HasClass("btn--ghost") {
			t.Errorf("%s: close button missing", name)
		}
	}
}

func TestMoreActions_PostHasCSRFAndDangerClass(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-1"})
	doc := uitest.RenderCtx(t, ctx, components.MoreActions([]components.MoreAction{
		{Label: "Print", Href: "/doc/po/1/print", NewTab: true},
		{Label: "Cancel order", Href: "/doc/po/1/cancel", Post: true, Danger: true},
	}))
	if got := text(doc.Find("h2")); got != "More actions" {
		t.Errorf("heading = %q", got)
	}
	f := doc.Find(`form[method="post"][action="/doc/po/1/cancel"]`)
	if f.Find(`input[type="hidden"][name="csrf_token"]`).AttrOr("value", "") != "tok-1" {
		t.Error("POST action has no csrf_token")
	}
	b := f.Find(`button[type="submit"]`)
	if !b.HasClass("btn-error") || text(b) != "Cancel order" {
		t.Errorf("danger button class %q text %q", b.AttrOr("class", ""), text(b))
	}
	a := doc.Find(`a[href="/doc/po/1/print"]`)
	if a.AttrOr("target", "") != "_blank" || a.AttrOr("rel", "") != "noopener" || a.HasClass("btn-error") || !a.HasClass("btn--ghost") {
		t.Errorf("print item target %q rel %q class %q", a.AttrOr("target", ""), a.AttrOr("rel", ""), a.AttrOr("class", ""))
	}
	if got := a.AttrOr("aria-label", ""); got != "Print (opens in a new tab)" {
		t.Errorf("print item aria-label = %q", got)
	}
	doc = uitest.Render(t, components.MoreActions([]components.MoreAction{
		{Label: "Delete draft", Href: "/doc/po/1/delete", Danger: true},
	}))
	if l := doc.Find(`a[href="/doc/po/1/delete"]`); !l.HasClass("btn--danger") || !l.HasClass("tap") {
		t.Errorf("danger link class %q", l.AttrOr("class", ""))
	}

	doc = uitest.Render(t, components.MoreActions(nil))
	if h, _ := doc.Find("body").Html(); strings.TrimSpace(h) != "" {
		t.Errorf("empty MoreActions rendered %q", h)
	}
}

func TestToast_OnlySuccessAndInfo(t *testing.T) {
	for _, level := range []string{"error", "warning", "danger", "loud"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("Toast(%q) did not panic", level)
				}
			}()
			_ = components.Toast(level, "No.").Render(context.Background(), &strings.Builder{})
		}()
	}
	for _, level := range []string{"success", "info"} {
		doc := uitest.Render(t, components.Toast(level, "Saved."))
		n := doc.Find(`#toasts[hx-swap-oob="beforeend"] > .alert.banner.banner--` + level)
		if n.Length() != 1 {
			t.Fatalf("%s: no banner toast in #toasts", level)
		}
		if _, ok := n.Attr("role"); ok {
			t.Errorf("%s: toast has its own role", level)
		}
		if !strings.Contains(text(n), "Saved.") || n.Find("strong").First().Text() == "" {
			t.Errorf("%s: toast text %q", level, text(n))
		}
		b := n.Find(`button[type="button"][data-dismiss][aria-label="Dismiss"]`)
		if b.Length() != 1 {
			t.Errorf("%s: toast has no dismiss button", level)
		}
	}
}

func TestBanner_ErrorIsDangerUnknownPanics(t *testing.T) {
	b := uitest.Render(t, components.Banner(components.BannerProps{Kind: "error", Message: "Failed."})).Find(".alert")
	if !b.HasClass("banner--danger") || b.AttrOr("role", "") != "alert" {
		t.Errorf("error banner class %q role %q", b.AttrOr("class", ""), b.AttrOr("role", ""))
	}
	n := uitest.Render(t, components.Banner(components.BannerProps{Kind: components.Warning, Message: "Check.", NoRole: true})).Find(".alert")
	if _, ok := n.Attr("role"); ok || !n.HasClass("banner--warning") {
		t.Errorf("NoRole banner role %q class %q", n.AttrOr("role", ""), n.AttrOr("class", ""))
	}
	defer func() {
		if recover() == nil {
			t.Error("unknown banner kind did not panic")
		}
	}()
	_ = components.Banner(components.BannerProps{Kind: "loud", Message: "?"}).Render(context.Background(), &strings.Builder{})
}

func TestMoreActions_PostBusyLabelIsItsLabel(t *testing.T) {
	ctx := ui.WithShell(context.Background(), ui.Shell{CSRF: "tok-1"})
	doc := uitest.RenderCtx(t, ctx, components.MoreActions([]components.MoreAction{
		{Label: "Cancel order", Href: "/doc/po/1/cancel", Post: true, Danger: true},
	}))
	if got := doc.Find("button").AttrOr("data-busy-label", ""); got != "Cancel order…" {
		t.Errorf("data-busy-label = %q", got)
	}
}
