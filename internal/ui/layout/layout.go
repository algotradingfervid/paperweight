// Package layout is the app shell: the full page around every screen and the
// htmx main fragment (design-system §6).
package layout

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"

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
// the main content and Side in the side panel shown from 80rem.
type PageOpts struct {
	Title                    string
	Error                    bool
	Bare                     bool
	Width                    Width
	Back                     *components.Link
	BannerSlot, BarSlot, Side templ.Component
	HeadingID                string
	NoHeading                bool
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

func alertClass(level string) string {
	switch level {
	case "success":
		return "alert alert-success"
	case "info":
		return "alert alert-info"
	case "warning":
		return "alert alert-warning"
	}
	return "alert alert-error"
}
