package files_test

import (
	"bytes"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	msgParentMissing = "Save the document before attaching files."
	msgCancelled     = "This document is cancelled. Attach files to its amended copy instead."
	msgLimit         = "A document can have at most 20 attachments. Remove one first."
)

// FILE-023
func TestAttachment_ParentMustExist(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	missing := auth.RecordRef{DocType: "expense", ID: 999999, CreatedBy: ravi.ID} // never saved

	sp, err := env.Store.Spool(bytes.NewReader(filestest.PDF(4096)), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	// No Cleanup here: Attach itself must remove the temp file on refusal.
	err = env.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := env.Store.Attach(t.Context(), tx, ravi, missing, "bill.pdf", sp)
		return err
	})
	wantRefused(t, "Attach(expense 999999)", err, files.KindParentMissing, 404, msgParentMissing)

	if rows := env.Rows(missing); len(rows) != 0 {
		t.Errorf("Rows(expense 999999) = %d rows, want 0: %+v", len(rows), rows)
	}
	if got := tmpEntries(t, env); len(got) != 0 {
		t.Errorf("files/tmp = %q after refusal, want empty (spooled temp removed)", got)
	}
	if got := env.Blobs(); len(got) != 0 {
		t.Errorf("Blobs() = %q after refusal, want none (parent checks run before placeBlob)", got)
	}
}

// FILE-024
func TestAttachment_AddNeedsParentWrite(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	meena := env.User("meena@vardhan.test")
	env.Auth.Allow(meena.ID, "purchase_order", "read")
	po := env.Doc("purchase_order", 12, "approved", "PO/26-27/00012", ravi.ID)

	_, err := env.Attach(meena, po, "quote.pdf", filestest.PDF(2048))
	wantRefused(t, "Attach as Meena (read only)", err, files.KindForbidden, 403,
		"You don't have permission to add files to PO/26-27/00012.")

	if rows := env.Rows(po); len(rows) != 0 {
		t.Errorf("Rows(purchase_order 12) = %d rows, want 0: %+v", len(rows), rows)
	}
}

// FILE-040
func TestAttachment_AddAfterApproval(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	suresh := env.User("suresh@vardhan.test")
	env.Auth.Allow(suresh.ID, "grn", "read", "write")
	gr := env.Doc("grn", 34, "approved", "GR/26-27/00034", ravi.ID)

	before, err := env.Docs.Info(t.Context(), nil, "grn", 34)
	if err != nil {
		t.Fatalf("docfake Info before: %v", err)
	}

	content := filestest.PDF(48213)
	sha := shaHex(content)
	id, err := env.Attach(suresh, gr, "vendor-bill.pdf", content)
	if err != nil {
		t.Fatalf("Attach(vendor-bill.pdf) to approved GR error = %v, want nil", err)
	}
	if rows := env.Rows(gr); len(rows) != 1 || rows[0].ID != id || rows[0].Name != "vendor-bill.pdf" {
		t.Errorf("Rows(grn 34) = %+v, want one row id %d named vendor-bill.pdf", rows, id)
	}

	after, err := env.Docs.Info(t.Context(), nil, "grn", 34)
	if err != nil {
		t.Fatalf("docfake Info after: %v", err)
	}
	if after.State != "approved" {
		t.Errorf("document state = %q after attach, want %q (FILE never changes state)", after.State, "approved")
	}
	if !reflect.DeepEqual(after, before) {
		t.Errorf("document Info changed by attach:\n got %+v\nwant %+v", after, before)
	}
	if calls := env.Docs.Calls(); len(calls) != 0 {
		t.Errorf("docfake Do calls = %+v, want none", calls)
	}

	entries := env.Audit.Entries()
	if len(entries) != 1 {
		t.Fatalf("audit entries = %d, want 1: %+v", len(entries), entries)
	}
	got := entries[0]
	wantAt := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if !got.At.Equal(wantAt) {
		t.Errorf("audit At = %v, want %v", got.At, wantAt)
	}
	got.At = time.Time{}
	want := audit.Entry{
		DocType: "grn",
		DocID:   34,
		Action:  "attachment_added",
		Changes: []audit.Change{
			{Field: "file_name", After: "vendor-bill.pdf"},
			{Field: "size", After: strconv.Itoa(len(content))},
			{Field: "sha256", After: sha},
		},
		By: suresh.ID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audit entry =\n %+v\nwant\n %+v", got, want)
	}
}

