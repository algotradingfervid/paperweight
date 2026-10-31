package goodsreceipt

// rights builds a DefaultRights entry: every right in rights gets scope.
func rights(scope string, rights ...string) map[string]string {
	m := make(map[string]string, len(rights))
	for _, r := range rights {
		m[r] = scope
	}
	return m
}

// defaultRights are GRN spec §10's rows. Stores has no cancel: a Purchase
// Manager cancels and Stores amends. Administrator holds every right through
// its role, so it has no key.
func defaultRights() map[string]map[string]string {
	stores := rights("all", "read", "create", "write", "submit", "amend", "print")
	stores["delete"] = "own"
	return map[string]map[string]string{
		"Stores":           stores,
		"Purchase User":    rights("all", "read", "print", "export"),
		"Purchase Manager": rights("all", "read", "cancel", "print", "export"),
		"Director":         rights("all", "read", "print", "export"),
	}
}
