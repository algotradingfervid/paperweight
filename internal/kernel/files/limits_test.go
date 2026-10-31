package files_test

import (
	"errors"
	"net/http"
	"reflect"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

const (
	limitMsg20  = "A document can have at most 20 attachments. Remove one first."
	limitMsg100 = "A document can have at most 100 attachments. Remove one first."
)

// MOD-EXP X2, spec §3.3 and §15 item 2: the limit message takes its count from
// the policy; at 20 the text is unchanged.
func TestAttachLimits_CountFromPolicy(t *testing.T) {
	pol100 := files.DefaultPolicy
	pol100.MaxCount = 100

	wantRefused(t, "AttachLimits(DefaultPolicy, 20)", files.AttachLimits(files.DefaultPolicy, "bill.pdf", false, 20),
		files.KindLimit, http.StatusUnprocessableEntity, limitMsg20)
	if err := files.AttachLimits(pol100, "bill.pdf", false, 99); err != nil {
		t.Errorf("AttachLimits(MaxCount 100, 99) = %v, want nil", err)
	}
	wantRefused(t, "AttachLimits(MaxCount 100, 100)", files.AttachLimits(pol100, "bill.pdf", false, 100),
		files.KindLimit, http.StatusUnprocessableEntity, limitMsg100)

	// Through the Store, as the claim's policy is wired (spec §3.3).
	env := filestest.New(t)
	s, err := files.New(files.Config{
		DataDir: env.DataDir, DB: env.DB, Auth: env.Auth, Info: env.Docs, Clock: env.Clock, Audit: env.Audit,
		Policies: map[string]files.Policy{"expense": pol100},
	})
	if err != nil {
		t.Fatalf("files.New: %v", err)
	}
	env.Store = s
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)
	for i := range 100 {
		if _, err := env.Attach(ravi, exp, "receipt.pdf", filestest.PDF(100+i)); err != nil {
			t.Fatalf("Attach #%d: %v, want nil", i+1, err)
		}
	}
	_, err = env.Attach(ravi, exp, "receipt.pdf", filestest.PDF(500))
	wantRefused(t, "101st Attach with MaxCount 100", err, files.KindLimit, http.StatusUnprocessableEntity, limitMsg100)
}

// MOD-EXP X2, FILE-045, CD-142: ReceiptMismatch is the error CheckReceipt
// returns, so Expense's hash check shows files' text.
func TestReceiptMismatch_SameText(t *testing.T) {
	const want = "The receipt on line 2 belongs to another document. Attach it to this expense first."
	got := files.ReceiptMismatch(2, "expense")
	if got == nil {
		t.Fatalf("ReceiptMismatch(2, expense) = nil, want %q", want)
	}
	if got.Kind != files.KindReceiptMismatch {
		t.Errorf("Kind = %v, want KindReceiptMismatch", got.Kind)
	}
	if got.Status() != http.StatusUnprocessableEntity {
		t.Errorf("Status = %d, want 422", got.Status())
	}
	if got.Message != want {
		t.Errorf("Message = %q, want %q", got.Message, want)
	}

	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	deepak := env.User("deepak@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	env.Auth.Allow(deepak.ID, "expense", "read", "write")
	exp12 := env.Doc("expense", 12, "draft", "Draft 12", ravi.ID)
	exp13 := env.Doc("expense", 13, "draft", "Draft 13", deepak.ID)
	other, err := env.Attach(deepak, exp13, "taxi.jpg", filestest.JPEG(6000))
	if err != nil {
		t.Fatalf("Attach(taxi.jpg) to expense 13: %v", err)
	}

	for _, tc := range []struct {
		line  int
		label string
	}{
		{2, "expense"},
		{7, "claim"},
	} {
		err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
			return env.Store.CheckReceipt(t.Context(), tx, exp12, tc.line, other, tc.label)
		})
		var fromCheck *files.Error
		if !errors.As(err, &fromCheck) {
			t.Fatalf("CheckReceipt(line %d, %q) = %v, want *files.Error", tc.line, tc.label, err)
		}
		exported := files.ReceiptMismatch(tc.line, tc.label)
		if exported == nil || !reflect.DeepEqual(*exported, *fromCheck) {
			t.Errorf("ReceiptMismatch(%d, %q) = %+v, want CheckReceipt's refusal %+v", tc.line, tc.label, exported, fromCheck)
		}
	}
}
