package partyweb

import (
	"cmp"
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	mstweb "github.com/vardhan/internaltools/internal/masters/mstkit/web"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/ui/components"
)

const (
	partyPattern = "/{id:[1-9][0-9]*}"
	rowPattern   = "/{row:[1-9][0-9]*}"
)

// stored is an address or a contact as its form shows it.
type stored struct {
	PartyID int64
	Version int
	Title   string
	Values  map[string]string
}

// rowKind is what differs between an address's pages and a contact's. Each has
// its own rights, version and routes under its party (CD-50).
type rowKind struct {
	kind                                    mstkit.Kind
	path, newTitle                          string
	saved, disabledMsg, enabledMsg, deleted string
	fields                                  []doctype.Field
	defaults                                map[string]string
	get                                     func(ctx context.Context, tx *db.Tx, id int64) (stored, error)
	posted                                  func(r *http.Request) map[string]string
	create                                  func(ctx context.Context, p auth.Principal, partyID int64, v map[string]string) (mstkit.Saved, error)
	update                                  func(ctx context.Context, p auth.Principal, id int64, version int, v map[string]string) (mstkit.Saved, error)
	setDisabled                             func(ctx context.Context, p auth.Principal, id int64, version int, off bool) error
	remove                                  func(ctx context.Context, p auth.Principal, id int64, version int) error
	// extra is the form's controls besides fields.
	extra func(pages mstweb.Pages, f mstweb.Form) []templ.Component
}

// ref is the row AUTH checks: Address and Contact declare no project, site or
// employee field.
func (k rowKind) ref(id int64) *auth.RecordRef {
	return &auth.RecordRef{DocType: k.kind.DocType, ID: id}
}

func (k rowKind) names() []string {
	names := make([]string, len(k.fields))
	for i, fd := range k.fields {
		names[i] = fd.Name
	}
	return names
}

func (s records) addressKind() rowKind {
	a := s.addresses
	return rowKind{
		kind: address.Kind, path: stepAddresses, newTitle: "New address",
		saved: "Address saved.", disabledMsg: "Address disabled.", enabledMsg: "Address enabled.", deleted: "Address deleted.",
		fields: addressFields(), defaults: addressDefaults,
		get: func(ctx context.Context, tx *db.Tx, id int64) (stored, error) {
			x, err := a.Get(ctx, tx, id)
			return stored{PartyID: x.PartyID, Version: x.Version, Title: cmp.Or(x.Title, x.City), Values: addressValues(x)}, err
		},
		posted: func(r *http.Request) map[string]string {
			v := map[string]string{}
			for _, fd := range addressFields() {
				v[fd.Name] = r.PostFormValue(fd.Name)
			}
			return v
		},
		create: func(ctx context.Context, p auth.Principal, partyID int64, v map[string]string) (mstkit.Saved, error) {
			in := addressInput(v)
			in.PartyID = partyID
			return a.Create(ctx, p, in)
		},
		update: func(ctx context.Context, p auth.Principal, id int64, version int, v map[string]string) (mstkit.Saved, error) {
			return a.Update(ctx, p, id, version, addressInput(v))
		},
		setDisabled: a.SetDisabled, remove: a.Delete,
	}
}

