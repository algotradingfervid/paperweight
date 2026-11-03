package mstweb_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// No case ID: UI v2 R10 (§5.5 item 1, shell H8). A master declares its forms'
// section cards once, as data; every form of the master (new page, record
// page, section, wizard step) draws the cards holding its fields, and the
// declaration is checked when the routes are built.

// moreCards puts moreType's fields in two cards, the second in another order
// than the DocType's.
var moreCards = []mstweb.FormCard{
	{Title: "Who", Icon: "package", Description: "How it appears in pickers.", Fields: []string{"name", "code"}},
	{Title: "Extras", Icon: "tags", Description: "What most people skip.", Fields: []string{"no_print", "note", "urgent"}},
}

func withCards(s *screen) {
	onMore(s)
	s.Cards = moreCards
}

// cardFields is the input ids of each card in the form, with its title.
func cardFields(r webtest.Response) [][]string {
	var out [][]string
	r.Doc.Find("form section.card").Each(func(_ int, c *goquery.Selection) {
		row := []string{webtest.Squash(c.Find(".card-title").First().Text())}
		c.Find("input:not([type=hidden]), select, textarea").Each(func(_ int, in *goquery.Selection) {
			row = append(row, in.AttrOr("id", ""))
		})
		out = append(out, row)
	})
	return out
}

func wantCards(t *testing.T, r webtest.Response, want ...[]string) {
	t.Helper()
	got := cardFields(r)
	if len(got) != len(want) {
		t.Fatalf("cards = %q; want %q\nbody: %s", got, want, webtest.Squash(r.Body))
	}
	for i := range want {
		if strings.Join(got[i], " ") != strings.Join(want[i], " ") {
			t.Errorf("card %d = %q; want %q", i, got[i], want[i])
		}
	}
}

// A form is its cards in order, each with the tile, the heading and the line,
// and the fields in the card's order; a More field goes in its own card's
// group, after the card's other fields.
func TestCards_NewPage(t *testing.T) {
	h := harness(t, stubService{}, withCards)
	r := h.Get(t, h.Env.Kavya, base+"/new")
	webtest.WantStatus(t, r, http.StatusOK)
	wantCards(t, r,
		[]string{"Who", "f-name", "f-code"},
		[]string{"Extras", "f-no_print", "f-note", "f-urgent"},
	)
	first := r.Doc.Find("form section.card").First()
	if first.Find(".card-icon svg").Length() != 1 {
		t.Error("first card has no icon tile")
	}
	if got := webtest.Squash(first.Find(".card-desc").Text()); got != "How it appears in pickers." {
		t.Errorf("description = %q", got)
	}
	if n := r.Doc.Find("form > div input:not([type=hidden])").Not("section.card input").Length(); n != 0 {
		t.Errorf("%d inputs outside a card", n)
	}
	if n := first.Find("details.disclosure").Length(); n != 0 {
		t.Errorf("a card with no More field has %d More details groups", n)
	}
}

// A card with a More field and a plain one groups the More field; the group
// sits at the end of that card. A card of only More fields groups them too
// (Party's Payment card); only a form of only More fields shows them plainly.
func TestCards_MoreStaysInItsCard(t *testing.T) {
	cards := func(s *screen) {
		onMore(s)
		s.Cards = []mstweb.FormCard{
			{Title: "Who", Icon: "package", Description: "One line.", Fields: []string{"code", "name", "note"}},
			{Title: "Extras", Icon: "tags", Description: "One line.", Fields: []string{"urgent", "no_print"}},
		}
	}
	h := harness(t, stubService{}, cards)
	r := h.Get(t, h.Env.Kavya, base+"/new")
	d := r.Doc.Find("form section.card").First().Find("details.disclosure")
	if d.Length() != 1 || d.Find("#f-note").Length() != 1 {
		t.Fatalf("first card has no More details holding Note\nbody: %s", webtest.Squash(r.Body))
	}
	if got := webtest.Squash(d.Find("summary").Text()); got != "More details · note" {
		t.Errorf("summary = %q", got)
	}
	if isOpen(d) {
		t.Error("More details is open with nothing typed")
	}
	if n := r.Doc.Find("form details.disclosure").Length(); n != 2 {
		t.Errorf("%d groups, want one in each card", n)
	}
	extras := r.Doc.Find("form section.card").Eq(1).Find("details.disclosure")
	if got := webtest.Squash(extras.Find("summary").Text()); got != "More details · urgent, print" {
		t.Errorf("Extras card summary = %q", got)
	}
	h = harness(t, stubService{rec: moreRecord(map[string]string{"note": "x"})}, cards)
	if !isOpen(h.Get(t, h.Env.Kavya, record).Doc.Find("form details.disclosure").First()) {
		t.Error("record with a Note: More details is closed")
	}
	// A section of only More fields shows them plainly, in their card.
	h = harness(t, stubService{rec: moreRecord(nil)}, cards, func(s *screen) {
		s.Sections = []docscreen.FormStep{
			{Slug: "who", Label: "Who", Fields: []string{"code", "name", "note"}},
			{Slug: "extra", Label: "Extra", Fields: []string{"urgent", "no_print"}},
		}
	})
	sec := h.Get(t, h.Env.Kavya, record+"/edit/extra")
	webtest.WantStatus(t, sec, http.StatusOK)
	noGroup(t, sec, "section of only More fields")
	wantCards(t, sec, []string{"Extras", "f-urgent", "f-no_print"})
}

