package allmasters_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

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

// Each built master's screens are reachable through Routes.
func TestServices_RoutesMountTheMasters(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) { allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm}) }
	})
	for _, target := range []string{"/m/uom", "/m/tax-code", "/m/expense-type"} {
		webtest.WantStatus(t, h.Get(t, h.Env.Kavya, target), http.StatusOK)
	}
}
