package purchase

// rights builds a DefaultRights entry: every right in rights gets scope.
func rights(scope string, rights ...string) map[string]string {
	m := make(map[string]string, len(rights))
	for _, r := range rights {
		m[r] = scope
	}
	return m
}

// orderRights are purchase.Order's production DefaultRights (PO spec §10,
// 00-fixtures.md): only a draft's owner edits, submits or deletes it (§21),
// and amend is every Purchase User's, as the Order's CloseRight (CD-247).
// Administrator holds every right through its role.
func orderRights() map[string]map[string]string {
	user := rights("all", "read", "create", "amend", "print", "export")
	for r, s := range rights("own", "write", "submit", "delete") {
		user[r] = s
	}
	return map[string]map[string]string{
		"Purchase User":    user,
		"Purchase Manager": rights("all", "read", "approve", "cancel", "print", "export"),
		"Director":         rights("all", "read", "approve", "print", "export"),
		"Stores":           rights("all", "read"),
	}
}

// requestRights are purchase.Request's production DefaultRights (PO spec
// §20.6) without the approve rows, which P5 adds once auth.Applies allows
// approve through CloseRight (M0-24, CD-280 D2).
func requestRights() map[string]map[string]string {
	return map[string]map[string]string{
		"Employee":         rights("own", "read", "create", "write", "submit", "cancel", "delete"),
		"Stores":           rights("all", "read"),
		"Purchase User":    rights("all", "read"),
		"Purchase Manager": rights("all", "read"),
		"Director":         rights("all", "read"),
	}
}
