package purchaseweb

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Helpers and tests for P13's POST handler (MOD-PO P13; PO spec §20.4 item
// 1; UX §5.18). All names carry p13.

// The texts the Create PO handler answers with (PO spec §20.4, DOC-069's
// stale wording; CD-142).
const (
	p13PickVendor = "Pick a vendor."
	p13Nothing    = "Everything on %s is already on an order." // request display
)

func p13Stale(display string) string {
	return "Someone else changed " + display + " after you opened it. Reload it and make your changes again."
}

// p13Render is one renderFromRequest call.
type p13Render struct {
	status int
	errs   []doctype.FieldError
}

// p13Web is a purchase world with the module routes on a bare router (AUTH's
// guard and CSRF are internal/app's) and renderFromRequest replaced by a
// recorder that still answers with the plain messages.
type p13Web struct {
	env     *purchasetest.Env
	router  chi.Router
	renders []p13Render
}

func newP13Web(t *testing.T) *p13Web {
	t.Helper()
	env := purchasetest.New(t)
	r := chi.NewRouter()
	Routes(Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: scs.New(),
		CSRF: func(*http.Request) string { return "p13-token" },
	})(r)
	w := &p13Web{env: env, router: r}
	saved := renderFromRequest
	renderFromRequest = func(rw http.ResponseWriter, _ *http.Request, status int, errs []doctype.FieldError) {
		w.renders = append(w.renders, p13Render{status: status, errs: errs})
		plainErrors(rw, status, errs)
	}
	t.Cleanup(func() { renderFromRequest = saved })
	return w
}

// p13Answer is one response: status, Location and the body's text.
type p13Answer struct {
	status   int
	location string
	text     string
}

// post sends form to the request's Create PO route as p, as a plain form
// post.
func (w *p13Web) post(t *testing.T, p auth.Principal, requestID int64, form url.Values) p13Answer {
	t.Helper()
	w.renders = nil
	path := "/modules/purchase/requests/" + strconv.FormatInt(requestID, 10) + "/order"
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), p), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	w.router.ServeHTTP(rec, req)
	d, err := goquery.NewDocumentFromReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("POST %s: parse body: %v", path, err)
	}
	return p13Answer{status: rec.Code, location: rec.Header().Get("Location"), text: strings.Join(strings.Fields(d.Text()), " ")}
}

// id is the id, as a form value, of the master row with code in table.
func (w *p13Web) id(t *testing.T, table, code string) string {
	t.Helper()
	var n int64
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT id FROM `+table+` WHERE code = ?`, code).Scan(&n); err != nil { //nolint:gosec // G202: fixed table names
		t.Fatalf("id of %s %s: %v", table, code, err)
	}
	return strconv.FormatInt(n, 10)
}

// orders is the number of purchase orders and lastOrder the newest one's id.
func (w *p13Web) orders(t *testing.T) (n int, last int64) {
	t.Helper()
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*), COALESCE(max(id), 0) FROM purchase_order`).Scan(&n, &last); err != nil {
		t.Fatalf("count purchase_order: %v", err)
	}
	return n, last
}

// version is request id's stored version, as a form value.
func (w *p13Web) version(t *testing.T, id int64) string {
	t.Helper()
	var v int
	if err := w.env.DB.SQL().QueryRowContext(t.Context(), `SELECT version FROM purchase_request WHERE id = ?`, id).Scan(&v); err != nil {
		t.Fatalf("version of request %d: %v", id, err)
	}
	return strconv.Itoa(v)
}

// request is Suresh's submitted request (METRO-SITE / PRJ-METRO) with one row
// of 100 kg MS-ANGLE-50.
func (w *p13Web) request(t *testing.T) doc.Doc {
	t.Helper()
	return purchasetest.NewRequest(t, w.env)
}

// hiddenRequest is Ravi's submitted TECH-SITE / PRJ-TECHPARK request, which
// Priya (PRJ-METRO only) can't read.
func (w *p13Web) hiddenRequest(t *testing.T) int64 {
	t.Helper()
	env, ctx := w.env, t.Context()
	res, err := env.Engine.Do(ctx, env.Ravi, purchase.RequestDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"site_id": w.id(t, "site", "TECH-SITE"), "project_id": w.id(t, "project", "PRJ-TECHPARK"),
			"request_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{{"item_id": w.id(t, "item", "MS-ANGLE-50"), "uom_id": w.id(t, "uom", "KGS"), "qty": "10"}},
	})
	if err != nil {
		t.Fatalf("save Ravi's TECH-SITE request: %v", err)
	}
	if _, err := env.Engine.Do(ctx, env.Ravi, purchase.RequestDocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version}); err != nil {
		t.Fatalf("submit Ravi's TECH-SITE request: %v", err)
	}
	return res.ID
}

