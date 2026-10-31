package purchase_test

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// PO-053 (PO spec §20.5, §26; CD-147): each condition on a PO line with a
// request row gives its own message, one field error on the line, and the
// refused save writes nothing. DOC owns the three link texts (draft, closed,
// cancelled request) and P6 adds no copy of them; checkRequestRows owns the
// six module conditions, each on Field request_line_id with Line = the PO
// line (AD-13 ruling). The "already on" condition names the oldest PO, and
// it also refuses a submit (the kernel runs Validate again, D-APR-15), which
// the amend path reaches: amend copies request_line_id and keeps Validate's
// errors (PO spec §20.5 write paths).
func TestPO_RequestRowRules(t *testing.T) {
	// refusedAs saves a new PO as p with header and lines and asserts the
	// one field error want, with nothing written; refused saves as Priya.
	refusedAs := func(t *testing.T, env *purchasetest.Env, p auth.Principal, header map[string]string, lines []map[string]string, want doctype.FieldError) {
		t.Helper()
		before := p2CountAll(t, env)
		_, err := p2Create(t, env, p, header, lines)
		p2WantInvalid(t, "save", err, want)
		p2SameCounts(t, "refused save", before, env)
	}
	refused := func(t *testing.T, env *purchasetest.Env, header map[string]string, lines []map[string]string, want doctype.FieldError) {
		t.Helper()
		refusedAs(t, env, env.Priya, header, lines, want)
	}
	metro := func(env *purchasetest.Env) map[string]string {
		return p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO")
	}

	t.Run("request is a draft (DOC)", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		res := p5Must(t, "Suresh saves a request")(p5Create(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"),
			[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "40")}))
		req := p5Load(t, env, purchase.RequestDocType, res.ID)
		refused(t, env, metro(env), []map[string]string{p6Bag(t, env, "40", req.Lines[0].ID)},
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6DocNotApproved, 1, p6Label(req))})
	})

	t.Run("request is closed (DOC)", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))
		refused(t, env, metro(env), []map[string]string{p6Bag(t, env, "40", req.Lines[0].ID)},
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6DocClosed, 1, req.Number)})
	})

	t.Run("request is cancelled (DOC)", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		p5Must(t, "Suresh cancels the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionCancel, p5CancelReason))
		refused(t, env, metro(env), []map[string]string{p6Bag(t, env, "40", req.Lines[0].ID)},
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6DocCancelled, 1, req.Number)})
	})

	t.Run("request's site differs", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "PUNE-WH", "PRJ-METRO"),
			[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "40")})
		refused(t, env, metro(env), []map[string]string{p6Bag(t, env, "40", req.Lines[0].ID)},
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgSite, 1, req.Number, "PUNE-WH", "METRO-SITE")})
	})

	t.Run("request's project differs", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "PUNE-WH", "PRJ-TECHPARK"),
			[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "40")})
		// Arjun (no restrictions) can read PRJ-TECHPARK's request; Priya can't.
		refusedAs(t, env, env.Arjun, p2Header(t, env, "SHREE", "PUNE-WH", "PRJ-METRO"), []map[string]string{p6Bag(t, env, "40", req.Lines[0].ID)},
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgProject, 1, req.Number, "PRJ-TECHPARK", "PRJ-METRO")})
	})

	t.Run("item differs from the row's", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
			p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "CEMENT-OPC", "BAG", "40")})
		// PO line 2 links request line 1 (MS-ANGLE-50) with WIRE-18G.
		refused(t, env, metro(env), []map[string]string{
			p2Line(t, env, "WIRE-18G", "KGS", "10", "96"),
			p6Link(p2Line(t, env, "WIRE-18G", "KGS", "100", "96"), req.Lines[0].ID),
		}, doctype.FieldError{Field: "request_line_id", Line: 2, Message: p6Fmt(p6MsgItem, 2, req.Number, 1, "MS-ANGLE-50")})
	})

	t.Run("UoM differs from the row's", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
			p5Line(t, env, "MS-ANGLE-50", "KGS", "100"), p5Line(t, env, "BOLT-M12", "NOS", "200")})
		// PO line 1 links request line 2 (BOLT-M12 in NOS) in BOX, a unit the
		// item converts to (Box = 100 Nos), so only the request rule refuses.
		refused(t, env, metro(env), []map[string]string{
			p6Link(p2Line(t, env, "BOLT-M12", "BOX", "2", "900"), req.Lines[1].ID),
		}, doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgUoM, 1, req.Number, 2, "NOS")})
	})

	t.Run("same request row on two rows", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
			p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
		// Lines 1 and 3 both take request line 2, 30 + 20 kg of 100: only the
		// later line is refused, naming the earlier one.
		refused(t, env, metro(env), []map[string]string{
			p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "30", "62.5"), req.Lines[1].ID),
			p6Bag(t, env, "10", req.Lines[0].ID),
			p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "20", "62.5"), req.Lines[1].ID),
		}, doctype.FieldError{Field: "request_line_id", Line: 3, Message: p6Fmt(p6MsgSameRow, 3, req.Number, 2, 1)})
	})

	t.Run("other POs already reach the row, oldest named", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
			p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
		row := req.Lines[0].ID
		// Arjun's draft holds 15 (the oldest), Priya's approved PO orders 25:
		// 40 of 40.
		oldest := p6Order(t, env, env.Arjun, nil, []map[string]string{p6Bag(t, env, "15", row)}, doctype.StateDraft)
		p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "25", row)}, doctype.StateApproved)
		refused(t, env, metro(env), []map[string]string{
			p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"), req.Lines[1].ID),
			p6Bag(t, env, "5", row),
		}, doctype.FieldError{Field: "request_line_id", Line: 2, Message: p6Fmt(p6MsgAlreadyOn, 2, req.Number, 1, p6Label(oldest))})

		// Undo: once Arjun deletes his draft, 25 of 40 is on orders and the
		// same save goes through.
		p2Must(t, "Arjun deletes his draft")(p2Do(t, env, env.Arjun, oldest.ID, doc.ActionDelete, ""))
		if _, err := p2Create(t, env, env.Priya, metro(env), []map[string]string{
			p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", "10", "62.5"), req.Lines[1].ID),
			p6Bag(t, env, "5", row),
		}); err != nil {
			t.Errorf("save with 25 of 40 on orders: %v %+v, want it saved", err, p5Fields(err))
		}
	})

	t.Run("already on, refused on submit of an amendment", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		first := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateApproved)
		p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, first.ID, doc.ActionCancel, p6CancelReason))
		other := p6Order(t, env, env.Arjun, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateDraft)
		am := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, first.ID, doc.ActionAmend, ""))
		if got := p2LineSnap(t, env, am.ID, "request_line_id"); len(got) != 1 || got[0]["request_line_id"] != p5Plain(row) {
			t.Fatalf("amendment lines %v, want request_line_id %d copied", got, row)
		}

		_, err := p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, "")
		p2WantInvalid(t, "submit the amendment", err,
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgAlreadyOn, 1, req.Number, 1, p6Label(other))})
		if st := p2Load(t, env, am.ID).State; st != doctype.StateDraft {
			t.Errorf("after the refused submit the amendment is %s, want draft", st)
		}

		// Undo: Arjun deletes his draft and the submit goes through.
		p2Must(t, "Arjun deletes his draft")(p2Do(t, env, env.Arjun, other.ID, doc.ActionDelete, ""))
		if _, err := p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""); err != nil {
			t.Errorf("submit with nothing else on the row: %v %+v, want submitted", err, p5Fields(err))
		}
	})
}

