package approval_test

import (
	"slices"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/approval"
	at "github.com/vardhan/internaltools/internal/kernel/approval/approvaltest"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Why a request is with the approver, and the inbox filter by type
// (design-system §8.5; wp/polish plan P2). The screens are tested in
// web/inbox_why_filter_test.go.

// evaluate runs Evaluate for d at amount as p.
func evaluate(t *testing.T, e *at.Env, d at.Doc, amount types.Money, p auth.Principal) approval.Requirement {
	t.Helper()
	var req approval.Requirement
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) (err error) {
		req, err = e.A.Evaluate(t.Context(), tx, d.Type, d.ID, amount, p)
		return err
	}))
	return req
}

// whyByTitle is p's whole inbox as title → Why.
func whyByTitle(t *testing.T, e *at.Env, p auth.Principal) map[string]string {
	t.Helper()
	res, err := e.A.Inbox(t.Context(), p, types.Page{Number: 1, Size: 100})
	at.MustOK(t, err)
	out := map[string]string{}
	for _, it := range res.Rows {
		out[it.Title] = it.Why
	}
	return out
}

// editR1 moves rule R1's threshold, as an Administrator's edit would.
func editR1(t *testing.T, e *at.Env, from types.Money) {
	t.Helper()
	_, err := e.A.UpdateRule(t.Context(), e.Anil, e.R1, 1, approval.RuleInput{
		DocType: at.Order, FromAmount: from, ApproverType: approval.ApproverRole, Role: "Purchase Manager",
	})
	at.MustOK(t, err)
}

func titles(rows []approval.InboxItem) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.Title)
	}
	return out
}

func TestWhyText_ExactCopy(t *testing.T) {
	for _, c := range []struct {
		from types.Money
		want string
	}{
		{0, "Any amount"},
		{at.Rs(50_000), "₹50,000 or more"},
		{at.Rs(5_00_000), "₹5,00,000 or more"}, // Indian grouping
		{at.Rs(1), "₹1 or more"},
		{at.Rs(50_000) + 50, "₹50,000.50 or more"}, // paise kept when present
		{at.Rs(50_000) + 5, "₹50,000.05 or more"},
		{1, "₹0.01 or more"},
	} {
		if got := approval.WhyText(c.from); got != c.want {
			t.Errorf("WhyText(%d) = %q, want %q", c.from, got, c.want)
		}
	}
}

func TestEvaluate_RequirementCarriesRuleFromAmount(t *testing.T) {
	e := at.New(t)
	order := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	claim := e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)
	for _, c := range []struct {
		label  string
		d      at.Doc
		amount types.Money
		p      auth.Principal
		needed bool
		want   types.Money
	}{
		{"R1", order, at.Rs(60000), e.Priya, true, at.Rs(50_000)},
		{"R1 at its threshold", order, at.Rs(50_000), e.Priya, true, at.Rs(50_000)},
		{"R2", order, at.Rs(6_00_000), e.Priya, true, at.Rs(5_00_000)},
		{"E1 from zero", claim, at.Rs(3450), e.Deepak, true, 0},
		{"below every rule", order, at.Rs(40000), e.Priya, false, 0},
	} {
		req := evaluate(t, e, c.d, c.amount, c.p)
		if req.Needed != c.needed || req.FromAmount != c.want {
			t.Errorf("%s: Needed %v, FromAmount %d; want %v, %d", c.label, req.Needed, req.FromAmount, c.needed, c.want)
		}
	}
}

func TestInboxWhy_ShowsTheMatchedRule(t *testing.T) {
	e := at.New(t)
	e.AddRule(t, approval.RuleInput{DocType: at.Order, FromAmount: at.Rs(75_000) + 50, ApproverType: approval.ApproverRole, Role: "Purchase Manager"})
	for _, d := range []at.Doc{
		e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya),
		e.NewDoc(t, at.Order, 44, at.Rs(80000), e.Priya),
		e.NewDoc(t, at.Order, 50, at.Rs(6_00_000), e.Priya),
	} {
		at.MustOK(t, e.Submit(e.Priya, d))
	}
	at.MustOK(t, e.Submit(e.Deepak, e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)))

	got := whyByTitle(t, e, e.Meena)
	for title, want := range map[string]string{
		"Draft 42": "₹50,000 or more",
		"Draft 44": "₹75,000.50 or more",
		"Draft 50": "₹5,00,000 or more",
		"Draft 10": "Any amount",
	} {
		if got[title] != want {
			t.Errorf("%s Why = %q, want %q", title, got[title], want)
		}
	}
}

