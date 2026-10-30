package uom

import (
	"errors"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	factorScale       = 1_000_000_000 // types.Factor is 1e-9
	factorSixDPUnit   = 1_000         // 1e-6 in Factor units
	qtyUnit           = 1_000         // one whole unit in types.Qty
	msgFactorDecimals = "Conversion factor can have at most 6 decimal places."
	msgFactorNineDP   = "Conversion factor can have at most 9 decimal places." // types.ParseFactor's wording
)

// types.ParseFactor's wording.
const (
	msgFactorZero  = "Conversion factor must be more than 0."
	msgFactorLarge = "Conversion factor is too large. The maximum is 10,00,000."
)

// FactorOne is a conversion factor of 1.
const FactorOne types.Factor = factorScale

// CheckFactor checks a factor that was not parsed against ParseFactor's
// rules: more than 0, at most types.MaxFactor and at most 6 decimal places
// (MST-085). The messages are ParseFactor's.
func CheckFactor(f types.Factor) error {
	switch {
	case f <= 0:
		return &types.InputError{Msg: msgFactorZero}
	case f > types.MaxFactor:
		return &types.InputError{Msg: msgFactorLarge}
	case f%factorSixDPUnit != 0:
		return &types.InputError{Msg: msgFactorDecimals}
	}
	return nil
}

// ParseFactor reads a conversion factor: more than 0 and at most 6 decimal
// places (MST-085), stored at types.Factor's 1e-9 scale.
func ParseFactor(s string) (types.Factor, error) {
	f, err := types.ParseFactor(s)
	var ie *types.InputError
	if (errors.As(err, &ie) && ie.Msg == msgFactorNineDP) || (err == nil && f%factorSixDPUnit != 0) {
		return 0, &types.InputError{Msg: msgFactorDecimals}
	}
	return f, err
}

// ToStock converts q to the stock UoM: q × f / 1e9, rounded half-up to the
// quantity's precision with a 128-bit intermediate (types.MulDivRound, which
// multiplies the magnitudes with bits.Mul64 and restores the sign). It doesn't
// report overflow: use it on a quantity CheckLineQty has passed.
func ToStock(q types.Qty, f types.Factor) types.Qty {
	v, _ := types.MulDivRound(int64(q), int64(f), factorScale)
	return types.Qty(v)
}

// RateToStock converts a rate per line UoM to a rate per stock UoM:
// r × 1e9 / f, rounded half-up, with a 128-bit intermediate. f must be > 0;
// with f >= 1e-6 (ParseFactor) and |r| <= MaxRate it cannot overflow.
func RateToStock(r types.Rate, f types.Factor) types.Rate {
	v, _ := types.MulDivRound(int64(r), factorScale, int64(f))
	return types.Rate(v)
}

// IsWhole reports whether q has no fractional part.
func IsWhole(q types.Qty) bool { return q%qtyUnit == 0 }

// CheckLineQty checks a document line's quantity against the whole-number
// flags of its UoM (MST-086) and of the item's stock UoM after conversion by
// f (MST-087). The stock quantity is rounded to quantity precision first, so
// conversion dust passes (MST-088); a stock quantity past types.MaxQty is
// refused with NUM's message. It returns nil when the line is fine.
func CheckLineQty(line int, qty types.Qty, lineUoM, stockUoM UoM, f types.Factor) *doctype.FieldError {
	row := "Row " + strconv.Itoa(line) + ": "
	if lineUoM.WholeNumber && !IsWhole(qty) {
		return &doctype.FieldError{Field: "qty", Line: line,
			Message: row + "Quantity " + types.FormatQty(qty) + " can't be a fraction for UoM " + lineUoM.Code + ". Enter a whole number."}
	}
	stock, err := types.StockQty(qty, f)
	if err != nil {
		return &doctype.FieldError{Field: "qty", Line: line, Message: row + err.Error()}
	}
	if stockUoM.WholeNumber && !IsWhole(stock) {
		return &doctype.FieldError{Field: "qty", Line: line,
			Message: row + types.FormatQtyUoM(qty, lineUoM.Code) + " = " + types.FormatQtyUoM(stock, stockUoM.Code) +
				", but " + stockUoM.Code + " must be a whole number. Change the quantity."}
	}
	return nil
}
