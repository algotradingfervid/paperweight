package files_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// storeWithGuards is a Store over env's database, data directory and fakes
// with the given RemoveGuards.
func storeWithGuards(t *testing.T, env *filestest.Env, guards map[string]files.RemoveGuard) *files.Store {
	t.Helper()
	s, err := files.New(files.Config{
		DataDir: env.DataDir, DB: env.DB, Auth: env.Auth, Info: env.Docs, Clock: env.Clock, Audit: env.Audit,
		RemoveGuards: guards,
	})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	return s
}

// spool spools b outside any transaction; the caller attaches it.
func spool(t *testing.T, env *filestest.Env, b []byte) *files.Spooled {
	t.Helper()
	sp, err := env.Store.Spool(bytes.NewReader(b), files.MaxFileBytes)
	if err != nil {
		t.Fatalf("Spool: %v", err)
	}
	t.Cleanup(func() { _ = sp.Cleanup() })
	return sp
}

// FILE-017
func TestAttachment_SameContentSameDocRejected(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	content := filestest.JPEG(48213)
	if _, err := env.Attach(ravi, exp, "receipt.jpg", content); err != nil {
		t.Fatalf("Attach(receipt.jpg) error = %v, want nil", err)
	}

	_, err := env.Attach(ravi, exp, "copy.jpg", content)
	wantRefused(t, "Attach(copy.jpg, same bytes, same expense)", err, files.KindDuplicate, 422,
		"This file is already attached to Draft 42.")

	rows := env.Rows(exp)
	if len(rows) != 1 || rows[0].Name != "receipt.jpg" {
		t.Errorf("Rows(expense 42) = %+v, want only receipt.jpg (count unchanged)", rows)
	}
	if n := len(entriesWith(env, "attachment_added")); n != 1 {
		t.Errorf("attachment_added audit entries = %d, want 1 (the refusal is not audited)", n)
	}
}

// FILE-022
func TestAttachment_LimitConcurrent(t *testing.T) {
	setup := func(t *testing.T) (*filestest.Env, auth.Principal, auth.RecordRef) {
		t.Helper()
		env := filestest.New(t)
		ravi := env.User("ravi@vardhan.test")
		env.Auth.Allow(ravi.ID, "expense", "read", "write")
		exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)
		for i := range 19 {
			if _, err := env.Attach(ravi, exp, "bill-"+strconv.Itoa(i)+".pdf", filestest.PDF(1000+i)); err != nil {
				t.Fatalf("seed Attach #%d error = %v, want nil", i+1, err)
			}
		}
		if n := len(env.Rows(exp)); n != 19 {
			t.Fatalf("seeded rows = %d, want 19", n)
		}
		return env, ravi, exp
	}
	attachIn := func(env *filestest.Env, p auth.Principal, ref auth.RecordRef, name string, sp *files.Spooled) error {
		return env.DB.Write(t.Context(), func(tx *db.Tx) error {
			_, err := env.Store.Attach(t.Context(), tx, p, ref, name, sp)
			return err
		})
	}
	// wantOneEach checks exactly one nil error and one KindLimit refusal, and 20 rows.
	wantOneEach := func(t *testing.T, label string, env *filestest.Env, exp auth.RecordRef, errs []error) {
		t.Helper()
		ok, limited := 0, 0
		for g, err := range errs {
			switch {
			case err == nil:
				ok++
			case files.KindOf(err) == files.KindLimit:
				limited++
				wantRefused(t, label+": loser "+strconv.Itoa(g), err, files.KindLimit, 422, msgLimit)
			default:
				t.Errorf("%s: upload %d error = %v, want nil or KindLimit", label, g, err)
			}
		}
		if ok != 1 || limited != 1 {
			t.Errorf("%s: %d succeeded and %d got KindLimit, want exactly 1 and 1 (errors %v)", label, ok, limited, errs)
		}
		if n := len(env.Rows(exp)); n != 20 {
			t.Errorf("%s: rows = %d, want 20", label, n)
		}
	}

	// Forced order: the second upload starts while the first one's write
	// transaction is still open, after its insert. The count must be read
	// inside the second upload's own BEGIN IMMEDIATE transaction, so it sees 20.
	t.Run("second starts while first is uncommitted", func(t *testing.T) {
		env, ravi, exp := setup(t)
		spA := spool(t, env, filestest.JPEG(5000))
		spB := spool(t, env, filestest.PNG(5000))
		inserted := make(chan struct{})
		errs := make([]error, 2)
		var wg sync.WaitGroup
		wg.Go(func() {
			errs[0] = env.DB.Write(t.Context(), func(tx *db.Tx) error {
				_, err := env.Store.Attach(t.Context(), tx, ravi, exp, "a.jpg", spA)
				close(inserted)
				if err != nil {
					return err
				}
				time.Sleep(100 * time.Millisecond) // hold the transaction open while B starts
				return nil
			})
		})
		wg.Go(func() {
			<-inserted
			errs[1] = attachIn(env, ravi, exp, "b.png", spB)
		})
		wg.Wait()
		if errs[0] != nil {
			t.Errorf("first upload error = %v, want nil", errs[0])
		}
		wantRefused(t, "second upload", errs[1], files.KindLimit, 422, msgLimit)
		wantOneEach(t, "forced order", env, exp, errs)
	})

	// Free race: both spool first, then attach at the same moment.
	t.Run("simultaneous", func(t *testing.T) {
		for i := range 20 {
			env, ravi, exp := setup(t)
			sps := []*files.Spooled{spool(t, env, filestest.JPEG(5000+i)), spool(t, env, filestest.PNG(5000+i))}
			names := []string{"a.jpg", "b.png"}
			start := make(chan struct{})
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for g := range 2 {
				wg.Go(func() {
					<-start
					errs[g] = attachIn(env, ravi, exp, names[g], sps[g])
				})
			}
			close(start)
			wg.Wait()
			wantOneEach(t, "iteration "+strconv.Itoa(i), env, exp, errs)
		}
	})
}

