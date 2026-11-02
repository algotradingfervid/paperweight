package purchaseweb

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// Texts the POST passes on unchanged (PO spec §21.2, §22; DOC-069; CD-142).
const (
	hwCantCreate = "Deepak Nair can't raise purchase orders. Pick someone with the Purchase User role."
	hwSameOwner  = "This order is already Priya Shah's."
	hwPick       = "Pick who takes over this order."
	hwTooLong    = "A comment can be at most 1,000 characters."
	hwNotDraft   = "An approved, closed or cancelled order can't be handed over."
	hwStale      = "Someone else changed %s after you opened it. Reload it and make your changes again."
)

// hwMissing is the 404 text for a PO that doesn't exist (or a bad id):
// AUTH's, the same as a hidden PO (CD-245), never DOC's missing text.
var hwMissing = []string{auth.MsgNotFound}

// hwRender is one call of renderHandover.
type hwRender struct {
	status int
	stale  bool
	errs   []doctype.FieldError
}

// hwRecordRender replaces renderHandover (P15e's page redraw) for one test
// and returns its calls. Tests that use it never call t.Parallel.
func hwRecordRender(t *testing.T) *[]hwRender {
	t.Helper()
	var calls []hwRender
	old := renderHandover
	renderHandover = func(w http.ResponseWriter, _ *http.Request, status int, stale bool, errs []doctype.FieldError) {
		calls = append(calls, hwRender{status, stale, slices.Clone(errs)})
		w.WriteHeader(status)
	}
	t.Cleanup(func() { renderHandover = old })
	return &calls
}

func hwHandler(env *purchasetest.Env) http.Handler {
	sessions := scs.New()
	r := chi.NewRouter()
	pd := Deps{
		Service: env.Service, Approvals: env.Approvals, Comments: env.Comments, Sessions: sessions,
		CSRF:        func(*http.Request) string { return "token" },
		ReceiveLink: func(context.Context, auth.Principal, int64) (string, bool) { return "", false },
	}
	pd.ListHidden = docscreen.Deps{DB: env.DB, Modules: Modules(pd)}.ListHidden
	Routes(pd)(r)
	return sessions.LoadAndSave(r)
}

func hwPost(t *testing.T, h http.Handler, p auth.Principal, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), p), http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func hwPath(id int64) string {
	return "/modules/purchase/orders/" + strconv.FormatInt(id, 10) + "/handover"
}

func hwForm(version int, to types.UserID, note string) url.Values {
	f := url.Values{"version": {strconv.Itoa(version)}, "note": {note}}
	if to != 0 {
		f.Set("to", strconv.FormatInt(int64(to), 10))
	}
	return f
}

func hwOwner(t *testing.T, env *purchasetest.Env, id int64) (types.UserID, int) {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d.CreatedBy, d.Version
}

// hwErrorPage fails unless rec is the platform's error page for status with
// one of msgs, and renderHandover was never called (plan P1 Do 3: 403 and 404 never
// go through a render function).
func hwErrorPage(t *testing.T, rec *httptest.ResponseRecorder, calls *[]hwRender, status int, msgs ...string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `role="alert"`) || !slices.ContainsFunc(msgs, func(m string) bool { return strings.Contains(body, html.EscapeString(m)) }) {
		t.Errorf("body = %q, want the error page with one of %q", body, msgs)
	}
	if len(*calls) != 0 {
		t.Errorf("renderHandover called %+v, want the error page instead", *calls)
	}
}

// hwRendered fails unless renderHandover was called once with status and
// want among its field errors, and the response has that status.
func hwRendered(t *testing.T, rec *httptest.ResponseRecorder, calls *[]hwRender, status int, want doctype.FieldError) {
	t.Helper()
	if len(*calls) != 1 {
		t.Fatalf("renderHandover calls = %+v (response %d %q), want one", *calls, rec.Code, rec.Body.String())
	}
	c := (*calls)[0]
	if c.status != status || !slices.Contains(c.errs, want) {
		t.Errorf("renderHandover(%d, %+v), want (%d, [... %+v ...])", c.status, c.errs, status, want)
	}
	if rec.Code != status {
		t.Errorf("response status = %d, want %d", rec.Code, status)
	}
}

