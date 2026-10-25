package print

import (
	"context"
	"errors"

	"github.com/vardhan/internaltools/internal/platform/db"
)

// ErrNoLetterhead is returned by LetterheadFor when ctx carries no Letterhead.
var ErrNoLetterhead = errors.New("print: no letterhead source")

type letterheadKey struct{}

// WithLetterhead returns ctx carrying l, for the views LetterheadFor serves.
func WithLetterhead(ctx context.Context, l Letterhead) context.Context {
	return context.WithValue(ctx, letterheadKey{}, l)
}

// LetterheadFor returns the letterhead for branchID from the Letterhead in ctx.
func LetterheadFor(ctx context.Context, tx *db.Tx, branchID int64) (LetterheadInfo, error) {
	l, ok := ctx.Value(letterheadKey{}).(Letterhead)
	if !ok {
		return LetterheadInfo{}, ErrNoLetterhead
	}
	return l.ForBranch(ctx, tx, branchID)
}
