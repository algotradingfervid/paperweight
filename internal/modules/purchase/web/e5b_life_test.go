package purchaseweb

import (
	"fmt"
	"maps"
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// e5bLife writes a Life map as sorted "word:mark:note:date" (mark d done,
// c current, - later).
func e5bLife(life map[string]components.LifeStep) []string {
	marks := map[components.LifeMark]string{components.LifeTodo: "-", components.LifeDone: "d", components.LifeCurrent: "c"}
	var out []string
	for _, w := range slices.Sorted(maps.Keys(life)) {
		s := life[w]
		out = append(out, fmt.Sprintf("%s:%s:%s:%s", w, marks[s.Mark], s.Note, s.Date))
	}
	return out
}

// UI v2 E5b (§5.13 item 4): a PO's Sent step is reached once it is sent,
// with the Sent record's day; Received once anything is received, with
// "Part" under it when part received. On an approved PO the latest reached
// step is current (Received, else Sent); closed or cancelled, both are done. A draft, pending or rejected PO reaches
// neither, and a Hide that hides receipt_status hides Received.
func TestOrderLife(t *testing.T) {
	dt, _ := doctype.Lookup(purchase.DocType)
	sent := orderRecords{isSent: true, sent: purchase.Sent{SentOn: types.Date{Year: 2026, Month: 11, Day: 5}}}
	for _, c := range []struct {
		name    string
		state   doctype.State
		rec     orderRecords
		receipt string
		hide    bool
		want    []string
	}{
		{"approved, not sent", doctype.StateApproved, orderRecords{}, "not_received", false, nil},
		{"approved, sent", doctype.StateApproved, sent, "not_received", false, []string{"Sent:c::05-11"}},
		{"approved, sent, part received", doctype.StateApproved, sent, "part_received", false, []string{"Received:c:Part:", "Sent:d::05-11"}},
		{"approved, sent, fully received", doctype.StateApproved, sent, "fully_received", false, []string{"Received:c::", "Sent:d::05-11"}},
		{"closed, fully received", doctype.StateClosed, sent, "fully_received", false, []string{"Received:d::", "Sent:d::05-11"}},
		{"approved, part received, not sent", doctype.StateApproved, orderRecords{}, "part_received", false, []string{"Received:c:Part:"}},
		{"approved, receipt hidden", doctype.StateApproved, sent, "part_received", true, []string{"Sent:c::05-11"}},
		{"closed, part received", doctype.StateClosed, sent, "part_received", false, []string{"Received:d:Part:", "Sent:d::05-11"}},
		{"cancelled, sent", doctype.StateCancelled, sent, "not_received", false, []string{"Sent:d::05-11"}},
		{"pending", doctype.StatePending, sent, "part_received", false, nil},
		{"rejected", doctype.StateRejected, sent, "part_received", false, nil},
		{"draft", doctype.StateDraft, sent, "part_received", false, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			v := &docscreen.DocView{Info: doctype.Info{State: c.state}, Inputs: map[string]string{"receipt_status": c.receipt}}
			var mods map[string]docscreen.Module
			if c.hide {
				mods = map[string]docscreen.Module{purchase.DocType: {Hide: func(_ docscreen.Ctx, f doctype.Field) bool { return f.Name == "receipt_status" }}}
			}
			ctx := docscreen.Ctx{DT: dt, View: v, Deps: docscreen.Deps{Modules: mods}}
			if got := e5bLife(orderLife(ctx, c.rec)); !slices.Equal(got, c.want) {
				t.Errorf("life = %q, want %q", got, c.want)
			}
		})
	}
}

// e5bReceive writes PO id's receipt_status as GRN does, with each line's
// accepted quantity its ordered quantity divided by div (1 = all of it).
func e5bReceive(t *testing.T, w *p15dWeb, id int64, div int64) {
	t.Helper()
	err := w.env.DB.Write(t.Context(), func(tx *db.Tx) error {
		rows, err := dbq.New(tx.SQL()).ReceiptLines(t.Context(), id)
		if err != nil {
			return err
		}
		accepted := map[int64]types.Qty{}
		for _, r := range rows {
			accepted[r.ID] = types.Qty(r.Qty / div)
		}
		return purchase.RefreshReceiptStatus(t.Context(), tx, id, accepted)
	})
	if err != nil {
		t.Fatalf("receive PO %d: %v", id, err)
	}
}

// Through the page (A-26): an approved PO declares Sent and Received after
// Approved, and exactly one step is current: Approved before sending, Sent
// once sent, then Received (with "Part" under it while part received).
func TestPO_SentStep(t *testing.T) {
	w := newP15bWeb(t, false)
	env := w.env
	po := w.order(t, doctype.StateApproved)
	_, steps := e5Header(w.page(t, env.Suresh, po.ID))
	want := []string{"Draft (done)|", "Pending approval (done)|", "Approved|step", "Sent|", "Received|"}
	if !slices.Equal(steps, want) {
		t.Errorf("before sending: steps = %q, want %q", steps, want)
	}
	today := types.FormatDate(types.Today(env.Clock))
	w.markSent(t, env.Priya, po.ID, today, "email", "")
	_, steps = e5Header(w.page(t, env.Suresh, po.ID))
	want = []string{"Draft (done)|", "Pending approval (done)|", "Approved (done)|", "Sent " + today[:5] + "|step", "Received|"}
	if !slices.Equal(steps, want) {
		t.Errorf("after sending: steps = %q, want %q", steps, want)
	}
	sent := "Sent (done) " + today[:5] + "|"
	for _, c := range []struct {
		what string
		div  int64
		want []string
	}{
		{"part received", 2, []string{"Draft (done)|", "Pending approval (done)|", "Approved (done)|", sent, "Received Part|step"}},
		{"fully received", 1, []string{"Draft (done)|", "Pending approval (done)|", "Approved (done)|", sent, "Received|step"}},
	} {
		e5bReceive(t, w, po.ID, c.div)
		page := w.page(t, env.Suresh, po.ID)
		if _, steps := e5Header(page); !slices.Equal(steps, c.want) {
			t.Errorf("%s: steps = %q, want %q", c.what, steps, c.want)
		}
		if n := page.doc.Find(`.doc-head li[aria-current="step"]`).Length(); n != 1 {
			t.Errorf("%s: %d current steps, want exactly one", c.what, n)
		}
	}
}

// Both purchase DocTypes declare the purchase home, so the success block
// after Submit offers "Go to Purchase" (head ruling on the E5b review).
func TestPurchase_Home(t *testing.T) {
	mods := Modules(Deps{})
	want := components.Link{Label: "Purchase", Href: "/modules/purchase"}
	for _, dt := range []string{purchase.DocType, purchase.RequestDocType} {
		if got := mods[dt].Home; got != want {
			t.Errorf("%s Home = %+v, want %+v", dt, got, want)
		}
	}
}
