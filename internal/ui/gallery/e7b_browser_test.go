//go:build e2e

package gallery

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// CD-379, UI v2 §5.9, §5.10: the long Tiles option, the upload inside a card
// that names it, the attached-file tile and the busy upload, in a browser.

// e7bOverflow fails when the page scrolls sideways.
func e7bOverflow(t *testing.T, ctx context.Context, what string) {
	t.Helper()
	got, err := uibrowser.Overflow(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("%s: sideways overflow: %q", what, got)
	}
}

// e7bTile is the label of the fields page's expense-type tile with value v.
func e7bTile(v string) string {
	return `document.querySelector(` + strconv.Quote(`input[name="f_type"][value="`+v+`"]`) + `).closest("label")`
}

// At 360 px the 100-character option spans the grid's two columns, its
// neighbours keep one line's height and nothing scrolls sideways.
func TestTilesBrowser_LongOptionFullRow(t *testing.T) {
	ctx, eval, _ := openFieldsB4(t, 360)
	long := e7bTile("other")
	full, err := strconv.ParseBool(eval("spans", `String(`+long+`.offsetWidth === `+long+`.parentElement.clientWidth - 8)`))
	if err != nil || !full {
		t.Errorf("the long tile is %s px wide in a track of %s", eval("width", `String(`+long+`.offsetWidth)`), eval("track", `String(`+long+`.parentElement.clientWidth)`))
	}
	if short := e7bTile("fuel"); eval("half", `String(`+short+`.offsetWidth * 2 < `+short+`.parentElement.clientWidth)`) != "true" {
		t.Error("a short tile spans the row")
	}
	for _, v := range []string{"fuel", "travel", "stay"} {
		if h, err := strconv.Atoi(eval("height", `String(`+e7bTile(v)+`.offsetHeight)`)); err != nil || h > 48 {
			t.Errorf("tile %s is %d px tall beside the long one, want one line (%v)", v, h, err)
		}
	}
	e7bOverflow(t, ctx, "long tile")
}

// §5.5: every mix of short and wide options, in every place, draws in reading
// order with the gap closed: each tile is on the same row to the right of the
// one before it in the DOM, or lower, so a keyboard user's next tile is the
// next one seen; and the rows are as few as the shorts in pairs, then one per
// wide tile. All at both phone widths.
func TestTilesBrowser_DOMOrderIsVisualOrder(t *testing.T) {
	ctx := uibrowser.New(t)
	s := startAttach(t)
	for _, width := range []int64{320, 360} {
		if err := uibrowser.At(ctx, width, s.URL+"/_gallery/tile-orders"); err != nil {
			t.Fatal(err)
		}
		if n := attachEval[int](t, ctx, `document.querySelectorAll(".segmented--tiles").length`); n != 32 {
			t.Errorf("%d groups drawn, want every one of the 32 mixes", n)
		}
		got := attachEval[[]string](t, ctx, `[...document.querySelectorAll(".segmented--tiles")].flatMap((g, n) => {
  const ls = [...g.children].map((l) => ({r: l.getBoundingClientRect(), wide: l.classList.contains("segmented__wide"), text: l.textContent.trim()}));
  const out = ls.slice(1).flatMap((l, i) => {
    const a = ls[i];
    return (l.r.top === a.r.top && l.r.left >= a.r.right) || l.r.top >= a.r.bottom ? [] : ["group " + n + ": " + a.text + " then " + l.text];
  });
  const shorts = ls.filter((l) => !l.wide).length;
  const rows = new Set(ls.map((l) => l.r.top)).size;
  const want = Math.ceil(shorts / 2) + ls.length - shorts;
  return rows === want ? out : [...out, "group " + n + ": " + rows + " rows, want " + want];
})`)
		if len(got) != 0 {
			t.Errorf("at %d px: DOM order isn't visual order: %q", width, got)
		}
	}
}

