package goodsreceipt_test

import (
	"fmt"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// MOD-GRN G11: the duplicate challan warning (CD-337 (6); AD-12 rulings).
// A GRN whose challan no. (trimmed, any case) is also on another GRN of the
// same vendor that isn't cancelled (G7's state <> 'cancelled': draft,
// pending, rejected, approved and closed all count) warns. The other GRNs
// are named in id order as "A, B and C", a numberless one as naming.Label
// ("Draft 42", CD-53), when the viewer can read every one of them (CanTx
// read, as G7's duplicate invoice); any unreadable one turns the whole text
// into the "another receipt" form (CD-233, CD-240, CD-248). The text shows
// this GRN's own challan no., trimmed. A blank challan never warns, a GRN
// never matches itself, and another vendor's GRN never matches. It is read
// through goodsreceipt.DuplicateChallan, which the screens add after
// Warnings (S3); it refuses nothing. grntest.GRN gives every receipt
// DC-1042, so these tests make their GRNs with their own challan nos.

// g11Dup is the duplicate challan warning naming list (G7's msgBillDup
// shape, "Invoice %s is also on %s.").
func g11Dup(no, list string) string { return "Challan " + no + " is also on " + list + "." }

// g11DupHidden is the warning when the viewer can't read one of the other
// GRNs (G7's msgBillDupHidden shape).
func g11DupHidden(no string) string { return "Challan " + no + " is also on another receipt." }

// g11Name is how the warning names GRN d: its number, or "Draft {id}".
func g11Name(d doc.Doc) string { return naming.Label(d.Number, d.ID) }

// g11GRN makes a GRN as p on po through the engine with challan no.
// challan (posted as given), one line of 10 on the PO's first line, and
// submits it unless stop is draft; stop pending needs a Goods Receipt rule.
func g11GRN(t *testing.T, env *grntest.Env, p auth.Principal, po doc.Doc, challan string, stop doctype.State) doc.Doc {
	t.Helper()
	h := g2Header(t, env, po)
	h["challan_no"] = challan
	res := g2MustCreate(t, env, p, h, g2Line(po.Lines[0], "10", "", ""))
	if stop != doctype.StateDraft {
		r, err := g2Submit(t, env, p, res.ID)
		if err != nil {
			t.Fatalf("submit GRN %d (challan %q) as %s: %v %+v", res.ID, challan, p.Name, err, g1FieldErrors(err))
		}
		res = r
	}
	if res.State != stop {
		t.Fatalf("GRN %d (challan %q) is %s, want %s", res.ID, challan, res.State, stop)
	}
	return env.Load(t, goodsreceipt.DocType, res.ID)
}

// g11Warn is goodsreceipt.DuplicateChallan for GRN id as p, in a read
// transaction.
func g11Warn(t *testing.T, env *grntest.Env, p auth.Principal, id int64) string {
	t.Helper()
	var out string
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		out, err = goodsreceipt.DuplicateChallan(t.Context(), tx, p, id)
		return err
	}); err != nil {
		t.Fatalf("DuplicateChallan(GRN %d) as %s: %v", id, p.Name, err)
	}
	return out
}

// g11Want asserts GRN id's warning as p is exactly want ("" for none).
func g11Want(t *testing.T, what string, env *grntest.Env, p auth.Principal, id int64, want string) {
	t.Helper()
	if got := g11Warn(t, env, p, id); got != want {
		t.Errorf("%s: DuplicateChallan(GRN %d) as %s\n got %q\nwant %q", what, id, p.Name, got, want)
	}
}

// Rows 1 and 2: the same vendor and challan on another GRN in each state.
// Every state but cancelled warns, naming the other GRN (a pending, rejected
// or draft GRN has no number and is "Draft {id}"); a cancelled one never
// warns. The subject is an approved GRN made before the rule.
func TestGRN_DuplicateChallan_States(t *testing.T) {
	const challan = "DC-7003"
	for _, c := range []struct {
		state doctype.State
		warns bool
	}{
		{doctype.StateDraft, true},
		{doctype.StatePending, true},
		{doctype.StateRejected, true},
		{doctype.StateApproved, true},
		{doctype.StateClosed, true},
		{doctype.StateCancelled, false},
	} {
		t.Run(string(c.state), func(t *testing.T) {
			env := grntest.New(t)
			po := g2RefPO(t, env)
			subject := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
			var other doc.Doc
			switch c.state {
			case doctype.StateDraft, doctype.StateApproved:
				other = g11GRN(t, env, env.Suresh, po, challan, c.state)
			case doctype.StatePending, doctype.StateRejected:
				env.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
				other = g11GRN(t, env, env.Suresh, po, challan, doctype.StatePending)
				if c.state == doctype.StateRejected {
					other = g3Decide(t, env, other.ID, doctype.ActionReject, "Wrong truck")
				}
			case doctype.StateClosed:
				other = g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
				env.ForceClosed(t, other.ID)
				other = env.Load(t, goodsreceipt.DocType, other.ID)
			case doctype.StateCancelled:
				other = g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
				other = env.Cancel(t, env.Ravi, other.ID, g4CancelReason)
			}
			if other.State != c.state {
				t.Fatalf("the other GRN is %s, want %s", other.State, c.state)
			}
			want := ""
			if c.warns {
				want = g11Dup(challan, g11Name(other))
			}
			g11Want(t, "the other GRN "+string(c.state), env, env.Suresh, subject.ID, want)
		})
	}
}

