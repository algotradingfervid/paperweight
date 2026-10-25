package types

// NetRate returns price less discount, rounded half-up to the rate's scale:
// price × (100% − discount) / 100%.
func NetRate(price Rate, discount Percent) (Rate, error) {
	if discount < 0 || discount > MaxPercent {
		return 0, &InputError{Msg: "Percentage must be between 0 and 100."}
	}
	// |price| <= MaxRate keeps the product far inside 128 bits and the result inside int64.
	r, _ := MulDivRound(int64(price), int64(MaxPercent-discount), int64(MaxPercent))
	return Rate(r), nil
}
