package mstkit

// The M1 table (CD-10): Masters Manager rights, and read for the roles whose
// documents link to the master. Every scope is "all"; Administrator has no
// rows (AUTH grants it everything through the role).
var (
	allRights     = []string{"read", "create", "write", "delete", "export", "import"}
	noImport      = []string{"read", "create", "write", "delete", "export"}
	purchaseStore = []string{"Purchase User", "Stores"}
	allCreators   = []string{"Purchase User", "Stores", "Employee"}
	employeeOnly  = []string{"Employee"}
)

type masterRights struct {
	manager, readers []string
}

var defaultRights = map[string]masterRights{
	"Company":       {[]string{"read", "write", "export"}, allCreators},
	"Branch":        {noImport, allCreators},
	"Party":         {allRights, purchaseStore},
	"Address":       {allRights, purchaseStore},
	"Contact":       {allRights, purchaseStore},
	"Payment Terms": {noImport, purchaseStore},
	"Item":          {allRights, purchaseStore},
	"UoM":           {allRights, purchaseStore},
	"Tax Code":      {allRights, allCreators},
	"Project":       {allRights, allCreators},
	"Site":          {allRights, allCreators},
	"Employee":      {allRights, employeeOnly},
	"Expense Type":  {allRights, employeeOnly},
}

// DefaultRights returns role → right → scope for a master DocType, nil for an
// unknown one. Each call returns fresh maps.
func DefaultRights(docType string) map[string]map[string]string {
	mr, ok := defaultRights[docType]
	if !ok {
		return nil
	}
	out := map[string]map[string]string{roleMastersManager: scopeAll(mr.manager...)}
	for _, role := range mr.readers {
		out[role] = scopeAll("read")
	}
	return out
}

func scopeAll(rights ...string) map[string]string {
	m := make(map[string]string, len(rights))
	for _, r := range rights {
		m[r] = "all"
	}
	return m
}