// A record's page draws the same cards.
func TestCards_RecordPage(t *testing.T) {
	h := harness(t, stubService{rec: moreRecord(nil)}, withCards)
	r := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, r, http.StatusOK)
	wantCards(t, r,
		[]string{"Who", "f-name", "f-code"},
		[]string{"Extras", "f-no_print", "f-note", "f-urgent"},
	)
}

// A section or a wizard step draws only the cards holding its fields, with
// only its fields.
func TestCards_SectionAndWizardStep(t *testing.T) {
	sections := func(s *screen) {
		withCards(s)
		s.Sections = []docscreen.FormStep{
			{Slug: "who", Label: "Who", Fields: []string{"code", "name"}},
			{Slug: "rest", Label: "Rest", Fields: []string{"urgent", "note", "no_print"}},
		}
		s.Cards = []mstweb.FormCard{moreCards[0], moreCards[1]}
	}
	h := harness(t, stubService{rec: moreRecord(nil)}, sections)
	wantCards(t, h.Get(t, h.Env.Kavya, record+"/edit/who"), []string{"Who", "f-name", "f-code"})

	wiz := func(s *screen) {
		moreWizard(s)
		s.Copy.Icon = "package"
		s.Cards = []mstweb.FormCard{
			{Title: "One", Icon: "package", Description: "One line.", Fields: []string{"code"}},
			{Title: "Two", Icon: "tags", Description: "One line.", Fields: []string{"name"}},
			{Title: "Three", Icon: "tags", Description: "One line.", Fields: []string{"note", "urgent", "no_print"}},
		}
	}
	b := wizardHarness(t, nil, wiz).Browser(h.Env.Kavya)
	r := b.Get(t, wizardFirst)
	webtest.WantStatus(t, r, http.StatusOK)
	wantCards(t, r,
		[]string{"One", "f-code"},
		[]string{"Three", "f-note", "f-urgent", "f-no_print"},
	)
}

// The rows editor sits in the card that declares Rows, after the fields.
func TestCards_RowsCard(t *testing.T) {
	cards := func(s *screen) {
		s.Cards = []mstweb.FormCard{
			itemCards[0], itemCards[1],
			{Title: "Units", Icon: "ruler", Description: "How units convert.", Rows: true},
		}
	}
	h := harness(t, stubService{}, cards)
	r := h.Get(t, h.Env.Kavya, base+"/new")
	card := r.Doc.Find("form section.card").Last()
	if got := webtest.Squash(card.Find(".card-title").Text()); got != "Units" {
		t.Fatalf("last card = %q", got)
	}
	if card.Find("fieldset#f-lines").Length() != 1 {
		t.Errorf("the rows editor isn't in its card\nbody: %s", webtest.Squash(r.Body))
	}
	if n := r.Doc.Find("fieldset#f-lines").Length(); n != 1 {
		t.Errorf("%d rows editors, want 1", n)
	}
	if n := r.Doc.Find("form section.card").Length(); n != 3 {
		t.Errorf("%d cards, want 3", n)
	}
}

