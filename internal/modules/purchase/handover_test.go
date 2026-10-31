package purchase_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// PO spec §21.2 and §22 texts, copied exactly (CD-142: the hand-over texts
// are purchase's, the note texts modkit's).
const (
	hoNotDraft    = "An approved, closed or cancelled order can't be handed over."
	hoPending     = "Withdraw this order before handing it over."
	hoSameOwner   = "This order is already %s's."
	hoCantCreate  = "%s can't raise purchase orders. Pick someone with the Purchase User role."
	hoCantSee     = "%s can't see this order's project or site. Pick someone else."
	hoDisabled    = "%s can't sign in at the moment. Pick someone else."
	hoPick        = "Pick who takes over this order."                                                     // UX 5.12, msgPickOwner
	hoTooLong     = "A comment can be at most 1,000 characters."                                          // spec §22, modkit's
	hoStale       = "Someone else changed %s after you opened it. Reload it and make your changes again." // DOC-069
	hoHistoryText = "Handed over from %s to %s"                                                           // DOC's reassign history text (CD-259)
	hoAction      = "reassign"                                                                            // DOC's reassign audit Action (CD-259)
)

// hoMissing are the 404 texts a missing PO may get: AUTH's (CD-245) or DOC's
// missing answer, which Reassign gives (CD-259 "load"). A hidden PO always
// gets AUTH's.
var hoMissing = []string{auth.MsgNotFound, "This document no longer exists. It may have been deleted."}

// hoSnap is what a refused hand-over leaves as it was: the PO's owner,
// version and state, and the row counts of every table a hand-over touches
// (CD-272 "rollback by row counts").
type hoSnap struct {
	Owner    types.UserID
	Version  int
	State    doc.State
	Orders   int // purchase_order rows
	Comments int // module_comment rows
	Audit    int // audit_log rows
}

func hoSnapshot(t *testing.T, env *purchasetest.Env, id int64) hoSnap {
	t.Helper()
	var s hoSnap
	err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		d, err := doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		if err != nil {
			return err
		}
		s.Owner, s.Version, s.State = d.CreatedBy, d.Version, d.State
		s.Audit, err = audit.Count(t.Context(), tx)
		return err
	})
	if err != nil {
		t.Fatalf("snapshot PO %d: %v", id, err)
	}
	s.Orders = countRows(t, env.DB, "purchase_order", "1 = 1")
	s.Comments = countRows(t, env.DB, "module_comment", "1 = 1")
	return s
}

func hoUnchanged(t *testing.T, env *purchasetest.Env, id int64, before hoSnap) {
	t.Helper()
	if after := hoSnapshot(t, env, id); after != before {
		t.Errorf("refused hand-over changed something: before %+v, after %+v", before, after)
	}
}

// hoRefused fails unless err is a doctype.HTTPError with text msg. status 0
// accepts 409 or 422 (the spec fixes no status for a state refusal); field
// "to" also requires the field error {Field: "to", Message: msg} (plan P1 Do 3:
// the person-specific hand-over errors are at the radios).
func hoRefused(t *testing.T, err error, msg string, status int, field string) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("err = %v (%T), want a doctype.HTTPError %q", err, err, msg)
	}
	if err.Error() != msg {
		t.Errorf("err = %q, want %q", err.Error(), msg)
	}
	switch {
	case status == 0 && he.Status() != http.StatusConflict && he.Status() != http.StatusUnprocessableEntity:
		t.Errorf("status = %d, want 409 or 422", he.Status())
	case status != 0 && he.Status() != status:
		t.Errorf("status = %d, want %d", he.Status(), status)
	}
	if field != "" {
		want := doctype.FieldError{Field: field, Message: msg}
		if !slices.Contains(he.FieldErrors(), want) {
			t.Errorf("FieldErrors = %+v, want %+v among them", he.FieldErrors(), want)
		}
	}
}

// hoHandOver runs Service.HandOver on a pool of one, so a second connection
// inside one of its transactions fails the test (CD-213, CD-224).
func hoHandOver(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, version int, to types.UserID, note string) error {
	t.Helper()
	return testkit.NoSecondConn(t, env.DB, func(ctx context.Context) error {
		return env.Service.HandOver(ctx, p, id, version, to, note)
	})
}

