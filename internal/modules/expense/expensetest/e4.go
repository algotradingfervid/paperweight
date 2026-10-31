package expensetest

import "testing"

// E4 (Advance settlement and invariants) helpers.

// Fingerprint is what WantRefused compares: every claim's and advance's
// state, version, less advance, used amount and paid on, the line counts, the
// audit log and the attachments. FuzzExp_Invariants compares it around each
// refused step, whose refusal text the random sequence can't know.
func (e *Env) Fingerprint(t testing.TB) string {
	t.Helper()
	return e.fingerprint(t)
}