func (s records) contactKind() rowKind {
	a := s.addresses
	fields := fieldsOf(address.ContactDocType, "first_name", "middle_name", "last_name", "is_primary")
	return rowKind{
		kind: address.ContactKind, path: stepContacts, newTitle: "New contact",
		saved: "Contact saved.", disabledMsg: "Contact disabled.", enabledMsg: "Contact enabled.", deleted: "Contact deleted.",
		fields: fields,
		get: func(ctx context.Context, tx *db.Tx, id int64) (stored, error) {
			x, err := a.GetContact(ctx, tx, id)
			return stored{PartyID: x.PartyID, Version: x.Version, Title: address.FullName(x), Values: contactValues(x)}, err
		},
		posted: func(r *http.Request) map[string]string {
			v := map[string]string{}
			for _, fd := range fields {
				v[fd.Name] = r.PostFormValue(fd.Name)
			}
			for key := range r.PostForm { // PostFormValue parsed it
				if strings.HasPrefix(key, emailList+"[") || strings.HasPrefix(key, phoneList+"[") {
					v[key] = r.PostFormValue(key)
				}
			}
			return v
		},
		create: func(ctx context.Context, p auth.Principal, partyID int64, v map[string]string) (mstkit.Saved, error) {
			in := contactFormInput(v)
			in.PartyID = partyID
			return a.CreateContact(ctx, p, in)
		},
		update: func(ctx context.Context, p auth.Principal, id int64, version int, v map[string]string) (mstkit.Saved, error) {
			return a.UpdateContact(ctx, p, id, version, contactFormInput(v))
		},
		setDisabled: a.SetContactDisabled, remove: a.DeleteContact,
		extra: contactLists,
	}
}

func addressValues(a address.Address) map[string]string {
	return map[string]string{
		"title": a.Title, "line1": a.Line1, "line2": a.Line2, "city": a.City, "state_code": a.StateCode,
		"country": a.Country, "pincode": a.Pincode, "gstin": a.GSTIN,
		"is_billing": flag(a.IsBilling), "is_shipping": flag(a.IsShipping),
		"is_primary_billing": flag(a.PrimaryBilling), "is_primary_shipping": flag(a.PrimaryShipping),
	}
}

// A contact form's lists post as emails[N].email, emails[N].primary,
// phones[N].number, phones[N].mobile and phones[N].primary.
const (
	emailList = "emails"
	phoneList = "phones"
)

func listKey(name string, no int, field string) string {
	return name + "[" + strconv.Itoa(no) + "]." + field
}

func contactValues(c address.Contact) map[string]string {
	v := map[string]string{
		"first_name": c.FirstName, "middle_name": c.MiddleName, "last_name": c.LastName, "is_primary": flag(c.Primary),
	}
	for i, e := range c.Emails {
		v[listKey(emailList, i+1, "email")] = e.Email
		v[listKey(emailList, i+1, "primary")] = flag(e.Primary)
	}
	for i, ph := range c.Phones {
		v[listKey(phoneList, i+1, "number")] = ph.Number
		v[listKey(phoneList, i+1, "mobile")] = flag(ph.Mobile)
		v[listKey(phoneList, i+1, "primary")] = flag(ph.Primary)
	}
	return v
}

// filledNos is the numbers of the list's rows with something in field, in order.
func filledNos(v map[string]string, name, field string) []int {
	var nos []int
	for key, value := range v {
		rest, ok := strings.CutPrefix(key, name+"[")
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		num, f, _ := strings.Cut(rest, "].")
		if no, err := strconv.Atoi(num); err == nil && f == field {
			nos = append(nos, no)
		}
	}
	slices.Sort(nos)
	return nos
}

// contactFormInput is the contact form's values as the service input. A row left
// blank is the form's spare one, not an email to refuse.
func contactFormInput(v map[string]string) address.ContactInput {
	in := address.ContactInput{
		FirstName: v["first_name"], MiddleName: v["middle_name"], LastName: v["last_name"], Primary: v["is_primary"] == "1",
	}
	for _, no := range filledNos(v, emailList, "email") {
		in.Emails = append(in.Emails, address.EmailInput{Email: v[listKey(emailList, no, "email")], Primary: v[listKey(emailList, no, "primary")] == "1"})
	}
	for _, no := range filledNos(v, phoneList, "number") {
		in.Phones = append(in.Phones, address.PhoneInput{
			Number: v[listKey(phoneList, no, "number")], Mobile: v[listKey(phoneList, no, "mobile")] == "1", Primary: v[listKey(phoneList, no, "primary")] == "1",
		})
	}
	return in
}

