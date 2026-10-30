package allmasters_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/csvio"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/list"
	"github.com/vardhan/internaltools/internal/masters/address"
	"github.com/vardhan/internaltools/internal/masters/allmasters"
	"github.com/vardhan/internaltools/internal/masters/allmasters/webtest"
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
)

// CD-223 and CD-224 over every master, on the real auth.Service: own scope
// (a row's created_by decides), the name length limit, and the rights each Tx
// twin checks for itself.

const (
	pgMine, pgTheirs, pgNobody = "Mine Row", "Their Row", "Nobody Row"
	pgChanged                  = "Changed Row"
	pgRole                     = "Own Masters"
	pgLimit                    = 140
	pgDeepak, pgRavi           = auth.UserID(7), auth.UserID(3) // restricted on nothing
)

// pgOps are one master's calls with its input type erased; value lands in the
// master's name column, or the field the row says.
type pgOps struct {
	create        func(ctx context.Context, p auth.Principal, code, value string) (mstkit.Saved, error)
	update        func(ctx context.Context, p auth.Principal, s mstkit.Saved, code, value string) (mstkit.Saved, error)
	setDisabled   func(ctx context.Context, p auth.Principal, s mstkit.Saved, disabled bool) error
	del           func(ctx context.Context, p auth.Principal, s mstkit.Saved) error
	createTx      func(ctx context.Context, tx *db.Tx, p auth.Principal, code, value string) error
	updateTx      func(ctx context.Context, tx *db.Tx, p auth.Principal, s mstkit.Saved, code, value string) error
	setDisabledTx func(ctx context.Context, tx *db.Tx, p auth.Principal, s mstkit.Saved, disabled bool) error
	deleteTx      func(ctx context.Context, tx *db.Tx, p auth.Principal, s mstkit.Saved) error
}

// pgMaster is one master. base is its screens' path; "" means they aren't on
// this branch (Party, Address and Contact, AD-2's lane), so the kernel's list
// and export are called directly.
type pgMaster struct {
	k    mstkit.Kind
	base string
	ops  func(w *nsWorld) pgOps
}

func pgOf[In any](k mstkit.Kind, base string, svc func(*nsWorld) nsService[In], in func(w *nsWorld, code, value string) In) pgMaster {
	return pgMaster{k: k, base: base, ops: func(w *nsWorld) pgOps {
		s := svc(w)
		return pgOps{
			create: func(ctx context.Context, p auth.Principal, code, value string) (mstkit.Saved, error) {
				return s.Create(ctx, p, in(w, code, value))
			},
			update: func(ctx context.Context, p auth.Principal, o mstkit.Saved, code, value string) (mstkit.Saved, error) {
				return s.Update(ctx, p, o.ID, o.Version, in(w, code, value))
			},
			setDisabled: func(ctx context.Context, p auth.Principal, o mstkit.Saved, disabled bool) error {
				return s.SetDisabled(ctx, p, o.ID, o.Version, disabled)
			},
			del: func(ctx context.Context, p auth.Principal, o mstkit.Saved) error {
				return s.Delete(ctx, p, o.ID, o.Version)
			},
			createTx: func(ctx context.Context, tx *db.Tx, p auth.Principal, code, value string) error {
				_, err := s.CreateTx(ctx, tx, p, in(w, code, value))
				return err
			},
			updateTx: func(ctx context.Context, tx *db.Tx, p auth.Principal, o mstkit.Saved, code, value string) error {
				_, err := s.UpdateTx(ctx, tx, p, o.ID, o.Version, in(w, code, value))
				return err
			},
			setDisabledTx: func(ctx context.Context, tx *db.Tx, p auth.Principal, o mstkit.Saved, disabled bool) error {
				return s.SetDisabledTx(ctx, tx, p, o.ID, o.Version, disabled)
			},
			deleteTx: func(ctx context.Context, tx *db.Tx, p auth.Principal, o mstkit.Saved) error {
				return s.DeleteTx(ctx, tx, p, o.ID, o.Version)
			},
		}
	}}
}

func pgMasters() []pgMaster {
	return []pgMaster{
		pgOf(party.Kind, "", func(w *nsWorld) nsService[party.Input] { return w.svc.Party }, nsPartyInput),
		pgOf(address.Kind, "", func(w *nsWorld) nsService[address.Input] { return w.svc.Address }, nsAddressInput),
		pgOf(address.ContactKind, "", func(w *nsWorld) nsService[address.ContactInput] { return nsContacts{w.svc.Address} }, nsContactInput),
		pgOf(company.Kind, "/m/branch", func(w *nsWorld) nsService[company.BranchInput] { return w.svc.Company }, nsBranchInput),
		pgOf(item.Kind, "/m/item", func(w *nsWorld) nsService[item.Input] { return w.svc.Item }, nsItemInput),
		pgOf(uom.Kind, "/m/uom", func(w *nsWorld) nsService[uom.Input] { return w.svc.UoM }, nsUoMInput),
		pgOf(taxcode.Kind, "/m/tax-code", func(w *nsWorld) nsService[taxcode.Input] { return w.svc.TaxCode }, nsTaxInput),
		pgOf(expensetype.Kind, "/m/expense-type", func(w *nsWorld) nsService[expensetype.Input] { return w.svc.ExpenseType }, nsExpenseInput),
		pgOf(paymentterms.Kind, "/m/payment-terms", func(w *nsWorld) nsService[paymentterms.Input] { return w.svc.Terms }, nsTermsInput),
		pgOf(project.Kind, "/m/project", func(w *nsWorld) nsService[project.Input] { return w.svc.Project }, nsProjectInput),
		pgOf(site.Kind, "/m/site", func(w *nsWorld) nsService[site.Input] { return w.svc.Site }, nsSiteInput),
		pgOf(employee.Kind, "/m/employee", func(w *nsWorld) nsService[employee.Input] { return w.svc.Employee }, nsEmployeeInput),
	}
}