// PO-054 (PO spec §20.5, §26; CD-94, CD-213): Priya and Arjun (non-admin)
// each have a draft; both save a line for the row's full 40 Bag at once, each
// on its own connection. The check reads the other POs inside the write, so
// one saves and the other is refused naming the winner.
func TestPO_RequestRowRace(t *testing.T) {
	env := purchasetest.New(t)
	req := p6CementRequest(t, env, "40")
	row := req.Lines[0].ID
	priya := p6Order(t, env, env.Priya, nil, []map[string]string{p2Line(t, env, "WIRE-18G", "KGS", "10", "96")}, doctype.StateDraft)
	arjun := p6Order(t, env, env.Arjun, nil, []map[string]string{p2Line(t, env, "WIRE-18G", "KGS", "10", "96")}, doctype.StateDraft)
	type attempt struct {
		id      int64
		version int
		lines   []map[string]string
		err     error
	}
	attempts := []*attempt{
		{id: priya.ID, version: priya.Version, lines: []map[string]string{p6Bag(t, env, "40", row)}},
		{id: arjun.ID, version: arjun.Version, lines: []map[string]string{p6Bag(t, env, "40", row)}},
	}
	owners := []doc.Doc{priya, arjun}
	saves := []func() (doc.Result, error){
		func() (doc.Result, error) {
			return env.Engine.Do(t.Context(), env.Priya, purchase.DocType, attempts[0].id, doc.ActionSave,
				doc.ActionInput{Version: attempts[0].version, Lines: attempts[0].lines})
		},
		func() (doc.Result, error) {
			return env.Engine.Do(t.Context(), env.Arjun, purchase.DocType, attempts[1].id, doc.ActionSave,
				doc.ActionInput{Version: attempts[1].version, Lines: attempts[1].lines})
		},
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range saves {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, attempts[i].err = saves[i]()
		}()
	}
	close(start)
	wg.Wait()

	won := slices.IndexFunc(attempts, func(a *attempt) bool { return a.err == nil })
	lost := slices.IndexFunc(attempts, func(a *attempt) bool { return a.err != nil })
	if won < 0 || lost < 0 {
		t.Fatalf("errors %v and %v, want one save and one refusal", attempts[0].err, attempts[1].err)
	}
	p2WantInvalid(t, "the second save", attempts[lost].err,
		doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgAlreadyOn, 1, req.Number, 1, p6Label(owners[won]))})
	if got := p2LineSnap(t, env, owners[lost].ID, "request_line_id"); len(got) != 1 || got[0]["request_line_id"] != "" {
		t.Errorf("refused PO's lines %v, want its WIRE-18G line unchanged (rolled back)", got)
	}
	if got := p6Held(t, env, req.ID, row); got != p6Q(t, "40") {
		t.Errorf("held after the race = %s, want 40 (one PO)", got.Plain())
	}
}

