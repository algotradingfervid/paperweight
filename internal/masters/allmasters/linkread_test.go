package allmasters_test

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
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
)

// CD-227 on the real auth.Service: a link a user posts from one master to
// another reads the target with the user's own rights, and a target the user
// can't read looks exactly like an unknown id.

const (
	lrRole                  = "Scoped Masters"
	lrHiddenCode, lrOffCode = "HID-1", "OFF-1"
	lrHiddenName            = "Hidden Target"
	lrUnknown               = int64(9_999_999)
	lrBranchGone            = "Branch not found. Pick one from the list."
	lrPartyGone             = "Party not found. Pick one from the list."
	lrTermsGone             = "Payment Terms not found. Pick one from the list."
)

// lrOps are a source master's calls with the link under test set to target.
type lrOps struct {
	create func(ctx context.Context, p auth.Principal, code string, target int64) (mstkit.Saved, error)
	update func(ctx context.Context, p auth.Principal, s mstkit.Saved, code string, target int64) (mstkit.Saved, error)
}

// lrLink is one user-fed link. fixed links can't change after create (CD-216(1)).
// mine is a target the test user created, for the links that need a second
// row of the target's kind. cells are the link's CSV cells, nil if it has no column.
type lrLink struct {
	source, target mstkit.Kind
	field, gone    string
	line           int
	fixed          bool
	ops            func(w *nsWorld, mine int64) lrOps
	newTarget      func(t *testing.T, w *nsWorld, p auth.Principal, code, value string) mstkit.Saved
	cells          func(w *nsWorld, code string) map[string]string
}

func (l lrLink) name() string { return l.source.Table + "." + l.field }

func lrOf[In any](source mstkit.Kind, field string, target mstkit.Kind, gone string,
	svc func(*nsWorld) nsService[In], in func(w *nsWorld, mine int64, code string, id int64) In) lrLink {
	return lrLink{source: source, target: target, field: field, gone: gone, ops: func(w *nsWorld, mine int64) lrOps {
		s := svc(w)
		return lrOps{
			create: func(ctx context.Context, p auth.Principal, code string, id int64) (mstkit.Saved, error) {
				return s.Create(ctx, p, in(w, mine, code, id))
			},
			update: func(ctx context.Context, p auth.Principal, o mstkit.Saved, code string, id int64) (mstkit.Saved, error) {
				return s.Update(ctx, p, o.ID, o.Version, in(w, mine, code, id))
			},
		}
	}}
}

func (l lrLink) fixedOwner() lrLink { l.fixed = true; return l }

func (l lrLink) onLine(n int) lrLink { l.line = n; return l }

func (l lrLink) csv(cells func(w *nsWorld, code string) map[string]string) lrLink {
	l.cells = cells
	return l
}

func (l lrLink) madeBy(fn func(t *testing.T, w *nsWorld, p auth.Principal, code, value string) mstkit.Saved) lrLink {
	l.newTarget = fn
	return l
}

func lrAddresses(w *nsWorld) nsService[address.Input]        { return w.svc.Address }
func lrContacts(w *nsWorld) nsService[address.ContactInput]  { return nsContacts{w.svc.Address} }
func lrItems(w *nsWorld) nsService[item.Input]               { return w.svc.Item }
func lrExpenseTypes(w *nsWorld) nsService[expensetype.Input] { return w.svc.ExpenseType }
func lrProjects(w *nsWorld) nsService[project.Input]         { return w.svc.Project }
func lrSites(w *nsWorld) nsService[site.Input]               { return w.svc.Site }
func lrEmployees(w *nsWorld) nsService[employee.Input]       { return w.svc.Employee }

func lrPartyCells(_ *nsWorld, code string) map[string]string {
	return map[string]string{"Party": code, "First name": "Row", "Line 1": "1 Main Road", "City": "Mumbai", "State": "Maharashtra", "Pincode": "400001", "Shipping": "Yes"}
}

func lrItemCells(code, label string) map[string]string {
	cells := map[string]string{"Code": "SRC-1", "Name": "Row", "Type": "Goods", "HSN/SAC": "7216", "UoM": "KGS", "Tax Code": "GST18"}
	cells[label] = code
	return cells
}

func lrOrgCells(code, label string) map[string]string {
	cells := map[string]string{
		"Code": "SRC-1", "Name": "Row", "Branch": "PUNE", "Type": "Warehouse", "Date of joining": "01/04/2024", "Status": "Active",
	}
	cells[label] = code
	return cells
}

func lrProjectCells(code, label string) map[string]string {
	cells := lrOrgCells(code, label)
	cells["Status"] = "Open"
	return cells
}