// A declaration that doesn't fit the form panics when the routes are built,
// never leaving a field off the page or drawing a blank tile.
func TestCards_BadDeclarationPanics(t *testing.T) {
	ok := func() []mstweb.FormCard {
		return []mstweb.FormCard{
			{Title: "Who", Icon: "package", Description: "One line.", Fields: []string{"code", "name"}},
			{Title: "Extras", Icon: "tags", Description: "One line.", Fields: []string{"note", "urgent", "no_print"}},
		}
	}
	for name, tc := range map[string]struct {
		edit func(c []mstweb.FormCard) []mstweb.FormCard
		want string
	}{
		"unknown field": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[0].Fields = append(c[0].Fields, "nonsense")
			return c
		}, "card Who names no field nonsense"},
		"left-out field": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[0].Fields = append(c[0].Fields, "disabled")
			return c
		}, "card Who names no field disabled"},
		"field left out": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Fields = []string{"note", "urgent"}
			return c
		}, "no card holds field no_print"},
		"field twice": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Fields = append(c[1].Fields, "code")
			return c
		}, "field code is in two cards"},
		"unknown icon": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Icon = "nonsense"
			return c
		}, "card Extras names no icon nonsense"},
		"blank icon": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Icon = ""
			return c
		}, "card Extras names no icon "},
		"no title": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Title = ""
			return c
		}, "a card needs a Title and a Description"},
		"no description": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Description = ""
			return c
		}, "a card needs a Title and a Description"},
		"rows without rows": {func(c []mstweb.FormCard) []mstweb.FormCard {
			c[1].Rows = true
			return c
		}, "card Extras has Rows, but the form has none"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, _ := recover().(string)
				if !strings.Contains(got, tc.want) {
					t.Errorf("panic = %q; want it to hold %q", got, tc.want)
				}
			}()
			harness(t, stubService{}, func(s *screen) {
				onMore(s)
				s.Cards = tc.edit(ok())
			})
		})
	}
}

// Cards need the empty list's icon: a master that declares its forms declares
// its object.
func TestCards_NeedCopyIcon(t *testing.T) {
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, "Copy.Icon is required with Cards") {
			t.Errorf("panic = %q", got)
		}
	}()
	harness(t, stubService{}, withCards, func(s *screen) { s.Copy.Icon = "" })
}

// A Fills entry needs a code field and a Select, on both sides.
func TestCards_FillsNeedsACodeAndASelect(t *testing.T) {
	code := map[string]components.CodeKind{"hsn_sac": components.GSTIN}
	sel := map[string][]components.Option{"name": {{Value: "27", Label: "Maharashtra"}}}
	for name, tc := range map[string]struct {
		codes map[string]components.CodeKind
		sels  map[string][]components.Option
	}{"no code": {nil, sel}, "no select": {code, nil}} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, _ := recover().(string)
				if !strings.Contains(got, "Fills needs a Codes field and a Selects field") {
					t.Errorf("panic = %q", got)
				}
			}()
			harness(t, stubService{}, func(s *screen) {
				s.Codes, s.Selects, s.Fills = tc.codes, tc.sels, map[string]string{"hsn_sac": "name"}
			})
		})
	}
	h := harness(t, stubService{}, func(s *screen) {
		s.Codes, s.Selects, s.Fills = code, sel, map[string]string{"hsn_sac": "name"}
	})
	r := h.Get(t, h.Env.Kavya, base+"/new")
	if got := r.Doc.Find("#f-hsn_sac").AttrOr("data-fills", ""); got != "f-name" {
		t.Errorf("data-fills = %q", got)
	}
}

// A code field with no Fills names none, and keeps its MaxLen and example.
func TestCards_CodeFieldAttrs(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.Codes = map[string]components.CodeKind{"hsn_sac": components.HSN, "code": components.GSTIN}
		s.Fills = nil
	})
	r := h.Get(t, h.Env.Kavya, base+"/new")
	in := r.Doc.Find("#f-hsn_sac")
	if _, ok := in.Attr("data-fills"); ok {
		t.Error("a field with no Fills has data-fills")
	}
	if got := in.AttrOr("maxlength", ""); got != "8" {
		t.Errorf("maxlength = %q", got)
	}
	if got := in.AttrOr("placeholder", ""); got != "e.g. 7216" {
		t.Errorf("placeholder = %q", got)
	}
}

// itemCards hold Item's form fields, at most 6 a card.
var itemCards = []mstweb.FormCard{
	{Title: "All", Icon: "package", Description: "One line.", Fields: []string{"code", "name", "item_type", "stock_uom_id", "hsn_sac", "purchase_uom_id"}},
	{Title: "Rest", Icon: "percent", Description: "One line.", Fields: []string{"default_tax_code_id", "is_stock", "standard_rate"}},
}

// A form with rows needs a card for them, once.
func TestCards_RowsNeedOneCard(t *testing.T) {
	rows := mstweb.FormCard{Title: "Units", Icon: "ruler", Description: "One line.", Rows: true}
	for name, cards := range map[string][]mstweb.FormCard{
		"none": {itemCards[0], itemCards[1]},
		"two":  {itemCards[0], itemCards[1], rows, rows},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, _ := recover().(string)
				if !strings.Contains(got, "one card needs Rows") {
					t.Errorf("panic = %q", got)
				}
			}()
			harness(t, stubService{}, func(s *screen) { s.Cards = cards })
		})
	}
}

