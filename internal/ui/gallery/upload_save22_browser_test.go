//go:build e2e

package gallery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/chromedp/chromedp"

	"github.com/vardhan/internaltools/internal/platform/brand"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/layout"
	"github.com/vardhan/internaltools/internal/ui/uibrowser"
)

// HOOK-041 (CD-275 UX-E5; expense UX §5.5, EUX-030, EUX-039): the expense
// line page's shape, built from today's components: a FileUpload outside the
// line form whose AlsoDisable names the sticky bar's Save line, a line form
// with Amount, and the bar's Save line posting it through form=. The upload
// answers only #m22-files, so the inputs stay in the page (as on the line
// page), and is held until the test answers it.

const (
	m22Page    = "/_m22/line"
	m22Action  = "/_m22/receipt"
	m22Upload  = "m22-receipt"
	m22Form    = "m22-form"
	m22Amount  = "m22-amount"
	m22SaveSel = "button[form=" + m22Form + "]"
)

// m22Answer is how the server answers one held upload: a 200 with a new
// #m22-files, a refusal (422, HX-Reswap none and a notice), or a dropped
// connection.
type m22Answer struct {
	name           string
	ok, drop       bool
	status         int
	text, fileName string
}

var m22Answers = []m22Answer{
	{name: "success", ok: true, fileName: "receipt.jpg"},
	{name: "refused", status: http.StatusUnprocessableEntity, text: "File type .gif is not allowed. Use PDF, JPG, PNG, WEBP, HEIC, XLSX, DOCX or CSV."},
	{name: "dropped", drop: true},
}

// m22Server serves the m22 line page with the gallery's shell and static
// files, and holds each upload until the test answers it.
type m22Server struct {
	*httptest.Server
	arrived chan struct{}
	answer  chan m22Answer
	dropAll atomic.Bool   // after a drop, drop every post (a browser may retry one)
	done    chan struct{} // closed when the test ends, so a held post returns
}

func m22Start(t *testing.T) *m22Server {
	t.Helper()
	s := &m22Server{arrived: make(chan struct{}, 8), answer: make(chan m22Answer), done: make(chan struct{})}
	r := web.NewRouter(web.Options{})
	r.Use(ui.ShellMiddleware(shell))
	r.Handle("/static/*", web.Static())
	r.Handle("/static/ui/*", http.StripPrefix("/static/ui", ui.Static()))
	r.Handle("/static/brand/*", brand.Static())
	r.Get(m22Page, func(w http.ResponseWriter, r *http.Request) {
		render(w, r, http.StatusOK, layout.PageOpts{Title: "Add an expense", Width: layout.Form, BarSlot: m22Bar()}, m22Body())
	})
	r.Post(m22Action, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if s.dropAll.Load() {
			drop(t, w)
			return
		}
		s.arrived <- struct{}{}
		var a m22Answer
		select {
		case a = <-s.answer:
		case <-s.done:
			http.Error(w, "test ended", http.StatusInternalServerError)
			return
		case <-time.After(2 * attachStep):
			http.Error(w, "test never answered", http.StatusInternalServerError)
			return
		}
		switch {
		case a.drop:
			s.dropAll.Store(true)
			drop(t, w)
		case a.ok:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, `<div id="m22-files"><p>`+templ.EscapeString(a.fileName)+`</p></div>`)
		default:
			notice, err := json.Marshal(map[string]any{"notice": map[string]string{"level": "error", "text": a.text}})
			if err != nil {
				t.Error(err)
			}
			w.Header().Set("HX-Reswap", "none")
			w.Header().Set("HX-Trigger", string(notice))
			w.WriteHeader(a.status)
		}
	})
	s.Server = httptest.NewServer(r)
	t.Cleanup(func() {
		close(s.done)
		s.Close()
	})
	return s
}

// m22Body is the receipt area (outside the form) and the line form.
func m22Body() templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		up := components.UploadProps{ID: m22Upload, Label: "Receipt", Action: m22Action, Target: "#m22-files", AlsoDisable: m22SaveSel}
		if err := components.FileUpload(up).Render(ctx, w); err != nil {
			return err
		}
		if _, err := io.WriteString(w, `<div id="m22-files"></div>`); err != nil {
			return err
		}
		amount := components.TextInput(components.FieldProps{ID: m22Amount, Name: "amount", Label: "Amount"})
		return components.Form(components.FormProps{ID: m22Form, Action: "/_m22/save"}).Render(templ.WithChildren(ctx, amount), w)
	})
}

// m22Bar is the sticky bar with Save line posting the line form.
func m22Bar() templ.Component {
	return components.ActionBar(components.ActionBarProps{Primary: &components.ButtonProps{Label: "Save line", Variant: components.Primary}, Form: m22Form})
}

func (s *m22Server) held(t *testing.T) {
	t.Helper()
	select {
	case <-s.arrived:
	case <-time.After(attachStep):
		t.Fatal("no upload POST arrived")
	}
}

