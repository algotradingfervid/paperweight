package types

// CheckUoM rejects a fractional quantity for a whole-number unit.
func (q Qty) CheckUoM(uom string, wholeNumber bool) error {
	if wholeNumber && q%1000 != 0 {
		return &InputError{Msg: "Quantity for " + uom + " must be a whole number (you entered " + FormatQty(q) + ")."}
	}
	return nil
}

// StockQty converts q to the stock unit: q × f / 1e9, half-up.
func StockQty(q Qty, f Factor) (Qty, error) {
	v, ok := MulDivRound(int64(q), int64(f), 1_000_000_000)
	if !ok || v > int64(MaxQty) || v < -int64(MaxQty) {
		return 0, &InputError{Msg: qtyLarge}
	}
	return Qty(v), nil
}
