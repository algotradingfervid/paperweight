package main

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	adminEmail   = "admin@vardhan.test"
	deepakEmail  = "deepak@vardhan.test"
	testPassword = "Correct-Horse-42"
	newPassword  = "Temporary-Horse-77"
)

func TestApp_MigrateCreatesDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := run(context.Background(), []string{"migrate", "--data", dir}); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	conn, err := sql.Open("sqlite", filepath.Join(dir, "app.db")+"?mode=ro")
	if err != nil {
		t.Fatalf("open app.db: %v", err)
	}
	defer func() { _ = conn.Close() }()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = 'app_meta'`).Scan(&n); err != nil {
		t.Fatalf("query app.db: %v", err)
	}
	if n != 1 {
		t.Fatalf("app.db has no app_meta table")
	}
}

func TestApp_HealthzOK(t *testing.T) {
	bound := make(chan string, 1)
	listening = func(addr string) { bound <- addr }
	t.Cleanup(func() { listening = nil })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- run(ctx, []string{"serve", "--data", t.TempDir(), "--addr", "127.0.0.1:0"}) }()

	var addr string
	select {
	case addr = <-bound:
	case err := <-done:
		t.Fatalf("serve stopped before listening: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not start listening within 5 s")
	}

	client := &http.Client{Timeout: time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Errorf("GET /healthz = %d %q, want 200 \"ok\"", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("serve returned %v after cancel, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop within 2 s of cancel")
	}
}

func TestApp_UnknownCommand(t *testing.T) {
	err := run(context.Background(), []string{"frobnicate"})
	want := "unknown command \"frobnicate\". Run `app help` to see the commands."
	if err == nil || err.Error() != want {
		t.Fatalf("run(frobnicate) = %v, want %q", err, want)
	}
}

func TestApp_UnexpectedArgument(t *testing.T) {
	for _, args := range [][]string{
		{"migrate", "--data", t.TempDir(), "extra"},
		{"serve", "--data", t.TempDir(), "extra"},
	} {
		err := run(context.Background(), args)
		want := "unexpected argument \"extra\". Run `app help` to see the commands."
		if err == nil || err.Error() != want {
			t.Errorf("run(%q) = %v, want %q", args, err, want)
		}
	}
}

// feedStdin makes text what the next command reads as its password prompts.
func feedStdin(t *testing.T, text string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stdin")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = old
		_ = f.Close()
	})
}

// adminCreate runs `app admin create` for Anil.
func adminCreate(t *testing.T, dir string) error {
	t.Helper()
	feedStdin(t, testPassword+"\n"+testPassword+"\n")
	return run(context.Background(), []string{"admin", "create", "--data", dir, "--email", adminEmail, "--name", "Anil Kapoor"})
}

// world opens the data dir with the real auth.Auth, as a second process would.
type world struct {
	d *db.DB
	a *auth.Auth
}

func open(t *testing.T, dir string) *world {
	t.Helper()
	d, err := db.Open(filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatalf("open app.db: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return &world{d: d, a: auth.New(auth.Deps{DB: d, Clock: clock.Real{}, Audit: audit.Default})}
}

// principal loads a user created by a command.
func (w *world) principal(t *testing.T, email string) auth.Principal {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := w.d.SQL().QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id); err != nil {
		t.Fatalf("find %s: %v", email, err)
	}
	p, _, err := w.a.LoadPrincipal(ctx, auth.UserID(id))
	if err != nil {
		t.Fatalf("LoadPrincipal: %v", err)
	}
	return p
}

// addDeepak creates Deepak (Employee) with a stored session and returns his id.
func (w *world) addDeepak(t *testing.T) auth.UserID {
	t.Helper()
	ctx := context.Background()
	id, _, err := w.a.CreateUser(ctx, w.principal(t, adminEmail), auth.NewUser{
		Email: deepakEmail, Name: "Deepak Nair", Password: testPassword, Roles: []string{"Employee"},
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	w.addSession(t, id)
	return id
}

func (w *world) addSession(t *testing.T, id auth.UserID) {
	t.Helper()
	w.exec(t, `INSERT INTO sessions (token_hash, user_id, data, created_at, last_seen_at) VALUES (?, ?, x'00', '2026-10-24T05:00:00.000Z', '2026-10-24T05:00:00.000Z')`,
		"hash-of-"+string(rune('a'+id)), id)
}

func (w *world) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := w.d.SQL().ExecContext(context.Background(), q, args...); err != nil {
		t.Fatalf("exec %q: %v", q, err)
	}
}

func (w *world) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := w.d.SQL().QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	return n
}

// AUTH-049, CD-66.
func TestApp_AdminCreateOnlyOnce(t *testing.T) {
	dir := t.TempDir() // a data dir nothing has migrated yet
	if err := adminCreate(t, dir); err != nil {
		t.Fatalf("first admin create: %v", err)
	}
	const want = "Users already exist. Sign in as an administrator to add more users."
	err := adminCreate(t, dir)
	if err == nil || err.Error() != want {
		t.Fatalf("second admin create = %v, want %q", err, want)
	}
	if !errors.Is(err, auth.ErrUsersExist) {
		t.Errorf("second admin create error = %v, want auth.ErrUsersExist", err)
	}
	if n := open(t, dir).count(t, `SELECT count(*) FROM users`); n != 1 {
		t.Errorf("users = %d, want 1", n)
	}
}

// CD-18, CD-91, AUTH-007.
func TestApp_UserDisableCommand(t *testing.T) {
	dir := t.TempDir()
	if err := adminCreate(t, dir); err != nil {
		t.Fatalf("admin create: %v", err)
	}
	w := open(t, dir)
	id := w.addDeepak(t)
	ctx := context.Background()

	if err := run(ctx, []string{"user", "disable", "--data", dir, "--email", deepakEmail}); err != nil {
		t.Fatalf("user disable: %v", err)
	}
	_, err := w.a.SignIn(ctx, auth.SignInInput{Email: deepakEmail, Password: testPassword, IP: "127.0.0.1"})
	const disabled = "Your account is disabled. Ask your administrator to enable it."
	if err == nil || err.Error() != disabled {
		t.Errorf("sign-in after disable = %v, want %q", err, disabled)
	}
	if n := w.count(t, `SELECT count(*) FROM sessions WHERE user_id = ?`, id); n != 0 {
		t.Errorf("Deepak's sessions after disable = %d, want 0", n)
	}

	err = run(ctx, []string{"user", "disable", "--data", dir, "--email", "nobody@vardhan.test"})
	if err == nil || !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("user disable of an unknown email = %v, want auth.ErrNotFound", err)
	}
}

// resetPassword runs `app admin reset-password` with the typed passwords.
func resetPassword(t *testing.T, dir, email, first, second string) error {
	t.Helper()
	feedStdin(t, first+"\n"+second+"\n")
	return run(context.Background(), []string{"admin", "reset-password", "--data", dir, "--email", email})
}

// CD-228: a sole admin who forgets a password recovers without SQL.
func TestApp_AdminResetPassword(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := adminCreate(t, dir); err != nil {
		t.Fatalf("admin create: %v", err)
	}
	w := open(t, dir)
	admin := w.principal(t, adminEmail)
	deepak := w.addDeepak(t)
	w.addSession(t, admin.ID)
	w.exec(t, `INSERT INTO login_attempts (key, count, first_failure_at, expires_at) VALUES (?, 99, '2026-10-24T05:00:00.000Z', '2999-01-01T00:00:00.000Z')`, auth.AccountKey(adminEmail))
	signIn := func(email, pw string) (auth.SignInResult, error) {
		return w.a.SignIn(ctx, auth.SignInInput{Email: email, Password: pw, IP: "127.0.0.1"})
	}
	unchanged := func(t *testing.T) {
		t.Helper()
		if n := w.count(t, `SELECT count(*) FROM sessions WHERE user_id = ?`, admin.ID); n != 1 {
			t.Errorf("admin sessions after a refusal = %d, want 1 (nothing changed)", n)
		}
		if n := w.count(t, `SELECT count(*) FROM users WHERE id = ? AND must_change_password = 1`, admin.ID); n != 0 {
			t.Error("must-change is set after a refusal")
		}
	}

	t.Run("confirmation_differs", func(t *testing.T) {
		err := resetPassword(t, dir, adminEmail, newPassword, "Temporary-Horse-78")
		if !errors.Is(err, auth.ErrPasswordMismatch) {
			t.Errorf("reset = %v, want auth.ErrPasswordMismatch", err)
		}
		unchanged(t)
	})
	t.Run("policy", func(t *testing.T) {
		err := resetPassword(t, dir, adminEmail, "short", "short")
		if !errors.Is(err, auth.ErrPasswordTooShort) {
			t.Errorf("reset = %v, want auth.ErrPasswordTooShort", err)
		}
		unchanged(t)
	})
	t.Run("not_an_administrator", func(t *testing.T) {
		if err := resetPassword(t, dir, deepakEmail, newPassword, newPassword); err == nil {
			t.Fatal("reset of a non-administrator succeeded, want a refusal")
		}
		if n := w.count(t, `SELECT count(*) FROM sessions WHERE user_id = ?`, deepak); n != 1 {
			t.Errorf("Deepak's sessions = %d, want 1 (nothing changed)", n)
		}
		if n := w.count(t, `SELECT count(*) FROM users WHERE id = ? AND must_change_password = 1`, deepak); n != 0 {
			t.Error("must-change is set on Deepak after a refusal")
		}
	})
	t.Run("unknown_email", func(t *testing.T) {
		if err := resetPassword(t, dir, "nobody@vardhan.test", newPassword, newPassword); !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("reset = %v, want auth.ErrNotFound", err)
		}
	})
	t.Run("resets", func(t *testing.T) {
		if err := resetPassword(t, dir, adminEmail, newPassword, newPassword); err != nil {
			t.Fatalf("reset: %v", err)
		}
		if n := w.count(t, `SELECT count(*) FROM sessions WHERE user_id = ?`, admin.ID); n != 0 {
			t.Errorf("admin sessions = %d, want 0", n)
		}
		if n := w.count(t, `SELECT count(*) FROM login_attempts WHERE key = ?`, auth.AccountKey(adminEmail)); n != 0 {
			t.Errorf("lockout rows = %d, want 0", n)
		}
		if n := w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type = 'User' AND doc_id = ? AND user_id = 0 AND action <> 'create'`, admin.ID); n != 1 {
			t.Errorf("audit entries by the system = %d, want 1 (By 0)", n)
		}
		res, err := signIn(adminEmail, newPassword)
		if err != nil {
			t.Fatalf("sign-in with the temporary password: %v", err)
		}
		if !res.MustChangePassword {
			t.Error("MustChangePassword = false, want true")
		}
		if _, err := signIn(adminEmail, testPassword); !errors.Is(err, auth.ErrLoginInvalid) {
			t.Errorf("sign-in with the old password = %v, want auth.ErrLoginInvalid", err)
		}
	})
}

