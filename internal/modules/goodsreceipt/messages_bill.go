package goodsreceipt

// G7's texts: GRN spec §3.3; CD-279 (3), (4), (11); DOC's msgStale with the
// label Bill details and DOC's msgMaxLen with the field's label (UX §5.7).
const (
	msgBillNotApproved  = "Add bill details after the receipt is approved."
	msgBillNoRequired   = "Vendor invoice no. is required."
	msgBillNoMaxLen     = "Vendor invoice no. can be at most 40 characters."
	msgBillDateNeeded   = "Invoice date is required."
	msgBillDateFuture   = "Invoice date can't be in the future."
	msgBillNegative     = "Invoice total can't be negative."
	msgBillStale        = "Someone else changed Bill details after you opened it. Reload it and make your changes again."
	msgBillDup          = "Invoice %s is also on %s."
	msgBillDupHidden    = "Invoice %s is also on another receipt."
	msgBillAccepted     = "Accepted value before tax is ₹%s."
	noteBillCopied      = "Copied from the tax invoice entered at the gate. Check and save."
	auditBillCopiedFrom = "Copied from %s"
	// The audit actions on the GRN; internal/app words them for History.
	auditBillSaved  = "bill_saved"
	auditBillCopied = "bill_copied"
	billNoMaxLen    = 40 // characters of the invoice no. (spec §3.3)
)

// G11's texts (CD-337 (6)), G7's duplicate invoice shape.
const (
	msgChallanDup       = "Challan %s is also on %s."
	msgChallanDupHidden = "Challan %s is also on another receipt."
)
