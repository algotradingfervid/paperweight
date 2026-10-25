package printtest

import (
	"github.com/vardhan/internaltools/internal/kernel/print"
	"github.com/vardhan/internaltools/internal/types"
)

var refDate = types.Date{Year: 2026, Month: 10, Day: 24}

func shree() print.Party {
	return print.Party{Label: "Vendor", Name: "Shree Steel Traders", Address: []string{"Mumbai, Maharashtra"}, GSTIN: "27AABCS1429B1ZU"}
}

// ReferencePO is the fixtures' reference PO (00-fixtures.md) on PUNE's letterhead.
func ReferencePO() print.TradeDoc {
	return print.TradeDoc{
		Title:      "Purchase Order",
		Letterhead: Letterhead[PuneID],
		Date:       refDate,
		Party:      shree(),
		Lines: []print.TradeLine{
			{Sr: 1, Item: "MS-ANGLE-50 MS Angle 50x50x5", HSN: "7216", Qty: 1_200_000, UoM: "kg", Rate: 625_000, Amount: 7_500_000},
			{Sr: 2, Item: "WIRE-18G Binding Wire 18G", HSN: "7217", Qty: 50_000, UoM: "kg", Rate: 960_000, Amount: 480_000},
		},
		Taxable: 7_980_000,
		Taxes: []print.TaxRow{
			{Name: "CGST", Rate: 9000, Amount: 718_200},
			{Name: "SGST", Rate: 9000, Amount: 718_200},
		},
		GrandTotal:      9_416_400,
		RoundedTotal:    9_416_400,
		HasRoundedTotal: true,
		Terms:           "Delivery within 7 days of PO date.",
	}
}

// ReferenceReceipt is a goods receipt for the reference PO's two lines,
// received in full, with no taxes.
func ReferenceReceipt() print.TradeDoc {
	po := ReferencePO()
	return print.TradeDoc{
		Title:      "Goods Receipt",
		Letterhead: po.Letterhead,
		Date:       refDate,
		Party:      shree(),
		Lines:      po.Lines,
		Taxable:    po.Taxable,
		GrandTotal: po.Taxable,
	}
}

// ReferenceClaim is an expense claim with a TRAVEL and a FOOD line, no taxes,
// rounded to the rupee.
func ReferenceClaim() print.TradeDoc {
	return print.TradeDoc{
		Title:      "Expense Claim",
		Letterhead: Letterhead[PuneID],
		Date:       refDate,
		Party:      print.Party{Label: "Employee", Name: "Deepak Nair"},
		Lines: []print.TradeLine{
			{Sr: 1, Item: "TRAVEL", Qty: 1_000, Rate: 12_500_000, Amount: 125_000},
			{Sr: 2, Item: "FOOD", Qty: 1_000, Rate: 4_805_000, Amount: 48_050},
		},
		Taxable:         173_050,
		GrandTotal:      173_050,
		RoundedTotal:    173_100,
		HasRoundedTotal: true,
	}
}