// pgScreens is nsNew with every master's screens mounted.
func pgScreens(t *testing.T) (*nsWorld, *webtest.Harness) {
	t.Helper()
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	return nsWrap(t, h.Env, msttest.SeedMasters(t, h.Env), allmasters.New(h.Env.Kit)), h
}

func (w *nsWorld) pgAuth(t *testing.T) *auth.Auth {
	t.Helper()
	a, ok := w.Kit.Auth.(*auth.Auth)
	if !ok {
		t.Fatalf("Kit.Auth is %T", w.Kit.Auth)
	}
	return a
}

// pgUser creates pgRole with the rights on docType and returns user id holding
// only it. AUTH decides from a Principal's roles, as a session loads them.
func (w *nsWorld) pgUser(t *testing.T, id auth.UserID, docType string, rights map[auth.Right]auth.Scope) auth.Principal {
	t.Helper()
	if err := w.pgAuth(t).CreateRole(t.Context(), w.Admin, pgRole); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, pgRole, docType, rights); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	return auth.Principal{ID: id, Name: "Scoped", Roles: []string{pgRole}}
}

func pgOwnRights() map[auth.Right]auth.Scope {
	return map[auth.Right]auth.Scope{
		auth.RightRead: auth.ScopeOwn, auth.RightCreate: auth.ScopeOwn, auth.RightWrite: auth.ScopeOwn,
		auth.RightDelete: auth.ScopeOwn, auth.RightExport: auth.ScopeOwn,
	}
}

// pgNoCreator makes the row look seeded by a migration.
func (w *nsWorld) pgNoCreator(t *testing.T, table string, id int64) {
	t.Helper()
	err := w.DB.Write(t.Context(), func(tx *db.Tx) error {
		_, err := tx.SQL().ExecContext(t.Context(), `UPDATE `+table+` SET created_by = NULL WHERE id = ?`, id) //nolint:gosec // G202: fixed test table names
		return err
	})
	if err != nil {
		t.Fatalf("clear created_by: %v", err)
	}
}

func pgCreate(t *testing.T, ops pgOps, p auth.Principal, code, value string) mstkit.Saved {
	t.Helper()
	saved, err := ops.create(t.Context(), p, code, value)
	if err != nil {
		t.Fatalf("create %s as user %d: %v", code, p.ID, err)
	}
	return saved
}

func pgRecord(base string, s mstkit.Saved) string { return base + "/" + strconv.FormatInt(s.ID, 10) }

func pgVersion(s mstkit.Saved) url.Values { return url.Values{"version": {strconv.Itoa(s.Version)}} }

// pgSurfaces is what p's list, picker and export show of the rows called Row.
func (m pgMaster) pgSurfaces(t *testing.T, w *nsWorld, h *webtest.Harness, p auth.Principal) (lst, pick, export string, pickIDs []string) {
	t.Helper()
	if m.base == "" {
		ctx := t.Context()
		ls, dt := &list.Service{DB: w.DB, Auth: w.Kit.Auth}, registered(t, m.k.DocType)
		page, err := ls.List(ctx, p, dt, list.Query{Page: 1, PageSize: 25})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		var names []string
		for _, r := range page.Rows {
			names = append(names, r.Values["name"].(string))
		}
		picked, err := ls.Picker(ctx, p, dt, "Row", 25)
		if err != nil {
			t.Fatalf("picker: %v", err)
		}
		for _, r := range picked.Rows {
			pickIDs = append(pickIDs, strconv.FormatInt(r.ID, 10))
			pick += " " + r.Values["name"].(string)
		}
		var buf bytes.Buffer
		if _, err := csvio.NewExporter(w.DB, w.Kit.Auth).Export(ctx, &buf, p, dt, list.Query{}); err != nil {
			t.Fatalf("export: %v", err)
		}
		return strings.Join(names, " "), pick, buf.String(), pickIDs
	}
	listed := h.Get(t, p, m.base)
	webtest.WantStatus(t, listed, http.StatusOK)
	picked := h.GetHTMX(t, p, "/pick/"+url.PathEscape(m.k.DocType)+"?q=Row")
	webtest.WantStatus(t, picked, http.StatusOK)
	for _, r := range picked.PickRows() {
		pickIDs = append(pickIDs, r.Value)
		pick += " " + r.Text
	}
	exported := h.Get(t, p, m.base+"/export.csv")
	webtest.WantStatus(t, exported, http.StatusOK)
	return listed.Text(), pick, exported.Body, pickIDs
}

