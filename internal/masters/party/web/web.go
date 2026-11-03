// Package partyweb is the Party master's screens (MST Task 22, CD-50, CD-89): the
// list, disable, enable and delete under /m/party, the create wizard at
// /m/party/new, a record's per-section edit with its address and contact pages,
// and the picker search at /pick/Party. The screens themselves are mstweb's;
// this package gives them the services, the copy and the functions below.
package partyweb

import (
	"context"
	"slices"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	gstinweb "github.com/vardhan/internaltools/internal/masters/gstin/web"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
	"github.com/vardhan/internaltools/internal/ui/docscreen"
)

const (
	basePath = "/m/party"

	stepAddresses = "addresses"
	stepContacts  = "contacts"

	termsField = "default_payment_terms_id"

	// roleField is the form's one choice for the two role columns (UI v2
	// §5.5 item 9, CD-369); import and the API keep the columns.
	roleField = "role"
)

// The role choices and the columns each sets.
const (
	roleVendor   = "vendor"
	roleCustomer = "customer"
	roleBoth     = "both"
)

// Routes mounts the party screens under /m/party and the picker at /pick/Party.
// The wizard's values wait in sm's session.
func Routes(svc *party.Service, addresses *address.Service, env mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
	rec := records{env: env, parties: svc, addresses: addresses}
	saver := wizardSaver{env: env, parties: svc, addresses: addresses}
	return mstweb.Screen[party.Party, party.Input]{
		DocType: party.DocType,
		Base:    basePath,
		Copy: mstweb.Copy{
			Plural: "Parties", Noun: "parties", One: "party", New: "New party",
			Search: "Search parties", Empty: "No parties yet.", Icon: "handshake",
			Saved: "Party saved.", Disabled: "Party disabled.", Enabled: "Party enabled.", Deleted: "Party deleted.",
		},
		Svc:      svc,
		Env:      env,
		ToInput:  toInput,
		Validate: party.Validate,
		Values:   values,
		Head:     head,
		Ref:      ref,
		Codes:    map[string]components.CodeKind{"pan": components.PAN, "gstin": components.GSTIN},
		Selects:  map[string][]components.Option{"state_code": gstinweb.StateOptions()},
		Extra:    rec.lists,
		Mount:    rec.routes,
		PickRole: pickRole(svc),
		PickLine: addresses.PickLine,
		Choices: map[string]mstweb.Choice{roleField: {Label: "This party is a", Hint: "Only vendors can be picked on purchase orders.", Required: true, Options: []components.Option{
			{Value: roleVendor, Label: "Vendor"}, {Value: roleCustomer, Label: "Customer"}, {Value: roleBoth, Label: "Both"},
		}}},
		Renames: map[string]string{"is_vendor": roleField, "is_customer": roleField},
		// A vendor or customer picker adds a party and its billing address
		// (UI v2 §5.8, CD-375).
		Quick: &mstweb.Quick{
			Fields:  quickFields(),
			Given:   map[string]mstweb.Given{"state_code": {By: "gstin", Check: components.GSTINState, Echo: stateEcho}},
			Nouns:   map[string]components.Noun{roleVendor: {One: "vendor", Many: "vendors"}, roleCustomer: {One: "customer", Many: "customers"}},
			Icon:    "user-plus",
			Line:    "Just enough to raise this order. Add contacts and more addresses later in Parties.",
			Creates: []string{address.DocType},
			Save:    saver.quick,
		},
		// A party's wizard also creates its addresses and contacts: say no at
		// the door, not at the last step.
		CanCreate: func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
			if err := env.Auth.CanTx(ctx, tx, p, address.DocType, auth.RightCreate, nil); err != nil {
				return err
			}
			return env.Auth.CanTx(ctx, tx, p, address.ContactDocType, auth.RightCreate, nil)
		},
		Sections: []docscreen.FormStep{
			{Slug: "details", Label: "Party details", Icon: "handshake", Fields: []string{"code", "name", roleField}},
			{Slug: "gst", Label: "GST", Icon: "percent", Fields: []string{"pan", "gst_category"}},
			{Slug: "terms", Label: "Payment Terms", Icon: "calendar-clock", Fields: []string{termsField}},
		},
		Cards: []mstweb.FormCard{
			{Title: "Who they are", Icon: "handshake", Description: "How the party appears in pickers and on orders.", Fields: []string{"code", "name", roleField}},
			{Title: "Tax", Icon: "percent", Description: "Decides whether orders show CGST and SGST or IGST.", Fields: []string{"pan", "gst_category"}},
			{Title: "Payment", Icon: "calendar-clock", Description: "When this party is paid, unless an order says otherwise.", Fields: []string{termsField}},
		},
		Wizard: mstweb.CreateWizard[draft]{
			Sessions: sm,
			Steps: []docscreen.FormStep{ // Party's fields declare no Step: its other steps are other DocTypes
				{Slug: "details", Label: "Party", Icon: "handshake", Fields: []string{"code", "name", roleField, "pan", "gst_category", termsField},
					Intro: "A party is a company you buy from or sell to. Nothing is saved until the last step."},
				{Slug: stepAddresses, Label: "Addresses", Icon: "map-pin", Intro: "Add the addresses that go on orders and invoices."},
				{Slug: stepContacts, Label: "Contacts", Icon: "contact", Intro: "Add the people you deal with there, if any."},
			},
			Rows: map[string]mstweb.RowStep{
				stepAddresses: {Legend: "Addresses", AddLabel: "Add address", Noun: "address", Fields: addressFields(), Defaults: addressDefaults, FirstDefaults: firstAddressDefaults},
				stepContacts:  {Legend: "Contacts", AddLabel: "Add contact", Noun: "contact", Fields: contactFields()},
			},
			ToInput:  toDraft,
			Validate: validateDraft,
			Save:     saver.save,
		},
	}.Routes()
}