// contactLists is the contact form's emails and phones, each with a spare row
// for another, ticked Mobile because most are.
func contactLists(pages mstweb.Pages, f mstweb.Form) []templ.Component {
	var out []templ.Component
	input := func(key, label string, kind doctype.FieldType) {
		out = append(out, pages.Input(doctype.Field{Label: label, Type: kind}, key, f))
	}
	for i, no := range withSpare(filledNos(f.Values, emailList, "email")) {
		input(listKey(emailList, no, "email"), "Email "+strconv.Itoa(i+1), doctype.Text)
		input(listKey(emailList, no, "primary"), "Primary email", doctype.Check)
	}
	phones := filledNos(f.Values, phoneList, "number")
	spare := withSpare(phones)
	f.Values[listKey(phoneList, spare[len(phones)], "mobile")] = "1"
	for i, no := range spare {
		input(listKey(phoneList, no, "number"), "Phone "+strconv.Itoa(i+1), doctype.Text)
		input(listKey(phoneList, no, "mobile"), "Mobile", doctype.Check)
		input(listKey(phoneList, no, "primary"), "Primary phone", doctype.Check)
	}
	return out
}

// withSpare is nos and one number past them.
func withSpare(nos []int) []int {
	spare := 1
	if len(nos) > 0 {
		spare = nos[len(nos)-1] + 1
	}
	return append(slices.Clone(nos), spare)
}

// records is the pages and actions of a party's addresses and contacts.
type records struct {
	env       mstkit.Env
	parties   *party.Service
	addresses *address.Service
}

// routes mounts the row pages under the record. Every answer is a 303 to the
// record; a form's refusal stays on the form, an action's on the record page.
func (s records) routes(pages mstweb.Pages) func(chi.Router) {
	return func(r chi.Router) {
		for _, k := range []rowKind{s.addressKind(), s.contactKind()} {
			base := basePath + partyPattern + "/" + k.path
			row := base + rowPattern
			r.Get(base+"/new", pages.Handle(s.newPage(pages, k)))
			r.Post(base+"/new", pages.Handle(s.create(pages, k)))
			r.Get(row, pages.Handle(s.edit(pages, k)))
			r.Post(row, pages.Handle(s.save(pages, k)))
			r.Post(row+"/disable", pages.Handle(s.setDisabled(pages, k, true)))
			r.Post(row+"/enable", pages.Handle(s.setDisabled(pages, k, false)))
			r.Post(row+"/delete", pages.Handle(s.remove(pages, k)))
		}
		r.Post(basePath+partyPattern+"/"+stepAddresses+rowPattern+"/set-primary", pages.Handle(s.setPrimaryAddress(pages)))
		r.Post(basePath+partyPattern+"/"+stepContacts+rowPattern+"/set-primary", pages.Handle(s.setPrimaryContact(pages)))
	}
}

func partyPath(id int64) string { return basePath + "/" + strconv.FormatInt(id, 10) }

func (k rowKind) rowPath(partyID, id int64) string {
	return partyPath(partyID) + "/" + k.path + "/" + strconv.FormatInt(id, 10)
}

func rowID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(chi.URLParam(r, "row"), 10, 64) // too large is no row: Get's 404
	return id
}

// readParty reads the party a row route is under, refusing as its record does:
// rows live under a party, so a user who can't read it reaches none of them.
func (s records) readParty(ctx context.Context, tx *db.Tx, p auth.Principal, id int64) (party.Party, error) {
	if err := s.env.CanRowTx(ctx, tx, p, party.Kind, auth.RightRead, ref(id)); err != nil {
		return party.Party{}, err
	}
	return s.parties.Get(ctx, tx, id)
}

// urlParty is readParty in one read, for a route that checks its row in the write.
func (s records) urlParty(r *http.Request, p auth.Principal) (party.Party, error) {
	ctx := r.Context()
	var pt party.Party
	err := s.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		pt, err = s.readParty(ctx, tx, p, mstweb.RecordID(r))
		return err
	})
	return pt, err
}