// TestCD223_OwnScope: a user with own-scope rights opens, lists, picks, exports
// and changes the row they created; a row someone else made, or a migration
// made (created_by NULL), is absent from all four and every write is AUTH's 404.
func TestCD223_OwnScope(t *testing.T) {
	for _, m := range pgMasters() {
		t.Run(m.k.Table, func(t *testing.T) {
			w, h := pgScreens(t)
			ops, ctx := m.ops(w), t.Context()
			own := w.pgUser(t, pgDeepak, m.k.DocType, pgOwnRights())
			mine := pgCreate(t, ops, own, "OWN-1", pgMine)
			theirs := pgCreate(t, ops, w.Kavya, "OTH-1", pgTheirs)
			nobody := pgCreate(t, ops, w.Kavya, "NUL-1", pgNobody)
			w.pgNoCreator(t, m.k.Table, nobody.ID)

			lst, pick, export, ids := m.pgSurfaces(t, w, h, own)
			for name, got := range map[string]string{"list": lst, "picker": pick, "export": export} {
				if !strings.Contains(got, pgMine) {
					t.Errorf("%s lacks the user's own row: %s", name, got)
				}
				if strings.Contains(got, pgTheirs) || strings.Contains(got, pgNobody) {
					t.Errorf("%s shows a row the user didn't create: %s", name, got)
				}
			}
			if want := strconv.FormatInt(mine.ID, 10); !slices.Equal(ids, []string{want}) {
				t.Errorf("picker ids = %q, want %q", ids, want)
			}

			if m.base != "" {
				page := h.Get(t, own, pgRecord(m.base, mine))
				webtest.WantStatus(t, page, http.StatusOK)
				webtest.WantText(t, page, pgMine)
				webtest.WantText(t, page, "Disable")
				webtest.WantText(t, page, "Delete")
				confirm := h.Get(t, own, pgRecord(m.base, mine)+"?delete")
				if _, ok := confirm.FormValue(pgRecord(m.base, mine)+"/delete", "version"); !ok {
					t.Errorf("the owner's confirm step has no delete form\ntext: %s", confirm.Text())
				}
			}

			for _, other := range []struct {
				code  string
				saved mstkit.Saved
			}{{"OTH-1", theirs}, {"NUL-1", nobody}} {
				if m.base != "" {
					webtest.WantStatus(t, h.Get(t, own, pgRecord(m.base, other.saved)), http.StatusNotFound)
					webtest.WantStatus(t, h.Post(t, own, pgRecord(m.base, other.saved)+"/disable", pgVersion(other.saved)), http.StatusNotFound)
					webtest.WantStatus(t, h.Post(t, own, pgRecord(m.base, other.saved)+"/delete", pgVersion(other.saved)), http.StatusNotFound)
				}
				if _, err := ops.update(ctx, own, other.saved, other.code, pgChanged); !errors.Is(err, auth.ErrNotFound) {
					t.Errorf("update of %s = %v, want not found", other.code, err)
				}
				if err := ops.setDisabled(ctx, own, other.saved, true); !errors.Is(err, auth.ErrNotFound) {
					t.Errorf("disable of %s = %v, want not found", other.code, err)
				}
				if err := ops.del(ctx, own, other.saved); !errors.Is(err, auth.ErrNotFound) {
					t.Errorf("delete of %s = %v, want not found", other.code, err)
				}
				w.wantStored(t, m.k.Table, other.saved.ID, "version", strconv.Itoa(other.saved.Version))
			}

			saved, err := ops.update(ctx, own, mine, "OWN-1", pgChanged)
			if err != nil {
				t.Fatalf("update of the user's own row: %v", err)
			}
			if err := ops.setDisabled(ctx, own, saved, true); err != nil {
				t.Fatalf("disable of the user's own row: %v", err)
			}
			saved.Version++
			if m.base != "" {
				webtest.WantStatus(t, h.Post(t, own, pgRecord(m.base, saved)+"/delete", pgVersion(saved)), http.StatusSeeOther)
			} else if err := ops.del(ctx, own, saved); err != nil {
				t.Fatalf("delete of the user's own row: %v", err)
			}
			if n := rowCount(t, w.Env, m.k.Table, mine.ID); n != 0 {
				t.Errorf("%d rows after the owner's delete, want 0", n)
			}
		})
	}
}