// lrLinks is every user-fed link of a master to a master. TestCD227_EveryLinkListed
// compares it with the registry.
func lrLinks() []lrLink {
	customer := func(t *testing.T, w *nsWorld, p auth.Principal, code, value string) mstkit.Saved {
		t.Helper()
		saved, err := w.svc.Party.Create(t.Context(), p, party.Input{Code: code, Name: value, IsCustomer: true})
		if err != nil {
			t.Fatalf("create customer %s: %v", code, err)
		}
		return saved
	}
	conversions := func(mine, id int64) []item.Conversion {
		if id == mine {
			return nil
		}
		return []item.Conversion{{UoMID: id, Factor: 2 * uom.FactorOne}}
	}
	return []lrLink{
		lrOf(address.Kind, "party_id", party.Kind, lrPartyGone, lrAddresses, func(w *nsWorld, _ int64, code string, id int64) address.Input {
			in := nsAddressInput(w, code, "Row")
			in.PartyID = id
			return in
		}).fixedOwner().csv(lrPartyCells),
		lrOf(address.Kind, "branch_id", company.Kind, lrBranchGone, lrAddresses, func(w *nsWorld, _ int64, code string, id int64) address.Input {
			in := nsAddressInput(w, code, "Row")
			in.PartyID, in.BranchID = 0, id
			return in
		}).fixedOwner(),
		lrOf(address.ContactKind, "party_id", party.Kind, lrPartyGone, lrContacts, func(w *nsWorld, _ int64, code string, id int64) address.ContactInput {
			in := nsContactInput(w, code, "Row")
			in.PartyID = id
			return in
		}).fixedOwner().csv(lrPartyCells),
		lrOf(address.ContactKind, "branch_id", company.Kind, lrBranchGone, lrContacts, func(w *nsWorld, _ int64, code string, id int64) address.ContactInput {
			in := nsContactInput(w, code, "Row")
			in.PartyID, in.BranchID = 0, id
			return in
		}).fixedOwner(),
		lrOf(party.Kind, "default_payment_terms_id", paymentterms.Kind, lrTermsGone, func(w *nsWorld) nsService[party.Input] { return w.svc.Party }, func(w *nsWorld, _ int64, code string, id int64) party.Input {
			in := nsPartyInput(w, code, "Row")
			in.DefaultPaymentTermsID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string {
			return map[string]string{"Code": "SRC-1", "Name": "Row", "Vendor": "Yes", "Payment Terms": code}
		}),
		lrOf(item.Kind, "stock_uom_id", uom.Kind, item.MsgStockNotFound, lrItems, func(w *nsWorld, _ int64, code string, id int64) item.Input {
			in := nsItemInput(w, code, "Row")
			in.StockUoMID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrItemCells(code, "UoM") }),
		lrOf(item.Kind, "purchase_uom_id", uom.Kind, item.MsgPurchaseNotFound, lrItems, func(w *nsWorld, mine int64, code string, id int64) item.Input {
			in := nsItemInput(w, code, "Row")
			in.StockUoMID, in.PurchaseUoMID, in.Conversions = mine, id, conversions(mine, id)
			return in
		}),
		lrOf(item.Kind, "uom_id", uom.Kind, "Row 1: UoM not found. Pick one from the list.", lrItems, func(w *nsWorld, mine int64, code string, id int64) item.Input {
			in := nsItemInput(w, code, "Row")
			in.StockUoMID, in.Conversions = mine, conversions(mine, id)
			return in
		}).onLine(1),
		lrOf(item.Kind, "default_tax_code_id", taxcode.Kind, taxcode.MsgDefaultNotFound, lrItems, func(w *nsWorld, _ int64, code string, id int64) item.Input {
			in := nsItemInput(w, code, "Row")
			in.DefaultTaxCodeID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrItemCells(code, "Tax Code") }),
		lrOf(expensetype.Kind, "default_tax_code_id", taxcode.Kind, taxcode.MsgDefaultNotFound, lrExpenseTypes, func(w *nsWorld, _ int64, code string, id int64) expensetype.Input {
			in := nsExpenseInput(w, code, "Row")
			in.DefaultTaxCodeID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string {
			return map[string]string{"Code": "SRC-1", "Name": "Row", "Default tax code": code}
		}),
		lrOf(project.Kind, "branch_id", company.Kind, project.MsgBranchNotFound, lrProjects, func(w *nsWorld, _ int64, code string, id int64) project.Input {
			in := nsProjectInput(w, code, "Row")
			in.BranchID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrProjectCells(code, "Branch") }),
		lrOf(project.Kind, "customer_id", party.Kind, project.MsgCustomerNotFound, lrProjects, func(w *nsWorld, _ int64, code string, id int64) project.Input {
			in := nsProjectInput(w, code, "Row")
			in.CustomerID = id
			return in
		}).madeBy(customer).csv(func(_ *nsWorld, code string) map[string]string { return lrProjectCells(code, "Customer") }),
		lrOf(site.Kind, "branch_id", company.Kind, site.MsgBranchNotFound, lrSites, func(w *nsWorld, _ int64, code string, id int64) site.Input {
			in := nsSiteInput(w, code, "Row")
			in.BranchID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrOrgCells(code, "Branch") }),
		lrOf(site.Kind, "project_id", project.Kind, site.MsgProjectNotFound, lrSites, func(w *nsWorld, _ int64, code string, id int64) site.Input {
			in := nsSiteInput(w, code, "Row")
			in.ProjectID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrOrgCells(code, "Project") }),
		lrOf(employee.Kind, "branch_id", company.Kind, employee.MsgBranchNotFound, lrEmployees, func(w *nsWorld, _ int64, code string, id int64) employee.Input {
			in := nsEmployeeInput(w, code, "Row")
			in.BranchID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrOrgCells(code, "Branch") }),
		lrOf(employee.Kind, "manager_id", employee.Kind, employee.MsgManagerNotFound, lrEmployees, func(w *nsWorld, _ int64, code string, id int64) employee.Input {
			in := nsEmployeeInput(w, code, "Row")
			in.ManagerID = id
			return in
		}).csv(func(_ *nsWorld, code string) map[string]string { return lrOrgCells(code, "Reporting manager") }),
	}
}

// lrScoped is a user whose only role reads every master, with own-scope rights
// on the link's two ends.
func (w *nsWorld) lrScoped(t *testing.T, l lrLink) auth.Principal {
	t.Helper()
	a := w.pgAuth(t)
	if err := a.CreateRole(t.Context(), w.Admin, lrRole); err != nil {
		t.Fatalf("CreateRole: %v", err)
	}
	for _, m := range pgMasters() {
		rights := map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeAll}
		if m.k.DocType == l.source.DocType || m.k.DocType == l.target.DocType {
			rights = pgOwnRights()
		}
		if m.k.DocType == l.source.DocType && auth.Applies(registered(t, m.k.DocType), auth.RightImport) {
			rights[auth.RightImport] = auth.ScopeOwn
		}
		if err := a.SetRights(t.Context(), w.Admin, lrRole, m.k.DocType, rights); err != nil {
			t.Fatalf("SetRights(%s): %v", m.k.DocType, err)
		}
	}
	return auth.Principal{ID: pgDeepak, Name: "Scoped", Roles: []string{lrRole}}
}

// lrMaster is the master docType.
func lrMaster(t *testing.T, docType string) pgMaster {
	t.Helper()
	for _, m := range pgMasters() {
		if m.k.DocType == docType {
			return m
		}
	}
	t.Fatalf("no master %s", docType)
	return pgMaster{}
}

// lrTarget creates a row of the link's target kind as p.
func (l lrLink) lrTarget(t *testing.T, w *nsWorld, p auth.Principal, code, value string) mstkit.Saved {
	t.Helper()
	if l.newTarget != nil {
		return l.newTarget(t, w, p, code, value)
	}
	return pgCreate(t, lrMaster(t, l.target.DocType).ops(w), p, code, value)
}

func lrDisable(t *testing.T, w *nsWorld, k mstkit.Kind, p auth.Principal, s mstkit.Saved) {
	t.Helper()
	if err := lrMaster(t, k.DocType).ops(w).setDisabled(t.Context(), p, s, true); err != nil {
		t.Fatalf("disable %s: %v", k.DocType, err)
	}
}

// lrWantGone fails unless err refuses field on line with exactly text.
func lrWantGone(t *testing.T, what string, err error, field string, line int, text string) {
	t.Helper()
	var ve *mstkit.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("%s: err = %v (%T), want a validation error", what, err, err)
	}
	for _, f := range ve.Fields {
		if f.Field == field && f.Line == line {
			if f.Message != text {
				t.Errorf("%s: %s says %q, want %q", what, field, f.Message, text)
			}
			return
		}
	}
	t.Errorf("%s: no error on %s: %v", what, field, ve.Fields)
}