// A wizard's fields and Choices need their cards as a section's do.
func TestCards_ChoiceBelongsInACard(t *testing.T) {
	const choice = "kind"
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, "no card holds field "+choice) {
			t.Errorf("panic = %q", got)
		}
	}()
	harness(t, stubService{}, func(s *screen) {
		onMore(s)
		s.Choices = map[string]mstweb.Choice{choice: {Label: "Kind", Options: []components.Option{{Value: "a", Label: "A"}}}}
		s.Sections = []docscreen.FormStep{{Slug: "one", Label: "One", Fields: []string{"code", choice}}}
		s.Cards = []mstweb.FormCard{{Title: "Who", Icon: "package", Description: "One line.", Fields: []string{"code"}}}
	})
}

// A Choice's Hint shows under its control.
func TestCards_ChoiceHint(t *testing.T) {
	h := harness(t, stubService{rec: moreRecord(nil)}, func(s *screen) {
		onMore(s)
		s.Choices = map[string]mstweb.Choice{"kind": {Label: "Kind", Hint: "Only vendors reach orders.", Options: []components.Option{{Value: "a", Label: "A"}}}}
		s.Sections = []docscreen.FormStep{{Slug: "one", Label: "One", Fields: []string{"code", "kind"}}}
	})
	webtest.WantText(t, h.Get(t, h.Env.Kavya, record+"/edit/one"), "Only vendors reach orders.")
}

// Errors list in the page's order, whichever produced them first: a parse
// error on a row or a late field never comes ahead of Code (shell M15), on
// the new page, a record's page, a section and a wizard step. The cards set
// the order.
func TestCards_ErrorsInPageOrder(t *testing.T) {
	summary := func(r webtest.Response) string {
		var got []string
		r.Doc.Find(".error-summary a").Each(func(_ int, a *goquery.Selection) {
			got = append(got, a.AttrOr("href", ""))
		})
		return strings.Join(got, " ")
	}
	parseErrs := func(v map[string]string) (stubRecord, []doctype.FieldError) {
		return v, []doctype.FieldError{
			{Field: "no_print", Message: "Print is wrong."},
			{Field: "note", Message: "Note is wrong."},
			{Field: "unknown", Message: "Nobody's."},
			{Field: "code", Message: "Code is wrong."},
			{Field: "name", Message: "Name is wrong."},
		}
	}
	const want = "#f-name #f-code #f-no_print #f-note #f-unknown"
	t.Run("new page", func(t *testing.T) {
		h := harness(t, stubService{}, withCards, func(s *screen) { s.ToInput = parseErrs })
		r := h.Post(t, h.Env.Kavya, base+"/new", url.Values{})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if got := summary(r); got != want {
			t.Errorf("summary = %q; want %q", got, want)
		}
	})
	t.Run("record page", func(t *testing.T) {
		h := harness(t, stubService{rec: moreRecord(nil)}, withCards, func(s *screen) { s.ToInput = parseErrs })
		r := h.Post(t, h.Env.Kavya, record, url.Values{"version": {"3"}})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if got := summary(r); got != want {
			t.Errorf("summary = %q; want %q", got, want)
		}
	})
	t.Run("section", func(t *testing.T) {
		h := harness(t, stubService{rec: moreRecord(nil)}, withCards, func(s *screen) {
			s.ToInput = parseErrs
			s.Sections = []docscreen.FormStep{{Slug: "all", Label: "All", Fields: []string{"code", "name", "note", "urgent", "no_print"}}}
		})
		r := h.Post(t, h.Env.Kavya, record+"/edit/all", url.Values{"version": {"3"}})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if got := summary(r); got != want {
			t.Errorf("summary = %q; want %q", got, want)
		}
	})
	t.Run("wizard step", func(t *testing.T) {
		h := wizardHarness(t, nil, func(s *screen) {
			moreWizard(s)
			w := s.Wizard.(mstweb.CreateWizard[stubDraft])
			w.ToInput = func(map[string]string) (stubDraft, []doctype.FieldError) {
				return stubDraft{}, []doctype.FieldError{{Field: "note", Message: "Note is wrong."}, {Field: "code", Message: "Code is wrong."}}
			}
			s.Wizard = w
		})
		r := h.Browser(h.Env.Kavya).Post(t, wizardFirst, url.Values{"code": {"X"}})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if got, want := summary(r), "#f-code #f-note"; got != want {
			t.Errorf("summary = %q; want %q", got, want)
		}
	})
	t.Run("rows after the fields, by row", func(t *testing.T) {
		h := harness(t, stubService{}, func(s *screen) {
			s.ToInput = func(map[string]string) (stubRecord, []doctype.FieldError) {
				return nil, []doctype.FieldError{
					{Field: "uom_id", Line: 2, Message: "Row 2."},
					{Field: "factor", Line: 1, Message: "Row 1."},
					{Field: "code", Message: "Code is wrong."},
				}
			}
		})
		r := h.Post(t, h.Env.Kavya, base+"/new", url.Values{})
		webtest.WantStatus(t, r, http.StatusUnprocessableEntity)
		if got, want := summary(r), "#f-code #f-lines-1-factor #f-lines-2-uom_id"; got != want {
			t.Errorf("summary = %q; want %q", got, want)
		}
	})
}

