package grnweb

import (
	"context"
	"encoding/gob"
	"fmt"
	"net/http"
	"strconv"

	"github.com/a-h/templ"
	"github.com/alexedwards/scs/v2"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/goodsreceipt"
	"github.com/vardhan/internaltools/internal/types"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

// S3b: the Bill details card on the document page (UX doc §5.7), drawn by
// billCard (bill.templ) from goodsreceipt.BillCard, and from a post the
// Service refused: right away for htmx, or on the next load from the
// session for a post without JavaScript.

// S3b copy (UX doc §5.7 copy table).
const (
	labelInvoiceNo    = "Vendor invoice no."
	labelInvoiceDate  = "Invoice date"
	labelInvoiceTotal = "Invoice total"
	labelSaveBill     = "Save bill details"
	labelChange       = "Change"
	labelReload       = "Reload"
	billEmpty         = "No bill details yet."
	billHint          = "Add the invoice photo under Attachments first: adding a file reloads the page."
	billSaved         = "Bill details saved."
	billChanged       = "Last changed by %s on %s"
)

// billFields are the form's fields, in the error summary's order.
var billFields = []string{"invoice_no", "invoice_date", "invoice_amount"}

// billPost is a bill details post the Service refused (422 or 409): the
// typed values at the posted version, with the field errors or else the
// message.
type billPost struct {
	In      goodsreceipt.BillInput
	Version int
	Errs    map[string]string // field → message
	Msg     string
	Stale   bool
}

func init() { gob.Register(billPost{}) } // scs stores session values with gob

// billPostOf is the refused post of in at version, refused with he.
func billPostOf(in goodsreceipt.BillInput, version int, he doctype.HTTPError) billPost {
	k := billPost{In: in, Version: version, Errs: map[string]string{}, Stale: he.Status() == http.StatusConflict}
	for _, f := range he.FieldErrors() {
		k.Errs[f.Field] = f.Message
	}
	if len(k.Errs) == 0 {
		k.Msg = he.Error()
	}
	return k
}

// billKey is the session key of GRN id's refused post.
func billKey(id int64) string { return "grn-bill:" + strconv.FormatInt(id, 10) }

// takeBillPost returns and clears GRN id's refused post, or nil. ctx must
// be inside sm.LoadAndSave.
func takeBillPost(ctx context.Context, sm *scs.SessionManager, id int64) *billPost {
	k, ok := sm.Pop(ctx, billKey(id)).(billPost)
	if !ok {
		return nil
	}
	return &k
}

// billPanel is the Bill details card of c's GRN; nil draws nothing. ?bill=edit
// opens the form over stored bill details.
func (m *module) billPanel(c docscreen.Ctx) (templ.Component, error) {
	ctx, id := c.Req.Context(), c.View.Info.ID
	b, err := m.d.Service.BillCard(ctx, c.Principal, id)
	if err != nil {
		return nil, err
	}
	v, ok := billViewOf(id, b, takeBillPost(ctx, m.d.Sessions, id), c.Req.URL.Query().Get("bill") == "edit", false)
	if !ok {
		return nil, nil
	}
	return billCard(v), nil
}

// billView is the card as billCard draws it.
type billView struct {
	Change   *components.Link
	Msg      *components.BannerProps
	Errs     []components.FieldErr
	Saved    bool // the polite status after an htmx save
	Empty    bool
	Note     string // the tax invoice offer's note
	Rows     []components.Pair
	Lines    []string // who changed it last; Copied from
	Info     []string
	Warnings []string
	Form     *billForm
}

// billForm is the card's form.
type billForm struct {
	Action, Version string
	No, Amount      components.FieldProps
	Date            components.DateProps
	Save            components.ButtonProps
}

// billViewOf is GRN id's card from b, with kept (a refused post, or nil)
// over it; ok is false when there is no card (UX doc §5.7: a draft or
// pending receipt, or one without bill details that isn't approved). A
// writer on an approved receipt gets the form while there are no bill
// details, at ?bill=edit (edit) and over a refused post; everyone else the
// read view, or the empty text.
func billViewOf(id int64, b goodsreceipt.BillCard, kept *billPost, edit, saved bool) (billView, bool) {
	approved, stored := b.State == doctype.StateApproved, b.Bill.Version > 0
	if kept == nil && !approved && !stored { // stored bill details are on approved or cancelled receipts only
		return billView{}, false
	}
	v := billView{Warnings: b.Warnings, Saved: saved}
	if kept != nil {
		for _, f := range billFields {
			if msg, ok := kept.Errs[f]; ok {
				v.Errs = append(v.Errs, components.FieldErr{ID: components.FieldID(f), Message: msg})
			}
		}
		if kept.Msg != "" {
			v.Msg = &components.BannerProps{Kind: components.Danger, Message: kept.Msg}
			if kept.Stale {
				// UI v2 E6 (A-29): a stale page is a warning, and Reload names
				// what it reloads.
				v.Msg.Kind = components.Warning
				v.Msg.Link = &components.Link{Label: labelReload + " " + billTitle, Href: docPath(id) + "#bill"}
			}
		}
	}
	if approved && b.CanWrite && (!stored || edit || kept != nil) {
		v.Empty, v.Form, v.Note = !stored, billFormOf(id, b, kept), b.Bill.Note
		return v, true
	}
	if !stored {
		v.Empty = true
		return v, true
	}
	v.Rows = []components.Pair{{Label: labelInvoiceNo, Value: b.Bill.InvoiceNo}, {Label: labelInvoiceDate, Value: types.FormatDate(b.Bill.InvoiceDate)}}
	if b.Bill.InvoiceAmount.Valid {
		v.Rows = append(v.Rows, components.Pair{Label: labelInvoiceTotal, Value: types.FormatINR(b.Bill.InvoiceAmount.V), Num: true})
	}
	v.Lines = []string{fmt.Sprintf(billChanged, b.ChangedBy, types.FormatDate(types.BusinessDate(b.Bill.UpdatedAt)))}
	if b.CopiedFrom != "" {
		v.Lines = append(v.Lines, b.CopiedFrom)
	}
	v.Info = b.Info
	if approved && b.CanWrite {
		v.Change = &components.Link{Label: labelChange, Href: "?bill=edit#bill"}
	}
	return v, true
}

// billFormOf is the form over b's bill details (the stored row or the
// offer), or over kept's typed values at its posted version.
func billFormOf(id int64, b goodsreceipt.BillCard, kept *billPost) *billForm {
	in := goodsreceipt.BillInput{InvoiceNo: b.Bill.InvoiceNo}
	if b.Bill.InvoiceDate != (types.Date{}) {
		in.InvoiceDate = types.FormatDate(b.Bill.InvoiceDate)
	}
	if b.Bill.InvoiceAmount.Valid {
		in.InvoiceAmount = types.FormatMoney(b.Bill.InvoiceAmount.V)
	}
	version, errs := b.Bill.Version, map[string]string{}
	if kept != nil {
		in, version, errs = kept.In, kept.Version, kept.Errs
	}
	field := func(name, label, value string, required bool) components.FieldProps {
		return components.FieldProps{ID: components.FieldID(name), Name: name, Label: label, Value: value, Error: errs[name], Required: required}
	}
	no := field("invoice_no", labelInvoiceNo, in.InvoiceNo, true)
	no.Attrs = templ.Attributes{"maxlength": strconv.Itoa(goodsreceipt.BillNoMaxLen)}
	amount := field("invoice_amount", labelInvoiceTotal, in.InvoiceAmount, false)
	amount.Hint = b.Hint
	return &billForm{
		Action: billPath(id), Version: strconv.Itoa(version),
		No: no, Amount: amount,
		Date: components.DateProps{FieldProps: field("invoice_date", labelInvoiceDate, in.InvoiceDate, true),
			Today: types.FormatDate(b.Today), Yesterday: types.FormatDate(types.AddDays(b.Today, -1))},
		Save: components.ButtonProps{Label: labelSaveBill, Variant: components.Secondary},
	}
}

// docPath is GRN id's document page.
func docPath(id int64) string {
	return "/doc/" + goodsreceipt.DocType + "/" + strconv.FormatInt(id, 10)
}

// billPath is GRN id's bill route.
func billPath(id int64) string {
	return "/modules/goodsreceipt/receipts/" + strconv.FormatInt(id, 10) + "/bill"
}
