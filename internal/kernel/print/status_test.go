package print_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
)

const (
	draftMark     = `<div class="watermark" data-status="draft">DRAFT</div>`
	draftTitle    = `<h1 class="doc-title">Purchase Order <span class="badge" data-status="draft">DRAFT</span></h1>`
	cancelMark    = `<div class="watermark" data-status="cancelled">CANCELLED</div>`
	cancelTitle   = `<h1 class="doc-title">Purchase Order <span class="badge" data-status="cancelled">CANCELLED</span></h1>`
	firstPONumber = "PO/26-27/00001"
)

func TestStatusFor(t *testing.T) {
	cases := map[string]print.Status{
		"approved":         {},
		"closed":           {},
		"cancelled":        {Code: "cancelled", Label: "CANCELLED"},
		"draft":            {Code: "draft", Label: "DRAFT"},
		"pending_approval": {Code: "draft", Label: "DRAFT"},
		"rejected":         {Code: "draft", Label: "DRAFT"},
		"archived":         {Code: "draft", Label: "DRAFT"},
		"":                 {Code: "draft", Label: "DRAFT"},
	}
	for state, want := range cases {
		if got := print.StatusFor(state); got != want {
			t.Errorf("StatusFor(%q) = %+v, want %+v", state, got, want)
		}
	}
}

// FILE-052
func TestPrint_DraftWatermark(t *testing.T) {
	for _, state := range []doctype.State{doctype.StateDraft, doctype.StatePending, doctype.StateRejected} {
		t.Run(string(state), func(t *testing.T) {
			html := renderPO(t, state, 42, "", "Draft 42")
			for _, want := range []string{draftMark, draftTitle, "Draft 42"} {
				if !strings.Contains(html, want) {
					t.Errorf("missing %q in\n%s", want, html)
				}
			}
			if strings.Contains(html, "PO/26-27/") {
				t.Errorf("draft print shows a number:\n%s", html)
			}
		})
	}
}

// FILE-053
func TestPrint_CancelledWatermark(t *testing.T) {
	html := renderPO(t, doctype.StateCancelled, 42, firstPONumber, firstPONumber)
	for _, want := range []string{cancelMark, cancelTitle, firstPONumber} {
		if !strings.Contains(html, want) {
			t.Errorf("missing %q in\n%s", want, html)
		}
	}
}

// FILE-054
func TestPrint_NoWatermarkWhenFinal(t *testing.T) {
	for _, state := range []doctype.State{doctype.StateApproved, doctype.StateClosed} {
		t.Run(string(state), func(t *testing.T) {
			html := renderPO(t, state, 42, firstPONumber, firstPONumber)
			if !strings.Contains(html, firstPONumber) {
				t.Errorf("missing number in\n%s", html)
			}
			for _, bad := range []string{`class="watermark"`, "DRAFT", "CANCELLED"} {
				if strings.Contains(html, bad) {
					t.Errorf("final print contains %q:\n%s", bad, html)
				}
			}
		})
	}
}

// Review Focus 1: a state the shell doesn't know is not final.
func TestPrint_UnknownStateIsWatermarked(t *testing.T) {
	for _, state := range []doctype.State{"archived", ""} {
		t.Run(string(state), func(t *testing.T) {
			html := renderPO(t, state, 42, "", "Draft 42")
			if !strings.Contains(html, draftMark) {
				t.Errorf("state %q: missing DRAFT watermark in\n%s", state, html)
			}
		})
	}
}
