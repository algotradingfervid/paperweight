package types

// The largest values the app accepts (each also applies negated).
const (
	MaxMoney   Money   = 1_000_000_000_000     // ₹10,00,00,00,000.00
	MaxQty     Qty     = 10_000_000_000        // 1,00,00,000
	MaxRate    Rate    = 1_000_000_000_000     // ₹10,00,00,000.0000
	MaxPercent Percent = 100_000               // 100%
	MaxFactor  Factor  = 1_000_000_000_000_000 // 10,00,000
)