// TestCD223_OwnWriteOfAReadableRow: with read on everything, own-scope write
// and delete give AUTH's own-scope refusal on another's row and work on the
// user's own, and the record page offers only what the service would allow.
func TestCD223_OwnWriteOfAReadableRow(t *testing.T) {
	for _, m := range pgMasters() {
		t.Run(m.k.Table, func(t *testing.T) {
			w, h := pgScreens(t)
			ops, ctx := m.ops(w), t.Context()
			p := w.pgUser(t, pgDeepak, m.k.DocType, map[auth.Right]auth.Scope{
				auth.RightRead: auth.ScopeAll, auth.RightCreate: auth.ScopeAll, auth.RightWrite: auth.ScopeOwn, auth.RightDelete: auth.ScopeOwn,
			})
			mine := pgCreate(t, ops, p, "OWN-1", pgMine)
			theirs := pgCreate(t, ops, w.Kavya, "OTH-1", pgTheirs)

			if m.base != "" {
				page := h.Get(t, p, pgRecord(m.base, theirs))
				webtest.WantStatus(t, page, http.StatusOK)
				webtest.WantNoText(t, page, "Disable")
				webtest.WantNoText(t, page, "Delete")
				webtest.WantStatus(t, h.Post(t, p, pgRecord(m.base, theirs)+"/disable", pgVersion(theirs)), http.StatusForbidden)
				webtest.WantText(t, h.Get(t, p, pgRecord(m.base, mine)), "Disable")
			}
			_, upd := ops.update(ctx, p, theirs, "OTH-1", pgChanged)
			wantWrite := w.Kit.Auth.Can(ctx, p, m.k.DocType, auth.RightWrite, &auth.RecordRef{DocType: m.k.DocType, ID: theirs.ID})
			if !errors.Is(upd, auth.ErrForbidden) || upd.Error() != wantWrite.Error() {
				t.Errorf("update of another's row = %v, want %v", upd, wantWrite)
			}
			del := ops.del(ctx, p, theirs)
			wantDelete := w.Kit.Auth.Can(ctx, p, m.k.DocType, auth.RightDelete, &auth.RecordRef{DocType: m.k.DocType, ID: theirs.ID})
			if !errors.Is(del, auth.ErrForbidden) || del.Error() != wantDelete.Error() {
				t.Errorf("delete of another's row = %v, want %v", del, wantDelete)
			}
			w.wantStored(t, m.k.Table, theirs.ID, "version", strconv.Itoa(theirs.Version))
			if _, err := ops.update(ctx, p, mine, "OWN-1", pgChanged); err != nil {
				t.Errorf("update of the user's own row: %v", err)
			}
		})
	}
}

// The form's save reaches the same check as the service (mstweb's checkSubmit).
func TestCD223_OwnScopeSaveThroughTheForm(t *testing.T) {
	w, h := pgScreens(t)
	m := pgOf(uom.Kind, "/m/uom", func(w *nsWorld) nsService[uom.Input] { return w.svc.UoM }, nsUoMInput)
	ops := m.ops(w)
	own := w.pgUser(t, pgDeepak, uom.DocType, pgOwnRights())
	mine := pgCreate(t, ops, own, "OWN-1", pgMine)
	theirs := pgCreate(t, ops, w.Kavya, "OTH-1", pgTheirs)

	form := func(code string, s mstkit.Saved) url.Values {
		v := pgVersion(s)
		v.Set("code", code)
		v.Set("name", pgChanged)
		v.Set("uqc", "NOS")
		return v
	}
	if !h.Get(t, own, pgRecord(m.base, mine)).HasControl("name") {
		t.Error("the owner's record page shows values, not the form")
	}
	webtest.WantRedirect(t, h.Post(t, own, pgRecord(m.base, mine), form("OWN-1", mine)), pgRecord(m.base, mine))
	w.wantStored(t, uom.Kind.Table, mine.ID, "name", pgChanged)
	webtest.WantStatus(t, h.Post(t, own, pgRecord(m.base, theirs), form("OTH-1", theirs)), http.StatusNotFound)
	w.wantStored(t, uom.Kind.Table, theirs.ID, "name", pgTheirs)
}

// A section's edit page checks the right on the row before showing the form
// (mstweb's checkSubmit, which the service doesn't reach on a GET).
func TestCD223_OwnScopeSectionPage(t *testing.T) {
	w, h := pgScreens(t)
	m := pgOf(employee.Kind, "/m/employee", func(w *nsWorld) nsService[employee.Input] { return w.svc.Employee }, nsEmployeeInput)
	ops := m.ops(w)
	own := w.pgUser(t, pgDeepak, employee.DocType, pgOwnRights())
	mine := pgCreate(t, ops, own, "OWN-1", pgMine)
	theirs := pgCreate(t, ops, w.Kavya, "OTH-1", pgTheirs)
	webtest.WantStatus(t, h.Get(t, own, pgRecord(m.base, mine)+"/edit/details"), http.StatusOK)
	webtest.WantStatus(t, h.Get(t, own, pgRecord(m.base, theirs)+"/edit/details"), http.StatusNotFound)
}

