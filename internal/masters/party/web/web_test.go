package partyweb_test

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	partyweb "github.com/vardhan/internaltools/internal/masters/party/web"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// Screens under test (MST Task 22, CD-50, CD-218, CD-222): /m/party list, record
// and More actions; the create wizard at /m/party/new/{details,addresses,
// contacts,check}; the per-section edit at /m/party/{id}/edit/{slug}, whose
// slugs these tests find from the Change links; address and contact pages and
// actions under the record; the picker at /pick/Party?q=&role=.
//
// Field names are the DocType's Field names. A wizard row posts lines[N].field
// (the kit's rowKey and FieldError.Path), on the Addresses step with Address's
// fields and on the Contacts step with first_name, middle_name, last_name,
// email, mobile and is_primary; Add and Remove post _rows=add and
// _rows=remove:N, as the kit's RowsEditor does.
const (
	base          = "/m/party"
	wizardMain    = base + "/new"
	stepDetails   = base + "/new/details"
	stepAddresses = base + "/new/addresses"
	stepContacts  = base + "/new/contacts"
	stepCheck     = base + "/new/check"
	startOver     = wizardMain + "/start-over"

	panMehta    = "AAGCM1234F"
	gstinMumbai = "27AAGCM1234F1ZS" // PAN AAGCM1234F; on no fixture party (MST-048)
	gstinBLR    = "29AAGCM1234F1ZO"
	gstinShree  = "29AABCS1429B1ZQ" // SHREE's PAN, in a state SHREE has no address in
	gstinBharat = "29AABCT1332L1ZA"
	gstinRajkot = "24AAACR5055K1ZD"

	msgCodeReq    = "Code is required. Enter a short code such as SHREE."                                                                                          // MST-001
	msgCodeForm   = "Code can use letters, digits and . _ / - only, must start with a letter or digit, and be at most 20 characters."                              // MST-004
	msgNameReq    = "Name is required."                                                                                                                            // MST-017
	msgRole       = "Tick Vendor, Customer or both."                                                                                                               // MST-041
	msgPAN        = "PAN must be 10 characters: 5 letters, 4 digits, 1 letter (e.g. AAFCV2449G)."                                                                  // MST-020
	msgCategory   = "GST category must be Registered Regular, Registered Composition, Unregistered, SEZ or Overseas."                                              // CD-166
	msgNoTerms    = "Payment Terms not found. Pick one from the list."                                                                                             // CD-216
	msgOffTerms   = "Payment terms NET30 is disabled. Pick another payment terms."                                                                                 // CD-216
	msgLine1      = "Address line 1 is required."                                                                                                                  // MST-053
	msgCity       = "City is required."                                                                                                                            // MST-053
	msgState      = "State is required for an Indian address."                                                                                                     // MST-053
	msgType       = "Tick Billing, Shipping or both."                                                                                                              // MST-054
	msgPincode    = "Pincode must be 6 digits and can't start with 0."                                                                                             // MST-055
	msgOverseas   = "Addresses outside India must have GST category Overseas."                                                                                     // MST-047
	msgOwner      = "An address must belong to one party or one branch."                                                                                           // MST-067
	msgNeedGSTIN  = "Enter a GSTIN, or set GST category to Unregistered or Overseas."                                                                              // MST-034
	msgUnreg      = "GST category can't be Unregistered when a GSTIN is entered."                                                                                  // MST-034
	msgNeedBill   = "A registered party needs a primary billing address with a GSTIN. Add the billing address first."                                              // CD-180 D1
	msgKeepBill   = "This is the party's primary billing address. Make another billing address primary first."                                                     // CD-177
	msgEnableAddr = "Enable this address before making it primary."                                                                                                // CD-177
	msgTickBill   = "Tick Billing on this address before making it the primary billing address."                                                                   // CD-176
	msgTickShip   = "Tick Shipping on this address before making it the primary shipping address."                                                                 // CD-176
	msgName       = "Contact name is required."                                                                                                                    // MST-062
	msgEmail      = "Enter a valid email address, e.g. accounts@shreesteel.test."                                                                                  // MST-063
	msgMobile     = "Enter a 10-digit Indian mobile number starting 6–9, optionally with +91."                                                                     // MST-064
	msgOneEmail   = "Only one email can be primary."                                                                                                               // MST-065
	msgOnePhone   = "Only one phone can be primary."                                                                                                               // CD-190
	msgEnableCont = "Enable this contact before making it primary."                                                                                                // CD-190
	msgBelong     = "That contact belongs to another party. Pick one of this party's contacts."                                                                    // MST-051
	msgNoContact  = "Contact not found. Pick one from the list."                                                                                                   // CD-190
	msgTooBig     = "This draft is too large to keep. Shorten what you typed, or start over."                                                                      // the kit's
	msgNoCreate   = "You don't have permission to create Party. Ask your administrator for a role that allows it."                                                 // AUTH
	msgNoWrite    = "You don't have permission to change Party. Ask your administrator for a role that allows it."                                                 // AUTH
	staleShree    = "Someone else changed Party SHREE after you opened it. Reload it and make your changes again."                                                 // DOC
	staleMumbai   = "Someone else changed Address Mumbai after you opened it. Reload it and make your changes again."                                              // DOC
	staleThane    = "Someone else changed Address Thane after you opened it. Reload it and make your changes again."                                               // DOC
	staleSunil    = "Someone else changed Contact Sunil Mehta after you opened it. Reload it and make your changes again."                                         // DOC
	usedByBharat  = "GSTIN " + gstinBharat + " is already used by party BHARAT. Use that party instead."                                                           // CD-176
	usedByRajkot  = "GSTIN " + gstinRajkot + " is already used by party RAJKOT. Use that party instead."                                                           // CD-176
	offRajkot     = "GSTIN " + gstinRajkot + " is already used by party RAJKOT (disabled). Enable and use that party instead."                                     // MST-044
	otherPAN      = "GSTIN " + gstinShree + " belongs to PAN AABCS1429B, but this party's PAN is " + panMehta + ". Use a separate party for a different business." // MST-048
	shreePANNote  = "has the same PAN AABCS1429B"                                                                                                                  // MST-045, both warnings
)

