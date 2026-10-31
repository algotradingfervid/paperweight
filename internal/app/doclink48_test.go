package app_test

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// CD-321 on the real auth.Service: a claim's advance shows its number only to
// a viewer who may read the advance. The auditor reads every claim and no
// advance, so the claim's pages show the advance blank; the admin sees it.
func TestApp_DocLinkHiddenFromNonReader(t *testing.T) {
	w := newWorld(t)
	admin, _, _ := w.canonical()
	ctx := context.Background()
	const role, auditor = "Claim Auditor", "auditor@vardhan.test"
	if err := w.app.Auth().CreateRole(ctx, admin, role); err != nil {
		t.Fatal(err)
	}
	if err := w.app.Auth().SetRights(ctx, admin, role, expense.ClaimDocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.app.Auth().CreateUser(ctx, admin, auth.NewUser{Email: auditor, Name: "Asha Auditor", Password: testPassword, Roles: []string{role}}); err != nil {
		t.Fatal(err)
	}

	employee := w.text(`SELECT CAST(id AS TEXT) FROM employee ORDER BY id LIMIT 1`)
	travel := w.text(`SELECT CAST(id AS TEXT) FROM expense_type WHERE code = 'TRAVEL'`)
	adv, err := w.app.Engine().Do(ctx, admin, expense.AdvanceDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": employee, "advance_date": "2026-10-24", "purpose": "Pune Metro site visit"},
		Lines:  []map[string]string{{"expense_type_id": travel, "line_amount": "5000.00"}},
	})
	if err != nil {
		t.Fatalf("save the advance: %v", err)
	}
	// Approved by a fixture query (the approval flow isn't under test), with a
	// number of its own, so the page can't show it for any other reason.
	const number = "EA/26-27/04848"
	if _, err := w.app.DB().SQL().ExecContext(ctx, `UPDATE employee_advance SET number = ?, state = 'approved' WHERE id = ?`, number, adv.ID); err != nil {
		t.Fatal(err)
	}
	claim, err := w.app.Engine().Do(ctx, admin, expense.ClaimDocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{"employee_id": employee, "claim_date": "2026-10-24", "purpose": "Pune Metro site visit", "advance_id": strconv.FormatInt(adv.ID, 10)},
		Lines:  []map[string]string{{"expense_type_id": travel, "expense_date": "2026-10-24", "amount": "1000.00"}},
	})
	if err != nil {
		var he doctype.HTTPError
		_ = errors.As(err, &he)
		t.Fatalf("save the claim: %v %v", err, he.FieldErrors())
	}
	if got := w.text(`SELECT CAST(advance_id AS TEXT) FROM expense_claim WHERE id = ?`, claim.ID); got != strconv.FormatInt(adv.ID, 10) {
		t.Fatalf("claim advance_id = %q, want %d", got, adv.ID)
	}

	page := "/doc/" + expense.ClaimDocType + "/" + strconv.FormatInt(claim.ID, 10)
	for _, c := range []struct {
		email string
		show  bool
	}{{adminEmail, true}, {auditor, false}} {
		t.Run(c.email, func(t *testing.T) {
			b := w.signedIn(c.email)
			r := b.get(page)
			for r.status == http.StatusSeeOther {
				r = b.get(r.location())
			}
			if r.status != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body %.300s", page, r.status, r.body)
			}
			if !strings.Contains(r.body, "Pune Metro site visit") {
				t.Fatalf("the claim's page lacks its purpose; body %.300s", r.body)
			}
			if got := strings.Contains(r.body, number); got != c.show {
				t.Errorf("%s: page shows the advance's number %s = %v, want %v", c.email, number, got, c.show)
			}
		})
	}
}
