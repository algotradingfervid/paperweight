package expenseweb_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/expense/expensetest"
	expenseweb "github.com/vardhan/internaltools/internal/modules/expense/web"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// EXP-054 through the module routes POST /modules/expense/advances/{id}/paid
// and …/unpaid (spec §4, plan E9): the posted version, the redirect to the
// advance page, 422 with the message on a refusal, DOC's 409 on a stale
// version, AUTH's 403 and 404. The redraw is S3's renderPaidPage, so only
// the status and the message are asserted. AUTH's route guard (read) is
// internal/app's.

const (
	webMsgFuture      = "Paid on can't be in the future."
	webMsgAlreadyPaid = "This advance is already marked as paid."
	webMsgNotPaid     = "This advance isn't marked as paid."
	webMsgClaimUses   = "A claim already uses this advance, so it stays paid."
	webMsgNoCancel    = "You don't have permission to cancel Employee Advance. Ask your administrator for a role that allows it."
	webMsgStale       = "Someone else changed EA/26-27/00001 after you opened it. Reload it and make your changes again."

	webDeepak = "deepak@vardhan.test"
	webPriya  = "priya@vardhan.test"
	webMeena  = "meena@vardhan.test"
	webKavya  = "kavya@vardhan.test"
)

type webEnv struct {
	*expensetest.Env
	router chi.Router
}

func newWebEnv(t *testing.T) *webEnv {
	t.Helper()
	e := expensetest.New(t)
	r := chi.NewRouter()
	expenseweb.Routes(expenseweb.Deps{Service: e.Service, Engine: e.Engine, Auth: e.Auth, Files: e.Files})(r)
	return &webEnv{Env: e, router: r}
}

// approvedAdvance is Deepak's advance dated 2026-10-20, approved by Priya.
func (e *webEnv) approvedAdvance(t *testing.T) doc.Result {
	t.Helper()
	ctx := context.Background()
	res := e.DraftAdvance(t, e.P(t, webDeepak), expensetest.Values{"advance_date": "2026-10-20"})
	res, err := e.Engine.Do(ctx, e.P(t, webDeepak), expense.AdvanceDocType, res.ID, doc.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("submit advance: %v", err)
	}
	if res.State == doc.StatePending {
		res, err = e.Engine.Do(ctx, e.P(t, webPriya), expense.AdvanceDocType, res.ID, doc.ActionApprove, doc.ActionInput{Version: res.Version})
		if err != nil {
			t.Fatalf("approve advance: %v", err)
		}
	}
	if res.State != doc.StateApproved || res.Number != "EA/26-27/00001" {
		t.Fatalf("advance is %q %q, want approved EA/26-27/00001", res.State, res.Number)
	}
	return res
}

func (e *webEnv) stored(t *testing.T, id int64) (version int, paidOn sql.NullString) {
	t.Helper()
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(context.Background(),
			`SELECT version, paid_on FROM employee_advance WHERE id = ?`, id).Scan(&version, &paidOn)
	})
	if err != nil {
		t.Fatalf("read advance %d: %v", id, err)
	}
	return version, paidOn
}

// post sends form to the advance's route (paid or unpaid) as email.
func (e *webEnv) post(t *testing.T, email string, id int64, route string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	path := "/modules/expense/advances/" + strconv.FormatInt(id, 10) + "/" + route
	req := httptest.NewRequestWithContext(auth.WithPrincipal(context.Background(), e.P(t, email)),
		http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func form(version int, paidOn string) url.Values {
	f := url.Values{"version": {strconv.Itoa(version)}}
	if paidOn != "" {
		f.Set("paid_on", paidOn)
	}
	return f
}

// wantStatus checks the answer's status and that its page text carries msg
// (text, not bytes: the page escapes apostrophes).
func wantStatus(t *testing.T, what string, rec *httptest.ResponseRecorder, status int, msg string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("%s: status %d, want %d (body %q)", what, rec.Code, status, rec.Body.String())
	}
	if msg == "" {
		return
	}
	d, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatalf("%s: parse body: %v", what, err)
	}
	if text := strings.Join(strings.Fields(d.Text()), " "); !strings.Contains(text, msg) {
		t.Errorf("%s: page text %q doesn't carry %q", what, text, msg)
	}
}

// wantRefusedPost posts and expects status with msg, and nothing written.
func (e *webEnv) wantRefusedPost(t *testing.T, what, email string, id int64, route string, f url.Values, status int, msg string) {
	t.Helper()
	var rec *httptest.ResponseRecorder
	text := msg
	if text == "" {
		text = "-"
	}
	// WantRefused needs an error carrying the text; the answer's status and
	// body are checked separately.
	e.WantRefused(t, text, func() error {
		rec = e.post(t, email, id, route, f)
		return refusal(text)
	})
	wantStatus(t, what, rec, status, msg)
}

type refusal string

func (r refusal) Error() string { return string(r) }

