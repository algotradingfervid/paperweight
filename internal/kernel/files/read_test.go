package files_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

const msgFileNotFound = "File not found."

// Project ids for the authfake restriction; the fake compares ids only, so no
// seeded database is needed.
const (
	prjMetro    int64 = 1
	prjTechpark int64 = 2
)

// openBytes opens attachment id as p and reads it all; it fails the test on any error.
func openBytes(t *testing.T, env *filestest.Env, p auth.Principal, id int64) ([]byte, files.Meta) {
	t.Helper()
	rc, m, err := env.Store.Open(t.Context(), p, id)
	if err != nil {
		t.Fatalf("Open(%d) as %s error = %v, want nil", id, p.Name, err)
	}
	defer func() { _ = rc.Close() }()
	if _, ok := rc.(*os.File); !ok {
		t.Errorf("Open(%d) reader is %T, want *os.File (handlers type-assert io.ReadSeeker)", id, rc)
	}
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read Open(%d): %v", id, err)
	}
	return b, m
}

// wantNoReader fails when Open returned a reader, and closes it.
func wantNoReader(t *testing.T, label string, rc io.ReadCloser) {
	t.Helper()
	if rc != nil {
		_ = rc.Close()
		t.Errorf("%s returned a reader, want none (no bytes sent)", label)
	}
}

// FILE-025
func TestAttachment_ReadFollowsParentRecordRule(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	priya := env.User("priya@vardhan.test")
	meena := env.User("meena@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 1, "approved", "EX/26-27/00001", ravi.ID)
	env.DocInfo(exp, func(i *doctype.Info) { i.ProjectIDs = []int64{prjTechpark} })

	content := filestest.JPEG(2456789)
	id, err := env.Attach(ravi, exp, "receipt.jpg", content)
	if err != nil {
		t.Fatalf("Attach(receipt.jpg) error = %v, want nil", err)
	}

	env.Auth.Allow(priya.ID, "expense", "read")
	env.Auth.Restrict(priya.ID, []int64{prjMetro}, nil)

	rc, _, err := env.Store.Open(t.Context(), priya, id)
	wantNoReader(t, "Open as Priya (PRJ-METRO only)", rc)
	wantRefused(t, "Open as Priya (PRJ-METRO only)", err, files.KindNotFound, 404, msgFileNotFound)

	list, err := env.Store.List(t.Context(), priya, exp)
	if len(list) != 0 {
		t.Errorf("List as Priya = %+v, want none", list)
	}
	if k := files.KindOf(err); k != files.KindNotFound {
		t.Errorf("List as Priya: KindOf(%v) = %v, want KindNotFound (%v)", err, k, files.KindNotFound)
	}
	var fe *files.Error
	if errors.As(err, &fe) && fe.Status() != 404 {
		t.Errorf("List as Priya: Status() = %d, want 404", fe.Status())
	}

	env.Auth.Allow(meena.ID, "expense", "read")
	got, m := openBytes(t, env, meena, id)
	if !bytes.Equal(got, content) {
		t.Errorf("Open as Meena: %d bytes, want the uploaded %d bytes unchanged", len(got), len(content))
	}
	if m.ID != id || m.Name != "receipt.jpg" || m.Size != int64(len(content)) || m.SHA256 != shaHex(content) || m.MIME != "image/jpeg" {
		t.Errorf("Open as Meena: Meta = %+v, want id %d, receipt.jpg, %d bytes, image/jpeg, sha %s",
			m, id, len(content), shaHex(content))
	}

	list, err = env.Store.List(t.Context(), meena, exp)
	if err != nil {
		t.Fatalf("List as Meena error = %v, want nil", err)
	}
	if len(list) != 1 || list[0].ID != id || list[0].Name != "receipt.jpg" {
		t.Errorf("List as Meena = %+v, want one entry id %d named receipt.jpg", list, id)
	}
}

// FILE-026
func TestAttachment_NoOwnerBypass(t *testing.T) {
	setup := func(t *testing.T) (*filestest.Env, auth.Principal, int64) {
		t.Helper()
		env := filestest.New(t)
		deepak := env.User("deepak@vardhan.test")
		env.Auth.Allow(deepak.ID, "expense", "read", "write")
		exp := env.Doc("expense", 42, "draft", "Draft 42", deepak.ID)
		id, err := env.Attach(deepak, exp, "receipt.jpg", filestest.JPEG(4096))
		if err != nil {
			t.Fatalf("Attach(receipt.jpg) as Deepak error = %v, want nil", err)
		}
		return env, deepak, id
	}

	t.Run("role removed", func(t *testing.T) {
		env, deepak, id := setup(t)
		env.Auth.Deny(deepak.ID, "expense", "read", "write") // his only role is gone

		rc, _, err := env.Store.Open(t.Context(), deepak, id)
		wantNoReader(t, "Open as Deepak without a role", rc)
		wantRefused(t, "Open as Deepak without a role", err, files.KindForbidden, 403,
			"You don't have permission to read Expense Claim. Ask your administrator for a role that allows it.")
	})

	t.Run("record out of scope", func(t *testing.T) {
		env, deepak, id := setup(t)
		env.Auth.Hide("expense", 42)

		rc, _, err := env.Store.Open(t.Context(), deepak, id)
		wantNoReader(t, "Open as Deepak, record hidden", rc)
		wantRefused(t, "Open as Deepak, record hidden", err, files.KindNotFound, 404, msgFileNotFound)
	})
}