// The threshold shown is the one the request was opened under: UpdateRule can
// change from_amount on a rule already in use.
func TestInboxWhy_RuleEditedAfterOpenKeepsSnapshot(t *testing.T) {
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d))

	editR1(t, e, at.Rs(40_000))

	d43 := e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Priya)
	at.MustOK(t, e.Submit(e.Priya, d43))

	got := whyByTitle(t, e, e.Ravi)
	if got["Draft 42"] != "₹50,000 or more" {
		t.Errorf("Draft 42 Why after the edit = %q, want the snapshot ₹50,000 or more", got["Draft 42"])
	}
	if got["Draft 43"] != "₹40,000 or more" {
		t.Errorf("Draft 43, opened after the edit, Why = %q, want ₹40,000 or more", got["Draft 43"])
	}
	r, ok := e.Latest(t, d)
	if !ok || r.FromAmount != at.Rs(50_000) {
		t.Errorf("Draft 42 latest request ok=%v FromAmount=%d, want %d", ok, r.FromAmount, at.Rs(50_000))
	}
	var stored int64
	at.MustOK(t, e.DB.Read(t.Context(), func(tx *db.Tx) error {
		return tx.SQL().QueryRowContext(t.Context(),
			`SELECT rule_from_amount FROM approval_requests WHERE doctype = ? AND doc_id = ?`, d.Type, d.ID).Scan(&stored)
	}))
	if stored != int64(at.Rs(50_000)) {
		t.Errorf("approval_requests.rule_from_amount = %d, want %d", stored, int64(at.Rs(50_000)))
	}
}

// Open takes the threshold from the Requirement Evaluate gave it, like the
// roles, so a rule edited between the two doesn't change the snapshot.
func TestOpen_SnapshotsRequirementFromAmount(t *testing.T) {
	e := at.New(t)
	d := e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Priya)
	req := evaluate(t, e, d, at.Rs(60000), e.Priya)

	editR1(t, e, at.Rs(40_000))

	at.MustOK(t, e.DB.Write(t.Context(), func(tx *db.Tx) error {
		return e.A.Open(t.Context(), tx, d.Type, d.ID, req)
	}))
	r, ok := e.Latest(t, d)
	if !ok || r.FromAmount != at.Rs(50_000) {
		t.Errorf("latest request ok=%v FromAmount=%d, want %d from the Requirement", ok, r.FromAmount, at.Rs(50_000))
	}
}

// mixedInbox gives Meena, oldest first: Draft 41 (Order), Draft 10 (Claim),
// Draft 42 (Order), Draft 7 (Receipt), Draft 11 (Claim), Draft 43 (Order).
// Sorting by Label and sorting by DocType name give different orders here.
func mixedInbox(t *testing.T) *at.Env {
	t.Helper()
	e := at.New(t)
	e.Auth.Grant("Director", at.Receipt, "read", "all")
	e.Auth.Grant("Director", at.Receipt, "approve", "all")
	e.AddRule(t, approval.RuleInput{DocType: at.Receipt, FromAmount: 0, ApproverType: approval.ApproverRole, Role: "Director"})
	for i, s := range []struct {
		dt string
		id int64
		by auth.Principal
	}{
		{at.Order, 41, e.Priya},
		{at.Claim, 10, e.Deepak},
		{at.Order, 42, e.Priya},
		{at.Receipt, 7, e.Suresh},
		{at.Claim, 11, e.Deepak},
		{at.Order, 43, e.Priya},
	} {
		e.Clock.Set(time.Date(2026, 10, 24, 5, i, 0, 0, time.UTC))
		at.MustOK(t, e.Submit(s.by, e.NewDoc(t, s.dt, s.id, at.Rs(60000), s.by)))
	}
	return e
}

