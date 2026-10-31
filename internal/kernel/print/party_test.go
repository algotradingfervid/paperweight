package print_test

import (
	"context"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
)

func renderParty(t *testing.T, p print.Party) string {
	t.Helper()
	var b strings.Builder
	if err := print.PartyBlock(p).Render(context.Background(), &b); err != nil {
		t.Fatalf("render party: %v", err)
	}
	return b.String()
}

// CD-316 (5): a party with no Name (a draft with no vendor or customer
// chosen) prints nothing, not even the section.
func TestPartyBlock_EmptyNamePrintsNothing(t *testing.T) {
	got := renderParty(t, print.Party{Label: "Vendor", Address: []string{"Pune"}})
	if got != "" {
		t.Fatalf("PartyBlock with empty Name = %q, want empty", got)
	}
}

// FILE-063: a named party with no GSTIN still prints "GSTIN: Unregistered".
func TestPartyBlock_NameWithoutGSTINPrintsUnregistered(t *testing.T) {
	got := renderParty(t, print.Party{Label: "Vendor", Name: "Shree Traders"})
	for _, want := range []string{`<section class="party">`, "Shree Traders", "GSTIN: Unregistered"} {
		if !strings.Contains(got, want) {
			t.Errorf("PartyBlock = %q, want it to contain %q", got, want)
		}
	}
}