// e7bFile writes an empty-ish JPEG called name in a new directory.
func e7bFile(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte("\xff\xd8\xff\xe0 not really a photo"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// e7bTick sends upload progress from the upload's Choose file form.
func e7bTick(id string, loaded int) string {
	return `document.getElementById(` + strconv.Quote(id+"-file") + `).form.dispatchEvent(new CustomEvent("htmx:xhr:progress", {bubbles: true, detail: {loaded: ` + strconv.Itoa(loaded) + `, total: 100}}))`
}

// §5.9 at 320 px: while uploading, both labels look disabled and take no
// pointer, the status line names the file as text and the percent follows
// the progress beside it. Percent ticks never touch the status region, so
// they aren't announced. The longest file name a disk allows wraps, and a
// name that looks like markup stays text. All of it clears when the upload
// ends.
func TestUploadBrowser_BusyNameAndPercent(t *testing.T) {
	const id = "att-empty"
	ctx := uibrowser.New(t)
	for name, file := range map[string]string{
		"long":   strings.Repeat("bill-", 49) + ".jpg",
		"markup": `<img src=x>$&.jpg`,
	} {
		t.Run(name, func(t *testing.T) {
			s := startAttach(t)
			if err := uibrowser.At(ctx, 320, s.URL+"/_gallery/attachments"); err != nil {
				t.Fatal(err)
			}
			status := `document.getElementById(` + strconv.Quote(id+"-status") + `)`
			pct := attQ("#" + id + " .upload__percent")
			labels := `Array.from(document.querySelectorAll(` + strconv.Quote("#"+id+" .upload__button") + `))`
			inputs := attInputs(id)
			idleColor := attachEval[string](t, ctx, `getComputedStyle(`+labels+`[0]).color`)
			if got := attachEval[string](t, ctx, `getComputedStyle(`+inputs+`[0]).cursor`); got != "pointer" {
				t.Errorf("idle input cursor = %q", got)
			}
			attachEval[any](t, ctx, `(() => { window.e7bRecords = 0; new MutationObserver((r) => { window.e7bRecords += r.length; }).observe(`+status+`, {childList: true, characterData: true, subtree: true}); })()`)

			attachDo(t, ctx, "choose a file", chromedp.SetUploadFiles(chromedp.CSS("#"+id+"-file"), []string{e7bFile(t, file)}))
			s.held(t)
			attachWait(t, ctx, id, "busy", attBusy(id))
			if got := attachEval[string](t, ctx, pct+`.getAttribute("aria-hidden")`); got != "true" {
				t.Errorf("percent aria-hidden = %q", got)
			}
			if !attachEval[bool](t, ctx, `!`+status+`.contains(`+pct+`)`) {
				t.Error("the percent is inside the status region")
			}
			if got, want := attachEval[string](t, ctx, status+`.textContent`), "Uploading "+file+"…"; got != want {
				t.Errorf("status = %q, want %q", got, want)
			}
			if n := attachEval[int](t, ctx, status+`.children.length`); n != 0 {
				t.Errorf("the file name became %d elements", n)
			}
			attachWait(t, ctx, id, "the real progress shows", pct+`.textContent === "100%"`)
			records := attachEval[int](t, ctx, `window.e7bRecords`)
			for _, tick := range []int{25, 64} {
				attachEval[bool](t, ctx, e7bTick(id, tick))
				if got, want := attachEval[string](t, ctx, pct+`.textContent`), strconv.Itoa(tick)+"%"; got != want {
					t.Errorf("percent = %q, want %q", got, want)
				}
			}
			if got := attachEval[int](t, ctx, `window.e7bRecords`); got != records {
				t.Errorf("the percent changed the status region: %d records, had %d", got, records)
			}
			if got := attachEval[int](t, ctx, attQ("#"+id+" .upload__progress")+`.getBoundingClientRect().height`); got != 6 {
				t.Errorf("bar height = %d, want 6", got)
			}
			if !attachEval[bool](t, ctx, status+`.getBoundingClientRect().bottom <= `+attQ("#"+id+" .upload__progress")+`.getBoundingClientRect().top`) {
				t.Error("the bar isn't under the status line")
			}
			if !attachEval[bool](t, ctx, status+`.getBoundingClientRect().right <= `+pct+`.getBoundingClientRect().left`) {
				t.Error("the percent isn't beside the status line")
			}
			for i := range 2 {
				at := `[` + strconv.Itoa(i) + `]`
				attachWait(t, ctx, id, "the label greys", `getComputedStyle(`+labels+at+`).color === "rgb(138, 147, 161)"`)
				if got := attachEval[string](t, ctx, `getComputedStyle(`+labels+at+`).pointerEvents`); got != "none" {
					t.Errorf("label %d pointer-events = %q", i, got)
				}
				if got := attachEval[string](t, ctx, `getComputedStyle(`+inputs+at+`).cursor`); got == "pointer" {
					t.Errorf("input %d keeps the pointer cursor", i)
				}
			}
			e7bOverflow(t, ctx, "busy")

			s.reply(t, attachAnswer{status: http.StatusUnprocessableEntity, text: "No."})
			attachWait(t, ctx, id, "idle after the answer", attIdle(id))
			attachWait(t, ctx, id, "the label's colour returns", `getComputedStyle(`+labels+`[0]).color === `+strconv.Quote(idleColor))
		})
	}
}

// e7bStates records every text the status line of the upload id goes through.
func e7bStates(id string) string {
	return `(() => { const s = document.getElementById(` + strconv.Quote(id+"-status") + `); window.e7bStates = []; new MutationObserver(() => window.e7bStates.push(s.textContent)).observe(s, {childList: true, characterData: true, subtree: true}); })()`
}

const e7bDone = "Receipt attached."

// e7bStatus is the text of the upload id's status line.
func e7bStatus(id string) string {
	return `document.getElementById(` + strconv.Quote(id+"-status") + `).textContent`
}

// e7bSend chooses a file on the upload id and waits for the server to hold it.
func e7bSend(t *testing.T, ctx context.Context, s *attachServer, id string) {
	t.Helper()
	attachDo(t, ctx, "choose a file", chromedp.SetUploadFiles(chromedp.CSS("#"+id+"-file"), []string{e7bFile(t, "bill.jpg")}))
	s.held(t)
	attachWait(t, ctx, id, "busy", attBusy(id))
}

// e7bOpen opens the attachments page on a new server, recording id's status.
func e7bOpen(t *testing.T, ctx context.Context, id string) *attachServer {
	t.Helper()
	s := startAttach(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/attachments"); err != nil {
		t.Fatal(err)
	}
	attachEval[any](t, ctx, e7bStates(id))
	return s
}

// UX §5.5, WCAG 4.1.3: after a successful upload the status line goes from
// busy to the Done text, which replaces it in one step (nothing empty for a
// screen reader to skip) and stays. The next upload's busy text replaces it.
func TestUploadBrowser_DoneAnnounced(t *testing.T) {
	const id = "att-done"
	ctx := uibrowser.New(t)
	s := e7bOpen(t, ctx, id)
	if got := attachEval[string](t, ctx, attQ("#"+id+"-status")+`.dataset.done`); got != e7bDone {
		t.Fatalf("data-done = %q", got)
	}
	e7bSend(t, ctx, s, id)
	s.reply(t, attachAnswer{ok: true})
	attachWait(t, ctx, id, "the done text", e7bStatus(id)+` === `+strconv.Quote(e7bDone))
	if got, want := attachEval[[]string](t, ctx, `window.e7bStates`), []string{"Uploading bill.jpg…", e7bDone}; !slices.Equal(got, want) {
		t.Errorf("status went through %q, want %q", got, want)
	}
	attachWait(t, ctx, id, "the inputs are back", `!`+attInputs(id)+`.some(function (i) { return i.disabled; })`)
	if !attachEval[bool](t, ctx, attQ("#"+id+" .upload__progress")+`.hidden`) {
		t.Error("the bar stays up after the upload")
	}
	if got := attachEval[string](t, ctx, attQ("#"+id+" .upload__percent")+`.textContent`); got != "" {
		t.Errorf("percent = %q after the upload", got)
	}
	attachDo(t, ctx, "time to read it", chromedp.Sleep(time.Second))
	if got := attachEval[string](t, ctx, e7bStatus(id)); got != e7bDone {
		t.Errorf("the status line after a second = %q", got)
	}
	e7bSend(t, ctx, s, id) // e7bSend's busy wait: the next upload replaces it
}

// A refusal of any status, a dropped connection and a Remove never say Done,
// and a refusal or drop after a success takes the old Done text away.
func TestUploadBrowser_DoneNotOnFailure(t *testing.T) {
	const id = "att-done"
	ctx := uibrowser.New(t)
	for _, c := range []struct {
		name string
		a    attachAnswer
		want string
	}{
		{"422", attachAnswer{status: http.StatusUnprocessableEntity, text: "No."}, "No."},
		{"409", attachAnswer{status: http.StatusConflict, text: "No."}, "No."},
		{"403", attachAnswer{status: http.StatusForbidden, text: "No."}, "No."},
		{"413", attachAnswer{status: http.StatusRequestEntityTooLarge, text: "No."}, "No."},
		{"dropped", attachAnswer{drop: true}, msgDropped},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := e7bOpen(t, ctx, id)
			e7bSend(t, ctx, s, id)
			s.reply(t, attachAnswer{ok: true})
			attachWait(t, ctx, id, "the done text", e7bStatus(id)+` === `+strconv.Quote(e7bDone))
			attachEval[any](t, ctx, `window.e7bStates.length = 0`)

			e7bSend(t, ctx, s, id)
			s.reply(t, c.a)
			attachWait(t, ctx, id, "the message", attShows(id, c.want))
			attachWait(t, ctx, id, "idle", attIdle(id))
			if got := attachEval[[]string](t, ctx, `window.e7bStates`); slices.Contains(got, e7bDone) {
				t.Errorf("status went through %q", got)
			}
		})
	}
	t.Run("remove", func(t *testing.T) {
		s := e7bOpen(t, ctx, id)
		attachDo(t, ctx, "open the confirm", chromedp.Click(chromedp.CSS(`#att-done button[aria-label="Remove receipt.jpg"]`)))
		attachWait(t, ctx, id, "the confirm opens", `document.getElementById("remove-file-301").open`)
		attachDo(t, ctx, "confirm", chromedp.Click(chromedp.CSS("#remove-file-301 button[type=submit]")))
		s.held(t)
		s.reply(t, attachAnswer{ok: true})
		attachWait(t, ctx, id, "the answer arrives", `document.getElementById("att-done-files").textContent.trim() === "Stored."`)
		attachDo(t, ctx, "time for a late text", chromedp.Sleep(200*time.Millisecond))
		if got := attachEval[[]string](t, ctx, `window.e7bStates`); slices.Contains(got, e7bDone) {
			t.Errorf("status went through %q", got)
		}
		if got := attachEval[string](t, ctx, e7bStatus(id)); got != "" {
			t.Errorf("status = %q after a Remove", got)
		}
	})
}

