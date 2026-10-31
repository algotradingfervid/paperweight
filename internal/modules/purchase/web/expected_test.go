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

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// The texts the Expected on handlers answer with (PO spec §27.1, DOC's
// unreadable-value and stale wording, AUTH's bad form; CD-142, CD-146).
const (
	p18NotApproved = "Only an approved order can have an expected date."
	p18Blank       = "Enter the expected date."
	p18BeforeOrder = "Expected on can't be before the order date."
	p18Unparseable = "Expected on isn't a valid value. Check it and try again."
)

func p18Stale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// p18Render is one renderExpected call.
type p18Render struct {
	status int
	msgs   []string
}

// p18Web is a purchase world with the module routes on a bare router, as
// TestPurchaseWeb_RoutesStubbed mounts them (AUTH's guard and CSRF are
// internal/app's, TestApp_PurchaseRoutesStubbed), and renderExpected
// replaced by a recorder that still answers with the plain messages.
type p18Web struct {
	env     *purchasetest.Env
	router  chi.Router
	renders []p18Render
}

func newP18Web(t *testing.T) *p18Web {
	t.Helper()
	env := purchasetest.New(t)
	r := chi.NewRouter()
	Routes(Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: scs.New(),
		CSRF: func(*http.Request) string { return "p18-token" },
	})(r)
	w := &p18Web{env: env, router: r}
	saved := renderExpected
	renderExpected = func(rw http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) {
		msgs := make([]string, len(errs))
		for i, e := range errs {
			msgs[i] = e.Message
		}
		w.renders = append(w.renders, p18Render{status: status, msgs: msgs})
		plainErrors(rw, status, errs)
	}
	t.Cleanup(func() { renderExpected = saved })
	return w
}

// p18Answer is one response: status, Location and the body's text.
type p18Answer struct {
	status   int
	location string
	text     string
}

// post sends form to path as p, as a plain (non-htmx) form post.
func (w *p18Web) post(t *testing.T, p auth.Principal, path string, form url.Values) p18Answer {
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
	return p18Answer{status: rec.Code, location: rec.Header().Get("Location"), text: strings.Join(strings.Fields(d.Text()), " ")}
}

// row is the PO's Expected on row: expected_on (ISO) and version; ok false
// when there is none.
func (w *p18Web) row(t *testing.T, id int64) (on string, version int, ok bool) {
	t.Helper()
	err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT expected_on, version FROM purchase_order_expected WHERE po_id = ?`, id).Scan(&on, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false
	}
	if err != nil {
		t.Fatalf("read purchase_order_expected row of PO %d: %v", id, err)
	}
	return on, version, true
}

// rows is the number of Expected on rows.
func (w *p18Web) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM purchase_order_expected`).Scan(&n); err != nil {
		t.Fatalf("count purchase_order_expected: %v", err)
	}
	return n
}

func expectedPath(id int64) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + "/expected"
}

func clearPath(id int64) string { return expectedPath(id) + "/clear" }

func poPage(id int64) string { return "/doc/purchase.Order/" + strconv.FormatInt(id, 10) }

// setForm is the Expected on page's form: the row's version, the date as
// typed and the note.
func setForm(version, on, note string) url.Values {
	return url.Values{"version": {version}, "expected_on": {on}, "note": {note}}
}

