package files_test

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const msgRemoveLocked = "Files can't be removed after the document is submitted. Cancel and amend it to change files."

// remove calls Store.Remove inside db.Write.
func remove(t *testing.T, env *filestest.Env, p auth.Principal, id int64) error {
	t.Helper()
	return env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return env.Store.Remove(t.Context(), tx, p, id)
	})
}

// entriesWith returns the recorded audit entries with this action.
func entriesWith(env *filestest.Env, action string) []audit.Entry {
	var out []audit.Entry
	for _, e := range env.Audit.Entries() {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// FILE-038
func TestAttachment_RemoveFromDraftAudited(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", deepak.ID)

	content := filestest.JPEG(2456789)
	sha := shaHex(content)
	id, err := env.Attach(ravi, exp, "receipt.jpg", content)
	if err != nil {
		t.Fatalf("Attach(receipt.jpg) error = %v, want nil", err)
	}

	if err := remove(t, env, ravi, id); err != nil {
		t.Fatalf("Remove(receipt.jpg) from draft error = %v, want nil", err)
	}
	if rows := env.Rows(exp); len(rows) != 0 {
		t.Errorf("Rows(expense 42) after Remove = %+v, want none", rows)
	}

	removed := entriesWith(env, "attachment_removed")
	if len(removed) != 1 {
		t.Fatalf("attachment_removed audit entries = %d, want 1: %+v", len(removed), env.Audit.Entries())
	}
	got := removed[0]
	wantAt := time.Date(2026, 10, 24, 5, 0, 0, 0, time.UTC)
	if !got.At.Equal(wantAt) {
		t.Errorf("audit At = %v, want %v", got.At, wantAt)
	}
	got.At = time.Time{}
	want := audit.Entry{
		DocType: "expense",
		DocID:   42,
		Action:  "attachment_removed",
		Changes: []audit.Change{
			{Field: "file_name", Before: "receipt.jpg"},
			{Field: "size", Before: "2456789"},
			{Field: "sha256", Before: sha},
		},
		By: ravi.ID,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("audit entry =\n %+v\nwant\n %+v", got, want)
	}
}

// FILE-039
func TestAttachment_RemoveBlockedAfterSubmit(t *testing.T) {
	for _, tc := range []struct {
		state   string
		allowed bool
	}{
		{"pending_approval", false},
		{"approved", false},
		{"closed", false},
		{"cancelled", false},
		{"rejected", true},
	} {
		t.Run(tc.state, func(t *testing.T) {
			env := filestest.New(t)
			ravi := env.User("ravi@vardhan.test")
			env.Auth.Allow(ravi.ID, "expense", "read", "write")
			exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)
			id, err := env.Attach(ravi, exp, "receipt.jpg", filestest.JPEG(4096))
			if err != nil {
				t.Fatalf("Attach(receipt.jpg) error = %v, want nil", err)
			}
			env.SetState(exp, tc.state)

			err = remove(t, env, ravi, id)
			rows := env.Rows(exp)
			removed := entriesWith(env, "attachment_removed")
			if tc.allowed {
				if err != nil {
					t.Fatalf("Remove in %s error = %v, want nil", tc.state, err)
				}
				if len(rows) != 0 {
					t.Errorf("Rows after Remove in %s = %+v, want none", tc.state, rows)
				}
				if len(removed) != 1 {
					t.Errorf("attachment_removed entries = %d, want 1", len(removed))
				}
				return
			}
			wantRefused(t, "Remove in "+tc.state, err, files.KindRemoveLocked, 422, msgRemoveLocked)
			if len(rows) != 1 || rows[0].ID != id {
				t.Errorf("Rows after refused Remove in %s = %+v, want row %d kept", tc.state, rows, id)
			}
			if len(removed) != 0 {
				t.Errorf("attachment_removed entries after refusal = %+v, want none", removed)
			}
		})
	}
}

// amended is FILE-043's setup: cancelled PO/26-27/00001 with quote.pdf and
// drawing.pdf uploaded by Priya, copied by CopyAll into Ravi's Draft 43 at 05:10Z.
type amended struct {
	env           *filestest.Env
	ravi, priya   auth.Principal
	from, to      auth.RecordRef
	quote, sketch []byte
	blobsBefore   []string
	copyErr       error
}

var amendAt = time.Date(2026, 10, 24, 5, 10, 0, 0, time.UTC)

func amend(t *testing.T) amended {
	t.Helper()
	a := amended{env: filestest.New(t)}
	env := a.env
	a.ravi = env.User("ravi@vardhan.test")
	a.priya = env.User("priya@vardhan.test")
	env.Auth.Allow(a.priya.ID, "purchase_order", "read", "write")
	env.Auth.Allow(a.ravi.ID, "purchase_order", "read", "write")
	a.from = env.Doc("purchase_order", 1, "draft", "PO/26-27/00001", a.priya.ID)

	a.quote, a.sketch = filestest.PDF(52000), filestest.PDF(184320)
	if _, err := env.Attach(a.priya, a.from, "quote.pdf", a.quote); err != nil {
		t.Fatalf("Attach(quote.pdf) error = %v, want nil", err)
	}
	env.Clock.Advance(time.Minute)
	if _, err := env.Attach(a.priya, a.from, "drawing.pdf", a.sketch); err != nil {
		t.Fatalf("Attach(drawing.pdf) error = %v, want nil", err)
	}
	env.SetState(a.from, "cancelled")
	a.blobsBefore = env.Blobs()

	a.to = env.Doc("purchase_order", 43, "draft", "Draft 43", a.ravi.ID)
	env.Clock.Set(amendAt)
	a.copyErr = env.DB.Write(t.Context(), func(tx *db.Tx) error {
		return env.Store.CopyAll(t.Context(), tx, a.from, a.to)
	})
	return a
}

// FILE-043
func TestAttachment_CopiedOnAmend(t *testing.T) {
	a := amend(t)
	env := a.env
	if a.copyErr != nil {
		t.Fatalf("CopyAll(PO 1 → Draft 43) error = %v, want nil", a.copyErr)
	}

	orig := env.Rows(a.from)
	copies := env.Rows(a.to)
	if len(orig) != 2 {
		t.Fatalf("Rows(purchase_order 1) after copy = %d, want 2 (originals kept): %+v", len(orig), orig)
	}
	if len(copies) != 2 {
		t.Fatalf("Rows(purchase_order 43) = %d, want 2: %+v", len(copies), copies)
	}
	type key struct {
		Name, MIME, SHA256 string
		Size               int64
	}
	keys := func(ms []files.Meta) []key {
		out := make([]key, 0, len(ms))
		for _, m := range ms {
			out = append(out, key{m.Name, m.MIME, m.SHA256, m.Size})
		}
		slices.SortFunc(out, func(x, y key) int {
			if x.Name < y.Name {
				return -1
			}
			if x.Name > y.Name {
				return 1
			}
			return 0
		})
		return out
	}
	if got, want := keys(copies), keys(orig); !reflect.DeepEqual(got, want) {
		t.Errorf("copied rows (name, type, sha256, size) = %+v, want the originals %+v", got, want)
	}
	wantKeys := []key{
		{"drawing.pdf", "application/pdf", shaHex(a.sketch), int64(len(a.sketch))},
		{"quote.pdf", "application/pdf", shaHex(a.quote), int64(len(a.quote))},
	}
	if got := keys(copies); !reflect.DeepEqual(got, wantKeys) {
		t.Errorf("copied rows = %+v, want %+v", got, wantKeys)
	}
	for _, m := range copies {
		if m.UploadedBy != a.ravi.ID {
			t.Errorf("copy %s UploadedBy = %v, want Ravi %v (to.CreatedBy, not Priya %v)", m.Name, m.UploadedBy, a.ravi.ID, a.priya.ID)
		}
		if !m.UploadedAt.Equal(amendAt) {
			t.Errorf("copy %s UploadedAt = %v, want the amend time %v", m.Name, m.UploadedAt, amendAt)
		}
	}
	for _, m := range orig {
		if m.UploadedBy != a.priya.ID {
			t.Errorf("original %s UploadedBy = %v, want Priya %v (unchanged)", m.Name, m.UploadedBy, a.priya.ID)
		}
	}
	if got := env.Blobs(); !reflect.DeepEqual(got, a.blobsBefore) {
		t.Errorf("Blobs() after copy = %q, want unchanged %q (no new blob)", got, a.blobsBefore)
	}

	var onCopy []audit.Entry
	for _, e := range env.Audit.Entries() {
		if e.DocType == "purchase_order" && e.DocID == 43 {
			onCopy = append(onCopy, e)
		}
	}
	if len(onCopy) != 2 {
		t.Fatalf("audit entries on purchase_order 43 = %d, want 2: %+v", len(onCopy), onCopy)
	}
	byName := map[string][]byte{"quote.pdf": a.quote, "drawing.pdf": a.sketch}
	seen := map[string]bool{}
	for _, e := range onCopy {
		if e.Action != "attachment_added" {
			t.Errorf("copy audit Action = %q, want %q", e.Action, "attachment_added")
		}
		if e.Reason != "copied from PO/26-27/00001" {
			t.Errorf("copy audit Reason = %q, want %q", e.Reason, "copied from PO/26-27/00001")
		}
		if e.By != a.ravi.ID {
			t.Errorf("copy audit By = %v, want Ravi %v (the user who amended)", e.By, a.ravi.ID)
		}
		if !e.At.Equal(amendAt) {
			t.Errorf("copy audit At = %v, want %v", e.At, amendAt)
		}
		if len(e.Changes) != 3 {
			t.Errorf("copy audit Changes = %+v, want file_name, size, sha256", e.Changes)
			continue
		}
		name := e.Changes[0].After
		content, ok := byName[name]
		if !ok {
			t.Errorf("copy audit file_name = %q, want quote.pdf or drawing.pdf", name)
			continue
		}
		seen[name] = true
		want := []audit.Change{
			{Field: "file_name", After: name},
			{Field: "size", After: strconv.Itoa(len(content))},
			{Field: "sha256", After: shaHex(content)},
		}
		if !reflect.DeepEqual(e.Changes, want) {
			t.Errorf("copy audit Changes = %+v, want %+v", e.Changes, want)
		}
	}
	if len(seen) != 2 {
		t.Errorf("copy audit entries cover %v, want both quote.pdf and drawing.pdf", seen)
	}
}

// FILE-044
func TestAttachment_AmendCopyIndependent(t *testing.T) {
	a := amend(t)
	env := a.env
	if a.copyErr != nil {
		t.Fatalf("CopyAll(PO 1 → Draft 43) error = %v, want nil", a.copyErr)
	}

	var copyDrawing int64
	for _, m := range env.Rows(a.to) {
		if m.Name == "drawing.pdf" {
			copyDrawing = m.ID
		}
	}
	if copyDrawing == 0 {
		t.Fatalf("Rows(purchase_order 43) has no drawing.pdf: %+v", env.Rows(a.to))
	}
	if err := remove(t, env, a.ravi, copyDrawing); err != nil {
		t.Fatalf("Remove(drawing.pdf) from Draft 43 error = %v, want nil", err)
	}

	left, err := env.Store.List(t.Context(), a.ravi, a.to)
	if err != nil {
		t.Fatalf("List(Draft 43) error = %v, want nil", err)
	}
	if len(left) != 1 || left[0].Name != "quote.pdf" {
		t.Errorf("List(Draft 43) after remove = %+v, want only quote.pdf", left)
	}

	list, err := env.Store.List(t.Context(), a.ravi, a.from)
	if err != nil {
		t.Fatalf("List(PO/26-27/00001) error = %v, want nil", err)
	}
	names := make([]string, 0, len(list))
	for _, m := range list {
		names = append(names, m.Name)
	}
	// drawing.pdf was uploaded a minute after quote.pdf; List is newest first.
	if want := []string{"drawing.pdf", "quote.pdf"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("List(PO/26-27/00001) names = %q, want %q", names, want)
	}
	got, m := openBytes(t, env, a.ravi, list[0].ID)
	if m.Name != "drawing.pdf" {
		t.Errorf("Open(original drawing.pdf) Meta.Name = %q, want drawing.pdf", m.Name)
	}
	if !bytes.Equal(got, a.sketch) {
		t.Errorf("Open(original drawing.pdf): %d bytes, want the uploaded %d bytes", len(got), len(a.sketch))
	}
}