// TestCD227_LinkReadsTheTarget: for every link, a target another user created
// is refused with exactly the unknown id's text, on create and on update, with
// nothing written and no code or name of the target in the message; the user's
// own target, a user who reads all and a user given read all all work; a user
// with no read right at all is refused the same way.
func TestCD227_LinkReadsTheTarget(t *testing.T) {
	for _, l := range lrLinks() {
		t.Run(l.name(), func(t *testing.T) {
			w := nsNew(t)
			ctx := t.Context()
			own := w.lrScoped(t, l)
			hidden := l.lrTarget(t, w, w.Kavya, lrHiddenCode, lrHiddenName)
			off := l.lrTarget(t, w, w.Kavya, lrOffCode, lrHiddenName+" Off")
			lrDisable(t, w, l.target, w.Kavya, off)
			mine := l.lrTarget(t, w, own, "OWN-1", "Own Target")
			ops := l.ops(w, mine.ID)
			rows := nsCount(t, w.Env, l.source.Table, "1=1")

			_, unknownErr := ops.create(ctx, own, "SRC-1", lrUnknown)
			lrWantGone(t, "unknown id", unknownErr, l.field, l.line, l.gone)
			for name, id := range map[string]int64{"hidden": hidden.ID, "hidden and disabled": off.ID} {
				_, err := ops.create(ctx, own, "SRC-1", id)
				lrWantGone(t, name, err, l.field, l.line, l.gone)
				if err.Error() != unknownErr.Error() {
					t.Errorf("%s: %q differs from the unknown id's %q", name, err.Error(), unknownErr.Error())
				}
				for _, secret := range []string{lrHiddenCode, lrOffCode, lrHiddenName} {
					if strings.Contains(err.Error(), secret) {
						t.Errorf("%s: message names the target: %q", name, err.Error())
					}
				}
			}
			if got := nsCount(t, w.Env, l.source.Table, "1=1"); got != rows {
				t.Fatalf("%d rows after the refusals, want %d", got, rows)
			}

			saved, err := ops.create(ctx, own, "SRC-1", mine.ID)
			if err != nil {
				t.Fatalf("own target: %v", err)
			}
			if !l.fixed {
				_, err := ops.update(ctx, own, saved, "SRC-1", hidden.ID)
				lrWantGone(t, "update", err, l.field, l.line, l.gone)
				w.wantStored(t, l.source.Table, saved.ID, "version", "1")
				if _, err := ops.update(ctx, own, saved, "SRC-1", mine.ID); err != nil {
					t.Errorf("update keeping the own target: %v", err)
				}
			}

			if _, err := ops.create(ctx, w.Kavya, "SRC-2", hidden.ID); err != nil {
				t.Errorf("a user who reads all: %v", err)
			}

			a := w.pgAuth(t)
			wide := pgOwnRights()
			wide[auth.RightRead] = auth.ScopeAll
			if err := a.SetRights(ctx, w.Admin, lrRole, l.target.DocType, wide); err != nil {
				t.Fatalf("SetRights: %v", err)
			}
			if _, err := ops.create(ctx, own, "SRC-3", hidden.ID); err != nil {
				t.Errorf("read all on the target: %v", err)
			}

			if l.source.DocType == l.target.DocType {
				return
			}
			if err := a.SetRights(ctx, w.Admin, lrRole, l.target.DocType, map[auth.Right]auth.Scope{}); err != nil {
				t.Fatalf("SetRights: %v", err)
			}
			_, err = ops.create(ctx, own, "SRC-4", hidden.ID)
			lrWantGone(t, "no read right", err, l.field, l.line, l.gone)
		})
	}
}

