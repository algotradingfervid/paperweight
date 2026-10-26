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

const fixture = "testdata/mod"

// fixtureFile returns the lines of a fixture file.
func fixtureFile(t *testing.T, name string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixture, name)) //nolint:gosec // G304: a fixture file this test names.
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(string(data), "\n")
}

// drop returns the fixture file without the given 1-based lines, so the
// working tree adds them back.
func drop(t *testing.T, name string, lines ...int) string {
	t.Helper()
	var keep []string
	for i, l := range fixtureFile(t, name) {
		if !slices.Contains(lines, i+1) {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// insert returns the fixture file with text added after line after, so the
// working tree deletes it.
func insert(t *testing.T, name string, after int, text string) string {
	t.Helper()
	lines := fixtureFile(t, name)
	return strings.Join(slices.Insert(lines, after, text), "\n")
}

// check commits the fixture module with the base files replaced ("" removes
// the file from the base), restores the fixture as the working tree and runs
// covertouched against the base commit with the fixture's profile.
func check(t *testing.T, base map[string]string) (string, int) {
	t.Helper()
	profile, err := os.ReadFile(filepath.Join(fixture, "cover.out"))
	if err != nil {
		t.Fatal(err)
	}
	return checkProfile(t, base, string(profile))
}

// checkProfile is check with the given profile text.
func checkProfile(t *testing.T, base map[string]string, profile string) (string, int) {
	t.Helper()
	root := t.TempDir()
	copyFixture(t, root)
	for name, text := range base {
		path := filepath.Join(root, name)
		if text == "" {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "add", "-A")
	gitRun(t, root, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "base")
	copyFixture(t, root)

	profilePath := filepath.Join(t.TempDir(), "cover.out")
	if err := os.WriteFile(profilePath, []byte(profile), 0o600); err != nil { //nolint:gosec // G703: a fixed name in the test's temp dir.
		t.Fatal(err)
	}
	var out bytes.Buffer
	code := run([]string{"-base", "HEAD", "-profile", profilePath}, root, &out)
	return out.String(), code
}

func copyFixture(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() == "cover.out" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(fixture, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, e.Name()), data, 0o600); err != nil { //nolint:gosec // G703: a fixture name into the test's temp dir.
			t.Fatal(err)
		}
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// findings returns the reported locations.
func findings(out string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if loc, ok := strings.CutSuffix(l, ": not covered by any test"); ok {
			got = append(got, loc)
		}
	}
	return got
}

func TestCoverTouched(t *testing.T) {
	cases := []struct {
		name string
		base func(t *testing.T) map[string]string
		want []string
	}{
		{
			// calc.go:22 `return n, nil` is new and covered.
			name: "covered new line passes",
			base: func(t *testing.T) map[string]string { return map[string]string{"calc.go": drop(t, "calc.go", 22)} },
		},
		{
			// calc.go:20 `return 0, ErrNegative` is a refusal, not an error return.
			name: "uncovered new statement fails",
			base: func(t *testing.T) map[string]string { return map[string]string{"calc.go": drop(t, "calc.go", 20)} },
			want: []string{"calc.go:20"},
		},
		{
			// calc.go:17 `return 0, err` and :29 `return 0, fmt.Errorf(..., err)`.
			name: "uncovered error returns ignored",
			base: func(t *testing.T) map[string]string { return map[string]string{"calc.go": drop(t, "calc.go", 17, 29)} },
		},
		{
			// calc.go:19 `if n < 0 {` (the if itself is covered) and :21 `}`
			// touch the uncovered body block only by its braces.
			name: "brace-only lines of an uncovered block ignored",
			base: func(t *testing.T) map[string]string { return map[string]string{"calc.go": drop(t, "calc.go", 19, 21)} },
		},
		{
			// Untracked files count in full: gen.go is generated, calc_test.go
			// is a test; neither is checked although gen.go:7 is uncovered.
			name: "test files and generated files ignored",
			base: func(*testing.T) map[string]string { return map[string]string{"gen.go": "", "calc_test.go": ""} },
		},
		{
			name: "new untracked file checked",
			base: func(*testing.T) map[string]string { return map[string]string{"extra.go": ""} },
			want: []string{"extra.go:5", "extra.go:6"},
		},
		{
			// The base has an extra line inside the uncovered block at calc.go:19-21.
			name: "deleted-only hunks ignored",
			base: func(t *testing.T) map[string]string {
				return map[string]string{"calc.go": insert(t, "calc.go", 19, "\t\tprintln()")}
			},
		},
		{
			// Only calc.go:15 changed; the uncovered calc.go:20 and extra.go are untouched.
			name: "lines outside the diff ignored",
			base: func(t *testing.T) map[string]string {
				lines := fixtureFile(t, "calc.go")
				lines[14] = "\tn, err := strconv.ParseInt(s)"
				return map[string]string{"calc.go": strings.Join(lines, "\n")}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, code := check(t, tc.base(t))
			wantCode := 0
			if len(tc.want) > 0 {
				wantCode = 1
			}
			if code != wantCode {
				t.Errorf("exit code = %d, want %d\n%s", code, wantCode, out)
			}
			if got := findings(out); !slices.Equal(got, tc.want) {
				t.Errorf("findings = %q, want %q\n%s", got, tc.want, out)
			}
		})
	}
}

func TestChecked_SkipsGeneratedPaths(t *testing.T) {
	root := t.TempDir()
	for _, path := range []string{"a/page_templ.go", "a/dbq/query.go", "a/testdata/x.go", "a/x_test.go"} {
		if ok, err := checked(root, path); ok || err != nil {
			t.Errorf("checked(%q) = %v, %v; want false, nil", path, ok, err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "x.go"), []byte("package x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := checked(root, "x.go"); !ok || err != nil {
		t.Errorf("checked(x.go) = %v, %v; want true, nil", ok, err)
	}
}

func TestRun_UsageErrors(t *testing.T) {
	for _, args := range [][]string{{"-profile", "x"}, {"-base", "HEAD"}, {"-base", "HEAD", "-profile", "x", "extra"}} {
		var out bytes.Buffer
		if code := run(args, t.TempDir(), &out); code != 2 {
			t.Errorf("run(%q) = %d, want 2\n%s", args, code, out.String())
		}
	}
}

// dropLine20 makes calc.go:20 (`return 0, ErrNegative`, an uncovered
// refusal in the fixture profile) a touched line.
func dropLine20(t *testing.T) map[string]string {
	return map[string]string{"calc.go": drop(t, "calc.go", 20)}
}

func TestCoverTouched_IgnoresUserDiffPrefixes(t *testing.T) {
	for _, key := range []string{"diff.noprefix", "diff.mnemonicPrefix"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv("GIT_CONFIG_COUNT", "1")
			t.Setenv("GIT_CONFIG_KEY_0", key)
			t.Setenv("GIT_CONFIG_VALUE_0", "true")
			out, code := check(t, dropLine20(t))
			if code != 1 || !slices.Equal(findings(out), []string{"calc.go:20"}) {
				t.Errorf("exit code %d, findings %q; want 1, [calc.go:20]\n%s", code, findings(out), out)
			}
		})
	}
}

func TestCoverTouched_MergesDuplicateBlocks(t *testing.T) {
	// Two test binaries list the block of calc.go:19-21; one of them ran it.
	for _, counts := range [][2]string{{"0", "1"}, {"1", "0"}} {
		profile := "mode: set\n" +
			"example.com/mod/calc.go:19.11,21.3 1 " + counts[0] + "\n" +
			"example.com/mod/calc.go:19.11,21.3 1 " + counts[1] + "\n"
		out, code := checkProfile(t, dropLine20(t), profile)
		if code != 0 || len(findings(out)) != 0 {
			t.Errorf("counts %v: exit code %d, findings %q; want 0, none\n%s", counts, code, findings(out), out)
		}
	}
}

func TestCoverTouched_SkipsOtherModules(t *testing.T) {
	// The uncovered blocks lie outside module example.com/mod (one without
	// any module prefix, which would read as the repo's calc.go if kept);
	// calc.go's package is still in the profile through its covered block.
	profile := "mode: set\n" +
		"example.com/other/calc.go:19.11,21.3 1 0\n" +
		"calc.go:19.11,21.3 1 0\n" +
		"example.com/mod/calc.go:22.2,22.15 1 1\n"
	out, code := checkProfile(t, dropLine20(t), profile)
	if code != 0 || len(findings(out)) != 0 || strings.Contains(out, "not in profile") {
		t.Errorf("exit code %d, findings %q; want 0, none, no note\n%s", code, findings(out), out)
	}
}

func TestCoverTouched_NotesPackagesMissingFromProfile(t *testing.T) {
	profile := "mode: set\nexample.com/mod/sub/x.go:1.1,2.2 1 0\n"
	out, code := checkProfile(t, dropLine20(t), profile)
	if code != 0 || !strings.Contains(out, "calc.go: not in profile, not checked\n") {
		t.Errorf("exit code %d; want 0 and a not-in-profile note for calc.go\n%s", code, out)
	}
}

func TestCoverTouched_RejectsBadProfile(t *testing.T) {
	out, code := checkProfile(t, dropLine20(t), "mode: set\nnot a block\n")
	if code != 2 || !strings.Contains(out, "is not a coverage block") {
		t.Errorf("exit code %d; want 2 and \"is not a coverage block\"\n%s", code, out)
	}
}
