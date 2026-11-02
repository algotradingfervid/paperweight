package components_test

import (
	"bytes"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

const (
	e7bAttached = "Attached to this expense"
	e7bWide     = "segmented__wide"
)

// e7bLabel is a Tiles or Segmented label of n characters; Devanagari, so a
// count of bytes instead of characters gives the wrong side of the threshold.
func e7bLabel(n int) string { return strings.Repeat("क", n) }

// e7bWords is a Tiles label of n characters, 8 to 14, of two words short
// enough that only the whole label's length can make it wide.
func e7bWords(n int) string { return e7bLabel(6) + " " + e7bLabel(n-7) }

// e7bWideLabels are the labels of opts' tiles with the full-row class.
func e7bWideLabels(doc *goquery.Document) []string {
	var out []string
	doc.Find("." + e7bWide).Each(func(_ int, s *goquery.Selection) {
		out = append(out, strings.TrimSpace(s.Text()))
	})
	return out
}

// CD-379, CD-353: a Tiles option of 12 characters shares its row, one of 13
// takes the full row, and the seeded and existing labels fall where the
// threshold says.
func TestTiles_LongOptionTakesFullRow(t *testing.T) {
	for _, c := range []struct {
		name, label string
		wide        bool
	}{
		{"at the threshold", e7bWords(12), false},
		{"one past it", e7bWords(13), true},
		{"GRN's longest challan kind", "Delivery challan", true},
		{"the seeded expense type", "Site miscellaneous", true},
		{"a short word", "Fuel", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := []components.Option{{Value: "a", Label: "Fuel"}, {Value: "b", Label: c.label}}
			doc := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type"}, opts))
			got := e7bWideLabels(doc)
			if c.wide && (len(got) != 1 || got[0] != c.label) {
				t.Errorf("full-row labels = %q, want only %q", got, c.label)
			}
			if !c.wide && len(got) != 0 {
				t.Errorf("full-row labels = %q, want none", got)
			}
		})
	}
}

// A word over 7 characters takes the full row whatever the label's length:
// tileMaxWord's words split in a chosen half tile at 320 px. Words end at
// spaces only, so a hyphen doesn't shorten one.
func TestTiles_LongWordTakesFullRow(t *testing.T) {
	for _, c := range []struct {
		name, label string
		wide        bool
	}{
		{"one word at the limit", e7bLabel(7), false},
		{"one word past it", e7bLabel(8), true},
		{"the longest word first", e7bLabel(8) + " क", true},
		{"the longest word last", "क " + e7bLabel(8), true},
		{"short words under the threshold", "Tax invoice", false},
		{"short words over the threshold", "Tea and snacks", true},
		{"one word under the threshold", "Refreshments", true},
		{"a hyphen inside the word", "Tea-Coffee", true},
		{"seeded type", "Food", false},
		{"seeded type", "Fuel", false},
		{"seeded type", "Lodging", false},
		{"seeded type", "Travel", false},
	} {
		t.Run(c.name+" "+c.label, func(t *testing.T) {
			opts := []components.Option{{Value: "a", Label: "Fuel"}, {Value: "b", Label: c.label}}
			doc := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type"}, opts))
			got := e7bWideLabels(doc)
			if c.wide && (len(got) != 1 || got[0] != c.label) {
				t.Errorf("full-row labels = %q, want only %q", got, c.label)
			}
			if !c.wide && len(got) != 0 {
				t.Errorf("full-row labels = %q, want none", got)
			}
		})
	}
}

// A short tile's label keeps its bare markup, and Segmented never takes the
// full row, whatever the length: it has no second column to span.
func TestTiles_FullRowOnlyOnTiles(t *testing.T) {
	opts := []components.Option{{Value: "a", Label: e7bLabel(40)}, {Value: "b", Label: "Fuel"}}
	doc := uitest.Render(t, components.Segmented(components.FieldProps{ID: "f-t", Name: "t", Label: "Type"}, opts))
	if got := e7bWideLabels(doc); len(got) != 0 {
		t.Errorf("Segmented has full-row labels %q", got)
	}
	tiles := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type"}, opts))
	if _, ok := tiles.Find(`label:has(input[value="b"])`).Attr("class"); ok {
		t.Error("a short tile's label gained a class attribute")
	}
}

// e7bCSS is the rule block of name in a component CSS file.
func e7bCSS(t *testing.T, file, name string) string {
	t.Helper()
	css, err := os.ReadFile("../css/components/" + file) //nolint:gosec // G304: this test's own fixed names
	if err != nil {
		t.Fatal(err)
	}
	return m21CSSBlock(string(css), name)
}