// lrFK is one foreign key: table.column points at target's id.
type lrFK struct{ table, column, target string }

// lrFKGaps lists the foreign keys of a master or its row tables (named
// <master table>_<rows>) that point into a master table with no Link field to
// say so. A row table's key to its own master is its parent, which no user posts.
func lrFKGaps(fks []lrFK) []string {
	masters := map[string]*doctype.DocType{}
	for _, dt := range doc.All() {
		if dt.IsMaster() {
			masters[dt.Table] = dt
		}
	}
	owner := func(table string) *doctype.DocType {
		if dt, ok := masters[table]; ok {
			return dt
		}
		for name, dt := range masters {
			if strings.HasPrefix(table, name+"_") {
				return dt
			}
		}
		return nil
	}
	var gaps []string
	for _, fk := range fks {
		dt := owner(fk.table)
		if dt == nil || masters[fk.target] == nil {
			continue
		}
		if fk.table != dt.Table && fk.target == dt.Table {
			continue
		}
		f, ok := dt.Field(fk.column)
		if !ok || f.Type != doctype.Link || masters[fk.target].Name != f.Link {
			gaps = append(gaps, fk.table+"."+fk.column+" -> "+fk.target)
		}
	}
	return gaps
}

func TestCD227_FKGapsFindsAnUndeclaredKey(t *testing.T) {
	got := lrFKGaps([]lrFK{
		{"party", "default_payment_terms_id", "payment_terms"}, // a Link
		{"item_uom", "uom_id", "uom"},                          // a row table's Link
		{"item_uom", "parent_id", "item"},                      // its parent
		{"employee", "user_id", "users"},                       // not a master
		{"mst_order", "vendor_id", "party"},                    // not a master's
		{"party", "ghost_id", "uom"},
		{"item_other", "ghost_id", "uom"},
		{"site", "project_id", "uom"}, // a Link, to another master
	})
	want := []string{"party.ghost_id -> uom", "item_other.ghost_id -> uom", "site.project_id -> uom"}
	if !slices.Equal(got, want) {
		t.Errorf("gaps = %q, want %q", got, want)
	}
}

// TestCD227_EveryLinkListed: lrLinks is the registry's links from a master to a
// master, so a new link can't skip the table above, and every foreign key
// between masters is one of them. Masters never declare Links.
func TestCD227_EveryLinkListed(t *testing.T) {
	var want []string
	for _, l := range lrLinks() {
		want = append(want, l.source.DocType+"."+l.field+" -> "+l.target.DocType)
	}
	slices.Sort(want)
	var got []string
	for _, dt := range doc.All() {
		if !dt.IsMaster() {
			continue
		}
		if len(dt.Links) != 0 {
			t.Errorf("%s declares Links", dt.Name)
		}
		for _, f := range dt.Fields {
			if f.Type != doctype.Link {
				continue
			}
			if target, ok := doctype.Lookup(f.Link); ok && target.IsMaster() {
				got = append(got, dt.Name+"."+f.Name+" -> "+f.Link)
			}
		}
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("master links:\n got %q\nwant %q", got, want)
	}

	var fks []lrFK
	read(t, msttest.New(t), func(ctx context.Context, tx *db.Tx) error {
		rows, err := tx.SQL().QueryContext(ctx,
			`SELECT m.name, f."from", f."table" FROM sqlite_master m, pragma_foreign_key_list(m.name) f WHERE m.type = 'table'`)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var fk lrFK
			if err := rows.Scan(&fk.table, &fk.column, &fk.target); err != nil {
				return err
			}
			fks = append(fks, fk)
		}
		return rows.Err()
	})
	if len(fks) == 0 {
		t.Fatal("no foreign keys read")
	}
	if gaps := lrFKGaps(fks); len(gaps) != 0 {
		t.Errorf("foreign keys into a master with no Link field: %q", gaps)
	}
}

