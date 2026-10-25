package files_test

import (
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
)

// Supporting test for CD-156 (no FILE case ID): /files/{id} never points at
// another file, so deleting the newest attachment never hands its id on.
func TestAttachment_IdsNeverReused(t *testing.T) {
	env := filestest.New(t)
	ravi := env.User("ravi@vardhan.test")
	env.Auth.Allow(ravi.ID, "expense", "read", "write")
	exp := env.Doc("expense", 42, "draft", "Draft 42", ravi.ID)

	attach := func(name string, size int) int64 {
		t.Helper()
		id, err := env.Attach(ravi, exp, name, filestest.PDF(size))
		if err != nil {
			t.Fatalf("Attach(%s) error = %v, want nil", name, err)
		}
		return id
	}
	attach("a.pdf", 100)
	newest := attach("b.pdf", 101)
	env.DeleteRow(newest)
	if got := attach("c.pdf", 102); got <= newest {
		t.Errorf("attachment id after deleting newest id %d = %d, want > %d", newest, got, newest)
	}
}