// The full-row class spans both columns. The grid flows in plain row order,
// never dense: Tiles draws the wide options last, so the visual order and the
// keyboard's are the same.
func TestTiles_FullRowCSS(t *testing.T) {
	if b := e7bCSS(t, "choice.css", e7bWide); !regexp.MustCompile(`grid-column:\s*1\s*/\s*-1`).MatchString(b) {
		t.Errorf("%s = %q, want a span of every column", e7bWide, b)
	}
	if b := e7bCSS(t, "choice.css", "segmented--tiles"); !regexp.MustCompile(`grid-auto-flow:\s*row;`).MatchString(b) {
		t.Errorf("segmented--tiles = %q, want a plain row flow (segmented's own flow is column)", b)
	}
}

// e7bOption is an option whose value is its label.
func e7bOption(label string) components.Option {
	return components.Option{Value: label, Label: label}
}

// e7bDrawn are the values of the radios Tiles draws, in DOM order.
func e7bDrawn(doc *goquery.Document) []string {
	var out []string
	doc.Find(".segmented input[type=radio]").Each(func(_ int, s *goquery.Selection) {
		out = append(out, s.AttrOr("value", ""))
	})
	return out
}

// Tiles draws the options up to the threshold, then the wide ones, each group
// in the order given, and leaves the caller's list as it was. Forty options,
// more than the sort library's small-slice path, so an unstable sort shows.
func TestTiles_WideOptionsDrawnLast(t *testing.T) {
	short1, short2, short3 := "Food", "Fuel", e7bWords(12)
	wide1, wide2 := e7bLabel(13), "Site miscellaneous"
	var manyIn, manyShort, manyWide []string
	for i := range 40 {
		l := strconv.Itoa(i) + "-" + e7bLabel(i%2*20)
		manyIn = append(manyIn, l)
		if i%2 == 0 {
			manyShort = append(manyShort, l)
		} else {
			manyWide = append(manyWide, l)
		}
	}
	for _, c := range []struct {
		name     string
		in, want []string
	}{
		{"seeded order", []string{"Food", "Fuel", "Lodging", "Site miscellaneous", "Travel"}, []string{"Food", "Fuel", "Lodging", "Travel", "Site miscellaneous"}},
		{"wide first", []string{wide1, short1, short2}, []string{short1, short2, wide1}},
		{"two wide keep their order", []string{wide2, short1, wide1, short2}, []string{short1, short2, wide2, wide1}},
		{"all short", []string{short3, short1, short2}, []string{short3, short1, short2}},
		{"all wide", []string{wide2, wide1}, []string{wide2, wide1}},
		{"GRN's two kinds", []string{"Delivery challan", "Tax invoice"}, []string{"Tax invoice", "Delivery challan"}},
		{"forty", manyIn, slices.Concat(manyShort, manyWide)},
	} {
		t.Run(c.name, func(t *testing.T) {
			opts := make([]components.Option, len(c.in))
			for i, l := range c.in {
				opts[i] = e7bOption(l)
			}
			before := slices.Clone(opts)
			doc := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type"}, opts))
			if got := e7bDrawn(doc); !slices.Equal(got, c.want) {
				t.Errorf("drawn order = %q, want %q", got, c.want)
			}
			if !slices.EqualFunc(opts, before, func(a, b components.Option) bool { return a.Value == b.Value }) {
				t.Errorf("Tiles changed the caller's list: %+v, was %+v", opts, before)
			}
		})
	}
}

// An error link goes to the field's id, which must be the first drawn option
// even when the list given starts with a wide one; the others stay unique and
// the chosen value stays checked wherever it moved to.
func TestTiles_ErrorLinkReachesFirstDrawn(t *testing.T) {
	opts := []components.Option{e7bOption("Site miscellaneous"), e7bOption("Food"), e7bOption("Fuel")}
	doc := uitest.Render(t, components.Tiles(components.FieldProps{ID: "f-t", Name: "t", Label: "Type", Value: "Site miscellaneous"}, opts))
	inputs := doc.Find(".segmented input[type=radio]")
	if got := inputs.First().AttrOr("id", ""); got != "f-t" {
		t.Errorf("first drawn option's id = %q, want the field's id f-t", got)
	}
	if got := doc.Find("#f-t").AttrOr("value", ""); got != "Food" {
		t.Errorf("#f-t is %q, want the first drawn option Food", got)
	}
	var ids []string
	inputs.Each(func(_ int, s *goquery.Selection) { ids = append(ids, s.AttrOr("id", "")) })
	if want := []string{"f-t", "f-t-1", "f-t-2"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %q, want %q", ids, want)
	}
	if got := doc.Find("input:checked").AttrOr("value", ""); got != "Site miscellaneous" {
		t.Errorf("checked = %q", got)
	}
}