// TestCD227_SeededRolesReadTheirTargets: every role that M1 lets create or
// write a master reads, in all, each master it links to, so the check refuses
// no seeded role.
func TestCD227_SeededRolesReadTheirTargets(t *testing.T) {
	for _, l := range lrLinks() {
		target := mstkit.DefaultRights(l.target.DocType)
		writers := 0
		for role, rights := range mstkit.DefaultRights(l.source.DocType) {
			if rights["create"] == "" && rights["write"] == "" {
				continue
			}
			writers++
			if got := target[role]["read"]; got != "all" {
				t.Errorf("%s: role %s writes %s but reads %s at %q", l.name(), role, l.source.DocType, l.target.DocType, got)
			}
		}
		if writers == 0 {
			t.Errorf("%s: no seeded role writes it", l.name())
		}
	}
}

// TestCD227_PANNeverReachesAHiddenParty: MST-048's text quotes the party's
// stored PAN. The user's own party gets it; a hidden one gets the not-found
// text, and its PAN isn't taken or changed.
func TestCD227_PANNeverReachesAHiddenParty(t *testing.T) {
	w := nsNew(t)
	l := lrLinks()[0]
	own := w.lrScoped(t, l)
	in := func(id int64) address.Input {
		return address.Input{
			PartyID: id, Line1: "1 Main Road", City: "Mumbai", StateCode: "27", Pincode: "400001",
			GSTIN: "27AAGCM1234F1ZS", IsBilling: true,
		}
	}
	newParty := func(p auth.Principal, code string) mstkit.Saved {
		saved, err := w.svc.Party.Create(t.Context(), p, party.Input{Code: code, Name: code, IsVendor: true, PAN: partiesShreePAN, GSTCategory: gstin.Regular})
		if err != nil {
			t.Fatalf("create party: %v", err)
		}
		return saved
	}
	theirs, mine := newParty(w.Kavya, "THEIRS"), newParty(own, "MINE")

	_, err := w.svc.Address.Create(t.Context(), own, in(mine.ID))
	want := "GSTIN 27AAGCM1234F1ZS belongs to PAN AAGCM1234F, but this party's PAN is " + partiesShreePAN + ". Use a separate party for a different business."
	lrWantGone(t, "own party", err, "gstin", 0, want)

	_, err = w.svc.Address.Create(t.Context(), own, in(theirs.ID))
	lrWantGone(t, "hidden party", err, "party_id", 0, lrPartyGone)
	if strings.Contains(err.Error(), partiesShreePAN) {
		t.Errorf("the message shows the PAN: %q", err.Error())
	}
	w.wantStored(t, party.Kind.Table, theirs.ID, "version", "1")
}

// TestCD227_TargetRulesStayHidden: the rules that read a target's other fields
// come after the check, so their texts never describe a hidden target.
func TestCD227_TargetRulesStayHidden(t *testing.T) {
	links := map[string]lrLink{}
	for _, l := range lrLinks() {
		links[l.name()] = l
	}

	t.Run("a vendor as customer", func(t *testing.T) {
		w := nsNew(t)
		l := links["project.customer_id"]
		own := w.lrScoped(t, l)
		vendor := pgCreate(t, lrMaster(t, party.DocType).ops(w), w.Kavya, "VEND-1", "Vendor Only")
		_, err := l.ops(w, 0).create(t.Context(), own, "SRC-1", vendor.ID)
		lrWantGone(t, "vendor", err, "customer_id", 0, project.MsgCustomerNotFound)
	})

	t.Run("a project of another branch", func(t *testing.T) {
		w := nsNew(t)
		l := links["site.project_id"]
		own := w.lrScoped(t, l)
		pr, err := w.svc.Project.Create(t.Context(), w.Kavya, project.Input{Code: "HID-1", Name: "Elsewhere", BranchID: w.m.Branches["BLR"]})
		if err != nil {
			t.Fatalf("create project: %v", err)
		}
		_, err = l.ops(w, 0).create(t.Context(), own, "SRC-1", pr.ID)
		lrWantGone(t, "other branch", err, "project_id", 0, site.MsgProjectNotFound)
	})

	t.Run("a manager who has left", func(t *testing.T) {
		w := nsNew(t)
		l := links["employee.manager_id"]
		own := w.lrScoped(t, l)
		gone := l.lrTarget(t, w, w.Kavya, "LEFT-1", "Former Manager")
		lrDisable(t, w, l.target, w.Kavya, gone)
		_, err := l.ops(w, 0).create(t.Context(), own, "SRC-1", gone.ID)
		lrWantGone(t, "left", err, "manager_id", 0, employee.MsgManagerNotFound)
	})

	t.Run("a registered party outside India", func(t *testing.T) {
		w := nsNew(t)
		own := w.lrScoped(t, links["address.party_id"])
		in := nsAddressInput(w, "", "Row")
		in.Country, in.StateCode, in.Pincode = "Nepal", "", "44600"
		_, err := w.svc.Address.Create(t.Context(), own, in)
		lrWantGone(t, "overseas", err, "party_id", 0, lrPartyGone)
	})
}

