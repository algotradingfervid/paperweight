package print

import (
	"time"

	"github.com/vardhan/internaltools/internal/types"
)

// Party is the vendor, customer or employee block, as saved on the document
// (FILE-063). An empty GSTIN prints "GSTIN: Unregistered".
type Party struct {
	Label, Name string
	Address     []string
	GSTIN       string
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
}

// GRNLine is one received item.
type GRNLine struct {
	Sr                           int
	Item, HSN, UoM               string
	Received, Accepted, Rejected types.Qty
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
}

// ExpenseLine is one expense with its receipt marker.
type ExpenseLine struct {
	Sr                int
	Date              types.Date
	Type, Description string
	Amount            types.Money
	HasReceipt        bool
}

// payableTotal is the rounded total when the claim rounds, else the grand total.
func (d ExpenseDoc) payableTotal() types.Money {
	if d.HasRoundedTotal {
		return d.RoundedTotal
	}
	return d.GrandTotal
}

// words is the payable total in words, or "" when it can't be computed.
func (d ExpenseDoc) words() string {
	w, err := InWordsINR(d.payableTotal())
	if err != nil {
		return ""
	}
	return w
}