// A caller with no Done keeps today's behaviour: the same successful answer
// leaves the status line empty, not a stale busy text.
func TestUploadBrowser_NoDoneStaysEmpty(t *testing.T) {
	const id = "att-no-done"
	ctx := uibrowser.New(t)
	s := e7bOpen(t, ctx, id)
	e7bSend(t, ctx, s, id)
	s.reply(t, attachAnswer{ok: true})
	attachWait(t, ctx, id, "idle after the answer", attIdle(id))
	if got, want := attachEval[[]string](t, ctx, `window.e7bStates`), []string{"Uploading bill.jpg…", ""}; !slices.Equal(got, want) {
		t.Errorf("status went through %q, want %q", got, want)
	}
}

// A Remove confirm's request reports progress too; only an upload shows a
// percent, which has no status line beside it there.
func TestUploadBrowser_RemoveShowsNoPercent(t *testing.T) {
	ctx := uibrowser.New(t)
	s := startAttach(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/attachments"); err != nil {
		t.Fatal(err)
	}
	attachEval[bool](t, ctx, `document.querySelector("#att-draft dialog form").dispatchEvent(new CustomEvent("htmx:xhr:progress", {bubbles: true, detail: {loaded: 1, total: 2}}))`)
	if got := attachEval[string](t, ctx, attQ("#att-draft .upload__percent")+`?.textContent ?? ""`); got != "" {
		t.Errorf("percent = %q on a Remove", got)
	}
}

