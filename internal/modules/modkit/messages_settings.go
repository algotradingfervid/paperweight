package modkit

// Business settings and period lock messages (PO spec §14.1, §14.2; M0-7
// rulings, CD-262).
const (
	msgSettingsAdminOnly = "Only an administrator can change business settings."
	msgSettingsStale     = "Someone else changed Business settings after you opened it. Reload it and make your changes again."
	msgTermsTooLong      = "Default PO terms can be at most 2,000 characters."
	// msgLocked is "{Label} {display} is dated {date}, which is locked. The
	// period is locked up to {lock date}." (§14.2), dates dd-mm-yyyy.
	msgLocked = "%s %s is dated %s, which is locked. The period is locked up to %s."
)
