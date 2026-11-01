// Package layout is the app shell: the full page around every screen and the
// htmx main fragment (design-system §6).
package layout

import (
	"context"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// Width is the content column's maximum width.
type Width string

// Widths.
const (
	Form    Width = "form"    // 40rem
	Content Width = "content" // 72rem, the default
)

// PageOpts describes one page. BannerSlot renders above the h1, BarSlot after
// the main content and Side in the side panel shown from 80rem. Focus is for
// wizard, line and document pages, which have their own action footer: they
// get no bottom nav, and below 64rem, when the wizard head has its close ×,
// no app header, so the wizard head is the top bar (UI v2 §5.6, N12).
type PageOpts struct {
	Title                     string
	Error                     bool
	Bare                      bool
	Focus                     bool
	Width                     Width
	Back                      *components.Link
	BannerSlot, BarSlot, Side templ.Component
	HeadingID                 string
	NoHeading                 bool
}

// htmxConfig is research §2.2 plus the 409 entry (Decision U5, CD-47).
const htmxConfig = `{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"selfRequestsOnly":true,"historyCacheSize":0,"refreshOnHistoryMiss":true,"defaultFocusScroll":true,"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"422","swap":true,"error":false},{"code":"409","swap":true,"error":false},{"code":"[45]..","swap":false,"error":true}]}`

const viewport = "width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content"

// Simple is Page(PageOpts{Title: title}) around body.
func Simple(title string, body templ.Component) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return Page(PageOpts{Title: title}).Render(templ.WithChildren(ctx, body), w)
	})
}

func pageTitle(o PageOpts, appName string) string {
	t := o.Title + " · " + appName
	if o.Error {
		t = "Error: " + t
	}
	return t
}

func csrfHeaders(token string) string {
	b, _ := json.Marshal(map[string]string{"X-CSRF-Token": token})
	return string(b)
}

func headingID(o PageOpts) string {
	if o.HeadingID != "" {
		return o.HeadingID
	}
	return "page-title"
}

func widthClass(o PageOpts) string {
	if o.Width == Form {
		return "form-width"
	}
	return "content"
}

// currentHref is the Href of the nav item that is the longest path prefix of
// path, or "" when none is.
func currentHref(items []ui.NavItem, path string) string {
	best := ""
	for _, it := range items {
		h := it.Href
		if (path == h || strings.HasPrefix(path, strings.TrimSuffix(h, "/")+"/")) && len(h) > len(best) {
			best = h
		}
	}
	return best
}

// inboxPath is the approvals inbox, linked from the header and the sidebar.
const inboxPath = "/approvals"

// navGroup is one sidebar group: its heading (blank for items with no
// group), its items and those under its More disclosure.
type navGroup struct {
	Name        string
	Items, More []ui.NavItem
}

// groupNav groups items by Group, in the order each group first appears.
func groupNav(items []ui.NavItem) []navGroup {
	var out []navGroup
	at := map[string]int{}
	for _, it := range items {
		i, ok := at[it.Group]
		if !ok {
			i = len(out)
			at[it.Group] = i
			out = append(out, navGroup{Name: it.Group})
		}
		if it.More {
			out[i].More = append(out[i].More, it)
		} else {
			out[i].Items = append(out[i].Items, it)
		}
	}
	return out
}

// groupID is the id of a group's heading; prefix keeps the sidebar's and the
// phone sheet's apart.
func groupID(prefix, group string) string {
	return prefix + "-" + strings.ReplaceAll(strings.ToLower(group), " ", "-")
}

// groupAttrs labels a group's links by its heading; a group with no name has
// no heading.
func groupAttrs(prefix, group string) templ.Attributes {
	if group == "" {
		return templ.Attributes{}
	}
	return templ.Attributes{"aria-labelledby": groupID(prefix, group)}
}

// moreOpen reports whether the current item is under the More disclosure.
func moreOpen(more []ui.NavItem, cur string) bool {
	return slices.ContainsFunc(more, func(it ui.NavItem) bool { return it.Href == cur })
}

// initials are the first letters of the first and last words of name.
func initials(name string) string {
	w := strings.Fields(name)
	if len(w) == 0 {
		return ""
	}
	first := func(s string) string { r, _ := utf8.DecodeRuneInString(s); return string(r) }
	out := first(w[0])
	if len(w) > 1 {
		out += first(w[len(w)-1])
	}
	return strings.ToUpper(out)
}

// Bottom nav paths (UI v2 §5.2, N1). Me goes to the sessions page without
// JavaScript; with it, Me opens the account sheet.
const (
	newPath     = "/new"
	searchPath  = "/search"
	accountPath = "/account/sessions"
	newSheetID  = "new-sheet"
	meSheetID   = "me-sheet"
)

// bottomNavShown reports whether the page has the bottom nav: signed-in app
// pages that aren't in focus mode.
func bottomNavShown(o PageOpts, s ui.Shell) bool {
	return s.SignedIn && !o.Bare && !o.Focus
}

// bodyAttrs gives <body> room for the bottom nav when the page has it, and
// marks focus mode, whose CSS hides the app header below 64rem on a page
// with a wizard close ×.
func bodyAttrs(o PageOpts, s ui.Shell) templ.Attributes {
	switch {
	case bottomNavShown(o, s):
		return templ.Attributes{"class": "with-bottomnav"}
	case o.Focus:
		return templ.Attributes{"class": "focus-mode"}
	}
	return templ.Attributes{}
}

// under reports whether path is p or below it.
func under(path, p string) bool {
	return path == p || strings.HasPrefix(path, p+"/")
}

// bottomCurrent is the bottom nav item path is on: "home", "approvals",
// "new", "search", "me" or "".
func bottomCurrent(path string) string {
	switch {
	case path == "/":
		return "home"
	case under(path, inboxPath):
		return "approvals"
	case under(path, newPath):
		return "new"
	case under(path, searchPath):
		return "search"
	case under(path, "/account"):
		return "me"
	}
	return ""
}

// current is aria-current for the bottom nav item named item.
func current(path, item string) templ.Attributes {
	if bottomCurrent(path) == item {
		return templ.Attributes{"aria-current": "page"}
	}
	return templ.Attributes{}
}

// approvalsCount ends the bottom nav's Approvals name: ", 3 waiting", or
// nothing with no count.
func approvalsCount(n int) string {
	if n == 0 {
		return ""
	}
	return ", " + strconv.Itoa(n) + " waiting"
}

func inboxLabel(n int) string {
	return "Waiting for my approval: " + strconv.Itoa(n)
}

// split returns the flash messages shown as banners (success, info) and
// those shown in #alerts (warning, error).
func split(notices []web.Notice) (banners, alerts []web.Notice) {
	for _, n := range notices {
		if n.Level == "success" || n.Level == "info" {
			banners = append(banners, n)
		} else {
			alerts = append(alerts, n)
		}
	}
	return banners, alerts
}

// flashKind is the banner kind for a flash level; "error" (and anything
// unknown) is Danger.
func flashKind(level string) components.BannerKind {
	switch k := components.BannerKind(level); k {
	case components.Success, components.Info, components.Warning:
		return k
	}
	return components.Danger
}
