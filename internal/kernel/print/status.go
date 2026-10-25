package print

import (
	"context"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
)

// Status is the watermark and badge a print carries; the zero value means none.
type Status struct{ Code, Label string }

// StatusFor maps a document state to its print status. Only approved and
// closed prints are clean; cancelled is marked CANCELLED and every other
// state, including one the shell doesn't know, is marked DRAFT.
func StatusFor(state string) Status {
	switch doctype.State(state) {
	case doctype.StateApproved, doctype.StateClosed:
		return Status{}
	case doctype.StateCancelled:
		return Status{Code: "cancelled", Label: "CANCELLED"}
	default:
		return Status{Code: "draft", Label: "DRAFT"}
	}
}

var titleReplacer = strings.NewReplacer("/", "_", `\`, "_", "%", "_", "?", "_", "#", "_")

// SafeTitle replaces each of / \ % ? # with _ so the page title can be a file name.
func SafeTitle(s string) string { return titleReplacer.Replace(s) }

// Header is what the page shell decides from database state: status, number and title.
type Header struct {
	Status        Status
	Number, Title string
}

type headerKey struct{}

// HeaderFrom returns the Header Render put in ctx, or the zero Header.
func HeaderFrom(ctx context.Context) Header {
	h, _ := ctx.Value(headerKey{}).(Header)
	return h
}

func withHeader(ctx context.Context, h Header) context.Context {
	return context.WithValue(ctx, headerKey{}, h)
}
