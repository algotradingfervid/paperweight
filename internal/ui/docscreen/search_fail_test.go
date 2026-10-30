package docscreen_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// failingAuth is an AUTH that can't answer: not a refusal, a failed check.
type failingAuth struct{ auth.Service }

func (failingAuth) Can(context.Context, auth.Principal, string, auth.Right, *auth.RecordRef) error {
	return errors.New("rights table gone")
}

// failingReader is a database that can't be read.
type failingReader struct{}

func (failingReader) Read(context.Context, func(*db.Tx) error) error {
	return errors.New("disk on fire")
}

// A rights check or a row that fails is an error, not a skipped DocType: the
// page must not say "Nothing found" for something it never looked at.
func TestSearch_FailedCheckOrRowIsAnErrorNotNothingFound(t *testing.T) {
	for _, c := range []struct {
		name string
		fail func(*srchEnv)
	}{
		{"rights check", func(e *srchEnv) { e.d.Auth = failingAuth{e.d.Auth} }},
		{"row values", func(e *srchEnv) { e.d.DB = failingReader{} }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := newSearchEnv(t, srchOrder)
			e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))
			c.fail(e)

			rec := e.get(t, "/search?q=steel")
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Nothing found") {
				t.Error("a failed search is reported as no match")
			}
		})
	}
}

// The row helper the search page shares with the document list keeps the
// list's answer when the read fails.
func TestList_FailedRowReadIsAnError(t *testing.T) {
	e := newSearchEnv(t, srchOrder)
	e.d.DB = failingReader{}
	e.lister.set(srchOrder, srchRow(srchOrder, 1, "PO/26-27/00001", ""))

	if rec := e.get(t, "/doc/"+srchOrder); rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}
