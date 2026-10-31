package expensetest

import (
	"bytes"
	"context"
	"maps"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// E3 (Receipts) helpers: attach and remove files through the real
// files.Store, as the line page does (spec §3.3), and edit a stored claim's
// lines.

// AttachFile spools content outside any transaction and attaches it as name
// to document dt id, as p, in one db.Write (CD-133). On success it returns
// the stored Meta, with the SHA-256 a line's receipt_sha256 holds.
func (e *Env) AttachFile(t testing.TB, p auth.Principal, dt string, id int64, name string, content []byte) (files.Meta, error) {
	t.Helper()
	sp, err := e.Files.Spool(bytes.NewReader(content), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("expensetest: spool %s: %v", name, err)
	}
	defer func() { _ = sp.Cleanup() }()
	var m files.Meta
	err = e.DB.Write(context.Background(), func(tx *db.Tx) error {
		aid, err := e.Files.Attach(context.Background(), tx, p, auth.RecordRef{DocType: dt, ID: id}, name, sp)
		if err != nil {
			return err
		}
		m, err = e.Files.Meta(context.Background(), tx, aid)
		return err
	})
	return m, err
}

// MustAttach is AttachFile that fails the test on an error.
func (e *Env) MustAttach(t testing.TB, p auth.Principal, dt string, id int64, name string, content []byte) files.Meta {
	t.Helper()
	m, err := e.AttachFile(t, p, dt, id, name, content)
	if err != nil {
		t.Fatalf("expensetest: attach %s to %s %d as %s: %v", name, dt, id, p.Name, err)
	}
	return m
}

// RemoveFile removes attachment id as p in one db.Write.
func (e *Env) RemoveFile(p auth.Principal, id int64) error {
	return e.DB.Write(context.Background(), func(tx *db.Tx) error {
		return e.Files.Remove(context.Background(), tx, p, id)
	})
}

// FilesOf lists document dt id's attachments, newest first, as stored.
func (e *Env) FilesOf(t testing.TB, dt string, id int64) []files.Meta {
	t.Helper()
	var got []files.Meta
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		got, err = e.Files.ListTx(context.Background(), tx, auth.RecordRef{DocType: dt, ID: id})
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: list the files of %s %d: %v", dt, id, err)
	}
	return got
}

// LoadDoc is document dt id as the engine loads it.
func (e *Env) LoadDoc(t testing.TB, dt string, id int64) doc.Record {
	t.Helper()
	var rec doc.Record
	err := e.DB.Read(context.Background(), func(tx *db.Tx) error {
		var err error
		rec, err = e.Engine.Load(context.Background(), tx, dt, id)
		return err
	})
	if err != nil {
		t.Fatalf("expensetest: load %s %d: %v", dt, id, err)
	}
	return rec
}

// ClaimEdit is the input that saves stored draft claim id with its stored
// version: the header values in header (the engine keeps the stored value of
// every header field not sent) and every stored line in full, with lines[n]
// merged over line n (1-based). It reads outside any transaction, so a case
// can build the input first and save inside testkit.NoSecondConn.
func (e *Env) ClaimEdit(t testing.TB, id int64, header Values, lines map[int]Values) doc.ActionInput {
	t.Helper()
	rec := e.LoadDoc(t, expense.ClaimDocType, id)
	in := doc.ActionInput{Version: rec.Version, Values: merge(Values{}, header)}
	for i, l := range rec.Lines {
		in.Lines = append(in.Lines, merge(maps.Clone(l), lines[i+1]))
	}
	return in
}

// EditClaim saves ClaimEdit's input as p. It returns the engine's result and
// error, so a case can expect a refusal.
func (e *Env) EditClaim(t testing.TB, p auth.Principal, id int64, header Values, lines map[int]Values) (doc.Result, error) {
	t.Helper()
	return e.Engine.Do(context.Background(), p, expense.ClaimDocType, id, doc.ActionSave, e.ClaimEdit(t, id, header, lines))
}
