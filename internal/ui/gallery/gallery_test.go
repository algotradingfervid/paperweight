package gallery

import (
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/screens"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// galleryPages are the gallery's own pages (plan Task 15), each served at
// /_gallery/<page>.
var galleryPages = []string{
	"buttons", "fields", "badges-banners", "dialogs", "datalist", "lines",
	"picker", "upload", "wizard-1", "wizard-2", "wizard-3", "wizard-4",
	"actionbar", "warnings", "filterbar", "pagination", "bare",
}

// pageChecks pin the worst-case content each page must carry (Review
// Focus 1): the overflow sweep only proves something if the values are there.
var pageChecks = map[string]func(t *testing.T, d *goquery.Document){
	"fields": func(t *testing.T, d *goquery.Document) {
		if !strings.Contains(d.Text(), uitest.LongError) {
			t.Error("fields: no field shows uitest.LongError (the 422 state)")
		}
		if d.Find(`[aria-invalid="true"]`).Length() == 0 {
			t.Error(`fields: no input has aria-invalid="true" (the 422 state)`)
		}
		if d.Find(`input[hx-post="/ui/validate/money"]`).Not(`[aria-invalid]`).Length() == 0 {
			t.Error("fields: no valid money input checked by /ui/validate/money")
		}
		if d.Find(`input.num[hx-post="/ui/validate/factor"][value="9,99,999.999999999"]`).Length() == 0 {
			t.Error("fields: no Factor input holding the worst-case value 9,99,999.999999999")
		}
	},
	"datalist": func(t *testing.T, d *goquery.Document) {
		text := d.Text()
		for name, v := range map[string]string{
			"LongName100": uitest.LongName100, "LongCode60": uitest.LongCode60,
			"MaxMoney": uitest.MaxMoney, "LongDevanagari": uitest.LongDevanagari,
		} {
			if !strings.Contains(text, v) {
				t.Errorf("datalist: uitest.%s not shown", name)
			}
		}
	},
	"lines": func(t *testing.T, d *goquery.Document) {
		if n := d.Find("ol.lines > li").Length(); n < 500 {
			t.Errorf("lines: %d line cards, want at least 500 (uitest.Lines(500))", n)
		}
		if d.Find("#line-editor form[id^='grid-row-']").Length() == 0 {
			t.Error("lines: no LineGrid row forms inside #line-editor")
		}
		if d.Find("#line-editor form[id^='grid-row-'] input[id$='-qty']").Length() == 0 {
			t.Error("lines: no grid qty input (id grid-<no>-qty)")
		}
		if d.Find("#doc-version").Length() != 1 {
			t.Error("lines: want exactly one #doc-version input (CD-95)")
		}
		if d.Find("[data-add-line]").Length() == 0 {
			t.Error("lines: no editable list with [data-add-line]")
		}
	},
	"picker": func(t *testing.T, d *goquery.Document) {
		if d.Find(`[role="combobox"][hx-get]`).Length() == 0 {
			t.Error("picker: no live combobox")
		}
	},
	"upload": func(t *testing.T, d *goquery.Document) {
		if d.Find("img[loading=lazy]").Length() == 0 {
			t.Error("upload: no image attachment")
		}
		if !strings.Contains(d.Text(), "HEIC image, opens as download") {
			t.Error("upload: no HEIC attachment")
		}
	},
	"warnings": func(t *testing.T, d *goquery.Document) {
		if n := strings.Count(d.Find("main#main").Text(), uitest.LongError); n != 3 {
			t.Errorf("warnings: uitest.LongError shown %d times, want 3", n)
		}
	},
	"filterbar": func(t *testing.T, d *goquery.Document) {
		if n := d.Find(".filterbar-chips a.chip").Length(); n != 6 {
			t.Errorf("filterbar: %d chips, want 6", n)
		}
	},
	"bare": func(t *testing.T, d *goquery.Document) {
		if !strings.Contains(d.Text(), uitest.LongEmail) {
			t.Error("bare: uitest.LongEmail not shown")
		}
		if d.Find("nav").Length() != 0 {
			t.Error("bare: has a nav; want the Bare shell")
		}
	},
	"dialogs": func(t *testing.T, d *goquery.Document) {
		if n := d.Find("dialog").Length(); n < 2 {
			t.Errorf("dialogs: %d dialogs, want a dialog and a sheet", n)
		}
		if d.Find("[data-open-dialog]").Length() < 2 {
			t.Error("dialogs: want an opener for the dialog and for the sheet")
		}
	},
}

func init() {
	for i := 1; i <= 4; i++ {
		want := "Step " + strconv.Itoa(i) + " of 4"
		pageChecks["wizard-"+strconv.Itoa(i)] = func(t *testing.T, d *goquery.Document) {
			if got := strings.TrimSpace(d.Find("#step-count").Text()); got != want {
				t.Errorf("wizard-%d: #step-count = %q, want %q", i, got, want)
			}
		}
	}
}

func get(t *testing.T, h http.Handler, url string) *goquery.Document {
	t.Helper()
	rec := uitest.Do(t, h, http.MethodGet, url, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("GET %s = %d, want 200", url, rec.Code)
		return nil
	}
	d := uitest.Doc(t, rec)
	if d.Find("main#main").Length() != 1 {
		t.Errorf("GET %s: no main#main", url)
	}
	return d
}

func TestGallery_EveryPageRenders(t *testing.T) {
	h := Handler()

	if idx := get(t, h, "/_gallery"); idx != nil {
		for _, p := range galleryPages {
			if idx.Find(`a[href="/_gallery/`+p+`"]`).Length() == 0 {
				t.Errorf("index: no link to /_gallery/%s", p)
			}
		}
	}
	for _, p := range galleryPages {
		d := get(t, h, "/_gallery/"+p)
		if d == nil {
			continue
		}
		if check := pageChecks[p]; check != nil {
			check(t, d)
		}
	}

	all := Screens("")
	for _, p := range galleryPages {
		found := false
		for _, s := range all {
			if s.Route == "/_gallery/{page}" && strings.SplitN(s.URL, "?", 2)[0] == "/_gallery/"+p {
				found = true
			}
		}
		if !found {
			t.Errorf("Screens has no entry for /_gallery/%s", p)
		}
	}
	for _, s := range all {
		if s.As != "" {
			t.Errorf("screen %q: As = %q, want \"\" (the gallery signs everyone in as Priya)", s.Name, s.As)
		}
		get(t, h, s.URL)
	}
}

func TestGallery_ScreensIncludeDocscreen(t *testing.T) {
	ids := galleryIDs
	if ids.Lines500 != 500 {
		t.Errorf("galleryIDs.Lines500 = %d, want 500", ids.Lines500)
	}
	seen := map[int64]string{}
	for name, id := range map[string]int64{
		"Draft": ids.Draft, "Pending": ids.Pending, "Approved": ids.Approved, "Rejected": ids.Rejected,
		"Cancelled": ids.Cancelled, "Closed": ids.Closed, "Lines500": ids.Lines500,
	} {
		if id == 0 {
			t.Errorf("galleryIDs.%s is 0; want a seeded document", name)
		} else if other, dup := seen[id]; dup {
			t.Errorf("galleryIDs.%s and .%s are both %d", name, other, id)
		}
		seen[id] = name
	}

	all := Screens("")
	want := docscreen.Screens(docscreentest.Sample, ids)
	if len(want) == 0 {
		t.Fatal("docscreen.Screens returned nothing")
	}
	for _, w := range want {
		var got *screens.Screen
		for i := range all {
			if all[i].Name == w.Name {
				got = &all[i]
			}
		}
		if got == nil {
			t.Errorf("Screens has no %q", w.Name)
			continue
		}
		if got.Route != w.Route || got.URL != w.URL {
			t.Errorf("%q = {Route %q, URL %q}, want {%q, %q}", w.Name, got.Route, got.URL, w.Route, w.URL)
		}
		if (got.Setup == nil) != (w.Setup == nil) {
			t.Errorf("%q: Setup kept = %v, want %v", w.Name, got.Setup != nil, w.Setup != nil)
		}
		if got.As != "" {
			t.Errorf("%q: As = %q, want \"\"", w.Name, got.As)
		}
	}
}

func TestGallery_RoutesCovered(t *testing.T) {
	r := chi.NewRouter()
	Routes()(r)
	screens.Register(Screens("")...)
	if missing := screens.Uncovered(r, screens.DefaultSkip); len(missing) != 0 {
		t.Errorf("GET routes with no screen: %q", missing)
	}
	var gets int
	_ = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodGet && (route == "/_gallery" || route == "/_gallery/{page}" || route == "/doc/{doctype}") {
			gets++
		}
		return nil
	})
	if gets != 3 {
		t.Errorf("Routes mounts %d of GET /_gallery, /_gallery/{page}, /doc/{doctype}; want all 3", gets)
	}
}

func TestGallery_NotInRelease(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", "github.com/vardhan/internaltools/cmd/app").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps ./cmd/app: %v\n%s", err, out)
	}
	deps := string(out)
	if !strings.Contains(deps, "github.com/vardhan/internaltools/cmd/app") {
		t.Fatalf("go list -deps output does not list cmd/app itself:\n%s", deps)
	}
	if strings.Contains(deps, "internal/ui/gallery") {
		t.Error("cmd/app (release build) imports internal/ui/gallery")
	}
	if strings.Contains(deps, "github.com/chromedp/") {
		t.Error("cmd/app (release build) depends on chromedp")
	}
}