func e7bUpload(named bool) components.UploadProps {
	return components.UploadProps{ID: "receipt", Label: "Receipt", Hint: "Photo or PDF.", Action: "/receipt", CardNamed: named}
}

// UI v2 §5.9: with CardNamed the root gains one class and nothing else
// changes, so the legend, the hint and aria-describedby all stay in place
// for a screen reader.
func TestFileUpload_CardNamed(t *testing.T) {
	plainHTML, plain := m22Render(t, e7bUpload(false))
	namedHTML, named := m22Render(t, e7bUpload(true))

	if got := plain.Find(".upload").AttrOr("class", ""); got != "upload" {
		t.Errorf("without CardNamed the root's class = %q", got)
	}
	if !named.Find(".upload").HasClass("upload--card-named") {
		t.Error("CardNamed left out upload--card-named")
	}
	want := strings.Replace(plainHTML, `class="upload"`, `class="upload upload--card-named"`, 1)
	if namedHTML != want {
		t.Errorf("CardNamed changed more than the class:\n got %s\nwant %s", namedHTML, want)
	}
	if got := strings.TrimSpace(named.Find("fieldset > legend").Text()); got != "Receipt" {
		t.Errorf("legend = %q", got)
	}
	if got := strings.TrimSpace(named.Find("#receipt-hint").Text()); got != "Photo or PDF." {
		t.Errorf("hint = %q", got)
	}
	named.Find("input[type=file]").Each(func(_ int, s *goquery.Selection) {
		if got := s.AttrOr("aria-describedby", ""); got != "receipt-hint receipt-msg" {
			t.Errorf("aria-describedby = %q", got)
		}
	})
	b := e7bCSS(t, "upload.css", "upload--card-named")
	for _, part := range []string{".field__label", ".field__hint", "@apply visually-hidden"} {
		if !strings.Contains(b, part) {
			t.Errorf("upload--card-named = %q, lacks %s", b, part)
		}
	}
}

func e7bTiles() []components.Attachment {
	return []components.Attachment{
		{ID: 1, Kind: "image", Name: "receipt.jpg", Size: "2.3 MB", UploadedAt: "24-10-2026 09:14", URL: "/files/1", RemoveURL: "/files/1/remove", Attached: e7bAttached},
		{ID: 2, Kind: "pdf", Name: "bill.pdf", Size: "312 KB", UploadedAt: "24-10-2026 09:15", URL: "/files/2", Attached: e7bAttached},
		{ID: 3, Kind: "heic", Name: "IMG_1.heic", Size: "1.9 MB", UploadedAt: "24-10-2026 09:16", URL: "/files/3", Attached: e7bAttached},
	}
}

// UI v2 §5.9: an attached-file tile is Attachments' own item: its thumbnail
// or chip link, the name, "size · uploaded time", the caller's words with a
// check icon, and Remove with its confirm. It names no uploader.
func TestAttachments_AttachedTile(t *testing.T) {
	doc := uitest.Render(t, components.Attachments(e7bTiles()))
	items := doc.Find("ul.attachments > li.attachments-tile")
	if items.Length() != 3 {
		t.Fatalf("%d tiles, want 3", items.Length())
	}
	for i, want := range []struct{ link, name, meta string }{
		{`a.attachments__thumb[href="/files/1"] > img[alt="receipt.jpg"]`, "receipt.jpg", "2.3 MB · uploaded 24-10-2026 09:14"},
		{`a.attachments__chip[href="/files/2"] > svg`, "bill.pdf", "312 KB · uploaded 24-10-2026 09:15"},
		{`a.attachments__chip[href="/files/3"][download] > svg`, "IMG_1.heic", "1.9 MB · uploaded 24-10-2026 09:16 · opens as download"},
	} {
		li := items.Eq(i)
		if li.Find(want.link).Length() != 1 {
			t.Errorf("tile %d lacks its link %s", i, want.link)
		}
		if got := m04Norm(li.Find(".attachments__name").Text()); got != want.name {
			t.Errorf("tile %d name = %q, want %q", i, got, want.name)
		}
		if got := m04Norm(li.Find(".attachments__meta").Text()); got != want.meta {
			t.Errorf("tile %d meta = %q, want %q", i, got, want.meta)
		}
		ok := li.Find(".attachments__attached")
		if got := m04Norm(ok.Text()); got != e7bAttached {
			t.Errorf("tile %d attached line = %q", i, got)
		}
		if ok.Find("svg").Length() != 1 {
			t.Errorf("tile %d attached line has no icon", i)
		}
	}
	first := items.Eq(0)
	if first.Find(`button[data-open-dialog="remove-file-1"]`).Length() != 1 || first.Find(`dialog#remove-file-1 form[hx-post="/files/1/remove"]`).Length() != 1 {
		t.Error("the tile lost Remove or its confirm")
	}
	if n := items.Eq(1).Find("[data-open-dialog]").Length(); n != 0 {
		t.Errorf("a tile with no RemoveURL has %d Remove buttons", n)
	}
}