// mixedTypes is Meena's type list for mixedInbox, whatever the filter.
var mixedTypes = []approval.InboxType{
	{DocType: at.Claim, Label: "Expense Claim", Count: 2},
	{DocType: at.Receipt, Label: "Goods Receipt", Count: 1},
	{DocType: at.Order, Label: "Purchase Order", Count: 3},
}

func TestInboxOfType_FiltersBeforePaging(t *testing.T) {
	e := mixedInbox(t)
	for _, c := range []struct {
		doctype string
		page    types.Page
		want    []string
		total   int
	}{
		{"", types.Page{Number: 1, Size: 2}, []string{"Draft 41", "Draft 10"}, 6},
		{"", types.Page{Number: 3, Size: 2}, []string{"Draft 11", "Draft 43"}, 6},
		{at.Order, types.Page{Number: 1, Size: 2}, []string{"Draft 41", "Draft 42"}, 3},
		{at.Order, types.Page{Number: 2, Size: 2}, []string{"Draft 43"}, 3},
		{at.Order, types.Page{Number: 3, Size: 2}, nil, 3},
		{at.Claim, types.Page{Number: 1, Size: 25}, []string{"Draft 10", "Draft 11"}, 2},
		{at.Receipt, types.Page{}, []string{"Draft 7"}, 1}, // zero page = Number 1, Size 25
	} {
		res, _, err := e.A.InboxOfType(t.Context(), e.Meena, c.doctype, c.page)
		at.MustOK(t, err)
		if got := titles(res.Rows); !slices.Equal(got, c.want) || res.Total != c.total {
			t.Errorf("InboxOfType(%q, %+v) = %q total %d, want %q total %d", c.doctype, c.page, got, res.Total, c.want, c.total)
		}
	}
}

func TestInboxOfType_TypesCoverWholeInboxWhateverTheFilter(t *testing.T) {
	e := mixedInbox(t)
	for _, c := range []struct {
		doctype string
		page    types.Page
	}{
		{"", types.Page{Number: 1, Size: 25}},
		{"", types.Page{Number: 1, Size: 1}}, // counts are not limited to the page
		{at.Order, types.Page{Number: 1, Size: 1}},
		{at.Claim, types.Page{Number: 2, Size: 1}},
		{at.Receipt, types.Page{Number: 9, Size: 25}},
	} {
		_, got, err := e.A.InboxOfType(t.Context(), e.Meena, c.doctype, c.page)
		at.MustOK(t, err)
		if !slices.Equal(got, mixedTypes) {
			t.Errorf("InboxOfType(%q, %+v) types = %+v, want %+v", c.doctype, c.page, got, mixedTypes)
		}
	}
}

// §5's Inbox keeps its signature and behaves as the unfiltered InboxOfType.
func TestInboxOfType_EmptyTypeMatchesInbox(t *testing.T) {
	e := mixedInbox(t)
	page := types.Page{Number: 2, Size: 2}
	want, err := e.A.Inbox(t.Context(), e.Meena, page)
	at.MustOK(t, err)
	got, _, err := e.A.InboxOfType(t.Context(), e.Meena, "", page)
	at.MustOK(t, err)
	if !slices.Equal(titles(got.Rows), titles(want.Rows)) || got.Total != want.Total || got.Total != 6 {
		t.Errorf("InboxOfType(\"\") = %q total %d, Inbox = %q total %d, want the same with total 6",
			titles(got.Rows), got.Total, titles(want.Rows), want.Total)
	}
}

func TestInboxOfType_UnknownOrAbsentTypeGivesNoRows(t *testing.T) {
	e := mixedInbox(t)
	ravi := []approval.InboxType{{DocType: at.Order, Label: "Purchase Order", Count: 3}}
	for _, c := range []struct {
		label   string
		p       auth.Principal
		doctype string
		types   []approval.InboxType
	}{
		{"unknown type", e.Meena, "no.such.Type", mixedTypes},
		{"names are matched exactly", e.Meena, "fixture.order", mixedTypes},
		{"the label is not a type", e.Meena, "Purchase Order", mixedTypes},
		{"real type absent from this inbox", e.Ravi, at.Claim, ravi},
		{"empty inbox", e.Kavya, at.Order, nil},
	} {
		res, got, err := e.A.InboxOfType(t.Context(), c.p, c.doctype, types.Page{Number: 1, Size: 25})
		if err != nil {
			t.Errorf("%s: err = %v, want none", c.label, err)
			continue
		}
		if len(res.Rows) != 0 || res.Total != 0 {
			t.Errorf("%s: rows %q total %d, want none", c.label, titles(res.Rows), res.Total)
		}
		if !slices.Equal(got, c.types) {
			t.Errorf("%s: types %+v, want %+v", c.label, got, c.types)
		}
	}
}

