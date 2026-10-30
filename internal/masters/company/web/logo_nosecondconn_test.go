package companyweb_test

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/testkit"
)

// CD-213, CD-209: the logo routes and the company save as a non-Administrator,
// on the real auth.Service and the real files.Store, with the pool cut to one
// connection. TestLogo_ReplaceTakesNoSecondConnectionAsAdministrator keeps the
// stall of the queued writers; these cover what an Administrator skips.

// serveOnOneConn serves req as p on the pool of one. The request runs on
// NoSecondConn's ctx, so a stall ends with the test.
func (lw *logoWorld) serveOnOneConn(t *testing.T, p auth.Principal, req *http.Request) webtest.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	err := testkit.NoSecondConn(t, lw.h.Env.DB, func(ctx context.Context) error {
		lw.h.Router.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(ctx, p)))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(rec.Body.String()))
	if err != nil {
		t.Fatal(err)
	}
	return webtest.Response{Status: rec.Code, Location: rec.Header().Get("Location"), Header: rec.Header(), Body: rec.Body.String(), Doc: doc}
}

// logoRequest is the upload form's POST of one file.
func logoRequest(t *testing.T, target, name string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	w, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

// formRequest is a form-encoded POST.
func formRequest(target string, form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// nonAdmin returns Kavya after checking she is what these tests need.
func nonAdmin(t *testing.T, lw *logoWorld) auth.Principal {
	t.Helper()
	kavya := lw.h.Env.Kavya
	if slices.Contains(kavya.Roles, auth.RoleAdministrator) {
		t.Fatal("Kavya is an Administrator: Can would return before reading anything")
	}
	if _, ok := lw.h.Env.Kit.Auth.(*auth.Auth); !ok {
		t.Fatalf("Kit.Auth is %T, want the real *auth.Auth", lw.h.Env.Kit.Auth)
	}
	return kavya
}

// wantStoredBytes fails unless the only logo has this name and content, read
// back from the data directory through the store.
func (lw *logoWorld) wantStoredBytes(t *testing.T, name string, want []byte) {
	t.Helper()
	got := lw.logos(t)
	if len(got) != 1 || got[0].Name != name {
		t.Fatalf("logos = %+v; want only %s", got, name)
	}
	rc, _, err := lw.store.Open(context.Background(), lw.h.Env.Kavya, got[0].ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = rc.Close() }()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("%s holds %d bytes, want the %d uploaded", name, buf.Len(), len(want))
	}
}

// Attach on the first upload; Remove and Attach in one write on a replace;
// Remove alone on the remove route.
func TestLogo_ReplaceAndRemoveOnOneConnectionAsNonAdmin(t *testing.T) {
	lw := seeded(t)
	kavya := nonAdmin(t, lw)
	first, second := filestest.PNG(1024), filestest.PNG(2048)
	action := uploadAction(t, lw.h.Get(t, kavya, logoPath))

	webtest.WantRedirect(t, lw.serveOnOneConn(t, kavya, logoRequest(t, action, "first.png", first)), logoPath)
	lw.wantStoredBytes(t, "first.png", first)
	old := lw.logos(t)[0]

	webtest.WantRedirect(t, lw.serveOnOneConn(t, kavya, logoRequest(t, action, "second.png", second)), logoPath)
	lw.wantStoredBytes(t, "second.png", second)
	if got := lw.logos(t)[0]; got.ID == old.ID {
		t.Errorf("logo id = %d after the replace; want a new file", got.ID)
	}
	if got, want := lw.logoURL(t, kavya), fileURL(lw.logos(t)[0].ID); got != want {
		t.Errorf("LogoURL = %q; want %q", got, want)
	}

	remove := removeAction(t, lw.h.Get(t, kavya, logoPath))
	webtest.WantRedirect(t, lw.serveOnOneConn(t, kavya, formRequest(remove, url.Values{})), logoPath)
	if got := lw.logos(t); len(got) != 0 {
		t.Fatalf("logos after remove = %+v; want none", got)
	}
	if got := lw.logoURL(t, kavya); got != "" {
		t.Errorf("LogoURL after remove = %q; want empty", got)
	}
}

// A file the policy refuses stops inside the write and rolls the replace back.
func TestLogo_RefusedReplaceOnOneConnectionAsNonAdmin(t *testing.T) {
	lw := seeded(t)
	kavya := nonAdmin(t, lw)
	first := filestest.PNG(1024)
	action := uploadAction(t, lw.h.Get(t, kavya, logoPath))
	webtest.WantRedirect(t, lw.serveOnOneConn(t, kavya, logoRequest(t, action, "first.png", first)), logoPath)

	resp := lw.serveOnOneConn(t, kavya, logoRequest(t, action, "logo.svg", filestest.SVG()))
	webtest.WantStatus(t, resp, http.StatusUnprocessableEntity)
	webtest.WantText(t, resp, msgLogoType)
	lw.wantStoredBytes(t, "first.png", first)
}

// SaveCompanyTx through the company screen's save.
func TestCompany_SaveOnOneConnectionAsNonAdmin(t *testing.T) {
	lw := seeded(t)
	kavya := nonAdmin(t, lw)
	before := lw.company(t)
	form := url.Values{"version": {strconv.Itoa(before.Version)}, "name": {newName}, "pan": {pan}}
	webtest.WantRedirect(t, lw.serveOnOneConn(t, kavya, formRequest("/m/company/1", form)), "/m/company/1")
	if got := lw.company(t); got.LegalName != newName || got.Version != before.Version+1 {
		t.Errorf("company = %+v; want %q at version %d", got, newName, before.Version+1)
	}
}
