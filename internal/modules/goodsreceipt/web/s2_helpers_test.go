package grnweb_test

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"

	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt/grntest"
	"github.com/vardhan/internaltools/internal/modules/purchase/purchasetest"
)

// MOD-GRN S2 helpers: step 3 Received quantities (UX doc §5.4) driven over
// HTTP on s1World (real engine, real auth.Service), as Suresh unless a
// case says otherwise (CD-213). Inputs are found the way a screen reader
// finds them, by their accessible names (UX §5.4 "Accessible name"), so the
// tests don't fix the Body's input names; the posted form is every named
// control of #wizard-form, as a browser sends it.

// The field labels each card's inputs are named after (UX §5.4).
const (
	s2Received = "Received now"
	s2Rejected = "Rejected"
	s2Reason   = "Reason for rejecting"
)

// Item names of the fixture items (00-fixtures.md).
const (
	s2Angle = "MS Angle 50x50x5"
	s2Wire  = "Binding Wire 18G"
	s2Bolt  = "Hex Bolt M12x60"
)

// s2Line is one PO line to raise: item code, UoM code, quantity.
type s2Line struct{ item, uom, qty string }

// s2PO is an approved PO by Priya on METRO-SITE from SHREE with lines.
func (w *s1World) s2PO(lines ...s2Line) doc.Doc {
	w.t.Helper()
	var ls []map[string]string
	for _, l := range lines {
		ls = append(ls, map[string]string{"item_id": w.id("item", l.item), "uom_id": w.id("uom", l.uom), "qty": l.qty})
	}
	return purchasetest.NewOrder(w.t, w.Env.Env, purchasetest.StopAtApproved(), purchasetest.OrderLines(ls...))
}

// s2RefPO is the reference PO (grn.md): line 1 MS-ANGLE-50 1,200 KGS,
// line 2 WIRE-18G 50 KGS.
func (w *s1World) s2RefPO() doc.Doc {
	w.t.Helper()
	return w.s2PO(s2Line{"MS-ANGLE-50", "KGS", "1200"}, s2Line{"WIRE-18G", "KGS", "50"})
}

// s2Approve approves a GRN by Suresh on po receiving qty on PO line n
// (1-based) of po, for each pair n, qty.
func (w *s1World) s2Approve(po doc.Doc, pairs map[int]string) doc.Doc {
	w.t.Helper()
	var lines []grntest.Line
	for n := 1; n <= len(po.Lines); n++ {
		if q, ok := pairs[n]; ok {
			lines = append(lines, grntest.Line{POLineID: po.Lines[n-1].ID, Received: q})
		}
	}
	return w.GRN(w.t, w.Suresh, po.ID, lines, doctype.StateApproved)
}

// s2Items is GET step 3 of GRN id as Suresh, which must answer 200.
func (w *s1World) s2Items(id int64) s1Page {
	w.t.Helper()
	r := w.get(w.Suresh, stepPath(id, "items"))
	if r.status != http.StatusOK {
		w.t.Fatalf("GET %s = %d, want 200 (%s)", stepPath(id, "items"), r.status, r.text())
	}
	return r
}

// s2Labels maps a field key to its visible label.
var s2Labels = map[string]string{"received": s2Received, "rejected": s2Rejected, "reason": s2Reason}

// s2Input is card n's input for field key (received, rejected, reason):
// the control whose aria-label starts "{Label}, line {n}, " (UX §5.4).
func (p s1Page) s2Input(key string, n int) *goquery.Selection {
	return p.doc.Find(`#wizard-form [aria-label^="` + s2Labels[key] + `, line ` + strconv.Itoa(n) + `, "]`)
}

// s2Value is the value of card n's input for key: an input's value
// attribute, a textarea's text.
func (p s1Page) s2Value(key string, n int) string {
	s := p.s2Input(key, n)
	if goquery.NodeName(s) == "textarea" {
		return s.Text()
	}
	return s.AttrOr("value", "")
}

// s2TitleRE matches a card title "{n} · {item name}".
var s2TitleRE = regexp.MustCompile(`^(\d+) · `)

// s2Cards are the step's line cards, in page order: each section.card whose
// h2 title is "{n} · {item name}".
func (p s1Page) s2Cards() []*goquery.Selection {
	var out []*goquery.Selection
	p.doc.Find("section.card").Each(func(_ int, s *goquery.Selection) {
		if s2TitleRE.MatchString(s2Norm(s.Find("h2.card-title").First().Text())) {
			out = append(out, s)
		}
	})
	return out
}

// s2Titles are the card titles in page order.
func (p s1Page) s2Titles() []string {
	var out []string
	for _, c := range p.s2Cards() {
		out = append(out, s2Norm(c.Find("h2.card-title").First().Text()))
	}
	return out
}