// FILE hand-off.
func TestApp_FilesGC(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := run(ctx, []string{"files", "gc", "--data", dir}); err != nil {
		t.Fatalf("files gc on a new data dir: %v", err)
	}
	blob := func(c string) string {
		t.Helper()
		path, err := files.BlobPath(dir, strings.Repeat(c, 64))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("orphan"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	old, fresh := blob("a"), blob("b")
	when := time.Now().Add(-2 * files.GCGrace)
	if err := os.Chtimes(old, when, when); err != nil {
		t.Fatal(err)
	}
	if err := run(ctx, []string{"files", "gc", "--data", dir}); err != nil {
		t.Fatalf("files gc: %v", err)
	}
	if _, err := os.Stat(old); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an orphan past files.GCGrace still exists (%v)", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("an orphan inside the grace period was removed: %v", err)
	}
}

// FILE hand-off: the exit code comes from WriteCheckReport.
func TestApp_FilesCheck(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	if err := run(ctx, []string{"files", "check", "--data", dir}); err != nil {
		t.Fatalf("files check on a clean data dir: %v", err)
	}
	open(t, dir).exec(t, `INSERT INTO attachment (parent_doctype, parent_id, file_name, size, content_type, sha256, uploaded_by, uploaded_at)
		VALUES ('fixture.Order', 1, 'gone.pdf', 10, 'application/pdf', ?, 1, '2026-10-24T05:00:00.000Z')`, strings.Repeat("c", 64))
	if err := run(ctx, []string{"files", "check", "--data", dir}); err == nil {
		t.Error("files check with a missing blob = nil, want a non-zero exit")
	}
}

// startServe runs `app serve` until the test ends and returns its address.
func startServe(t *testing.T, args ...string) (addr string, stop func()) {
	t.Helper()
	bound := make(chan string, 1)
	listening = func(a string) { bound <- a }
	t.Cleanup(func() { listening = nil })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, append([]string{"serve", "--data", t.TempDir(), "--addr", "127.0.0.1:0"}, args...))
	}()
	select {
	case addr = <-bound:
	case err := <-done:
		cancel()
		t.Fatalf("serve stopped before listening: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("serve did not start listening within 5 s")
	}
	stopped := false
	stop = func() {
		if stopped {
			return
		}
		stopped = true
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("serve returned %v after cancel, want nil", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("serve did not stop within 5 s of cancel")
		}
	}
	t.Cleanup(stop)
	return addr, stop
}

