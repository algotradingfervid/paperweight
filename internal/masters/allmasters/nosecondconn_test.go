package allmasters_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/company"
	"github.com/vardhan/internaltools/internal/masters/employee"
	"github.com/vardhan/internaltools/internal/masters/expensetype"
	"github.com/vardhan/internaltools/internal/masters/gstin"
	"github.com/vardhan/internaltools/internal/masters/item"
	"github.com/vardhan/internaltools/internal/masters/mstkit"
	"github.com/vardhan/internaltools/internal/masters/msttest"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/masters/paymentterms"
	"github.com/vardhan/internaltools/internal/masters/project"
	"github.com/vardhan/internaltools/internal/masters/site"
	"github.com/vardhan/internaltools/internal/masters/taxcode"
	"github.com/vardhan/internaltools/internal/masters/uom"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// CD-213 step 2: every public master write as Kavya, a non-Administrator on the
// real auth.Service, with the pool cut to one connection, so an AUTH read that
// opens its own connection inside the write fails naming the call site. Setup
// runs on the full pool. Every row asserts the write's outcome as well, so a
// row can't pass by refusing early.

const (
	nsCode, nsRecode  = "NS-1", "NS-2"
	nsFirst, nsSecond = "First value", "Second value"

	// nsGSTIN's PAN belongs to no fixture party or branch.
	nsGSTIN = "27AAGCM1234F1ZS"
	nsPAN   = "AAGCM1234F"

	nsCompanyPAN   = "AAFCV2449G"
	nsCompanyGSTIN = "27AAFCV2449G1Z7"

	nsNoChange    = "You don't have permission to change Party. Ask your administrator for a role that allows it."
	nsCodeChange  = "Only a Masters Manager can change a code."                                                      // mstkit
	nsNoProjects  = "You can only work with your assigned projects. Ask your administrator to add this project."     // CD-172
	nsSiteProject = "You can only link sites to your assigned projects. Ask your administrator to add this project." // CD-214
	nsSiteRead    = "This site is on a project you aren't assigned to. Ask your administrator to change it."         // CD-216(6)
	nsSingleton   = "This install supports one company. Add a branch instead."                                       // MST-019
	nsBranchSites = "Disable the branch's sites first: METRO-SITE, PUNE-WH."                                         // MST-108
	nsStockReset  = "Stock UoM changed. Re-enter the other conversions."                                             // MST-089
	nsItemDraft   = "Cannot delete Item WIRE-18G: it is used in Draft %d. Disable it instead."                       // MST-014
	nsTaxInUse    = "Cannot delete Tax Code GST-TEST: it is used in Expense Type TRAVEL. Disable it instead."        // CD-167
)

// nsChain is the audit trail of nsCRUD's chain row; an employee's disable and
// enable are saves of its status.
var (
	nsChain         = []string{"create", "save", "disable", "enable", "delete"}
	nsEmployeeChain = []string{"create", "save", "save", "save", "delete"}
)

// nsWorld is the canonical masters as seen by Kavya on the real auth.Service.
type nsWorld struct {
	*msttest.Env
	m   msttest.Masters
	svc *allmasters.Services
}

func nsWrap(t *testing.T, env *msttest.Env, m msttest.Masters, svc *allmasters.Services) *nsWorld {
	t.Helper()
	if slices.Contains(env.Kavya.Roles, auth.RoleAdministrator) {
		t.Fatal("Kavya is an Administrator: Can would return before reading anything")
	}
	if _, ok := env.Kit.Auth.(*auth.Auth); !ok {
		t.Fatalf("Kit.Auth is %T, want the real *auth.Auth", env.Kit.Auth)
	}
	return &nsWorld{Env: env, m: m, svc: svc}
}

func nsNew(t *testing.T) *nsWorld {
	t.Helper()
	env, m, svc := seeded(t)
	return nsWrap(t, env, m, svc)
}

// nsBare is a world with no company, for the first save and for setup.
func nsBare(t *testing.T) *nsWorld {
	t.Helper()
	env := msttest.New(t)
	return nsWrap(t, env, msttest.Masters{}, allmasters.New(env.Kit))
}

// do runs fn on the pool of one, for a call that has to succeed.
func (w *nsWorld) do(t *testing.T, fn func(ctx context.Context) error) {
	t.Helper()
	if err := testkit.NoSecondConn(t, w.DB, fn); err != nil {
		t.Fatalf("got %v, want ok", err)
	}
}

// save is do for a call that returns what it saved.
func (w *nsWorld) save(t *testing.T, fn func(ctx context.Context) (mstkit.Saved, error)) mstkit.Saved {
	t.Helper()
	var saved mstkit.Saved
	w.do(t, func(ctx context.Context) (err error) {
		saved, err = fn(ctx)
		return err
	})
	return saved
}

// inTx runs fn inside one write on the pool of one, as a caller's tx does.
func (w *nsWorld) inTx(t *testing.T, fn func(ctx context.Context, tx *db.Tx) error) error {
	t.Helper()
	return testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
		return w.DB.Write(ctx, func(tx *db.Tx) error { return fn(ctx, tx) })
	})
}

