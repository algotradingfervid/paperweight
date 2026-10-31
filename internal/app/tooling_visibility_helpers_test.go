package app_test

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/kernel/approval"
	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/files"
	"github.com/vardhan/internaltools/internal/kernel/files/filestest"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/modules/expense"
	"github.com/vardhan/internaltools/internal/modules/purchase"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/testkit"
	"github.com/vardhan/internaltools/internal/types"
)

// The visibility sweep's seed (TOOLING T2b; CD-233, CD-240, CD-248). Every text
// a hidden record carries holds hiddenToken and every text a control carries
// holds controlToken, so a body that names a hidden record is found by one
// substring, whatever the page cuts the text to.
const (
	hiddenToken  = "ZQHIDE"
	controlToken = "ZQSHOW"
	// The search and picker queries are inner parts of the tokens: a page that
	// echoes its query then doesn't name the token.
	hiddenQuery  = "QHID"
	controlQuery = "QSHO"

	sharedInvoice = "INV-SWEEP-1"

	viewerRole  = "Tooling sweep viewer"
	wideRole    = "Tooling sweep wide approver"
	wideEmail   = "kavya@vardhan.test" // EMP-006
	wideID      = auth.UserID(6)
	viewerEmail = deepakEmail // linked to EMP-007, so own scope has an employee

	projectA, siteA = "PRJ-METRO", "METRO-SITE"
	projectB, siteB = "PRJ-TECHPARK", "TECH-SITE"
	hiddenVendor    = "RAJKOT" // used only by hidden documents
	controlVendor   = "SHREE"
	// approverID (Meena, Director) also holds viewerRole, the approver slot
	// of every rule here, and reads every record: the records go approved and
	// rejected through her.
	approverID     = auth.UserID(2)
	hiddenEmployee = "EMP-005" // Suresh: the viewer's own scope leaves him out
	viewerEmployee = "EMP-007"
	// distinctive is the smallest amount or rate taken as a marker: the
	// controls stay under it, so a marker never matches a control's value.
	distinctive = types.Money(1000000) // ₹10,000.00
	// hiddenQty is every hidden line's quantity: not 1, so a line's rate and
	// amount differ, and long enough to be a marker.
	hiddenQty = "12.345"
	// fixtureOrderCounter moves fixture.Order's PO/ series past
	// purchase.Order's, which shares its prefix (CD-295).
	fixtureOrderCounter = 50000
)

// seedState is where add takes a record.
type seedState string

const (
	stDraft     seedState = "draft"
	stPending   seedState = "pending"      // submitted by the admin, waiting for viewerRole
	stMine      seedState = "pending-mine" // submitted by the viewer
	stApproved  seedState = "approved"
	stRejected  seedState = "rejected"
	stCancelled seedState = "cancelled"
	stClosed    seedState = "closed"
)

// sweepRecord is one seeded document.
type sweepRecord struct {
	dt, label string // label: what the record is, for messages
	id        int64
	hidden    bool
	state     seedState
	doc       doc.Doc
	amount    types.Money // the DocType's Amount hook: what the inbox shows
	eligible  bool        // the viewer is one of its eligible approvers
	wideOK    bool        // the wide approver is one of its eligible approvers
	file      int64       // an attachment's id, 0 = none
}

// sweepSeed is the seeded world the sweep reads as the viewer.
type sweepSeed struct {
	w        *world
	admin    auth.Principal
	viewer   auth.Principal
	wide     auth.Principal // the wide approver
	approver auth.Principal
	m        fixture.Masters
	records  []*sweepRecord
	ids      map[string]map[string]int64 // master table → code → id
}

func (s *sweepSeed) id(table, code string) int64 {
	s.w.t.Helper()
	if m, ok := s.ids[table]; ok {
		if id, ok := m[code]; ok {
			return id
		}
	}
	id, err := strconv.ParseInt(s.w.text(`SELECT CAST(id AS TEXT) FROM `+table+` WHERE code = ?`, code), 10, 64)
	if err != nil {
		s.w.t.Fatal(err)
	}
	if s.ids[table] == nil {
		s.ids[table] = map[string]int64{}
	}
	s.ids[table][code] = id
	return id
}

func (s *sweepSeed) idText(table, code string) string { return idText(s.id(table, code)) }

