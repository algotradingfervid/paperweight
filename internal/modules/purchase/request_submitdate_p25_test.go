package purchase_test

import (
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-PO P25 (CD-330 (1)): a Request's hidden request date is set again at
// submit, to the submit's business date (IST), so a draft saved on 31-03 and
// submitted just after IST midnight on 01-04 numbers in the new FY. A draft
// saved and submitted on the same day keeps that day.
func TestRequest_SubmitSetsRequestDate(t *testing.T) {
	for _, c := range []struct {
		what       string
		save, sub  time.Time
		date, want string
	}{
		// 12:30 UTC is 18:00 IST on 31-03; 18:31 UTC is 00:01 IST on 01-04.
		{"saved 31-03, submitted 01-04", time.Date(2027, 3, 31, 12, 30, 0, 0, time.UTC), time.Date(2027, 3, 31, 18, 31, 0, 0, time.UTC), "2027-04-01", "PR/27-28/00001"},
		{"saved and submitted 31-03", time.Date(2027, 3, 31, 4, 30, 0, 0, time.UTC), time.Date(2027, 3, 31, 12, 30, 0, 0, time.UTC), "2027-03-31", "PR/26-27/00001"},
	} {
		env := purchasetest.New(t)
		env.SetNow(t, c.save)
		h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
		h["request_date"] = ""
		draft := p5Must(t, c.what+": create")(p5Create(t, env, env.Suresh, h, p5Lines(t, env)))
		if got := p5Plain(p5Load(t, env, purchase.RequestDocType, draft.ID).Header["request_date"]); got != c.save.In(p25IST).Format(time.DateOnly) {
			t.Fatalf("%s: draft request_date %s, want the save's date", c.what, got)
		}
		env.SetNow(t, c.sub)
		res := p5Must(t, c.what+": submit")(p5Do(t, env, env.Suresh, draft.ID, doctype.ActionSubmit, ""))
		if res.Number != c.want {
			t.Errorf("%s: number %q, want %q", c.what, res.Number, c.want)
		}
		d := p5Load(t, env, purchase.RequestDocType, draft.ID)
		if got := p5Plain(d.Header["request_date"]); got != c.date || d.Number != c.want {
			t.Errorf("%s: stored request_date %s number %q, want %s %q", c.what, got, d.Number, c.date, c.want)
		}
	}
}

// MOD-PO P25 (CD-330 (1), PO spec §20.2): the submit's date is checked
// against needed-by. A draft needed by 31-03 submitted on 01-04 IST is
// refused with the needed-by text and stays an unnumbered draft dated 31-03.
func TestRequest_SubmitDateRechecksNeededBy(t *testing.T) {
	env := purchasetest.New(t)
	env.SetNow(t, time.Date(2027, 3, 31, 12, 30, 0, 0, time.UTC))
	h := p5Header(t, env, "METRO-SITE", "PRJ-METRO")
	h["request_date"], h["required_by"] = "", "2027-03-31"
	draft := p5Must(t, "create")(p5Create(t, env, env.Suresh, h, p5Lines(t, env)))
	env.SetNow(t, time.Date(2027, 3, 31, 18, 31, 0, 0, time.UTC))
	_, err := p5Do(t, env, env.Suresh, draft.ID, doctype.ActionSubmit, "")
	p5WantInvalid(t, "submit on 01-04", err, doctype.FieldError{Field: "required_by", Message: p5MsgNeededBy})
	d := p5Load(t, env, purchase.RequestDocType, draft.ID)
	if got := p5Plain(d.Header["request_date"]); got != "2027-03-31" || d.State != doctype.StateDraft || d.Number != "" {
		t.Errorf("refused submit left %s %q dated %s, want an unnumbered draft dated 2027-03-31", d.State, d.Number, got)
	}
}

var p25IST = time.FixedZone("IST", 5*3600+1800)
