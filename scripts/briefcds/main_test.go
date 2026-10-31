package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // G304: a fixture file this test names.
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseEntries(t *testing.T) {
	got := parseEntries(readFixture(t, "head.md"))
	var ids []string
	for _, e := range got {
		ids = append(ids, e.id)
	}
	if want := []string{"CD-01", "CD-02", "CD-03", "CD-104", "CD-310", "CD-311", "CD-312"}; !slices.Equal(ids, want) {
		t.Fatalf("ids = %q, want %q", ids, want)
	}
	// An entry runs on to its paragraphs and bullets, and stops at the next
	// row or heading.
	if e := got[1]; !strings.Contains(e.text, "UX-E6") || strings.Contains(e.text, "csvio") {
		t.Errorf("CD-02 text = %q, want its bullets and not CD-03", e.text)
	}
	if e := got[2]; strings.Contains(e.text, "Round 2") {
		t.Errorf("CD-03 text = %q, want it to stop at the heading", e.text)
	}
	if e := got[0]; !strings.HasPrefix(e.text, "**Shared types") {
		t.Errorf("CD-01 text = %q, want it to start after the id cell", e.text)
	}
}

func TestNewOrChanged(t *testing.T) {
	base := parseEntries(readFixture(t, "base.md"))
	head := parseEntries(readFixture(t, "head.md"))
	var ids []string
	for _, e := range newOrChanged(base, head) {
		ids = append(ids, e.id)
	}
	if want := []string{"CD-02", "CD-310", "CD-311", "CD-312"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %q, want %q", ids, want)
	}
}

func TestTerms(t *testing.T) {
	for _, tc := range []struct {
		arg  string
		want []string
	}{
		{"internal/modules/expense/claim.go", []string{"internal/modules/expense/claim.go", "claim", "expense"}},
		{"./internal/ui/docscreen/...", []string{"internal/ui/docscreen", "docscreen"}},
		{"internal/ui/docscreen/", []string{"internal/ui/docscreen", "docscreen"}},
		{"e2e", []string{"e2e"}},
		{"main.go", []string{"main.go", "main"}},
	} {
		if got := terms(tc.arg); !slices.Equal(got, tc.want) {
			t.Errorf("terms(%q) = %q, want %q", tc.arg, got, tc.want)
		}
	}
}

func TestMentions(t *testing.T) {
	for _, tc := range []struct {
		text, term string
		want       bool
	}{
		{"`docscreen.Deps.LinkRead` is", "docscreen", true},
		{"a formatter", "form", false},
		{"not a form.", "form", true},
		{"form", "form", true},
		{"reform it", "form", false},
		{"see internal/ui/docscreen/form.go:12", "internal/ui/docscreen/form.go", true},
		{"see internal/ui/docscreen/form.gox", "internal/ui/docscreen/form.go", false},
		{"Docscreen", "docscreen", false},
	} {
		if got := mentions(tc.text, []string{tc.term}); got != tc.want {
			t.Errorf("mentions(%q, %q) = %v, want %v", tc.text, tc.term, got, tc.want)
		}
	}
}

// repo commits base.md then head.md as the CD log on main, then checks out a
// lane branch at the base commit, as a lane that lags main. It returns the
// repo and the base commit.
func repo(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	gitRun(t, root, "init", "-q", "-b", "main")
	log := filepath.Join(root, "docs", "specs", "contract-changes.md")
	if err := os.MkdirAll(filepath.Dir(log), 0o750); err != nil {
		t.Fatal(err)
	}
	commit := func(name string) string {
		if err := os.WriteFile(log, []byte(readFixture(t, name)), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun(t, root, "add", "-A")
		gitRun(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", name)
		return strings.TrimSpace(gitRun(t, root, "rev-parse", "HEAD"))
	}
	base := commit("base.md")
	commit("head.md")
	gitRun(t, root, "checkout", "-q", "-b", "lane", base)
	return root, base
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...) //nolint:gosec // G204: fixed git subcommands in a test.
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestRun(t *testing.T) {
	root, base := repo(t)
	long := "CD-310: **UX-E5 focus return.** When an upload ends, `upload.js` in internal/ui/js returns focus to the upload's input unless focus is in a form field outside that uplo\n"
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"file and package dir", []string{"-base", base, "internal/ui/docscreen/form.go"},
			"CD-02: **Expense docscreen gaps.** The gaps are cited as UX-E1 to UX-E7. | AD-10 | *Gaps.* - UX-E5: `hx-disabled-elt` through `UploadProps.AlsoDisable`. Amended by CD-\n" +
				long +
				"CD-312: **Document link targets are checked by auth.** `docscreen.Deps.LinkRead` is for masters only; a formatter is not a form. | AD-13 |\n"},
		{"ident", []string{"-base", base, "-ident", "Validate", "-ident", "nothing"},
			"CD-311: **E10 rulings.** An expense claim's `less_advance` is NonNegative; see `Validate` in internal/modules/expense/claim.go. | AD-11 |\n"},
		{"file name", []string{"-base", base, "other/claim.go"},
			"CD-311: **E10 rulings.** An expense claim's `less_advance` is NonNegative; see `Validate` in internal/modules/expense/claim.go. | AD-11 |\n"},
		{"no match", []string{"-base", base, "internal/platform/db"},
			"no CDs since " + base + " mention these\n"},
		{"head is the lagging lane", []string{"-base", base, "-head", "HEAD", "internal/ui/docscreen/form.go"},
			"no CDs since " + base + " mention these\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			if code := run(tc.args, root, &out); code != 0 {
				t.Fatalf("exit %d, output:\n%s", code, out.String())
			}
			if out.String() != tc.want {
				t.Errorf("output:\n%s\nwant:\n%s", out.String(), tc.want)
			}
		})
	}
}

func TestRun_Usage(t *testing.T) {
	root, base := repo(t)
	for _, args := range [][]string{{"internal/ui"}, {"-base", base}, {"-base", "nosuchrev", "internal/ui"}, {"-base", base, "-head", "nosuchrev", "internal/ui"}} {
		var out bytes.Buffer
		if code := run(args, root, &out); code != 2 {
			t.Errorf("run(%q) = %d, want 2; output:\n%s", args, code, out.String())
		}
	}
}
