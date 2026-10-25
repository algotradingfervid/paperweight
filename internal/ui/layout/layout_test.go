package layout_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

func shell() ui.Shell {
	return ui.Shell{
		AppName: "Internal tools", User: "Priya Shah", CSRF: `tok"x`,
		Nav: []ui.NavItem{
			{Label: "Home", Href: "/"},
			{Label: "Purchase orders", Href: "/doc/fixture.Order"},
			{Label: "Order lines", Href: "/doc/fixture.OrderLine"},
			{Label: "Goods receipts", Href: "/doc/fixture.Receipt"},
		},
		InboxCount: 3, Path: "/doc/fixture.Order/42", SignedIn: true,
	}
}

func text(s string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, `<p id="body-text">`+s+`</p>`)
		return err
	})
}

func render(t *testing.T, s ui.Shell, o layout.PageOpts) *goquery.Document {
	t.Helper()
	ctx := templ.WithChildren(ui.WithShell(context.Background(), s), text("Hello"))
	return uitest.RenderCtx(t, ctx, layout.Page(o))
}

func TestLayout_Head(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "Purchase orders"})
	if got, _ := doc.Find(`meta[name="viewport"]`).Attr("content"); got != "width=device-width, initial-scale=1, viewport-fit=cover, interactive-widget=resizes-content" {
		t.Errorf("viewport = %q", got)
	}
	if got, _ := doc.Find(`meta[name="color-scheme"]`).Attr("content"); got != "light" {
		t.Errorf("color-scheme = %q", got)
	}
	if doc.Find(`head meta[charset="utf-8"]`).Length() != 1 {
		t.Error("no meta charset utf-8")
	}
	css := doc.Find(`link[rel="stylesheet"]`)
	if css.Length() != 1 {
		t.Fatalf("%d stylesheets, want 1", css.Length())
	}
	if got, _ := css.Attr("href"); got != ui.CSSURL {
		t.Errorf("stylesheet href = %q, want %q", got, ui.CSSURL)
	}
	if got, _ := doc.Find(`link[rel="preload"][as="font"]`).Attr("href"); got != ui.InterURL {
		t.Errorf("font preload = %q, want %q", got, ui.InterURL)
	}
	var srcs []string
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		if _, ok := s.Attr("defer"); !ok {
			t.Errorf("script %v is not deferred", s.AttrOr("src", ""))
		}
		srcs = append(srcs, s.AttrOr("src", ""))
	})
	if want := []string{web.HTMXURL, ui.JSURL, web.AlpineURL}; !reflect.DeepEqual(srcs, want) {
		t.Errorf("scripts = %v, want %v", srcs, want)
	}
	if doc.Find("style").Length() != 0 {
		t.Error("page has a <style> element")
	}
	if doc.Find("[data-theme]").Length() != 0 {
		t.Error("page sets data-theme (CD-112)")
	}
}