// open reads the party, checks p's right, on the row when the URL names one, and
// reads the row. A row that isn't the party's is a 404: the owner never changes
// (CD-176, CD-190), so this read holds for the write that follows.
func (s records) open(r *http.Request, p auth.Principal, k rowKind, right auth.Right) (party.Party, stored, error) {
	ctx := r.Context()
	id := rowID(r)
	var pt party.Party
	var st stored
	err := s.env.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if pt, err = s.readParty(ctx, tx, p, mstweb.RecordID(r)); err != nil {
			return err
		}
		if id == 0 {
			return s.env.Auth.CanTx(ctx, tx, p, k.kind.DocType, right, nil)
		}
		if err := s.env.CanRowTx(ctx, tx, p, k.kind, right, k.ref(id)); err != nil {
			return err
		}
		if st, err = k.get(ctx, tx, id); err != nil {
			return err
		}
		if st.PartyID != pt.ID {
			return doctype.ErrNotFound
		}
		return nil
	})
	return pt, st, err
}

// show is the row's form, a new row's with id 0.
func (s records) show(w http.ResponseWriter, r *http.Request, pages mstweb.Pages, status int, k rowKind, pt party.Party, id int64, title string, f mstweb.Form, confirm bool) error {
	sub := mstweb.SubForm{
		ID: id, Title: title, Action: partyPath(pt.ID) + "/" + k.path + "/new", Form: f, Confirm: confirm,
		Back: components.Link{Label: pt.Code + " " + pt.Name, Href: partyPath(pt.ID)},
	}
	if id != 0 {
		sub.Action = k.rowPath(pt.ID, id)
	}
	for _, fd := range k.fields {
		sub.Inputs = append(sub.Inputs, pages.Input(fd, fd.Name, f))
	}
	if k.extra != nil {
		sub.Inputs = append(sub.Inputs, k.extra(pages, f)...)
	}
	mstweb.RenderSubForm(w, r, status, sub)
	return nil
}

func (s records) newPage(pages mstweb.Pages, k rowKind) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, _, err := s.open(r, p, k, auth.RightCreate)
		if err != nil {
			return err
		}
		values := map[string]string{}
		maps.Copy(values, k.defaults)
		return s.show(w, r, pages, http.StatusOK, k, pt, 0, k.newTitle, mstweb.Form{Values: values}, false)
	}
}

func (s records) create(pages mstweb.Pages, k rowKind) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, _, err := s.open(r, p, k, auth.RightCreate)
		if err != nil {
			return err
		}
		f := mstweb.Form{Values: k.posted(r)}
		saved, err := k.create(r.Context(), p, pt.ID, f.Values)
		if err != nil {
			return s.refused(w, r, pages, k, pt, 0, k.newTitle, f, err)
		}
		return mstweb.Done(w, r, k.saved, partyPath(pt.ID), saved.Warnings...)
	}
}

// edit serves the row's form, or with ?delete its confirm step, which needs
// delete and not write.
func (s records) edit(pages mstweb.Pages, k rowKind) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		confirm := r.URL.Query().Has("delete")
		right := auth.RightWrite
		if confirm {
			right = auth.RightDelete
		}
		pt, st, err := s.open(r, p, k, right)
		if err != nil {
			return err
		}
		return s.show(w, r, pages, http.StatusOK, k, pt, rowID(r), st.Title, mstweb.Form{Values: st.Values, Version: st.Version}, confirm)
	}
}

// save updates the row at the version the page posted (CD-94).
func (s records) save(pages mstweb.Pages, k rowKind) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, st, err := s.open(r, p, k, auth.RightWrite)
		if err != nil {
			return err
		}
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		f := mstweb.Form{Values: k.posted(r), Version: version}
		saved, err := k.update(r.Context(), p, rowID(r), version, f.Values)
		if err != nil {
			return s.refused(w, r, pages, k, pt, rowID(r), st.Title, f, err)
		}
		return mstweb.Done(w, r, k.saved, partyPath(pt.ID), saved.Warnings...)
	}
}