func hoState(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) (purchase.HandOverState, error) {
	t.Helper()
	var st purchase.HandOverState
	err := testkit.NoSecondConn(t, env.DB, func(ctx context.Context) error {
		var err error
		st, err = env.Service.HandOverState(ctx, p, id)
		return err
	})
	return st, err
}

func hoLoad(t *testing.T, env *purchasetest.Env, id int64) doc.Doc {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, purchase.DocType, id)
		return err
	}); err != nil {
		t.Fatalf("load PO %d: %v", id, err)
	}
	return d
}

// hoHistory is the PO's history as p reads it (doc.History, the page's source).
func hoHistory(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) []audit.Entry {
	t.Helper()
	h, err := doc.History(t.Context(), env.DB, env.Auth, p, purchase.DocType, id)
	if err != nil {
		t.Fatalf("History of PO %d as %s: %v", id, p.Name, err)
	}
	return h
}

func hoComments(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) []modkit.Comment {
	t.Helper()
	cs, err := env.Comments.ListComments(t.Context(), p, purchase.DocType, id)
	if err != nil {
		t.Fatalf("ListComments of PO %d as %s: %v", id, p.Name, err)
	}
	return cs
}

// hoWantHandedOver checks one successful hand-over from a to b by p: owner b,
// version up by one, one new history row by p with DOC's text, and, when note
// isn't blank, one new comment by p with the trimmed note (none otherwise).
func hoWantHandedOver(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, before hoSnap, histBefore, commentsBefore int, a, b auth.Principal, note string) {
	t.Helper()
	d := hoLoad(t, env, id)
	if d.CreatedBy != b.ID || d.Version != before.Version+1 || d.State != before.State {
		t.Errorf("after hand-over: created_by %d, version %d, state %s; want %d (%s), %d, %s",
			d.CreatedBy, d.Version, d.State, b.ID, b.Name, before.Version+1, before.State)
	}
	hist := hoHistory(t, env, b, id)
	if len(hist) != histBefore+1 {
		t.Fatalf("history rows = %d, want %d (one new)", len(hist), histBefore+1)
	}
	last := hist[len(hist)-1]
	if want := fmt.Sprintf(hoHistoryText, a.Name, b.Name); last.Action != hoAction || last.By != p.ID || last.Reason != want {
		t.Errorf("new history row = {Action %q, By %d, Reason %q}, want {%q, %d (%s), %q}",
			last.Action, last.By, last.Reason, hoAction, p.ID, p.Name, want)
	}
	cs := hoComments(t, env, b, id)
	trimmed := strings.TrimSpace(note)
	if trimmed == "" {
		if len(cs) != commentsBefore {
			t.Errorf("comments = %d after a hand-over with a blank note, want %d (none added): %+v", len(cs), commentsBefore, cs)
		}
		return
	}
	if len(cs) != commentsBefore+1 {
		t.Fatalf("comments = %d, want %d (the note)", len(cs), commentsBefore+1)
	}
	if c := cs[len(cs)-1]; c.Body != trimmed || c.CreatedBy != p.ID {
		t.Errorf("note comment = {Body %q, CreatedBy %d}, want {%q, %d (%s, the person handing over)}", c.Body, c.CreatedBy, trimmed, p.ID, p.Name)
	}
}

// hoSave saves a remark on draft id as p at version: the edit an owner may
// make and nobody else may.
func hoSave(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64, version int) (doc.Result, error) {
	t.Helper()
	return env.Engine.Do(t.Context(), p, purchase.DocType, id, doctype.ActionSave, doc.ActionInput{
		Version: version, Values: map[string]string{"remarks": "Checked by " + p.Name}, Only: []string{"remarks"},
	})
}

func hoStatus(t *testing.T, err error, status int) {
	t.Helper()
	var he doctype.HTTPError
	if !errors.As(err, &he) || he.Status() != status {
		t.Errorf("err = %v (%T), want status %d", err, err, status)
	}
}