// FILE-030
func TestAttachmentList_Formatting(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	uploads := []struct {
		at   time.Time
		name string
		b    []byte
	}{
		{time.Date(2026, 10, 24, 4, 30, 0, 0, time.UTC), "receipt.jpg", filestest.JPEG(524288)},
		{time.Date(2026, 10, 24, 4, 35, 0, 0, time.UTC), "bill.pdf", filestest.PDF(2456789)},
		{time.Date(2026, 10, 24, 4, 40, 0, 0, time.UTC), "photo.png", filestest.PNG(524288)},
	}
	for _, u := range uploads {
		env.Clock.Set(u.at)
		if _, err := env.Attach(ravi, exp, u.name, u.b); err != nil {
			t.Fatalf("Attach(%s) at %v error = %v, want nil", u.name, u.at, err)
		}
	}

	list, err := env.Store.List(t.Context(), ravi, exp)
	if err != nil {
		t.Fatalf("List error = %v, want nil", err)
	}
	want := []struct {
		name, ist, entry string
		at               time.Time
	}{
		{"photo.png", "24-10-2026 10:10", "photo.png · 512 KB · Ravi Kulkarni · 24-10-2026 10:10", uploads[2].at},
		{"bill.pdf", "24-10-2026 10:05", "bill.pdf · 2.3 MB · Ravi Kulkarni · 24-10-2026 10:05", uploads[1].at},
		{"receipt.jpg", "24-10-2026 10:00", "receipt.jpg · 512 KB · Ravi Kulkarni · 24-10-2026 10:00", uploads[0].at},
	}
	if len(list) != len(want) {
		t.Fatalf("List = %d entries, want %d: %+v", len(list), len(want), list)
	}
	for i, w := range want {
		m := list[i]
		if m.Name != w.name {
			t.Errorf("List[%d].Name = %q, want %q (newest first)", i, m.Name, w.name)
		}
		if !m.UploadedAt.Equal(w.at) {
			t.Errorf("List[%d].UploadedAt = %v, want %v", i, m.UploadedAt, w.at)
		}
		if got := types.FormatIST(m.UploadedAt); got != w.ist {
			t.Errorf("FormatIST(List[%d].UploadedAt) = %q, want %q", i, got, w.ist)
		}
		if got := files.ListEntry(m, "Ravi Kulkarni"); got != w.entry {
			t.Errorf("ListEntry(List[%d]) = %q, want %q", i, got, w.entry)
		}
	}

	for _, c := range []struct {
		n    int64
		want string
	}{{524288, "512 KB"}, {2456789, "2.3 MB"}} {
		if got := files.HumanSize(c.n); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// FILE-045
func TestExpenseLine_ReceiptMustBelongToDoc(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	exp12 := env.Doc("expense", 12, "draft", "Draft 12", ravi.ID)
	exp13 := env.Doc("expense", 13, "draft", "Draft 13", deepak.ID)

	other, err := env.Attach(deepak, exp13, "taxi.jpg", filestest.JPEG(6000))
	if err != nil {
		t.Fatalf("Attach(taxi.jpg) to expense 13 error = %v, want nil", err)
	}
	own, err := env.Attach(ravi, exp12, "hotel.pdf", filestest.PDF(7000))
	if err != nil {
		t.Fatalf("Attach(hotel.pdf) to expense 12 error = %v, want nil", err)
	}

	check := func(line int, id int64) error {
		return env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return env.Store.CheckReceipt(t.Context(), tx, exp12, line, id, "expense")
		})
	}

	err = check(2, other)
	const want = "The receipt on line 2 belongs to another document. Attach it to this expense first."
	if err == nil {
		t.Fatalf("CheckReceipt(line 2, expense 13's attachment) = nil, want %q", want)
	}
	if err.Error() != want {
		t.Errorf("CheckReceipt(line 2, expense 13's attachment) = %q, want %q", err.Error(), want)
	}

	if err := check(1, own); err != nil {
		t.Errorf("CheckReceipt(line 1, expense 12's own attachment) = %v, want nil", err)
	}
}

