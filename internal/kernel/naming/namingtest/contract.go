// Package namingtest is the fake-vs-real contract suite for naming.Service
// and naming.Admin (WP3 Task 1, CD-182). The real Namer and namingfake both
// run it; every input the real service refuses, the fake must refuse with the
// same sentinel and message (CD-125, CD-163, CD-173).
package namingtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Setup arranges state through public paths only.
type Setup struct {
	// DB gives the write (and read) transactions the Service methods run in,
	// as DOC's engine does. For the real Namer it is the Namer's own database.
	DB *db.DB
	// Admin seeds series and counters (EnsureSeries, SetCounter, SetTemplate).
	Admin naming.Admin
	// By is an Administrator. User 4 (Priya Shah) must not be one.
	By auth.Principal
}

// Fixture DocTypes and series (00-fixtures.md; CD-113).
const (
	dtPO     = "fixture.Order"
	dtGRN    = "fixture.Receipt"
	dtNone   = "fixture.Unknown" // no series
	poLabel  = "Purchase Order"
	grnLabel = "GRN"
	poTpl    = "PO/{FY}/{#####}"
	grnTpl   = "GR/{FY}/{#####}"
)

// priya is a Purchase User, not an Administrator (NAM-060).
var priya = auth.Principal{ID: 4, Name: "Priya Shah", Roles: []string{"Purchase User", "Employee"}}

var errEffect = errors.New("namingtest: approval effect failed")

func day(y int, m time.Month, d int) types.Date { return types.Date{Year: y, Month: m, Day: d} }

// fixtureDay is the fixture clock's business date (24-10-2026, FY 26-27).
var fixtureDay = day(2026, time.October, 24)

// Run runs every contract behaviour; newSvc builds a fresh service per behaviour.
func Run(t *testing.T, newSvc func(t *testing.T) (naming.Service, Setup)) {
	t.Helper()
	for _, b := range behaviours {
		t.Run(b.name, func(t *testing.T) {
			svc, s := newSvc(t)
			c := &env{t: t, svc: svc, s: s, ctx: context.Background()}
			b.run(c)
		})
	}
}

type env struct {
	t   *testing.T
	svc naming.Service
	s   Setup
	ctx context.Context
}

var behaviours = []struct {
	name string
	run  func(c *env)
}{
	{"FirstNumber", firstNumber},
	{"FYResetUsesDocumentDate", fyReset},
	{"PerDocTypeCounters", perDocType},
	{"AssignInCallerTx", assignInCallerTx},
	{"RollbackLeavesNoGap", rollbackNoGap},
	{"GaplessUnderRollback", gaplessUnderRollback},
	{"PreviewDoesNotConsume", previewDoesNotConsume},
	{"AmendmentSuffixes", amendmentSuffixes},
	{"AmendmentHyphenatedPrefix", amendmentHyphenated},
	{"DraftLabel", draftLabel},
	{"ValidateTemplate", validateTemplate},
	{"Refuse/NoSeries", refuseNoSeries},
	{"Refuse/MissingDate", refuseMissingDate},
	{"Refuse/NeedsWriteTx", refuseNeedsTx},
	{"Refuse/NumberImmutable", refuseImmutable},
	{"Refuse/AmendmentLimit", refuseAmendLimit},
	{"Refuse/UnknownRoot", refuseUnknownRoot},
	{"Refuse/AlreadyAmended", refuseAlreadyAmended},
	{"Refuse/Exhausted", refuseExhausted},
	{"Refuse/AdminChanges", refuseAdmin},
}

// ───────── helpers ─────────

func (c *env) ensure(dt, label, tpl string) {
	c.t.Helper()
	if err := c.s.Admin.EnsureSeries(c.ctx, dt, label, tpl); err != nil {
		c.t.Fatalf("EnsureSeries(%s, %q, %q): %v", dt, label, tpl, err)
	}
}

func (c *env) defaults() {
	c.t.Helper()
	c.ensure(dtPO, poLabel, poTpl)
	c.ensure(dtGRN, grnLabel, grnTpl)
}

func (c *env) setCounter(dt, prefix string, last int64) {
	c.t.Helper()
	if err := c.s.Admin.SetCounter(c.ctx, c.s.By, dt, prefix, last); err != nil {
		c.t.Fatalf("SetCounter(%s, %s, %d): %v", dt, prefix, last, err)
	}
}

// assign runs Assign in its own committed write, as DOC's approve does.
func (c *env) assign(dt string, id int64, d types.Date) (string, error) {
	var out string
	err := c.s.DB.Write(c.ctx, func(tx *db.Tx) error {
		var err error
		out, err = c.svc.Assign(c.ctx, tx, dt, id, d)
		return err
	})
	return out, err
}

