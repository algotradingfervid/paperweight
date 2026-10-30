//go:build e2e

package gallery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// upload.js on the attachments mock (UX design 2026-10-31 §6 changes 1 and
// 2(a); the browser half of ATT-014, ATT-015, ATT-017, ATT-026, ATT-027 and
// ATT-029). The server answers each post as the test says, with the headers
// files/web gives htmx (CD-241): a refusal is its status, an empty body,
// HX-Reswap: none and an HX-Trigger notice.

// attachStep bounds every browser step and every wait on the server.
const attachStep = 5 * time.Second

const (
	msgDropped   = "The upload didn't finish. Check your connection and try again."
	msgRemoveOld = "Files can't be removed after the document is submitted. Cancel and amend it to change files."
)

// attachAnswer is how the server answers one held post: a refusal with
// status and text, or a dropped connection.
type attachAnswer struct {
	status int
	text   string
	drop   bool
}

// attachServer is gallery.Handler with POST /_gallery/attachments held until
// the test answers it.
type attachServer struct {
	*httptest.Server
	arrived chan string // each held post's Content-Type
	answer  chan attachAnswer
	dropAll atomic.Bool   // after a drop, drop every post (a browser may retry one)
	done    chan struct{} // closed when the test ends, so a held post returns
}

func startAttach(t *testing.T) *attachServer {
	t.Helper()
	s := &attachServer{arrived: make(chan string, 8), answer: make(chan attachAnswer), done: make(chan struct{})}
	h := Handler()
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != attachAction {
			h.ServeHTTP(w, r)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		if s.dropAll.Load() {
			drop(t, w)
			return
		}
		s.arrived <- r.Header.Get("Content-Type")
		var a attachAnswer
		select {
		case a = <-s.answer:
		case <-s.done:
			http.Error(w, "test ended", http.StatusInternalServerError)
			return
		case <-time.After(2 * attachStep):
			http.Error(w, "test never answered", http.StatusInternalServerError)
			return
		}
		if a.drop {
			s.dropAll.Store(true)
			drop(t, w)
			return
		}
		notice, err := json.Marshal(map[string]any{"notice": map[string]string{"level": "error", "text": a.text}})
		if err != nil {
			t.Error(err)
		}
		w.Header().Set("HX-Reswap", "none")
		w.Header().Set("HX-Trigger", string(notice))
		w.WriteHeader(a.status)
	}))
	t.Cleanup(func() {
		close(s.done)
		s.Close()
	})
	return s
}

// drop closes the connection with no answer.
func drop(t *testing.T, w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		t.Error("the test server can't drop a connection")
		return
	}
	if conn, _, err := hj.Hijack(); err == nil {
		_ = conn.Close()
	}
}

// held waits for the next post and returns its Content-Type.
func (s *attachServer) held(t *testing.T) string {
	t.Helper()
	select {
	case ct := <-s.arrived:
		return ct
	case <-time.After(attachStep):
		t.Fatal("no POST arrived")
		return ""
	}
}

// none checks no post arrives within a short while.
func (s *attachServer) none(t *testing.T, what string) {
	t.Helper()
	select {
	case <-s.arrived:
		t.Fatalf("%s posted", what)
	case <-time.After(400 * time.Millisecond):
	}
}

func (s *attachServer) reply(t *testing.T, a attachAnswer) {
	t.Helper()
	select {
	case s.answer <- a:
	case <-time.After(attachStep):
		t.Fatal("the server took no answer")
	}
}

func attachDo(t *testing.T, ctx context.Context, what string, steps ...chromedp.Action[chromedp.Void]) {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, attachStep)
	defer cancel()
	if err := chromedp.Do(sctx, steps...); err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

func attachEval[T any](t *testing.T, ctx context.Context, expr string) T {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, attachStep)
	defer cancel()
	v, err := chromedp.Run(sctx, chromedp.Evaluate[T](expr))
	if err != nil {
		t.Fatalf("evaluate %s: %v", expr, err)
	}
	return v
}

