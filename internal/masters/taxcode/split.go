package taxcode

import "github.com/vardhan/internaltools/internal/types"

// allowedRates is India Compliance's GST_TAX_RATES (D-MST-16), in
// thousandths of a percent.
var allowedRates = []types.Percent{0, 100, 250, 1000, 1500, 3000, 5000, 6000, 7500, 12000, 18000, 28000, 40000}

// AllowedRates returns the notified GST rates in ascending order (MST-096).
func AllowedRates() []types.Percent {
	return append([]types.Percent(nil), allowedRates...)
}

// Split is the intra-state CGST and SGST rates (half each) and the
// inter-state IGST rate (the whole rate). It assumes a validated rate:
// notified, hence even in thousandths of a percent. Every allowed rate is even in
// thousandths of a percent, so the halves are exact (MST-098); rounding of
// amounts belongs to NUM.
func Split(rate types.Percent) (cgst, sgst, igst types.Percent) {
	return rate / 2, rate / 2, rate
}
