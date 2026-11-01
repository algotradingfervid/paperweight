package expenseweb

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

const (
	// receiptUploadID is the line page's FileUpload (UX §5.5).
	receiptUploadID = "receipt"
	// saveLine is the sticky bar's Save line, which posts #line-form (UX-E4).
	saveLine = `button[form="line-form"]`
	// maxTypeRadios is the most expense types shown as radios; more is a
	// Select (design-system §7).
	maxTypeRadios = 6
)

// receiptHashName is the name the line form posts the receipt hash under:
// no field of expense.Claim has it, as the module hides receipt_sha256 and a
// field the module hides is never an input on any page (CD-319).
const receiptHashName = "receipt_hash"

// receiptHashID is the hidden receipt hash input's id.
var receiptHashID = components.FieldID("receipt_sha256")

// lineState is one draw of the line page: line no (0 = new) of c's claim,
// redrawn from the post when s.Posted.
type lineState struct {
	c      docscreen.Ctx
	no     int
	s      docscreen.StepState
	stored map[string]string // the stored line's inputs; empty for a new line
}

func newLineState(c docscreen.Ctx, no int, s docscreen.StepState) lineState {
	l := lineState{c: c, no: no, s: s, stored: map[string]string{}}
	if no > 0 {
		l.stored = c.View.Lines[no-1].Inputs
	}
	return l
}

// value is name's value: typed on a redraw, else stored.
func (l lineState) value(name string) string {
	if l.s.Posted {
		return l.c.Req.PostFormValue(name)
	}
	return l.stored[name]
}

// errorOf is the message of name's first error.
func (l lineState) errorOf(name string) string {
	for _, e := range l.s.Errors {
		if e.Field == name {
			return e.Message
		}
	}
	return ""
}

// hash is the line's receipt hash: posted on a redraw, else stored.
func (l lineState) hash() string {
	if l.s.Posted {
		return l.c.Req.PostFormValue(receiptHashName)
	}
	return l.stored["receipt_sha256"]
}

// filled reports whether one of names has a value or an error.
func (l lineState) filled(names ...string) bool {
	for _, n := range names {
		if l.value(n) != "" || l.errorOf(n) != "" {
			return true
		}
	}
	return false
}

// field is the claim's field name with hint.
func (l lineState) field(name, hint string) doctype.Field {
	f, _ := l.c.DT.Field(name)
	f.Hint = hint
	return f
}

// hidden reports whether the module hides field name (Module.Hide, the
// same function docscreen uses), so the page neither draws nor posts it and
// Save line never blanks it (CD-319).
func (l lineState) hidden(name string) bool {
	hide := l.c.Deps.Modules[l.c.DT.Name].Hide
	f, _ := l.c.DT.Field(name)
	return hide != nil && hide(l.c, f)
}

// part is c, or nothing when the module hides field name.
func (l lineState) part(name string, c templ.Component) templ.Component {
	if l.hidden(name) {
		return templ.NopComponent
	}
	return c
}

// props are FieldProps for field name, as FieldInput builds them.
func (l lineState) props(name, hint string) components.FieldProps {
	f := l.field(name, hint)
	return components.FieldProps{
		ID: components.FieldID(name), Name: name, Label: f.Label, Hint: hint,
		Error: l.errorOf(name), Value: l.value(name), Clearable: !f.Required,
	}
}

// submitProps are props with no "(optional)": a ticked line needs the field
// at submit (AD ruling, S2 review).
func (l lineState) submitProps(name, hint string) components.FieldProps {
	p := l.props(name, hint)
	p.Clearable = false
	return p
}

// input is FieldInput for field name with hint, unless the module hides it.
func (l lineState) input(name, hint string) templ.Component {
	return l.part(name, docscreen.FieldInput(l.field(name, hint), components.FieldID(name), l.value(name), l.errorOf(name), l.c.Deps))
}

// lineTop is the receipt area: FileUpload posting to the receipt route at the
// page's version (the posted one on a redraw, so a stale page stays stale)
// and with the line's number, disabling Save line while it uploads (UX-E5),
// and #receipt-files with the line's receipt, or the posted one after a 422
// or 409.
func lineTop(d Deps, c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	l := newLineState(c, no, s)
	version := c.View.Version
	if v, err := strconv.Atoi(c.Req.PostFormValue("version")); s.Posted && err == nil {
		version = v
	}
	action := "/modules/expense/claims/" + strconv.FormatInt(c.View.Info.ID, 10) + "/receipt?version=" + strconv.Itoa(version)
	if no > 0 {
		action += "&line=" + strconv.Itoa(no)
	}
	props := components.UploadProps{
		ID: receiptUploadID, Label: msgReceiptLabel, Hint: msgReceiptHint, Action: action,
		Accept:      "image/*,application/pdf",
		Target:      "#receipt-files",
		AlsoDisable: saveLine,
		Error:       l.errorOf("receipt_sha256"),
	}
	var items []components.Attachment
	if hash := l.hash(); hash != "" {
		metas, err := d.Files.List(c.Req.Context(), c.Principal, c.View.Record)
		if err != nil {
			return nil, err
		}
		for _, m := range metas {
			if m.SHA256 == hash { // only this line's receipt
				items = append(items, receiptAttachment(m))
			}
		}
	}
	return templ.Join(components.FileUpload(props), receiptFiles(items, "")), nil
}