// Row 3: three other GRNs are listed in id order, which differs from their
// number order (submitted C, A, B), with a numberless draft last by id.
func TestGRN_DuplicateChallan_IDOrder(t *testing.T) {
	const challan = "DC-7002"
	env := grntest.New(t)
	po := g2RefPO(t, env)
	subject := g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft)
	var ids [3]int64
	for i := range ids {
		ids[i] = g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft).ID
	}
	draft := g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft)
	for _, i := range []int{2, 0, 1} {
		if _, err := g2Submit(t, env, env.Suresh, ids[i]); err != nil {
			t.Fatalf("submit GRN %d: %v %+v", ids[i], err, g1FieldErrors(err))
		}
	}
	a, b, c := env.Load(t, goodsreceipt.DocType, ids[0]), env.Load(t, goodsreceipt.DocType, ids[1]), env.Load(t, goodsreceipt.DocType, ids[2])
	if c.Number != "GR/26-27/00001" || a.Number != "GR/26-27/00002" || b.Number != "GR/26-27/00003" {
		t.Fatalf("numbers by id %q %q %q, want GR/26-27/00002, 00003, 00001", a.Number, b.Number, c.Number)
	}
	want := g11Dup(challan, "GR/26-27/00002, GR/26-27/00003, GR/26-27/00001 and "+fmt.Sprintf("Draft %d", draft.ID))
	g11Want(t, "three approved GRNs and a draft", env, env.Suresh, subject.ID, want)
}

// Row 4: one duplicate the viewer can't read. Suresh (sites PUNE-WH and
// METRO-SITE) and Priya (project PRJ-METRO), on the real auth.Service,
// can't read the Administrator's TECH-SITE / PRJ-TECHPARK GRN: the text
// names no GRN at all, not even a readable one beside it (before or after
// it in id order), and nothing of the hidden one's number or state. The
// Administrator reads both and is told both, in id order.
func TestGRN_DuplicateChallan_Hidden(t *testing.T) {
	const challan = "DC-7001"
	env := grntest.New(t)
	po := g2RefPO(t, env)
	tech := g6TechPO(t, env)
	subject := g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft)
	hidden := g11GRN(t, env, env.Admin, tech, challan, doctype.StateApproved)
	g11Want(t, "Suresh, only the hidden GRN", env, env.Suresh, subject.ID, g11DupHidden(challan))
	g11Want(t, "Priya, only the hidden GRN", env, env.Priya, subject.ID, g11DupHidden(challan))
	g11Want(t, "the Administrator, only the TECH-SITE GRN", env, env.Admin, subject.ID, g11Dup(challan, hidden.Number))

	// A readable GRN after the hidden one in id order; env2 has it before.
	readable := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
	g11Want(t, "Suresh, hidden then readable", env, env.Suresh, subject.ID, g11DupHidden(challan))
	g11Want(t, "Priya, hidden then readable", env, env.Priya, subject.ID, g11DupHidden(challan))
	g11Want(t, "the Administrator, both", env, env.Admin, subject.ID, g11Dup(challan, hidden.Number+" and "+readable.Number))

	env2 := grntest.New(t)
	po2 := g2RefPO(t, env2)
	tech2 := g6TechPO(t, env2)
	subject2 := g11GRN(t, env2, env2.Suresh, po2, challan, doctype.StateDraft)
	first := g11GRN(t, env2, env2.Suresh, po2, challan, doctype.StateApproved)
	last := g11GRN(t, env2, env2.Admin, tech2, challan, doctype.StateDraft)
	g11Want(t, "Suresh, readable then hidden", env2, env2.Suresh, subject2.ID, g11DupHidden(challan))
	g11Want(t, "the Administrator, readable then hidden draft", env2, env2.Admin, subject2.ID,
		g11Dup(challan, first.Number+" and "+fmt.Sprintf("Draft %d", last.ID)))
}

