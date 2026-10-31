package purchase

// Hand-over messages, copied from PO spec §21.2 and UX 5.12 (one condition
// each, CD-147). The note's texts are modkit's (CD-142), and the stale and
// 403/404 texts DOC's and AUTH's.
const (
	msgPickOwner          = "Pick who takes over this order."
	msgHandOverNotDraft   = "An approved, closed or cancelled order can't be handed over."
	msgHandOverPending    = "Withdraw this order before handing it over."
	msgHandOverSameOwner  = "This order is already %s's."
	msgHandOverCantCreate = "%s can't raise purchase orders. Pick someone with the Purchase User role."
	msgHandOverCantSee    = "%s can't see this order's project or site. Pick someone else."
	msgHandOverDisabled   = "%s can't sign in at the moment. Pick someone else."
)