// refused shows a save's refusal on its form, what the form has no control for
// in the banner.
func (s records) refused(w http.ResponseWriter, r *http.Request, pages mstweb.Pages, k rowKind, pt party.Party, id int64, title string, f mstweb.Form, err error) error {
	f, status, ok := f.Refused(err, k.names())
	if !ok {
		return err
	}
	return s.show(w, r, pages, status, k, pt, id, title, f, false)
}

func (s records) setDisabled(pages mstweb.Pages, k rowKind, off bool) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, _, err := s.open(r, p, k, auth.RightWrite)
		if err != nil {
			return err
		}
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		if err := k.setDisabled(r.Context(), p, rowID(r), version, off); err != nil {
			return pages.Refused(w, r, p, pt.ID, err)
		}
		msg := k.enabledMsg
		if off {
			msg = k.disabledMsg
		}
		return mstweb.Done(w, r, msg, partyPath(pt.ID))
	}
}

func (s records) remove(pages mstweb.Pages, k rowKind) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, _, err := s.open(r, p, k, auth.RightDelete)
		if err != nil {
			return err
		}
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		if err := k.remove(r.Context(), p, rowID(r), version); err != nil {
			return pages.Refused(w, r, p, pt.ID, err)
		}
		return mstweb.Done(w, r, k.deleted, partyPath(pt.ID))
	}
}

// setPrimaryAddress posts kind=billing or shipping, at the posted version. The
// service takes no owner, so the write checks it first.
func (s records) setPrimaryAddress(pages mstweb.Pages) mstweb.HandlerFunc {
	k := s.addressKind()
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		pt, err := s.urlParty(r, p)
		if err != nil {
			return err
		}
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		kind := r.PostFormValue("kind")
		if kind != "billing" && kind != "shipping" {
			return auth.ErrBadForm
		}
		ctx := r.Context()
		err = s.env.Write(ctx, p, address.Kind, auth.RightWrite, func(tx *db.Tx) error {
			a, err := s.addresses.Get(ctx, tx, rowID(r))
			if err != nil {
				return err
			}
			if a.PartyID != pt.ID {
				return doctype.ErrNotFound
			}
			return s.addresses.SetPrimaryTx(ctx, tx, p, a.ID, kind == "billing", version)
		})
		if err != nil {
			return pages.Refused(w, r, p, pt.ID, err)
		}
		return mstweb.Done(w, r, k.saved, partyPath(pt.ID))
	}
}

// setPrimaryContact makes the contact the party's primary. The service words an
// unknown contact and another party's, so there is no 404 here; it takes no
// version, so a row that is the party's has the posted one checked first, in
// the same write (CD-94).
func (s records) setPrimaryContact(pages mstweb.Pages) mstweb.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, p auth.Principal) error {
		ctx := r.Context()
		pt, err := s.urlParty(r, p)
		if err != nil {
			return err
		}
		id := rowID(r)
		version, err := mstweb.PostedVersion(r)
		if err != nil {
			return err
		}
		err = s.env.Write(ctx, p, address.ContactKind, auth.RightWrite, func(tx *db.Tx) error {
			c, err := s.addresses.GetContact(ctx, tx, id)
			if err != nil && !errors.Is(err, doctype.ErrNotFound) {
				return err
			}
			if err == nil && c.PartyID == pt.ID {
				// The right first: a hidden row is not told apart by its version.
				if err := s.env.CanRowTx(ctx, tx, p, address.ContactKind, auth.RightWrite, s.contactKind().ref(id)); err != nil {
					return err
				}
				if err := mstkit.CheckVersion(ctx, tx, address.ContactKind, id, version); err != nil {
					return err
				}
			}
			return s.addresses.SetPrimaryContactTx(ctx, tx, p, pt.ID, id)
		})
		if err != nil {
			return pages.Refused(w, r, p, pt.ID, err)
		}
		return mstweb.Done(w, r, s.contactKind().saved, partyPath(pt.ID))
	}
}

// rowsView is one party's addresses or contacts on its record page.
type rowsView struct {
	Title, Empty string
	Add          *components.Link
	Rows         []rowView
}

