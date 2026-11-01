package print

import (
	"strings"
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// Party is the vendor, customer or employee block, as saved on the document
// (FILE-063). An empty GSTIN prints "GSTIN: Unregistered"; an empty Name
// prints no block at all (CD-316).
type Party struct {
	Label, Name string
	Address     []string
	GSTIN       string
}

// PartyAddress are the vendor address snapshot's lines without its last
// line when that is the vendor's GSTIN line (address.DisplayText ends with
// it), which the party block prints on its own.
func PartyAddress(text, gstin string) []string {
	if text == "" {
		return nil // no address snapshot, so no address lines
	}
	out := strings.Split(text, "\n")
	if n := len(out); out[n-1] == "GSTIN: "+gstin {
		out = out[:n-1]
	}
	return out
}

// TradeLine is one row of the lines table.
type TradeLine struct {
	Sr                     int
	Item, Description, HSN string
	Qty                    types.Qty
	UoM                    string
	Rate                   types.Rate
	Amount                 types.Money
}

// TaxRow is one tax line under the taxable value (CGST, SGST or IGST).
type TaxRow struct {
	Name   string
	Rate   types.Percent
	Amount types.Money
}

// DocRef is one label and pre-formatted value printed near the date, such as
// "Payment terms: Net 30".
type DocRef struct{ Label, Value string }

// TradeDoc is the model a module print view fills for TradeDocument.
type TradeDoc struct {
	Title                    string
	Letterhead               LetterheadInfo
	Date                     types.Date
	Party                    Party
	Lines                    []TradeLine
	Taxable                  types.Money
	Taxes                    []TaxRow
	GrandTotal, RoundedTotal types.Money
	HasRoundedTotal          bool
	Terms                    string
	ApprovedBy               string
	ApprovedAt               time.Time
	AmendsNumber             string
	// DeliverTo, Refs, PlaceReason and ReverseChargeNote are each left out
	// when empty (FILE-064).
	DeliverTo         []string
	Refs              []DocRef
	PlaceReason       string
	ReverseChargeNote string
}

// PayableTotal is the rounded total when the DocType rounds, else the grand total.
func (d TradeDoc) PayableTotal() types.Money {
	if d.HasRoundedTotal {
		return d.RoundedTotal
	}
	return d.GrandTotal
}

// AmountInWords is the payable total in Indian words (FILE-082).
func (d TradeDoc) AmountInWords() (string, error) {
	return InWordsINR(d.PayableTotal())
}

// showRounded reports whether the Rounded Total row adds anything.
func (d TradeDoc) showRounded() bool {
	return d.HasRoundedTotal && d.RoundedTotal != d.GrandTotal
}

// words is the amount-in-words line, or "" when it can't be computed: the
// print shows the figures and leaves the words out rather than fail.
func (d TradeDoc) words() string {
	w, err := d.AmountInWords()
	if err != nil {
		return ""
	}
	return w
}

// GRNDoc is the model a goods receipt print view fills for GRNDocument (FILE-067).
type GRNDoc struct {
	Letterhead  LetterheadInfo
	Date        types.Date
	Party       Party
	PONumber    string
	PODate      types.Date
	ChallanNo   string
	ChallanDate types.Date
	Lines       []GRNLine
	// ChallanLabel is "Delivery challan no." or "Tax invoice no." from
	// challan_kind; blank prints "Challan or invoice no." (CD-272 Gap 5). A
	// blank ChallanNo prints a dash and a zero ChallanDate omits its line.
	// ChallanDateLabel is "Challan date" or "Invoice date" from the same
	// kind; blank prints "Challan Date" (CD-285).
	// The fields below are each left out when empty (GRN spec §7, §9).
	ChallanLabel      string
	ChallanDateLabel  string
	VehicleNo         string
	Transporter       string
	EWayBillNo        string
	Remarks           string
	ReverseChargeNote string
}

// GRNLine is one received item.
type GRNLine struct {
	Sr                           int
	Item, HSN, UoM               string
	Received, Accepted, Rejected types.Qty
	// RejectReason prints under the item when set.
	RejectReason string
}

// ExpenseDoc is the model an expense claim print view fills for
// ExpenseDocument (FILE-068). Attachments are the attached file names.
type ExpenseDoc struct {
	Letterhead               LetterheadInfo
	Employee                 string
	PeriodFrom, PeriodTo     types.Date
	Lines                    []ExpenseLine
	GrandTotal, RoundedTotal types.Money
	HasRoundedTotal          bool
	Attachments              []string
	ApprovedBy               string
	ApprovedAt               time.Time
	// Purpose, LessAdvance, AdvanceNumber and NetPayable are each left out
	// when empty; NetPayable prints when HasNetPayable is set, and the words
	// then follow it (Expense spec §10).
	Purpose       string
	LessAdvance   types.Money
	AdvanceNumber string
	NetPayable    types.Money
	HasNetPayable bool
}

// ExpenseLine is one expense with its receipt marker.
type ExpenseLine struct {
	Sr                int
	Date              types.Date
	Type, Description string
	Amount            types.Money
	HasReceipt        bool
	Project           string // left out when empty
}

// payableTotal is the rounded total when the claim rounds, else the grand total.
func (d ExpenseDoc) payableTotal() types.Money {
	if d.HasRoundedTotal {
		return d.RoundedTotal
	}
	return d.GrandTotal
}

// words is the net payable in words when set, else the payable total, or ""
// when it can't be computed.
func (d ExpenseDoc) words() string {
	m := d.payableTotal()
	if d.HasNetPayable {
		m = d.NetPayable
	}
	w, err := InWordsINR(m)
	if err != nil {
		return ""
	}
	return w
}
