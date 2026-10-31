package expense

// E2's user messages, copied from Expense spec §5 and CD-281. Line texts carry
// their full `Line {n}: ` prefix (CD-264 (8)).
const (
	msgNoEmployee        = "Your user isn't linked to an employee. Ask your administrator."
	msgOnlyOwnClaims     = "You can only raise claims for yourself."
	msgOnlyOwnAdvances   = "You can only raise advances for yourself."
	msgClaimDateFuture   = "Claim date can't be in the future."
	msgAdvanceDateFuture = "Advance date can't be in the future."
	msgLineDateFuture    = "Line %d: Date can't be in the future."
	msgLineAfterClaim    = "Line %d: Date can't be after the claim date."
	msgGSTAboveAmount    = "Line %d: GST can't be more than the amount."
	msgGSTINRequired     = "Line %d: Supplier GSTIN is required for a bill in the company GSTIN."
	msgInvoiceRequired   = "Line %d: Invoice no. is required for a bill in the company GSTIN."
	msgBilledToRequired  = "Line %d: Billed to is required for a bill in the company GSTIN."
	msgBranchNoGSTIN     = "Line %d: Choose a branch with a GSTIN."
	// CD-308: a header total over types.MaxMoney, on the lines as a whole.
	msgClaimTotalTooLarge   = "The total is too large. The most an expense claim can be is ₹10,00,00,00,000.00. Split it into more than one expense claim."
	msgAdvanceTotalTooLarge = "The total is too large. The most an employee advance can be is ₹10,00,00,00,000.00. Split it into more than one employee advance."
	// msgLinePrefix goes before gstin.ValidateGSTIN's MST-031 text (CD-38).
	msgLinePrefix = "Line %d: "
)