// s2Card is the card titled "{n} · …"; it fails the test unless there is
// exactly one.
func (p s1Page) s2Card(t *testing.T, n int) *goquery.Selection {
	t.Helper()
	var got []*goquery.Selection
	for _, c := range p.s2Cards() {
		if m := s2TitleRE.FindStringSubmatch(s2Norm(c.Find("h2.card-title").First().Text())); m[1] == strconv.Itoa(n) {
			got = append(got, c)
		}
	}
	if len(got) != 1 {
		t.Fatalf("%d cards titled %d · …, want 1; titles %q", len(got), n, p.s2Titles())
	}
	return got[0]
}

// s2Disclosure is the details.disclosure inside sel whose own summary
// reads summary.
func s2Disclosure(sel *goquery.Selection, summary string) *goquery.Selection {
	return sel.Find("details.disclosure").FilterFunction(func(_ int, d *goquery.Selection) bool {
		return s2Norm(d.ChildrenFiltered("summary").Text()) == summary
	})
}

// s2Open reports whether a details element is open.
func s2Open(d *goquery.Selection) bool {
	_, ok := d.Attr("open")
	return ok
}

// s2Norm collapses runs of white space.
func s2Norm(s string) string { return strings.Join(strings.Fields(s), " ") }

// s2Form is what a browser posts from #wizard-form with no button pressed:
// every named input, textarea and select, checkboxes and radios only when
// checked, never a button.
func (p s1Page) s2Form() url.Values {
	f := url.Values{}
	p.doc.Find("#wizard-form").Find("input, textarea, select").Each(func(_ int, s *goquery.Selection) {
		name, ok := s.Attr("name")
		if !ok || name == "" {
			return
		}
		switch goquery.NodeName(s) {
		case "textarea":
			f.Add(name, s.Text())
		case "select":
			f.Add(name, s.Find("option[selected]").AttrOr("value", ""))
		default:
			switch s.AttrOr("type", "text") {
			case "submit", "button", "image", "reset":
				return
			case "checkbox", "radio":
				if _, on := s.Attr("checked"); !on {
					return
				}
				f.Add(name, s.AttrOr("value", "on"))
			default:
				f.Add(name, s.AttrOr("value", ""))
			}
		}
	})
	return f
}

// s2Set is one value to type: card line's input for key.
type s2Set struct {
	key  string
	line int
	val  string
}

// s2Typed is p's form with sets typed into it; it fails the test when a
// card's input isn't there or has no name.
func (p s1Page) s2Typed(t *testing.T, sets ...s2Set) url.Values {
	t.Helper()
	f := p.s2Form()
	for _, s := range sets {
		in := p.s2Input(s.key, s.line)
		name := in.AttrOr("name", "")
		if in.Length() != 1 || name == "" {
			t.Fatalf("card %d has %d named %q inputs (name %q); titles %q", s.line, in.Length(), s2Labels[s.key], name, p.s2Titles())
		}
		f.Set(name, s.val)
	}
	return f
}

// s2Save types sets into page p of GRN id's step 3 and presses Save and
// continue (no nav value).
func (w *s1World) s2Save(id int64, p s1Page, sets ...s2Set) s1Page {
	w.t.Helper()
	return w.post(w.Suresh, stepPath(id, "items"), p.s2Typed(w.t, sets...))
}

// s2Button is the submit button in #wizard-form whose text is label.
func (p s1Page) s2Button(label string) *goquery.Selection {
	return p.doc.Find("#wizard-form button").FilterFunction(func(_ int, b *goquery.Selection) bool {
		return s2Norm(b.Text()) == label
	})
}

// s2Press types sets into page p and presses the Body button label: the
// form plus the button's name and value, posted to its formaction (resolved
// against the step URL), as a browser does without JavaScript.
func (w *s1World) s2Press(id int64, p s1Page, label string, sets ...s2Set) s1Page {
	w.t.Helper()
	b := p.s2Button(label)
	if b.Length() != 1 {
		w.t.Fatalf("%d %q buttons in the step form, want 1", b.Length(), label)
	}
	f := p.s2Typed(w.t, sets...)
	if name := b.AttrOr("name", ""); name != "" {
		f.Set(name, b.AttrOr("value", ""))
	}
	base, err := url.Parse(stepPath(id, "items"))
	if err != nil {
		w.t.Fatal(err)
	}
	action, err := url.Parse(b.AttrOr("formaction", ""))
	if err != nil {
		w.t.Fatalf("%q formaction: %v", label, err)
	}
	return w.post(w.Suresh, base.ResolveReference(action).String(), f)
}

// s2Links maps each error summary link's text to its href.
func (p s1Page) s2Links() map[string]string {
	out := map[string]string{}
	p.doc.Find(".error-summary a").Each(func(_ int, a *goquery.Selection) {
		out[s2Norm(a.Text())] = a.AttrOr("href", "")
	})
	return out
}

