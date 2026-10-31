package purchaseweb

// Add from requests words (UX §5.6, §5.7; PO spec §20.4 item 2).
const (
	fromLinkText   = "Add from requests (%d open)"
	fromHeading    = "Add from requests"
	fromIntro      = "Open request lines for %s · %s."
	fromEmpty      = "No open request lines for %s · %s."
	fromNeededBy   = "%s · Needed by %s"
	fromNoNeededBy = "%s · No needed-by date"
	fromPending    = "Pending %s %s"
	fromQtyLabel   = "Quantity to order"
	fromHeldOn     = "On %s (%s)"
	fromHeldOther  = "On another order"
	fromPrimary    = "Add selected"
	fromBack       = "Back to items"
	fromGoneLabel  = "No longer open"
	msgFromNoTick  = "Tick at least one line to add."
	// A ticked row no longer among the open rows: named only when the
	// viewer may read its request (CD-248 family).
	msgFromGone       = "Line %d: %s line %d is no longer open. Untick it or reload the page."
	msgFromGoneUnread = "Line %d: That request line is no longer open. Untick it or reload the page."
)
