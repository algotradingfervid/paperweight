package gallery

import (
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/uitest"
)

// The auth pages (CD-88) show the target shape of AUTH's views, built only
// from UI components. Their entries are in pages (routes.go). The structs
// below are the gallery's own samples, not AUTH's page types.

// passwordRules are the rules listed before the password fields (D-AUTH-03).
var passwordRules = []string{
	"Use 10 to 256 characters.",
	"Don't use a common password.",
	"Don't use the part of your email before the @.",
}

// authSession is one signed-in device on the sessions page.
type authSession struct {
	id, device, signedIn, lastSeen string
	current                        bool
}

var authSessions = []authSession{
	{id: "1", device: "Firefox on Linux", signedIn: "24-10-2026 09:15", lastSeen: "24-10-2026 10:30", current: true},
	{id: "2", device: uitest.LongName100, signedIn: "20-10-2026 18:02", lastSeen: "23-10-2026 21:40"},
	{id: "3", device: "Safari on iPhone", signedIn: "02-10-2026 08:45", lastSeen: "22-10-2026 07:10"},
}

func usersList() components.DataListProps {
	return components.DataListProps{
		Label: "Users",
		Columns: []components.Column{
			{Key: "name", Label: "Name", Priority: components.PTitle},
			{Key: "email", Label: "Email", Priority: components.PMeta, Code: true},
			{Key: "roles", Label: "Roles", Priority: components.PMeta},
			{Key: "status", Label: "Status", Priority: components.PLow},
		},
		Rows: []components.Row{
			{Href: "/_gallery/auth-users", Values: map[string]string{"name": uitest.LongName100, "email": uitest.LongEmail, "roles": "Administrator, Purchase manager, Employee", "status": "Active"}},
			{Href: "/_gallery/auth-users", Values: map[string]string{"name": "Priya Shah", "email": "priya@vardhan.test", "roles": "Employee", "status": "Active"}},
			{Href: "/_gallery/auth-users", Values: map[string]string{"name": uitest.LongDevanagari, "email": "ravi@vardhan.test", "roles": "Employee", "status": "Disabled"}},
		},
	}
}

// rightsDocType is one DocType's card on the rights page; team is offered
// only where the DocType has an employee field (CD-11).
type rightsDocType struct {
	key, label string
	team       bool
	rights     []rightRow
}

type rightRow struct{ key, label, meaning, value string }

var rightsDocTypes = []rightsDocType{
	{key: "expense_claim", label: "Expense Claim", team: true, rights: []rightRow{
		{"read", "Read", "See claims and their attachments.", "team"},
		{"approve", "Approve", "Approve or reject claims waiting for approval.", "team"},
	}},
	{key: "purchase_order", label: "Purchase Order", rights: []rightRow{
		{"read", "Read", "See orders and their attachments.", "all"},
		{"create", "Create", "Start new orders and edit drafts.", "own"},
	}},
}

func scopeOptions(team bool) []components.Option {
	opts := []components.Option{{Value: "none", Label: "None"}, {Value: "own", Label: "Own"}}
	if team {
		opts = append(opts, components.Option{Value: "team", Label: "Team"})
	}
	return append(opts, components.Option{Value: "all", Label: "All"})
}