// wantStored fails unless the row's column reads want.
func (w *nsWorld) wantStored(t *testing.T, table string, id int64, column, want string) {
	t.Helper()
	var got string
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT CAST(`+column+` AS TEXT) FROM `+table+` WHERE id = ?`, id).Scan(&got) //nolint:gosec // G202: fixed test names
	})
	if got != want {
		t.Errorf("%s %d: %s = %q, want %q", table, id, column, got, want)
	}
}

func (w *nsWorld) wantVersion(t *testing.T, saved mstkit.Saved, want int) {
	t.Helper()
	if saved.Version != want {
		t.Errorf("Saved = %+v, want version %d", saved, want)
	}
}

// restrict limits u to the given ids of dim.
func (w *nsWorld) restrict(t *testing.T, u auth.Principal, dim auth.Dimension, ids ...int64) {
	t.Helper()
	a, ok := w.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", w.Kit.Auth)
	}
	if err := a.ChangeRestrictions(t.Context(), w.Admin, u.ID, dim, ids, nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
}

// grant gives the role rights on docType, read first as the matrix requires.
func (w *nsWorld) grant(t *testing.T, role, docType string, rights ...auth.Right) {
	t.Helper()
	a, ok := w.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", w.Kit.Auth)
	}
	set := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
	for _, r := range rights {
		set[r] = auth.ScopeAll
	}
	if err := a.SetRights(t.Context(), w.Admin, role, docType, set); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
}

// nsWantText fails unless err matches target (errors.Is) and has exactly msg.
func nsWantText(t *testing.T, what string, err, target error, msg string) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s: err = %v (%T), want errors.Is %v", what, err, err, target)
		return
	}
	if err.Error() != msg {
		t.Errorf("%s: message = %q, want %q", what, err.Error(), msg)
	}
}

// nsRow is one call or chain of calls. bare rows get nsBare's world.
type nsRow struct {
	name string
	bare bool
	run  func(t *testing.T, w *nsWorld)
}

// nsService is what every master's service offers; contacts come through
// nsContacts.
type nsService[In any] interface {
	Create(ctx context.Context, p auth.Principal, in In) (mstkit.Saved, error)
	CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in In) (mstkit.Saved, error)
	Update(ctx context.Context, p auth.Principal, id int64, version int, in In) (mstkit.Saved, error)
	UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in In) (mstkit.Saved, error)
	SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error
	SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error
	Delete(ctx context.Context, p auth.Principal, id int64, version int) error
	DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error
}

// nsContacts gives address.Service's contact methods nsService's names.
type nsContacts struct{ s *address.Service }

func (c nsContacts) Create(ctx context.Context, p auth.Principal, in address.ContactInput) (mstkit.Saved, error) {
	return c.s.CreateContact(ctx, p, in)
}

func (c nsContacts) CreateTx(ctx context.Context, tx *db.Tx, p auth.Principal, in address.ContactInput) (mstkit.Saved, error) {
	return c.s.CreateContactTx(ctx, tx, p, in)
}

func (c nsContacts) Update(ctx context.Context, p auth.Principal, id int64, version int, in address.ContactInput) (mstkit.Saved, error) {
	return c.s.UpdateContact(ctx, p, id, version, in)
}

func (c nsContacts) UpdateTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, in address.ContactInput) (mstkit.Saved, error) {
	return c.s.UpdateContactTx(ctx, tx, p, id, version, in)
}

func (c nsContacts) SetDisabled(ctx context.Context, p auth.Principal, id int64, version int, disabled bool) error {
	return c.s.SetContactDisabled(ctx, p, id, version, disabled)
}

func (c nsContacts) SetDisabledTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int, disabled bool) error {
	return c.s.SetContactDisabledTx(ctx, tx, p, id, version, disabled)
}

func (c nsContacts) Delete(ctx context.Context, p auth.Principal, id int64, version int) error {
	return c.s.DeleteContact(ctx, p, id, version)
}

func (c nsContacts) DeleteTx(ctx context.Context, tx *db.Tx, p auth.Principal, id int64, version int) error {
	return c.s.DeleteContactTx(ctx, tx, p, id, version)
}

// nsCRUD is the rows every master shares: Create, Update, Update with a code
// change, SetDisabled both ways, Delete, and one chain of the Tx twins inside a
// single caller's write. in builds an input; column holds its value.
func nsCRUD[In any](k mstkit.Kind, column string, chain []string, svc func(*nsWorld) nsService[In], in func(w *nsWorld, code, value string) In) []nsRow {
	stored := func(t *testing.T, w *nsWorld, id int64, disabled string, version int) {
		t.Helper()
		w.wantStored(t, k.Table, id, "disabled", disabled)
		w.wantStored(t, k.Table, id, "version", fmt.Sprint(version))
	}
	existing := func(t *testing.T, w *nsWorld) mstkit.Saved {
		t.Helper()
		saved, err := svc(w).Create(t.Context(), w.Kavya, in(w, nsCode, nsFirst))
		if err != nil {
			t.Fatalf("setup Create: %v", err)
		}
		return saved
	}
	rows := []nsRow{
		{name: "create", run: func(t *testing.T, w *nsWorld) {
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return svc(w).Create(ctx, w.Kavya, in(w, nsCode, nsFirst))
			})
			w.wantVersion(t, saved, 1)
			w.wantStored(t, k.Table, saved.ID, column, nsFirst)
		}},
		{name: "update", run: func(t *testing.T, w *nsWorld) {
			old := existing(t, w)
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return svc(w).Update(ctx, w.Kavya, old.ID, old.Version, in(w, nsCode, nsSecond))
			})
			w.wantVersion(t, saved, old.Version+1)
			w.wantStored(t, k.Table, old.ID, column, nsSecond)
		}},
		{name: "disable", run: func(t *testing.T, w *nsWorld) {
			old := existing(t, w)
			w.do(t, func(ctx context.Context) error { return svc(w).SetDisabled(ctx, w.Kavya, old.ID, old.Version, true) })
			stored(t, w, old.ID, "1", old.Version+1)
		}},
		{name: "enable", run: func(t *testing.T, w *nsWorld) {
			old := existing(t, w)
			if err := svc(w).SetDisabled(t.Context(), w.Kavya, old.ID, old.Version, true); err != nil {
				t.Fatalf("setup SetDisabled: %v", err)
			}
			w.do(t, func(ctx context.Context) error { return svc(w).SetDisabled(ctx, w.Kavya, old.ID, old.Version+1, false) })
			stored(t, w, old.ID, "0", old.Version+2)
		}},
		{name: "delete", run: func(t *testing.T, w *nsWorld) {
			old := existing(t, w)
			w.do(t, func(ctx context.Context) error { return svc(w).Delete(ctx, w.Kavya, old.ID, old.Version) })
			if n := rowCount(t, w.Env, k.Table, old.ID); n != 0 {
				t.Errorf("%d rows after the delete, want 0", n)
			}
		}},
		{name: "chain in one tx", run: func(t *testing.T, w *nsWorld) {
			var id int64
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				saved, err := svc(w).CreateTx(ctx, tx, w.Kavya, in(w, nsCode, nsFirst))
				if err != nil {
					return fmt.Errorf("CreateTx: %w", err)
				}
				id = saved.ID
				code := nsCode
				if k.HasCode {
					code = nsRecode
				}
				if saved, err = svc(w).UpdateTx(ctx, tx, w.Kavya, id, saved.Version, in(w, code, nsSecond)); err != nil {
					return fmt.Errorf("UpdateTx: %w", err)
				}
				if err := svc(w).SetDisabledTx(ctx, tx, w.Kavya, id, saved.Version, true); err != nil {
					return fmt.Errorf("SetDisabledTx: %w", err)
				}
				if err := svc(w).SetDisabledTx(ctx, tx, w.Kavya, id, saved.Version+1, false); err != nil {
					return fmt.Errorf("SetDisabledTx enable: %w", err)
				}
				if err := svc(w).DeleteTx(ctx, tx, w.Kavya, id, saved.Version+2); err != nil {
					return fmt.Errorf("DeleteTx: %w", err)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("got %v, want ok", err)
			}
			var got []string
			for _, e := range auditEntries(t, w.Env, k.DocType, id) {
				got = append(got, e.Action)
			}
			if !slices.Equal(got, chain) {
				t.Errorf("audit trail = %q, want %q", got, chain)
			}
			if n := rowCount(t, w.Env, k.Table, id); n != 0 {
				t.Errorf("%d rows after the delete, want 0", n)
			}
		}},
	}
	if k.HasCode {
		rows = append(rows, nsRow{name: "update with a code change", run: func(t *testing.T, w *nsWorld) {
			old := existing(t, w)
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return svc(w).Update(ctx, w.Kavya, old.ID, old.Version, in(w, nsRecode, nsFirst))
			})
			w.wantVersion(t, saved, old.Version+1)
			w.wantStored(t, k.Table, old.ID, "code", nsRecode)
		}})
	}
	for i := range rows {
		rows[i].name = k.Table + "/" + rows[i].name
	}
	return rows
}

// The inputs nsCRUD saves. Addresses and contacts hang off a fixture party.
func nsPartyInput(_ *nsWorld, code, value string) party.Input {
	return party.Input{Code: code, Name: value, IsVendor: true}
}

func nsAddressInput(w *nsWorld, _, value string) address.Input {
	return address.Input{PartyID: w.m.Parties["SHREE"], Title: value, Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001", IsShipping: true}
}

func nsContactInput(w *nsWorld, _, value string) address.ContactInput {
	return address.ContactInput{PartyID: w.m.Parties["SHREE"], FirstName: value}
}

func nsBranchInput(_ *nsWorld, code, value string) company.BranchInput {
	return company.BranchInput{Code: code, Name: value, StateCode: "27"}
}

func nsItemInput(w *nsWorld, code, value string) item.Input {
	return item.Input{Code: code, Name: value, Type: item.TypeGoods, HSNSAC: "7216", StockUoMID: w.m.UoMs["KGS"], DefaultTaxCodeID: w.m.TaxCodes["GST18"], IsStock: true}
}

func nsUoMInput(_ *nsWorld, code, value string) uom.Input {
	return uom.Input{Code: code, Name: value, UQC: "NOS"}
}

func nsTaxInput(_ *nsWorld, code, value string) taxcode.Input {
	return taxcode.Input{Code: code, Name: value, Treatment: taxcode.Taxable, Rate: gst12}
}

func nsExpenseInput(_ *nsWorld, code, value string) expensetype.Input {
	return expensetype.Input{Code: code, Name: value}
}

func nsTermsInput(_ *nsWorld, code, value string) paymentterms.Input {
	return paymentterms.Input{Code: code, Name: value, Rows: []paymentterms.Row{{Portion: 100_000, Days: 30}}}
}

func nsProjectInput(w *nsWorld, code, value string) project.Input {
	return project.Input{Code: code, Name: value, BranchID: w.m.Branches["PUNE"]}
}

func nsSiteInput(w *nsWorld, code, value string) site.Input {
	return site.Input{Code: code, Name: value, BranchID: w.m.Branches["PUNE"], Type: site.Warehouse}
}

// nsEmployeeInput carries a relieving date because SetDisabled keeps the stored
// one and a Left employee needs it (MST-118).
func nsEmployeeInput(w *nsWorld, code, value string) employee.Input {
	return employee.Input{
		Code: code, Name: value, BranchID: w.m.Branches["PUNE"], Joining: orderDate,
		Relieving: types.Date{Year: 2026, Month: 10, Day: 25}, Status: employee.Active,
	}
}

func nsAllCRUD() []nsRow {
	return slices.Concat(
		nsCRUD(party.Kind, "name", nsChain, func(w *nsWorld) nsService[party.Input] { return w.svc.Party }, nsPartyInput),
		nsCRUD(address.Kind, "title", nsChain, func(w *nsWorld) nsService[address.Input] { return w.svc.Address }, nsAddressInput),
		nsCRUD(address.ContactKind, "first_name", nsChain, func(w *nsWorld) nsService[address.ContactInput] { return nsContacts{w.svc.Address} }, nsContactInput),
		nsCRUD(company.Kind, "name", nsChain, func(w *nsWorld) nsService[company.BranchInput] { return w.svc.Company }, nsBranchInput),
		nsCRUD(item.Kind, "name", nsChain, func(w *nsWorld) nsService[item.Input] { return w.svc.Item }, nsItemInput),
		nsCRUD(uom.Kind, "name", nsChain, func(w *nsWorld) nsService[uom.Input] { return w.svc.UoM }, nsUoMInput),
		nsCRUD(taxcode.Kind, "name", nsChain, func(w *nsWorld) nsService[taxcode.Input] { return w.svc.TaxCode }, nsTaxInput),
		nsCRUD(expensetype.Kind, "name", nsChain, func(w *nsWorld) nsService[expensetype.Input] { return w.svc.ExpenseType }, nsExpenseInput),
		nsCRUD(paymentterms.Kind, "name", nsChain, func(w *nsWorld) nsService[paymentterms.Input] { return w.svc.Terms }, nsTermsInput),
		nsCRUD(project.Kind, "name", nsChain, func(w *nsWorld) nsService[project.Input] { return w.svc.Project }, nsProjectInput),
		nsCRUD(site.Kind, "name", nsChain, func(w *nsWorld) nsService[site.Input] { return w.svc.Site }, nsSiteInput),
		nsCRUD(employee.Kind, "name", nsEmployeeChain, func(w *nsWorld) nsService[employee.Input] { return w.svc.Employee }, nsEmployeeInput),
	)
}

// nsVendor is a new Unregistered vendor, whose addresses need no GSTIN.
func (w *nsWorld) nsVendor(t *testing.T) int64 {
	t.Helper()
	saved, err := w.svc.Party.Create(t.Context(), w.Kavya, party.Input{Code: "NS-VENDOR", Name: "Vendor", IsVendor: true})
	if err != nil {
		t.Fatalf("setup vendor: %v", err)
	}
	return saved.ID
}

// nsAddress adds a billing and shipping address to the party.
func (w *nsWorld) nsAddress(t *testing.T, partyID int64, title string) mstkit.Saved {
	t.Helper()
	saved, err := w.svc.Address.Create(t.Context(), w.Kavya, address.Input{
		PartyID: partyID, Title: title, Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001", IsBilling: true, IsShipping: true,
	})
	if err != nil {
		t.Fatalf("setup address %s: %v", title, err)
	}
	return saved
}

// nsContact adds a contact to the party.
func (w *nsWorld) nsContact(t *testing.T, partyID int64, name string, primary bool) mstkit.Saved {
	t.Helper()
	saved, err := w.svc.Address.CreateContact(t.Context(), w.Kavya, address.ContactInput{PartyID: partyID, FirstName: name, Primary: primary})
	if err != nil {
		t.Fatalf("setup contact %s: %v", name, err)
	}
	return saved
}

// nsWantPrimary fails unless the two addresses hold the column as given.
func (w *nsWorld) nsWantPrimary(t *testing.T, column string, set, cleared int64) {
	t.Helper()
	w.wantStored(t, address.Kind.Table, set, column, "1")
	w.wantStored(t, address.Kind.Table, cleared, column, "0")
}

// nsSetPrimary makes a second address of a vendor primary, inside a caller's tx.
func nsSetPrimary(column string, billing bool) func(t *testing.T, w *nsWorld) {
	return func(t *testing.T, w *nsWorld) {
		vendor := w.nsVendor(t)
		first, second := w.nsAddress(t, vendor, "First"), w.nsAddress(t, vendor, "Second")
		w.wantStored(t, address.Kind.Table, first.ID, column, "1")
		err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
			return w.svc.Address.SetPrimaryTx(ctx, tx, w.Kavya, second.ID, billing, second.Version)
		})
		if err != nil {
			t.Fatalf("SetPrimaryTx: %v", err)
		}
		w.nsWantPrimary(t, column, second.ID, first.ID)
	}
}

// nsAddressRows are the address and contact writes beyond CRUD.
func nsAddressRows() []nsRow {
	return []nsRow{
		{name: "address/set primary billing", run: nsSetPrimary("is_primary_billing", true)},
		{name: "address/set primary shipping", run: nsSetPrimary("is_primary_shipping", false)},
		{name: "address/set primary on a branch", run: func(t *testing.T, w *nsWorld) {
			pune := w.m.BranchAddresses["PUNE"]
			second, err := w.svc.Address.Create(t.Context(), w.Kavya, address.Input{
				BranchID: w.m.Branches["PUNE"], Title: "Second", Line1: "1 Main Road", City: "Pune", StateCode: "27", Pincode: "411019", IsBilling: true,
			})
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			err = w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				return w.svc.Address.SetPrimaryTx(ctx, tx, w.Kavya, second.ID, true, second.Version)
			})
			if err != nil {
				t.Fatalf("SetPrimaryTx: %v", err)
			}
			w.nsWantPrimary(t, "is_primary_billing", second.ID, pune)
		}},
		{name: "address/disable a primary clears it", run: func(t *testing.T, w *nsWorld) {
			first := w.nsAddress(t, w.nsVendor(t), "First")
			w.wantStored(t, address.Kind.Table, first.ID, "is_primary_billing", "1")
			w.do(t, func(ctx context.Context) error {
				return w.svc.Address.SetDisabled(ctx, w.Kavya, first.ID, first.Version, true)
			})
			w.wantStored(t, address.Kind.Table, first.ID, "disabled", "1")
			w.wantStored(t, address.Kind.Table, first.ID, "is_primary_billing", "0")
		}},
		{name: "address/create applies the GSTIN to its party", run: func(t *testing.T, w *nsWorld) {
			vendor := w.nsVendor(t)
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Address.Create(ctx, w.Kavya, address.Input{
					PartyID: vendor, Title: "Registered", Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001",
					GSTIN: nsGSTIN, IsBilling: true, PrimaryBilling: true,
				})
			})
			w.wantStored(t, address.Kind.Table, saved.ID, "gstin", nsGSTIN)
			if got := getParty(t, w.Env, w.svc, vendor); got.PAN != nsPAN || got.GSTCategory != gstin.Regular {
				t.Errorf("party = PAN %q category %q, want %q %q", got.PAN, got.GSTCategory, nsPAN, gstin.Regular)
			}
		}},
		{name: "contact/create as primary replaces the previous", run: func(t *testing.T, w *nsWorld) {
			vendor := w.nsVendor(t)
			first := w.nsContact(t, vendor, "First", true)
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Address.CreateContact(ctx, w.Kavya, address.ContactInput{PartyID: vendor, FirstName: "Second", Primary: true})
			})
			w.wantStored(t, address.ContactKind.Table, saved.ID, "is_primary", "1")
			w.wantStored(t, address.ContactKind.Table, first.ID, "is_primary", "0")
		}},
		{name: "contact/update as primary replaces the previous", run: func(t *testing.T, w *nsWorld) {
			vendor := w.nsVendor(t)
			first, second := w.nsContact(t, vendor, "First", true), w.nsContact(t, vendor, "Second", false)
			w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Address.UpdateContact(ctx, w.Kavya, second.ID, second.Version, address.ContactInput{PartyID: vendor, FirstName: "Second", Primary: true})
			})
			w.wantStored(t, address.ContactKind.Table, second.ID, "is_primary", "1")
			w.wantStored(t, address.ContactKind.Table, first.ID, "is_primary", "0")
		}},
		{name: "contact/set primary", run: func(t *testing.T, w *nsWorld) {
			vendor := w.nsVendor(t)
			first, second := w.nsContact(t, vendor, "First", true), w.nsContact(t, vendor, "Second", false)
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				return w.svc.Address.SetPrimaryContactTx(ctx, tx, w.Kavya, vendor, second.ID)
			})
			if err != nil {
				t.Fatalf("SetPrimaryContactTx: %v", err)
			}
			w.wantStored(t, address.ContactKind.Table, second.ID, "is_primary", "1")
			w.wantStored(t, address.ContactKind.Table, first.ID, "is_primary", "0")
		}},
		{name: "contact/disable a primary clears it", run: func(t *testing.T, w *nsWorld) {
			c := w.nsContact(t, w.nsVendor(t), "Only", true)
			w.do(t, func(ctx context.Context) error {
				return w.svc.Address.SetContactDisabled(ctx, w.Kavya, c.ID, c.Version, true)
			})
			w.wantStored(t, address.ContactKind.Table, c.ID, "disabled", "1")
			w.wantStored(t, address.ContactKind.Table, c.ID, "is_primary", "0")
		}},
		{name: "contact/on a branch", run: func(t *testing.T, w *nsWorld) {
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Address.CreateContact(ctx, w.Kavya, address.ContactInput{BranchID: w.m.Branches["PUNE"], FirstName: "Branch"})
			})
			w.wantStored(t, address.ContactKind.Table, saved.ID, "branch_id", fmt.Sprint(w.m.Branches["PUNE"]))
		}},
	}
}

// nsCompany reads the company.
func (w *nsWorld) nsCompany(t *testing.T) company.Company {
	t.Helper()
	var c company.Company
	var ok bool
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
		c, ok, err = w.svc.Company.Company(ctx, tx)
		return err
	})
	if !ok {
		t.Fatal("no company stored")
	}
	return c
}

// nsCompanyRows are the singleton's writes and the branch writes beyond CRUD.
func nsCompanyRows() []nsRow {
	return []nsRow{
		{name: "company/first save", bare: true, run: func(t *testing.T, w *nsWorld) {
			w.do(t, func(ctx context.Context) error {
				return w.svc.Company.SaveCompany(ctx, w.Kavya, company.CompanyInput{LegalName: nsFirst, PAN: nsCompanyPAN}, 0)
			})
			if got := w.nsCompany(t); got.LegalName != nsFirst || got.PAN != nsCompanyPAN || got.Version != 1 {
				t.Errorf("company = %+v, want %q %q at version 1", got, nsFirst, nsCompanyPAN)
			}
		}},
		{name: "company/save", run: func(t *testing.T, w *nsWorld) {
			c := w.nsCompany(t)
			w.do(t, func(ctx context.Context) error {
				return w.svc.Company.SaveCompany(ctx, w.Kavya, company.CompanyInput{LegalName: nsSecond, PAN: c.PAN}, c.Version)
			})
			if got := w.nsCompany(t); got.LegalName != nsSecond || got.Version != c.Version+1 {
				t.Errorf("company = %+v, want %q at version %d", got, nsSecond, c.Version+1)
			}
		}},
		{name: "company/save in a caller's tx", run: func(t *testing.T, w *nsWorld) {
			c := w.nsCompany(t)
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				return w.svc.Company.SaveCompanyTx(ctx, tx, w.Kavya, company.CompanyInput{LegalName: nsSecond, PAN: c.PAN}, c.Version)
			})
			if err != nil {
				t.Fatalf("SaveCompanyTx: %v", err)
			}
			if got := w.nsCompany(t); got.LegalName != nsSecond {
				t.Errorf("company = %+v, want %q", got, nsSecond)
			}
		}},
		{name: "company/setup", bare: true, run: func(t *testing.T, w *nsWorld) {
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				return w.svc.Company.SetupTx(ctx, tx, w.Kavya, company.SetupInput{LegalName: nsFirst, StateCode: "27", GSTIN: nsCompanyGSTIN, BranchCode: "PUNE"})
			})
			if err != nil {
				t.Fatalf("SetupTx: %v", err)
			}
			if got := w.nsCompany(t); got.PAN != nsCompanyPAN {
				t.Errorf("company = %+v, want PAN %q from the GSTIN", got, nsCompanyPAN)
			}
			var b company.Branch
			var ok bool
			read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
				b, ok, err = w.svc.Company.ByCode(ctx, tx, "PUNE")
				return err
			})
			if !ok || !b.IsHeadOffice {
				t.Errorf("branch PUNE = %+v (found %v), want the head office", b, ok)
			}
		}},
		{name: "company/delete is refused", run: func(t *testing.T, w *nsWorld) {
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				return w.svc.Company.DeleteCompanyTx(ctx, tx, w.Kavya, 1)
			})
			wantRefused(t, err, nsSingleton)
		}},
		{name: "branch/new head office takes over", run: func(t *testing.T, w *nsWorld) {
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Company.Create(ctx, w.Kavya, company.BranchInput{Code: nsCode, Name: nsFirst, StateCode: "27", IsHeadOffice: true})
			})
			w.wantStored(t, company.Kind.Table, saved.ID, "is_head_office", "1")
			w.wantStored(t, company.Kind.Table, w.m.Branches["PUNE"], "is_head_office", "0")
		}},
		{name: "branch/disable is refused with enabled sites", run: func(t *testing.T, w *nsWorld) {
			id := w.m.Branches["PUNE"]
			version := rowVersion(t, w.Env, company.Kind.Table, id)
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return w.svc.Company.SetDisabled(ctx, w.Kavya, id, version, true)
			})
			wantRefused(t, err, nsBranchSites)
			w.wantStored(t, company.Kind.Table, id, "disabled", "0")
		}},
	}
}

// nsEmployeeLeave relieves Deepak, who has a user and no reports, on the pool
// of one.
func (w *nsWorld) nsEmployeeLeave(t *testing.T) (id int64, version int) {
	t.Helper()
	id = w.m.Employees["EMP-007"]
	var e employee.Employee
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
		e, err = w.svc.Employee.Get(ctx, tx, id)
		return err
	})
	saved, err := w.svc.Employee.Update(t.Context(), w.Kavya, id, e.Version, employee.Input{
		Code: e.Code, Name: e.Name, BranchID: e.BranchID, UserID: e.UserID, ManagerID: e.ManagerID,
		Joining: e.Joining, Relieving: types.Date{Year: 2026, Month: 10, Day: 25}, Status: e.Status,
	})
	if err != nil {
		t.Fatalf("setup relieving date: %v", err)
	}
	return id, saved.Version
}

func (w *nsWorld) nsUserEnabled(t *testing.T, u auth.UserID) bool {
	t.Helper()
	var info auth.UserInfo
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
		info, _, err = w.Kit.Auth.User(ctx, tx, u)
		return err
	})
	return info.Enabled
}

// nsOtherRows are the employee, item, project and site writes beyond CRUD.
func nsOtherRows() []nsRow {
	const deepak = auth.UserID(7)
	return []nsRow{
		{name: "employee/leaving disables the linked user", run: func(t *testing.T, w *nsWorld) {
			id, version := w.nsEmployeeLeave(t)
			w.do(t, func(ctx context.Context) error { return w.svc.Employee.SetDisabled(ctx, w.Kavya, id, version, true) })
			w.wantStored(t, employee.Kind.Table, id, "status", string(employee.Left))
			if w.nsUserEnabled(t, deepak) {
				t.Error("Deepak's user is still enabled")
			}
		}},
		{name: "employee/re-joining enables the linked user", run: func(t *testing.T, w *nsWorld) {
			id, version := w.nsEmployeeLeave(t)
			if err := w.svc.Employee.SetDisabled(t.Context(), w.Kavya, id, version, true); err != nil {
				t.Fatalf("setup leave: %v", err)
			}
			w.do(t, func(ctx context.Context) error { return w.svc.Employee.SetDisabled(ctx, w.Kavya, id, version+1, false) })
			w.wantStored(t, employee.Kind.Table, id, "status", string(employee.Active))
			if !w.nsUserEnabled(t, deepak) {
				t.Error("Deepak's user is still disabled")
			}
		}},
		{name: "item/update adds a conversion", run: func(t *testing.T, w *nsWorld) {
			old := createItem(t, w.Env, w.svc, nsItemInput(w, nsCode, nsFirst))
			in := nsItemInput(w, nsCode, nsFirst)
			factor := 10 * uom.FactorOne
			in.Conversions = []item.Conversion{{UoMID: w.m.UoMs["BOX"], Factor: factor}}
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Item.Update(ctx, w.Kavya, old.ID, old.Version, in)
			})
			w.wantVersion(t, saved, old.Version+1)
			var got []item.Conversion
			read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
				got, err = w.svc.Item.Conversions(ctx, tx, old.ID)
				return err
			})
			want := []item.Conversion{{UoMID: w.m.UoMs["BOX"], Factor: factor}, {UoMID: w.m.UoMs["KGS"], Factor: uom.FactorOne}}
			if !slices.Equal(got, want) {
				t.Errorf("Conversions = %+v, want %+v", got, want)
			}
		}},
		{name: "item/stock UoM change resets the conversions", run: func(t *testing.T, w *nsWorld) {
			in := nsItemInput(w, nsCode, nsFirst)
			in.Conversions = []item.Conversion{{UoMID: w.m.UoMs["BOX"], Factor: 10 * uom.FactorOne}}
			old := createItem(t, w.Env, w.svc, in)
			in.Conversions, in.StockUoMID = nil, w.m.UoMs["MTR"]
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
				return w.svc.Item.Update(ctx, w.Kavya, old.ID, old.Version, in)
			})
			if !slices.Equal(saved.Warnings, []string{nsStockReset}) {
				t.Errorf("Warnings = %q, want %q", saved.Warnings, nsStockReset)
			}
			w.wantStored(t, item.Kind.Table, old.ID, "stock_uom_id", fmt.Sprint(w.m.UoMs["MTR"]))
		}},
		{name: "item/delete is refused while a draft uses it", run: func(t *testing.T, w *nsWorld) {
			const code = "WIRE-18G"
			draft := order(t, w.Env, w.m, code)
			id := w.m.Items[code]
			version := rowVersion(t, w.Env, item.Kind.Table, id)
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return w.svc.Item.Delete(ctx, w.Kavya, id, version)
			})
			wantRefused(t, err, fmt.Sprintf(nsItemDraft, draft))
			if n := rowCount(t, w.Env, item.Kind.Table, id); n != 1 {
				t.Errorf("%d rows after the refusal, want 1", n)
			}
		}},
		{name: "taxcode/delete is refused while a master links to it", run: func(t *testing.T, w *nsWorld) {
			tax, err := w.svc.TaxCode.Create(t.Context(), w.Kavya, nsTaxInput(w, "GST-TEST", nsFirst))
			if err != nil {
				t.Fatalf("setup tax code: %v", err)
			}
			id := w.m.ExpenseTypes["TRAVEL"]
			version := rowVersion(t, w.Env, expensetype.Kind.Table, id)
			if _, err := w.svc.ExpenseType.Update(t.Context(), w.Kavya, id, version, expensetype.Input{Code: "TRAVEL", Name: "Travel", DefaultTaxCodeID: tax.ID}); err != nil {
				t.Fatalf("setup expense type: %v", err)
			}
			err = testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return w.svc.TaxCode.Delete(ctx, w.Kavya, tax.ID, tax.Version)
			})
			wantRefused(t, err, nsTaxInUse)
		}},
		{name: "project/create with a customer", run: func(t *testing.T, w *nsWorld) {
			customer := othersCustomer(t, w.Env, w.svc, "NS-CUST")
			in := nsProjectInput(w, nsCode, nsFirst)
			in.CustomerID = customer
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) { return w.svc.Project.Create(ctx, w.Kavya, in) })
			w.wantStored(t, project.Kind.Table, saved.ID, "customer_id", fmt.Sprint(customer))
		}},
		{name: "site/create on a project", run: func(t *testing.T, w *nsWorld) {
			in := nsSiteInput(w, nsCode, nsFirst)
			in.ProjectID = w.m.Projects["PRJ-METRO"]
			saved := w.save(t, func(ctx context.Context) (mstkit.Saved, error) { return w.svc.Site.Create(ctx, w.Kavya, in) })
			w.wantStored(t, site.Kind.Table, saved.ID, "project_id", fmt.Sprint(in.ProjectID))
		}},
	}
}

// nsRefusalRows are record-level checks that say no inside the write: the row
// stays as it was, and the text is the producer's.
func nsRefusalRows() []nsRow {
	return []nsRow{
		{name: "refused/a reader inside the write", run: func(t *testing.T, w *nsWorld) {
			id := w.m.Parties["SHREE"]
			var was party.Party
			read(t, w.Env, func(ctx context.Context, tx *db.Tx) (err error) {
				was, err = w.svc.Party.Get(ctx, tx, id)
				return err
			})
			in := partyInput(was)
			in.Name = nsSecond
			err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
				_, err := w.svc.Party.UpdateTx(ctx, tx, w.Priya, id, was.Version, in)
				return err
			})
			nsWantText(t, "UpdateTx as Priya", err, auth.ErrForbidden, nsNoChange)
			w.wantStored(t, party.Kind.Table, id, "name", was.Name)
		}},
		{name: "refused/code change without the role", run: func(t *testing.T, w *nsWorld) {
			w.grant(t, "Purchase User", uom.DocType, auth.RightWrite)
			id := w.m.UoMs["BOX"]
			version := rowVersion(t, w.Env, uom.Kind.Table, id)
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				_, err := w.svc.UoM.Update(ctx, w.Priya, id, version, uom.Input{Code: nsRecode, Name: "Box", UQC: "BOX"})
				return err
			})
			if !hasCodeError(err, nsCodeChange) {
				t.Errorf("Update as Priya = %v, want code error %q", err, nsCodeChange)
			}
			w.wantStored(t, uom.Kind.Table, id, "code", "BOX")
		}},
		{name: "refused/project outside the restriction", run: func(t *testing.T, w *nsWorld) {
			w.restrict(t, w.Kavya, auth.DimProject, w.m.Projects["PRJ-METRO"])
			id := w.m.Projects["PRJ-TECHPARK"]
			version := rowVersion(t, w.Env, project.Kind.Table, id)
			in := nsProjectInput(w, "PRJ-TECHPARK", nsSecond)
			in.BranchID = w.m.Branches["BLR"]
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				_, err := w.svc.Project.Update(ctx, w.Kavya, id, version, in)
				return err
			})
			nsWantText(t, "Update of a hidden project", err, auth.ErrNotFound, auth.MsgNotFound)
			if got := rowVersion(t, w.Env, project.Kind.Table, id); got != version {
				t.Errorf("version = %d after the refusal, want %d", got, version)
			}
		}},
		{name: "refused/site outside the restriction", run: func(t *testing.T, w *nsWorld) {
			w.restrict(t, w.Kavya, auth.DimSite, w.m.Sites["PUNE-WH"])
			id := w.m.Sites["TECH-SITE"]
			version := rowVersion(t, w.Env, site.Kind.Table, id)
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return w.svc.Site.SetDisabled(ctx, w.Kavya, id, version, true)
			})
			nsWantText(t, "SetDisabled of a hidden site", err, auth.ErrNotFound, auth.MsgNotFound)
			w.wantStored(t, site.Kind.Table, id, "disabled", "0")
		}},
		{name: "refused/new project while restricted", run: func(t *testing.T, w *nsWorld) {
			w.restrict(t, w.Kavya, auth.DimProject, w.m.Projects["PRJ-METRO"])
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				_, err := w.svc.Project.Create(ctx, w.Kavya, nsProjectInput(w, nsCode, nsFirst))
				return err
			})
			nsWantText(t, "Create while restricted", err, auth.ErrForbidden, nsNoProjects)
		}},
		{name: "refused/site on another project", run: func(t *testing.T, w *nsWorld) {
			w.restrict(t, w.Kavya, auth.DimProject, w.m.Projects["PRJ-METRO"])
			in := nsSiteInput(w, nsCode, nsFirst)
			in.BranchID, in.ProjectID = w.m.Branches["BLR"], w.m.Projects["PRJ-TECHPARK"]
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				_, err := w.svc.Site.Create(ctx, w.Kavya, in)
				return err
			})
			nsWantText(t, "Create on another project", err, auth.ErrForbidden, nsSiteProject)
		}},
		{name: "refused/site read-only on another project", run: func(t *testing.T, w *nsWorld) {
			w.restrict(t, w.Kavya, auth.DimProject, w.m.Projects["PRJ-METRO"])
			id := w.m.Sites["TECH-SITE"]
			version := rowVersion(t, w.Env, site.Kind.Table, id)
			err := testkit.NoSecondConn(t, w.DB, func(ctx context.Context) error {
				return w.svc.Site.Delete(ctx, w.Kavya, id, version)
			})
			nsWantText(t, "Delete on another project", err, auth.ErrForbidden, nsSiteRead)
			if n := rowCount(t, w.Env, site.Kind.Table, id); n != 1 {
				t.Errorf("%d rows after the refusal, want 1", n)
			}
		}},
	}
}

// hasCodeError reports a field error on code with exactly msg.
func hasCodeError(err error, msg string) bool {
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) || len(ve.Fields) != 1 {
		return false
	}
	return ve.Fields[0].Field == "code" && ve.Fields[0].Message == msg
}

// nsAcross writes through nine masters in one caller's tx, the way a WP3 flow
// or an importer reaches another master's Tx methods.
func nsAcross(t *testing.T, w *nsWorld) {
	var ids struct{ party, address, contact, uom, item, project, site, employee, expense int64 }
	err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
		p := w.Kavya
		steps := []func() (err error){
			func() (err error) {
				saved, err := w.svc.Party.CreateTx(ctx, tx, p, nsPartyInput(w, nsCode, nsFirst))
				ids.party = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.Address.CreateTx(ctx, tx, p, address.Input{
					PartyID: ids.party, Title: nsFirst, Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001",
					GSTIN: nsGSTIN, IsBilling: true, PrimaryBilling: true,
				})
				ids.address = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.Address.CreateContactTx(ctx, tx, p, address.ContactInput{PartyID: ids.party, FirstName: nsFirst, Primary: true})
				ids.contact = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.UoM.CreateTx(ctx, tx, p, nsUoMInput(w, nsCode, nsFirst))
				ids.uom = saved.ID
				return err
			},
			func() (err error) {
				in := nsItemInput(w, nsCode, nsFirst)
				in.StockUoMID = ids.uom
				saved, err := w.svc.Item.CreateTx(ctx, tx, p, in)
				ids.item = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.Project.CreateTx(ctx, tx, p, nsProjectInput(w, nsCode, nsFirst))
				ids.project = saved.ID
				return err
			},
			func() (err error) {
				in := nsSiteInput(w, nsCode, nsFirst)
				in.ProjectID = ids.project
				saved, err := w.svc.Site.CreateTx(ctx, tx, p, in)
				ids.site = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.Employee.CreateTx(ctx, tx, p, nsEmployeeInput(w, nsCode, nsFirst))
				ids.employee = saved.ID
				return err
			},
			func() (err error) {
				saved, err := w.svc.ExpenseType.CreateTx(ctx, tx, p, nsExpenseInput(w, nsCode, nsFirst))
				ids.expense = saved.ID
				return err
			},
		}
		for i, step := range steps {
			if err := step(); err != nil {
				return fmt.Errorf("step %d: %w", i, err)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("got %v, want ok", err)
	}
	if got := getParty(t, w.Env, w.svc, ids.party); got.PAN != nsPAN {
		t.Errorf("party PAN = %q, want %q from the address written in the same tx", got.PAN, nsPAN)
	}
	if got := getItem(t, w.Env, w.svc, ids.item); got.StockUoMID != ids.uom {
		t.Errorf("item stock UoM = %d, want the UoM written in the same tx (%d)", got.StockUoMID, ids.uom)
	}
	w.wantStored(t, site.Kind.Table, ids.site, "project_id", fmt.Sprint(ids.project))
	for _, c := range []struct {
		table string
		id    int64
	}{
		{address.Kind.Table, ids.address}, {address.ContactKind.Table, ids.contact},
		{employee.Kind.Table, ids.employee}, {expensetype.Kind.Table, ids.expense},
	} {
		if n := rowCount(t, w.Env, c.table, c.id); n != 1 {
			t.Errorf("%s %d: %d rows, want 1", c.table, c.id, n)
		}
	}
}

// Rows run in parallel: each has its own database, each failure costs
// NoSecondConn's whole deadline, and no row here calls RegisterImporters.
func TestNoSecondConn_Masters(t *testing.T) {
	rows := slices.Concat(nsAllCRUD(), nsAddressRows(), nsCompanyRows(), nsOtherRows(), nsRefusalRows(),
		[]nsRow{{name: "across masters in one tx", run: nsAcross}})
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if r.bare {
				r.run(t, nsBare(t))
				return
			}
			r.run(t, nsNew(t))
		})
	}
}

// ApplyGSTINTx is called by the address package with the caller's tx.
func TestNoSecondConn_PartyApplyGSTIN(t *testing.T) {
	w := nsNew(t)
	vendor := w.nsVendor(t)
	err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
		return w.svc.Party.ApplyGSTINTx(ctx, tx, w.Kavya, vendor, nsGSTIN)
	})
	if err != nil {
		t.Fatalf("ApplyGSTINTx: %v", err)
	}
	if got := getParty(t, w.Env, w.svc, vendor); got.PAN != nsPAN || got.GSTCategory != gstin.Regular {
		t.Errorf("party = PAN %q category %q, want %q %q", got.PAN, got.GSTCategory, nsPAN, gstin.Regular)
	}
}

// nsImports is one file per registered importer: two rows, the second disabled
// where the template has that column, so the RowFunc reaches SetDisabledTx too.
// rows is a WHERE that matches both rows with their cells read back; off the
// disabled one.
func nsImports() []struct{ docType, table, body, rows, off string } {
	return []struct{ docType, table, body, rows, off string }{
		{
			docType: party.DocType, table: party.Kind.Table,
			body: partiesCSV(partiesPartyHeader, "NS-P1,Vendor One,Yes,No,,Unregistered,,No", "NS-P2,Vendor Two,Yes,No,,Unregistered,,Yes"),
			rows: "(code = 'NS-P1' AND name = 'Vendor One') OR (code = 'NS-P2' AND name = 'Vendor Two')",
			off:  "code = 'NS-P2' AND disabled = 1",
		},
		{
			docType: address.DocType, table: address.Kind.Table,
			body: partiesCSV(partiesAddressHeader,
				partiesAddressRow("SHREE", "Kolhapur", "Maharashtra", "416001", "", "No", "Yes", "No"),
				partiesAddressRow("SHREE", "Nashik", "Maharashtra", "422001", "", "No", "Yes", "Yes")),
			rows: "title IN ('Kolhapur', 'Nashik') AND pincode IN ('416001', '422001')",
			off:  "title = 'Nashik' AND disabled = 1",
		},
		{
			docType: address.ContactDocType, table: address.ContactKind.Table,
			body: partiesCSV(partiesContactHeader, "SHREE,Asha,,Rao,asha@vardhan.test,9876543210,No", "SHREE,Vikram,,Das,,,No"),
			rows: "(first_name = 'Asha' AND last_name = 'Rao') OR (first_name = 'Vikram' AND last_name = 'Das')",
		},
		{
			docType: uom.DocType, table: uom.Kind.Table,
			body: othersCSV(othersUoMHeader, "NS-U1,Unit One,No,NOS,No", "NS-U2,Unit Two,No,NOS,Yes"),
			rows: "(code = 'NS-U1' AND name = 'Unit One') OR (code = 'NS-U2' AND name = 'Unit Two')",
			off:  "code = 'NS-U2' AND disabled = 1",
		},
		{
			docType: taxcode.DocType, table: taxcode.Kind.Table,
			body: othersCSV(othersTaxHeader, "NS-T1,Tax One,Taxable,18,No", "NS-T2,Tax Two,Taxable,12,Yes"),
			rows: "(code = 'NS-T1' AND name = 'Tax One') OR (code = 'NS-T2' AND name = 'Tax Two')",
			off:  "code = 'NS-T2' AND disabled = 1",
		},
		{
			docType: item.DocType, table: item.Kind.Table,
			body: othersCSV(othersItemHeader, "NS-I1,Item One,Goods,KGS,7216,GST18,62.5,No", "NS-I2,Item Two,Goods,KGS,7216,GST18,62.5,Yes"),
			rows: "(code = 'NS-I1' AND name = 'Item One') OR (code = 'NS-I2' AND name = 'Item Two')",
			off:  "code = 'NS-I2' AND disabled = 1",
		},
		{
			docType: expensetype.DocType, table: expensetype.Kind.Table,
			body: othersCSV(othersExpenseHeader, "NS-E1,Expense One,,,No", "NS-E2,Expense Two,,,Yes"),
			rows: "(code = 'NS-E1' AND name = 'Expense One') OR (code = 'NS-E2' AND name = 'Expense Two')",
			off:  "code = 'NS-E2' AND disabled = 1",
		},
		{
			docType: project.DocType, table: project.Kind.Table,
			body: othersCSV(othersProjectHeader, "NS-P1,Project One,PUNE,,,,Open,No", "NS-P2,Project Two,PUNE,,,,Open,Yes"),
			rows: "(code = 'NS-P1' AND name = 'Project One') OR (code = 'NS-P2' AND name = 'Project Two')",
			off:  "code = 'NS-P2' AND disabled = 1",
		},
		{
			docType: site.DocType, table: site.Kind.Table,
			body: othersCSV(othersSiteHeader, "NS-S1,Site One,PUNE,warehouse,,No", "NS-S2,Site Two,PUNE,site,PRJ-METRO,Yes"),
			rows: "(code = 'NS-S1' AND name = 'Site One') OR (code = 'NS-S2' AND name = 'Site Two')",
			off:  "code = 'NS-S2' AND disabled = 1",
		},
		{
			docType: employee.DocType, table: employee.Kind.Table,
			body: othersCSV(othersEmployeeHeader, othersEmployeeRow("NS-E1", "Employee One", "", ""), othersEmployeeRow("NS-E2", "Employee Two", "", "NS-E1")),
			rows: "(code = 'NS-E1' AND name = 'Employee One') OR (code = 'NS-E2' AND name = 'Employee Two')",
		},
	}
}

// nsCount counts table's rows matching where.
func nsCount(t *testing.T, env *msttest.Env, table, where string) int {
	t.Helper()
	var n int
	read(t, env, func(ctx context.Context, tx *db.Tx) error {
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM `+table+` WHERE `+where).Scan(&n) //nolint:gosec // G202: fixed test SQL
	})
	return n
}

