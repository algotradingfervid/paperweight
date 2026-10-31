package goodsreceipt_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// MOD-GRN G1 Do 3 (GRN spec §5; plan hook table): cancelling an approved GRN
// as the Purchase Manager runs ReverseEffects' composition (checkLock,
// reverseReceipt, restNotComing off) and succeeds; Stores then amends it into
// a new draft. Built through grntest's builders, with the row counts moving
// only by the amendment.
func TestGRN_Hooks_CancelAndAmend(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "40"}}, doctype.StateApproved)
	if grn.Number != "GR/26-27/00001" {
		t.Fatalf("GRN number = %q, want GR/26-27/00001", grn.Number)
	}
	before := env.RowCounts(t)

	cancelled := env.Cancel(t, env.Ravi, grn.ID, "Wrong quantity entered")
	if cancelled.State != doctype.StateCancelled || cancelled.StateReason != "Wrong quantity entered" {
		t.Errorf("after cancel: state %s reason %q, want cancelled with the reason", cancelled.State, cancelled.StateReason)
	}
	amended := env.Amend(t, env.Suresh, grn.ID)
	if amended.State != doctype.StateDraft || amended.AmendedFromID != grn.ID || len(amended.Lines) != 1 {
		t.Errorf("amendment: state %s amended from %d, %d lines; want a draft amended from %d with 1 line",
			amended.State, amended.AmendedFromID, len(amended.Lines), grn.ID)
	}
	after := env.RowCounts(t)
	if after.Receipts != before.Receipts+1 || after.Lines != before.Lines+1 || after.Bills != before.Bills || after.Audit <= before.Audit {
		t.Errorf("row counts %+v after cancel and amend, from %+v; want one more receipt and line, same bills, more audit", after, before)
	}
}

// h1Calls runs each hook method that returns an error.
var h1Calls = map[string]func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error{
	"BeforeSubmit": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.BeforeSubmit(ctx, tx, id)
	},
	"ApplyEffects": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.ApplyEffects(ctx, tx, id)
	},
	"ReverseEffects": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		return h.ReverseEffects(ctx, tx, id)
	},
	"Amount": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		_, err := h.Amount(ctx, tx, id)
		return err
	},
	"Date": func(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) error {
		_, err := h.Date(ctx, tx, id)
		return err
	},
}