// The filter must not leak: a document the approver can't read adds no type
// and no count. Priya holds Purchase Manager here but is restricted to
// PRJ-METRO. web/inbox_why_filter_test.go has the same case on real AUTH.
func TestInboxOfType_RestrictedUserGetsNoTypeOrCountForUnreadableDoc(t *testing.T) {
	setup := func(t *testing.T) *at.Env {
		e := at.New(t)
		e.Auth.AddRole(e.Ravi.ID, "Purchase User")
		e.Auth.AddRole(e.Priya.ID, "Purchase Manager")
		at.MustOK(t, e.Submit(e.Deepak, e.NewDoc(t, at.Claim, 10, at.Rs(3450), e.Deepak)))
		at.MustOK(t, e.Submit(e.Ravi, e.NewDoc(t, at.Order, 42, at.Rs(60000), e.Ravi, at.Project(at.ProjTechPark))))
		return e
	}
	claim := approval.InboxType{DocType: at.Claim, Label: "Expense Claim", Count: 1}

	t.Run("every document of the type hidden", func(t *testing.T) {
		e := setup(t)
		if got, want := e.InboxTitles(t, e.Priya), []string{"Draft 10"}; !slices.Equal(got, want) {
			t.Fatalf("Priya's inbox = %q, want %q", got, want)
		}
		_, got, err := e.A.InboxOfType(t.Context(), e.Priya, "", types.Page{Number: 1, Size: 25})
		at.MustOK(t, err)
		if want := []approval.InboxType{claim}; !slices.Equal(got, want) {
			t.Errorf("types = %+v, want %+v (no Purchase Order)", got, want)
		}
		res, got, err := e.A.InboxOfType(t.Context(), e.Priya, at.Order, types.Page{Number: 1, Size: 25})
		at.MustOK(t, err)
		if len(res.Rows) != 0 || res.Total != 0 {
			t.Errorf("filtered to the hidden type: rows %q total %d, want none", titles(res.Rows), res.Total)
		}
		if want := []approval.InboxType{claim}; !slices.Equal(got, want) {
			t.Errorf("filtered to the hidden type: types = %+v, want %+v", got, want)
		}
	})

	t.Run("one document of the type hidden", func(t *testing.T) {
		e := setup(t)
		at.MustOK(t, e.Submit(e.Ravi, e.NewDoc(t, at.Order, 43, at.Rs(60000), e.Ravi, at.Project(at.ProjMetro))))
		res, got, err := e.A.InboxOfType(t.Context(), e.Priya, at.Order, types.Page{Number: 1, Size: 25})
		at.MustOK(t, err)
		if want := []string{"Draft 43"}; !slices.Equal(titles(res.Rows), want) || res.Total != 1 {
			t.Errorf("Priya's orders = %q total %d, want %q total 1", titles(res.Rows), res.Total, want)
		}
		want := []approval.InboxType{claim, {DocType: at.Order, Label: "Purchase Order", Count: 1}}
		if !slices.Equal(got, want) {
			t.Errorf("Priya's types = %+v, want %+v", got, want)
		}
		// Meena, unrestricted, counts both orders.
		_, got, err = e.A.InboxOfType(t.Context(), e.Meena, "", types.Page{Number: 1, Size: 25})
		at.MustOK(t, err)
		want = []approval.InboxType{claim, {DocType: at.Order, Label: "Purchase Order", Count: 2}}
		if !slices.Equal(got, want) {
			t.Errorf("Meena's types = %+v, want %+v", got, want)
		}
	})
}