// attachWait waits until expr is true; on the deadline it fails with what
// and the upload's state.
func attachWait(t *testing.T, ctx context.Context, id, what, expr string) {
	t.Helper()
	sctx, cancel := context.WithTimeout(ctx, attachStep)
	defer cancel()
	if _, err := chromedp.Run(sctx, chromedp.Poll[bool](`(function () { try { return !!(`+expr+`); } catch (e) { return false; } })()`, chromedp.WithPollingInterval(50*time.Millisecond))); err != nil {
		t.Fatalf("%s: %v\nstate: %s", what, err, attachEval[string](t, ctx, attState(id)))
	}
}

// js builders over the FileUpload with id id.
func attQ(sel string) string { return `document.querySelector(` + strconv.Quote(sel) + `)` }

func attInputs(id string) string {
	return `Array.from(document.querySelectorAll(` + strconv.Quote("#"+id+" .upload__controls input[type=file]") + `))`
}

func attState(id string) string {
	return `JSON.stringify({inputs: ` + attInputs(id) + `.map(function (i) { return {disabled: i.disabled, invalid: i.getAttribute("aria-invalid")}; }),
  bar: (function (p) { return p && {hidden: p.hidden, value: p.value}; })(` + attQ("#"+id+" .upload__progress") + `),
  status: (function (e) { return e && e.textContent; })(document.getElementById(` + strconv.Quote(id+"-status") + `)),
  msg: (function (e) { return e && e.outerHTML; })(document.getElementById(` + strconv.Quote(id+"-msg") + `)),
  active: document.activeElement && document.activeElement.outerHTML.slice(0, 120),
  dialogs: Array.from(document.querySelectorAll("dialog[open]")).map(function (d) { return d.id; })})`
}

// busy: both inputs disabled, the bar shown and Uploading… in the status line.
func attBusy(id string) string {
	return `(` + attInputs(id) + `.every(function (i) { return i.disabled; }) && !` + attQ("#"+id+" .upload__progress") + `.hidden && document.getElementById(` + strconv.Quote(id+"-status") + `).textContent.trim() === "Uploading…")`
}

// idle: both inputs enabled, the bar hidden at 0 and the status line empty.
func attIdle(id string) string {
	return `(` + attInputs(id) + `.every(function (i) { return !i.disabled; }) && ` + attQ("#"+id+" .upload__progress") + `.hidden && ` + attQ("#"+id+" .upload__progress") + `.value === 0 && document.getElementById(` + strconv.Quote(id+"-status") + `).textContent === "")`
}

// shows: the message region holds text in the error style with its icon.
func attShows(id, text string) string {
	m := `document.getElementById(` + strconv.Quote(id+"-msg") + `)`
	return `(` + m + `.textContent.trim() === ` + strconv.Quote(text) + ` && ` + m + `.classList.contains("field__msg--error") && ` + m + `.querySelector("svg") !== null)`
}

func attInvalid(id string) string {
	return attInputs(id) + `.filter(function (i) { return i.getAttribute("aria-invalid") === "true"; }).length`
}

const attAlertCount = `document.getElementById("alerts").children.length`

func attAlertsHold(text string) string {
	return `document.getElementById("alerts").textContent.indexOf(` + strconv.Quote(text) + `) >= 0`
}