// ApplyGSTINTx has no check of its own: the address twin is its gate, so a user
// who may change the address and has no right on Party still back-fills it
// (MST-049).
func TestCD223_AddressWriteBackFillsPartyWithoutPartyRights(t *testing.T) {
	w := nsNew(t)
	ctx := t.Context()
	vendor := w.nsVendor(t)
	old := w.nsAddress(t, vendor, "First")
	p := w.pgUser(t, pgDeepak, address.DocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll, auth.RightWrite: auth.ScopeAll})
	if err := w.Kit.Auth.Can(ctx, p, party.DocType, auth.RightRead, nil); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("the user reads Party: %v", err)
	}
	w.save(t, func(ctx context.Context) (mstkit.Saved, error) {
		return w.svc.Address.Update(ctx, p, old.ID, old.Version, address.Input{
			PartyID: vendor, Title: "First", Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001",
			GSTIN: nsGSTIN, IsBilling: true, IsShipping: true, PrimaryBilling: true,
		})
	})
	if got := getParty(t, w.Env, w.svc, vendor); got.PAN != nsPAN || got.GSTCategory != gstin.Regular {
		t.Errorf("party = PAN %q category %q, want %q %q", got.PAN, got.GSTCategory, nsPAN, gstin.Regular)
	}
}

// The SetPrimary twins check the row, not only the kind: an own-scope user moves
// the primary to their own row, and another's row is AUTH's 404.
func TestCD223_OwnScopeSetPrimary(t *testing.T) {
	w := nsNew(t)
	vendor := w.nsVendor(t)
	own := w.pgUser(t, pgDeepak, address.DocType, pgOwnRights())
	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, pgRole, address.ContactDocType, pgOwnRights()); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	w.nsAddress(t, vendor, "First")
	theirs := w.nsAddress(t, vendor, "Their")
	w.nsContact(t, vendor, "First", true)
	theirContact := w.nsContact(t, vendor, "Their", false)
	mine, err := w.svc.Address.Create(t.Context(), own, address.Input{
		PartyID: vendor, Title: pgMine, Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001", IsBilling: true,
	})
	if err != nil {
		t.Fatalf("own address: %v", err)
	}
	myContact, err := w.svc.Address.CreateContact(t.Context(), own, address.ContactInput{PartyID: vendor, FirstName: pgMine})
	if err != nil {
		t.Fatalf("own contact: %v", err)
	}

	for what, err := range map[string]error{
		"address": w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
			return w.svc.Address.SetPrimaryTx(ctx, tx, own, theirs.ID, true, theirs.Version)
		}),
		"contact": w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
			return w.svc.Address.SetPrimaryContactTx(ctx, tx, own, vendor, theirContact.ID)
		}),
	} {
		if !errors.Is(err, auth.ErrNotFound) {
			t.Errorf("%s of another user = %v, want not found", what, err)
		}
	}
	if err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
		return w.svc.Address.SetPrimaryTx(ctx, tx, own, mine.ID, true, mine.Version)
	}); err != nil {
		t.Errorf("own address: %v", err)
	}
	if err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
		return w.svc.Address.SetPrimaryContactTx(ctx, tx, own, vendor, myContact.ID)
	}); err != nil {
		t.Errorf("own contact: %v", err)
	}
	w.wantStored(t, address.Kind.Table, mine.ID, "is_primary_billing", "1")
	w.wantStored(t, address.ContactKind.Table, myContact.ID, "is_primary", "1")
}

// The Company has no create, so the first save is what makes a row its own.
func TestCD223_OwnScopeCompany(t *testing.T) {
	h := webtest.NewSessions(t, func(k mstkit.Env, sm *scs.SessionManager) func(chi.Router) {
		return func(r chi.Router) {
			allmasters.New(k).Routes(r, allmasters.WebDeps{Sessions: sm, Files: (*files.Store)(nil)})
		}
	})
	w, ctx := nsWrap(t, h.Env, msttest.Masters{}, allmasters.New(h.Env.Kit)), t.Context()
	rights := pgOwnRights()
	delete(rights, auth.RightCreate)
	delete(rights, auth.RightDelete)
	own := w.pgUser(t, pgDeepak, company.CompanyDocType, rights)
	other := auth.Principal{ID: pgRavi, Name: "Other", Roles: own.Roles}
	const record = "/m/company/1"

	in := company.CompanyInput{LegalName: pgMine, PAN: nsCompanyPAN}
	if err := w.svc.Company.SaveCompany(ctx, own, in, 0); err != nil {
		t.Fatalf("first save: %v", err)
	}
	webtest.WantText(t, h.Get(t, own, record), pgMine)
	webtest.WantText(t, h.Get(t, own, "/m/company"), pgMine)
	if rows := h.GetHTMX(t, own, "/pick/Company?q=Row").PickRows(); len(rows) != 1 {
		t.Errorf("owner's picker rows = %+v", rows)
	}
	if body := h.Get(t, own, "/m/company/export.csv").Body; !strings.Contains(body, pgMine) {
		t.Errorf("owner's export = %s", body)
	}
	in.LegalName = pgChanged
	if err := w.svc.Company.SaveCompany(ctx, own, in, 1); err != nil {
		t.Fatalf("owner's save: %v", err)
	}

	webtest.WantStatus(t, h.Get(t, other, record), http.StatusNotFound)
	webtest.WantNoText(t, h.Get(t, other, "/m/company"), pgChanged)
	if rows := h.GetHTMX(t, other, "/pick/Company?q=Row").PickRows(); len(rows) != 0 {
		t.Errorf("another user's picker rows = %+v", rows)
	}
	if body := h.Get(t, other, "/m/company/export.csv").Body; strings.Contains(body, pgChanged) {
		t.Errorf("another user's export = %s", body)
	}
	if err := w.svc.Company.SaveCompany(ctx, other, in, 2); !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("another user's save = %v, want not found", err)
	}

	w.pgNoCreator(t, company.CompanyKind.Table, 1)
	webtest.WantStatus(t, h.Get(t, own, record), http.StatusNotFound)
}

