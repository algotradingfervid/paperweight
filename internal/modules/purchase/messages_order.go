package purchase

// P2's messages (order Validate, BeforeSubmit and OrderWarnings), copied from
// PO spec §3.2, §4, §27.3, CD-176 and CD-278. One condition each (CD-147).
const (
	msgOrderDateFuture   = "Order date can't be in the future."
	msgRequiredByEarly   = "Required by can't be before the order date."
	msgSiteProject       = "Site %s is on project %s. Pick that project or another site." // site code, project code
	msgOverseasVendor    = "Overseas vendors aren't supported on purchase orders yet."
	msgEarlierPONeeded   = "Earlier PO no. is required when a line has Already received."
	msgEarlierPOWithDate = "Earlier PO no. is required with an earlier PO date."
	msgReverseCharge     = "Line %d: Reverse charge needs a taxable tax code on every line."
	msgNoConversion      = "Line %d: Item %s has no conversion for UoM %s. Pick another UoM." // CD-278
	msgAlreadyAbove      = "Line %d: Already received is more than the quantity."
	msgZeroPrice         = "Price is ₹0 on line %d."
	msgTotalTooLarge     = "The total is too large. The most a purchase order can be is %s. Split it into more than one purchase order." // CD-308, CD-311 (1); the maximum
	// site code, its branch code and state, project code, its branch code and state
	msgSiteProjectState = "Site %s is under branch %s in %s, but project %s belongs to branch %s in %s. Check the tax with your CA."
)
