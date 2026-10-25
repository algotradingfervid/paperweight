package mstkit

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// msgStale is DOC's stale wording (doc/messages.go, unexported there).
const msgStale = "Someone else changed %s after you opened it. Reload it and make your changes again."

// CheckVersion returns DOC's stale error (409) when (k, id) is no longer at
// the version the page posted (CD-94). A missing row is doctype.ErrNotFound.
func CheckVersion(ctx context.Context, tx *db.Tx, k Kind, id int64, version int) error {
	col := "name"
	if k.HasCode {
		col = "code"
	}
	var current int
	var title string
	err := tx.SQL().QueryRowContext(ctx,
		"SELECT version, "+col+" FROM "+k.Table+" WHERE id = ?", id).Scan(&current, &title) //nolint:gosec // G202: Table/FKColumn come from each master's package-level Kind, never from a request
	if errors.Is(err, sql.ErrNoRows) {
		return doctype.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != version {
		return staleError(k, title)
	}
	return nil
}

// staleError is DOC's stale error with the record shown as "{Label} {title}".
func staleError(k Kind, title string) *doc.Error {
	return &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgStale, k.Label+" "+title)}
}