// A section with nothing in it says so, on the record page and on Check.
func TestCards_EmptySectionSaysNotSet(t *testing.T) {
	const notSet = "Not set"
	sections := func(s *screen) {
		onMore(s)
		s.Sections = []docscreen.FormStep{
			{Slug: "code", Label: "Coded", Fields: []string{"code"}},
			{Slug: "extra", Label: "Extra", Fields: []string{"note"}},
		}
	}
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, sections)
	r := h.Get(t, h.Env.Kavya, record)
	webtest.WantStatus(t, r, http.StatusOK)
	cards := r.Doc.Find("section.card")
	if got := webtest.Squash(cards.Eq(1).Text()); !strings.Contains(got, notSet) {
		t.Errorf("empty section = %q; want %q", got, notSet)
	}
	if got := webtest.Squash(cards.Eq(0).Text()); strings.Contains(got, notSet) {
		t.Errorf("filled section = %q; has %q", got, notSet)
	}

	b := wizardHarness(t, nil, moreWizard).Browser(h.Env.Kavya)
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)
	webtest.WantStatus(t, b.Post(t, base+"/new/more", url.Values{}), http.StatusSeeOther)
	check := b.Get(t, base+"/new/check")
	webtest.WantStatus(t, check, http.StatusOK)
	got := check.Doc.Find("section.card")
	if s := webtest.Squash(got.Eq(1).Text()); !strings.Contains(s, notSet) {
		t.Errorf("Check's empty step = %q; want %q", s, notSet)
	}
	if s := webtest.Squash(got.Eq(0).Text()); strings.Contains(s, notSet) {
		t.Errorf("Check's filled step = %q; has %q", s, notSet)
	}
}

// A master's empty list carries its own icon, not the inbox, also when every
// record is disabled; a Copy with an unknown icon panics.
func TestCards_EmptyListIcon(t *testing.T) {
	icon := func(s *screen) { s.Copy.Icon = "package" }
	h := harness(t, stubService{}, icon)
	r := h.Get(t, h.Env.Kavya, base)
	webtest.WantStatus(t, r, http.StatusOK)
	got, _ := r.Doc.Find(".empty-state-icon svg").Html()
	if want := iconInner(t, "package"); got != want {
		t.Errorf("empty list icon = %q; want package's %q", got, want)
	}
	plain := harness(t, stubService{}, func(s *screen) { s.Copy.Icon = "" })
	got, _ = plain.Get(t, plain.Env.Kavya, base).Doc.Find(".empty-state-icon svg").Html()
	if want := iconInner(t, "inbox"); got != want {
		t.Errorf("no Copy.Icon: icon = %q; want the inbox", got)
	}
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, "Copy.Icon names no icon nonsense") {
			t.Errorf("panic = %q", got)
		}
	}()
	harness(t, stubService{}, func(s *screen) { s.Copy.Icon = "nonsense" })
}

// iconInner is an icon's markup as a page shows it.
func iconInner(t *testing.T, name string) string {
	t.Helper()
	var b strings.Builder
	if err := components.ObjectEmpty(name, "x", "").Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	s, _ := doc.Find(".empty-state-icon svg").Html()
	return s
}

// With Sections or a Wizard, a form draws only its steps' fields, so the cards
// need hold no other.
func TestCards_StepsNameTheFields(t *testing.T) {
	cards := []mstweb.FormCard{{Title: "Who", Icon: "package", Description: "One line.", Fields: []string{"code", "name"}}}
	h := harness(t, stubService{rec: moreRecord(nil)}, onMore, func(s *screen) {
		s.Sections = []docscreen.FormStep{{Slug: "who", Label: "Who", Fields: []string{"code", "name"}}}
		s.Cards = cards
	})
	wantCards(t, h.Get(t, h.Env.Kavya, record+"/edit/who"), []string{"Who", "f-code", "f-name"})
	w := wizardHarness(t, nil, func(s *screen) {
		s.DocType = moreType
		s.Copy.Icon = "package"
		wz := s.Wizard.(mstweb.CreateWizard[stubDraft])
		wz.Steps = []docscreen.FormStep{{Slug: "details", Label: "Details", Fields: []string{"code"}}, {Slug: "more", Label: "More", Fields: []string{"name"}}}
		s.Wizard = wz
		s.Cards = cards
	})
	wantCards(t, w.Browser(w.Env.Kavya).Get(t, wizardFirst), []string{"Who", "f-code"})
}