func getNoRedirect(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{
		Timeout:       2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp
}

// serve runs the composition root: a fresh install goes to /setup.
func TestApp_ServeRunsTheApp(t *testing.T) {
	addr, _ := startServe(t)
	resp := getNoRedirect(t, "http://"+addr+"/")
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/setup" {
		t.Errorf("GET / = %d to %q, want 303 to /setup", resp.StatusCode, resp.Header.Get("Location"))
	}
}

// serve's new flags reach app.Config.
func TestApp_ServeFlags(t *testing.T) {
	addr, _ := startServe(t, "--secure", "--trust-proxy", "--trusted-origin", "https://a.vardhan.test", "--trusted-origin", "https://b.vardhan.test")
	if resp := getNoRedirect(t, "http://"+addr+"/setup"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("GET /setup with --secure = %d, want 404", resp.StatusCode)
	}
	if got := getNoRedirect(t, "http://"+addr+"/healthz").Header.Get("Strict-Transport-Security"); got == "" {
		t.Error("/healthz with --secure has no HSTS header")
	}
	for _, origin := range []string{"https://a.vardhan.test", "https://b.vardhan.test"} {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+addr+"/signin", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", origin)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST /signin: %v", err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if !strings.Contains(string(body), auth.ErrCSRF.Msg) {
			t.Errorf("POST from %s = %d %q, want past the cross-origin check to %q", origin, resp.StatusCode, body, auth.ErrCSRF.Msg)
		}
	}
}

// Spec §7: logs are JSON on stdout.
func TestApp_ServeLogsJSONToStdout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stdout")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = f
	t.Cleanup(func() {
		os.Stdout = old
		_ = f.Close()
	})

	addr, stop := startServe(t)
	getNoRedirect(t, "http://"+addr+"/healthz")
	stop()

	logs, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	requests := 0
	sc := bufio.NewScanner(strings.NewReader(string(logs)))
	for sc.Scan() {
		var line map[string]any
		if err := json.Unmarshal(sc.Bytes(), &line); err != nil {
			t.Errorf("stdout line %q isn't JSON: %v", sc.Text(), err)
			continue
		}
		if line["msg"] == "request" {
			requests++
		}
	}
	if requests == 0 {
		t.Errorf("stdout = %q, want a JSON request line", logs)
	}
}