// FILE-046
func TestAttachment_RemoveBlockedWhileReferencedByLine(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	taxi, err := env.Attach(ravi, exp, "taxi.jpg", filestest.JPEG(8000))
	if err != nil {
		t.Fatalf("Attach(taxi.jpg) error = %v, want nil", err)
	}

	type call struct {
		hasTx              bool
		parentID, attachID int64
	}
	var calls []call
	cleared := false
	store := storeWithGuards(t, env, map[string]files.RemoveGuard{
		"expense": func(_ context.Context, tx *db.Tx, parentID, attachmentID int64) (int, error) {
			calls = append(calls, call{tx != nil, parentID, attachmentID})
			if !cleared && attachmentID == taxi {
				return 2, nil
			}
			return 0, nil
		},
	})
	removeWith := func() error {
		return env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return store.Remove(t.Context(), tx, ravi, taxi)
		})
	}

	err = removeWith()
	wantRefused(t, "Remove(taxi.jpg) while line 2 uses it", err, files.KindInUse, 422,
		"This file is the receipt on line 2. Clear it there first.")
	if want := []call{{true, 42, taxi}}; !reflect.DeepEqual(calls, want) {
		t.Errorf("guard calls = %+v, want %+v", calls, want)
	}
	if rows := env.Rows(exp); len(rows) != 1 || rows[0].ID != taxi {
		t.Errorf("Rows after refused remove = %+v, want taxi.jpg (id %d) kept", rows, taxi)
	}
	if n := len(entriesWith(env, "attachment_removed")); n != 0 {
		t.Errorf("attachment_removed audit entries = %d after refusal, want 0", n)
	}

	cleared = true // the line's receipt is cleared
	if err := removeWith(); err != nil {
		t.Fatalf("Remove(taxi.jpg) after clearing the line error = %v, want nil", err)
	}
	if rows := env.Rows(exp); len(rows) != 0 {
		t.Errorf("Rows after remove = %+v, want none", rows)
	}
	if n := len(entriesWith(env, "attachment_removed")); n != 1 {
		t.Errorf("attachment_removed audit entries = %d, want 1", n)
	}
}

// fileState is every file under <DataDir>/files (tmp included) with its bytes and mtime.
type fileState struct {
	b     string
	mtime time.Time
}

func snapshotFiles(t *testing.T, env *filestest.Env) map[string]fileState {
	t.Helper()
	root := filepath.Join(env.DataDir, "files")
	out := map[string]fileState{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		b, err := os.ReadFile(path) //nolint:gosec // test data dir
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out[filepath.ToSlash(rel)] = fileState{string(b), info.ModTime()}
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot files: %v", err)
	}
	return out
}