// noRights are the DocTypes the viewer holds no right on: a record of theirs
// is hidden by the missing grant, not by scope or restriction.
var noRights = map[string]bool{fixture.Receipt: true}

// viewerRights is what viewerRole grants on dt: read, plus write, print and
// export so the check step, print and CSV pages open, plus submit and approve
// where the DocType is approvable, so the viewer holds the rules' approver
// slot and has requests of its own. Own scope where the DocType has an
// employee, all otherwise; the project restriction applies on top.
func viewerRights(dt *doctype.DocType) map[auth.Right]auth.Scope {
	scope := auth.ScopeAll
	if dt.EmployeeField != "" {
		scope = auth.ScopeOwn
	}
	want := []auth.Right{auth.RightRead, auth.RightWrite, auth.RightPrint, auth.RightExport}
	if dt.Approvable {
		want = append(want, auth.RightSubmit, auth.RightApprove)
	}
	out := map[auth.Right]auth.Scope{}
	for _, r := range want {
		if auth.Applies(dt, r) {
			out[r] = scope
		}
	}
	return out
}

// sweepSeeders seed each registered module DocType, in this order (orders
// link to requests). TestOnly DocTypes (CD-291) are not in the release: only
// seedCauses uses two of them, for the roll-up history no module has yet.
var sweepSeeders = []struct {
	name string
	seed func(*sweepSeed)
}{
	{purchase.RequestDocType, seedRequests},
	{purchase.DocType, seedOrders},
	{expense.AdvanceDocType, seedAdvances},
	{expense.ClaimDocType, seedClaims},
}

// seedSweep builds the world: the canonical seed; the viewer (Deepak with only
// viewerRole, restricted to project A); Meena, who also holds viewerRole;
// a ₹0 rule with viewerRole as approver on every approvable module DocType;
// and for every registered module DocType a control set the viewer may read
// and a hidden set it may not, in every state the DocType has.
func seedSweep(t *testing.T) *sweepSeed {
	t.Helper()
	w := newWorld(t)
	admin, _, m := w.canonical()
	s := &sweepSeed{w: w, admin: admin, m: m, ids: map[string]map[string]int64{}}
	ctx := context.Background()
	if err := w.app.Auth().CreateRole(ctx, admin, viewerRole); err != nil {
		t.Fatalf("create the viewer role: %v", err)
	}
	for _, dt := range doctype.All() {
		if noRights[dt.Name] {
			continue
		}
		if err := w.app.Auth().SetRights(ctx, admin, viewerRole, dt.Name, viewerRights(dt)); err != nil {
			t.Fatalf("viewer rights on %s: %v", dt.Name, err)
		}
	}
	roles := []string{viewerRole}
	if _, err := w.app.Auth().UpdateUser(ctx, admin, deepakID, auth.UserPatch{Roles: &roles}); err != nil {
		t.Fatalf("viewer roles: %v", err)
	}
	roles = []string{"Director", viewerRole}
	if _, err := w.app.Auth().UpdateUser(ctx, admin, approverID, auth.UserPatch{Roles: &roles}); err != nil {
		t.Fatalf("approver roles: %v", err)
	}
	if err := w.app.Auth().ChangeRestrictions(ctx, admin, deepakID, auth.DimProject, []int64{s.id("project", projectA)}, nil, nil); err != nil {
		t.Fatalf("restrict the viewer: %v", err)
	}
	// The wide approver holds viewerRole's slot and read own on claims, plus
	// wideRole's approve:all on claims: approve wider than read, so the inbox
	// must still leave out a claim it can't read (approval canApprove and
	// eligible's readers filter).
	if err := w.app.Auth().CreateRole(ctx, admin, wideRole); err != nil {
		t.Fatalf("create the wide role: %v", err)
	}
	if err := w.app.Auth().SetRights(ctx, admin, wideRole, expense.ClaimDocType, map[auth.Right]auth.Scope{auth.RightRead: auth.ScopeOwn, auth.RightApprove: auth.ScopeAll}); err != nil {
		t.Fatalf("wide role rights: %v", err)
	}
	roles = []string{viewerRole, wideRole}
	if _, err := w.app.Auth().UpdateUser(ctx, admin, wideID, auth.UserPatch{Roles: &roles}); err != nil {
		t.Fatalf("wide approver roles: %v", err)
	}
	if err := w.app.Auth().ChangeRestrictions(ctx, admin, wideID, auth.DimProject, []int64{s.id("project", projectA)}, nil, nil); err != nil {
		t.Fatalf("restrict the wide approver: %v", err)
	}
	s.wide = w.principal(wideID)
	s.viewer = w.principal(deepakID)
	s.approver = w.principal(approverID)

	listed := map[string]bool{}
	for _, sd := range sweepSeeders {
		listed[sd.name] = true
	}
	for _, dt := range doctype.All() {
		if dt.IsDocument() && !dt.TestOnly && !listed[dt.Name] {
			t.Fatalf("document DocType %s has no seeder in sweepSeeders: add one, so the visibility sweep covers it", dt.Name)
		}
		if dt.IsDocument() && !dt.TestOnly && dt.Approvable {
			// APR asks AUTH about the role as the signed-in user (app.go roleDirectory).
			if _, err := w.app.Approvals().CreateRule(auth.WithPrincipal(ctx, admin), admin, approval.RuleInput{DocType: dt.Name, ApproverType: approval.ApproverRole, Role: viewerRole}); err != nil {
				t.Fatalf("approval rule on %s: %v", dt.Name, err)
			}
		}
	}
	for _, sd := range sweepSeeders {
		sd.seed(s)
	}
	seedCauses(s)
	s.reload()
	return s
}

