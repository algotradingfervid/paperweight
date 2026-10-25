package files_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// FILE-045: a receipt from another document and a non-existent attachment id
// get the same 422 refusal.
func TestCheckReceipt_KindAndStatus(t *testing.T) {
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

	const want = "The receipt on line 3 belongs to another document. Attach it to this expense first."
	for _, tc := range []struct {
		name string
		id   int64
	}{
		{"another document's attachment", other},
		{"non-existent attachment", other + 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
				return env.Store.CheckReceipt(t.Context(), tx, exp12, 3, tc.id, "expense")
			})
			var fe *files.Error
			if !errors.As(err, &fe) {
				t.Fatalf("CheckReceipt = %v, want *files.Error", err)
			}
			if fe.Kind != files.KindReceiptMismatch {
				t.Errorf("Kind = %v, want KindReceiptMismatch", fe.Kind)
			}
			if fe.Status() != http.StatusUnprocessableEntity {
				t.Errorf("Status = %d, want 422", fe.Status())
			}
			if fe.Message != want {
				t.Errorf("Message = %q, want %q", fe.Message, want)
			}
		})
	}
}
