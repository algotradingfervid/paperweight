package expenseweb

// The expense line page's and receipt upload's texts (UX §5.5, §7).
const (
	msgReceiptLabel  = "Receipt"
	msgReceiptHint   = "Photo or PDF of the bill, up to 10 MB."
	msgReceiptReused = "This file is already on this claim, as the receipt on line %d. It is used for this expense too."
	msgReceiptOnce   = "This file is already on this claim. It is used for this expense."
	// msgNoFile is files/web's text for a body with no file part, which it
	// doesn't export (companyweb copies it the same way).
	msgNoFile = "Choose a file to upload."

	msgAmountHint   = "Including any GST."
	msgNoteHint     = `Printed on the claim. For example "Pune to Mumbai, Volvo bus".`
	msgReasonHint   = `For example "Auto rickshaw, no bill given" or "Paper file".`
	msgTickOption   = "The bill shows our GSTIN"
	msgTickHint     = "Tick only when the supplier billed the company, not you. The accounts team decides whether GST can be claimed."
	msgBillDate     = "Use the bill date."
	msgBilledToHint = "The branch whose GSTIN is printed on the bill."
	msgGSTHint      = "Part of the amount above, not added to it."

	msgSameSummary = "Same receipt as another expense"
	msgSameOwn     = "No, a receipt of its own"
	msgSameAs      = "Same receipt as line %d"
	msgNoReceipt   = "No receipt"
	msgGSTSummary  = "Bill in company GSTIN"
	msgProjSummary = "Project or site"
)
