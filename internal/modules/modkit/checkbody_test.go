package modkit_test

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/modules/modkit"
)

// CheckCommentBody is AddTx's body rule for callers that check a note before
// another write (hand-over before Reassign; CD-280 D4): trimmed, not empty,
// at most 1,000 characters, PO spec §22's messages, 422.
func TestCheckCommentBody(t *testing.T) {
	thousand := strings.Repeat("é", 1000) // 2,000 bytes
	for _, c := range []struct{ what, body, msg string }{
		{"empty", "", msgCommentEmpty},
		{"only spaces", " \n\t  ", msgCommentEmpty},
		{"1,001 characters", thousand + "x", msgCommentTooLong},
		{"1,001 characters after trimming", "  " + thousand + "x  ", msgCommentTooLong},
	} {
		wantInvalid(t, "CheckCommentBody "+c.what, modkit.CheckCommentBody(c.body), c.msg)
	}
	for _, body := range []string{thousand, "  " + thousand + "  "} {
		if err := modkit.CheckCommentBody(body); err != nil {
			t.Errorf("CheckCommentBody(%d characters) = %v, want nil", len([]rune(body)), err)
		}
	}
}