// pickRole is the picker search of a Link with a LinkRole (CD-218). Any other
// role is a bad URL: no form asks for one.
func pickRole(svc *party.Service) func(context.Context, auth.Principal, string, string) ([]components.PickRow, error) {
	return func(ctx context.Context, p auth.Principal, role, search string) ([]components.PickRow, error) {
		r := party.Vendor
		switch role {
		case "vendor":
		case "customer":
			r = party.Customer
		default:
			return nil, auth.ErrBadForm
		}
		found, err := svc.Search(ctx, p, r, search, false)
		if err != nil {
			return nil, err
		}
		rows := make([]components.PickRow, len(found))
		for i, c := range found {
			rows[i] = components.PickRow{ID: c.ID, Code: c.Code, Name: c.Name}
		}
		return rows, nil
	}
}

// toInput is the form's values (keyed by Field.Name) as the service input. A
// blank link is none; one that can't be parsed gets the service's refusal of one
// that doesn't exist. A blank role sets neither column, which the service
// refuses with its own text.
func toInput(v map[string]string) (party.Input, []doctype.FieldError) {
	var errs []doctype.FieldError
	var terms int64
	if s := v[termsField]; s != "" {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			errs = append(errs, doctype.FieldError{Field: termsField, Message: party.MsgTermsNotFound})
		}
		terms = id
	}
	return party.Input{
		Code: v["code"], Name: v["name"], IsVendor: v[roleField] == roleVendor || v[roleField] == roleBoth,
		IsCustomer: v[roleField] == roleCustomer || v[roleField] == roleBoth,
		PAN:        v["pan"], GSTCategory: gstin.Category(v["gst_category"]), DefaultPaymentTermsID: terms,
	}, errs
}

// values is a stored party as the form's values.
func values(p party.Party) map[string]string {
	return map[string]string{
		"code": p.Code, "name": p.Name, roleField: role(p),
		"pan": p.PAN, "gst_category": string(p.GSTCategory), termsField: strconv.FormatInt(p.DefaultPaymentTermsID, 10),
	}
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// role is p's role choice, none when neither column is set.
func role(p party.Party) string {
	switch {
	case p.IsVendor && p.IsCustomer:
		return roleBoth
	case p.IsVendor:
		return roleVendor
	case p.IsCustomer:
		return roleCustomer
	}
	return ""
}

func head(p party.Party) mstweb.Head {
	return mstweb.Head{Code: p.Code, Name: p.Name, Disabled: p.Disabled, Version: p.Version}
}

// ref is the record AUTH checks, as party.Service builds it: Party declares no
// project, site or employee field.
func ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: party.DocType, ID: id}
}

// fieldsOf is docType's fields of these names, in order, ImportOnly ones too.
func fieldsOf(docType string, names ...string) []doctype.Field {
	dt, _ := doctype.Lookup(docType)                   // registered by the master package's init
	all := slices.Concat(dt.Fields, dt.ImportFields()) // email and mobile are only in the second
	out := make([]doctype.Field, len(names))
	for i, name := range names {
		out[i] = all[slices.IndexFunc(all, func(fd doctype.Field) bool { return fd.Name == name })]
	}
	return out
}

// quickFields is the quick-create form (CD-375, CD-388): the party's name,
// then its billing address's GSTIN, city, line 1 and state. The code comes
// from the name unless it is refused. State is required, and shows only
// while no valid GSTIN gives it.
func quickFields() []doctype.Field {
	f := slices.Concat(fieldsOf(party.DocType, "code", "name"), fieldsOf(address.DocType, "gstin", "city", "line1", "state_code"))
	f[0].Hint = "Made from the name. You can change it."
	f[1].Hint = "The code is made from this. You can change it later in Parties."
	f[4].Label, f[4].Hint = "Address line 1", "Saved as their billing address."
	f[5].Required = true
	return f
}

// stateEcho is the live check's echo for g when it is a valid GSTIN that
// names a state, and "" when it doesn't give one.
func stateEcho(g string) string {
	_, echo, err := gstinweb.Parsers()[string(components.GSTINState)](g)
	if err != nil {
		return ""
	}
	return echo
}

func addressFields() []doctype.Field {
	f := fieldsOf(address.DocType, "title", "line1", "line2", "city", "state_code", "country", "pincode", "gstin",
		"is_billing", "is_shipping", "is_primary_billing", "is_primary_shipping")
	f[1].Label, f[2].Label = "Address line 1", "Address line 2" // the service's messages say so; the CSV column stays Line 1
	return f
}

func contactFields() []doctype.Field {
	return fieldsOf(address.ContactDocType, "first_name", "middle_name", "last_name", "email", "mobile", "is_primary")
}

// addressDefaults is a new address's values.
var addressDefaults = map[string]string{"country": "India"}

// firstAddressDefaults is a party's first address: it bills, and is the
// primary billing address, which a registered party needs (CD-180).
var firstAddressDefaults = map[string]string{"country": "India", "is_billing": "1", "is_primary_billing": "1"}

// addressInput is an address's fields as the service input.
func addressInput(v map[string]string) address.Input {
	return address.Input{
		Title: v["title"], Line1: v["line1"], Line2: v["line2"], City: v["city"], StateCode: v["state_code"],
		Country: v["country"], Pincode: v["pincode"], GSTIN: v["gstin"],
		IsBilling: v["is_billing"] == "1", IsShipping: v["is_shipping"] == "1",
		PrimaryBilling: v["is_primary_billing"] == "1", PrimaryShipping: v["is_primary_shipping"] == "1",
	}
}
