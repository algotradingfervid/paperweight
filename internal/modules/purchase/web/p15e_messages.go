package purchaseweb

import "github.com/vardhan/internaltools/internal/ui/layout"

// The module pages' words (UX §5.1, §5.2, §5.12, §5.13, §5.19, §5.20,
// approved in CD-258).
const (
	// Success flashes after the hand-over, send and Expected on POSTs.
	msgHandedOver      = "%s is now %s's."
	msgMarkedSent      = "%s marked as sent."
	msgMarkedNotSent   = "%s marked as not sent."
	msgExpectedSaved   = "Expected on for %s saved."
	msgExpectedCleared = "Expected on for %s cleared."

	// Purchase home.
	homeTitle       = "Purchase"
	recentTitle     = "Recent comments"
	newOrderLabel   = "New purchase order"
	newReqLabel     = "New purchase request"
	waitingLabel    = "Waiting for you"
	waitingWhy      = layout.HomeWhyApproval
	metaChanges     = layout.HomeWhyChanges
	metaNotSent     = layout.HomeWhyNotSent
	seeAllLabel     = "See all %d"
	recentDocsLabel = "Your recent documents"
	allOrdersLabel  = "All purchase orders"

	// Hand over.
	msgNobodyEligible = "Nobody else can take over this order. Ask an Administrator to give someone the Purchase User role for its project and site."
	handOverTo        = "Hand over to"
	noteHintHandOver  = "Posted as a comment on the order."

	// Send.
	sendGetHeading    = "Get the order to the vendor"
	sendRecordHeading = "Record how it was sent"
	sendFileHint      = "Save the order as a PDF, then send it by email or WhatsApp."
	sendPrintLabel    = "Print or save as PDF"
	sendNoteHint      = "For example, who you sent it to or the courier's docket number."
	msgSentBanner     = "Sent to vendor on %s by %s (%s)"

	// noteMax is the Send and Expected on note's length (spec §23, §27.1).
	noteMax = 200

	// Expected on.
	expectedHint = "The date the vendor now gives. It replaces the required-by date in the overdue lists."

	// Pending items.
	pendingTitle   = "Pending items"
	msgPendingNone = "Nothing is pending on approved orders."
	msgPendingMiss = "No pending lines match these filters."
	overdueOnly    = "Overdue only"
)
