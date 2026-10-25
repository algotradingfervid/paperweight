package files_test

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/files/web"
)

const (
	msgLogoType = "Logo must be a PNG or JPG image."
	msgLogoSize = "Logo must be 1 MB or smaller."
)

// FILE-048
func TestCompanyLogo_Rules(t *testing.T) {
	want := files.Policy{
		MaxBytes:        1_048_576,
		MaxCount:        20,
		Exts:            []string{".png", ".jpg", ".jpeg"},
		ReadAnySignedIn: true,
		IgnoreState:     true,
		TypeMessage:     msgLogoType,
		SizeMessage:     msgLogoSize,
	}
	if !reflect.DeepEqual(files.LogoPolicy, want) {
		t.Errorf("LogoPolicy = %+v, want %+v", files.LogoPolicy, want)
	}

	env := filestest.New(t)
	s, err := files.New(files.Config{
		DataDir: env.DataDir, DB: env.DB, Auth: env.Auth, Info: env.Docs, Clock: env.Clock, Audit: env.Audit,
		Policies: map[string]files.Policy{"company": files.LogoPolicy},
	})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	env.Store = s // env.Attach now goes through the Store with the logo policy

	kavya := env.User("kavya@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(kavya.ID, "company", "read", "write")
	company := env.Doc("company", 1, "", "Vardhan Infra Projects Pvt Ltd", kavya.ID)

	_, err = env.Attach(kavya, company, "logo.svg", filestest.SVG())
	wantRefused(t, "Attach(logo.svg)", err, files.KindType, http.StatusUnprocessableEntity, msgLogoType)

	_, err = env.Attach(kavya, company, "logo.png", filestest.PNG(1_572_864))
	wantRefused(t, "Attach(logo.png, 1.5 MB)", err, files.KindTooLarge, http.StatusRequestEntityTooLarge, msgLogoSize)

	if rows := env.Rows(company); len(rows) != 0 {
		t.Fatalf("Rows(company 1) after refusals = %d rows, want 0: %+v", len(rows), rows)
	}

	logo := filestest.PNG(184_320)
	id, err := env.Attach(kavya, company, "logo.png", logo)
	if err != nil {
		t.Fatalf("Attach(logo.png, 180 KB) error = %v, want saved", err)
	}
	rows := env.Rows(company)
	if len(rows) != 1 || rows[0].ID != id || rows[0].Name != "logo.png" || rows[0].Size != 184_320 {
		t.Fatalf("Rows(company 1) = %+v, want one row id %d logo.png of 184320 bytes", rows, id)
	}

	// Deepak has no right on company at all, so only ReadAnySignedIn lets him read it.
	ref := company
	if err := env.Auth.Can(t.Context(), deepak, "company", "read", &ref); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("precondition: Can(deepak, company, read) = %v, want auth.ErrForbidden", err)
	}
	rc, m, err := env.Store.Open(t.Context(), deepak, id)
	if err != nil {
		t.Fatalf("Open(deepak, logo %d) error = %v, want ok (any signed-in user reads the logo)", id, err)
	}
	got, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatalf("Open(deepak, logo %d): read: %v", id, err)
	}
	if !bytes.Equal(got, logo) {
		t.Errorf("Open(deepak, logo %d) = %d bytes, want the %d stored bytes", id, len(got), len(logo))
	}
	if m.ID != id || m.Name != "logo.png" || m.MIME != "image/png" {
		t.Errorf("Open(deepak, logo %d) Meta = %+v, want id %d logo.png image/png", id, m, id)
	}

	h := web.Routes(web.Config{Store: env.Store, DB: env.DB})
	target := "/files/" + strconv.FormatInt(id, 10)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	if rec.Code != http.StatusSeeOther {
		t.Errorf("signed-out GET %s: status = %d, want 303 (body %q)", target, rec.Code, rec.Body.String())
	}
	if got, want := rec.Header().Get("Location"), "/signin?next=%2Ffiles%2F"+strconv.FormatInt(id, 10); got != want {
		t.Errorf("signed-out GET %s: Location = %q, want %q", target, got, want)
	}
	if bytes.Contains(rec.Body.Bytes(), logo[:64]) {
		t.Errorf("signed-out GET %s: body contains the logo's bytes", target)
	}

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	h.ServeHTTP(rec, req.WithContext(auth.WithPrincipal(req.Context(), deepak)))
	if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), logo) {
		t.Errorf("signed-in (deepak) GET %s: status = %d, body %d bytes; want 200 with the %d stored bytes", target, rec.Code, rec.Body.Len(), len(logo))
	}
}