// Row 5: the comparison is trimmed and case-insensitive, and the text shows
// this GRN's own challan no. trimmed. A GRN alone with its challan doesn't
// warn (never itself), another vendor's GRN with the same challan doesn't,
// and blank challans never warn, even beside each other.
func TestGRN_DuplicateChallan_Matching(t *testing.T) {
	env := grntest.New(t)
	po := g2RefPO(t, env)
	bharat := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved(),
		purchasetest.OrderHeader(map[string]string{"vendor_id": g1MasterID(t, env.DB, "party", "BHARAT")}))

	subject := g11GRN(t, env, env.Suresh, po, "dc-7004 ", doctype.StateDraft)
	g11Want(t, "alone with its challan (itself)", env, env.Suresh, subject.ID, "")
	other := g11GRN(t, env, env.Suresh, bharat, "DC-7004", doctype.StateApproved)
	g11Want(t, "BHARAT's GRN with the same challan", env, env.Suresh, subject.ID, "")
	g11Want(t, "BHARAT's GRN, SHREE's has it", env, env.Suresh, other.ID, "")

	blank := g11GRN(t, env, env.Suresh, po, "", doctype.StateDraft)
	blank2 := g11GRN(t, env, env.Suresh, po, "", doctype.StateDraft)
	g11Want(t, "a blank challan beside another blank", env, env.Suresh, blank.ID, "")
	g11Want(t, "the other blank challan", env, env.Suresh, blank2.ID, "")

	padded := g11GRN(t, env, env.Suresh, po, "  DC-7004", doctype.StateApproved)
	g11Want(t, "dc-7004 against padded DC-7004", env, env.Suresh, subject.ID, g11Dup("dc-7004", padded.Number))
	g11Want(t, "padded DC-7004 against dc-7004", env, env.Suresh, padded.ID, g11Dup("DC-7004", fmt.Sprintf("Draft %d", subject.ID)))
	g11Want(t, "BHARAT's GRN after SHREE's match", env, env.Suresh, other.ID, "")
	g11Want(t, "a blank challan after the match", env, env.Suresh, blank.ID, "")
}

// Row 5 (amendment): an amendment never matches the cancelled receipt it
// amends, but does match another live GRN with its challan.
func TestGRN_DuplicateChallan_Amendment(t *testing.T) {
	const challan = "DC-7005"
	env := grntest.New(t)
	po := g2RefPO(t, env)
	orig := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
	env.Cancel(t, env.Ravi, orig.ID, g4CancelReason)
	am := env.Amend(t, env.Suresh, orig.ID)
	if got := fmt.Sprint(am.Header["challan_no"]); got != challan {
		t.Fatalf("the amendment's challan no. = %q, want %q copied", got, challan)
	}
	g11Want(t, "the amendment beside the cancelled original", env, env.Suresh, am.ID, "")
	live := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
	g11Want(t, "the amendment beside a live GRN", env, env.Suresh, am.ID, g11Dup(challan, live.Number))
}

// Row 6: the warning refuses nothing: a GRN carrying it submits (pending
// under a Goods Receipt rule) and is approved, and still warns after each.
func TestGRN_DuplicateChallan_SubmitApprove(t *testing.T) {
	const challan = "DC-7006"
	env := grntest.New(t)
	po := g2RefPO(t, env)
	other := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
	env.AddRule(t, goodsreceipt.DocType, 0, "Administrator")
	subject := g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft)
	want := g11Dup(challan, other.Number)
	g11Want(t, "the draft", env, env.Suresh, subject.ID, want)
	res, err := g2Submit(t, env, env.Suresh, subject.ID)
	if err != nil || res.State != doctype.StatePending {
		t.Fatalf("submit the warned GRN: %v %+v (state %s), want pending_approval", err, g1FieldErrors(err), res.State)
	}
	g11Want(t, "pending", env, env.Suresh, subject.ID, want)
	if got := g3Decide(t, env, subject.ID, doctype.ActionApprove, ""); got.State != doctype.StateApproved {
		t.Fatalf("approve the warned GRN: state %s, want approved", got.State)
	}
	g11Want(t, "approved", env, env.Suresh, subject.ID, want)
}

// Row 7 (lessons §11): a user holding only read on the Goods Receipt is
// told the other GRN's number; a user with no Goods Receipt right gets the
// "another receipt" form. So the check is read, on each other GRN.
func TestGRN_DuplicateChallan_ReadRight(t *testing.T) {
	const challan = "DC-7007"
	env := grntest.New(t)
	po := g2RefPO(t, env)
	reader := env.ReadOnlyReceipts(t)
	nobody := env.NoReceiptRights(t)
	other := g11GRN(t, env, env.Suresh, po, challan, doctype.StateApproved)
	subject := g11GRN(t, env, env.Suresh, po, challan, doctype.StateDraft)
	g11Want(t, "read-only user", env, reader, subject.ID, g11Dup(challan, other.Number))
	g11Want(t, "no Goods Receipt rights", env, nobody, subject.ID, g11DupHidden(challan))
}
