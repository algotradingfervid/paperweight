package purchase

// Create PO from a request messages, copied from PO spec §20.4 (one condition
// each, CD-147) and DOC's stale wording with the request's display (DOC
// exports no constructor).
const (
	msgFromRequestNoVendor = "Pick a vendor."
	// msgFromRequestNothing is "Everything on {PR} is already on an order."
	msgFromRequestNothing = "Everything on %s is already on an order."
	// msgFromRequestStale is "Someone else changed {display} after you opened
	// it. Reload it and make your changes again."
	msgFromRequestStale = "Someone else changed %s after you opened it. Reload it and make your changes again."
	// msgFromRequestDraft is "{PR} isn't submitted yet. Submit it before you
	// create a purchase order from it." (AD-13).
	msgFromRequestDraft = "%s isn't submitted yet. Submit it before you create a purchase order from it."
	// msgFromRequestCancelled is "{PR} is cancelled. You can't create a
	// purchase order from it." (AD-13).
	msgFromRequestCancelled = "%s is cancelled. You can't create a purchase order from it."
)
