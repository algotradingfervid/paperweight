package purchase_test

import (
	"slices"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// p22Search are the ids the list shows p for the search q (the list's ?q=),
// sorted.
func p22Search(t *testing.T, env *purchasetest.Env, p auth.Principal, dtName, q string) []int64 {
	t.Helper()
	dt := lookup(t, dtName)
	svc := &list.Service{DB: env.DB, Auth: env.Auth}
	res, err := svc.List(t.Context(), p, dt, list.Query{Search: q, Page: 1, PageSize: 500})
	if err != nil {
		t.Fatalf("search %s for %q as %s: %v", dtName, q, p.Name, err)
	}
	var ids []int64
	for _, r := range res.Rows {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	return ids
}

// p22Number is the stored number of dt's document id; it stops the test when
// there is none.
func p22Number(t *testing.T, env *purchasetest.Env, dt string, id int64) string {
	t.Helper()
	var d doc.Doc
	if err := env.DB.Read(t.Context(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(t.Context(), tx, dt, id)
		return err
	}); err != nil {
		t.Fatalf("load %s %d: %v", dt, id, err)
	}
	if d.Number == "" {
		t.Fatalf("%s %d has no number", dt, id)
	}
	return d.Number
}

// p22Check runs each search as who and compares the ids.
func p22Check(t *testing.T, env *purchasetest.Env, dt string, cases []p22Case) {
	t.Helper()
	for _, c := range cases {
		got := p22Search(t, env, c.who, dt, c.q)
		want := slices.Clone(c.want)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: %s searching %s for %q = %v, want %v", c.what, c.who.Name, dt, c.q, got, want)
		}
	}
}

type p22Case struct {
	what string
	who  auth.Principal
	q    string
	want []int64
}

// CD-317: the PO list's search (?q=) finds an order by its number and by
// its remarks, and never returns one the viewer can't read: Priya
// (restricted to PRJ-METRO) doesn't get the PRJ-TECHPARK order that matches,
// while the Administrator does.
func TestPO_ListSearch(t *testing.T) {
	env := purchasetest.New(t)
	metroA := purchasetest.NewOrder(t, env, purchasetest.OrderHeader(map[string]string{"remarks": "Steel for tower B"})).ID
	metroB := purchasetest.NewOrder(t, env).ID
	techHeader := p2Header(t, env, "SHREE", "TECH-SITE", "PRJ-TECHPARK")
	techHeader["remarks"] = "Steel for tower C"
	tech := p3Order(t, env, env.Admin, techHeader, p3SmallLine(t, env), false)
	numB, numTech := p22Number(t, env, purchase.DocType, metroB), p22Number(t, env, purchase.DocType, tech)

	p22Check(t, env, purchase.DocType, []p22Case{
		{"by number", env.Priya, numB, []int64{metroB}},
		{"by remarks", env.Priya, "steel for tower", []int64{metroA}},
		{"by remarks, all projects", env.Admin, "steel for tower", []int64{metroA, tech}},
		{"unreadable by number", env.Priya, numTech, nil},
		{"by number, all projects", env.Admin, numTech, []int64{tech}},
	})
}

// CD-317: the request list's search (?q=) finds a request by its number and
// by its purpose (the table has no requester name snapshot), and never
// returns one the viewer can't read: Priya (restricted to PRJ-METRO) doesn't
// get the PRJ-TECHPARK request that matches, while the Administrator does.
func TestPR_ListSearch(t *testing.T) {
	env := purchasetest.New(t)
	metroA := purchasetest.NewRequest(t, env, purchasetest.RequestHeader(map[string]string{"purpose": "Shuttering for slab 4"})).ID
	metroB := purchasetest.NewRequest(t, env).ID
	tech := purchasetest.NewRequest(t, env, purchasetest.RequestHeader(map[string]string{
		"site_id":    masterID(t, env.DB, "site", "PUNE-WH"),
		"project_id": masterID(t, env.DB, "project", "PRJ-TECHPARK"),
		"purpose":    "Shuttering for slab 5",
	})).ID
	numB, numTech := p22Number(t, env, purchase.RequestDocType, metroB), p22Number(t, env, purchase.RequestDocType, tech)

	p22Check(t, env, purchase.RequestDocType, []p22Case{
		{"by number", env.Priya, numB, []int64{metroB}},
		{"by purpose", env.Priya, "shuttering for slab", []int64{metroA}},
		{"by purpose, all projects", env.Admin, "shuttering for slab", []int64{metroA, tech}},
		{"unreadable by number", env.Priya, numTech, nil},
		{"by number, all projects", env.Admin, numTech, []int64{tech}},
	})
}
