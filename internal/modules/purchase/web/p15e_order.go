package purchaseweb

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/web"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui"
	"github.com/vardhan/internaltools/internal/ui/components"
)

// The page redraws P10, P11 and P18's POSTs call for a refusal other than
// 403 and 404, with the HTTPError's status and field errors. Each reloads
// its page's state from the path id and shows the typed values from
// r.PostForm. They are variables so the POSTs' tests can watch the calls;
// the request carries its handlers (withHandlers).
var (
	renderHandover = func(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
		handlersOf(r).handoverRedraw(w, r, status, errs)
	}
	renderSend = func(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
		handlersOf(r).sendRedraw(w, r, status, errs)
	}
	renderExpected = func(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
		handlersOf(r).expectedRedraw(w, r, status, errs)
	}
)

type handlersKey struct{}

// withHandlers is r carrying h, for the render variables.
func (h *handlers) withHandlers(r *http.Request) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), handlersKey{}, h))
}

// handlersOf is the handlers withHandlers put on r.
func handlersOf(r *http.Request) *handlers {
	h, _ := r.Context().Value(handlersKey{}).(*handlers)
	return h
}

// pathID is the path's PO id; not a number is 0, which no PO has (404).
func pathID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	return id
}

func poHref(id int64) string { return "/doc/" + purchase.DocType + "/" + strconv.FormatInt(id, 10) }

// printHref is the PO's print view, which the browser prints or saves as a
// PDF (CD-258 Q1).
func printHref(id int64) string {
	return "/print/" + purchase.DocType + "/" + strconv.FormatInt(id, 10)
}

func orderPage(id int64, tail string) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + tail
}

// summaryLine joins the non-blank parts with " · " (UX §5.12 to §5.20).
func summaryLine(parts ...string) string {
	var set []string
	for _, s := range parts {
		if s != "" {
			set = append(set, s)
		}
	}
	return strings.Join(set, metaSep)
}

// pageErrs is a redraw's refusals as the page shows them: each field's
// message, the error summary linking to them, and a refusal with no field
// as a danger banner, with Reload when it is the stale 409.
type pageErrs struct {
	fields  map[string]string
	summary []components.FieldErr
	problem string
	reload  string
}

// errsOf sorts errs for the page at self; a field-less message equal to the
// page's own state refusal is left to the page's refusal banner.
func errsOf(status int, errs []doctype.FieldError, refusal, self string) pageErrs {
	e := pageErrs{fields: map[string]string{}}
	var problems []string
	for _, fe := range errs {
		if fe.Field == "" {
			if fe.Message != refusal {
				problems = append(problems, fe.Message)
			}
			continue
		}
		e.fields[fe.Field] = fe.Message
		e.summary = append(e.summary, components.FieldErr{ID: components.FieldID(fe.Field), Message: fe.Message})
	}
	e.problem = strings.Join(problems, " ")
	if status == http.StatusConflict {
		e.reload = self
	}
	return e
}

func (e pageErrs) banner() templ.Component {
	if e.problem == "" {
		return nil
	}
	b := components.BannerProps{Kind: components.Danger, Message: e.problem}
	if e.reload != "" {
		b.Link = &components.Link{Label: "Reload", Href: e.reload}
	}
	return components.Banner(b)
}

// handoverView is the hand-over page (UX §5.12).
type handoverView struct {
	id                int64
	st                purchase.HandOverState
	version, to, note string
	e                 pageErrs
}

// refusal is the state refusal's text, "" when the PO can be handed over.
func (v handoverView) refusal() string {
	if v.st.Refusal == nil {
		return ""
	}
	return v.st.Refusal.Error()
}

func (v handoverView) choices() []components.Option {
	opts := make([]components.Option, len(v.st.Eligible))
	for i, e := range v.st.Eligible {
		opts[i] = components.Option{Value: strconv.FormatInt(int64(e.ID), 10), Label: e.Name, Hint: e.Roles}
	}
	return opts
}

// handoverPage answers GET …/orders/{id}/handover from HandOverState: a
// missing or hidden PO is AUTH's 404, no right AUTH's 403.
func (h *handlers) handoverPage(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.HandOverState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	ui.Render(w, r, http.StatusOK, handoverPage(handoverView{id: id, st: st, version: strconv.Itoa(st.Version)}), nil)
}