// Done is on the status line as data-done only when set, so every other
// caller's markup stays as it was; the bar always has its accessible name.
func TestFileUpload_DoneAndBarName(t *testing.T) {
	plainHTML, plain := m22Render(t, e7bUpload(false))
	if _, ok := plain.Find("#receipt-status").Attr("data-done"); ok {
		t.Error("without Done the status line has data-done")
	}
	if strings.Contains(plainHTML, "data-done") {
		t.Errorf("data-done in %s", plainHTML)
	}
	p := e7bUpload(false)
	p.Done = "Receipt attached."
	doneHTML, done := m22Render(t, p)
	if got := done.Find("#receipt-status").AttrOr("data-done", ""); got != p.Done {
		t.Errorf("data-done = %q, want %q", got, p.Done)
	}
	if want := strings.Replace(plainHTML, `<p id="receipt-status" role="status"`, `<p id="receipt-status" role="status" data-done="Receipt attached."`, 1); doneHTML != want {
		t.Errorf("Done changed more than the attribute:\n got %s\nwant %s", doneHTML, want)
	}
	if got := plain.Find("progress.upload__progress").AttrOr("aria-label", ""); got != "Upload progress" {
		t.Errorf("the bar's aria-label = %q", got)
	}
}

// An item without Attached keeps its bare <li>, and Attached wins over
// Compact, with one class attribute.
func TestAttachments_ItemClass(t *testing.T) {
	both := e7bTiles()[0]
	both.Compact = true
	plain := components.Attachment{ID: 9, Kind: "image", Name: "a.jpg", URL: "/files/9"}
	var buf bytes.Buffer
	if err := components.Attachments([]components.Attachment{plain, both}).Render(t.Context(), &buf); err != nil {
		t.Fatal(err)
	}
	html := buf.String()
	if !strings.Contains(html, `<ul class="attachments"><li><a `) {
		t.Errorf("a plain item lost its bare <li>: %s", html)
	}
	if !strings.Contains(html, `<li class="attachments-tile"><a `) {
		t.Errorf("an item with Attached and Compact isn't only a tile: %s", html)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	text := doc.Find("li.attachments-tile .attachments__tile-text")
	if text.Length() != 1 {
		t.Errorf("%d tile texts, want 1: %s", text.Length(), html)
	}
	if got := strings.TrimSpace(text.Find(".attachments__attached").Text()); got != e7bAttached {
		t.Errorf("the tile's words = %q, want %q", got, e7bAttached)
	}
	if got, want := text.Find(".attachments__meta").Text(), "2.3 MB · uploaded 24-10-2026 09:14"; got != want {
		t.Errorf("the tile's meta = %q, want %q", got, want)
	}
	if strings.Contains(html, both.Name+" · "+both.Size) {
		t.Errorf("Attached and Compact drew the compact line: %s", html)
	}
}

// Inside FileUpload the tile's Remove confirm swaps the upload's target.
func TestFileUpload_AttachedTileRemoveTarget(t *testing.T) {
	p := e7bUpload(false)
	p.Files = e7bTiles()[:1]
	_, doc := m22Render(t, p)
	if got := doc.Find("li.attachments-tile dialog form").AttrOr("hx-target", ""); got != "#receipt" {
		t.Errorf("Remove's hx-target = %q, want #receipt", got)
	}
}

// §5.9: the tile's thumbnail is 64 × 80, and a chip fills the same box.
func TestAttachments_TileCSS(t *testing.T) {
	b := e7bCSS(t, "upload.css", "attachments-tile")
	for _, part := range []string{"4rem minmax(0, 1fr)", "block-size: 5rem"} {
		if !strings.Contains(b, part) {
			t.Errorf("attachments-tile = %q, lacks %q", b, part)
		}
	}
}

// §5.9: the busy labels look disabled, the bar is 6 px and the words wrap.
func TestFileUpload_BusyCSS(t *testing.T) {
	b := e7bCSS(t, "upload.css", "upload")
	for _, part := range []string{".upload__button:has(input:disabled)", "--color-text-disabled", "block-size: 0.375rem", "overflow-wrap: anywhere", "& > .upload__progress { order: 3; }"} {
		if !strings.Contains(b, part) {
			t.Errorf("upload = %q, lacks %q", b, part)
		}
	}
}