// s2WantLinkIn fails unless the summary links msg to an id that is exactly
// one element on the page, card n's input for key, with M0-26's per-line
// id f-lines-{k}-{field} (R1, CD-286), and msg shows under that input
// (#{id}-msg). It returns the id.
func s2WantLinkIn(t *testing.T, p s1Page, msg, key, field string, n int) string {
	t.Helper()
	href, ok := p.s2Links()[msg]
	if !ok {
		t.Errorf("the summary doesn't link %q; links %q", msg, p.s2Links())
		return ""
	}
	id := strings.TrimPrefix(href, "#")
	if !regexp.MustCompile(`^f-lines-\d+-` + field + `$`).MatchString(id) {
		t.Errorf("%q links to %q, want a per-line id f-lines-{k}-%s (M0-26)", msg, href, field)
	}
	if c := p.doc.Find(`[id="` + id + `"]`).Length(); c != 1 {
		t.Errorf("%d elements have id %q, want 1", c, id)
	}
	if in := p.s2Input(key, n); in.Length() != 1 || in.AttrOr("id", "") != id {
		t.Errorf("%q links to %q, want card %d's %s input (id %q)", msg, href, n, s2Labels[key], in.AttrOr("id", ""))
	}
	if got := s2Norm(p.doc.Find(`[id="` + id + `-msg"]`).Text()); !strings.Contains(got, msg) {
		t.Errorf("message under %s = %q, want %q", id, got, msg)
	}
	return id
}

// s2Lines are GRN id's stored lines as "po{n}={received}/{rejected}/{reason}#{id}"
// where n is the PO line's position in po.
func (w *s1World) s2Lines(id int64, po doc.Doc) []string {
	w.t.Helper()
	pos := map[string]int{}
	for i, l := range po.Lines {
		pos[strconv.FormatInt(l.ID, 10)] = i + 1
	}
	var out []string
	for _, l := range w.Load(w.t, goodsreceipt.DocType, id).Lines {
		reason, _ := l.Values["reject_reason"].(string)
		out = append(out, fmt.Sprintf("po%d=%s/%s/%s#%d", pos[fmt.Sprint(l.Values["po_line_id"])],
			s2Qty(l.Values["received_qty"]), s2Qty(l.Values["rejected_qty"]), reason, l.ID))
	}
	return out
}

// s2Qty is a stored quantity as plain text ("" for none).
func s2Qty(v any) string {
	if q, ok := v.(interface{ Plain() string }); ok {
		return q.Plain()
	}
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// s2LineIDs are GRN id's stored line ids by PO line position in po.
func (w *s1World) s2LineIDs(id int64, po doc.Doc) map[int]int64 {
	w.t.Helper()
	pos := map[string]int{}
	for i, l := range po.Lines {
		pos[strconv.FormatInt(l.ID, 10)] = i + 1
	}
	out := map[int]int64{}
	for _, l := range w.Load(w.t, goodsreceipt.DocType, id).Lines {
		out[pos[fmt.Sprint(l.Values["po_line_id"])]] = l.ID
	}
	return out
}

// s2Version is GRN id's stored version.
func (w *s1World) s2Version(id int64) int {
	w.t.Helper()
	return w.Load(w.t, goodsreceipt.DocType, id).Version
}

// s2Delivery saves step 2 of draft id with a delivery challan DC-1042, so
// a submit reaches the line rules.
func (w *s1World) s2Delivery(id int64) {
	w.t.Helper()
	r := w.post(w.Suresh, stepPath(id, "delivery"), url.Values{"version": {strconv.Itoa(w.s2Version(id))},
		"challan_kind": {"Delivery challan"}, "challan_no": {"DC-1042"}})
	if r.status != http.StatusSeeOther {
		w.t.Fatalf("save step 2 = %d %q", r.status, r.errorSummary())
	}
}

// s2WantNoHidden fails when page p renders a control named after a field
// Module.Hide hides (CD-256 (1), CD-319).
func s2WantNoHidden(t *testing.T, p s1Page, what string) {
	t.Helper()
	for _, name := range []string{"po_line_id", "site_id", "project_id"} {
		if n := p.doc.Find(`[name="` + name + `"]`).Length(); n != 0 {
			t.Errorf("%s: %d controls named %s, which Hide hides (CD-319)", what, n, name)
		}
	}
}

// s2WantUniqueIDs fails when two elements of page p share an id.
func s2WantUniqueIDs(t *testing.T, p s1Page, what string) {
	t.Helper()
	seen := map[string]int{}
	p.doc.Find("[id]").Each(func(_ int, s *goquery.Selection) { seen[s.AttrOr("id", "")]++ })
	for id, n := range seen {
		if n > 1 {
			t.Errorf("%s: %d elements have id %q", what, n, id)
		}
	}
}