// receiptAttachment is m as the receipt area shows it: S1's attachmentOf
// as the compact item (CD-326 (6)).
func receiptAttachment(m files.Meta) components.Attachment {
	a := attachmentOf(m)
	a.Compact = true
	return a
}

// lineBody is the line form in UX §5.5's order: the hidden receipt hash,
// Type, Amount, Date (today on a new line), Note, then the closed choices.
// A field the module hides isn't drawn.
func lineBody(d Deps, c docscreen.Ctx, no int, s docscreen.StepState) (templ.Component, error) {
	choices, err := d.Service.LineForm(c.Req.Context())
	if err != nil {
		return nil, err
	}
	l := newLineState(c, no, s)
	date := l.value("expense_date")
	if no == 0 && !s.Posted && c.Deps.Today != nil {
		date, _ = c.Deps.Today()
	}
	parts := []templ.Component{
		receiptHash(l.hash(), false),
		l.part("expense_type_id", typeInput(l, choices.Types)),
		l.input("amount", msgAmountHint),
		l.part("expense_date", docscreen.FieldInput(l.field("expense_date", ""), components.FieldID("expense_date"), date, l.errorOf("expense_date"), c.Deps)),
		more(c, "description", l.value("description"), l.errorOf("description"), l.input("description", msgNoteHint)),
	}
	if same := sameReceipt(l, choices.Types); same != nil {
		parts = append(parts, same)
	}
	parts = append(parts,
		disclosure(msgNoReceipt, l.filled("no_receipt_reason"), []templ.Component{
			l.part("no_receipt_reason", components.Textarea(l.props("no_receipt_reason", msgReasonHint), 200)),
		}),
		gstDisclosure(l, choices),
		disclosure(msgProjSummary, l.filled("project_id", "site_id"), []templ.Component{
			l.input("project_id", ""), l.input("site_id", ""),
		}),
	)
	return templ.Join(parts...), nil
}

// typeInput is Type: the enabled expense types by name with no default
// (CD-270 Q7), radios up to 6 and a Select above.
func typeInput(l lineState, all []expense.LineType) templ.Component {
	var opts []components.Option
	for _, t := range all {
		if !t.Disabled {
			opts = append(opts, components.Option{Value: strconv.FormatInt(t.ID, 10), Label: t.Name})
		}
	}
	p := l.props("expense_type_id", "")
	if len(opts) <= maxTypeRadios {
		return components.Radios(p, opts)
	}
	return components.Select(p, append([]components.Option{{}}, opts...))
}

// gstDisclosure is Bill in company GSTIN: the tick, the bill-date sentence,
// Billed to (only with more than one branch GSTIN, CD-271 G4), then the
// supplier's details. It is open when ticked, when Billed to, Supplier
// GSTIN, Invoice no. or GST has a value, or when any of its fields has an
// error; tax code and HSN/SAC values don't open it, as Validate fills them
// from the type on every line (AD ruling, S2 review).
func gstDisclosure(l lineState, choices expense.LineForm) templ.Component {
	var ticked []string
	if l.value("company_bill") == "true" { // DOC's plain Check value
		ticked = []string{"true"}
	}
	parts := []templ.Component{
		l.part("company_bill", components.Checkboxes(l.submitProps("company_bill", msgTickHint), []components.Option{{Value: "true", Label: msgTickOption}}, ticked)),
		billDate(),
	}
	if choices.BranchGSTINs > 1 {
		opts := make([]components.Option, 0, len(choices.Branches))
		for _, b := range choices.Branches {
			opts = append(opts, components.Option{Value: strconv.FormatInt(b.ID, 10), Label: b.Code + " · " + b.Name, Hint: "GSTIN " + b.GSTIN})
		}
		parts = append(parts, l.part("billed_to_id", components.Radios(l.submitProps("billed_to_id", msgBilledToHint), opts)))
	}
	// The three text boxes show their field's example (CD-370); Billed to
	// is radios, which take none.
	invoice := l.submitProps("invoice_no", "")
	invoice.Attrs = templ.Attributes{"maxlength": "40"}
	invoice.Placeholder = l.field("invoice_no", "").Placeholder
	gstin := l.submitProps("supplier_gstin", "")
	gstin.Placeholder = l.field("supplier_gstin", "").Placeholder
	hsn := l.props("hsn_sac", "")
	hsn.Placeholder = l.field("hsn_sac", "").Placeholder
	parts = append(parts,
		l.part("supplier_gstin", components.CodeInput(gstin, components.GSTIN)),
		l.part("invoice_no", components.TextInput(invoice)),
		l.input("tax_code_id", ""),
		l.part("hsn_sac", components.CodeInput(hsn, components.HSN)),
		l.input("gst_amount", msgGSTHint),
	)
	open := len(ticked) > 0 || l.filled("billed_to_id", "supplier_gstin", "invoice_no", "gst_amount")
	for _, n := range []string{"company_bill", "tax_code_id", "hsn_sac"} {
		open = open || l.errorOf(n) != ""
	}
	return disclosure(msgGSTSummary, open, parts)
}

