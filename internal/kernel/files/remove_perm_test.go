package files_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// Remove needs write on the parent; AUTH words the write right as "change" (AUTH-081).
func TestAttachment_RemoveWithoutWriteForbidden(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	env.Auth.Allow(ravi.ID, "expense", "read")
	exp := env.Doc("expense", 42, "draft", "Draft 42", deepak.ID)
	id, err := env.Attach(deepak, exp, "receipt.jpg", filestest.JPEG(4096))
	if err != nil {
		t.Fatalf("Attach(receipt.jpg) as Deepak error = %v, want nil", err)
	}

	err = remove(t, env, ravi, id)
	wantRefused(t, "Remove as Ravi without write", err, files.KindForbidden, 403,
		"You don't have permission to change Expense Claim. Ask your administrator for a role that allows it.")
	if rows := env.Rows(exp); len(rows) != 1 || rows[0].ID != id {
		t.Errorf("Rows after refused Remove = %+v, want row %d kept", rows, id)
	}
	if removed := entriesWith(env, "attachment_removed"); len(removed) != 0 {
		t.Errorf("attachment_removed entries after refusal = %+v, want none", removed)
	}
}

// List needs read on the parent.
func TestAttachment_ListWithoutReadForbidden(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", deepak.ID)
	if _, err := env.Attach(deepak, exp, "receipt.jpg", filestest.JPEG(4096)); err != nil {
		t.Fatalf("Attach(receipt.jpg) as Deepak error = %v, want nil", err)
	}

	got, err := env.Store.List(t.Context(), ravi, exp)
	if got != nil {
		t.Errorf("List as Ravi without read = %+v, want nil", got)
	}
	wantRefused(t, "List as Ravi without read", err, files.KindForbidden, 403,
		"You don't have permission to read Expense Claim. Ask your administrator for a role that allows it.")
}
