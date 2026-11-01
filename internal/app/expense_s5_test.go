package app_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/modules/expense"
)

// MOD-EXP S5, CD-321 (M0-48) on the check page: a viewer who may write the
// claim but can't read its advance sees no advance number anywhere on the
// page. With an advance the totals are drawn and cover advance_id, so the
// number could only come from Less advance's label (readableAdvance). The
// Administrator, who reads the advance, sees it, so the case can't pass for
// lack of the number.
func TestS5_CheckPageHidesUnreadableAdvance(t *testing.T) {
	s := s1New(t)
	ctx := context.Background()
	const role, clerk = "Claim Clerk", "clerk@vardhan.test"
	if err := s.app.Auth().CreateRole(ctx, s.admin, role); err != nil {
		t.Fatal(err)
	}
	all := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeAll}
	if err := s.app.Auth().SetRights(ctx, s.admin, role, expense.ClaimDocType, all); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.app.Auth().CreateUser(ctx, s.admin, auth.NewUser{Email: clerk, Name: "Chitra Clerk", Password: testPassword, Roles: []string{role}}); err != nil {
		t.Fatal(err)
	}

	deepak, priya := s.principal(deepakID), s.principal(priyaID)
	adv := s.advance(deepak, priya, "4000.00", true)
	number := s.text(`SELECT number FROM employee_advance WHERE id = ?`, adv)
	c := s.claim(deepak, map[string]string{"advance_id": strconv.FormatInt(adv, 10)}, nil)
	check := s1URL(c.ID, "/edit/check")
	s1Has(t, "the Administrator's check page", s1Page(t, s.signedIn(adminEmail), check).pageText(), "Less advance ("+number+")")
	s1Lacks(t, "the clerk's check page", s1Page(t, s.signedIn(clerk), check).pageText(), number)
}
