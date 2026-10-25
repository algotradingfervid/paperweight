package files_test

import (
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// logoEnv is a filestest.Env whose Store uses LogoPolicy for "company", with
// one saved logo on company 1; it returns the env and the logo's id.
func logoEnv(t *testing.T) (*filestest.Env, int64) {
	t.Helper()
	env := filestest.New(t)
	s, err := files.New(files.Config{
		DataDir: env.DataDir, DB: env.DB, Auth: env.Auth, Info: env.Docs, Clock: env.Clock, Audit: env.Audit,
		Policies: map[string]files.Policy{"company": files.LogoPolicy},
	})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	env.Store = s
	kavya := env.User("kavya@vardhan.test")
	env.Auth.Allow(kavya.ID, "company", "read", "write")
	company := env.Doc("company", 1, "", "Vardhan Infra Projects Pvt Ltd", kavya.ID)
	id, err := env.Attach(kavya, company, "logo.png", filestest.PNG(1024))
	if err != nil {
		t.Fatalf("Attach(logo.png): %v", err)
	}
	return env, id
}

func TestCompanyLogo_OpenSignedOutRefused(t *testing.T) {
	env, id := logoEnv(t)
	rc, _, err := env.Store.Open(t.Context(), auth.Principal{}, id)
	if rc != nil {
		_ = rc.Close()
	}
	wantRefused(t, "Open(no principal, logo)", err, files.KindForbidden, http.StatusForbidden,
		"You don't have permission to read Company. Ask your administrator for a role that allows it.")
}

func TestCompanyLogo_OpenMissingCompany(t *testing.T) {
	env, id := logoEnv(t)
	// A Store over the same rows whose Info knows no company: the parent is gone.
	s, err := files.New(files.Config{
		DataDir: env.DataDir, DB: env.DB, Auth: env.Auth, Info: docfake.New(), Clock: env.Clock, Audit: env.Audit,
		Policies: map[string]files.Policy{"company": files.LogoPolicy},
	})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	rc, _, err := s.Open(t.Context(), env.User("deepak@vardhan.test"), id)
	if rc != nil {
		_ = rc.Close()
	}
	wantRefused(t, "Open(deepak, logo of a missing company)", err, files.KindNotFound, http.StatusNotFound, files.MsgNotFound)
}