// FILE-041
func TestAttachment_AddToCancelledBlocked(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "purchase_order", "read", "write")
	po := env.Doc("purchase_order", 12, "cancelled", "PO/26-27/00012", ravi.ID)

	_, err := env.Attach(ravi, po, "quote.pdf", filestest.PDF(2048))
	wantRefused(t, "Attach to cancelled PO", err, files.KindCancelled, 422, msgCancelled)

	if rows := env.Rows(po); len(rows) != 0 {
		t.Errorf("Rows(purchase_order 12) = %d rows, want 0: %+v", len(rows), rows)
	}
}

// FILE-021
func TestAttachment_PerDocumentLimit(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	ids := make([]int64, 0, 20)
	for i := range 20 {
		id, err := env.Attach(ravi, exp, "bill-"+strconv.Itoa(i)+".pdf", filestest.PDF(1000+i))
		if err != nil {
			t.Fatalf("Attach #%d error = %v, want nil", i+1, err)
		}
		ids = append(ids, id)
	}

	_, err := env.Attach(ravi, exp, "bill-20.pdf", filestest.PDF(1020))
	wantRefused(t, "Attach #21", err, files.KindLimit, 422, msgLimit)
	if rows := env.Rows(exp); len(rows) != 20 {
		t.Errorf("Rows after the 21st = %d, want 20", len(rows))
	}

	env.DeleteRow(ids[0])
	if _, err := env.Attach(ravi, exp, "bill-21.pdf", filestest.PDF(1021)); err != nil {
		t.Errorf("Attach after removing one error = %v, want nil", err)
	}
	if rows := env.Rows(exp); len(rows) != 20 {
		t.Errorf("Rows after remove and re-add = %d, want 20", len(rows))
	}
}

// FILE-029
func TestAttachment_DedupeNoLeak(t *testing.T) {
	env := filestest.New(t)
	suresh := env.User("suresh@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(suresh.ID, "expense", "read", "write")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	sExp := env.Doc("expense", 7, "draft", "Draft 7", suresh.ID)
	dExp := env.Doc("expense", 8, "draft", "Draft 8", deepak.ID)

	salary := filestest.PDF(30000)
	sID, err := env.Attach(suresh, sExp, "salary.pdf", salary)
	if err != nil {
		t.Fatalf("Suresh Attach(salary.pdf) error = %v, want nil", err)
	}
	env.Auth.Hide("expense", 7) // Deepak can't read Suresh's expense (CD-56)

	dupID, dupErr := env.Attach(deepak, dExp, "salary.pdf", salary)
	if dupErr != nil {
		t.Fatalf("Deepak Attach(identical bytes) error = %v, want nil", dupErr)
	}
	if dt, pid := env.RowParent(dupID); dt != "expense" || pid != 8 {
		t.Errorf("RowParent(%d) = (%q, %d), want (\"expense\", 8)", dupID, dt, pid)
	}
	if rows := env.Rows(sExp); len(rows) != 1 || rows[0].ID != sID {
		t.Errorf("Rows(expense 7) = %+v, want only Suresh's row %d", rows, sID)
	}
	dupRows := env.Rows(dExp)
	if len(dupRows) != 1 || dupRows[0].ID != dupID {
		t.Fatalf("Rows(expense 8) = %+v, want only Deepak's row %d", dupRows, dupID)
	}
	dupMeta := dupRows[0]

	rc, _, err := env.Store.Open(t.Context(), deepak, sID)
	if rc != nil {
		_ = rc.Close()
		t.Errorf("Open(Suresh's row) as Deepak returned a reader, want none")
	}
	if k := files.KindOf(err); k != files.KindNotFound {
		t.Errorf("Open(Suresh's row) as Deepak: KindOf(%v) = %v, want KindNotFound (%v)", err, k, files.KindNotFound)
	}

	freshID, freshErr := env.Attach(deepak, dExp, "fresh.pdf", filestest.PDF(30001))
	if !reflect.DeepEqual(freshErr, dupErr) {
		t.Errorf("new-content Attach error = %v, dedupe Attach error = %v, want the same", freshErr, dupErr)
	}
	var freshMeta files.Meta
	for _, m := range env.Rows(dExp) {
		if m.ID == freshID {
			freshMeta = m
		}
	}
	if freshMeta.ID == 0 {
		t.Fatalf("Rows(expense 8) has no row %d for the new-content upload", freshID)
	}
	strip := func(m files.Meta) files.Meta {
		m.ID, m.Name, m.Size, m.SHA256 = 0, "", 0, ""
		return m
	}
	if a, b := strip(dupMeta), strip(freshMeta); !reflect.DeepEqual(a, b) {
		t.Errorf("dedupe and new-content Meta differ beyond ID, Name, Size, SHA256:\n dedupe %+v\n    new %+v", a, b)
	}
}
