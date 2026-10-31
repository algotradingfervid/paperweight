package purchase

// P6's messages (PO lines with a request row), copied from PO spec §20.5.
// One condition each (CD-147). DOC owns the draft, closed and cancelled
// request texts.
const (
	msgRequestSite        = "Line %d: Request %s is for site %s. This order delivers to %s."                   // n, PR, request site, PO site
	msgRequestProject     = "Line %d: Request %s is for project %s. This order is for %s."                     // n, PR, request project, PO project
	msgRequestItem        = "Line %d: Request %s line %d is for %s. Remove the row to order a different item." // n, PR, m, item code
	msgRequestUoM         = "Line %d: Request %s line %d is in %s. Keep that unit."                            // n, PR, m, uom code
	msgRequestSameRow     = "Line %d: Request %s line %d is already on line %d."                               // n, PR, m, k
	msgRequestAlreadyOn   = "Line %d: Request %s line %d is already on %s."                                    // n, PR, m, PO display
	msgRequestOverRequest = "Line %d: ordering %s more than request %s asked for."                             // n, x, PR (a warning)
)
