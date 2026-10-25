package print_test

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/kernel/print/printtest"
)

// Supporting test (no case ID): views find the letterhead in the context.
func TestLetterhead_FromContext(t *testing.T) {
	ctx := print.WithLetterhead(t.Context(), printtest.Letterhead)
	lh, err := print.LetterheadFor(ctx, nil, printtest.BLRID)
	if err != nil {
		t.Fatalf("LetterheadFor(BLR): %v", err)
	}
	if lh.GSTIN != "29AAFCV2449G1Z3" {
		t.Errorf("BLR GSTIN = %q, want 29AAFCV2449G1Z3", lh.GSTIN)
	}
	if _, err := print.LetterheadFor(t.Context(), nil, printtest.BLRID); !errors.Is(err, print.ErrNoLetterhead) {
		t.Errorf("LetterheadFor(bare ctx) err = %v, want ErrNoLetterhead", err)
	}
}