// PO-069, service part (PO spec §20.3, §20.5, §26): Suresh closes his request
// while Priya's pending PO holds its row. The close goes through; Ravi's
// approve is then refused with DOC's closed text, and the PO stays pending
// with no number and its approval request open. A closed request has 0
// pending to order. Undo: Ravi rejects, Priya revises, takes the row off and
// resubmits, and Ravi approves; the closed request's ordered_qty stays 0.
func TestRequest_CloseWhileHeld(t *testing.T) {
	env := purchasetest.New(t)
	req := p6CementRequest(t, env, "40")
	row := req.Lines[0].ID
	po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StatePending)

	if got := p6Pending(t, env, row)[row]; got != 0 {
		t.Errorf("pending to order while the pending PO holds the row = %s, want 0", got.Plain())
	}
	hs := p6HeldBy(t, env, req.ID)
	if len(hs) != 1 || hs[0].POID != po.ID || hs[0].State != doctype.StatePending || hs[0].Owner != env.Priya.ID ||
		hs[0].Qty != p6Q(t, "40") || hs[0].RequestLineID != row || hs[0].PODisplay != p6Label(po) {
		t.Errorf("HeldBy = %+v, want Priya's pending %s holding 40 of row %d", hs, p6Label(po), row)
	}

	p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))
	_, err := p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, "")
	p2WantInvalid(t, "approve", err, doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6DocClosed, 1, req.Number)})
	if d := p2Load(t, env, po.ID); d.State != doctype.StatePending || d.Number != "" {
		t.Errorf("after the refused approve the PO is %s %q, want pending with no number", d.State, d.Number)
	}
	if r, ok := p3Latest(t, env, po.ID); !ok || r.Outcome != "" {
		t.Errorf("approval request %+v (found %v), want it still open", r, ok)
	}
	if got := p6ReadRow(t, env, row); got.ordered != 0 || got.status != "not_ordered" {
		t.Errorf("closed request row = %+v, want ordered 0, not_ordered", got)
	}

	p2Must(t, "Ravi rejects")(p2Do(t, env, env.Ravi, po.ID, doc.ActionReject, p6RejectReason))
	p2Must(t, "Priya revises")(p2Do(t, env, env.Priya, po.ID, doc.ActionRevise, ""))
	p2Must(t, "Priya takes the row off")(p2Save(t, env, env.Priya, po.ID, nil, []map[string]string{p6Bag(t, env, "40", 0)}))
	p2Must(t, "Priya resubmits")(p2Do(t, env, env.Priya, po.ID, doc.ActionSubmit, ""))
	p2Must(t, "Ravi approves")(p2Do(t, env, env.Ravi, po.ID, doc.ActionApprove, ""))
	if got := p6ReadRow(t, env, row); got.ordered != 0 || got.status != "not_ordered" {
		t.Errorf("after the PO without the row is approved, request row = %+v, want ordered 0, not_ordered", got)
	}
	if got := p6Pending(t, env, row)[row]; got != 0 {
		t.Errorf("pending to order of the closed request's row = %s, want 0 (a closed request has 0 pending)", got.Plain())
	}
	if hs := p6HeldBy(t, env, req.ID); len(hs) != 0 {
		t.Errorf("HeldBy after the row is taken off = %+v, want none", hs)
	}
}