// PO-059 web (plan P10, P1 Do 3; UX 5.12; CD-142): POST
// /modules/purchase/orders/{id}/handover. Success is 303 to the PO page; a
// refusal goes through renderHandover with the HTTPError's status and field
// errors (Field "to" for the person-specific ones); a stale version is 409
// through it; 403 and 404 use the platform's error page.
func TestPurchaseWeb_HandOverPost(t *testing.T) {
	env := purchasetest.New(t)
	h := hwHandler(env)

	t.Run("success redirects to the PO page", func(t *testing.T) {
		calls := hwRecordRender(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		rec := hwPost(t, h, env.Priya, hwPath(po.ID), hwForm(po.Version, env.Arjun.ID, "Over to you."))
		if want := "/doc/" + purchase.DocType + "/" + strconv.FormatInt(po.ID, 10); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
			t.Errorf("POST = %d Location %q, want 303 to %q", rec.Code, rec.Header().Get("Location"), want)
		}
		if len(*calls) != 0 {
			t.Errorf("renderHandover called %+v on success", *calls)
		}
		if owner, v := hwOwner(t, env, po.ID); owner != env.Arjun.ID || v != po.Version+1 {
			t.Errorf("after POST: owner %d version %d, want Arjun (%d), %d", owner, v, env.Arjun.ID, po.Version+1)
		}
	})

	for _, c := range []struct {
		name string
		to   func() types.UserID
		note string
		want doctype.FieldError
	}{
		{"can't raise POs", func() types.UserID { return env.Deepak.ID }, "", doctype.FieldError{Field: "to", Message: hwCantCreate}},
		{"same owner", func() types.UserID { return env.Priya.ID }, "", doctype.FieldError{Field: "to", Message: hwSameOwner}},
		{"nobody picked", func() types.UserID { return 0 }, "", doctype.FieldError{Message: hwPick}},
		{"note too long", func() types.UserID { return env.Arjun.ID }, strings.Repeat("n", 1001), doctype.FieldError{Message: hwTooLong}},
	} {
		t.Run("refusal "+c.name, func(t *testing.T) {
			calls := hwRecordRender(t)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
			rec := hwPost(t, h, env.Priya, hwPath(po.ID), hwForm(po.Version, c.to(), c.note))
			if c.want.Field == "" {
				// Not person-specific: only the message is fixed, at any field.
				if len(*calls) == 1 {
					for _, e := range (*calls)[0].errs {
						if e.Message == c.want.Message {
							c.want.Field = e.Field
						}
					}
				}
			}
			hwRendered(t, rec, calls, http.StatusUnprocessableEntity, c.want)
			if owner, v := hwOwner(t, env, po.ID); owner != env.Priya.ID || v != po.Version {
				t.Errorf("after a refused POST: owner %d version %d, want Priya (%d), %d", owner, v, env.Priya.ID, po.Version)
			}
		})
	}

	t.Run("state refusal", func(t *testing.T) {
		calls := hwRecordRender(t)
		po := purchasetest.NewOrder(t, env)
		rec := hwPost(t, h, env.Priya, hwPath(po.ID), hwForm(po.Version, env.Arjun.ID, ""))
		if len(*calls) != 1 {
			t.Fatalf("renderHandover calls = %+v (response %d %q), want one", *calls, rec.Code, rec.Body.String())
		}
		c := (*calls)[0]
		if (c.status != http.StatusConflict && c.status != http.StatusUnprocessableEntity) ||
			!slices.ContainsFunc(c.errs, func(e doctype.FieldError) bool { return e.Message == hwNotDraft }) {
			t.Errorf("renderHandover(%d, %+v), want 409 or 422 with %q", c.status, c.errs, hwNotDraft)
		}
		if c.stale {
			t.Error("a state refusal reached the redraw as stale (CD-381)")
		}
	})

	t.Run("stale version is 409", func(t *testing.T) {
		calls := hwRecordRender(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		if _, err := env.Engine.Do(t.Context(), env.Priya, purchase.DocType, po.ID, doctype.ActionSave, doc.ActionInput{
			Version: po.Version, Values: map[string]string{"remarks": "Other tab"}, Only: []string{"remarks"},
		}); err != nil {
			t.Fatalf("setup: save: %v", err)
		}
		rec := hwPost(t, h, env.Priya, hwPath(po.ID), hwForm(po.Version, env.Arjun.ID, ""))
		want := fmt.Sprintf(hwStale, fmt.Sprintf("Draft %d", po.ID))
		if len(*calls) != 1 {
			t.Fatalf("renderHandover calls = %+v (response %d %q), want one", *calls, rec.Code, rec.Body.String())
		}
		if c := (*calls)[0]; c.status != http.StatusConflict || !c.stale || !slices.ContainsFunc(c.errs, func(e doctype.FieldError) bool { return e.Message == want }) {
			t.Errorf("renderHandover(%d, %v, %+v), want (409, stale, [%q])", c.status, c.stale, c.errs, want)
		}
		if owner, _ := hwOwner(t, env, po.ID); owner != env.Priya.ID {
			t.Errorf("owner = %d after a stale POST, want Priya", owner)
		}
	})

	t.Run("403 uses the error page", func(t *testing.T) {
		calls := hwRecordRender(t)
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		var want string
		if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
			info, err := env.Engine.Info(t.Context(), tx, purchase.DocType, po.ID)
			if err != nil {
				return err
			}
			rec := &auth.RecordRef{DocType: purchase.DocType, ID: po.ID, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
				ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
			if err := env.Auth.CanTx(t.Context(), tx, env.Arjun, purchase.DocType, auth.RightApprove, rec); err != nil {
				want = err.Error()
			}
			return nil
		}); err != nil || want == "" {
			t.Fatalf("setup: AUTH's 403 for Arjun: %q %v", want, err)
		}
		rec := hwPost(t, h, env.Arjun, hwPath(po.ID), hwForm(po.Version, env.Arjun.ID, ""))
		hwErrorPage(t, rec, calls, http.StatusForbidden, want)
		if owner, _ := hwOwner(t, env, po.ID); owner != env.Priya.ID {
			t.Errorf("owner = %d after a refused POST, want Priya", owner)
		}
	})

	t.Run("404 uses the error page", func(t *testing.T) {
		calls := hwRecordRender(t)
		for _, path := range []string{hwPath(999_999), "/modules/purchase/orders/abc/handover"} {
			rec := hwPost(t, h, env.Priya, path, hwForm(1, env.Arjun.ID, ""))
			hwErrorPage(t, rec, calls, http.StatusNotFound, hwMissing...)
		}
	})
}