func (s *m22Server) reply(t *testing.T, a m22Answer) {
	t.Helper()
	select {
	case s.answer <- a:
	case <-time.After(attachStep):
		t.Fatal("the server took no answer")
	}
}

// js over the m22 page.
var (
	m22SaveJS   = attQ(m22SaveSel)
	m22AmountJS = `document.getElementById(` + strconv.Quote(m22Amount) + `)`
	m22InputJS  = `document.getElementById(` + strconv.Quote(m22Upload+"-file") + `)`
)

// m22Open loads the line page at a phone width and checks Save line is
// there, outside the upload, and enabled.
func m22Open(t *testing.T, ctx context.Context, s *m22Server) {
	t.Helper()
	if err := uibrowser.At(ctx, 360, s.URL+m22Page); err != nil {
		t.Fatal(err)
	}
	if !attachEval[bool](t, ctx, m22SaveJS+` !== null && !`+m22SaveJS+`.disabled && `+m22AmountJS+` !== null && `+m22InputJS+` !== null`) {
		t.Fatalf("the m22 page lacks an enabled Save line, Amount or Choose file: %s", attachEval[string](t, ctx, attState(m22Upload)))
	}
}

// m22WantSaveDuring fails unless the upload is busy and Save line is
// disabled; m22WantSaveAfter, unless both are idle again after it ends.
func m22WantSaveDuring(t *testing.T, ctx context.Context) {
	t.Helper()
	attachWait(t, ctx, m22Upload, "busy while uploading", attBusy(m22Upload))
	attachWait(t, ctx, m22Upload, "Save line disabled while uploading", m22SaveJS+`.disabled`)
}

func m22WantSaveAfter(t *testing.T, ctx context.Context, a m22Answer) {
	t.Helper()
	attachWait(t, ctx, m22Upload, "idle after the upload ends", attIdle(m22Upload))
	attachWait(t, ctx, m22Upload, "Save line enabled again after the upload ends", `!`+m22SaveJS+`.disabled`)
	if a.ok {
		attachWait(t, ctx, m22Upload, "the answer replaced #m22-files", `document.getElementById("m22-files").textContent.trim() === `+strconv.Quote(a.fileName))
	}
}

// HOOK-041: while a file uploads, Save line is disabled, and it is enabled
// again when the upload succeeds, is refused or drops. If the user moved
// focus into Amount during the upload, focus stays there when it ends and
// the typed digits are kept; if focus was on the file input (which falls to
// the body while it is disabled), it returns to that input.
func TestUploadBrowser_SaveDisabledFocus(t *testing.T) {
	ctx := uibrowser.New(t)
	file := attJPEG(t)
	for _, a := range m22Answers {
		t.Run(a.name+"/focus in Amount stays", func(t *testing.T) {
			s := m22Start(t) // its own server, so a held post never takes the next case's answer
			m22Open(t, ctx, s)
			attachDo(t, ctx, "choose a file", chromedp.SetUploadFiles(chromedp.CSS("#"+m22Upload+"-file"), []string{file}))
			s.held(t)
			m22WantSaveDuring(t, ctx)
			attachDo(t, ctx, "type 45 in Amount", chromedp.SendKeys(chromedp.CSS("#"+m22Amount), "45"))
			attachWait(t, ctx, m22Upload, "focus in Amount while uploading", `document.activeElement === `+m22AmountJS)
			s.reply(t, a)
			m22WantSaveAfter(t, ctx, a)
			if !attachEval[bool](t, ctx, `document.activeElement === `+m22AmountJS) {
				t.Errorf("after the upload ends focus is on %s, want it kept in Amount (UX-E5)", attachEval[string](t, ctx, `document.activeElement ? document.activeElement.outerHTML.slice(0, 120) : "(none)"`))
			}
			if got := attachEval[string](t, ctx, m22AmountJS+`.value`); got != "45" {
				t.Errorf("Amount = %q after the upload ends, want the typed 45", got)
			}
		})

		t.Run(a.name+"/focus on the input returns", func(t *testing.T) {
			s := m22Start(t)
			m22Open(t, ctx, s)
			attachDo(t, ctx, "focus Choose file", chromedp.Evaluate[chromedp.Void](m22InputJS+`.focus()`))
			attachWait(t, ctx, m22Upload, "focus on Choose file before the upload", `document.activeElement === `+m22InputJS)
			attachDo(t, ctx, "choose a file", chromedp.SetUploadFiles(chromedp.CSS("#"+m22Upload+"-file"), []string{file}))
			s.held(t)
			m22WantSaveDuring(t, ctx)
			if !attachEval[bool](t, ctx, `document.activeElement === `+m22InputJS+` || document.activeElement === document.body`) {
				t.Fatalf("while uploading focus is on %s, want the disabled input or the body", attachEval[string](t, ctx, `document.activeElement ? document.activeElement.outerHTML.slice(0, 120) : "(none)"`))
			}
			s.reply(t, a)
			m22WantSaveAfter(t, ctx, a)
			attachWait(t, ctx, m22Upload, "focus back on Choose file", `document.activeElement === `+m22InputJS)
		})
	}
}