// act runs one engine action as p.
func (s *sweepSeed) act(p auth.Principal, dt string, id int64, a doctype.Action, in doc.ActionInput) doc.Result {
	s.w.t.Helper()
	res, err := s.w.app.Engine().Do(context.Background(), p, dt, id, a, in)
	if err != nil {
		s.w.t.Fatalf("%s %s %d as %s: %v", a, dt, id, p.Name, err)
	}
	return res
}

// add saves a draft as the admin and takes it to st.
func (s *sweepSeed) add(dt string, hidden bool, st seedState, in doc.ActionInput) *sweepRecord {
	s.w.t.Helper()
	res := s.act(s.admin, dt, 0, doctype.ActionSave, in)
	reason := doc.ActionInput{Reason: tokenOf(hidden) + "-reason-" + string(st)}
	step := func(p auth.Principal, a doctype.Action, in doc.ActionInput) {
		in.Version = res.Version
		res = s.act(p, dt, res.ID, a, in)
	}
	switch st {
	case stDraft:
	case stMine:
		step(s.viewer, doctype.ActionSubmit, doc.ActionInput{})
	default:
		step(s.admin, doctype.ActionSubmit, doc.ActionInput{})
	}
	switch st {
	case stApproved, stCancelled, stClosed:
		if res.State == doctype.StatePending {
			step(s.approver, doctype.ActionApprove, doc.ActionInput{})
		}
	case stRejected:
		step(s.approver, doctype.ActionReject, reason)
	}
	switch st {
	case stCancelled:
		step(s.admin, doctype.ActionCancel, reason)
	case stClosed:
		step(s.admin, doctype.ActionClose, reason)
	}
	want := map[seedState]doctype.State{
		stDraft: doctype.StateDraft, stPending: doctype.StatePending, stMine: doctype.StatePending, stApproved: doctype.StateApproved,
		stRejected: doctype.StateRejected, stCancelled: doctype.StateCancelled, stClosed: doctype.StateClosed,
	}[st]
	if res.State != want {
		s.w.t.Fatalf("%s %d: state %s, want %s", dt, res.ID, res.State, want)
	}
	r := &sweepRecord{dt: dt, label: dt + " " + string(st), id: res.ID, hidden: hidden, state: st}
	s.records = append(s.records, r)
	return r
}

// reload reads every record's stored values, after the whole seed.
func (s *sweepSeed) reload() {
	s.w.t.Helper()
	ctx := context.Background()
	err := s.w.app.DB().Read(ctx, func(tx *db.Tx) error {
		for _, r := range s.records {
			d, err := doc.LoadDoc(ctx, tx, r.dt, r.id)
			if err != nil {
				return err
			}
			r.doc = d
			dt, _ := doctype.Lookup(r.dt)
			if r.amount, err = dt.Hooks.Amount(ctx, tx, r.id); err != nil {
				return err
			}
			if d.State == doctype.StatePending {
				users, err := s.w.app.Approvals().EligibleApprovers(ctx, tx, r.dt, r.id)
				if err != nil {
					return err
				}
				r.eligible = slices.Contains(users, s.viewer.ID)
				r.wideOK = slices.Contains(users, s.wide.ID)
			}
		}
		return nil
	})
	if err != nil {
		s.w.t.Fatalf("load the seeded records: %v", err)
	}
}