// hoAuthErr is what the real AUTH answers p for approve on the stored PO: the
// hand-over refusal for someone who is neither the owner nor an approver
// (spec §21.2 "AUTH's 403 text").
func hoAuthErr(t *testing.T, env *purchasetest.Env, p auth.Principal, id int64) error {
	t.Helper()
	var want error
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		info, err := env.Engine.Info(t.Context(), tx, purchase.DocType, id)
		if err != nil {
			return err
		}
		rec := &auth.RecordRef{DocType: purchase.DocType, ID: id, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID,
			ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
		want = env.Auth.CanTx(t.Context(), tx, p, purchase.DocType, auth.RightApprove, rec)
		return nil
	}); err != nil {
		t.Fatalf("read: %v", err)
	}
	if want == nil {
		t.Fatalf("setup: %s holds approve on PO %d, want refused", p.Name, id)
	}
	return want
}

func hoUserID(t *testing.T, env *purchasetest.Env, email string) types.UserID {
	t.Helper()
	var u auth.UserInfo
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var ok bool
		var err error
		u, ok, err = env.Auth.UserByEmail(t.Context(), tx, email)
		if err == nil && !ok {
			err = fmt.Errorf("no user %s", email)
		}
		return err
	}); err != nil {
		t.Fatalf("user %s: %v", email, err)
	}
	return u.ID
}

// hoTechOrder is a draft PO on TECH-SITE / PRJ-TECHPARK owned by the
// Administrator (00-fixtures: Priya is restricted to PRJ-METRO, so it is
// hidden from her), created through Engine.Do as the builders do.
func hoTechOrder(t *testing.T, env *purchasetest.Env) doc.Doc {
	t.Helper()
	res, err := env.Engine.Do(t.Context(), env.Admin, purchase.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"vendor_id":  masterID(t, env.DB, "party", "SHREE"),
			"site_id":    masterID(t, env.DB, "site", "TECH-SITE"),
			"project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK"),
			"order_date": types.Today(env.Clock).ISO(),
		},
		Lines: []map[string]string{{
			"item_id": masterID(t, env.DB, "item", "MS-ANGLE-50"), "uom_id": masterID(t, env.DB, "uom", "KGS"),
			"qty": "100", "price": "62.5", "tax_code_id": masterID(t, env.DB, "tax_code", "GST18"), "hsn_sac": "7216",
		}},
	})
	if err != nil {
		t.Fatalf("setup: TECH-SITE draft as admin: %v", err)
	}
	return hoLoad(t, env, res.ID)
}

// hoPendingEnv is an env with one more purchase.Order rule, Purchase Manager
// from ₹0, so a submitted PO waits for Ravi whatever its amount.
func hoPendingEnv(t *testing.T) *purchasetest.Env {
	t.Helper()
	env := purchasetest.New(t)
	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: purchase.DocType, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	}); err != nil {
		t.Fatalf("setup: rule from 0: %v", err)
	}
	return env
}

func hoAct(t *testing.T, env *purchasetest.Env, p auth.Principal, d doc.Doc, a doctype.Action, reason string) doc.Doc {
	t.Helper()
	if _, err := env.Engine.Do(t.Context(), p, purchase.DocType, d.ID, a, doc.ActionInput{Version: d.Version, Reason: reason}); err != nil {
		t.Fatalf("setup: %s PO %d as %s: %v", a, d.ID, p.Name, err)
	}
	return hoLoad(t, env, d.ID)
}