func (c *env) amend(dt string, id int64, root string, k int) (string, error) {
	var out string
	err := c.s.DB.Write(c.ctx, func(tx *db.Tx) error {
		var err error
		out, err = c.svc.AssignAmendment(c.ctx, tx, dt, id, root, k)
		return err
	})
	return out, err
}

// mustAssign fails the behaviour unless Assign gives want.
func (c *env) mustAssign(dt string, id int64, d types.Date, want string) {
	c.t.Helper()
	got, err := c.assign(dt, id, d)
	if err != nil || got != want {
		c.t.Errorf("Assign(%s, %d, %v) = %q, %v; want %q", dt, id, d, got, err, want)
	}
}

func (c *env) mustAmend(dt string, id int64, root string, k int, want string) {
	c.t.Helper()
	got, err := c.amend(dt, id, root, k)
	if err != nil || got != want {
		c.t.Errorf("AssignAmendment(%s, %d, %q, %d) = %q, %v; want %q", dt, id, root, k, got, err, want)
	}
}

func (c *env) wantPreview(dt string, d types.Date, want string) {
	c.t.Helper()
	got, err := c.svc.Preview(c.ctx, dt, d)
	if err != nil || got != want {
		c.t.Errorf("Preview(%s, %v) = %q, %v; want %q", dt, d, got, err, want)
	}
}

// counter is the last number for (dt, prefix) as Admin.List reports it; 0
// when there is no counter yet.
func (c *env) counter(dt, prefix string) int64 {
	c.t.Helper()
	all, err := c.s.Admin.List(c.ctx)
	if err != nil {
		c.t.Fatalf("Admin.List: %v", err)
	}
	for _, s := range all {
		if s.DocType == dt {
			return s.Counters[prefix]
		}
	}
	c.t.Fatalf("Admin.List has no series for %s", dt)
	return 0
}

func (c *env) wantCounter(dt, prefix string, want int64) {
	c.t.Helper()
	if got := c.counter(dt, prefix); got != want {
		c.t.Errorf("counter %s %q = %d; want %d", dt, prefix, got, want)
	}
}

// wantRefused checks err is sentinel with exactly msg.
func (c *env) wantRefused(call string, err, sentinel error, msg string) {
	c.t.Helper()
	if err == nil {
		c.t.Errorf("%s: got no error; want %v %q", call, sentinel, msg)
		return
	}
	if !errors.Is(err, sentinel) {
		c.t.Errorf("%s: error %v (%T) is not %v", call, err, err, sentinel)
	}
	if err.Error() != msg {
		c.t.Errorf("%s: message %q; want %q", call, err.Error(), msg)
	}
}

// ───────── behaviours ─────────

// NAM-023, NAM-024, NAM-030: the first approval in a fresh series is 00001,
// the next 00002.
func firstNumber(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 42, fixtureDay, "PO/26-27/00001")
	c.mustAssign(dtPO, 43, fixtureDay, "PO/26-27/00002")
	c.wantCounter(dtPO, "PO/26-27/", 2)
}

// NAM-004, NAM-005, NAM-006: the FY comes from the document date; a new FY
// prefix starts at 1 and the old prefix's counter stays and continues.
func fyReset(c *env) {
	c.defaults()
	c.setCounter(dtPO, "PO/25-26/", 143)
	c.mustAssign(dtPO, 1, day(2026, time.April, 1), "PO/26-27/00001")
	c.wantCounter(dtPO, "PO/25-26/", 143)
	c.mustAssign(dtPO, 2, day(2026, time.March, 31), "PO/25-26/00144")
	c.mustAssign(dtPO, 3, day(2026, time.April, 12), "PO/26-27/00002")
	c.wantCounter(dtPO, "PO/25-26/", 144)
	c.wantCounter(dtPO, "PO/26-27/", 2)
}

// NAM-028: each DocType has its own counter.
func perDocType(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	c.mustAssign(dtGRN, 1, fixtureDay, "GR/26-27/00001")
	c.mustAssign(dtPO, 2, fixtureDay, "PO/26-27/00002")
}