// pgField is one name-like field: the field's error key and label, and how to
// put a value in it.
type pgField struct {
	name, field, label string
	m                  pgMaster
	column             string
}

// pgWith is the master with value in one input field, for the service calls.
func pgWith[In any](k mstkit.Kind, svc func(*nsWorld) nsService[In], in func(w *nsWorld, code, value string) In, set func(*In, string)) pgMaster {
	return pgOf(k, "", svc, func(w *nsWorld, code, value string) In {
		x := in(w, code, "Fixed")
		set(&x, value)
		return x
	})
}

func pgFields() []pgField {
	var out []pgField
	for _, m := range pgMasters() {
		switch m.k.Table {
		case address.Kind.Table, address.ContactKind.Table:
		default:
			out = append(out, pgField{m.k.Table, "name", "Name", m, "name"})
		}
	}
	addr := func(set func(*address.Input, string)) pgMaster {
		return pgWith(address.Kind, func(w *nsWorld) nsService[address.Input] { return w.svc.Address }, nsAddressInput, set)
	}
	contact := func(set func(*address.ContactInput, string)) pgMaster {
		return pgWith(address.ContactKind, func(w *nsWorld) nsService[address.ContactInput] { return nsContacts{w.svc.Address} }, nsContactInput, set)
	}
	return append(out,
		pgField{"address title", "title", "Title", addr(func(in *address.Input, v string) { in.Title = v }), "title"},
		pgField{"address line 1", "line1", "Line 1", addr(func(in *address.Input, v string) { in.Line1 = v }), "line1"},
		pgField{"address line 2", "line2", "Line 2", addr(func(in *address.Input, v string) { in.Line2 = v }), "line2"},
		pgField{"address city", "city", "City", addr(func(in *address.Input, v string) { in.City = v }), "city"},
		pgField{"contact first", "first_name", "First name", contact(func(in *address.ContactInput, v string) { in.FirstName = v }), "first_name"},
		pgField{"contact middle", "middle_name", "Middle name", contact(func(in *address.ContactInput, v string) { in.MiddleName = v }), "middle_name"},
		pgField{"contact last", "last_name", "Last name", contact(func(in *address.ContactInput, v string) { in.LastName = v }), "last_name"},
	)
}

// pgWantTooLong fails unless err is the one field error with the exact text.
func pgWantTooLong(t *testing.T, what string, err error, field, label string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("%s = %v (%T), want a *ValidationError", what, err, err)
	}
	want := []doctype.FieldError{{Field: field, Message: label + " can be at most 140 characters."}}
	if !slices.Equal(ve.Fields, want) {
		t.Errorf("%s fields = %+v, want %+v", what, ve.Fields, want)
	}
}

// TestCD223_NameLength: through Create and Update, 140 characters of Hindi
// (three bytes each) pass and 141 are refused with the field's own label.
func TestCD223_NameLength(t *testing.T) {
	fit, over := strings.Repeat("क", pgLimit), strings.Repeat("क", pgLimit+1)
	for _, f := range pgFields() {
		t.Run(f.name, func(t *testing.T) {
			w := nsNew(t)
			ops, ctx := f.m.ops(w), t.Context()
			_, err := ops.create(ctx, w.Kavya, nsCode, over)
			pgWantTooLong(t, "Create", err, f.field, f.label)
			saved := pgCreate(t, ops, w.Kavya, nsCode, fit)
			w.wantStored(t, f.m.k.Table, saved.ID, f.column, fit)
			_, err = ops.update(ctx, w.Kavya, saved, nsCode, over)
			pgWantTooLong(t, "Update", err, f.field, f.label)
			w.wantStored(t, f.m.k.Table, saved.ID, f.column, fit)
			w.wantStored(t, f.m.k.Table, saved.ID, "version", strconv.Itoa(saved.Version))
		})
	}
}

