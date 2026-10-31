package expense

import "github.com/vardhan/internaltools/internal/kernel/files"

// ClaimPolicy is the attachment policy of expense.Claim: the default types,
// with room for a receipt per line (spec §3.3). internal/app passes it to
// files.New.
var ClaimPolicy = func() files.Policy {
	p := files.DefaultPolicy
	p.MaxCount = 100
	return p
}()

// RemoveGuard refuses removing a claim's attachment while a line holds its
// receipt (spec §3.3, FILE-046). internal/app passes it to files.New for
// expense.Claim. receiptLine reads the attachment with Store.Meta in the
// guard's tx (no rights check, no second connection).
var RemoveGuard files.RemoveGuard = receiptLine
