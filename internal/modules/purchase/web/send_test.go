package purchaseweb

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// The texts the Send handlers answer with (PO spec §23, DOC's stale wording,
// AUTH's bad form; CD-142, CD-146).
const (
	p11NotApproved = "Only an approved order can be marked as sent."
	p11Future      = "Sent on can't be in the future."
	p11BeforeOrder = "Sent on can't be before the order date."
	p11NoMethod    = "Choose how it was sent."
)

func p11Stale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// p11Render is one renderSend call.
type p11Render struct {
	status int
	stale  bool
	msgs   []string
}

// p11Web is a purchase world with the module routes on a bare router, as
// TestPurchaseWeb_RoutesStubbed mounts them (AUTH's guard and CSRF are
// internal/app's), and renderSend replaced by a recorder that still answers
// with the plain messages.
type p11Web struct {
	env     *purchasetest.Env
	router  chi.Router
	renders []p11Render
}

func newP11Web(t *testing.T) *p11Web {
	t.Helper()
	env := purchasetest.New(t)
	r := chi.NewRouter()
	pd := Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: scs.New(),
		CSRF: func(*http.Request) string { return "p11-token" },
	}
	pd.ListHidden = docscreen.Deps{DB: env.DB, Modules: Modules(pd)}.ListHidden
	Routes(pd)(r)
	w := &p11Web{env: env, router: r}
	saved := renderSend
	renderSend = func(rw http.ResponseWriter, _ *http.Request, status int, stale bool, errs []doctype.FieldError) {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Message
		}
		w.renders = append(w.renders, p11Render{status: status, stale: stale, msgs: msgs})
		plainErrors(rw, status, errs)
	}
	t.Cleanup(func() { renderSend = saved })
	return w
}

// p11Answer is one response: status, Location and the body's text.
type p11Answer struct {
	status   int
	location string
	text     string
}

// post sends form to path as p, as a plain (non-htmx) form post.
func (w *p11Web) post(t *testing.T, p auth.Principal, path string, form url.Values) p11Answer {
	t.Helper()
	w.renders = nil
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("POST %s: parse body: %v", path, err)
	}
	return p11Answer{status: rec.Code, location: rec.Header().Get("Location"), text: strings.Join(strings.Fields(d.Text()), " ")}
}

// row is the PO's Sent row: sent_on (ISO), sent_via and version; ok false
// when there is none.
func (w *p11Web) row(t *testing.T, id int64) (on, via string, version int, ok bool) {
	t.Helper()
	err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT sent_on, sent_via, version FROM purchase_order_sent WHERE po_id = ?`, id).Scan(&on, &via, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, false
	}
	if err != nil {
		t.Fatalf("read purchase_order_sent row of PO %d: %v", id, err)
	}
	return on, via, version, true
}

// rows is the number of Sent rows.
func (w *p11Web) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM purchase_order_sent`).Scan(&n); err != nil {
		t.Fatalf("count purchase_order_sent: %v", err)
	}
	return n
}

// hiddenOrder is an approved PRJ-TECHPARK / TECH-SITE PO by Arjun through
// Engine.Do, hidden from Priya (PRJ-METRO only).
func (w *p11Web) hiddenOrder(t *testing.T) int64 {
	t.Helper()
	env, ctx := w.env, t.Context()
	id := func(table, code string) string {
		var n int64
		if err := env.DB.SQL().QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
			t.Fatalf("id of %s %s: %v", table, code, err)
		}
		return strconv.FormatInt(n, 10)
	}
	res, err := env.Engine.Do(ctx, env.Arjun, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id": id("party", "SHREE"), "site_id": id("site", "TECH-SITE"), "project_id": id("project", "PRJ-TECHPARK"),
			"order_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{{
			"item_id": id("item", "MS-ANGLE-50"), "uom_id": id("uom", "KGS"),
			"qty": "10", "price": "62.5", "tax_code_id": id("tax_code", "GST18"), "hsn_sac": "7216",
		}},
	})
	if err != nil {
		t.Fatalf("save PRJ-TECHPARK PO as Arjun: %v", err)
	}
	res, err = env.Engine.Do(ctx, env.Arjun, purchase.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil || res.State != doctype.StateApproved {
		t.Fatalf("submit PRJ-TECHPARK PO as Arjun = %s, %v; want approved", res.State, err)
	}
	return res.ID
}

func sendPath(id int64) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + "/send"
}

func undoPath(id int64) string { return sendPath(id) + "/undo" }

func p11POPage(id int64) string { return "/doc/purchase.Order/" + strconv.FormatInt(id, 10) }

// sendForm is the Send page's form: the row's version, Sent on as typed, How
// (sent_via) and the note.
func sendForm(version, on, via, note string) url.Values {
	return url.Values{"version": {version}, "sent_on": {on}, "sent_via": {via}, "note": {note}}
}