// rowView is one row: its text and what p may do with it.
type rowView struct {
	Pairs []components.Pair
	Links []components.ButtonProps
	Posts []rowPost
}

// rowPost is one action's form, carrying the row's own version.
type rowPost struct {
	Label, Action, Version, Kind string
}

// lists is the record page's addresses and contacts. A user who can't read one
// sees none of it (it is Address's or Contact's to show), so it renders with
// the request's user.
func (s records) lists(pt party.Party) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		p, _ := auth.PrincipalFrom(ctx)
		var views []rowsView
		for _, build := range []func(context.Context, auth.Principal, int64) (*rowsView, error){s.addressRows, s.contactRows} {
			v, err := build(ctx, p, pt.ID)
			if err != nil {
				return err
			}
			if v != nil {
				views = append(views, *v)
			}
		}
		return partyRows(views).Render(ctx, w)
	})
}

// rowIDs is the ids of the party's rows of docType that p may read, disabled
// ones too.
func (s records) rowIDs(ctx context.Context, p auth.Principal, docType string, partyID int64) ([]int64, error) {
	found, _ := doctype.Lookup(docType) // registered by the master package's init
	dt := *found
	dt.Fields = slices.Clone(found.Fields)
	for i, fd := range dt.Fields {
		if fd.Name == "party_id" {
			dt.Fields[i].Filterable = true // only here: no list screen offers it
		}
	}
	ls := list.Service{DB: s.env.DB, Auth: s.env.Auth}
	var ids []int64
	for page := 1; ; page++ {
		res, err := ls.List(ctx, p, &dt, list.Query{
			Filters: []list.Filter{{Field: "party_id", Op: list.OpEq, Values: []string{strconv.FormatInt(partyID, 10)}}},
			Page:    page, PageSize: list.PageSizes[len(list.PageSizes)-1],
		})
		if err != nil {
			return nil, err
		}
		for _, row := range res.Rows {
			ids = append(ids, row.ID)
		}
		if len(ids) >= res.Total {
			return ids, nil
		}
	}
}