// nsReport runs Import or Preview as Kavya on the pool of one.
func (w *nsWorld) nsReport(t *testing.T, run func(context.Context, auth.Principal) (csvio.Report, error)) csvio.Report {
	t.Helper()
	var rep csvio.Report
	w.do(t, func(ctx context.Context) (err error) {
		rep, err = run(ctx, w.Kavya)
		return err
	})
	if len(rep.Errors) != 0 {
		t.Fatalf("row errors: %+v", rep.Errors)
	}
	return rep
}

// Every registered importer, Preview and then Import, through csvio with the
// create check and the RowFunc in one write. Not parallel: RegisterImporters'
// Services are process-wide.
func TestNoSecondConn_Importers(t *testing.T) {
	rows := nsImports()
	covered := make([]string, len(rows))
	for i, c := range rows {
		covered[i] = c.docType
		t.Run(c.docType, func(t *testing.T) {
			w := nsNew(t)
			allmasters.RegisterImporters(w.svc)
			im := othersImporter(w.Env)
			dt := registered(t, c.docType)
			t.Run("preview", func(t *testing.T) {
				rep := w.nsReport(t, func(ctx context.Context, p auth.Principal) (csvio.Report, error) {
					return im.Preview(ctx, p, dt, strings.NewReader(c.body))
				})
				if rep.Rows != 2 || rep.Ready != 2 || rep.Imported != 0 {
					t.Errorf("Rows, Ready, Imported = %d, %d, %d; want 2, 2, 0", rep.Rows, rep.Ready, rep.Imported)
				}
				if n := nsCount(t, w.Env, c.table, c.rows); n != 0 {
					t.Errorf("%d rows stored by Preview, want 0", n)
				}
			})
			t.Run("import", func(t *testing.T) {
				rep := w.nsReport(t, func(ctx context.Context, p auth.Principal) (csvio.Report, error) {
					return im.Import(ctx, p, dt, strings.NewReader(c.body))
				})
				if rep.Rows != 2 || rep.Imported != 2 {
					t.Errorf("Rows, Imported = %d, %d; want 2, 2", rep.Rows, rep.Imported)
				}
				if n := nsCount(t, w.Env, c.table, c.rows); n != 2 {
					t.Errorf("%d rows read back, want 2", n)
				}
				if c.off != "" && nsCount(t, w.Env, c.table, c.off) != 1 {
					t.Errorf("row %q was not stored disabled", c.off)
				}
			})
		})
	}
	slices.Sort(covered)
	want := slices.Clone(nsImporterDocTypes)
	slices.Sort(want)
	if !slices.Equal(covered, want) {
		t.Errorf("rows cover %q, want every importer %q", covered, want)
	}
}

// nsImporterDocTypes names the DocTypes of importers_test.go's importedMasters,
// which that internal test checks against csvio's registry.
var nsImporterDocTypes = []string{
	party.DocType, address.DocType, address.ContactDocType, uom.DocType, taxcode.DocType,
	item.DocType, expensetype.DocType, project.DocType, site.DocType, employee.DocType,
}