// TestCD227_OwnerCheckedOnCreateOnly: an address's or contact's owner can't
// change, so a save after the owner stopped being readable still works
// (CD-216(1)).
func TestCD227_OwnerCheckedOnCreateOnly(t *testing.T) {
	for _, l := range lrLinks() {
		if !l.fixed {
			continue
		}
		t.Run(l.name(), func(t *testing.T) {
			w := nsNew(t)
			own := w.lrScoped(t, l)
			mine := l.lrTarget(t, w, own, "OWN-1", "Own Target")
			ops := l.ops(w, mine.ID)
			saved, err := ops.create(t.Context(), own, "SRC-1", mine.ID)
			if err != nil {
				t.Fatalf("create: %v", err)
			}
			if err := w.pgAuth(t).SetRights(t.Context(), w.Admin, lrRole, l.target.DocType, map[auth.Right]auth.Scope{}); err != nil {
				t.Fatalf("SetRights: %v", err)
			}
			if _, err := ops.update(t.Context(), own, saved, "SRC-1", mine.ID); err != nil {
				t.Errorf("update after the owner became hidden: %v", err)
			}
		})
	}
}

// TestCD227_LeaveKeepsItsSkips: marking an employee Left skips the read check on
// the links it keeps (CD-204); any other save re-checks them (CD-216(1)).
func TestCD227_LeaveKeepsItsSkips(t *testing.T) {
	w := nsNew(t)
	ctx := t.Context()
	var l lrLink
	for _, c := range lrLinks() {
		if c.name() == "employee.manager_id" {
			l = c
		}
	}
	own := w.lrScoped(t, l)
	a := w.pgAuth(t)
	wide := pgOwnRights()
	wide[auth.RightRead] = auth.ScopeAll
	if err := a.SetRights(ctx, w.Admin, lrRole, employee.DocType, wide); err != nil {
		t.Fatalf("SetRights: %v", err)
	}
	manager := pgCreate(t, lrMaster(t, employee.DocType).ops(w), w.Kavya, "MGR-1", "Manager")
	in := nsEmployeeInput(w, "EMP-901", "Leaver")
	in.ManagerID = manager.ID
	saved, err := w.svc.Employee.Create(ctx, own, in)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for docType, rights := range map[string]map[auth.Right]auth.Scope{employee.DocType: pgOwnRights(), company.DocType: pgOwnRights()} {
		if err := a.SetRights(ctx, w.Admin, lrRole, docType, rights); err != nil {
			t.Fatalf("SetRights: %v", err)
		}
	}

	in.Name = "Renamed"
	_, err = w.svc.Employee.Update(ctx, own, saved.ID, saved.Version, in)
	lrWantGone(t, "a plain save", err, "branch_id", 0, employee.MsgBranchNotFound)
	lrWantGone(t, "a plain save", err, "manager_id", 0, employee.MsgManagerNotFound)

	if err := w.svc.Employee.SetDisabled(ctx, own, saved.ID, saved.Version, true); err != nil {
		t.Errorf("leave with hidden kept links: %v", err)
	}
	w.wantStored(t, employee.Kind.Table, saved.ID, "status", string(employee.Left))
}

// lrImport runs one CSV row naming code in the link's column as p.
func (l lrLink) lrImport(t *testing.T, w *nsWorld, p auth.Principal, code string, preview bool) []string {
	t.Helper()
	dt := registered(t, l.source.DocType)
	var columns []string
	for _, f := range dt.ImportFields() {
		columns = append(columns, f.Label)
	}
	cells := l.cells(w, code)
	row := make([]string, len(columns))
	for i, c := range columns {
		row[i] = cells[c]
	}
	file := strings.NewReader(strings.Join(columns, ",") + "\n" + strings.Join(row, ",") + "\n")
	run := othersImporter(w.Env).Import
	if preview {
		run = othersImporter(w.Env).Preview
	}
	rep, err := run(t.Context(), p, dt, file)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return othersMessages(rep)
}

