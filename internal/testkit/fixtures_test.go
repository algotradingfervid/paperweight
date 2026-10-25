package testkit_test

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/testkit"
)

// rightWords reads the 11 right words from the comment on auth.Right in
// internal/kernel/auth/contract.go, the only place they are listed.
func rightWords(t *testing.T) []string {
	t.Helper()
	src, err := os.ReadFile("../kernel/auth/contract.go")
	if err != nil {
		t.Fatalf("read auth contract: %v", err)
	}
	const prefix = "type Right string //"
	for line := range strings.Lines(string(src)) {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			words := strings.Fields(rest)
			if len(words) != 11 {
				t.Fatalf("auth.Right comment lists %d rights %q, want 11", len(words), words)
			}
			return words
		}
	}
	t.Fatalf("auth contract has no line starting %q", prefix)
	return nil
}

func TestTestkit_FixtureRightsUseAuthWords(t *testing.T) {
	rights := rightWords(t)
	scopes := []string{string(auth.ScopeOwn), string(auth.ScopeTeam), string(auth.ScopeAll)}
	fx := testkit.Seed(t, testkit.NewDB(t))
	for _, r := range fx.Rights {
		for _, w := range r.Rights {
			if !slices.Contains(rights, w) {
				t.Errorf("rights row %s/%s has right %q, want one of %q", r.Role, r.DocType, w, rights)
			}
		}
		if !slices.Contains(scopes, r.Scope) {
			t.Errorf("rights row %s/%s scope = %q, want one of %q", r.Role, r.DocType, r.Scope, scopes)
		}
	}
}