// PO-059 (PO spec §21.2, §22, §26; CD-247 (2), CD-259, CD-280 D4; plan P10),
// on the real auth.Service. Priya hands her draft to Arjun with a note and
// Arjun hands it back (do and undo, CD-272); Ravi (approve) hands one over;
// a blank note adds no comment. created_by changes, version goes up, DOC's
// history row "Handed over from {a} to {b}" is written by the person handing
// over, the note is their comment, and the new owner edits while the old one
// gets 403.
func TestPO_HandOver(t *testing.T) {
	env := purchasetest.New(t)

	t.Run("owner to arjun with a note, then back", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		before := hoSnapshot(t, env, po.ID)
		hist, cs := len(hoHistory(t, env, env.Priya, po.ID)), len(hoComments(t, env, env.Priya, po.ID))
		note := "  Over to you while I'm on leave; vendor quote is attached.  "
		if err := hoHandOver(t, env, env.Priya, po.ID, po.Version, env.Arjun.ID, note); err != nil {
			t.Fatalf("HandOver(priya → arjun): %v", err)
		}
		hoWantHandedOver(t, env, env.Priya, po.ID, before, hist, cs, env.Priya, env.Arjun, note)

		// The old owner can't edit any more (AUTH's 403), the new one can.
		v := hoLoad(t, env, po.ID).Version
		_, err := hoSave(t, env, env.Priya, po.ID, v)
		hoStatus(t, err, http.StatusForbidden)
		res, err := hoSave(t, env, env.Arjun, po.ID, v)
		if err != nil {
			t.Fatalf("save as the new owner Arjun: %v", err)
		}

		// Undo: Arjun hands it back, without a note.
		before = hoSnapshot(t, env, po.ID)
		hist, cs = len(hoHistory(t, env, env.Arjun, po.ID)), len(hoComments(t, env, env.Arjun, po.ID))
		if err := hoHandOver(t, env, env.Arjun, po.ID, res.Version, env.Priya.ID, ""); err != nil {
			t.Fatalf("HandOver(arjun → priya): %v", err)
		}
		hoWantHandedOver(t, env, env.Arjun, po.ID, before, hist, cs, env.Arjun, env.Priya, "")
		v = hoLoad(t, env, po.ID).Version
		_, err = hoSave(t, env, env.Arjun, po.ID, v)
		hoStatus(t, err, http.StatusForbidden)
		if _, err := hoSave(t, env, env.Priya, po.ID, v); err != nil {
			t.Errorf("save as Priya after the hand-back: %v", err)
		}
	})

	t.Run("ravi with approve hands priya's draft to arjun", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		before := hoSnapshot(t, env, po.ID)
		hist, cs := len(hoHistory(t, env, env.Priya, po.ID)), len(hoComments(t, env, env.Priya, po.ID))
		note := "Priya is away this week."
		if err := hoHandOver(t, env, env.Ravi, po.ID, po.Version, env.Arjun.ID, note); err != nil {
			t.Fatalf("HandOver(ravi: priya → arjun): %v", err)
		}
		hoWantHandedOver(t, env, env.Ravi, po.ID, before, hist, cs, env.Priya, env.Arjun, note)
		v := hoLoad(t, env, po.ID).Version
		_, err := hoSave(t, env, env.Priya, po.ID, v)
		hoStatus(t, err, http.StatusForbidden)
		if _, err := hoSave(t, env, env.Arjun, po.ID, v); err != nil {
			t.Errorf("save as the new owner Arjun: %v", err)
		}
	})

	t.Run("blank note gives no comment", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		before := hoSnapshot(t, env, po.ID)
		hist, cs := len(hoHistory(t, env, env.Priya, po.ID)), len(hoComments(t, env, env.Priya, po.ID))
		if err := hoHandOver(t, env, env.Priya, po.ID, po.Version, env.Arjun.ID, " \n\t "); err != nil {
			t.Fatalf("HandOver with a blank note: %v", err)
		}
		hoWantHandedOver(t, env, env.Priya, po.ID, before, hist, cs, env.Priya, env.Arjun, "")
		if got := countRows(t, env.DB, "module_comment", "1 = 1"); got != before.Comments {
			t.Errorf("module_comment rows = %d, want %d", got, before.Comments)
		}
	})

	t.Run("refused targets and notes change nothing", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		tech := hoTechOrder(t, env)
		former := hoUserID(t, env, "old@vardhan.test") // Former Staff: disabled (00-fixtures)
		long := strings.Repeat("a", modkit.MaxCommentRunes+1)
		if err := modkit.CheckCommentBody(long); err == nil || err.Error() != hoTooLong {
			t.Fatalf("setup: modkit.CheckCommentBody(1,001) = %v, want %q", err, hoTooLong)
		}
		for _, c := range []struct {
			name   string
			p      auth.Principal
			d      doc.Doc
			to     types.UserID
			note   string
			msg    string
			field  string
			status int
		}{
			{"nobody picked", env.Priya, po, 0, "", hoPick, "", http.StatusUnprocessableEntity},
			{"nobody picked, with a note", env.Priya, po, 0, "Please take this.", hoPick, "", http.StatusUnprocessableEntity},
			{"same owner", env.Priya, po, env.Priya.ID, "", fmt.Sprintf(hoSameOwner, "Priya Shah"), "to", http.StatusUnprocessableEntity},
			{"can't raise POs", env.Priya, po, env.Deepak.ID, "", fmt.Sprintf(hoCantCreate, "Deepak Nair"), "to", http.StatusUnprocessableEntity},
			{"can't raise POs (approver)", env.Priya, po, env.Ravi.ID, "", fmt.Sprintf(hoCantCreate, "Ravi Kulkarni"), "to", http.StatusUnprocessableEntity},
			{"can't see project or site", env.Admin, tech, env.Priya.ID, "", fmt.Sprintf(hoCantSee, "Priya Shah"), "to", http.StatusUnprocessableEntity},
			{"sign-in disabled", env.Priya, po, former, "", fmt.Sprintf(hoDisabled, "Former Staff"), "to", http.StatusUnprocessableEntity},
			{"note over 1,000 characters", env.Priya, po, env.Arjun.ID, long, hoTooLong, "", http.StatusUnprocessableEntity},
			// The note is checked before Reassign (plan P10 Do 2): with a
			// refused target too, the note's refusal is the one returned.
			{"note over 1,000 characters to a refused target", env.Priya, po, env.Deepak.ID, long, hoTooLong, "", http.StatusUnprocessableEntity},
		} {
			t.Run(c.name, func(t *testing.T) {
				before := hoSnapshot(t, env, c.d.ID)
				err := hoHandOver(t, env, c.p, c.d.ID, c.d.Version, c.to, c.note)
				hoRefused(t, err, c.msg, c.status, c.field)
				hoUnchanged(t, env, c.d.ID, before)
			})
		}
	})

	t.Run("state refusals", func(t *testing.T) {
		approved := purchasetest.NewOrder(t, env)
		closed := hoAct(t, env, env.Priya, purchasetest.NewOrder(t, env), doctype.ActionClose, "Rest not needed")
		cancelled := hoAct(t, env, env.Ravi, purchasetest.NewOrder(t, env), doctype.ActionCancel, "Vendor backed out")
		for _, d := range []doc.Doc{approved, closed, cancelled} {
			for _, p := range []auth.Principal{env.Priya, env.Ravi} {
				t.Run(string(d.State)+" by "+p.Name, func(t *testing.T) {
					before := hoSnapshot(t, env, d.ID)
					err := hoHandOver(t, env, p, d.ID, d.Version, env.Arjun.ID, "")
					hoRefused(t, err, hoNotDraft, 0, "")
					hoUnchanged(t, env, d.ID, before)
				})
			}
		}

		penv := hoPendingEnv(t)
		pending := purchasetest.NewOrder(t, penv, purchasetest.StopAtPending())
		for _, p := range []auth.Principal{penv.Priya, penv.Ravi} {
			t.Run("pending by "+p.Name, func(t *testing.T) {
				before := hoSnapshot(t, penv, pending.ID)
				err := hoHandOver(t, penv, p, pending.ID, pending.Version, penv.Arjun.ID, "")
				hoRefused(t, err, hoPending, 0, "")
				hoUnchanged(t, penv, pending.ID, before)
			})
		}

		// A rejected PO is still a draft in §21.1's sense: it can be handed over.
		t.Run("rejected is handed over", func(t *testing.T) {
			rejected := hoAct(t, penv, penv.Ravi, purchasetest.NewOrder(t, penv, purchasetest.StopAtPending()), doctype.ActionReject, "Wrong vendor")
			if rejected.State != doc.StateRejected {
				t.Fatalf("setup: state %s, want rejected", rejected.State)
			}
			before := hoSnapshot(t, penv, rejected.ID)
			hist, cs := len(hoHistory(t, penv, penv.Priya, rejected.ID)), len(hoComments(t, penv, penv.Priya, rejected.ID))
			if err := hoHandOver(t, penv, penv.Priya, rejected.ID, rejected.Version, penv.Arjun.ID, ""); err != nil {
				t.Fatalf("HandOver of a rejected PO: %v", err)
			}
			hoWantHandedOver(t, penv, penv.Priya, rejected.ID, before, hist, cs, penv.Priya, penv.Arjun, "")
		})
	})

	t.Run("stale version is 409", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		res, err := hoSave(t, env, env.Priya, po.ID, po.Version) // another tab saved
		if err != nil {
			t.Fatalf("setup: save: %v", err)
		}
		before := hoSnapshot(t, env, po.ID)
		err = hoHandOver(t, env, env.Priya, po.ID, po.Version, env.Arjun.ID, "Stale tab.")
		hoRefused(t, err, fmt.Sprintf(hoStale, fmt.Sprintf("Draft %d", po.ID)), http.StatusConflict, "")
		hoUnchanged(t, env, po.ID, before)
		if res.Version != before.Version {
			t.Fatalf("setup: version %d, want %d", before.Version, res.Version)
		}
	})

	t.Run("no right, hidden and missing", func(t *testing.T) {
		po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())
		tech := hoTechOrder(t, env)
		for _, p := range []auth.Principal{env.Arjun, env.Suresh} { // Purchase User (not the owner), Stores: read only
			t.Run("403 "+p.Name, func(t *testing.T) {
				want := hoAuthErr(t, env, p, po.ID)
				before := hoSnapshot(t, env, po.ID)
				err := hoHandOver(t, env, p, po.ID, po.Version, env.Arjun.ID, "Taking it.")
				if err == nil || err.Error() != want.Error() {
					t.Errorf("err = %v, want AUTH's %q", err, want)
				}
				hoStatus(t, err, http.StatusForbidden)
				hoUnchanged(t, env, po.ID, before)
			})
		}
		t.Run("404 hidden", func(t *testing.T) {
			before := hoSnapshot(t, env, tech.ID)
			err := hoHandOver(t, env, env.Priya, tech.ID, tech.Version, env.Arjun.ID, "")
			if !errors.Is(err, auth.ErrNotFound) || err.Error() != auth.MsgNotFound {
				t.Errorf("err = %v, want AUTH's 404 %q", err, auth.MsgNotFound)
			}
			hoStatus(t, err, http.StatusNotFound)
			hoUnchanged(t, env, tech.ID, before)
		})
		t.Run("404 missing", func(t *testing.T) {
			err := hoHandOver(t, env, env.Priya, 999_999, 1, env.Arjun.ID, "")
			if err == nil || !slices.Contains(hoMissing, err.Error()) {
				t.Errorf("err = %v, want a 404 in %q", err, hoMissing)
			}
			hoStatus(t, err, http.StatusNotFound)
		})
	})
}