// handoverRedraw is the page again with the refusal and what was posted.
func (h *handlers) handoverRedraw(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.HandOverState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := handoverView{id: id, st: st, version: r.PostFormValue("version"), to: r.PostFormValue("to"), note: r.PostFormValue("note")}
	v.e = errsOf(status, errs, v.refusal(), orderPage(id, "/handover"))
	ui.Render(w, r, status, handoverPage(v), nil)
}

// sentVia are the Send page's ways, in the spec's order (§23).
var sentVia = []components.Option{
	{Value: "email", Label: "Email"}, {Value: "whatsapp", Label: "WhatsApp"}, {Value: "courier", Label: "Courier"},
	{Value: "by_hand", Label: "By hand"}, {Value: "other", Label: "Other"},
}

// viaWords are sentVia's ways as words in the Sent sentence, "by hand" and
// not "by By hand" (UX §5.13); TestP24_SentBannerWords keeps both lists in
// step.
var viaWords = map[string]string{
	"email": "email", "whatsapp": "WhatsApp", "courier": "courier", "by_hand": "hand", "other": "other means",
}

// viaSentence is way v in the Sent sentence, v itself when it has no words.
func viaSentence(v string) string { return cmp.Or(viaWords[v], v) }

// sendView is the Send page (UX §5.13).
type sendView struct {
	id                     int64
	st                     purchase.SendState
	version, on, via, note string
	e                      pageErrs
}

func (v sendView) refusal() string {
	if v.st.Refusal == nil {
		return ""
	}
	return v.st.Refusal.Error()
}

// sentBanner is the record's banner text, "" when not sent.
func (v sendView) sentBanner() string {
	if v.st.Sent == nil {
		return ""
	}
	return fmt.Sprintf(msgSentBanner, types.FormatDate(v.st.Sent.SentOn), viaSentence(v.st.Sent.SentVia), v.st.SentByName)
}

func (v sendView) today() (string, string) {
	return types.FormatDate(v.st.Today), types.FormatDate(types.AddDays(v.st.Today, -1))
}

// sendPage answers GET …/orders/{id}/send from SendState: the record
// prefilled, else today.
func (h *handlers) sendPage(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.SendState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := sendView{id: id, st: st, version: strconv.Itoa(st.Version), on: types.FormatDate(st.Today)}
	if st.Sent != nil {
		v.on, v.via, v.note = types.FormatDate(st.Sent.SentOn), st.Sent.SentVia, st.Sent.Note
	}
	ui.Render(w, r, http.StatusOK, sendPage(v), nil)
}

func (h *handlers) sendRedraw(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.SendState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := sendView{id: id, st: st, version: r.PostFormValue("version"), on: r.PostFormValue("sent_on"), via: r.PostFormValue("sent_via"), note: r.PostFormValue("note")}
	v.e = errsOf(status, errs, v.refusal(), orderPage(id, "/send"))
	ui.Render(w, r, status, sendPage(v), nil)
}

// expectedView is the Expected on page (UX §5.20).
type expectedView struct {
	id                int64
	st                purchase.ExpectedState
	version, on, note string
	e                 pageErrs
}

func (v expectedView) summary() string {
	var due string
	if v.st.RequiredBy != (types.Date{}) {
		due = "Required by " + types.FormatDate(v.st.RequiredBy)
	}
	return summaryLine(v.st.Vendor, v.st.Total, due)
}

func (v expectedView) today() (string, string) {
	return types.FormatDate(v.st.Today), types.FormatDate(types.AddDays(v.st.Today, -1))
}

// expectedPage answers GET …/orders/{id}/expected from ExpectedState: the
// record prefilled, else blank.
func (h *handlers) expectedPage(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.ExpectedState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := expectedView{id: id, st: st, version: strconv.Itoa(st.Version)}
	if st.Set {
		v.on, v.note = types.FormatDate(st.Expected.ExpectedOn), st.Expected.Note
	}
	ui.Render(w, r, http.StatusOK, expectedPage(v), nil)
}

func (h *handlers) expectedRedraw(w http.ResponseWriter, r *http.Request, status int, errs []doctype.FieldError) {
	p, _ := auth.PrincipalFrom(r.Context())
	id := pathID(r)
	st, err := h.d.Service.ExpectedState(r.Context(), p, id)
	if err != nil {
		web.ErrorFrom(w, r, err)
		return
	}
	v := expectedView{id: id, st: st, version: r.PostFormValue("version"), on: r.PostFormValue("expected_on"), note: r.PostFormValue("note")}
	v.e = errsOf(status, errs, st.Refusal, orderPage(id, "/expected"))
	ui.Render(w, r, status, expectedPage(v), nil)
}
