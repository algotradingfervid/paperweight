package purchase_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/types"
)

// PO-053 guards (PO spec §20.5; CD-279 (10) G0c; CD-299 mutation pass): the
// request-state filter, the blank-site and blank-item guards of
// checkRequestRows.
func TestPO_RequestRowGuards(t *testing.T) {
	// A closed request's row is still checked: an amendment keeps the row
	// its amended-from PO held (G0c), but when another PO already holds the
	// row's full quantity the approve is refused with "is already on".
	// Priya's approved PO for 10 of 40 Bag is cancelled and amended; the
	// amendment is submitted (held 10); Arjun's draft then holds 40 (his
	// save sees 10 of 40 on other POs: saved); Suresh closes the request;
	// Ravi's approve of the amendment is refused naming Arjun's draft, and
	// the amendment stays pending.
	t.Run("closed request row already on another PO", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t, purchasetest.WithNoRules())
		p6RuleFromZero(t, env)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		po := p6Order(t, env, env.Priya, nil, []map[string]string{p6Bag(t, env, "10", row)}, doctype.StateApproved)
		p2Must(t, "Ravi cancels")(p2Do(t, env, env.Ravi, po.ID, doc.ActionCancel, p6CancelReason))
		am := p2Must(t, "Priya amends")(p2Do(t, env, env.Priya, po.ID, doc.ActionAmend, ""))
		p2Must(t, "Priya submits the amendment")(p2Do(t, env, env.Priya, am.ID, doc.ActionSubmit, ""))
		other := p6Order(t, env, env.Arjun, nil, []map[string]string{p6Bag(t, env, "40", row)}, doctype.StateDraft)
		p5Must(t, "Suresh closes the request")(p5Do(t, env, env.Suresh, req.ID, doctype.ActionClose, p5CloseReason))

		_, err := p2Do(t, env, env.Ravi, am.ID, doc.ActionApprove, "")
		p2WantInvalid(t, "approve the amendment", err,
			doctype.FieldError{Field: "request_line_id", Line: 1, Message: p6Fmt(p6MsgAlreadyOn, 1, req.Number, 1, p6Label(other))})
		if d := p2Load(t, env, am.ID); d.State != doctype.StatePending || d.Number != "" {
			t.Errorf("after the refused approve the amendment is %s %q, want pending with no number", d.State, d.Number)
		}
	})

	// Rows of a draft or cancelled request are DOC's: a site mismatch on
	// such a row adds no module error, only DOC's link text on each line.
	t.Run("draft and cancelled request rows with another site", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		draft := p5Must(t, "Suresh saves a request")(p5Create(t, env, env.Suresh, p5Header(t, env, "PUNE-WH", "PRJ-METRO"),
			[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "40")}))
		drafted := p5Load(t, env, purchase.RequestDocType, draft.ID)
		gone := p6Request(t, env, env.Suresh, p5Header(t, env, "PUNE-WH", "PRJ-METRO"),
			[]map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "40")})
		p5Must(t, "Suresh cancels the request")(p5Do(t, env, env.Suresh, gone.ID, doctype.ActionCancel, p5CancelReason))

		for _, c := range []struct {
			what string
			row  int64
			want string
		}{
			{"draft request", drafted.Lines[0].ID, p6Fmt(p6DocNotApproved, 1, p6Label(drafted))},
			{"cancelled request", gone.Lines[0].ID, p6Fmt(p6DocCancelled, 1, gone.Number)},
		} {
			before := p2CountAll(t, env)
			_, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"),
				[]map[string]string{p6Bag(t, env, "40", c.row)})
			p2WantInvalid(t, "save on a "+c.what, err, doctype.FieldError{Field: "request_line_id", Line: 1, Message: c.want})
			p2SameCounts(t, "refused save on a "+c.what, before, env)
		}
	})

	// A draft with a blank site may link a request row: the site rule waits
	// for a site, and the save goes through with the link kept.
	t.Run("blank site saves", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		res, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "", "PRJ-METRO"), []map[string]string{p6Bag(t, env, "40", row)})
		if err != nil {
			t.Fatalf("save a draft with a blank site and a request row: %v %+v, want saved", err, p5Fields(err))
		}
		if got := p2LineSnap(t, env, res.ID, "request_line_id"); len(got) != 1 || got[0]["request_line_id"] != p5Plain(row) {
			t.Errorf("saved lines %v, want request_line_id %d", got, row)
		}
	})

	// A request-linked line with a blank item gets no "is for {item}" error:
	// the item rule waits for an item. The draft saves with the link kept.
	t.Run("blank item saves", func(t *testing.T) {
		t.Parallel()
		env := purchasetest.New(t)
		req := p6CementRequest(t, env, "40")
		row := req.Lines[0].ID
		line := p6Bag(t, env, "40", row)
		line["item_id"] = ""
		res, err := p2Create(t, env, env.Priya, p2Header(t, env, "SHREE", "METRO-SITE", "PRJ-METRO"), []map[string]string{line})
		for _, f := range p5Fields(err) {
			if strings.Contains(f.Message, " is for ") {
				t.Errorf("blank item: module error %+v, want none", f)
			}
		}
		if err != nil {
			t.Fatalf("save a request-linked line with a blank item: %v %+v, want saved", err, p5Fields(err))
		}
		if got := p2LineSnap(t, env, res.ID, "request_line_id"); len(got) != 1 || got[0]["request_line_id"] != p5Plain(row) {
			t.Errorf("saved lines %v, want request_line_id %d", got, row)
		}
	})
}

// MOD-PO P6 Do 7, OpenRequestLines order (PO spec §20.4; queries
// OpenRequests): oldest needed-by first, a blank needed-by last, and two
// requests with the same needed-by in the order they were raised (oldest
// request first, the query's "then oldest request"). Raised in the order
// blank, 31-10, 31-10, 28-10, so no order is the insert order.
func TestRequest_OpenRequestLinesOrder(t *testing.T) {
	env := purchasetest.New(t)
	header := func(neededBy string) map[string]string {
		h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
		h["required_by"] = neededBy
		return h
	}
	line := func() []map[string]string { return []map[string]string{p5Line(t, env, "CEMENT-OPC", "BAG", "5")} }
	blank := p6Request(t, env, env.Suresh, header(""), line())
	first := p6Request(t, env, env.Suresh, header("2026-10-31"), line())
	second := p6Request(t, env, env.Suresh, header("2026-10-31"), line())
	soonest := p6Request(t, env, env.Suresh, header("2026-10-28"), line())

	metro, metroPrj := p5ID(t, masterID(t, env.DB, "site", "METRO-SITE")), p5ID(t, masterID(t, env.DB, "project", "PRJ-METRO"))
	got, err := env.Service.OpenRequestLines(t.Context(), env.Arjun, metro, metroPrj)
	if err != nil {
		t.Fatalf("OpenRequestLines: %v", err)
	}
	var ids []string
	for _, o := range got {
		nb := "blank"
		if o.NeededBy != (types.Date{}) {
			nb = o.NeededBy.ISO()
		}
		ids = append(ids, o.Display+" "+nb)
	}
	want := []string{
		soonest.Number + " 2026-10-28",
		first.Number + " 2026-10-31",
		second.Number + " 2026-10-31",
		blank.Number + " blank",
	}
	if !slices.Equal(ids, want) {
		t.Errorf("OpenRequestLines order =\n%s\nwant\n%s", strings.Join(ids, "\n"), strings.Join(want, "\n"))
	}
}
