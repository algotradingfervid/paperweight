package print_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
)

// FILE-066 (CD-58): every printable DocType, plus the fixture DocTypes, has a
// view that renders in every state; the approved renders match their goldens.
func TestPrint_AllPrintableDocTypesHaveTemplates(t *testing.T) {
	printtest.RegisterFixtureViews(print.Default)
	printtest.RegisterFixtureViews(print.Default) // idempotent on Default
	names := print.PrintableNames(doctype.All())
	for _, n := range printtest.FixtureDocTypes {
		if !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	if err := print.Default.Missing(names); err != nil {
		t.Fatal(err)
	}
	numbers := map[string]string{"fixture.Order": "PO/26-27/00001", "fixture.Receipt": "GR/26-27/00001", "fixture.Claim": "EX/26-27/00001"}
	goldens := map[string]string{"fixture.Order": "fixture_order_approved.golden.html", "fixture.Receipt": "fixture_receipt_approved.golden.html", "fixture.Claim": "fixture_claim_approved.golden.html"}
	states := []doctype.State{doctype.StateDraft, doctype.StatePending, doctype.StateApproved, doctype.StateRejected, doctype.StateClosed, doctype.StateCancelled}
	ctx := print.WithLetterhead(t.Context(), printtest.Letterhead)
	for _, name := range printtest.FixtureDocTypes {
		for _, s := range states {
			info := doctype.Info{DocType: name, ID: 1, State: s, Display: "Draft 1"}
			if s == doctype.StateApproved || s == doctype.StateClosed || s == doctype.StateCancelled {
				info.Number, info.Display = numbers[name], numbers[name]
			}
			var sb strings.Builder
			if err := print.Default.Render(ctx, nil, &sb, info); err != nil {
				t.Errorf("Render %s %s: %v", name, s, err)
				continue
			}
			if s == doctype.StateApproved {
				printtest.Golden(t, goldens[name], []byte(sb.String()))
			}
		}
	}
}
