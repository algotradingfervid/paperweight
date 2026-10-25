package authweb_test

import (
	"net/http"
	"net/url"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/auth/web/webtest"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
)

// t11EmptyWorld is a World over a migrated database with no users (the
// plan's authtest.NewEmptyWorld, kept local to this test file).
func t11EmptyWorld(t *testing.T) *authtest.World {
	t.Helper()
	authtest.RegisterDocTypes()
	w := &authtest.World{
		DB: testkit.NewDB(t), Clock: clock.NewFixed(authtest.T0), Dir: authfake.NewDirectory(), Audit: auditfake.New(),
		Refs: &authtest.Refs{}, Info: &authtest.Info{}, Users: map[string]auth.UserID{},
		Projects: map[string]int64{}, Sites: map[string]int64{}, Employees: map[string]int64{},
	}
	w.Auth = auth.New(auth.Deps{
		DB: w.DB, Clock: w.Clock, Directory: w.Dir, Audit: w.Audit, Refs: w.Refs, Info: w.Info,
	})
	return w
}

// AUTH-050
func TestBootstrap_SetupOnceRaceFree(t *testing.T) {
	w := t11EmptyWorld(t)
	h := webtest.New(t, w)

	if n := t11Int(t, w, `SELECT count(*) FROM users`); n != 0 {
		t.Fatalf("users before setup = %d, want 0", n)
	}

	// Form fields: email, name, password, csrf_token.
	people := []struct{ email, name string }{
		{"admin@vardhan.test", "Anil Kapoor"},
		{"meena@vardhan.test", "Meena Iyer"},
	}
	clients := make([]*webtest.Client, len(people))
	forms := make([]url.Values, len(people))
	for i, p := range people {
		clients[i] = h.Client()
		page := clients[i].Get("/setup")
		wantStatus(t, "GET /setup ("+p.email+")", page, http.StatusOK)
		forms[i] = url.Values{
			"email":      {p.email},
			"name":       {p.name},
			"password":   {t10Password},
			"csrf_token": {t11FormCSRF(t, "GET /setup", page)},
		}
	}

	results := make([]webtest.Response, len(people))
	var ready, done sync.WaitGroup
	start := make(chan struct{})
	for i := range people {
		ready.Add(1)
		done.Add(1)
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			results[i] = clients[i].NoCSRF().Post("/setup", forms[i])
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()

	statuses := []int{results[0].Status, results[1].Status}
	sort.Ints(statuses)
	if !reflect.DeepEqual(statuses, []int{http.StatusSeeOther, http.StatusConflict}) {
		t.Fatalf("POST /setup statuses = %v, want [303 409] (bodies %q, %q)", statuses, results[0].Body, results[1].Body)
	}
	winner := 0
	if results[1].Status == http.StatusSeeOther {
		winner = 1
	}
	won, lost := results[winner], results[1-winner]
	if got := won.Header.Get("Location"); got != "/signin" {
		t.Errorf("winning POST /setup Location = %q, want /signin", got)
	}
	if !strings.Contains(lost.Body, "Setup is already complete. Sign in instead.") {
		t.Errorf("losing POST /setup body %q, want %q", lost.Body, "Setup is already complete. Sign in instead.")
	}

	if n := t11Int(t, w, `SELECT count(*) FROM users`); n != 1 {
		t.Fatalf("users after setup = %d, want 1", n)
	}
	if n := t11Int(t, w, `SELECT count(*) FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		WHERE u.email = ? AND u.enabled = 1 AND r.name = 'Administrator'`, people[winner].email); n != 1 {
		t.Errorf("winner %s: enabled Administrator rows = %d, want 1", people[winner].email, n)
	}

	again := h.Client().Get("/setup")
	wantStatus(t, "GET /setup after setup", again, http.StatusNotFound)
	if !strings.Contains(again.Body, auth.MsgNotFound) {
		t.Errorf("GET /setup after setup: body %q, want %q", again.Body, auth.MsgNotFound)
	}
}