// Country is free text outside India, so it needs an Overseas party to save.
func TestCD223_CountryLength(t *testing.T) {
	w := nsNew(t)
	ctx := t.Context()
	overseas, err := w.svc.Party.Create(ctx, w.Kavya, party.Input{Code: "NS-ABROAD", Name: "Abroad", IsVendor: true, GSTCategory: gstin.Overseas})
	if err != nil {
		t.Fatalf("setup party: %v", err)
	}
	in := func(country string) address.Input {
		return address.Input{PartyID: overseas.ID, Line1: "1 Main Road", City: "Dubai", Country: country, IsShipping: true}
	}
	over, fit := strings.Repeat("क", pgLimit+1), strings.Repeat("क", pgLimit)
	_, err = w.svc.Address.Create(ctx, w.Kavya, in(over))
	pgWantTooLong(t, "Create", err, "country", "Country")
	saved, err := w.svc.Address.Create(ctx, w.Kavya, in(fit))
	if err != nil {
		t.Fatalf("140 characters: %v", err)
	}
	_, err = w.svc.Address.Update(ctx, w.Kavya, saved.ID, saved.Version, in(over))
	pgWantTooLong(t, "Update", err, "country", "Country")
	w.wantStored(t, address.Kind.Table, saved.ID, "country", fit)
}

// Each part of a contact's name is capped; the joined name is not.
func TestCD223_ContactFullNameUncapped(t *testing.T) {
	w := nsNew(t)
	part := strings.Repeat("क", pgLimit)
	saved, err := w.svc.Address.CreateContact(t.Context(), w.Kavya, address.ContactInput{
		PartyID: w.m.Parties["SHREE"], FirstName: part, MiddleName: part, LastName: part,
	})
	if err != nil {
		t.Fatalf("three parts of 140: %v", err)
	}
	w.wantStored(t, address.ContactKind.Table, saved.ID, "name", part+" "+part+" "+part)
}

// The Company's name field is labelled Legal name.
func TestCD223_NameLengthCompany(t *testing.T) {
	w := nsBare(t)
	ctx := t.Context()
	over := strings.Repeat("क", pgLimit+1)
	err := w.svc.Company.SaveCompany(ctx, w.Kavya, company.CompanyInput{LegalName: over, PAN: nsCompanyPAN}, 0)
	pgWantTooLong(t, "first save", err, "name", "Legal name")
	fit := strings.Repeat("क", pgLimit)
	if err := w.svc.Company.SaveCompany(ctx, w.Kavya, company.CompanyInput{LegalName: fit, PAN: nsCompanyPAN}, 0); err != nil {
		t.Fatalf("140 characters: %v", err)
	}
	err = w.svc.Company.SaveCompany(ctx, w.Kavya, company.CompanyInput{LegalName: over, PAN: nsCompanyPAN}, 1)
	pgWantTooLong(t, "save", err, "name", "Legal name")
	w.wantStored(t, company.CompanyKind.Table, 1, "name", fit)
}

// A CSV row reaches the master's Validate, so it gets the same refusal.
func TestCD223_NameLengthImport(t *testing.T) {
	env, _ := othersFresh(t)
	over := strings.Repeat("क", pgLimit+1)
	rep := othersImport(t, env, uom.DocType, othersCSV(othersUoMHeader, "LONG,"+over+",,,", "FITS,"+strings.Repeat("क", pgLimit)+",,,"))
	var msgs []string
	for _, e := range rep.Errors {
		msgs = append(msgs, e.Message)
	}
	if want := []string{"Name can be at most 140 characters."}; !slices.Equal(msgs, want) {
		t.Errorf("row errors = %q, want %q", msgs, want)
	}
}

// pgCounts is what a refused twin must leave alone.
type pgCounts struct{ rows, audit int }