func TestLayout_FlashShownOnce(t *testing.T) {
	sm := scs.New()
	r := web.NewRouter(web.Options{Sessions: sm})
	var page, main *goquery.Document
	r.Get("/", func(_ http.ResponseWriter, r *http.Request) {
		ctx := templ.WithChildren(ui.WithShell(r.Context(), shell()), text("Hello"))
		web.Flash(ctx, "success", "Approved. PO/26-27/00001 has been numbered.")
		web.Flash(ctx, "warning", "Check the rate on line 2.")
		var buf bytes.Buffer
		if err := layout.Main(layout.PageOpts{Title: "PO"}).Render(ctx, &buf); err != nil {
			t.Error(err)
		}
		main, _ = goquery.NewDocumentFromReader(&buf)
		page = uitest.RenderCtx(t, ctx, layout.Page(layout.PageOpts{Title: "PO"}))
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if page == nil || main == nil {
		t.Fatal("handler did not run")
	}
	if strings.Contains(main.Text(), "Approved") || strings.Contains(main.Text(), "Check the rate") {
		t.Error("Main fragment shows the flash")
	}
	banner := page.Find("main .alert-success")
	if !strings.Contains(banner.Text(), "Approved. PO/26-27/00001 has been numbered.") {
		t.Fatalf("no success banner with the flash; main = %q", page.Find("main").Text())
	}
	if page.Find("main h1").Length() != 1 {
		t.Fatal("no h1")
	}
	// The banner comes before the h1 in document order.
	html, _ := page.Find("main").Html()
	if strings.Index(html, "Approved.") > strings.Index(html, "<h1") {
		t.Error("success banner is not above the h1")
	}
	if !strings.Contains(page.Find("#alerts").Text(), "Check the rate on line 2.") {
		t.Error("warning flash is not in #alerts")
	}
	if strings.Contains(page.Find("#alerts").Text(), "Approved") {
		t.Error("success flash is in #alerts")
	}
	if n := strings.Count(page.Text(), "Approved. PO/26-27/00001"); n != 1 {
		t.Errorf("success flash shown %d times", n)
	}
}

func TestShell_SignOutPosts(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	f := doc.Find(`form[method="post"][action="/signout"]`)
	if f.Length() != 1 {
		t.Fatalf("%d sign-out forms", f.Length())
	}
	if got, _ := f.Find(`input[type="hidden"][name="csrf_token"]`).Attr("value"); got != `tok"x` {
		t.Errorf("csrf_token = %q", got)
	}
	if !strings.Contains(f.Find("button").Text(), "Sign out") {
		t.Error("no Sign out button")
	}
	if doc.Find(`a[href="/signout"]`).Length() != 0 {
		t.Error("sign-out is a link")
	}
}

func TestLayout_HTMXConfig(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	content, ok := doc.Find(`meta[name="htmx-config"]`).Attr("content")
	if !ok {
		t.Fatal("no htmx-config meta")
	}
	const want = `{"allowEval":false,"allowScriptTags":false,"includeIndicatorStyles":false,"selfRequestsOnly":true,"historyCacheSize":0,"refreshOnHistoryMiss":true,"defaultFocusScroll":true,"responseHandling":[{"code":"204","swap":false},{"code":"[23]..","swap":true},{"code":"422","swap":true,"error":false},{"code":"409","swap":true,"error":false},{"code":"[45]..","swap":false,"error":true}]}`
	var got, w map[string]any
	if err := json.Unmarshal([]byte(content), &got); err != nil {
		t.Fatalf("htmx-config is not JSON: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, w) {
		t.Errorf("htmx-config = %s", content)
	}
}

func TestLayout_CSRFHeaderIsJSON(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	var got map[string]string
	if err := json.Unmarshal([]byte(doc.Find("body").AttrOr("hx-headers", "")), &got); err != nil {
		t.Fatalf("hx-headers is not JSON: %v", err)
	}
	if want := map[string]string{"X-CSRF-Token": `tok"x`}; !reflect.DeepEqual(got, want) {
		t.Errorf("hx-headers = %v", got)
	}
}

func TestLayout_BrandAssets(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	if got, _ := doc.Find(`head link[rel="icon"]`).Attr("href"); got != brand.FaviconURL {
		t.Errorf("favicon = %q, want %q", got, brand.FaviconURL)
	}
	img := doc.Find("header img.brand-logo")
	if img.Length() != 1 {
		t.Fatalf("%d header logos", img.Length())
	}
	for attr, want := range map[string]string{
		"src": brand.AppLogoURL, "width": strconv.Itoa(brand.AppLogoWidth),
		"height": strconv.Itoa(brand.AppLogoHeight), "alt": "",
	} {
		if got, ok := img.Attr(attr); !ok || got != want {
			t.Errorf("logo %s = %q, want %q", attr, got, want)
		}
	}
	if !strings.Contains(img.Parent().Text(), "Internal tools") {
		t.Error("app name text is not next to the logo")
	}
}

func TestLayout_DefaultAppName(t *testing.T) {
	doc := uitest.RenderCtx(t, templ.WithChildren(context.Background(), text("x")), layout.Page(layout.PageOpts{Title: "Sign in", Bare: true}))
	if got, want := doc.Find("title").Text(), "Sign in · "+brand.Load().AppName; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestLayout_ErrorTitle(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "New purchase order", Error: true})
	if got := doc.Find("title").Text(); got != "Error: New purchase order · Internal tools" {
		t.Errorf("title = %q", got)
	}
}

func TestShell_InboxBadge(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	a := doc.Find(`a[aria-label="Waiting for my approval: 3"]`)
	if a.Length() != 1 {
		t.Fatalf("%d inbox links with count 3", a.Length())
	}
	if got := strings.TrimSpace(a.Find(".badge").Text()); got != "3" {
		t.Errorf("badge = %q", got)
	}
	s := shell()
	s.InboxCount = 0
	doc = render(t, s, layout.PageOpts{Title: "PO"})
	a = doc.Find(`a[aria-label="Waiting for my approval: 0"]`)
	if a.Length() != 1 {
		t.Fatalf("%d inbox links with count 0", a.Length())
	}
	if a.Find(".badge").Length() != 0 {
		t.Error("badge shown for count 0")
	}
}

func TestShell_NavCurrent(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "PO"})
	cur := doc.Find(`nav a[aria-current="page"]`)
	if cur.Length() == 0 {
		t.Fatal("no current nav item")
	}
	cur.Each(func(_ int, s *goquery.Selection) {
		if h := s.AttrOr("href", ""); h != "/doc/fixture.Order" {
			t.Errorf("aria-current on %q", h)
		}
	})
	if doc.Find(`nav a[href="/doc/fixture.OrderLine"]`).Length() == 0 {
		t.Error("nav item missing")
	}
}