// The rows card may come first.
func TestCards_RowsCardFirst(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.Cards = []mstweb.FormCard{
			{Title: "Units", Icon: "ruler", Description: "One line.", Rows: true},
			itemCards[0], itemCards[1],
		}
	})
	r := h.Get(t, h.Env.Kavya, base+"/new")
	if r.Doc.Find("section.card").FilterFunction(func(_ int, c *goquery.Selection) bool {
		return webtest.Squash(c.Find(".card-title").Text()) == "Units"
	}).Find("fieldset#f-lines").Length() != 1 {
		t.Errorf("the rows editor isn't in Units\nbody: %s", webtest.Squash(r.Body))
	}
}

// A row error lists after a header error that names no form field.
func TestCards_RowErrorsAfterUnknownField(t *testing.T) {
	h := harness(t, stubService{}, func(s *screen) {
		s.ToInput = func(map[string]string) (stubRecord, []doctype.FieldError) {
			return nil, []doctype.FieldError{
				{Field: "uom_id", Line: 1, Message: "Row 1."},
				{Field: "nobody", Message: "Nobody's."},
				{Field: "code", Message: "Code is wrong."},
			}
		}
	})
	r := h.Post(t, h.Env.Kavya, base+"/new", url.Values{})
	var got []string
	r.Doc.Find(".error-summary a").Each(func(_ int, a *goquery.Selection) { got = append(got, a.AttrOr("href", "")) })
	if want := "#f-code #f-nobody #f-lines-1-uom_id"; strings.Join(got, " ") != want {
		t.Errorf("summary = %q; want %q", got, want)
	}
}

// rowsWizard has a row step whose first row opens with other values than the
// rest: Party's first address bills.
func rowsWizard(first map[string]string) func(*screen) {
	return func(s *screen) {
		w := s.Wizard.(mstweb.CreateWizard[stubDraft])
		w.Steps = []docscreen.FormStep{w.Steps[0], {Slug: "more", Label: "More"}}
		w.Rows = map[string]mstweb.RowStep{"more": {
			Legend: "Rows", AddLabel: "Add row", Noun: "row", Defaults: map[string]string{"kind": "plain"}, FirstDefaults: first,
			Fields: []doctype.Field{{Name: "note", Label: "Note", Type: doctype.Text}, {Name: "kind", Label: "Kind", Type: doctype.Text}},
		}}
		s.Wizard = w
	}
}

// The first row opens with FirstDefaults and later ones with Defaults; without
// FirstDefaults the first opens with Defaults. A row holding only its own
// defaults is blank and goes at Next; the first row holding the others' isn't.
func TestWizard_FirstRowDefaults(t *testing.T) {
	const step = base + "/new/more"
	first := map[string]string{"kind": "first"}
	open := func(t *testing.T, opt func(*screen)) *webtest.Browser {
		t.Helper()
		b := wizardHarness(t, nil, opt).Browser(wizardHarness(t, nil).Env.Kavya)
		webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)
		return b
	}
	b := open(t, rowsWizard(first))
	r := b.Post(t, step, url.Values{"_rows": {"add"}})
	webtest.WantStatus(t, r, http.StatusOK)
	if v, _ := r.Value("lines[1].kind"); v != "first" {
		t.Errorf("first row kind = %q; want first", v)
	}
	r = b.Post(t, step, url.Values{"lines[1].kind": {"first"}, "lines[1].note": {"n"}, "_rows": {"add"}})
	if v, _ := r.Value("lines[2].kind"); v != "plain" {
		t.Errorf("second row kind = %q; want plain", v)
	}
	if v, _ := open(t, rowsWizard(nil)).Post(t, step, url.Values{"_rows": {"add"}}).Value("lines[1].kind"); v != "plain" {
		t.Errorf("no FirstDefaults: first row kind = %q; want plain", v)
	}
	b = open(t, rowsWizard(first))
	webtest.WantRedirect(t, b.Post(t, step, url.Values{"lines[1].kind": {"first"}}), wizardCheck)
	if got := webtest.Squash(b.Get(t, wizardCheck).Text()); strings.Contains(got, "Kind") {
		t.Errorf("a first row at its defaults reached Check: %s", got)
	}
	b = open(t, rowsWizard(first))
	webtest.WantRedirect(t, b.Post(t, step, url.Values{"lines[1].kind": {"plain"}}), wizardCheck)
	if got := webtest.Squash(b.Get(t, wizardCheck).Text()); !strings.Contains(got, "Kindplain") {
		t.Errorf("a first row holding plain was dropped: %s", got)
	}
	b = open(t, rowsWizard(first))
	webtest.WantStatus(t, b.Post(t, step, url.Values{"_rows": {"remove:9"}}), http.StatusBadRequest)
}