// readErr is the viewer's read check on r, through the real auth.Service.
func (s *sweepSeed) readErr(r *sweepRecord) error {
	s.w.t.Helper()
	ctx := context.Background()
	var got error
	err := s.w.app.DB().Read(ctx, func(tx *db.Tx) error {
		info, err := s.w.app.Engine().Info(ctx, tx, r.dt, r.id)
		if err != nil {
			return err
		}
		rec := auth.RecordRef{DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy, EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs}
		got = s.w.app.Auth().CanTx(ctx, tx, s.viewer, r.dt, auth.RightRead, &rec)
		return nil
	})
	if err != nil {
		s.w.t.Fatalf("info of %s %d: %v", r.dt, r.id, err)
	}
	return got
}

// checkReadable asserts that the viewer reads every control and no hidden
// record, so the sweep's split is the auth.Service's.
func (s *sweepSeed) checkReadable(t *testing.T) {
	t.Helper()
	for _, r := range s.records {
		err := s.readErr(r)
		switch {
		case r.hidden && !errors.Is(err, auth.ErrForbidden) && !errors.Is(err, auth.ErrNotFound):
			t.Errorf("viewer read on hidden %s %d = %v, want ErrForbidden or ErrNotFound (hidden reads as missing)", r.dt, r.id, err)
		case !r.hidden && err != nil:
			t.Errorf("viewer read on control %s %d = %v, want nil", r.dt, r.id, err)
		}
	}
}

// attach adds a PDF named after the record to its draft, as the admin.
func (s *sweepSeed) attach(r *sweepRecord) {
	s.w.t.Helper()
	ctx := context.Background()
	f, err := s.w.app.Files().Spool(bytes.NewReader(filestest.PDF(int(r.id))), files.MaxFileBytes)
	if err != nil {
		s.w.t.Fatal(err)
	}
	err = s.w.app.DB().Write(ctx, func(tx *db.Tx) error {
		r.file, err = s.w.app.Files().Attach(ctx, tx, s.admin, auth.RecordRef{DocType: r.dt, ID: r.id}, tokenOf(r.hidden)+"-receipt-"+idText(r.id)+".pdf", f)
		return err
	})
	if err != nil {
		s.w.t.Fatalf("attach to %s %d: %v", r.dt, r.id, err)
	}
}

func tokenOf(hidden bool) string {
	if hidden {
		return hiddenToken
	}
	return controlToken
}

// text is a record's free text: the token, then what it is.
func text(hidden bool, what string) string { return tokenOf(hidden) + "-" + what }

func (s *sweepSeed) find(dt string, hidden bool, st seedState) *sweepRecord {
	s.w.t.Helper()
	for _, r := range s.records {
		if r.dt == dt && r.hidden == hidden && r.state == st {
			return r
		}
	}
	s.w.t.Fatalf("no %s %s (hidden %v) seeded", dt, st, hidden)
	return nil
}

// firstLineID is the id of record r's line 1.
func (s *sweepSeed) firstLineID(r *sweepRecord) string {
	s.w.t.Helper()
	var d doc.Doc
	err := s.w.app.DB().Read(context.Background(), func(tx *db.Tx) error {
		var err error
		d, err = doc.LoadDoc(context.Background(), tx, r.dt, r.id)
		return err
	})
	if err != nil || len(d.Lines) == 0 {
		s.w.t.Fatalf("lines of %s %d: %v", r.dt, r.id, err)
	}
	return idText(d.Lines[0].ID)
}