// The values of one wizard row. They are shared: use row to change one.
var (
	mumbaiRow = map[string]string{
		"title": "Mumbai", "line1": "Plot 12, MIDC", "line2": "Andheri East", "city": "Mumbai",
		"state_code": "27", "pincode": "400093", "gstin": gstinMumbai, "is_billing": "1",
	}
	blrRow = map[string]string{
		"title": "Bengaluru", "line1": "4 Residency Road", "city": "Bengaluru",
		"state_code": "29", "pincode": "560001", "gstin": gstinBLR, "is_shipping": "1",
	}
	sunilRow = map[string]string{
		"first_name": "Sunil", "middle_name": "K", "last_name": "Mehta",
		"email": "Accounts@MehtaFab.test", "mobile": "98200 12345", "is_primary": "1",
	}
	kiranRow = map[string]string{
		"first_name": "Kiran", "last_name": "Joshi", "email": "kiran@mehtafab.test", "mobile": "9820054321",
	}
)

type world struct {
	h         *webtest.Harness
	parties   *party.Service
	addresses *address.Service
	m         msttest.Masters
}

func mount(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
	return partyweb.Routes(party.New(k), address.New(k), k, sm)
}

// newWorld mounts the screens on the fixture masters.
func newWorld(t *testing.T) *world {
	t.Helper()
	h := webtest.NewSessions(t, mount)
	return &world{h: h, parties: party.New(h.Env.Kit), addresses: address.New(h.Env.Kit), m: msttest.SeedMasters(t, h.Env)}
}

func (w *world) kavya() *webtest.Browser { return w.h.Browser(w.h.Env.Kavya) }

