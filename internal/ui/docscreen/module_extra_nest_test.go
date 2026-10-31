//go:build nestcheck

package docscreen_test

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
)

// m04NestEnv names the mode TestModule_NoNestedTxChild runs in.
const m04NestEnv = "DOCSCREEN_M04_NEST_CHILD"

// m04NestCalls counts the calls of each reading hook.
type m04NestCalls map[string]int

// m04ReadingModule's hooks each take a connection of their own, through
// Ctx.Deps.DB, as a hook that reads is allowed to (hooks spec §5 rule 1).
func m04ReadingModule(t *testing.T, calls m04NestCalls) docscreen.Module {
	read := func(c docscreen.Ctx, hook string) {
		calls[hook]++
		if err := c.Deps.DB.Read(c.Req.Context(), func(*db.Tx) error { return nil }); err != nil {
			t.Errorf("%s hook's read: %v", hook, err)
		}
	}
	return docscreen.Module{
		Hide: func(c docscreen.Ctx, _ doctype.Field) bool { read(c, "Hide"); return false },
		LineCard: func(c docscreen.Ctx, l docscreen.LineView) components.LineCard {
			read(c, "LineCard")
			return l.Card
		},
		Check: func(c docscreen.Ctx) (docscreen.CheckExtra, error) {
			read(c, "Check")
			return docscreen.CheckExtra{}, nil
		},
		Page: func(c docscreen.Ctx) (docscreen.PageExtra, error) {
			read(c, "Page")
			return docscreen.PageExtra{}, nil
		},
		Reason: func(c docscreen.Ctx, _ doctype.Action) (docscreen.ReasonExtra, error) {
			read(c, "Reason")
			return docscreen.ReasonExtra{}, nil
		},
	}
}

// TestModule_NoNestedTxChild is the process TestModule_NoNestedTx runs; on
// its own it skips. "inside" runs a reading hook inside a read transaction,
// the mistake the gate exists for; "docscreen" serves the check page, the
// document page and the close page with every hook reading, from where
// docscreen calls them.
func TestModule_NoNestedTxChild(t *testing.T) {
	mode := os.Getenv(m04NestEnv)
	if mode == "" {
		t.Skip("runs only as TestModule_NoNestedTx's child")
	}
	e := newHookedEnv(t)
	calls := m04NestCalls{}
	m := m04ReadingModule(t, calls)
	switch mode {
	case "inside":
		req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if err != nil {
			t.Fatal(err)
		}
		c := docscreen.Ctx{Req: req, Deps: e.d}
		_ = e.fx.DB.Read(t.Context(), func(*db.Tx) error {
			_, _ = m.Page(c)
			return nil
		})
		t.Fatal("a hook read inside a read transaction and the gate let it through")
	case "docscreen":
		draft, err := e.draft(t, e.open, "METRO-SITE")
		if err != nil {
			t.Fatalf("saving a Hooked draft: %v", err)
		}
		approved := m04ApprovedHooked(t, e)
		m04Mount(e, docscreentest.Hooked, m)
		for _, path := range []string{
			m04URL(docscreentest.Hooked, draft.ID) + "/edit/check",
			m04URL(docscreentest.Hooked, approved),
			m04URL(docscreentest.Hooked, approved) + "/close",
		} {
			if rec := e.get(t, path); rec.Code != http.StatusOK {
				t.Errorf("GET %s = %d, want 200; body %.300s", path, rec.Code, rec.Body)
			}
		}
		for _, hook := range []string{"Hide", "LineCard", "Check", "Page", "Reason"} {
			if calls[hook] == 0 {
				t.Errorf("the %s hook was never called", hook)
			}
		}
	default:
		t.Fatalf("unknown mode %q", mode)
	}
}

// HOOK-015 (hooks spec §5 rule 1, CD-213): a hook that takes a second
// connection inside a transaction ends the run under the nestcheck gate
// (exit 2, naming the hook), and every hook, each taking its own
// connection, passes when docscreen calls it: outside any transaction.
func TestModule_NoNestedTx(t *testing.T) {
	for _, tc := range []struct {
		mode string
		exit int
		want []string
	}{
		{"inside", 2, []string{"db: a transaction was opened inside another on this goroutine", "m04ReadingModule"}},
		{"docscreen", 0, nil},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestModule_NoNestedTxChild$", "-test.v")
			cmd.Env = append(os.Environ(), m04NestEnv+"="+tc.mode)
			var out bytes.Buffer
			cmd.Stdout, cmd.Stderr = &out, &out
			err := cmd.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatalf("run child: %v", err)
			}
			got := cmd.ProcessState.ExitCode()
			if got != tc.exit {
				t.Errorf("exit code = %d, want %d; output:\n%s", got, tc.exit, out.String())
			}
			for _, w := range tc.want {
				if !strings.Contains(out.String(), w) {
					t.Errorf("output lacks %q:\n%s", w, out.String())
				}
			}
			if tc.exit == 0 && got == 0 && !strings.Contains(out.String(), "--- PASS: TestModule_NoNestedTxChild") {
				t.Errorf("the child did not run its test:\n%s", out.String())
			}
		})
	}
}