// CD-289 (2), PO spec §21.2: requests have no hand-over. The route exists for
// purchase.Order only: a Request hand-over URL is a 404, and the order route
// given a request's id (no order has it) is the 404 error page, never a
// hand-over of the request.
func TestPurchaseWeb_RequestHandOverIs404(t *testing.T) {
	env := purchasetest.New(t)
	pr := purchasetest.NewRequest(t, env, purchasetest.StopAtDraft())
	h := hwHandler(env)
	calls := hwRecordRender(t)
	id := strconv.FormatInt(pr.ID, 10)

	for _, m := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequestWithContext(auth.WithPrincipal(t.Context(), env.Suresh), m, "/modules/purchase/requests/"+id+"/handover",
			strings.NewReader(hwForm(pr.Version, env.Deepak.ID, "").Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s /modules/purchase/requests/%s/handover = %d, want 404", m, id, rec.Code)
		}
	}

	if n := countOrders(t, env); n != 0 {
		t.Fatalf("setup: %d orders, want none so no order has the request's id", n)
	}
	rec := hwPost(t, h, env.Suresh, hwPath(pr.ID), hwForm(pr.Version, env.Deepak.ID, ""))
	hwErrorPage(t, rec, calls, http.StatusNotFound, hwMissing...)
	var owner types.UserID
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT created_by FROM purchase_request WHERE id = ?`, pr.ID).Scan(&owner); err != nil {
		t.Fatalf("request owner: %v", err)
	}
	if owner != env.Suresh.ID {
		t.Errorf("request owner = %d, want Suresh (%d) unchanged", owner, env.Suresh.ID)
	}
}

func countOrders(t *testing.T, env *purchasetest.Env) int {
	t.Helper()
	var n int
	if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM purchase_order`).Scan(&n); err != nil {
		t.Fatalf("count orders: %v", err)
	}
	return n
}

// Every web write posts version (CD-94, CD-146, CD-183): a hand-over POST
// with version missing or not a number is 400 with AUTH's bad-form text, and
// the PO keeps its owner and version.
func TestPurchaseWeb_HandOverPostBadVersion(t *testing.T) {
	env := purchasetest.New(t)
	h := hwHandler(env)
	for _, c := range []struct {
		name string
		form func() url.Values
	}{
		{"without version", func() url.Values {
			f := hwForm(0, env.Arjun.ID, "")
			f.Del("version")
			return f
		}},
		{"with version abc", func() url.Values {
			f := hwForm(0, env.Arjun.ID, "")
			f.Set("version", "abc")
			return f
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			calls := hwRecordRender(t)
			po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
			rec := hwPost(t, h, env.Priya, hwPath(po.ID), c.form())
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), auth.ErrBadForm.Msg) {
				t.Errorf("POST = %d %q, want 400 with %q", rec.Code, rec.Body.String(), auth.ErrBadForm.Msg)
			}
			if len(*calls) != 0 {
				t.Errorf("renderHandover called for a 400: %+v", *calls)
			}
			if owner, v := hwOwner(t, env, po.ID); owner != env.Priya.ID || v != po.Version {
				t.Errorf("after a bad-version POST: owner %d version %d, want Priya (%d), %d", owner, v, env.Priya.ID, po.Version)
			}
		})
	}
}