// PO-059 / UX 5.12 (plan P10 HandOverState; CD-142, CD-245, CD-259): the
// hand-over page's view. A missing or hidden PO is AUTH's 404 and a missing
// right AUTH's 403, as errors; Refusal holds only the state refusals, the
// same error HandOver returns for that state; Eligible lists the users
// Reassign would accept, with names and roles, sorted by name, without the
// owner.
func TestPO_HandOverState(t *testing.T) {
	env := purchasetest.New(t)
	// A Purchase User created after the fixtures, whose name sorts first, so
	// an id-ordered list fails the name sort (lessons §10).
	aakash, _, err := env.Auth.CreateUser(t.Context(), env.Admin, auth.NewUser{
		Email: "aakash@vardhan.test", Name: "Aakash Bhat", Password: "Correct-Horse-42", Roles: []string{"Purchase User"},
	})
	if err != nil {
		t.Fatalf("setup: CreateUser: %v", err)
	}
	po := purchasetest.NewOrder(t, env, purchasetest.StopAtDraft())

	t.Run("draft: view and eligible", func(t *testing.T) {
		for _, p := range []auth.Principal{env.Priya, env.Ravi} {
			st, err := hoState(t, env, p, po.ID)
			if err != nil {
				t.Fatalf("HandOverState as %s: %v", p.Name, err)
			}
			if st.Refusal != nil {
				t.Errorf("as %s: Refusal = %v on a draft, want nil", p.Name, st.Refusal)
			}
			var vendor string
			if err := env.DB.SQL().QueryRowContext(t.Context(), `SELECT name FROM party WHERE code = 'SHREE'`).Scan(&vendor); err != nil {
				t.Fatalf("vendor name: %v", err)
			}
			if want := fmt.Sprintf("Draft %d", po.ID); st.Display != want || st.Owner != "Priya Shah" || st.Version != po.Version || st.Vendor != vendor {
				t.Errorf("as %s: Display %q, Owner %q, Version %d, Vendor %q; want %q, %q, %d, %q",
					p.Name, st.Display, st.Owner, st.Version, st.Vendor, want, "Priya Shah", po.Version, vendor)
			}
			var ids, names []string
			for _, e := range st.Eligible {
				ids = append(ids, fmt.Sprint(e.ID))
				names = append(names, e.Name)
			}
			wantIDs := []string{fmt.Sprint(aakash), fmt.Sprint(env.Admin.ID), fmt.Sprint(env.Arjun.ID)}
			wantNames := []string{"Aakash Bhat", "Anil Kapoor", "Arjun Mehta"}
			if !slices.Equal(ids, wantIDs) || !slices.Equal(names, wantNames) {
				t.Errorf("as %s: Eligible = %v %v, want %v %v (by name, owner Priya left out)", p.Name, ids, names, wantIDs, wantNames)
			}
			for i, role := range []string{"Purchase User", "Administrator", "Purchase User"} {
				if i < len(st.Eligible) && !strings.Contains(st.Eligible[i].Roles, role) {
					t.Errorf("as %s: %s's Roles = %q, want it to name %q", p.Name, st.Eligible[i].Name, st.Eligible[i].Roles, role)
				}
			}
		}
	})

	t.Run("state refusals", func(t *testing.T) {
		approved := purchasetest.NewOrder(t, env)
		closed := hoAct(t, env, env.Priya, purchasetest.NewOrder(t, env), doctype.ActionClose, "Rest not needed")
		cancelled := hoAct(t, env, env.Ravi, purchasetest.NewOrder(t, env), doctype.ActionCancel, "Vendor backed out")
		penv := hoPendingEnv(t)
		pending := purchasetest.NewOrder(t, penv, purchasetest.StopAtPending())
		for _, c := range []struct {
			env *purchasetest.Env
			d   doc.Doc
			msg string
		}{
			{env, approved, hoNotDraft}, {env, closed, hoNotDraft}, {env, cancelled, hoNotDraft}, {penv, pending, hoPending},
		} {
			t.Run(string(c.d.State), func(t *testing.T) {
				st, err := hoState(t, c.env, c.env.Priya, c.d.ID)
				if err != nil {
					t.Fatalf("HandOverState: err = %v, want nil with a Refusal", err)
				}
				if st.Refusal == nil || st.Refusal.Error() != c.msg {
					t.Fatalf("Refusal = %v, want %q", st.Refusal, c.msg)
				}
				post := hoHandOver(t, c.env, c.env.Priya, c.d.ID, c.d.Version, c.env.Arjun.ID, "")
				if post == nil || post.Error() != st.Refusal.Error() {
					t.Errorf("HandOver = %v, want the same error as Refusal %q", post, st.Refusal)
				}
			})
		}
	})

	t.Run("403 and 404 are errors, not refusals", func(t *testing.T) {
		tech := hoTechOrder(t, env)
		for _, p := range []auth.Principal{env.Arjun, env.Suresh} {
			want := hoAuthErr(t, env, p, po.ID)
			st, err := hoState(t, env, p, po.ID)
			if err == nil || err.Error() != want.Error() {
				t.Errorf("HandOverState as %s: err = %v, want AUTH's %q", p.Name, err, want)
			}
			hoStatus(t, err, http.StatusForbidden)
			if st.Refusal != nil {
				t.Errorf("as %s: Refusal = %v, want nil (403 is the error)", p.Name, st.Refusal)
			}
		}
		for name, c := range map[string]struct {
			id   int64
			msgs []string
		}{"hidden": {tech.ID, []string{auth.MsgNotFound}}, "missing": {999_999, hoMissing}} {
			st, err := hoState(t, env, env.Priya, c.id)
			if err == nil || !slices.Contains(c.msgs, err.Error()) {
				t.Errorf("%s: err = %v, want a 404 in %q", name, err, c.msgs)
			}
			hoStatus(t, err, http.StatusNotFound)
			if st.Refusal != nil || len(st.Eligible) != 0 {
				t.Errorf("%s: state %+v, want empty", name, st)
			}
		}
	})
}