// MOD-GRN Global constraints "Hooks" (CD-284): every GRN hook run on a
// database handle nothing was wired for fails with the Wiring's plain
// internal error, though the rows exist on the file. Validate's signature
// (doctype.Hooks) returns only field errors, so it can't return the error: it
// panics with it, and the engine's recover turns that into its plain
// internal error (next test).
func TestGRN_Hooks_Unwired(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateDraft)

	unwired, err := db.Open(testkit.DBPath(t))
	if err != nil {
		t.Fatalf("open a second handle: %v", err)
	}
	t.Cleanup(func() { _ = unwired.Close() })
	dt, _ := doctype.Lookup(goodsreceipt.DocType)

	for _, m := range []string{"Validate", "BeforeSubmit", "ApplyEffects", "ReverseEffects", "Amount", "Date"} {
		t.Run(m, func(t *testing.T) {
			if err := unwired.Write(t.Context(), func(tx *db.Tx) error {
				var never modkit.Wiring[struct{}]
				_, wiringErr := never.Get(tx)
				if m == "Validate" {
					if got := h1ValidatePanic(t.Context(), dt.Hooks, tx, grn.ID); !strings.Contains(got, wiringErr.Error()) {
						t.Errorf("Validate on an unwired database panicked with %q, want the wiring error %q", got, wiringErr)
					}
					return nil
				}
				err := h1Calls[m](t.Context(), dt.Hooks, tx, grn.ID)
				var he doctype.HTTPError
				switch {
				case err == nil:
					t.Errorf("%s on an unwired database = nil, want the wiring error %q", m, wiringErr)
				case errors.As(err, &he):
					t.Errorf("%s on an unwired database = %v (status %d), want a plain internal error", m, err, he.Status())
				case err.Error() != wiringErr.Error():
					t.Errorf("%s on an unwired database = %v, want the wiring error %q", m, err, wiringErr)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// MOD-GRN Global constraints "Hooks" (CD-284): in a world whose database
// goodsreceipt was never wired for, a GRN save through the engine fails with
// a plain internal error (no HTTP status: a 500), and nothing is written.
func TestGRN_Hooks_UnwiredEngineSave(t *testing.T) {
	pe := purchasetest.New(t, purchasetest.WithNoRules())
	po := purchasetest.NewOrder(t, pe, purchasetest.StopAtApproved())
	_, err := pe.Engine.Do(t.Context(), pe.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(po.ID, 10),
			"site_id":           g1Int(t, po.Header["site_id"]),
			"project_id":        g1Int(t, po.Header["project_id"]),
			"receipt_date":      "2026-10-24",
		},
		Lines: []map[string]string{{"po_line_id": strconv.FormatInt(po.Lines[0].ID, 10), "received_qty": "10"}},
	})
	var he doctype.HTTPError
	if err == nil || errors.As(err, &he) || !strings.Contains(err.Error(), "nothing wired") {
		t.Errorf("save on an unwired database: err = %v, want a plain internal error naming the wiring", err)
	}
	if got := g1Count(t, pe.DB, "goods_receipt", "1 = 1"); got != 0 {
		t.Errorf("goods_receipt rows after the failed save = %d, want 0", got)
	}
}

// h1ValidatePanic calls Validate and returns what it panicked with, or ""
// when it returned.
func h1ValidatePanic(ctx context.Context, h doctype.Hooks, tx *db.Tx, id int64) (got string) {
	defer func() {
		if r := recover(); r != nil {
			got = fmt.Sprint(r)
		}
	}()
	h.Validate(ctx, tx, id)
	return ""
}

// MOD-GRN G1 follow-up (AD-12; 00-fixtures.md: a PO on another site is
// raised by admin): grntest.POAs creates a PO through the engine as the
// given user, on TECH-SITE / PRJ-TECHPARK for admin, at each stop-at state;
// with a Purchase Manager rule a large PO is approved by an eligible user.
func TestGRN_POAs(t *testing.T) {
	env := grntest.New(t)
	tech := map[string]string{"site_id": g1MasterID(t, env.DB, "site", "TECH-SITE"), "project_id": g1MasterID(t, env.DB, "project", "PRJ-TECHPARK")}
	po := grntest.POAs(t, env, env.Admin, doctype.StateApproved, tech)
	if po.State != doctype.StateApproved || po.CreatedBy != env.Admin.ID || g1Int(t, po.Header["site_id"]) != tech["site_id"] || len(po.Lines) != 1 {
		t.Errorf("POAs(admin, TECH-SITE) = state %s by %d site %v, %d lines; want approved by admin on TECH-SITE with 1 line",
			po.State, po.CreatedBy, po.Header["site_id"], len(po.Lines))
	}
	if d := grntest.POAs(t, env, env.Priya, doctype.StateDraft, nil, map[string]string{}, map[string]string{"qty": "5"}); d.State != doctype.StateDraft || len(d.Lines) != 2 {
		t.Errorf("POAs(priya, draft, two lines) = %s with %d lines, want a draft with 2", d.State, len(d.Lines))
	}

	if _, err := env.Approvals.CreateRule(t.Context(), env.Admin, approval.RuleInput{
		DocType: "purchase.Order", FromAmount: 50_000_00, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	}); err != nil {
		t.Fatalf("add a Purchase Manager rule: %v", err)
	}
	big := map[string]string{"qty": "1000"}
	if p := grntest.POAs(t, env, env.Priya, doctype.StatePending, nil, big); p.State != doctype.StatePending {
		t.Errorf("POAs(pending) = %s, want pending", p.State)
	}
	if a := grntest.POAs(t, env, env.Priya, doctype.StateApproved, nil, big); a.State != doctype.StateApproved {
		t.Errorf("POAs(approved) over the rule = %s, want approved", a.State)
	}
}

// MOD-GRN G1 Do 3 (GRN spec §6; plan hook table "Date = receipt_date"): the
// Date hook is receipt_date, so a GRN received on 31 March 2026 and approved
// in October takes its number from FY 2025-26: GR/25-26/00001.
func TestGRN_Hooks_DateSetsFY(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	res, err := env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, 0, doctype.ActionSave, doc.ActionInput{
		Values: map[string]string{
			"purchase_order_id": strconv.FormatInt(po.ID, 10),
			"site_id":           g1Int(t, po.Header["site_id"]),
			"project_id":        g1Int(t, po.Header["project_id"]),
			"receipt_date":      "2026-03-31",
			"challan_kind":      "Delivery challan",
			"challan_no":        "DC-1042",
		},
		Lines: []map[string]string{{"po_line_id": strconv.FormatInt(po.Lines[0].ID, 10), "received_qty": "10"}},
	})
	if err != nil {
		t.Fatalf("save GRN: %v %+v", err, g1FieldErrors(err))
	}
	res, err = env.Engine.Do(t.Context(), env.Suresh, goodsreceipt.DocType, res.ID, doctype.ActionSubmit, doc.ActionInput{Version: res.Version})
	if err != nil {
		t.Fatalf("submit GRN %d: %v %+v", res.ID, err, g1FieldErrors(err))
	}
	if got := env.Load(t, goodsreceipt.DocType, res.ID); got.State != doctype.StateApproved || got.Number != "GR/25-26/00001" {
		t.Errorf("GRN received 2026-03-31: state %s number %q, want approved GR/25-26/00001 (FY from receipt_date)", got.State, got.Number)
	}
}

// MOD-GRN G1 Do 3 (plan hook table "Amount = accepted_value"): the Amount
// hook returns the stored accepted_value. G2's Validate computes it; until
// then the test sets it with SQL, as the QtyUse test sets uom_id.
func TestGRN_Hooks_AmountIsAcceptedValue(t *testing.T) {
	env := grntest.New(t)
	po := purchasetest.NewOrder(t, env.Env, purchasetest.StopAtApproved())
	grn := env.GRN(t, env.Suresh, po.ID, []grntest.Line{{POLineID: po.Lines[0].ID, Received: "10"}}, doctype.StateDraft)
	dt, _ := doctype.Lookup(goodsreceipt.DocType)
	var got types.Money
	if err := env.DB.Write(t.Context(), func(tx *db.Tx) error {
		if _, err := tx.SQL().ExecContext(t.Context(), `UPDATE goods_receipt SET accepted_value = 62500 WHERE id = ?`, grn.ID); err != nil {
			return err
		}
		var err error
		got, err = dt.Hooks.Amount(t.Context(), tx, grn.ID)
		return err
	}); err != nil {
		t.Fatalf("Amount of GRN %d: %v", grn.ID, err)
	}
	if got != 62500 {
		t.Errorf("Amount = %d paise, want 62500 (the stored accepted_value)", got)
	}
}