// §5.9 at 360 px: with CardNamed the label and hint are clipped to a pixel
// and stay the group's name and the inputs' description; the tile's picture
// and file icon are 64 × 80, its long name wraps, and Remove still opens its
// confirm and posts. The upload without CardNamed keeps its words on show.
func TestUploadBrowser_CardNamedAndTile(t *testing.T) {
	const id = "att-tile"
	ctx := uibrowser.New(t)
	s := startAttach(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/attachments"); err != nil {
		t.Fatal(err)
	}
	size := func(sel string) string {
		return attachEval[string](t, ctx, `(() => { const r = `+attQ(sel)+`.getBoundingClientRect(); return r.width + "x" + r.height; })()`)
	}
	for _, sel := range []string{"#" + id + " legend", "#" + id + "-hint"} {
		if got := size(sel); got != "1x1" {
			t.Errorf("%s is %s, want clipped to 1x1", sel, got)
		}
	}
	if got := attachEval[string](t, ctx, `document.getElementById("att-tile-file").getAttribute("aria-describedby")`); got != "att-tile-hint att-tile-msg" {
		t.Errorf("aria-describedby = %q", got)
	}
	for _, sel := range []string{"#att-empty legend", "#att-empty-hint"} {
		if got := attachEval[bool](t, ctx, attQ(sel)+`.getBoundingClientRect().width > 20`); !got {
			t.Errorf("%s is hidden without CardNamed", sel)
		}
	}
	for _, sel := range []string{"#" + id + " .attachments__thumb", "#" + id + " .attachments__chip"} {
		if got := size(sel); got != "64x80" {
			t.Errorf("%s is %s, want 64x80", sel, got)
		}
	}
	e7bOverflow(t, ctx, "tiles")

	const remove = `#att-h button[aria-label="Remove receipt.jpg"]`
	attachDo(t, ctx, "open the confirm", chromedp.Click(chromedp.CSS(remove)))
	attachWait(t, ctx, id, "the confirm opens", `document.getElementById("remove-file-201").open`)
	attachDo(t, ctx, "confirm", chromedp.Click(chromedp.CSS("#remove-file-201 button[type=submit]")))
	s.held(t)
	s.reply(t, attachAnswer{status: http.StatusForbidden, text: "No."})
	attachWait(t, ctx, id, "the answer closes the confirm", `!document.getElementById("remove-file-201").open`)
}