// PO-063 handlers (PO spec §23; plan P11 and P1 Do 3; CD-94, CD-146,
// CD-245): POST …/send and …/send/undo redirect to the PO page on success; a
// refusal redraws through renderSend with the HTTPError's status (422, or
// 409 when stale) and its message; AUTH's 403 and 404 use the error page,
// never the redraw; a missing or non-numeric version is 400. Only status and
// message text are asserted (P15e tests the redraw).
func TestPurchaseWeb_SendPost(t *testing.T) {
	w := newP11Web(t)
	env := w.env
	po := purchasetest.NewOrder(t, env) // ordered 24-10-2026
	display := naming.Label(po.Number, po.ID)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
	hidden := w.hiddenOrder(t)
	env.SetNow(t, time.Date(2026, time.October, 31, 5, 0, 0, 0, time.UTC)) // 31-10-2026 10:30 IST

	t.Run("mark, change and undo redirect to the PO", func(t *testing.T) {
		a := w.post(t, env.Priya, sendPath(po.ID), sendForm("0", "31-10-2026", "whatsapp", "To Mr Shah"))
		if a.status != http.StatusSeeOther || a.location != p11POPage(po.ID)+"?sent=1" {
			t.Errorf("mark = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, p11POPage(po.ID))
		}
		if on, via, v, ok := w.row(t, po.ID); !ok || on != "2026-10-31" || via != "whatsapp" || v != 1 {
			t.Errorf("row after mark = %q %q v%d %v, want 2026-10-31 whatsapp v1", on, via, v, ok)
		}
		if len(w.renders) != 0 {
			t.Errorf("renderSend called on success: %+v", w.renders)
		}

		a = w.post(t, env.Ravi, sendPath(po.ID), sendForm("1", "24-10-2026", "courier", ""))
		if a.status != http.StatusSeeOther || a.location != p11POPage(po.ID)+"?sent=1" {
			t.Errorf("change = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, p11POPage(po.ID))
		}
		if on, via, v, ok := w.row(t, po.ID); !ok || on != "2026-10-24" || via != "courier" || v != 2 {
			t.Errorf("row after change = %q %q v%d %v, want 2026-10-24 courier v2", on, via, v, ok)
		}

		a = w.post(t, env.Priya, undoPath(po.ID), url.Values{"version": {"2"}})
		if a.status != http.StatusSeeOther || a.location != p11POPage(po.ID) {
			t.Errorf("undo = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, p11POPage(po.ID))
		}
		if _, _, _, ok := w.row(t, po.ID); ok {
			t.Error("row after undo still there, want deleted")
		}
		if len(w.renders) != 0 {
			t.Errorf("renderSend called on success: %+v", w.renders)
		}
	})

	// A row at version 1 for the refusals below.
	if a := w.post(t, env.Priya, sendPath(po.ID), sendForm("0", "30-10-2026", "email", "")); a.status != http.StatusSeeOther {
		t.Errorf("mark for the refusal cases = %d (%q), want 303 (the cases below then fail too)", a.status, a.text)
	}

	redraws := []struct {
		name   string
		p      auth.Principal
		path   string
		form   url.Values
		status int
		msg    string
	}{
		{"draft", env.Priya, sendPath(draft.ID), sendForm("0", "31-10-2026", "email", ""), http.StatusUnprocessableEntity, p11NotApproved},
		{"future date", env.Priya, sendPath(po.ID), sendForm("1", "01-11-2026", "email", ""), http.StatusUnprocessableEntity, p11Future},
		{"before the order date", env.Priya, sendPath(po.ID), sendForm("1", "23-10-2026", "email", ""), http.StatusUnprocessableEntity, p11BeforeOrder},
		{"no method chosen", env.Priya, sendPath(po.ID), sendForm("1", "31-10-2026", "", ""), http.StatusUnprocessableEntity, p11NoMethod},
		{"no method field", env.Priya, sendPath(po.ID), url.Values{"version": {"1"}, "sent_on": {"31-10-2026"}}, http.StatusUnprocessableEntity, p11NoMethod},
		{"mark stale", env.Ravi, sendPath(po.ID), sendForm("0", "31-10-2026", "email", ""), http.StatusConflict, p11Stale(display)},
		{"undo stale", env.Ravi, undoPath(po.ID), url.Values{"version": {"3"}}, http.StatusConflict, p11Stale(display)},
	}
	for _, c := range redraws {
		t.Run(c.name, func(t *testing.T) {
			n := w.rows(t)
			a := w.post(t, c.p, c.path, c.form)
			if a.status != c.status || !strings.Contains(a.text, c.msg) {
				t.Errorf("POST = %d %q, want %d with %q", a.status, a.text, c.status, c.msg)
			}
			want := p11Render{status: c.status, stale: c.msg == p11Stale(display), msgs: []string{c.msg}}
			if len(w.renders) != 1 || w.renders[0].status != want.status || w.renders[0].stale != want.stale || !slices.Equal(w.renders[0].msgs, want.msgs) {
				t.Errorf("renderSend calls = %+v, want one %+v", w.renders, want)
			}
			if got := w.rows(t); got != n {
				t.Errorf("rows after the refusal = %d, want %d", got, n)
			}
			if on, via, v, ok := w.row(t, po.ID); !ok || on != "2026-10-30" || via != "email" || v != 1 {
				t.Errorf("po's row after the refusal = %q %q v%d %v, want 2026-10-30 email v1", on, via, v, ok)
			}
		})
	}

	// AUTH's refusals use the platform's error page, never the redraw.
	denied := []string{
		(&auth.DeniedError{Right: auth.RightWrite, Label: "Purchase Order", Plural: "Purchase Orders"}).Error(),
		(&auth.DeniedError{Right: auth.RightWrite, Label: "Purchase Order", Plural: "Purchase Orders", OwnOnly: true}).Error(),
		(&auth.DeniedError{Right: auth.RightApprove, Label: "Purchase Order", Plural: "Purchase Orders"}).Error(),
	}
	errorPages := []struct {
		name   string
		p      auth.Principal
		path   string
		form   url.Values
		status int
		msgs   []string // any one of them
	}{
		{"mark as Arjun", env.Arjun, sendPath(po.ID), sendForm("1", "31-10-2026", "email", ""), http.StatusForbidden, denied},
		{"undo as Arjun", env.Arjun, undoPath(po.ID), url.Values{"version": {"1"}}, http.StatusForbidden, denied},
		{"mark as Suresh", env.Suresh, sendPath(po.ID), sendForm("1", "31-10-2026", "email", ""), http.StatusForbidden, denied},
		{"undo as Suresh", env.Suresh, undoPath(po.ID), url.Values{"version": {"1"}}, http.StatusForbidden, denied},
		{"mark on a missing PO", env.Priya, sendPath(999_999), sendForm("0", "31-10-2026", "email", ""), http.StatusNotFound, []string{auth.MsgNotFound}},
		{"undo on a missing PO", env.Priya, undoPath(999_999), url.Values{"version": {"1"}}, http.StatusNotFound, []string{auth.MsgNotFound}},
		{"mark on a hidden PO", env.Priya, sendPath(hidden), sendForm("0", "31-10-2026", "email", ""), http.StatusNotFound, []string{auth.MsgNotFound}},
		{"undo on a hidden PO", env.Priya, undoPath(hidden), url.Values{"version": {"1"}}, http.StatusNotFound, []string{auth.MsgNotFound}},
	}
	for _, c := range errorPages {
		t.Run(c.name, func(t *testing.T) {
			n := w.rows(t)
			a := w.post(t, c.p, c.path, c.form)
			found := slices.ContainsFunc(c.msgs, func(m string) bool { return strings.Contains(a.text, m) })
			if a.status != c.status || !found {
				t.Errorf("POST = %d %q, want %d with one of %q", a.status, a.text, c.status, c.msgs)
			}
			if len(w.renders) != 0 {
				t.Errorf("renderSend called for a %d: %+v (403 and 404 use the error page)", c.status, w.renders)
			}
			if got := w.rows(t); got != n {
				t.Errorf("rows after the refusal = %d, want %d", got, n)
			}
		})
	}

	// Every web write posts version (CD-94, CD-146, CD-183): missing or not a
	// number is 400 with AUTH's bad-form text, and nothing changes.
	for _, c := range []struct {
		name string
		path string
		form url.Values
	}{
		{"mark without version", sendPath(po.ID), url.Values{"sent_on": {"31-10-2026"}, "sent_via": {"email"}}},
		{"mark with version x", sendPath(po.ID), sendForm("x", "31-10-2026", "email", "")},
		{"mark with an empty version", sendPath(po.ID), sendForm("", "31-10-2026", "email", "")},
		{"undo without version", undoPath(po.ID), url.Values{}},
		{"undo with version x", undoPath(po.ID), url.Values{"version": {"x"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := w.post(t, env.Priya, c.path, c.form)
			if a.status != http.StatusBadRequest || !strings.Contains(a.text, auth.ErrBadForm.Msg) {
				t.Errorf("POST = %d %q, want 400 with %q", a.status, a.text, auth.ErrBadForm.Msg)
			}
			if len(w.renders) != 0 {
				t.Errorf("renderSend called for a 400: %+v", w.renders)
			}
			if on, via, v, ok := w.row(t, po.ID); !ok || on != "2026-10-30" || via != "email" || v != 1 {
				t.Errorf("po's row = %q %q v%d %v, want 2026-10-30 email v1", on, via, v, ok)
			}
		})
	}
}