func TestLayout_BareHasNoNav(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{Title: "Sign in", Bare: true})
	if doc.Find("nav").Length() != 0 {
		t.Error("bare page has a nav")
	}
	if doc.Find(`button[aria-label="Menu"]`).Length() != 0 {
		t.Error("bare page has a menu button")
	}
	if doc.Find(`a[aria-label^="Waiting for my approval"]`).Length() != 0 {
		t.Error("bare page has an inbox link")
	}
	if doc.Find("main#main #body-text").Length() != 1 {
		t.Error("bare page has no body")
	}
}

func TestLayout_SimpleWrapsBody(t *testing.T) {
	ctx := ui.WithShell(context.Background(), shell())
	doc := uitest.RenderCtx(t, ctx, layout.Simple("Waiting for my approval", text("Body")))
	if doc.Find("main#main #body-text").Length() != 1 {
		t.Error("body is not inside main#main")
	}
	if got := strings.TrimSpace(doc.Find("main h1").Text()); got != "Waiting for my approval" {
		t.Errorf("h1 = %q", got)
	}
	if got, _ := doc.Find("main h1").Attr("id"); got != "page-title" {
		t.Errorf("h1 id = %q", got)
	}
	if got, _ := doc.Find("main h1").Attr("tabindex"); got != "-1" {
		t.Errorf("h1 tabindex = %q", got)
	}
	if doc.Find(`a.skip-link[href="#main"]`).Length() != 1 {
		t.Error("no skip link to #main")
	}
}

func TestLayout_MainFragmentOnly(t *testing.T) {
	ctx := templ.WithChildren(ui.WithShell(context.Background(), shell()), text("Hello"))
	var buf bytes.Buffer
	if err := layout.Main(layout.PageOpts{Title: "PO", HeadingID: "po-title"}).Render(ctx, &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "<main") {
		t.Errorf("Main starts with %.40q", out)
	}
	if strings.Contains(out, "<html") || strings.Contains(out, "<header") {
		t.Error("Main renders the shell")
	}
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(out))
	if doc.Find("main#main #body-text").Length() != 1 || doc.Find("h1#po-title").Length() != 1 {
		t.Error("Main has no body or heading id")
	}
}

func TestLayout_SlotsAndOptions(t *testing.T) {
	doc := render(t, shell(), layout.PageOpts{
		Title: "PO", Width: layout.Form, NoHeading: true,
		BannerSlot: text("banner"), BarSlot: text("bar"), Side: text("side"),
	})
	if doc.Find("main h1").Length() != 0 {
		t.Error("NoHeading still renders an h1")
	}
	if doc.Find("main .form-width").Length() != 1 {
		t.Error("Width form does not use .form-width")
	}
	if doc.Find("aside").Length() != 1 || !strings.Contains(doc.Find("aside").Text(), "side") {
		t.Error("no side panel")
	}
	html, _ := doc.Find("main").Html()
	if b, h, bar := strings.Index(html, "banner"), strings.Index(html, "Hello"), strings.Index(html, "bar</p>"); b > h || h > bar {
		t.Error("banner, body and bar slots are out of order")
	}
}