// load reads PO id as DOC stores it.
func (w *p13Web) load(t *testing.T, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := w.env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d
}

func p13Form(version, vendor string) url.Values {
	return url.Values{"version": {version}, "vendor_id": {vendor}}
}

// p13Details is the new PO's Details step, where the wizard opens.
func p13Details(id int64) string {
	return "/doc/purchase.Order/" + strconv.FormatInt(id, 10) + "/edit/details"
}

// PO-051 handler (PO spec §20.4 item 1; plan P13 step 5 and P1 Do 3; UX
// §5.18; CD-94, CD-146, CD-245): POST /modules/purchase/requests/{id}/order
// with the request's version and a vendor creates the draft and redirects to
// its Details step. A 422 or 409 redraws through renderFromRequest with its
// status and message; AUTH's 403 and 404 use the error page, never the
// redraw; a missing or non-numeric version is 400. Only status and message
// text are asserted (P15d tests the redraw). Subtests marked "waits on P6"
// reach PendingToOrder.
func TestPurchaseWeb_FromRequestPost(t *testing.T) {
	w := newP13Web(t)
	env := w.env
	shree := w.id(t, "party", "SHREE")

	t.Run("create redirects to the new PO's Details (waits on P6)", func(t *testing.T) {
		req := w.request(t)
		n, _ := w.orders(t)
		a := w.post(t, env.Priya, req.ID, p13Form(w.version(t, req.ID), shree))
		got, last := w.orders(t)
		if got != n+1 {
			t.Fatalf("orders after the post = %d, want %d (%d %q)", got, n+1, a.status, a.text)
		}
		if a.status != http.StatusSeeOther || a.location != p13Details(last) {
			t.Errorf("POST = %d Location %q (%q), want 303 to %s", a.status, a.location, a.text, p13Details(last))
		}
		po := w.load(t, last)
		if fmt.Sprint(po.Header["vendor_id"]) != shree || po.CreatedBy != env.Priya.ID {
			t.Errorf("new PO vendor_id %v, owner %d; want SHREE %s, Priya %d", po.Header["vendor_id"], po.CreatedBy, shree, env.Priya.ID)
		}
		if len(w.renders) != 0 {
			t.Errorf("renderFromRequest called on success: %+v", w.renders)
		}
	})

	t.Run("a stale version redraws with DOC's 409 naming the request", func(t *testing.T) {
		req := w.request(t)
		v, _ := strconv.Atoi(w.version(t, req.ID))
		n, _ := w.orders(t)
		stale := p13Stale(naming.Label(req.Number, req.ID))
		a := w.post(t, env.Priya, req.ID, p13Form(strconv.Itoa(v-1), shree))
		if a.status != http.StatusConflict || !strings.Contains(a.text, stale) {
			t.Errorf("POST = %d %q, want 409 with %q", a.status, a.text, stale)
		}
		if len(w.renders) != 1 || w.renders[0].status != http.StatusConflict || len(w.renders[0].errs) != 1 || w.renders[0].errs[0].Message != stale {
			t.Errorf("renders = %+v, want one 409 with %q", w.renders, stale)
		}
		if got, _ := w.orders(t); got != n {
			t.Errorf("orders after the refusal = %d, want %d", got, n)
		}
	})

	t.Run("a blank vendor redraws with 422 Pick a vendor. on vendor_id", func(t *testing.T) {
		req := w.request(t)
		n, _ := w.orders(t)
		a := w.post(t, env.Priya, req.ID, p13Form(w.version(t, req.ID), ""))
		if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text, p13PickVendor) {
			t.Errorf("POST = %d %q, want 422 with %q", a.status, a.text, p13PickVendor)
		}
		want := doctype.FieldError{Field: "vendor_id", Message: p13PickVendor}
		if len(w.renders) != 1 || w.renders[0].status != http.StatusUnprocessableEntity || len(w.renders[0].errs) != 1 ||
			w.renders[0].errs[0].Field != want.Field || w.renders[0].errs[0].Message != want.Message {
			t.Errorf("renders = %+v, want one 422 with %+v", w.renders, want)
		}
		if got, _ := w.orders(t); got != n {
			t.Errorf("orders after the refusal = %d, want %d", got, n)
		}
	})

	t.Run("nothing pending redraws with 422 and §20.4's text (waits on P6)", func(t *testing.T) {
		req := w.request(t)
		purchasetest.NewOrder(t, env, purchasetest.StopAtDraft(), purchasetest.OrderLines(map[string]string{
			"request_line_id": strconv.FormatInt(req.Lines[0].ID, 10), "qty": "100", // holds all 100 kg
		}))
		n, _ := w.orders(t)
		msg := fmt.Sprintf(p13Nothing, naming.Label(req.Number, req.ID))
		a := w.post(t, env.Priya, req.ID, p13Form(w.version(t, req.ID), shree))
		if a.status != http.StatusUnprocessableEntity || !strings.Contains(a.text, msg) {
			t.Errorf("POST = %d %q, want 422 with %q", a.status, a.text, msg)
		}
		if len(w.renders) != 1 || w.renders[0].status != http.StatusUnprocessableEntity || len(w.renders[0].errs) != 1 || w.renders[0].errs[0].Message != msg {
			t.Errorf("renders = %+v, want one 422 with %q", w.renders, msg)
		}
		if got, _ := w.orders(t); got != n {
			t.Errorf("orders after the refusal = %d, want %d", got, n)
		}
	})

	// AUTH's refusals use the platform's error page, never the redraw.
	req := w.request(t)
	hidden := w.hiddenRequest(t)
	denied := (&auth.DeniedError{Right: auth.RightCreate, Label: "Purchase Order", Plural: "Purchase Orders"}).Error()
	reqV, _ := strconv.Atoi(w.version(t, req.ID))
	for _, c := range []struct {
		name   string
		p      auth.Principal
		id     int64
		form   url.Values
		status int
		msg    string
	}{
		{"Suresh can't create a PO (waits on P6)", env.Suresh, req.ID, p13Form(w.version(t, req.ID), shree), http.StatusForbidden, denied},
		{"Suresh with a stale version is AUTH's 403, rights before version (CD-259)", env.Suresh, req.ID, p13Form(strconv.Itoa(reqV-1), shree), http.StatusForbidden, denied},
		{"a missing request", env.Priya, 999_999, p13Form("1", shree), http.StatusNotFound, auth.MsgNotFound},
		{"a hidden request", env.Priya, hidden, p13Form(w.version(t, hidden), shree), http.StatusNotFound, auth.MsgNotFound},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, _ := w.orders(t)
			a := w.post(t, c.p, c.id, c.form)
			if a.status != c.status || !strings.Contains(a.text, c.msg) {
				t.Errorf("POST = %d %q, want %d with %q", a.status, a.text, c.status, c.msg)
			}
			if len(w.renders) != 0 {
				t.Errorf("renderFromRequest called for a %d: %+v (403 and 404 use the error page)", c.status, w.renders)
			}
			if got, _ := w.orders(t); got != n {
				t.Errorf("orders after the refusal = %d, want %d", got, n)
			}
		})
	}

	// Every web write posts version (CD-94, CD-146, CD-183): missing or not a
	// number is 400 with AUTH's bad-form text, and nothing is created.
	for _, c := range []struct {
		name string
		form url.Values
	}{
		{"no version", url.Values{"vendor_id": {shree}}},
		{"version x", p13Form("x", shree)},
		{"an empty version", p13Form("", shree)},
	} {
		t.Run(c.name, func(t *testing.T) {
			n, _ := w.orders(t)
			a := w.post(t, env.Priya, req.ID, c.form)
			if a.status != http.StatusBadRequest || !strings.Contains(a.text, auth.ErrBadForm.Error()) {
				t.Errorf("POST = %d %q, want 400 with %q", a.status, a.text, auth.ErrBadForm.Error())
			}
			if len(w.renders) != 0 {
				t.Errorf("renderFromRequest called for a bad form: %+v", w.renders)
			}
			if got, _ := w.orders(t); got != n {
				t.Errorf("orders after the refusal = %d, want %d", got, n)
			}
		})
	}
}