// NAM-024, D-NAM-02: Assign runs in the caller's write transaction; two
// assignments in one transaction are consecutive and both commit with it.
func assignInCallerTx(c *env) {
	c.defaults()
	var a, b string
	err := c.s.DB.Write(c.ctx, func(tx *db.Tx) error {
		var err error
		if a, err = c.svc.Assign(c.ctx, tx, dtPO, 1, fixtureDay); err != nil {
			return err
		}
		b, err = c.svc.Assign(c.ctx, tx, dtPO, 2, fixtureDay)
		return err
	})
	if err != nil || a != "PO/26-27/00001" || b != "PO/26-27/00002" {
		c.t.Errorf("two Assigns in one write = %q, %q, %v; want PO/26-27/00001, PO/26-27/00002", a, b, err)
	}
	c.wantCounter(dtPO, "PO/26-27/", 2)
	c.wantPreview(dtPO, fixtureDay, "PO/26-27/00003")
}

// NAM-031: a failed approval effect rolls the counter back; the next valid
// approval gets 00001.
func rollbackNoGap(c *env) {
	c.defaults()
	var got string
	err := c.s.DB.Write(c.ctx, func(tx *db.Tx) error {
		var err error
		if got, err = c.svc.Assign(c.ctx, tx, dtGRN, 7, fixtureDay); err != nil {
			return err
		}
		return errEffect
	})
	if !errors.Is(err, errEffect) {
		c.t.Fatalf("write with failing effect = %v (Assign gave %q); want the effect's error", err, got)
	}
	c.wantCounter(dtGRN, "GR/26-27/", 0)
	c.wantPreview(dtGRN, fixtureDay, "GR/26-27/00001")
	// The same document can be numbered after its failed attempt (NAM-031).
	c.mustAssign(dtGRN, 7, fixtureDay, "GR/26-27/00001")
}

// NAM-037, NAM-033: interleaved commits and rollbacks give exactly 1..N.
func gaplessUnderRollback(c *env) {
	c.defaults()
	want := []string{"PO/26-27/00001", "PO/26-27/00002", "PO/26-27/00003"}
	var got []string
	for i := range int64(5) {
		fail := i%2 == 1
		var num string
		err := c.s.DB.Write(c.ctx, func(tx *db.Tx) error {
			var err error
			if num, err = c.svc.Assign(c.ctx, tx, dtPO, 100+i, fixtureDay); err != nil {
				return err
			}
			if fail {
				return errEffect
			}
			return nil
		})
		switch {
		case fail && !errors.Is(err, errEffect):
			c.t.Fatalf("attempt %d: %v; want the effect's error", i, err)
		case !fail && err != nil:
			c.t.Fatalf("attempt %d: %v", i, err)
		case !fail:
			got = append(got, num)
		}
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		c.t.Errorf("committed numbers %q; want %q", got, want)
	}
	c.wantCounter(dtPO, "PO/26-27/", 3)
}

// NAM-036: preview shows the next number twice without using it.
func previewDoesNotConsume(c *env) {
	c.defaults()
	c.setCounter(dtPO, "PO/26-27/", 2)
	c.wantPreview(dtPO, fixtureDay, "PO/26-27/00003")
	c.wantPreview(dtPO, fixtureDay, "PO/26-27/00003")
	c.wantCounter(dtPO, "PO/26-27/", 2)
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00003")
}

// NAM-039, NAM-040: amendments are root-1, root-2 (not stacked) and never
// use the counter.
func amendmentSuffixes(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	c.mustAmend(dtPO, 2, "PO/26-27/00001", 1, "PO/26-27/00001-1")
	c.mustAmend(dtPO, 3, "PO/26-27/00001", 2, "PO/26-27/00001-2")
	c.wantCounter(dtPO, "PO/26-27/", 1)
	c.wantPreview(dtPO, fixtureDay, "PO/26-27/00002")
}

// NAM-041: the suffix comes from stored fields, not hyphen splitting.
func amendmentHyphenated(c *env) {
	c.ensure(dtPO, poLabel, "PO-{FY}-{#####}")
	c.mustAssign(dtPO, 1, fixtureDay, "PO-26-27-00001")
	c.mustAmend(dtPO, 2, "PO-26-27-00001", 1, "PO-26-27-00001-1")
	c.mustAmend(dtPO, 3, "PO-26-27-00001", 2, "PO-26-27-00001-2")
}

// NAM-022, CD-53: an unnumbered document is "Draft <id>"; a numbered one
// shows its number.
func draftLabel(c *env) {
	c.defaults()
	if got := naming.Label("", 42); got != "Draft 42" {
		c.t.Errorf(`Label("", 42) = %q; want "Draft 42"`, got)
	}
	num, err := c.assign(dtPO, 42, fixtureDay)
	if err != nil {
		c.t.Fatalf("Assign: %v", err)
	}
	if got := naming.Label(num, 42); got != "PO/26-27/00001" {
		c.t.Errorf("Label(%q, 42) = %q; want PO/26-27/00001", num, got)
	}
}