func attJPEG(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "receipt.jpg")
	if err := os.WriteFile(p, []byte("\xff\xd8\xff\xe0 not really a photo"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// §6 changes 1 and 2(a): while a file uploads both inputs are disabled and
// Uploading… shows; any refusal (a 422 htmx counts as a success, a 413 or
// 403 as an error) clears that, returns focus to the input used and writes
// the notice under the controls, marking both inputs; the next upload clears
// the message and the marks when it starts.
func TestAttachBrowser_UploadRefused(t *testing.T) {
	const id = "att-empty"
	ctx := uibrowser.New(t)
	file := attJPEG(t)
	for _, c := range []attachAnswer{
		{status: http.StatusUnprocessableEntity, text: "File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."},
		{status: http.StatusRequestEntityTooLarge, text: "File is larger than 10 MB. Choose a smaller file or compress it."},
		{status: http.StatusForbidden, text: "You don't have permission to add files to Draft 42."},
	} {
		t.Run(strconv.Itoa(c.status), func(t *testing.T) {
			s := startAttach(t) // its own server, so a held post never takes the next case's answer
			if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/attachments"); err != nil {
				t.Fatal(err)
			}
			if got := attachEval[string](t, ctx, `(document.getElementById(`+strconv.Quote(id+"-status")+`) || {textContent: "(no status line)"}).textContent`); got != "" {
				t.Errorf("the status line holds %q before any upload", got)
			}
			alerts := attachEval[int](t, ctx, attAlertCount)

			attachDo(t, ctx, "choose a file", chromedp.SetUploadFiles(chromedp.CSS("#"+id+"-file"), []string{file}))
			if ct := s.held(t); !strings.HasPrefix(ct, "multipart/form-data") {
				t.Errorf("upload Content-Type = %q", ct)
			}
			attachWait(t, ctx, id, "busy while uploading", attBusy(id))
			s.reply(t, c)
			attachWait(t, ctx, id, "idle after the refusal", attIdle(id))
			attachWait(t, ctx, id, "focus back on Choose file", `document.activeElement === document.getElementById(`+strconv.Quote(id+"-file")+`)`)
			attachWait(t, ctx, id, "the refusal under the controls", attShows(id, c.text))
			if n := attachEval[int](t, ctx, attInvalid(id)); n != 2 {
				t.Errorf("%d inputs have aria-invalid=true after an upload refusal, want 2", n)
			}
			attachWait(t, ctx, id, "the refusal in #alerts", attAlertsHold(c.text))
			if n := attachEval[int](t, ctx, attAlertCount); n != alerts+1 {
				t.Errorf("#alerts gained %d notices, want 1", n-alerts)
			}

			// The next upload, from the other input, starts clean.
			attachDo(t, ctx, "take a photo", chromedp.SetUploadFiles(chromedp.CSS("#"+id+" input[capture]"), []string{file}))
			s.held(t)
			attachWait(t, ctx, id, "busy again", attBusy(id))
			if got := attachEval[string](t, ctx, `document.getElementById(`+strconv.Quote(id+"-msg")+`).textContent.trim()`); got != "" {
				t.Errorf("the message region still holds %q while the next upload runs", got)
			}
			if n := attachEval[int](t, ctx, attInvalid(id)); n != 0 {
				t.Errorf("%d inputs keep aria-invalid while the next upload runs", n)
			}
			s.reply(t, attachAnswer{status: http.StatusUnprocessableEntity, text: "File is empty. Choose another file."})
			attachWait(t, ctx, id, "idle after the second refusal", attIdle(id))
			attachWait(t, ctx, id, "focus back on Take photo", `document.activeElement === `+attQ("#"+id+" input[capture]"))
			attachWait(t, ctx, id, "the second refusal", attShows(id, "File is empty. Choose another file."))
		})
	}
}

// §3 "Network drops mid-upload", ATT-029's browser half: no answer at all
// clears the busy state, returns focus and writes upload.js's own text under
// the controls, with nothing in #alerts.
func TestAttachBrowser_DroppedConnection(t *testing.T) {
	const id = "att-empty"
	s := startAttach(t)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 360, s.URL+"/_gallery/attachments"); err != nil {
		t.Fatal(err)
	}
	alerts := attachEval[int](t, ctx, attAlertCount)
	attachDo(t, ctx, "take a photo", chromedp.SetUploadFiles(chromedp.CSS("#"+id+" input[capture]"), []string{attJPEG(t)}))
	s.held(t)
	attachWait(t, ctx, id, "busy while uploading", attBusy(id))
	s.reply(t, attachAnswer{drop: true})
	attachWait(t, ctx, id, "idle after the drop", attIdle(id))
	attachWait(t, ctx, id, "the dropped-connection text", attShows(id, msgDropped))
	attachWait(t, ctx, id, "focus back on Take photo", `document.activeElement === `+attQ("#"+id+" input[capture]"))
	if n := attachEval[int](t, ctx, attAlertCount); n != alerts {
		t.Errorf("#alerts gained %d notices on a dropped connection, want none", n-alerts)
	}
	if n := attachEval[int](t, ctx, attInvalid(id)); n != 0 {
		t.Errorf("%d inputs have aria-invalid after a dropped connection, want none (the file wasn't wrong)", n)
	}
	if n := attachEval[int](t, ctx, `document.querySelectorAll("#`+id+` .attachments > li").length`); n != 0 {
		t.Errorf("the list changed: %d files", n)
	}
}