// A record page's section cards carry their step's icon tile.
func TestCards_SectionSummaryIcon(t *testing.T) {
	h := harness(t, stubService{rec: stubRecord{"code": "X"}}, onMore, func(s *screen) {
		s.Sections = []docscreen.FormStep{{Slug: "code", Label: "Coded", Icon: "truck", Fields: []string{"code"}}}
	})
	got, _ := h.Get(t, h.Env.Kavya, record).Doc.Find("section.card .card-icon svg").First().Html()
	if want := iconInner(t, "truck"); got != want {
		t.Errorf("section icon = %q; want truck's", got)
	}
}

// CheckCards holds a SubForm's cards to the same rules as a Screen's, and
// refuses a Rows card there; a card holds 1 to 6 fields everywhere.
func TestCards_CheckCards(t *testing.T) {
	names := []string{"a", "b", "c"}
	card := func(fields ...string) mstweb.FormCard {
		return mstweb.FormCard{Title: "One", Icon: "package", Description: "One line.", Fields: fields}
	}
	mstweb.CheckCards("sub", []mstweb.FormCard{card("a", "b"), card("c")}, names) // fits: no panic
	for name, tc := range map[string]struct {
		cards []mstweb.FormCard
		want  string
	}{
		"unknown":  {[]mstweb.FormCard{card("a", "b", "c", "x")}, "card One names no field x"},
		"left out": {[]mstweb.FormCard{card("a", "b")}, "no card holds field c"},
		"twice":    {[]mstweb.FormCard{card("a", "b", "c"), card("a")}, "field a is in two cards"},
		"seven":    {[]mstweb.FormCard{card("a", "b", "c", "a", "b", "c", "a")}, "card One holds 7 fields; a card holds 1 to 6"},
		"empty":    {[]mstweb.FormCard{card("a", "b", "c"), card()}, "card One holds 0 fields; a card holds 1 to 6"},
		"rows":     {[]mstweb.FormCard{card("a", "b", "c"), {Title: "R", Icon: "ruler", Description: "One line.", Rows: true}}, "card R has Rows, but the form has none"},
		"bad icon": {[]mstweb.FormCard{{Title: "One", Icon: "nonsense", Description: "x", Fields: names}}, "card One names no icon nonsense"},
		"no words": {[]mstweb.FormCard{{Title: "One", Icon: "package", Fields: names}}, "a card needs a Title and a Description"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, _ := recover().(string)
				if got != "mstweb: sub: "+tc.want {
					t.Errorf("panic = %q; want %q", got, "mstweb: sub: "+tc.want)
				}
			}()
			mstweb.CheckCards("sub", tc.cards, names)
		})
	}
}

// A Rows card holds no Fields: the rows editor has a card of its own.
func TestCards_RowsCardHasNoFields(t *testing.T) {
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, "card Units has Rows and Fields") {
			t.Errorf("panic = %q", got)
		}
	}()
	harness(t, stubService{}, func(s *screen) {
		s.Cards = []mstweb.FormCard{
			{Title: "All", Icon: "package", Description: "One line.", Fields: []string{"code", "name", "item_type", "stock_uom_id", "hsn_sac", "purchase_uom_id"}},
			{Title: "More", Icon: "package", Description: "One line.", Fields: []string{"default_tax_code_id", "is_stock"}},
			{Title: "Units", Icon: "ruler", Description: "One line.", Fields: []string{"standard_rate"}, Rows: true},
		}
	})
}

const oneRowType = "mstweb.OneRowR10"

func init() {
	mstkit.RegisterMaster(doctype.DocType{
		Name: oneRowType, Label: "Stub", PluralLabel: "Stubs", Table: "item",
		Fields: []doctype.Field{
			{Name: "code", Label: "Code", Column: "code", Type: doctype.Text},
			{Name: "disabled", Label: "Disabled", Column: "disabled", Type: doctype.Check},
			{Name: "factor", Label: "Factor", Column: "factor", Type: doctype.Text, OnLine: true},
		},
	}, mstkit.LookupFunc(func(context.Context, *db.Tx, int64) (doctype.MasterInfo, bool, error) {
		return doctype.MasterInfo{}, false, nil
	}))
}

// A form with one row field needs its Rows card as one with more does.
func TestCards_OneRowFieldNeedsRowsCard(t *testing.T) {
	defer func() {
		got, _ := recover().(string)
		if !strings.Contains(got, "exactly one card needs Rows") {
			t.Errorf("panic = %q", got)
		}
	}()
	harness(t, stubService{}, func(s *screen) {
		s.DocType = oneRowType
		s.Cards = []mstweb.FormCard{{Title: "All", Icon: "package", Description: "One line.", Fields: []string{"code"}}}
	})
}

