package party

// Snapshot is what a document copies from its party when saved (D-MST-03).
// address.PartySnapshot fills it (MST Task 6), because the GSTIN and the
// address text live on the address.
type Snapshot struct {
	Code, Name, GSTIN, GSTCategory, StateCode, StateName, AddressText string
}