// seedRequests: the control approved request is the one a hidden order's
// line orders (seedOrders).
func seedRequests(s *sweepSeed) {
	request := func(hidden bool, st seedState) {
		project, site, qty := projectA, siteA, "1"
		if hidden {
			project, site, qty = projectB, siteB, hiddenQty
		}
		s.add(purchase.RequestDocType, hidden, st, doc.ActionInput{
			Values: map[string]string{
				"site_id": s.idText("site", site), "project_id": s.idText("project", project),
				"request_date": "2026-10-24", "purpose": text(hidden, "pr-"+string(st)),
			},
			Lines: []map[string]string{{
				"item_id": s.idText("item", "MS-ANGLE-50"), "uom_id": s.idText("uom", "KGS"), "qty": qty,
				"description": text(hidden, "pr-line"), "note": text(hidden, "pr-note"),
			}},
		})
	}
	for _, st := range []seedState{stDraft, stApproved, stClosed} {
		request(false, st)
		request(true, st)
	}
}

// seedOrders: one hidden approved order's line orders the control approved
// request's line (request_line_id, register.go Links), so the request's page
// has a linked record the viewer can't read.
func seedOrders(s *sweepSeed) {
	linked := s.firstLineID(s.find(purchase.RequestDocType, false, stApproved))
	order := func(hidden bool, st seedState, price, requestLine string) {
		project, site, vendor, qty := projectA, siteA, controlVendor, "1"
		if hidden {
			project, site, vendor, qty = projectB, siteB, hiddenVendor, hiddenQty
		}
		line := map[string]string{
			"item_id": s.idText("item", "MS-ANGLE-50"), "uom_id": s.idText("uom", "KGS"), "qty": qty, "price": price,
			"tax_code_id": s.idText("tax_code", "GST18"), "hsn_sac": "7216", "description": text(hidden, "po-line"),
		}
		if requestLine != "" {
			line["request_line_id"] = requestLine
		}
		s.add(purchase.DocType, hidden, st, doc.ActionInput{
			Values: map[string]string{
				"vendor_id": s.idText("party", vendor), "site_id": s.idText("site", site), "project_id": s.idText("project", project),
				"order_date": "2026-10-24", "remarks": text(hidden, "po-"+string(st)), "vendor_quote_ref": text(hidden, "quote"),
			},
			Lines: []map[string]string{line},
		})
	}
	for i, st := range []seedState{stDraft, stApproved, stPending, stMine, stRejected, stClosed} {
		order(false, st, strconv.Itoa(1111+111*i)+".10", "")
	}
	for i, st := range []seedState{stDraft, stApproved, stCancelled, stPending, stRejected, stClosed} {
		order(true, st, strconv.Itoa(67891+i)+".19", "")
	}
	order(true, stApproved, "67999.19", linked)
	s.records[len(s.records)-1].label += " (orders the control request's line)"
}

func (s *sweepSeed) employee(hidden bool) string {
	if hidden {
		return s.idText("employee", hiddenEmployee)
	}
	return s.idText("employee", viewerEmployee)
}

func seedAdvances(s *sweepSeed) {
	advance := func(hidden bool, st seedState, amount string) {
		s.add(expense.AdvanceDocType, hidden, st, doc.ActionInput{
			Values: map[string]string{"employee_id": s.employee(hidden), "advance_date": "2026-10-24", "purpose": text(hidden, "ea-"+string(st))},
			Lines: []map[string]string{{
				"expense_type_id": s.idText("expense_type", "TRAVEL"), "line_amount": amount, "description": text(hidden, "ea-line"),
			}},
		})
	}
	advance(false, stApproved, "2333.30")
	for i, st := range []seedState{stDraft, stApproved, stPending, stRejected, stClosed} {
		advance(true, st, strconv.Itoa(876541+i)+".21")
	}
}

// seedClaims: only the hidden approved claim and the control draft share a
// supplier GSTIN and invoice no., so the control's duplicate warning must not
// name the hidden claim (CD-264 (4)). At a04f8f3d no page shows
// ClaimWarnings yet; the check and approval pages are swept once they do.
func seedClaims(s *sweepSeed) {
	claim := func(hidden bool, st seedState, amount, invoice string) *sweepRecord {
		return s.add(expense.ClaimDocType, hidden, st, doc.ActionInput{
			Values: map[string]string{
				"employee_id": s.employee(hidden), "claim_date": "2026-10-24",
				"purpose": text(hidden, "ex-"+string(st)), "remarks": text(hidden, "ex-remarks"),
			},
			Lines: []map[string]string{{
				"expense_date": "2026-10-24", "expense_type_id": s.idText("expense_type", "TRAVEL"), "amount": amount,
				"description": text(hidden, "ex-line"), "no_receipt_reason": text(hidden, "no-receipt"),
				"supplier_gstin": "27AABCS1429B1ZU", "invoice_no": invoice,
			}},
		})
	}
	s.attach(claim(false, stDraft, "3444.40", sharedInvoice))
	claim(false, stApproved, "3555.50", "INV-SWEEP-3")
	s.attach(claim(true, stDraft, "765432.19", "INV-SWEEP-2"))
	claim(true, stApproved, "765431.19", sharedInvoice)
	for i, st := range []seedState{stPending, stRejected, stClosed} {
		claim(true, st, strconv.Itoa(765421+i)+".19", "INV-SWEEP-5"+strconv.Itoa(i))
	}
}

