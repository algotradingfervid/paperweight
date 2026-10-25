package paymentterms

import (
	"errors"
	"strconv"
	"strings"

	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/types"
)

// hundredPercent is 100% in thousandths of a percent.
const hundredPercent types.Percent = 100000

// Row is one payment portion due Days after the document date.
type Row struct {
	Portion types.Percent
	Days    int
}

// RawRow is a row as typed on the form.
type RawRow struct {
	Portion, Days string
}

// Due is one schedule entry.
type Due struct {
	Date   types.Date
	Amount types.Money
}

func rowPrefix(i int) string { return "Row " + strconv.Itoa(i+1) + ": " }

// ParseRows parses typed rows; it returns nil rows with every row error.
func ParseRows(raw []RawRow) ([]Row, error) {
	var es mstkit.Errors
	rows := make([]Row, 0, len(raw))
	for i, r := range raw {
		portion, err := types.ParsePercent(r.Portion)
		switch {
		case errors.Is(err, types.ErrEmpty):
			es.AddLine(i+1, "portion", rowPrefix(i)+"Portion is required.")
		case err != nil:
			es.AddLine(i+1, "portion", rowPrefix(i)+err.Error())
		}
		days, err := strconv.Atoi(strings.TrimSpace(r.Days))
		if err != nil || days < 0 {
			es.AddLine(i+1, "days", rowPrefix(i)+"Days must be a whole number, 0 or more.")
		}
		rows = append(rows, Row{Portion: portion, Days: days})
	}
	if err := es.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// ValidateRows checks each row (portion above 0, days 0 or more, no repeated
// days) and that the portions total 100% (MST-124…MST-127).
func ValidateRows(rows []Row) error {
	var es mstkit.Errors
	var sum types.Percent
	seen := map[int]int{} // days → row index
	for i, r := range rows {
		if r.Portion <= 0 {
			es.AddLine(i+1, "portion", rowPrefix(i)+"Portion must be more than 0%.")
		}
		if r.Days < 0 {
			es.AddLine(i+1, "days", rowPrefix(i)+"Days must be a whole number, 0 or more.")
		}
		if j, ok := seen[r.Days]; ok {
			es.AddLine(i+1, "days", "Row "+strconv.Itoa(i+1)+" repeats row "+strconv.Itoa(j+1)+
				" ("+strconv.Itoa(r.Days)+" days). Merge them into one row of "+
				types.FormatPercent(rows[j].Portion+r.Portion)+".")
		} else {
			seen[r.Days] = i
		}
		sum += r.Portion
	}
	if sum != hundredPercent {
		es.Add("", "Payment portions add up to "+types.FormatPercent(sum)+". They must total 100%.")
	}
	return es.Err()
}

// Schedule splits total over rows: each row is due Days after docDate and
// gets total × portion rounded half-up to the paisa; the last row takes what
// is left, so the amounts sum exactly to total.
func Schedule(rows []Row, docDate types.Date, total types.Money) []Due {
	out := make([]Due, len(rows))
	var sum types.Money
	for i, r := range rows {
		amount := total - sum
		if i < len(rows)-1 {
			// total ≤ MaxMoney and portion ≤ 100%, so the product can't overflow.
			a, _ := types.MulDivRound(int64(total), int64(r.Portion), int64(hundredPercent))
			amount = types.Money(a)
		}
		sum += amount
		out[i] = Due{Date: types.AddDays(docDate, r.Days), Amount: amount}
	}
	return out
}