// NAM-009..NAM-015, NAM-017, NAM-018: template rules.
func validateTemplate(c *env) {
	for _, tpl := range []string{poTpl, "PO-{FY}-{#####}", "{FY}/PO/{###}", "P{YY}{MM}{####}", "GRN{######}", "EXP/{YYYY}/{####}"} {
		if err := c.svc.ValidateTemplate(tpl); err != nil {
			c.t.Errorf("ValidateTemplate(%q) = %v; want nil (NAM-018)", tpl, err)
		}
	}
	for _, r := range []struct{ tpl, msg string }{
		{"PO/{FY}/", "Series must have a number part, for example {#####}."},                                            // NAM-009
		{"PO/{###}/{#####}", "Series can have only one number part."},                                                   // NAM-010
		{"PO/{#####}/{FY}", "The number part must be at the end of the series."},                                        // NAM-011
		{"PO/{FY}/{##}", "The number part must have 3 to 6 digits."},                                                    // NAM-012
		{"PO/{XYZ}/{###}", "Unknown part {XYZ} in series. Use {FY}, {YYYY}, {YY}, {MM} or a number part like {#####}."}, // NAM-013
		{"PO {FY}/{###}", "Series can use only letters, digits, '-' and '/'."},                                          // NAM-014
		{"/PO/{###}", "Series must start with a letter or digit."},                                                      // NAM-015
		{"POR/{FY}/{#####}", "Numbers from this series would be 15 characters. Keep them to 14 or fewer (amendments add 2, and GST allows 16). Shorten the prefix or use fewer digits."}, // NAM-017
	} {
		c.wantRefused("ValidateTemplate("+r.tpl+")", c.svc.ValidateTemplate(r.tpl), naming.ErrInvalidTemplate, r.msg)
	}
}

// D-NAM-12, CD-66: a DocType with no series is refused by every Service call.
func refuseNoSeries(c *env) {
	c.defaults()
	const msg = "No numbering series is set up for fixture.Unknown. Ask an Administrator."
	_, err := c.assign(dtNone, 1, fixtureDay)
	c.wantRefused("Assign(unknown doctype)", err, naming.ErrNoSeries, msg)
	_, err = c.svc.Preview(c.ctx, dtNone, fixtureDay)
	c.wantRefused("Preview(unknown doctype)", err, naming.ErrNoSeries, msg)
	_, err = c.amend(dtNone, 2, "PO/26-27/00001", 1)
	c.wantRefused("AssignAmendment(unknown doctype)", err, naming.ErrNoSeries, msg)
}

// NAM-004 (Review Focus 1): a number needs the document date.
func refuseMissingDate(c *env) {
	c.defaults()
	const msg = "A document date is needed to give it a number."
	_, err := c.assign(dtPO, 1, types.Date{})
	c.wantRefused("Assign(zero date)", err, naming.ErrInvalidDate, msg)
	_, err = c.svc.Preview(c.ctx, dtPO, types.Date{})
	c.wantRefused("Preview(zero date)", err, naming.ErrInvalidDate, msg)
	c.wantCounter(dtPO, "PO/26-27/", 0)
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
}

// NAM-032 (P2; DOC relies on it, D-NAM-03): numbering runs only in a write
// transaction. A caller bug, so a plain internal error.
func refuseNeedsTx(c *env) {
	c.defaults()
	msg := naming.ErrNeedsTx.Error()
	_, err := c.svc.Assign(c.ctx, nil, dtPO, 1, fixtureDay)
	c.wantRefused("Assign(nil tx)", err, naming.ErrNeedsTx, msg)
	_, err = c.svc.AssignAmendment(c.ctx, nil, dtPO, 2, "PO/26-27/00001", 1)
	c.wantRefused("AssignAmendment(nil tx)", err, naming.ErrNeedsTx, msg)
	var rerr, aerr error
	if err := c.s.DB.Read(c.ctx, func(tx *db.Tx) error {
		_, rerr = c.svc.Assign(c.ctx, tx, dtPO, 1, fixtureDay)
		_, aerr = c.svc.AssignAmendment(c.ctx, tx, dtPO, 2, "PO/26-27/00001", 1)
		return nil
	}); err != nil {
		c.t.Fatalf("Read: %v", err)
	}
	c.wantRefused("Assign(read tx)", rerr, naming.ErrNeedsTx, msg)
	c.wantRefused("AssignAmendment(read tx)", aerr, naming.ErrNeedsTx, msg)
	c.wantCounter(dtPO, "PO/26-27/", 0)
}

