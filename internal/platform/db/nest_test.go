//go:build nestcheck

package db_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/platform/db"
)

const nestChildEnv = "DB_NEST_CHILD"

func noTx(*db.Tx) error { return nil }

// nestReadInWrite opens a Read inside a Write and swallows whatever comes back
// and any panic, so only the exit can stop it.
func nestReadInWrite(ctx context.Context, d *db.DB) error {
	defer func() { _ = recover() }()
	_ = d.Write(ctx, func(*db.Tx) error {
		_ = d.Read(ctx, noTx)
		return nil
	})
	return nil
}

// sequential opens one transaction after the other.
func sequential(ctx context.Context, d *db.DB) error {
	return errors.Join(d.Read(ctx, noTx), d.Write(ctx, noTx))
}

// parallelReads holds two transactions open at once, one per goroutine: each
// stays in its Read until the other has entered its own.
func parallelReads(ctx context.Context, d *db.DB) error {
	var entered, done sync.WaitGroup
	entered.Add(2)
	errs := make([]error, 2)
	for i := range errs {
		done.Go(func() {
			errs[i] = d.Read(ctx, func(*db.Tx) error {
				entered.Done()
				entered.Wait()
				return nil
			})
		})
	}
	done.Wait()
	return errors.Join(errs...)
}

// TestNestChild is the process TestNestGate runs; on its own it skips.
func TestNestChild(t *testing.T) {
	mode := os.Getenv(nestChildEnv)
	if mode == "" {
		t.Skip("runs only as TestNestGate's child")
	}
	fn := map[string]func(context.Context, *db.DB) error{
		"nested":     nestReadInWrite,
		"sequential": sequential,
		"parallel":   parallelReads,
	}[mode]
	if err := fn(t.Context(), openDB(t)); err != nil {
		t.Fatal(err)
	}
}

// CD-213: a transaction opened inside another on one goroutine ends the whole
// test binary with exit 2 and a stack; transactions that don't nest pass.
func TestNestGate(t *testing.T) {
	for _, tc := range []struct {
		mode string
		exit int
		want []string
	}{
		{"nested", 2, []string{"db: a transaction was opened inside another on this goroutine", "db_test.nestReadInWrite"}},
		{"sequential", 0, nil},
		{"parallel", 0, nil},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestNestChild$")
			cmd.Env = append(os.Environ(), nestChildEnv+"="+tc.mode)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			err := cmd.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatalf("run child: %v", err)
			}
			if got := cmd.ProcessState.ExitCode(); got != tc.exit {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", got, tc.exit, stderr.String())
			}
			for _, w := range tc.want {
				if !strings.Contains(stderr.String(), w) {
					t.Errorf("stderr lacks %q:\n%s", w, stderr.String())
				}
			}
		})
	}
}
