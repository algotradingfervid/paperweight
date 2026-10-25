// Package printtest holds test helpers for print views (CD-58, CD-85).
package printtest

import (
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/print"
)

// RenderString renders info's print page through reg with a nil transaction
// and fails the test on any error.
func RenderString(t testing.TB, reg *print.Registry, info doctype.Info) string {
	t.Helper()
	var sb strings.Builder
	if err := reg.Render(t.Context(), nil, &sb, info); err != nil {
		t.Fatalf("print.Render(%s %d): %v", info.DocType, info.ID, err)
	}
	return sb.String()
}
