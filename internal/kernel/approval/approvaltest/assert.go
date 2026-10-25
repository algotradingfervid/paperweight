package approvaltest

import (
	"errors"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// WantErr checks that err is of kind and has exactly the message msg.
func WantErr(t testing.TB, err error, kind error, msg string) {
	t.Helper()
	if err == nil || !errors.Is(err, kind) || err.Error() != msg {
		t.Errorf("error = %v\nwant %v with message %q", err, kind, msg)
	}
}

// WantStatus checks that err is a doctype.HTTPError with status (CD-44).
func WantStatus(t testing.TB, err error, status int) {
	t.Helper()
	var h doctype.HTTPError
	if !errors.As(err, &h) || h.Status() != status {
		t.Errorf("error %v: want an HTTPError with status %d", err, status)
	}
}

// WantNotFound checks for DOC's 404 (doctype.ErrNotFound, CD-52); no APR text.
func WantNotFound(t testing.TB, err error) {
	t.Helper()
	if !errors.Is(err, doctype.ErrNotFound) {
		t.Errorf("error = %v, want doctype.ErrNotFound", err)
	}
}

// MustOK stops the test on err.
func MustOK(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
