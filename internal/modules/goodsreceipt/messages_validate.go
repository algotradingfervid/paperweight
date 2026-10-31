package goodsreceipt

// G2's texts (GRN spec §4; plan G2; CD-264 (8): line texts start
// "Line {n}: "; the total is CD-308 as CD-311 words it).
const (
	msgChoosePO        = "Choose a purchase order."
	msgDeleteDraft     = "Delete this draft to receive against another order."
	msgLineNotOnPO     = "Line %d: This line isn't on %s."
	msgPOLineTwice     = "Line %d: This PO line is already on line %d."
	msgReceiptFuture   = "Receipt date can't be in the future."
	msgChallanAfter    = "Challan date can't be after the receipt date."
	msgRejectedAbove   = "Line %d: Rejected can't be more than received."
	msgTwiceOrdered    = "Line %d: Accepted %s is more than twice the %s ordered. Check the quantity."
	msgTotalTooLarge   = "The total is too large. The most a goods receipt can be is ₹10,00,00,00,000.00. Split it into more than one goods receipt."
	msgRejectNoReason  = "Line %d: Reason for rejecting is required."
	msgChallanRequired = "Challan or invoice no. is required for goods."
	msgChallanKind     = "Choose Delivery challan or Tax invoice."
)
