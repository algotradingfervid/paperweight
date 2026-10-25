package main

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
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
