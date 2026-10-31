package goodsreceipt

// G5's texts (GRN spec §4, §7; CD-272 Gaps 4 and 13; CD-264 (8): line
// texts start "Line {n}: ", n the PO line number, AD-12).
const (
	msgOverPending = "Line %d: Accepted %s is more than the %s pending on %s."
	msgBeforeOrder = "Receipt date is before the order date %s."
	noteAsOrdered  = "All lines received as ordered."
	noteUnreg      = "Unregistered vendor: the e-way bill may be ours to generate. Check with your CA."
	noteRejectEWay = "Reject the e-way bill on the portal within 72 hours if the goods went back."
	unregEWayAbove = 5000000 // ₹50,000.00 in paise (CD-272 Gap 13)
)
