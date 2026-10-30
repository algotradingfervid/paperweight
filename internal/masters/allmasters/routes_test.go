package allmasters_test

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/uom"
)

// New fills every service, and the directory over the same services, which
// WP3 hands to AUTH and APR.
func TestServices_NewFillsEveryService(t *testing.T) {
	var s *allmasters.Services
	webtest.New(t, func(k mstkit.Env) func(chi.Router) {
		s = allmasters.New(k)
		return func(chi.Router) {}
	})
	if s.Project == nil || s.Site == nil || s.Employee == nil || s.Directory == nil {
		t.Fatalf("allmasters.New left a service nil: %+v", s)
	}
	if s.Directory.Employees != s.Employee || s.Directory.Projects != s.Project || s.Directory.Sites != s.Site {
		t.Errorf("Directory = %+v; want the services of Services", s.Directory)
	}
}

// The import wizard is mounted through Routes on the session manager WP3
// passes: its first step's choice is still there on the next page.
func TestServices_RoutesMountTheImportWizard(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	webtest.WantStatus(t, h.Get(t, h.Env.Kavya, "/m/import/choose"), http.StatusOK)

	resp := h.Post(t, h.Env.Kavya, "/m/import/choose", url.Values{"doctype": {uom.DocType}})
	webtest.WantRedirect(t, resp, "/m/import/upload")
	page := h.Follow(t, h.Env.Kavya, resp)
	webtest.WantStatus(t, page, http.StatusOK)
	if !strings.Contains(page.Body, "doctype="+uom.DocType) {
		t.Errorf("upload step doesn't name the chosen master: %s", webtest.Squash(page.Text()))
	}
}

// Routes registers the real importers (CD-210), so the wizard's check step
// runs a real master's RowFunc instead of the no-importer banner. Tasks 20 and
// 21 aren't in yet: their stub refuses with a 500, and Task 21 turns this into
// a clean file importing a real UoM.
func TestServices_RoutesReachARealImporter(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) { allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	kavya := h.Env.Kavya
	var cookies []*http.Cookie
	send := func(req *http.Request) *httptest.ResponseRecorder {
		for _, c := range cookies {
			req.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		h.Router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), kavya)))
		if set := (&http.Response{Header: rec.Header()}).Cookies(); len(set) > 0 {
			cookies = set
		}
		return rec
	}

	choose := httptest.NewRequest(http.MethodPost, "/m/import/choose", strings.NewReader(url.Values{"doctype": {uom.DocType}}.Encode()))
	choose.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if rec := send(choose); rec.Code != http.StatusSeeOther {
		t.Fatalf("choose = %d; want 303", rec.Code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "uom.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("Code,Name,Whole Number,UQC,Disabled\r\nZED,Zed,No,NOS,No\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest(http.MethodPost, "/m/import/upload", &body)
	upload.Header.Set("Content-Type", mw.FormDataContentType())
	if rec := send(upload); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/m/import/check" {
		t.Fatalf("upload = %d to %q; want 303 to the check step", rec.Code, rec.Header().Get("Location"))
	}

	rec := send(httptest.NewRequest(http.MethodGet, "/m/import/check", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("check = %d; want 500 from the UoM stub", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Import isn't ready") {
		t.Error("check shows the no-importer banner; Routes didn't register the importers")
	}
}

// Each built master's screens are reachable through Routes.
func TestServices_RoutesMountTheMasters(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) { allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	for _, target := range []string{"/m/uom", "/m/tax-code", "/m/expense-type"} {
		webtest.WantStatus(t, h.Get(t, h.Env.Kavya, target), http.StatusOK)
	}
}