// PO-080 handlers (PO spec §27.1; plan P18 and P1 Do 3; CD-94, CD-146,
// CD-245): POST …/expected and …/expected/clear redirect to the PO page on
// success; a refusal redraws through renderExpected with the HTTPError's
// status (422, or 409 when stale) and its message; AUTH's 403 and 404 use
// the error page, never the redraw; a missing or unreadable version is 400.
// Only status and message text are asserted (P15e tests the redraw).
func TestPurchaseWeb_ExpectedPost(t *testing.T) {
	w := newP18Web(t)
	env := w.env
	po := purchasetest.NewOrder(t, env)
	display := naming.Label(po.Number, po.ID)
	draft := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	t.Run("set and clear redirect to the PO", func(t *testing.T) {
		a := w.post(t, env.Arjun, expectedPath(po.ID), setForm("0", "05-11-2026", "Vendor called"))
		if a.status != http.StatusSeeOther || a.location != poPage(po.ID) {
			t.Errorf("set = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, poPage(po.ID))
		}
		if on, v, ok := w.row(t, po.ID); !ok || on != "2026-11-05" || v != 1 {
			t.Errorf("row after set = %q v%d %v, want 2026-11-05 v1", on, v, ok)
		}
		if len(w.renders) != 0 {
			t.Errorf("renderExpected called on success: %+v", w.renders)
		}

		a = w.post(t, env.Ravi, expectedPath(po.ID), setForm("1", "24-10-2026", ""))
		if a.status != http.StatusSeeOther || a.location != poPage(po.ID) {
			t.Errorf("change = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, poPage(po.ID))
		}
		if on, v, ok := w.row(t, po.ID); !ok || on != "2026-10-24" || v != 2 {
			t.Errorf("row after change = %q v%d %v, want 2026-10-24 v2", on, v, ok)
		}

		a = w.post(t, env.Arjun, clearPath(po.ID), url.Values{"version": {"2"}})
		if a.status != http.StatusSeeOther || a.location != poPage(po.ID) {
			t.Errorf("clear = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, poPage(po.ID))
		}
		if _, _, ok := w.row(t, po.ID); ok {
			t.Error("row after clear still there, want deleted")
		}
	})

	// A row at version 1 for the refusals below.
	if a := w.post(t, env.Arjun, expectedPath(po.ID), setForm("0", "05-11-2026", "")); a.status != http.StatusSeeOther {
		t.Fatalf("set for the refusal cases = %d (%q), want 303", a.status, a.text)
	}

	redraws := []struct {
		name   string
		p      auth.Principal
		path   string
		form   url.Values
		status int
		msg    string
	}{
		{"draft", env.Priya, expectedPath(draft.ID), setForm("0", "05-11-2026", ""), http.StatusUnprocessableEntity, p18NotApproved},
		{"blank date", env.Arjun, expectedPath(po.ID), setForm("1", "", ""), http.StatusUnprocessableEntity, p18Blank},
		{"unparseable date", env.Arjun, expectedPath(po.ID), setForm("1", "soon", ""), http.StatusUnprocessableEntity, p18Unparseable},
		{"impossible date", env.Arjun, expectedPath(po.ID), setForm("1", "31-02-2026", ""), http.StatusUnprocessableEntity, p18Unparseable},
		{"before the order date", env.Arjun, expectedPath(po.ID), setForm("1", "23-10-2026", ""), http.StatusUnprocessableEntity, p18BeforeOrder},
		{"set stale", env.Ravi, expectedPath(po.ID), setForm("0", "24-10-2026", ""), http.StatusConflict, p18Stale(display)},
		{"clear stale", env.Ravi, clearPath(po.ID), url.Values{"version": {"3"}}, http.StatusConflict, p18Stale(display)},
	}
	for _, c := range redraws {
		t.Run(c.name, func(t *testing.T) {
			n := w.rows(t)
			a := w.post(t, c.p, c.path, c.form)
			if a.status != c.status || !strings.Contains(a.text, c.msg) {
				t.Errorf("POST = %d %q, want %d with %q", a.status, a.text, c.status, c.msg)
			}
			want := []p18Render{{status: c.status, msgs: []string{c.msg}}}
			if len(w.renders) != 1 || w.renders[0].status != c.status || !slices.Equal(w.renders[0].msgs, want[0].msgs) {
				t.Errorf("renderExpected calls = %+v, want %+v", w.renders, want)
			}
			if got := w.rows(t); got != n {
				t.Errorf("rows after the refusal = %d, want %d", got, n)
			}
			if on, v, ok := w.row(t, po.ID); !ok || on != "2026-11-05" || v != 1 {
				t.Errorf("po's row after the refusal = %q v%d %v, want 2026-11-05 v1", on, v, ok)
			}
		})
	}

	// AUTH's refusals use the platform's error page, never the redraw.
	denied := (&auth.DeniedError{Right: auth.RightAmend, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	deniedApprove := (&auth.DeniedError{Right: auth.RightApprove, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	errorPages := []struct {
		name   string
		p      auth.Principal
		path   string
		form   url.Values
		status int
		msgs   []string // any one of them
	}{
		{"set as Suresh", env.Suresh, expectedPath(po.ID), setForm("1", "06-11-2026", ""), http.StatusForbidden, []string{denied, deniedApprove}},
		{"clear as Suresh", env.Suresh, clearPath(po.ID), url.Values{"version": {"1"}}, http.StatusForbidden, []string{denied, deniedApprove}},
		{"set on a missing PO", env.Arjun, expectedPath(999_999), setForm("0", "06-11-2026", ""), http.StatusNotFound, []string{auth.MsgNotFound}},
		{"clear on a missing PO", env.Arjun, clearPath(999_999), url.Values{"version": {"1"}}, http.StatusNotFound, []string{auth.MsgNotFound}},
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
				t.Errorf("renderExpected called for a %d: %+v (403 and 404 use the error page)", c.status, w.renders)
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
		{"set without version", expectedPath(po.ID), url.Values{"expected_on": {"06-11-2026"}}},
		{"set with version x", expectedPath(po.ID), setForm("x", "06-11-2026", "")},
		{"clear without version", clearPath(po.ID), url.Values{}},
		{"clear with version x", clearPath(po.ID), url.Values{"version": {"x"}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := w.post(t, env.Arjun, c.path, c.form)
			if a.status != http.StatusBadRequest || !strings.Contains(a.text, auth.ErrBadForm.Msg) {
				t.Errorf("POST = %d %q, want 400 with %q", a.status, a.text, auth.ErrBadForm.Msg)
			}
			if on, v, ok := w.row(t, po.ID); !ok || on != "2026-11-05" || v != 1 {
				t.Errorf("po's row = %q v%d %v, want 2026-11-05 v1", on, v, ok)
			}
		})
	}
}