func TestAdv_MarkAsPaidRoutes(t *testing.T) {
	t.Run("mark and undo redirect to the advance", func(t *testing.T) {
		t.Parallel()
		e := newWebEnv(t)
		adv := e.approvedAdvance(t)
		v, _ := e.stored(t, adv.ID)
		page := "/doc/" + expense.AdvanceDocType + "/" + strconv.FormatInt(adv.ID, 10)

		rec := e.post(t, webMeena, adv.ID, "paid", form(v, "21-10-2026"))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != page {
			t.Errorf("POST paid: status %d, Location %q; want 303, %q (body %q)", rec.Code, rec.Header().Get("Location"), page, rec.Body.String())
		}
		v2, paidOn := e.stored(t, adv.ID)
		if paidOn.String != "2026-10-21" || v2 != v+1 {
			t.Errorf("after POST paid: paid_on %v, version %d; want 2026-10-21, %d", paidOn, v2, v+1)
		}

		rec = e.post(t, webMeena, adv.ID, "unpaid", form(v2, ""))
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != page {
			t.Errorf("POST unpaid: status %d, Location %q; want 303, %q (body %q)", rec.Code, rec.Header().Get("Location"), page, rec.Body.String())
		}
		v3, paidOn := e.stored(t, adv.ID)
		if paidOn.Valid || v3 != v2+1 {
			t.Errorf("after POST unpaid: paid_on %v, version %d; want NULL, %d", paidOn, v3, v2+1)
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		e := newWebEnv(t)
		adv := e.approvedAdvance(t)
		v, _ := e.stored(t, adv.ID)

		e.wantRefusedPost(t, "deepak marks", webDeepak, adv.ID, "paid", form(v, "21-10-2026"), http.StatusForbidden, webMsgNoCancel)
		e.wantRefusedPost(t, "priya marks", webPriya, adv.ID, "paid", form(v, "21-10-2026"), http.StatusForbidden, webMsgNoCancel)
		e.wantRefusedPost(t, "kavya marks", webKavya, adv.ID, "paid", form(v, "21-10-2026"), http.StatusNotFound, auth.MsgNotFound)
		e.wantRefusedPost(t, "missing id", webMeena, adv.ID+100, "paid", form(v, "21-10-2026"), http.StatusNotFound, auth.MsgNotFound)
		e.wantRefusedPost(t, "future date", webMeena, adv.ID, "paid", form(v, "25-10-2026"), http.StatusUnprocessableEntity, webMsgFuture)
		e.wantRefusedPost(t, "stale mark", webMeena, adv.ID, "paid", form(v-1, "21-10-2026"), http.StatusConflict, webMsgStale)
		e.wantRefusedPost(t, "no version", webMeena, adv.ID, "paid", url.Values{"paid_on": {"21-10-2026"}}, http.StatusBadRequest, "")
		e.wantRefusedPost(t, "undo unpaid", webMeena, adv.ID, "unpaid", form(v, ""), http.StatusUnprocessableEntity, webMsgNotPaid)

		if rec := e.post(t, webMeena, adv.ID, "paid", form(v, "21-10-2026")); rec.Code != http.StatusSeeOther {
			t.Fatalf("POST paid as meena: status %d, want 303 (body %q)", rec.Code, rec.Body.String())
		}
		v, _ = e.stored(t, adv.ID)
		e.wantRefusedPost(t, "already paid", webMeena, adv.ID, "paid", form(v, "22-10-2026"), http.StatusUnprocessableEntity, webMsgAlreadyPaid)
		e.wantRefusedPost(t, "deepak undoes", webDeepak, adv.ID, "unpaid", form(v, ""), http.StatusForbidden, webMsgNoCancel)
		e.wantRefusedPost(t, "kavya undoes", webKavya, adv.ID, "unpaid", form(v, ""), http.StatusNotFound, auth.MsgNotFound)
		e.wantRefusedPost(t, "stale undo", webMeena, adv.ID, "unpaid", form(v-1, ""), http.StatusConflict, webMsgStale)
		e.wantRefusedPost(t, "undo with no version", webMeena, adv.ID, "unpaid", url.Values{}, http.StatusBadRequest, "")

		// A pending claim now uses the advance.
		deepak := e.P(t, webDeepak)
		c := e.DraftClaim(t, deepak, nil)
		c = e.Against(t, deepak, c.ID, adv.ID, 100000)
		if _, err := e.Engine.Do(context.Background(), deepak, expense.ClaimDocType, c.ID, doc.ActionSubmit, doc.ActionInput{Version: c.Version}); err != nil {
			t.Fatalf("submit claim: %v", err)
		}
		v, _ = e.stored(t, adv.ID)
		e.wantRefusedPost(t, "undo with a pending claim", webMeena, adv.ID, "unpaid", form(v, ""), http.StatusUnprocessableEntity, webMsgClaimUses)
	})
}