// A row step needs its Noun, and its defaults name only its fields.
func TestWizard_RowStepDeclarationsChecked(t *testing.T) {
	for name, tc := range map[string]struct {
		edit func(*mstweb.RowStep)
		want string
	}{
		"no noun":        {func(rs *mstweb.RowStep) { rs.Noun = "" }, "row step more needs a Noun"},
		"defaults":       {func(rs *mstweb.RowStep) { rs.Defaults = map[string]string{"nonsense": "x"} }, "row step more defaults name no row field nonsense"},
		"first defaults": {func(rs *mstweb.RowStep) { rs.FirstDefaults = map[string]string{"nonsense": "x"} }, "row step more defaults name no row field nonsense"},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				got, _ := recover().(string)
				if !strings.Contains(got, tc.want) {
					t.Errorf("panic = %q; want %q", got, tc.want)
				}
			}()
			wizardHarness(t, nil, rowsWizard(nil), func(s *screen) {
				w := s.Wizard.(mstweb.CreateWizard[stubDraft])
				rs := w.Rows["more"]
				tc.edit(&rs)
				w.Rows = map[string]mstweb.RowStep{"more": rs}
				s.Wizard = w
			})
		})
	}
}

// Continue leaves out rows the user never filled in and says so, once, on the
// next page; a row with something typed is kept.
func TestWizard_LeftOutEmptyRowsNote(t *testing.T) {
	const step = base + "/new/more"
	open := func(t *testing.T) *webtest.Browser {
		t.Helper()
		h := wizardHarness(t, nil, rowsWizard(nil))
		b := h.Browser(h.Env.Kavya)
		webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)
		return b
	}
	note := func(r webtest.Response) string {
		return webtest.Squash(r.Doc.Find("#wizard .banner").Text())
	}
	b := open(t)
	webtest.WantRedirect(t, b.Post(t, step, url.Values{"lines[1].note": {""}, "lines[1].kind": {"plain"}}), wizardCheck)
	check := b.Get(t, wizardCheck)
	if got := note(check); !strings.Contains(got, "Left out the empty row.") {
		t.Errorf("note = %q\nbody: %s", got, webtest.Squash(check.Doc.Find("main").Text()))
	}
	if got := note(b.Get(t, wizardCheck)); strings.Contains(got, "Left out") {
		t.Errorf("the note shows twice: %q", got)
	}

	b = open(t)
	r := b.PostHTMX(t, step, url.Values{"lines[1].note": {""}, "lines[2].note": {" "}})
	if got := note(r); !strings.Contains(got, "Left out 2 empty rows.") {
		t.Errorf("htmx note = %q (status %d)\nbody: %s", got, r.Status, webtest.Squash(r.Body))
	}

	b = open(t)
	webtest.WantRedirect(t, b.Post(t, step, url.Values{"lines[1].note": {"typed"}}), wizardCheck)
	check = b.Get(t, wizardCheck)
	if n := check.Doc.Find("#wizard .banner").Length(); n != 0 {
		t.Errorf("a filled row: %d banners, want none", n)
	}
	if !strings.Contains(webtest.Squash(check.Text()), "Notetyped") {
		t.Error("the filled row is not on Check")
	}
}

// A row step's Check card with rows and no plain fields shows the rows, never
// Not set.
func TestCards_CheckRowsAreNotNotSet(t *testing.T) {
	h := wizardHarness(t, nil, rowsWizard(nil))
	b := h.Browser(h.Env.Kavya)
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)
	webtest.WantRedirect(t, b.Post(t, base+"/new/more", url.Values{"lines[1].note": {"typed"}}), wizardCheck)
	card := b.Get(t, wizardCheck).Doc.Find("section.card").Eq(1)
	if got := webtest.Squash(card.Text()); strings.Contains(got, "Not set") || !strings.Contains(got, "Notetyped") {
		t.Errorf("row step card = %q", got)
	}
}

// Start over shows once there is a draft to drop, not on a first visit.
func TestWizard_StartOverOnlyWithADraft(t *testing.T) {
	h := wizardHarness(t, nil)
	b := h.Browser(h.Env.Kavya)
	startOver := func(r webtest.Response) int {
		return r.Doc.Find(`form[action="` + base + `/new/start-over"]`).Length()
	}
	if n := startOver(b.Get(t, wizardFirst)); n != 0 {
		t.Errorf("first visit: %d Start over forms, want none", n)
	}
	webtest.WantStatus(t, b.Post(t, wizardFirst, url.Values{"code": {"X"}}), http.StatusSeeOther)
	if n := startOver(b.Get(t, wizardFirst)); n != 1 {
		t.Errorf("with a draft: %d Start over forms, want 1", n)
	}
}