// §5.4 and §6 change 2: Remove opens the confirm with no request; Keep file
// closes it and focus returns to Remove; a refused Remove file closes the
// confirm, then shows the notice under the controls without marking the
// inputs (ATT-017, ATT-027).
func TestAttachBrowser_RemoveConfirm(t *testing.T) {
	const id = "att-draft"
	s := startAttach(t)
	ctx := uibrowser.New(t)
	if err := uibrowser.At(ctx, 390, s.URL+"/_gallery/attachments"); err != nil {
		t.Fatal(err)
	}
	remove := attQ("#" + id + ` button[aria-label="Remove receipt.jpg"]`)
	dialog := `document.getElementById("remove-file-1")`
	inDialog := func(label string) string {
		return `Array.from(` + dialog + `.querySelectorAll("button")).find(function (b) { return b.textContent.trim() === ` + strconv.Quote(label) + `; })`
	}
	if !attachEval[bool](t, ctx, remove+` !== null && `+dialog+` !== null && `+dialog+`.closest("#`+id+`") !== null`) {
		t.Fatalf("no Remove receipt.jpg with its confirm inside #%s: %s", id, attachEval[string](t, ctx, attState(id)))
	}

	attachDo(t, ctx, "tap Remove", chromedp.Evaluate[chromedp.Void](remove+`.click()`))
	attachWait(t, ctx, id, "the confirm opens", dialog+`.open`)
	s.none(t, "Remove")

	attachDo(t, ctx, "tap Keep file", chromedp.Evaluate[chromedp.Void](inDialog("Keep file")+`.click()`))
	attachWait(t, ctx, id, "Keep file closes the confirm", `!`+dialog+`.open`)
	attachWait(t, ctx, id, "focus back on Remove", `document.activeElement === `+remove)
	s.none(t, "Keep file")

	alerts := attachEval[int](t, ctx, attAlertCount)
	attachDo(t, ctx, "tap Remove again", chromedp.Evaluate[chromedp.Void](remove+`.click()`))
	attachWait(t, ctx, id, "the confirm opens again", dialog+`.open`)
	attachDo(t, ctx, "tap Remove file", chromedp.Evaluate[chromedp.Void](inDialog("Remove file")+`.click()`))
	if ct := s.held(t); strings.HasPrefix(ct, "multipart/") {
		t.Errorf("Remove file posted %q, want a plain form", ct)
	}
	s.reply(t, attachAnswer{status: http.StatusUnprocessableEntity, text: msgRemoveOld})
	attachWait(t, ctx, id, "the refused remove closes the confirm", `!`+dialog+`.open`)
	attachWait(t, ctx, id, "the refusal under the controls", attShows(id, msgRemoveOld))
	attachWait(t, ctx, id, "focus back on Remove", `document.activeElement === `+remove)
	if n := attachEval[int](t, ctx, attInvalid(id)); n != 0 {
		t.Errorf("%d inputs have aria-invalid after a refused remove, want none", n)
	}
	attachWait(t, ctx, id, "the refusal in #alerts", attAlertsHold(msgRemoveOld))
	if n := attachEval[int](t, ctx, attAlertCount); n != alerts+1 {
		t.Errorf("#alerts gained %d notices, want 1", n-alerts)
	}
	if n := attachEval[int](t, ctx, `document.querySelectorAll("#`+id+` .attachments > li").length`); n != 3 {
		t.Errorf("the list has %d files after a refused remove, want 3", n)
	}
}
