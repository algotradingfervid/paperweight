package docscreen_test

import (
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authfake"
	"github.com/vardhan/internaltools/internal/kernel/doc/docfake"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
	"github.com/vardhan/internaltools/internal/ui/docscreen/docscreentest"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The CD-371 card DocTypes: both number their rows (title column) and list
// a Supplier link. In cardParty the required supplier_id is the party and
// ship_from is a second, optional link to the same master; cardNoParty's
// supplier_id is optional, so it has no party.
const (
	cardParty   = "docscreentest.CardParty"
	cardNoParty = "docscreentest.CardNoParty"
)

var cardOnce sync.Once

func registerCards() {
	docscreentest.RegisterSample()
	cardOnce.Do(func() {
		doctype.Register(doctype.DocType{
			Name: cardParty, Label: "Card Order", PluralLabel: "Card Orders", Table: "docscreentest_card_party", Submittable: true,
			Fields: []doctype.Field{
				{Name: "number", Label: "Number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: docscreentest.Supplier, Required: true, ListPriority: 4},
				{Name: "ship_from", Label: "Ship from", Type: doctype.Link, Link: docscreentest.Supplier, ListPriority: 4},
				{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
			},
		})
		doctype.Register(doctype.DocType{
			Name: cardNoParty, Label: "Card Note", PluralLabel: "Card Notes", Table: "docscreentest_card_noparty", Submittable: true,
			Fields: []doctype.Field{
				{Name: "number", Label: "Number", Type: doctype.Text, ReadOnly: true, ListPriority: 1},
				{Name: "supplier_id", Label: "Supplier", Type: doctype.Link, Link: docscreentest.Supplier, ListPriority: 4},
				{Name: "amount", Label: "Amount", Type: doctype.Money, ReadOnly: true, ListPriority: 2},
			},
		})
	})
	docscreentest.SetMaster(docscreentest.Supplier, 7, doctype.MasterInfo{Code: "V001", Name: "Shree Steel"})
}

// cardList renders name's list over rows for a reader of name.
func cardList(t *testing.T, name string, rows ...docscreen.ListRow) *goquery.Selection {
	t.Helper()
	registerCards()
	az := authfake.New()
	az.Grant("Buyer", name, "read", auth.ScopeAll)
	az.AddRole(priya.ID, "Buyer")
	lister := docscreentest.NewLister()
	lister.Set(name, types.Result[docscreen.ListRow]{Total: len(rows), Rows: rows})
	r := chi.NewRouter()
	docscreen.Routes(docscreen.Deps{
		DB: &docscreentest.Reader{}, Engine: docfake.New(), Auth: az, Approval: &approver{}, Lister: lister,
		Extras: docscreentest.NewExtras(), Principal: func(*http.Request) (auth.Principal, bool) { return priya, true },
	})(r)
	rec := uitest.Do(t, r, http.MethodGet, "/doc/"+name, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body)
	}
	return uitest.Doc(t, rec).Find("ul.datalist-rows > li")
}

func trimmed(s *goquery.Selection) string { return strings.TrimSpace(s.Text()) }

// cellTexts is "Label: value" for each value in sel; it removes their
// separators from the document.
func cellTexts(sel *goquery.Selection) []string {
	var out []string
	sel.Each(func(_ int, dl *goquery.Selection) {
		dl.Find(".datalist-sep").Remove()
		out = append(out, trimmed(dl.Find("dt"))+": "+trimmed(dl.Find("dd")))
	})
	return out
}

// card is what a phone card draws: row 1's title, the party line and row 3
// without the values cards don't draw (datalist-blank). rowCells is every
// row-3 value, which aligned rows (desktop) draw.
type card struct {
	title, party string
	cells        []string
}

func cardOf(li *goquery.Selection) card {
	return card{
		title: trimmed(li.Find(".datalist-r1 .datalist-title dd")),
		party: trimmed(li.Find(".datalist-party")),
		cells: cellTexts(li.Find(".datalist-r3 > dl:not(.datalist-blank)")),
	}
}

func rowCells(li *goquery.Selection) []string {
	return cellTexts(li.Find(".datalist-r3 > dl"))
}

func TestDocList_CardTitleFallsBackToDisplay(t *testing.T) {
	items := cardList(t, cardParty,
		docscreen.ListRow{
			Info:   doctype.Info{ID: 1, State: doctype.StateApproved, Number: "PO/26-27/00001", Display: "PO/26-27/00001", Party: "V001 Shree Steel"},
			Values: map[string]string{"number": "PO/26-27/00001", "supplier_id": "7", "amount": "94164.00"},
		},
		docscreen.ListRow{
			Info:   doctype.Info{ID: 57, State: doctype.StateDraft, Display: "Draft 57", Party: "V001 Shree Steel"},
			Values: map[string]string{"supplier_id": "7", "amount": "1200.50"},
		},
	)
	if items.Length() != 2 {
		t.Fatalf("got %d rows, want 2", items.Length())
	}
	for i, want := range []string{"PO/26-27/00001", "Draft 57"} {
		if got := cardOf(items.Eq(i)).title; got != want {
			t.Errorf("row %d title = %q, want %q", i+1, got, want)
		}
	}
}

func TestDocList_CardSkipsPartyColumn(t *testing.T) {
	items := cardList(t, cardParty, docscreen.ListRow{
		Info:   doctype.Info{ID: 1, State: doctype.StateApproved, Number: "PO/26-27/00001", Display: "PO/26-27/00001", Party: "V001 Shree Steel"},
		Values: map[string]string{"number": "PO/26-27/00001", "supplier_id": "7", "ship_from": "7", "amount": "94164.00"},
	})
	li := items.Eq(0)
	// The card draws one separator, between Ship from and State; the
	// skipped column draws none.
	if n := li.Find(".datalist-r3 .datalist-sep").Length(); n != 1 {
		t.Errorf("row 3 has %d separators, want 1", n)
	}
	// The party shows once, on row 2; ship_from, a link to the same master
	// that isn't the party field, stays.
	want := card{title: "PO/26-27/00001", party: "V001 Shree Steel", cells: []string{"Ship from: V001 Shree Steel", "State: Approved"}}
	if got := cardOf(li); !reflect.DeepEqual(got, want) {
		t.Errorf("card = %#v, want %#v", got, want)
	}
	if got, want := rowCells(li), []string{"Supplier: V001 Shree Steel", "Ship from: V001 Shree Steel", "State: Approved"}; !reflect.DeepEqual(got, want) {
		t.Errorf("row cells = %q, want %q (desktop keeps the party column)", got, want)
	}
}

func TestDocList_CardWithoutPartyKeepsColumns(t *testing.T) {
	items := cardList(t, cardNoParty, docscreen.ListRow{
		Info:   doctype.Info{ID: 3, State: doctype.StateDraft, Display: "Draft 3"},
		Values: map[string]string{"supplier_id": "7", "amount": "500.00"},
	})
	li := items.Eq(0)
	// No party: row 2 is the first meta column (the supplier), drawn once.
	if got := trimmed(li.Find(".datalist-r2 dd")); got != "V001 Shree Steel" {
		t.Errorf("row 2 = %q, want the supplier column", got)
	}
	want := card{title: "Draft 3", cells: []string{"State: Draft"}}
	if got := cardOf(li); !reflect.DeepEqual(got, want) {
		t.Errorf("card = %#v, want %#v", got, want)
	}
	if n := li.Find("dl.datalist-blank").Length(); n != 0 {
		t.Errorf("%d values skipped on a card with no party, want 0", n)
	}
}