func (w *nsWorld) pgCount(t *testing.T, table string) pgCounts {
	t.Helper()
	var c pgCounts
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) error {
		if err := tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM `+table).Scan(&c.rows); err != nil { //nolint:gosec // G202: fixed test table names
			return err
		}
		return tx.SQL().QueryRowContext(ctx, `SELECT count(*) FROM audit_log`).Scan(&c.audit)
	})
	return c
}

// pgWithout grants pgRole every right that applies to docType but r. read stays
// even when r is another right, because AUTH needs it first.
func (w *nsWorld) pgWithout(t *testing.T, docType string, r auth.Right) auth.Principal {
	t.Helper()
	rights := map[auth.Right]auth.Scope{}
	dt := registered(t, docType)
	for _, x := range auth.AllRights {
		if x != r && auth.Applies(dt, x) {
			rights[x] = auth.ScopeAll
		}
	}
	if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, pgRole, docType, rights); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	return auth.Principal{ID: pgDeepak, Name: "Scoped", Roles: []string{pgRole}}
}

// pgTwin is one Tx twin and the right it must check itself.
type pgTwin struct {
	name  string
	right auth.Right
	call  func(ctx context.Context, tx *db.Tx, p auth.Principal) error
}

// wantRefused calls the twin inside one write with a principal who has every
// right but its own, and commits whatever it did: a twin that wrote and then
// refused must fail too, and a rollback would hide it.
func (w *nsWorld) wantRefused(t *testing.T, k mstkit.Kind, twin pgTwin) {
	t.Helper()
	t.Run(twin.name, func(t *testing.T) {
		p := w.pgWithout(t, k.DocType, twin.right)
		before := w.pgCount(t, k.Table)
		var got error
		if err := w.inTx(t, func(ctx context.Context, tx *db.Tx) error {
			got = twin.call(ctx, tx, p)
			return nil
		}); err != nil {
			t.Fatalf("the write: %v", err)
		}
		want := w.Kit.Auth.Can(t.Context(), p, k.DocType, twin.right, nil)
		if want == nil {
			t.Fatalf("the principal has %s on %s", twin.right, k.DocType)
		}
		if !errors.Is(got, auth.ErrForbidden) || got.Error() != want.Error() {
			t.Errorf("got %v (%T), want AUTH's %q", got, got, want)
		}
		if after := w.pgCount(t, k.Table); after != before {
			t.Errorf("counts %+v, want %+v: a refused twin wrote", after, before)
		}
	})
}

// TestCD224_TxTwinsCheckTheirRight calls each twin directly, as importers and
// other masters do: Env.Write's kind-level check isn't on this path, so the
// twin's own CanTx is the only thing that refuses (site DeleteTx and uom
// UpdateTx kept mutants alive without this).
func TestCD224_TxTwinsCheckTheirRight(t *testing.T) {
	for _, m := range pgMasters() {
		t.Run(m.k.Table, func(t *testing.T) {
			w := nsNew(t)
			if err := w.pgAuth(t).CreateRole(t.Context(), w.Admin, pgRole); err != nil {
				t.Fatalf("CreateRole: %v", err)
			}
			ops := m.ops(w)
			on := pgCreate(t, ops, w.Kavya, nsCode, nsFirst)
			off := pgCreate(t, ops, w.Kavya, nsRecode, nsSecond)
			if err := ops.setDisabled(t.Context(), w.Kavya, off, true); err != nil {
				t.Fatalf("setup disable: %v", err)
			}
			off.Version++
			for _, twin := range []pgTwin{
				{"CreateTx", auth.RightCreate, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
					return ops.createTx(ctx, tx, p, "NS-3", "Third value")
				}},
				{"UpdateTx", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
					return ops.updateTx(ctx, tx, p, on, nsCode, "Changed value")
				}},
				{"SetDisabledTx disable", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
					return ops.setDisabledTx(ctx, tx, p, on, true)
				}},
				{"SetDisabledTx enable", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
					return ops.setDisabledTx(ctx, tx, p, off, false)
				}},
				{"DeleteTx", auth.RightDelete, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
					return ops.deleteTx(ctx, tx, p, on)
				}},
			} {
				w.wantRefused(t, m.k, twin)
			}
			w.wantStored(t, m.k.Table, on.ID, "version", strconv.Itoa(on.Version))
			w.wantStored(t, m.k.Table, off.ID, "version", strconv.Itoa(off.Version))
		})
	}
}

// The twins beyond the shared four.
func TestCD224_OtherTxTwinsCheckTheirRight(t *testing.T) {
	w := nsNew(t)
	if err := w.pgAuth(t).CreateRole(t.Context(), w.Admin, pgRole); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	vendor := w.nsVendor(t)
	w.nsAddress(t, vendor, "First")
	second := w.nsAddress(t, vendor, "Second")
	w.nsContact(t, vendor, "First", true)
	contact := w.nsContact(t, vendor, "Second", false)

	w.wantRefused(t, address.Kind, pgTwin{"SetPrimaryTx billing", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Address.SetPrimaryTx(ctx, tx, p, second.ID, true, second.Version)
	}})
	w.wantRefused(t, address.Kind, pgTwin{"SetPrimaryTx shipping", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Address.SetPrimaryTx(ctx, tx, p, second.ID, false, second.Version)
	}})
	w.wantStored(t, address.Kind.Table, second.ID, "is_primary_billing", "0")
	w.wantRefused(t, address.ContactKind, pgTwin{"SetPrimaryContactTx", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Address.SetPrimaryContactTx(ctx, tx, p, vendor, contact.ID)
	}})
	w.wantStored(t, address.ContactKind.Table, contact.ID, "is_primary", "0")

	in := company.CompanyInput{LegalName: "Changed", PAN: nsCompanyPAN}
	w.wantRefused(t, company.CompanyKind, pgTwin{"SaveCompanyTx update", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Company.SaveCompanyTx(ctx, tx, p, in, 1)
	}})
	w.wantRefused(t, company.CompanyKind, pgTwin{"SaveCompanyTx first save", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Company.SaveCompanyTx(ctx, tx, p, in, 0)
	}})
	w.wantRefused(t, company.CompanyKind, pgTwin{"SetupTx", auth.RightWrite, func(ctx context.Context, tx *db.Tx, p auth.Principal) error {
		return w.svc.Company.SetupTx(ctx, tx, p, company.SetupInput{LegalName: "Changed", StateCode: "27", PAN: nsCompanyPAN, BranchCode: "NS-HO"})
	}})
}