// FILE-049
func TestFilesCheck_Report(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	okBytes := filestest.PNG(2000)
	if _, err := env.Attach(ravi, exp, "ok.png", okBytes); err != nil {
		t.Fatalf("Attach(ok.png): %v", err)
	}
	okSHA := shaHex(okBytes)

	missingBytes := filestest.PDF(3000)
	missingID, err := env.Attach(ravi, exp, "missing.pdf", missingBytes)
	if err != nil {
		t.Fatalf("Attach(missing.pdf): %v", err)
	}
	missingSHA := shaHex(missingBytes)
	if err := os.Remove(blobFile(t, env, missingSHA)); err != nil {
		t.Fatalf("remove blob of missing.pdf: %v", err)
	}

	corruptBytes := filestest.JPEG(4000)
	if _, err := env.Attach(ravi, exp, "corrupt.jpg", corruptBytes); err != nil {
		t.Fatalf("Attach(corrupt.jpg): %v", err)
	}
	corruptSHA := shaHex(corruptBytes)
	tampered := bytes.Clone(corruptBytes)
	tampered[len(tampered)-1] ^= 0xFF // same size, different content
	if err := os.WriteFile(blobFile(t, env, corruptSHA), tampered, 0o600); err != nil {
		t.Fatalf("tamper blob of corrupt.jpg: %v", err)
	}

	orphanSHA := shaHex(filestest.PDF(5555))
	writeBlob(t, env, filestest.PDF(5555))

	rowsBefore := env.Rows(exp)
	filesBefore := snapshotFiles(t, env)

	rep, err := env.Store.Check(t.Context())
	if err != nil {
		t.Fatalf("Check error = %v, want nil", err)
	}
	if len(rep.MissingBlobs) != 1 {
		t.Errorf("MissingBlobs = %+v, want one (missing.pdf)", rep.MissingBlobs)
	} else if m := rep.MissingBlobs[0]; m.ID != missingID || m.SHA256 != missingSHA || m.Name != "missing.pdf" {
		t.Errorf("MissingBlobs[0] = %+v, want ID %d, Name missing.pdf, SHA256 %s", m, missingID, missingSHA)
	}
	if want := []string{orphanSHA}; !reflect.DeepEqual(rep.Orphans, want) {
		t.Errorf("Orphans = %q, want %q", rep.Orphans, want)
	}
	if want := []string{corruptSHA}; !reflect.DeepEqual(rep.Corrupt, want) {
		t.Errorf("Corrupt = %q, want %q", rep.Corrupt, want)
	}

	var out bytes.Buffer
	if code := files.WriteCheckReport(&out, rep); code != 1 {
		t.Errorf("WriteCheckReport(findings) = %d, want 1", code)
	}
	text := out.String()
	idRE := regexp.MustCompile(`\b` + strconv.FormatInt(missingID, 10) + `\b`)
	foundMissing := false
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, missingSHA) && idRE.MatchString(line) {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Errorf("WriteCheckReport output has no line with missing.pdf's id %d and hash %s:\n%s", missingID, missingSHA, text)
	}
	for _, sha := range []string{orphanSHA, corruptSHA} {
		if !strings.Contains(text, sha) {
			t.Errorf("WriteCheckReport output lacks %s:\n%s", sha, text)
		}
	}
	if strings.Contains(text, okSHA) {
		t.Errorf("WriteCheckReport output lists the healthy blob %s:\n%s", okSHA, text)
	}
	if code := files.WriteCheckReport(io.Discard, files.CheckReport{}); code != 0 {
		t.Errorf("WriteCheckReport(no findings) = %d, want 0", code)
	}

	if rows := env.Rows(exp); !reflect.DeepEqual(rows, rowsBefore) {
		t.Errorf("rows changed by Check:\n got %+v\nwant %+v", rows, rowsBefore)
	}
	filesAfter := snapshotFiles(t, env)
	if !reflect.DeepEqual(filesAfter, filesBefore) {
		t.Errorf("files changed by Check: before %d files, after %d", len(filesBefore), len(filesAfter))
		for k, v := range filesBefore {
			if a, ok := filesAfter[k]; !ok {
				t.Errorf("  %s removed", k)
			} else if a.b != v.b || !a.mtime.Equal(v.mtime) {
				t.Errorf("  %s changed (mtime %v -> %v)", k, v.mtime, a.mtime)
			}
		}
	}
	if _, err := os.Stat(blobFile(t, env, missingSHA)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing.pdf's blob exists after Check (stat err %v), want still missing", err)
	}
}
