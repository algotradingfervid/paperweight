package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// check runs the checker against testdata/<fixture> and returns its output and exit code.
func check(t *testing.T, fixture string, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	code := run(args, filepath.Join("testdata", fixture), &out)
	return out.String(), code
}

func lines(out, prefix string) []string {
	var got []string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			got = append(got, l)
		}
	}
	return got
}

func TestCheckTestIDs_FindsMissing(t *testing.T) {
	out, code := check(t, "missing")
	if code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, out)
	}
	got := lines(out, "MISSING ")
	if len(got) != 1 || got[0] != "MISSING NUM-001 TestMoney_Add P1" {
		t.Errorf("MISSING lines = %q, want [MISSING NUM-001 TestMoney_Add P1]\n%s", got, out)
	}
	if !strings.Contains(out, "P1 implemented: 0/1") {
		t.Errorf("summary missing \"P1 implemented: 0/1\":\n%s", out)
	}
}

func TestCheckTestIDs_PassesWhenPresent(t *testing.T) {
	out, code := check(t, "present")
	if code != 0 {
		t.Errorf("exit code = %d, want 0\n%s", code, out)
	}
	if got := lines(out, "MISSING "); len(got) != 0 {
		t.Errorf("unexpected MISSING lines %q", got)
	}
	if !strings.Contains(out, "P1 implemented: 1/1") {
		t.Errorf("summary missing \"P1 implemented: 1/1\":\n%s", out)
	}
}

func TestCheckTestIDs_IgnoresComments(t *testing.T) {
	out, code := check(t, "comments")
	if code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, out)
	}
	if got := lines(out, "MISSING "); len(got) != 1 || got[0] != "MISSING NUM-001 TestMoney_Add P1" {
		t.Errorf("MISSING lines = %q, want NUM-001 missing\n%s", got, out)
	}
}

func TestCheckTestIDs_PrefixFilter(t *testing.T) {
	out, _ := check(t, "prefix", "NAM")
	got := lines(out, "MISSING ")
	if len(got) != 1 || got[0] != "MISSING NAM-001 TestTemplate_Save P1" {
		t.Errorf("MISSING lines = %q, want only NAM-001\n%s", got, out)
	}
	if !strings.Contains(out, "P1 implemented: 0/1") {
		t.Errorf("summary missing \"P1 implemented: 0/1\":\n%s", out)
	}
}

func TestCheckTestIDs_ReportModeExitsZero(t *testing.T) {
	out, code := check(t, "missing", "--report")
	if code != 0 {
		t.Errorf("exit code = %d, want 0\n%s", code, out)
	}
	if got := lines(out, "MISSING "); len(got) != 1 {
		t.Errorf("report mode should still print MISSING lines, got %q", got)
	}
}

func TestCheckTestIDs_ExcludeLevel(t *testing.T) {
	out, code := check(t, "exclude", "--exclude-level", "e2e")
	if code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, out)
	}
	got := lines(out, "MISSING ")
	if len(got) != 1 || got[0] != "MISSING DOC-001 FuzzDoc_X P1" {
		t.Errorf("MISSING lines = %q, want only DOC-001\n%s", got, out)
	}
	if !strings.Contains(out, "P1 implemented: 0/1 (excluded 1 e2e)") {
		t.Errorf("summary missing \"P1 implemented: 0/1 (excluded 1 e2e)\":\n%s", out)
	}

	out, _ = check(t, "exclude")
	got = lines(out, "MISSING ")
	want := []string{"MISSING DOC-001 FuzzDoc_X P1", "MISSING DOC-090 TestE2E_FixtureLifecycle P1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("without the flag MISSING lines = %q, want %q\n%s", got, want, out)
	}
}

func TestCheckTestIDs_FlagAfterArea(t *testing.T) {
	out, _ := check(t, "exclude", "DOC", "--exclude-level", "e2e")
	got := lines(out, "MISSING ")
	if len(got) != 1 || got[0] != "MISSING DOC-001 FuzzDoc_X P1" {
		t.Errorf("MISSING lines = %q, want only DOC-001\n%s", got, out)
	}
	if !strings.Contains(out, "(excluded 1 e2e)") {
		t.Errorf("summary missing \"(excluded 1 e2e)\":\n%s", out)
	}
}

func TestCheckTestIDs_MissingLevelIsError(t *testing.T) {
	out, code := check(t, "nolevel", "--report")
	if code != 0 {
		t.Errorf("exit code with --report = %d, want 0\n%s", code, out)
	}
	got := lines(out, "ERROR ")
	if len(got) != 1 || !strings.HasPrefix(got[0], "ERROR DOC-005 ") {
		t.Errorf("ERROR lines = %q, want one naming DOC-005\n%s", got, out)
	}
	if _, code := check(t, "nolevel"); code != 1 {
		t.Errorf("exit code without --report = %d, want 1", code)
	}
}

func TestCheckTestIDs_SubtestNameMatchesFunc(t *testing.T) {
	out, code := check(t, "subtest_present")
	if code != 0 {
		t.Errorf("present: exit code = %d, want 0\n%s", code, out)
	}
	if got := lines(out, "MISSING "); len(got) != 0 {
		t.Errorf("present: unexpected MISSING lines %q", got)
	}
	if got := lines(out, "ERROR "); len(got) != 0 {
		t.Errorf("present: shared function via subtests must not be an error, got %q", got)
	}

	out, _ = check(t, "subtest_absent")
	got := lines(out, "MISSING ")
	want := []string{
		"MISSING NAM-009 TestTemplate_Validate/missing_number P1",
		"MISSING NAM-010 TestTemplate_Validate/bad_token P1",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("absent: MISSING lines = %q, want %q\n%s", got, want, out)
	}

	out, code = check(t, "subtest_duplicate")
	if code != 1 {
		t.Errorf("duplicate: exit code = %d, want 1\n%s", code, out)
	}
	errs := lines(out, "ERROR ")
	if len(errs) != 1 || !strings.HasPrefix(errs[0], "ERROR NAM-021 ") || !strings.Contains(errs[0], "NAM-020") {
		t.Errorf("duplicate: ERROR lines = %q, want one for NAM-021 naming NAM-020\n%s", errs, out)
	}
}

func TestCheckTestIDs_E2ENamePrefix(t *testing.T) {
	out, code := check(t, "e2e_prefix")
	if code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, out)
	}
	got := lines(out, "ERROR ")
	if len(got) != 1 || !strings.HasPrefix(got[0], "ERROR DOC-091 ") {
		t.Errorf("ERROR lines = %q, want one naming DOC-091\n%s", got, out)
	}
}

func TestCheckTestIDs_MissingGoTestIsError(t *testing.T) {
	out, code := check(t, "nogotest")
	if code != 1 {
		t.Errorf("exit code = %d, want 1\n%s", code, out)
	}
	got := lines(out, "ERROR ")
	if len(got) != 2 || !strings.HasPrefix(got[0], "ERROR DOC-007 ") || !strings.HasPrefix(got[1], "ERROR DOC-008 ") {
		t.Errorf("ERROR lines = %q, want one each for DOC-007 and DOC-008\n%s", got, out)
	}
	if got := lines(out, "MISSING "); len(got) != 0 {
		t.Errorf("a case without a Go test must not print MISSING, got %q", got)
	}
}