func (w *world) read(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Read(ctx, func(tx *db.Tx) error { return fn(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
}

func (w *world) exec(t *testing.T, query string, args ...any) {
	t.Helper()
	ctx := context.Background()
	if err := w.h.Env.DB.Write(ctx, func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(ctx, query, args...)
		return err
	}); err != nil {
		t.Fatal(err)
	}
}

// count is one number from the database.
func (w *world) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, query, args...).Scan(&n)
	})
	return n
}

func (w *world) byCode(t *testing.T, code string) (party.Party, bool) {
	t.Helper()
	var p party.Party
	var ok bool
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		p, ok, err = w.parties.ByCode(ctx, tx, code)
		return err
	})
	return p, ok
}

func (w *world) get(t *testing.T, code string) party.Party {
	t.Helper()
	p, ok := w.byCode(t, code)
	if !ok {
		t.Fatalf("party %s not found", code)
	}
	return p
}

func (w *world) address(t *testing.T, id int64) address.Address {
	t.Helper()
	var a address.Address
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		a, err = w.addresses.Get(ctx, tx, id)
		return err
	})
	return a
}

func (w *world) contact(t *testing.T, id int64) address.Contact {
	t.Helper()
	var c address.Contact
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		c, err = w.addresses.GetContact(ctx, tx, id)
		return err
	})
	return c
}

// partyAddresses is the party's addresses in the order they were added.
func (w *world) partyAddresses(t *testing.T, id int64) []address.Address {
	t.Helper()
	var out []address.Address
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		var err error
		out, err = w.addresses.ForParty(ctx, tx, id, true)
		return err
	})
	return out
}