// NAM-029, NAM-049: a numbered document is never numbered again, and the
// refused call uses no number.
func refuseImmutable(c *env) {
	c.defaults()
	const msg = "Document numbers can't be changed."
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	_, err := c.assign(dtPO, 1, fixtureDay)
	c.wantRefused("Assign(numbered doc)", err, naming.ErrNumberImmutable, msg)
	_, err = c.amend(dtPO, 1, "PO/26-27/00001", 1)
	c.wantRefused("AssignAmendment(numbered doc)", err, naming.ErrNumberImmutable, msg)
	c.wantCounter(dtPO, "PO/26-27/", 1)
	c.mustAssign(dtPO, 2, fixtureDay, "PO/26-27/00002")
}

// NAM-045 (D-NAM-08): at most nine amendments.
func refuseAmendLimit(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	_, err := c.amend(dtPO, 2, "PO/26-27/00001", 10)
	c.wantRefused("AssignAmendment(k=10)", err, naming.ErrAmendmentLimit,
		"PO/26-27/00001 has been amended 9 times, which is the limit. Create a new purchase order instead.")
}

// NAM-039, NAM-047: only a number issued for this DocType can be amended,
// and only in order (-2 needs -1).
func refuseUnknownRoot(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	c.mustAssign(dtGRN, 1, fixtureDay, "GR/26-27/00001")
	for _, r := range []struct {
		dt, root, label string
		k               int
	}{
		{dtPO, "PO/26-27/00099", poLabel, 1}, // never issued
		{dtPO, "GR/26-27/00001", poLabel, 1}, // issued for another DocType
		{dtPO, "PO/26-27/00001", poLabel, 2}, // skips -1
	} {
		_, err := c.amend(r.dt, 50, r.root, r.k)
		c.wantRefused("AssignAmendment("+r.root+")", err, naming.ErrUnknownRoot,
			r.root+" isn't a number issued for "+r.label+", so it can't be amended.")
	}
	c.wantCounter(dtPO, "PO/26-27/", 1)
}

// NAM-043: one amendment per root and suffix.
func refuseAlreadyAmended(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")
	c.mustAmend(dtPO, 2, "PO/26-27/00001", 1, "PO/26-27/00001-1")
	_, err := c.amend(dtPO, 3, "PO/26-27/00001", 1)
	c.wantRefused("AssignAmendment(second -1)", err, naming.ErrAlreadyAmended,
		"PO/26-27/00001 has already been amended as PO/26-27/00001-1. Open that document instead.")
}

// NAM-063 (D-NAM-07, CD-66): an exhausted series refuses and stays put.
func refuseExhausted(c *env) {
	c.ensure(dtGRN, grnLabel, "GR/{FY}/{###}")
	c.setCounter(dtGRN, "GR/26-27/", 999)
	const msg = "Series GR/26-27/ has used all 999 numbers. Ask an Administrator to add a digit to the series."
	_, err := c.assign(dtGRN, 1, fixtureDay)
	c.wantRefused("Assign(exhausted)", err, naming.ErrExhausted, msg)
	_, err = c.svc.Preview(c.ctx, dtGRN, fixtureDay)
	c.wantRefused("Preview(exhausted)", err, naming.ErrExhausted, msg)
	c.wantCounter(dtGRN, "GR/26-27/", 999)
}

// NAM-054, NAM-057, NAM-060: Admin refuses invalid templates, non-admins and
// a counter below the highest issued number, and changes nothing.
func refuseAdmin(c *env) {
	c.defaults()
	c.mustAssign(dtPO, 1, fixtureDay, "PO/26-27/00001")

	err := c.s.Admin.SetTemplate(c.ctx, c.s.By, dtPO, "PO {FY}/{##}")
	c.wantRefused("SetTemplate(invalid)", err, naming.ErrInvalidTemplate, "Series can use only letters, digits, '-' and '/'.")
	err = c.s.Admin.SetTemplate(c.ctx, priya, dtPO, "PR/{FY}/{#####}")
	c.wantRefused("SetTemplate(by Priya)", err, auth.ErrForbidden, naming.MsgForbidden)
	err = c.s.Admin.SetCounter(c.ctx, priya, dtPO, "PO/26-27/", 50)
	c.wantRefused("SetCounter(by Priya)", err, auth.ErrForbidden, naming.MsgForbidden)
	err = c.s.Admin.SetCounter(c.ctx, c.s.By, dtPO, "PO/26-27/", 0)
	c.wantRefused("SetCounter(below issued)", err, naming.ErrCounterBelowIssued,
		"PO/26-27/ has already issued numbers up to 00001. Set 1 or more.")

	c.wantCounter(dtPO, "PO/26-27/", 1)
	c.mustAssign(dtPO, 2, fixtureDay, "PO/26-27/00002")
}
