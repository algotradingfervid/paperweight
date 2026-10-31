package purchase

// P5's messages (request Validate), copied from PO spec §20.2. One condition
// each (CD-147). The site and project rules use the PO's texts (§4).
const (
	msgRequestDateFuture = "Request date can't be in the future."
	msgNeededByEarly     = "Needed by can't be before the request date."
)