// seedCauses gives a control document a roll-up caused by a document the
// viewer can't read, so its history has a cause to hide (CD-248 (1)). No
// module DocType has a RollUp yet, so it uses the fixture pair: a hidden
// fixture.Receipt (the viewer holds no right on it) receiving a control
// fixture.Order's line on project A.
func seedCauses(s *sweepSeed) {
	s.w.t.Helper()
	if s.m.Project[projectA] != s.id("project", projectA) {
		s.w.t.Fatalf("fixture project %s id %d != real project id %d: the restriction would not apply to the fixture order", projectA, s.m.Project[projectA], s.id("project", projectA))
	}
	namer := naming.New(s.w.app.DB(), testkit.Clock(), s.w.app.Auth(), audit.Default)
	if err := namer.SetCounter(context.Background(), s.admin, fixture.Order, "PO/26-27/", fixtureOrderCounter); err != nil {
		s.w.t.Fatalf("move fixture.Order's counter: %v", err)
	}
	order := s.add(fixture.Order, false, stApproved, doc.ActionInput{
		Values: map[string]string{
			"supplier_id": idText(s.m.Party[controlVendor]), "project_id": idText(s.m.Project[projectA]),
			"order_date": "2026-10-24", "amount": "4321.00", "remarks": text(false, "fo"),
		},
		Lines: []map[string]string{{"item_id": idText(s.m.Item["MS-ANGLE-50"]), "qty": "5", "rate": "10.0000", "uom": "kg", "description": text(false, "fo-line")}},
	})
	s.add(fixture.Receipt, true, stApproved, doc.ActionInput{
		Values: map[string]string{"receipt_date": "2026-10-24", "remarks": text(true, "gr")},
		Lines:  []map[string]string{{"order_line_id": s.firstLineID(order), "qty": "2"}},
	})
	s.records[len(s.records)-1].label += " (receives the control fixture order's line)"
}

// markers are what a page must never show to the viewer for record r: its
// number, its free texts (one hiddenToken substring finds them), every amount
// and rate from distinctive up and every quantity of 3 decimals, in each
// format the app prints, and its URLs.
func (r *sweepRecord) markers() []string {
	var out []string
	if r.doc.Number != "" {
		out = append(out, r.doc.Number)
	}
	values := func(v doc.Values) {
		for _, x := range v {
			switch x := x.(type) {
			case types.Money:
				if x >= distinctive {
					out = append(out, types.FormatMoney(x), x.Plain(), western(x.Plain()))
				}
			case types.Rate:
				if x >= types.Rate(distinctive)*100 { // Rate has 4 decimals
					out = append(out, types.FormatRate(x), x.Plain(), western(x.Plain()))
				}
			case types.Qty:
				if s := x.Plain(); len(s) >= len(hiddenQty) {
					out = append(out, types.FormatQty(x), s)
				}
			}
		}
	}
	values(r.doc.Header)
	for _, l := range r.doc.Lines {
		values(l.Values)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// urlPattern finds a link to r: its document, print or attachment URL.
func (r *sweepRecord) urlPattern() *regexp.Regexp {
	alts := []string{`/(doc|print)/` + regexp.QuoteMeta(r.dt) + `/` + idText(r.id)}
	if r.file != 0 {
		alts = append(alts, `/files/`+idText(r.file))
	}
	return regexp.MustCompile(`(` + strings.Join(alts, "|") + `)([^0-9]|$)`)
}

// western is a plain number grouped in thousands, "987,654.32".
func western(plain string) string {
	intPart, frac, _ := strings.Cut(plain, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac == "" {
		return b.String()
	}
	return b.String() + "." + frac
}