// view is k's row list, nil for a user who can't read it; row fills one row's
// view from its id, and every row shares one read.
func (s records) view(ctx context.Context, p auth.Principal, k rowKind, title, empty, addLabel string, partyID int64, row func(tx *db.Tx, id int64, can func(auth.Right) bool) (rowView, error)) (*rowsView, error) {
	if s.env.Auth.Can(ctx, p, k.kind.DocType, auth.RightRead, nil) != nil {
		return nil, nil //nolint:nilnil // nil is no such section, not a failure
	}
	ids, err := s.rowIDs(ctx, p, k.kind.DocType, partyID)
	if err != nil {
		return nil, err
	}
	v := &rowsView{Title: title, Empty: empty}
	if s.env.Auth.Can(ctx, p, k.kind.DocType, auth.RightCreate, nil) == nil {
		v.Add = &components.Link{Label: addLabel, Href: partyPath(partyID) + "/" + k.path + "/new"}
	}
	err = s.env.DB.Read(ctx, func(tx *db.Tx) error {
		for _, id := range ids {
			r, err := row(tx, id, func(right auth.Right) bool {
				return s.env.CanRowTx(ctx, tx, p, k.kind, right, k.ref(id)) == nil
			})
			if err != nil {
				return err
			}
			v.Rows = append(v.Rows, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// actions is what every row offers: Change, Disable or Enable, Delete after its
// confirm step, and the row's own set-primary posts.
func (k rowKind) actions(partyID, id int64, version int, title string, off bool, can func(auth.Right) bool, primaries []rowPost) rowView {
	path := k.rowPath(partyID, id)
	var v rowView
	if can(auth.RightWrite) {
		v.Links = append(v.Links, components.ButtonProps{Label: "Change", AriaLabel: "Change " + title, Href: path, Variant: components.Ghost})
		toggle := rowPost{Label: "Disable", Action: path + "/disable", Version: strconv.Itoa(version)}
		if off {
			toggle = rowPost{Label: "Enable", Action: path + "/enable", Version: strconv.Itoa(version)}
		} else {
			v.Posts = primaries
		}
		v.Posts = append(v.Posts, toggle)
	}
	if can(auth.RightDelete) {
		v.Links = append(v.Links, components.ButtonProps{Label: "Delete", AriaLabel: "Delete " + title, Href: path + "?delete", Variant: components.Ghost})
	}
	return v
}

func (s records) addressRows(ctx context.Context, p auth.Principal, partyID int64) (*rowsView, error) {
	k := s.addressKind()
	return s.view(ctx, p, k, "Addresses", "No addresses yet.", "Add address", partyID, func(tx *db.Tx, id int64, can func(auth.Right) bool) (rowView, error) {
		a, err := s.addresses.Get(ctx, tx, id)
		if err != nil {
			return rowView{}, err
		}
		title := cmp.Or(a.Title, a.City)
		version := strconv.Itoa(a.Version)
		var primaries []rowPost
		if a.IsBilling && !a.PrimaryBilling {
			primaries = append(primaries, rowPost{Label: "Make primary billing", Action: k.rowPath(partyID, id) + "/set-primary", Version: version, Kind: "billing"})
		}
		if a.IsShipping && !a.PrimaryShipping {
			primaries = append(primaries, rowPost{Label: "Make primary shipping", Action: k.rowPath(partyID, id) + "/set-primary", Version: version, Kind: "shipping"})
		}
		v := k.actions(partyID, id, a.Version, title, a.Disabled, can, primaries)
		v.Pairs = []components.Pair{{Label: "Title", Value: title}, {Label: "Address", Value: address.DisplayText(a)}}
		v.Pairs = appendWords(v.Pairs, "Type", word(a.IsBilling, "Billing"), word(a.IsShipping, "Shipping"))
		v.Pairs = appendWords(v.Pairs, "Primary", word(a.PrimaryBilling, "Primary billing"), word(a.PrimaryShipping, "Primary shipping"))
		v.Pairs = appendWords(v.Pairs, "Status", word(a.Disabled, "Disabled"))
		return v, nil
	})
}

func (s records) contactRows(ctx context.Context, p auth.Principal, partyID int64) (*rowsView, error) {
	k := s.contactKind()
	return s.view(ctx, p, k, "Contacts", "No contacts yet.", "Add contact", partyID, func(tx *db.Tx, id int64, can func(auth.Right) bool) (rowView, error) {
		c, err := s.addresses.GetContact(ctx, tx, id)
		if err != nil {
			return rowView{}, err
		}
		title := address.FullName(c)
		var primaries []rowPost
		if !c.Primary {
			primaries = append(primaries, rowPost{Label: "Make primary", Action: k.rowPath(partyID, id) + "/set-primary", Version: strconv.Itoa(c.Version)})
		}
		v := k.actions(partyID, id, c.Version, title, c.Disabled, can, primaries)
		v.Pairs = []components.Pair{{Label: "Name", Value: title}}
		var emails, phones []string
		for _, e := range c.Emails {
			emails = append(emails, e.Email)
		}
		for _, ph := range c.Phones {
			phones = append(phones, ph.Number)
		}
		v.Pairs = appendWords(v.Pairs, "Email", emails...)
		v.Pairs = appendWords(v.Pairs, "Phone", phones...)
		v.Pairs = appendWords(v.Pairs, "Primary", word(c.Primary, "Primary contact"))
		v.Pairs = appendWords(v.Pairs, "Status", word(c.Disabled, "Disabled"))
		return v, nil
	})
}

// word is text when on, else nothing.
func word(on bool, text string) string {
	if on {
		return text
	}
	return ""
}

// appendWords adds a pair of the non-blank words, none when there are none.
func appendWords(pairs []components.Pair, label string, words ...string) []components.Pair {
	words = slices.DeleteFunc(words, func(w string) bool { return w == "" })
	if len(words) == 0 {
		return pairs
	}
	return append(pairs, components.Pair{Label: label, Value: strings.Join(words, ", ")})
}
