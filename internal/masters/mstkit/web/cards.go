package mstweb

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/ui/icons"
)

// FormCard is one section of a master's forms (UI v2 §5.5 item 1): its icon
// tile, heading and one-line description around Fields (1 to 6), in that
// order, which is the page's order. A form draws the cards holding its fields,
// so a section or a wizard step shows only its own. A Rows card holds the rows
// editor and no Fields.
type FormCard struct {
	Title, Icon, Description string
	Fields                   []string
	Rows                     bool
}

// Card is c around inputs, for a master's own forms: a SubForm's. Each input
// comes from Pages.Input, so More fields go in the card's MoreDetails.
func Card(c FormCard, inputs []templ.Component) templ.Component {
	return formCardInputs(c, grouped(inputs))
}

// Cards is a SubForm's inputs in cards, each card's in its order: input gives
// a field's control (Pages.Input). CheckCards on the same cards and names when
// the routes are built keeps every field on the page once.
func Cards(cards []FormCard, input func(name string) templ.Component) []templ.Component {
	out := make([]templ.Component, 0, len(cards))
	for _, c := range cards {
		inputs := make([]templ.Component, len(c.Fields))
		for i, name := range c.Fields {
			inputs[i] = input(name)
		}
		out = append(out, Card(c, inputs))
	}
	return out
}

// CheckCards panics, naming owner, unless cards fit a form of names: every
// name in exactly one card and no other name, each card with its icon and
// words and 1 to 6 Fields (UI v2 §5.5), and no Rows card. A master's own
// SubForm calls it when its routes are built.
func CheckCards(owner string, cards []FormCard, names []string) {
	checkCards(func(msg string) { panic("mstweb: " + owner + ": " + msg) }, cards, names, false)
}

// checkCards is CheckCards for a form that may have rows, which then needs
// exactly one Rows card.
func checkCards(fail func(string), cards []FormCard, names []string, hasRows bool) {
	seen := map[string]bool{}
	rows := 0
	for _, c := range cards {
		if c.Title == "" || c.Description == "" {
			fail("a card needs a Title and a Description")
		}
		if !slices.Contains(icons.Names(), c.Icon) {
			fail("card " + c.Title + " names no icon " + c.Icon)
		}
		if c.Rows {
			if !hasRows {
				fail("card " + c.Title + " has Rows, but the form has none")
			}
			if len(c.Fields) > 0 {
				fail("card " + c.Title + " has Rows and Fields; the rows editor has a card of its own")
			}
			rows++
		} else if len(c.Fields) == 0 || len(c.Fields) > maxCardFields {
			fail("card " + c.Title + " holds " + strconv.Itoa(len(c.Fields)) + " fields; a card holds 1 to 6")
		}
		for _, name := range c.Fields {
			if !slices.Contains(names, name) {
				fail("card " + c.Title + " names no field " + name)
			}
			if seen[name] {
				fail("field " + name + " is in two cards")
			}
			seen[name] = true
		}
	}
	for _, name := range names {
		if !seen[name] {
			fail("no card holds field " + name)
		}
	}
	if hasRows && rows != 1 {
		fail("exactly one card needs Rows")
	}
}

// maxCardFields is the most fields a card holds (UI v2 §5.5 item 1).
const maxCardFields = 6

// checkCards panics unless the Screen's Cards fit its forms (checkCards'
// rules) and Copy.Icon is set.
func (h *handlers[R, In]) checkCards() {
	if h.Cards == nil {
		return
	}
	fail := func(msg string) { panic("mstweb: " + h.DocType + ": " + msg) }
	if h.Copy.Icon == "" {
		fail("Copy.Icon is required with Cards")
	}
	checkCards(fail, h.Cards, h.formNames(), len(h.rowFields) > 0)
}

// formNames is every field a form of the Screen draws: its steps', with
// Sections or a Wizard, else its fields.
func (h *handlers[R, In]) formNames() []string {
	var names []string
	for _, st := range slices.Concat(h.Sections, h.wiz.steps) {
		names = append(names, st.Fields...) // a name twice changes no check
	}
	if h.Sections != nil || h.Wizard != nil {
		return names
	}
	for _, fd := range h.fields {
		names = append(names, fd.Name)
	}
	return names
}

// carded is the inputs of names on form f: in the Screen's Cards, each card
// drawn only if it holds one of them, or as they are without Cards. A card
// groups its More fields; a form of only More fields (a section the user chose
// to change) shows them plainly.
func (h *handlers[R, In]) carded(names []string, f form) []templ.Component {
	input := func(name string) templ.Component {
		fd, _ := h.field(name) // handlers checked every step's fields
		return h.input(fd, name, f)
	}
	var out []templ.Component
	if h.Cards == nil {
		for _, name := range names {
			out = append(out, input(name))
		}
		return out
	}
	allMore := !slices.ContainsFunc(names, func(name string) bool {
		fd, _ := h.field(name)
		return !fd.More
	})
	for _, c := range h.Cards {
		var inputs []templ.Component
		for _, name := range c.Fields {
			if slices.Contains(names, name) {
				inputs = append(inputs, input(name))
			}
		}
		switch {
		case len(inputs) == 0:
		case allMore:
			out = append(out, formCardInputs(c, inputs))
		default:
			out = append(out, formCardInputs(c, groupedCard(inputs)))
		}
	}
	return out
}

// rowsCard is the card that holds the rows, nil when none does.
func (h *handlers[R, In]) rowsCard() *FormCard {
	i := slices.IndexFunc(h.Cards, func(c FormCard) bool { return c.Rows })
	if i < 0 {
		return nil
	}
	return &h.Cards[i]
}

// inPageOrder is errs in the order the page shows their fields: a parse error
// found first never lists ahead of a field above it (shell M15). An error
// naming no form field comes after those that do: a row's (row fields are no
// form field), by row, after a header one's.
func (h *handlers[R, In]) inPageOrder(errs []doctype.FieldError) []doctype.FieldError {
	var order []string
	for _, c := range h.Cards {
		order = append(order, c.Fields...)
	}
	if h.Cards == nil {
		order = h.formNames()
	}
	rank := func(e doctype.FieldError) int {
		if i := slices.Index(order, e.Field); i >= 0 {
			return i
		}
		return len(order)
	}
	out := slices.Clone(errs)
	slices.SortStableFunc(out, func(a, b doctype.FieldError) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Line, b.Line))
	})
	return out
}