// sameReceipt is Same receipt as another expense (spec §3.3, EUX-005): one
// option per distinct receipt hash on the other lines, labelled with the
// lowest line holding it, after the line's own receipt; nil when no other
// line has a receipt. A line holding a receipt that a lower line holds
// shares it, so that lower line's option is chosen and the disclosure open;
// a line that owns its receipt (no lower line holds it) opens with its own
// chosen and isn't offered the later lines sharing it, so a new photo
// uploaded there is what Save line stores.
func sameReceipt(l lineState, all []expense.LineType) templ.Component {
	names := map[string]string{}
	for _, t := range all {
		names[strconv.FormatInt(t.ID, 10)] = t.Name
	}
	own := l.stored["receipt_sha256"]
	picked := ""
	opts := []components.Option{{Label: msgSameOwn}}
	seen := map[string]bool{}
	for i, lv := range l.c.View.Lines {
		hash := lv.Inputs["receipt_sha256"]
		if i+1 == l.no || hash == "" || seen[hash] {
			continue
		}
		seen[hash] = true
		if hash == own {
			if i+1 > l.no {
				continue // a later line shares this line's own receipt
			}
			picked = strconv.Itoa(i + 1)
		}
		var hint []string
		for _, s := range []string{names[lv.Inputs["expense_type_id"]], lv.Display["expense_date"], lv.Display["amount"]} {
			if s != "" {
				hint = append(hint, s)
			}
		}
		opts = append(opts, components.Option{Value: strconv.Itoa(i + 1), Label: fmt.Sprintf(msgSameAs, i+1), Hint: strings.Join(hint, " · ")})
	}
	if len(opts) == 1 {
		return nil
	}
	if l.s.Posted {
		picked = l.c.Req.PostFormValue("same_as")
	}
	// The disclosure's summary names the group; no legend repeats it.
	p := components.FieldProps{ID: components.FieldID("same_as"), Name: "same_as", Value: picked}
	return disclosure(msgSameSummary, picked != "", []templ.Component{components.Radios(p, opts)})
}

// lineSave is this line's posted values in DOC's plain form, with NUM's
// parse errors, leaving out the fields the module hides so they keep their
// stored values (CD-319). receipt_sha256 is this page's own field, always
// posted as receiptHashName: the module hides it only from the generic
// pages (UX §5 intro, AD ruling S2 review). A picked same_as line holding a
// receipt gives that line's hash in place of the posted one; any other
// same_as is ignored.
func lineSave(c docscreen.Ctx, no int) (docscreen.Save, error) {
	l := lineState{c: c, no: no}
	out := docscreen.Save{Values: map[string]string{}}
	for _, f := range c.DT.Fields {
		posted := f.Name
		switch {
		case f.Name == "receipt_sha256":
			posted = receiptHashName
		case !f.OnLine || l.hidden(f.Name):
			continue
		}
		v, fe := docscreen.ToPlain(f, c.Req.PostFormValue(posted))
		if fe != nil {
			out.Errors = append(out.Errors, *fe)
			continue
		}
		out.Values[f.Name] = v
	}
	if same, err := strconv.Atoi(c.Req.PostFormValue("same_as")); err == nil && same >= 1 && same <= len(c.View.Lines) && same != no {
		if hash := c.View.Lines[same-1].Inputs["receipt_sha256"]; hash != "" {
			out.Values["receipt_sha256"] = hash
		}
	}
	return out, nil
}