// partyContacts is the party's contacts in the order they were added.
func (w *world) partyContacts(t *testing.T, id int64) []address.Contact {
	t.Helper()
	var ids []int64
	w.read(t, func(ctx context.Context, tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id FROM contact WHERE party_id = ? ORDER BY id`, id)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var c int64
			if err := rows.Scan(&c); err != nil {
				return err
			}
			ids = append(ids, c)
		}
		return rows.Err()
	})
	out := make([]address.Contact, 0, len(ids))
	for _, c := range ids {
		out = append(out, w.contact(t, c))
	}
	return out
}

// terms makes payment terms due in full after 30 days.
func (w *world) terms(t *testing.T, code, name string) int64 {
	t.Helper()
	rows, err := paymentterms.ParseRows([]paymentterms.RawRow{{Portion: "100", Days: "30"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := paymentterms.New(w.h.Env.Kit).Create(context.Background(), w.h.Env.Kavya, paymentterms.Input{Code: code, Name: name, Rows: rows})
	if err != nil {
		t.Fatalf("create terms %s: %v", code, err)
	}
	return saved.ID
}

// tally is every row the wizard can write, and the audit entries of its three
// DocTypes.
type tally struct{ parties, addresses, contacts, emails, phones, audits int }

func (w *world) tally(t *testing.T) tally {
	t.Helper()
	return tally{
		parties:   w.count(t, `SELECT count(*) FROM party`),
		addresses: w.count(t, `SELECT count(*) FROM address`),
		contacts:  w.count(t, `SELECT count(*) FROM contact`),
		emails:    w.count(t, `SELECT count(*) FROM contact_email`),
		phones:    w.count(t, `SELECT count(*) FROM contact_phone`),
		audits:    w.count(t, `SELECT count(*) FROM audit_log WHERE doc_type IN ('Party', 'Address', 'Contact')`),
	}
}

func (a tally) plus(b tally) tally {
	return tally{a.parties + b.parties, a.addresses + b.addresses, a.contacts + b.contacts, a.emails + b.emails, a.phones + b.phones, a.audits + b.audits}
}

func (w *world) wantTally(t *testing.T, want tally) {
	t.Helper()
	if got := w.tally(t); got != want {
		t.Errorf("rows and audit entries = %+v; want %+v", got, want)
	}
}

// details is step 1 of a registered vendor.
func details(code string) url.Values {
	return url.Values{
		"code": {code}, "name": {"Mehta Fabricators"}, "is_vendor": {"1"}, "pan": {panMehta},
		"gst_category": {"Registered Regular"}, "default_payment_terms_id": {""},
	}
}

// line is a wizard row's control name.
func line(no int, field string) string { return fmt.Sprintf("lines[%d].%s", no, field) }

// rows is a step's post with these rows, numbered from 1.
func rows(all ...map[string]string) url.Values {
	v := url.Values{}
	for i, r := range all {
		for field, value := range r {
			v.Set(line(i+1, field), value)
		}
	}
	return v
}

// alerts is the banners of the alerts region, where a save's warnings show
// (CD-80).
func alerts(r webtest.Response) []string {
	var out []string
	r.Doc.Find("#alerts .banner").Each(func(_ int, s *goquery.Selection) {
		out = append(out, webtest.Squash(s.Text()))
	})
	return out
}

func wantAlert(t *testing.T, r webtest.Response, msg string) {
	t.Helper()
	got := alerts(r)
	if !slices.ContainsFunc(got, func(s string) bool { return strings.Contains(s, msg) }) {
		t.Errorf("no alert shows %q; alerts: %q", msg, got)
	}
}

// row is r with kv set, field then value; a blank value is a blank box.
func row(r map[string]string, kv ...string) map[string]string {
	out := maps.Clone(r)
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// with is v with kv set, field then value.
func with(v url.Values, kv ...string) url.Values {
	out := url.Values{}
	maps.Copy(out, v)
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

// toCheck walks a browser through the three steps to the check page.
func toCheck(t *testing.T, b *webtest.Browser, d, a, c url.Values) {
	t.Helper()
	webtest.WantRedirect(t, b.Post(t, stepDetails, d), stepAddresses)
	webtest.WantRedirect(t, b.Post(t, stepAddresses, a), stepContacts)
	webtest.WantRedirect(t, b.Post(t, stepContacts, c), stepCheck)
}

func value(t *testing.T, r webtest.Response, name, want string) {
	t.Helper()
	got, ok := r.Value(name)
	if !ok {
		t.Fatalf("no control named %s\ntext: %s", name, r.Text())
	}
	if got != want {
		t.Errorf("%s = %q; want %q", name, got, want)
	}
}

func noControl(t *testing.T, r webtest.Response, name string) {
	t.Helper()
	if r.HasControl(name) {
		t.Errorf("the page has a control named %s; want none\ntext: %s", name, r.Text())
	}
}

// blankRow fails unless name holds nothing: an empty step may show no row or
// one blank row.
func blankRow(t *testing.T, r webtest.Response, name string) {
	t.Helper()
	if got, _ := r.Value(name); got != "" {
		t.Errorf("%s = %q; want no value", name, got)
	}
}

// checked reports whether the checkbox named name is ticked.
func checked(r webtest.Response, name string) bool {
	return r.Doc.Find(`input[type="checkbox"][name="`+name+`"][checked]`).Length() > 0
}

// chosen is the radio group's or select's chosen value.
func chosen(r webtest.Response, name string) string {
	if v, ok := r.Doc.Find(`input[type="radio"][name="` + name + `"][checked]`).Attr("value"); ok {
		return v
	}
	v, _ := r.Value(name)
	return v
}

func wantPage(t *testing.T, r webtest.Response, step string) {
	t.Helper()
	if !strings.Contains(r.Text(), step) {
		t.Errorf("page is not on %q\ntext: %s", step, r.Text())
	}
}

func recordURL(id int64) string { return base + "/" + strconv.FormatInt(id, 10) }

func addressURL(partyID, id int64) string {
	return recordURL(partyID) + "/addresses/" + strconv.FormatInt(id, 10)
}

func contactURL(partyID, id int64) string {
	return recordURL(partyID) + "/contacts/" + strconv.FormatInt(id, 10)
}

func newAddressURL(partyID int64) string { return recordURL(partyID) + "/addresses/new" }

func newContactURL(partyID int64) string { return recordURL(partyID) + "/contacts/new" }