// TestCD232_ImportHidesTargets: csvio reads a link cell with the importer's
// rights. A hidden code, a hidden and disabled one and an unknown one read the
// same but for the code typed, in the dry run and the import, nothing is
// written, and no suggestion names a row the user can't read.
func TestCD232_ImportHidesTargets(t *testing.T) {
	for _, l := range lrLinks() {
		if l.cells == nil {
			continue
		}
		for _, preview := range []bool{true, false} {
			name := l.name() + "/import"
			if preview {
				name = l.name() + "/preview"
			}
			t.Run(name, func(t *testing.T) {
				w := nsNew(t)
				allmasters.RegisterImporters(w.svc)
				own := w.lrScoped(t, l)
				l.lrTarget(t, w, w.Kavya, "ZED-1", lrHiddenName)
				off := l.lrTarget(t, w, w.Kavya, "ZED-2", lrHiddenName+" Off")
				lrDisable(t, w, l.target, w.Kavya, off)
				rows := nsCount(t, w.Env, l.source.Table, "1=1")

				say := func(code string) string {
					t.Helper()
					got := l.lrImport(t, w, own, code, preview)
					if len(got) == 0 {
						t.Fatalf("%s: no refusal", code)
					}
					return strings.ReplaceAll(strings.Join(got, " | "), code, "<code>")
				}
				unknown := say("ZED-9")
				if !strings.Contains(unknown, "'<code>' not found.") {
					t.Errorf("unknown code = %q", unknown)
				}
				for _, code := range []string{"ZED-1", "ZED-2"} {
					if got := say(code); got != unknown {
						t.Errorf("%s = %q, unknown code = %q", code, got, unknown)
					}
				}
				if strings.Contains(unknown, "such as ") {
					t.Errorf("the suggestion names a row the user can't read: %q", unknown)
				}
				if got := nsCount(t, w.Env, l.source.Table, "1=1"); got != rows {
					t.Errorf("%d rows after the import, want %d", got, rows)
				}

				// The user's own row is the one suggestion left.
				l.lrTarget(t, w, own, "ZED-3", "Own Target")
				if got := say("ZED-9"); !strings.Contains(got, " such as ZED-3.") {
					t.Errorf("with a readable row: %q", got)
				}
			})
		}
	}
}

// The cell's check and each suggestion's read in the import's tx: on a pool of
// one connection a second one would hang (CD-213).
func TestCD232_ImportOnOneConnection(t *testing.T) {
	l := lrLinks()[0]
	w := nsNew(t)
	allmasters.RegisterImporters(w.svc)
	own := w.lrScoped(t, l)
	l.lrTarget(t, w, w.Kavya, "ZED-1", lrHiddenName)
	var got []string
	for _, preview := range []bool{true, false} {
		w.do(t, func(context.Context) error {
			got = append(got, l.lrImport(t, w, own, "ZED-1", preview)...)
			return nil
		})
	}
	if len(got) != 2 {
		t.Errorf("errors = %q", got)
	}
}

// A user who reads all gets csvio's disabled text and suggestions as before.
func TestCD232_ImportReadAllAnswersAsBefore(t *testing.T) {
	l := lrLinks()[0]
	w := nsNew(t)
	allmasters.RegisterImporters(w.svc)
	off := l.lrTarget(t, w, w.Kavya, "ZED-2", lrHiddenName+" Off")
	lrDisable(t, w, l.target, w.Kavya, off)
	if got := l.lrImport(t, w, w.Kavya, "ZED-2", true); !slices.Equal(got, []string{"Row 2: Party 'ZED-2' is disabled. Enable it or use another Party."}) {
		t.Errorf("disabled = %q", got)
	}
	if got := l.lrImport(t, w, w.Kavya, "SHREE-X", true); !slices.Equal(got, []string{"Row 2: Party 'SHREE-X' not found. Use an existing Party code such as SHREE."}) {
		t.Errorf("unknown = %q", got)
	}
}