// PO-075 (PO spec §20.5, §27.5; CD-280 D1): ordering more than the request row
// asked saves, and OrderWarnings names the excess per line, as
// types.FormatQty with no unit. At or under the row's quantity there is no
// such warning.
func TestPO_OverRequestWarning(t *testing.T) {
	env := purchasetest.New(t)
	req := p6Request(t, env, env.Suresh, p5Header(t, env, "METRO-SITE", "PRJ-METRO"), []map[string]string{
		p5Line(t, env, "CEMENT-OPC", "BAG", "40"), p5Line(t, env, "MS-ANGLE-50", "KGS", "100")})
	if req.Number != "PR/26-27/00001" {
		t.Fatalf("request number %q, want PR/26-27/00001", req.Number)
	}
	lines := func(bags, kg string) []map[string]string {
		return []map[string]string{
			p6Bag(t, env, bags, req.Lines[0].ID),
			p6Link(p2Line(t, env, "MS-ANGLE-50", "KGS", kg, "62.5"), req.Lines[1].ID),
			p2Line(t, env, "WIRE-18G", "KGS", "500", "96"), // no request row: never warned
		}
	}
	res, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), lines("45", "102.5"))
	if err != nil {
		t.Fatalf("save 45 Bag and 102.5 kg: %v %+v, want saved (over-request only warns)", err, p5Fields(err))
	}
	want := []string{
		p6Fmt(p6MsgOverRequest, 1, "5", "PR/26-27/00001"),
		p6Fmt(p6MsgOverRequest, 2, "2.5", "PR/26-27/00001"),
	}
	if got := p6OverRequest(p2Warnings(t, env, env.Priya, res.ID)); !slices.Equal(got, want) {
		t.Errorf("over-request warnings = %q, want %q", got, want)
	}

	p2Must(t, "save 40 Bag and 100 kg")(p2Save(t, env, env.Priya, res.ID, nil, lines("40", "100")))
	if got := p6OverRequest(p2Warnings(t, env, env.Priya, res.ID)); len(got) != 0 {
		t.Errorf("over-request warnings at the requested quantities = %q, want none", got)
	}
}

// p6OverRequest are the over-request warnings among ws, in order.
func p6OverRequest(ws []string) []string {
	var out []string
	for _, w := range ws {
		if strings.Contains(w, " more than request ") {
			out = append(out, w)
		}
	}
	return out
}
