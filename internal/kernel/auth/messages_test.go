package auth

import (
	"testing"
	"time"
)

// TestWider_ScopeOrder checks all > team > own > "" (not granted).
func TestWider_ScopeOrder(t *testing.T) {
	tests := []struct{ a, b, want Scope }{
		{"", "", ""},
		{"", ScopeOwn, ScopeOwn},
		{ScopeOwn, "", ScopeOwn},
		{ScopeOwn, ScopeTeam, ScopeTeam},
		{ScopeTeam, ScopeOwn, ScopeTeam},
		{ScopeTeam, ScopeAll, ScopeAll},
		{ScopeAll, ScopeOwn, ScopeAll},
		{ScopeAll, ScopeAll, ScopeAll},
	}
	for _, tc := range tests {
		if got := Wider(tc.a, tc.b); got != tc.want {
			t.Errorf("Wider(%q, %q) = %q, want %q", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestMessages_Texts pins the texts TestMessages_Denied does not cover
// (plan Task 1 message table; 01-auth.md).
func TestMessages_Texts(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"ValueError sites", (&ValueError{Dim: DimSite, Allowed: []string{"PUNE-WH", "METRO-SITE"}}).Error(),
			"You can only use Sites PUNE-WH, METRO-SITE. Choose one of your allowed sites."},
		{"ValueError project", (&ValueError{Dim: DimProject, Allowed: []string{"PRJ-A"}}).Error(),
			"You can only use Project PRJ-A. Choose one of your allowed projects."},
		{"ValueError projects", (&ValueError{Dim: DimProject, Allowed: []string{"PRJ-A", "PRJ-B"}}).Error(),
			"You can only use Projects PRJ-A, PRJ-B. Choose one of your allowed projects."},
		{"LinkError", (&LinkError{Label: "Purchase Order"}).Error(),
			"You can't use this Purchase Order. It isn't available to you."},
		{"ImportRowMessage create", ImportRowMessage(2, RightCreate, "Item"),
			"Row 2: you don't have permission to create Item."},
		{"ImportRowMessage write", ImportRowMessage(3, RightWrite, "Item"),
			"Row 3: you don't have permission to change Item."},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestTimefmt_RoundTrip checks the stored format (fixed-width UTC, §4.3) and
// that ParseTime reads it back.
func TestTimefmt_RoundTrip(t *testing.T) {
	ist := time.FixedZone("IST", 5*3600+1800)
	in := time.Date(2026, 10, 24, 10, 30, 0, 7_000_000, ist)
	s := FormatTime(in)
	if want := "2026-10-24T05:00:00.007Z"; s != want {
		t.Errorf("FormatTime(%v) = %q, want %q", in, s, want)
	}
	if got := FormatTime(time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)); got != "2026-10-24T05:00:00.000Z" {
		t.Errorf("FormatTime(whole second) = %q, want %q", got, "2026-10-24T05:00:00.000Z")
	}
	out, err := ParseTime(s)
	if err != nil {
		t.Fatalf("ParseTime(%q): %v", s, err)
	}
	if !out.Equal(in) || out.Location() != time.UTC {
		t.Errorf("ParseTime(%q) = %v, want %v in UTC", s, out, in.UTC())
	}
}