// A row names a code an earlier line of the same file created, after a line
// that made csvio read the candidates: the check reads it in the import's tx,
// the user made it, and neither memo goes stale.
func TestCD232_ImportLinksARowFromTheSameFile(t *testing.T) {
	var l lrLink
	for _, c := range lrLinks() {
		if c.name() == "employee.manager_id" {
			l = c
		}
	}
	w := nsNew(t)
	allmasters.RegisterImporters(w.svc)
	own := w.lrScoped(t, l)
	dt := registered(t, l.source.DocType)
	const header = "Code,Name,Branch,Reporting manager,Date of joining,Status\n"
	row := func(code, manager string) string { return code + ",Row,PUNE," + manager + ",01/04/2024,Active\n" }

	file := header + row("HAND-1", "ZED-1") + row("ZED-1", "") + row("HAND-2", "ZED-1") + row("HAND-3", "ZED-9")
	rep, err := othersImporter(w.Env).Preview(t.Context(), own, dt, strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"Row 2: Reporting manager 'ZED-1' not found.",
		"Row 5: Reporting manager 'ZED-9' not found. Use an existing Reporting manager code such as ZED-1.",
	}
	if got := othersMessages(rep); !slices.Equal(got, want) {
		t.Errorf("errors = %q, want %q", got, want)
	}

	file = header + row("BOSS-1", "") + row("HAND-4", "BOSS-1")
	rep, err = othersImporter(w.Env).Import(t.Context(), own, dt, strings.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	if got := othersMessages(rep); len(got) != 0 || rep.Imported != 2 {
		t.Errorf("imported %d, errors %q", rep.Imported, got)
	}
}

// CanLinkTx carries a Site's own id as its site value (CD-64), so a user
// restricted on sites reads their site and no other.
func TestCD227_CanLinkTxRestrictedSite(t *testing.T) {
	w := nsNew(t)
	own := w.lrScoped(t, lrLink{source: address.Kind, target: address.Kind})
	ids := []int64{}
	for _, id := range w.m.Sites {
		ids = append(ids, id)
	}
	if len(ids) < 2 {
		t.Fatalf("%d sites", len(ids))
	}
	if err := w.pgAuth(t).ChangeRestrictions(t.Context(), w.Admin, own.ID, auth.DimSite, ids[:1], nil, nil); err != nil {
		t.Fatalf("ChangeRestrictions: %v", err)
	}
	read(t, w.Env, func(ctx context.Context, tx *db.Tx) error {
		if err := w.Kit.CanLinkTx(ctx, tx, own, site.Kind, ids[0]); err != nil {
			t.Errorf("the assigned site: %v", err)
		}
		if err := w.Kit.CanLinkTx(ctx, tx, own, site.Kind, ids[1]); !errors.Is(err, doctype.ErrNotFound) {
			t.Errorf("another site = %v, want not found", err)
		}
		return nil
	})
}

// lrAllowedRecs are the calls that may pass a record outside mstkit's own
// files: its screens' page, after CanRowTx filled ref's created_by.
var lrAllowedRecs = map[string]int{filepath.Join("..", "mstkit", "web", "web.go"): 2}

// lrBareRecs lists the Can and CanTx uses in f that may pass a record: a call
// whose last argument isn't nil, and a method value, which could be called with
// anything. Only mstkit's CanRowTx, which reads created_by first, may (CD-227).
func lrBareRecs(fset *token.FileSet, f *ast.File) []string {
	var out []string
	called := map[*ast.SelectorExpr]bool{}
	isCan := func(sel *ast.SelectorExpr) bool { return sel.Sel.Name == "Can" || sel.Sel.Name == "CanTx" }
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.CallExpr:
			sel, ok := ast.Unparen(n.Fun).(*ast.SelectorExpr)
			if !ok || !isCan(sel) {
				return true
			}
			called[sel] = true
			if len(n.Args) == 0 {
				out = append(out, fset.Position(n.Pos()).String())
				return true
			}
			if last, ok := n.Args[len(n.Args)-1].(*ast.Ident); !ok || last.Name != "nil" {
				out = append(out, fset.Position(n.Pos()).String())
			}
		case *ast.SelectorExpr:
			if isCan(n) && !called[n] {
				out = append(out, fset.Position(n.Pos()).String())
			}
		}
		return true
	})
	return out
}

func TestCD227_ScannerFindsARecord(t *testing.T) {
	const src = `package p
func f() {
	e.Auth.Can(ctx, p, d, r, nil)
	e.Auth.CanTx(ctx, tx, p, d, r, nil)
	(a.Can)(ctx, p, d, r, nil)
	e.Auth.CanTx(ctx, tx, p, d, r, ref(id))
	a.Can(ctx, p, d, r, rec)
	(a.Can)(ctx, p, d, r, rec)
	g := a.CanTx
	g(ctx, tx, p, d, r, rec)
	h := (a.Can)
	h(ctx, p, d, r, nil)
	use(auth.Service.Can)
}`
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, at := range lrBareRecs(fset, f) {
		_, rest, _ := strings.Cut(at, ":")
		line, _, _ := strings.Cut(rest, ":")
		n, err := strconv.Atoi(line)
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, n)
	}
	slices.Sort(lines)
	if want := []int{6, 7, 8, 9, 11, 13}; !slices.Equal(lines, want) {
		t.Errorf("found lines %v, want %v", lines, want)
	}
}

// TestCD227_NoRecordOutsideMstkit: a stored row's rights go through CanRowTx or
// CanRow, or own scope sees created_by 0 and refuses the row's owner (CD-223).
func TestCD227_NoRecordOutsideMstkit(t *testing.T) {
	fset := token.NewFileSet()
	files := 0
	found := map[string]int{}
	err := filepath.WalkDir("..", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if filepath.Dir(path) == filepath.Join("..", "mstkit") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files++
		found[path] += len(lrBareRecs(fset, f))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 50 {
		t.Errorf("scanned %d files; the walk missed the masters", files)
	}
	for path, n := range found {
		if n != lrAllowedRecs[path] {
			t.Errorf("%s: %d uses of Can or CanTx that may pass a record, want %d; use mstkit's CanRowTx or CanRow", path, n, lrAllowedRecs[path])
		}
	}
	for path, n := range lrAllowedRecs {
		if found[path] != n {
			t.Errorf("%s: %d allowed uses, found %d", path, n, found[path])
		}
	}
}
