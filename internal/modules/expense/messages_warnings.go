package expense

// The claim warnings of spec §5, never refusals (MOD-EXP E6). {claim} is the
// other claim's number only when the viewer can read it, else anotherClaim
// (CD-264 item 4, CD-240, CD-248).
const (
	msgSameReceipt = "Line %d: This receipt is also on %s."
	msgSameLine    = "Line %d: This looks the same as a line on %s."
	msgSameInClaim = "Line %d: This looks the same as line %d."                                   // CD-271 G2
	msgSameInvoice = "Line %d: Invoice %s from %s is also on %s."                                 // CD-271 G3
	msgTimeBarred  = "Line %d: This bill is dated in FY %s. Input tax credit on it closed on %s." // CD-271 G8
	msgOwnGSTIN    = "Line %d: Supplier GSTIN %s is our own branch's GSTIN."                      // CD-337 (5)
	anotherClaim   = "another claim"
)
