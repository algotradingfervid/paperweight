package modkit_test

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vardhan/internaltools/internal/fixture"
	"github.com/vardhan/internaltools/internal/fixture/fixturetest"
	"github.com/vardhan/internaltools/internal/kernel/audit/auditfake"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/auth/authtest"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/clock"
	"github.com/vardhan/internaltools/internal/testkit"
)

// PO spec §22 and PO-061 on the real auth.Service and the real document
// engine as the InfoSource. fixture.Order opts in to comments; fixture.Claim
// stays out, so it answers like a missing record.

const (
	msgCommentEmpty   = "Write a comment first."
	msgCommentTooLong = "A comment can be at most 1,000 characters."

	nameMeena = "Meena Iyer"
	nameRavi  = "Ravi Kulkarni"
	namePriya = "Priya Shah"
)

func TestMain(m *testing.M) {
	modkit.RegisterComments(fixture.Order)
	os.Exit(m.Run())
}

// cmEnv is the fixture harness (its engine and fake AUTH only make the
// documents) with Comments on the real AUTH and a clock the test moves.
type cmEnv struct {
	*fixturetest.H
	Clock *clock.Fixed
	C     *modkit.Comments
}

// newCommentsEnv seeds the default rights matrix, so Priya (Purchase User)
// reads every order but is restricted to PRJ-METRO, Ravi (Purchase Manager)
// and Meena (Director) read every order, and Deepak (Employee) has no read on
// orders at all.
func newCommentsEnv(t *testing.T) *cmEnv {
	t.Helper()
	ctx := t.Context()
	h := fixturetest.New(t)
	info := doc.NewEngine(doc.Deps{DB: h.DB, Clock: h.Clock, Naming: h.Naming, Numbers: fixturetest.Numbers(), Files: h.Files})
	a := auth.New(auth.Deps{
		DB: h.DB, Clock: h.Clock, Directory: testkit.Directory(h.F), Audit: auditfake.New(), Refs: &authtest.Refs{}, Info: info,
	})
	if err := a.SeedDefaults(ctx, auth.SeedOptions{}); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}
	if err := a.ChangeRestrictions(ctx, h.Admin, h.Priya.ID, auth.DimProject, []int64{h.M.Project["PRJ-METRO"]}, nil, nil); err != nil {
		t.Fatalf("restrict Priya: %v", err)
	}
	ck := testkit.Clock()
	c := modkit.NewComments(modkit.CommentsDeps{DB: h.DB, Auth: a, Info: info, Clock: ck})
	return &cmEnv{H: h, Clock: ck, C: c}
}

// order makes a draft order owned by p on project.
func (e *cmEnv) order(t *testing.T, p auth.Principal, project string) int64 {
	t.Helper()
	return e.NewOrderWith(t, p, map[string]string{"project_id": strconv.FormatInt(e.M.Project[project], 10)}).ID
}

// add is AddComment that has to succeed.
func (e *cmEnv) add(t *testing.T, p auth.Principal, dt string, id int64, body string) modkit.Comment {
	t.Helper()
	c, err := e.C.AddComment(t.Context(), p, dt, id, body)
	if err != nil {
		t.Fatalf("AddComment(%s %d, %q) as %s: %v, want ok", dt, id, body, p.Name, err)
	}
	return c
}

// rows counts module_comment.
func (e *cmEnv) rows(t *testing.T) int {
	t.Helper()
	var n int
	if err := e.DB.SQL().QueryRowContext(t.Context(), `SELECT count(*) FROM module_comment`).Scan(&n); err != nil {
		t.Fatalf("count module_comment: %v", err)
	}
	return n
}

// statusErr is any error carrying an HTTP status.
type statusErr interface {
	error
	Status() int
}

// wantNotFound fails unless err is AUTH-108's 404: errors.Is
// auth.ErrNotFound, status 404 and exactly auth.MsgNotFound (CD-245).
func wantNotFound(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, auth.ErrNotFound) {
		t.Errorf("%s: err = %v (%T), want errors.Is auth.ErrNotFound", what, err, err)
		return
	}
	var se statusErr
	if !errors.As(err, &se) || se.Status() != 404 {
		t.Errorf("%s: err %v has no status 404", what, err)
	}
	if err.Error() != auth.MsgNotFound {
		t.Errorf("%s: text = %q, want %q", what, err.Error(), auth.MsgNotFound)
	}
}

// wantInvalid fails unless err is a 422 with exactly msg.
func wantInvalid(t *testing.T, what string, err error, msg string) {
	t.Helper()
	var se statusErr
	if !errors.As(err, &se) {
		t.Errorf("%s: err = %v (%T), want a 422 %q", what, err, err, msg)
		return
	}
	if se.Status() != 422 || err.Error() != msg {
		t.Errorf("%s: got %d %q, want 422 %q", what, se.Status(), err.Error(), msg)
	}
}

// bodies returns each comment's body, in order.
func bodies(cs []modkit.Comment) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Body
	}
	return out
}

func eqStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") || len(got) != len(want) {
		t.Errorf("%s =\n  %q\nwant\n  %q", what, got, want)
	}
}

// PO-061: read is needed to add and list; a hidden record, a missing one and
// a DocType that didn't opt in all answer AUTH-108's 404; the body rules and
// their messages; the table is append-only.
func TestComments_Rules(t *testing.T) {
	e := newCommentsEnv(t)
	ctx := t.Context()
	metro := e.order(t, e.Priya, "PRJ-METRO")
	techpark := e.order(t, e.Ravi, "PRJ-TECHPARK") // hidden from Priya by her restriction
	claim := e.NewClaim(t, e.Priya, "EMP-004", fixturetest.CL{Type: "Travel", Amount: "1500.00"}).ID

	// Added comments come back trimmed, with the author's name and the clock's time.
	t0 := e.Clock.Now()
	first := e.add(t, e.Priya, fixture.Order, metro, "  Checked the rate with the vendor.\n")
	if first.ID <= 0 || first.DocType != fixture.Order || first.RecordID != metro ||
		first.Body != "Checked the rate with the vendor." || first.CreatedBy != e.Priya.ID ||
		first.CreatedByName != namePriya || !first.CreatedAt.Equal(t0) {
		t.Errorf("AddComment = %+v, want id > 0, %s %d, trimmed body, by %d %q at %v", first, fixture.Order, metro, e.Priya.ID, namePriya, t0)
	}
	e.Clock.Advance(time.Minute)
	second := e.add(t, e.Ravi, fixture.Order, metro, "Approved on the phone.")
	if second.ID <= first.ID || second.CreatedByName != nameRavi || !second.CreatedAt.Equal(t0.Add(time.Minute)) {
		t.Errorf("second AddComment = %+v, want a later id by %q at %v", second, nameRavi, t0.Add(time.Minute))
	}

	// Oldest first, for every reader.
	for _, p := range []auth.Principal{e.Priya, e.Ravi, e.Admin} {
		got, err := e.C.ListComments(ctx, p, fixture.Order, metro)
		if err != nil {
			t.Fatalf("ListComments as %s: %v", p.Name, err)
		}
		eqStrings(t, "ListComments bodies as "+p.Name, bodies(got), []string{first.Body, second.Body})
		if len(got) == 2 {
			if got[0].ID != first.ID || got[0].CreatedBy != e.Priya.ID || got[0].CreatedByName != namePriya || !got[0].CreatedAt.Equal(t0) ||
				got[0].DocType != fixture.Order || got[0].RecordID != metro {
				t.Errorf("ListComments[0] as %s = %+v, want %+v", p.Name, got[0], first)
			}
			if got[1].ID != second.ID || got[1].CreatedBy != e.Ravi.ID || got[1].CreatedByName != nameRavi || !got[1].CreatedAt.Equal(t0.Add(time.Minute)) {
				t.Errorf("ListComments[1] as %s = %+v, want %+v", p.Name, got[1], second)
			}
		}
	}
	if n := e.rows(t); n != 2 {
		t.Fatalf("module_comment rows = %d, want 2", n)
	}

	// Read is needed: no read grant (Deepak) and a record outside the
	// restriction (Priya on PRJ-TECHPARK) both answer the 404, adding nothing.
	refused := []struct {
		what string
		p    auth.Principal
		dt   string
		id   int64
	}{
		{"no read on orders (Deepak)", e.Deepak, fixture.Order, metro},
		{"hidden by restriction (Priya on PRJ-TECHPARK)", e.Priya, fixture.Order, techpark},
		{"missing order", e.Priya, fixture.Order, 999_999},
		{"DocType not registered for comments (Administrator on a claim)", e.Admin, fixture.Claim, claim},
		{"DocType not registered for comments (owner on her claim)", e.Priya, fixture.Claim, claim},
		{"unknown DocType", e.Admin, "nope.Thing", 1},
	}
	for _, c := range refused {
		_, err := e.C.AddComment(ctx, c.p, c.dt, c.id, "Is this order still needed?")
		wantNotFound(t, "AddComment: "+c.what, err)
		_, err = e.C.ListComments(ctx, c.p, c.dt, c.id)
		wantNotFound(t, "ListComments: "+c.what, err)
	}
	// Ravi can read the PRJ-TECHPARK order, so it is hidden only from Priya.
	if _, err := e.C.ListComments(ctx, e.Ravi, fixture.Order, techpark); err != nil {
		t.Errorf("ListComments(techpark) as Ravi: %v, want ok", err)
	}
	if n := e.rows(t); n != 2 {
		t.Errorf("module_comment rows after refusals = %d, want 2", n)
	}

	// Body rules, each with its own message; length counts characters, after trimming.
	thousand := strings.Repeat("é", 1000) // 2,000 bytes
	for _, c := range []struct{ what, body, msg string }{
		{"empty", "", msgCommentEmpty},
		{"only spaces", " \n\t  ", msgCommentEmpty},
		{"1,001 characters", thousand + "x", msgCommentTooLong},
		{"1,001 characters after trimming", "  " + thousand + "x  ", msgCommentTooLong},
	} {
		_, err := e.C.AddComment(ctx, e.Priya, fixture.Order, metro, c.body)
		wantInvalid(t, "AddComment "+c.what, err, c.msg)
	}
	if n := e.rows(t); n != 2 {
		t.Errorf("module_comment rows after refused bodies = %d, want 2", n)
	}
	exact := e.add(t, e.Priya, fixture.Order, metro, thousand)
	if exact.Body != thousand {
		t.Errorf("1,000-character body stored as %d characters, want it unchanged", len([]rune(exact.Body)))
	}
	padded := e.add(t, e.Priya, fixture.Order, metro, "  "+thousand+"\n")
	if padded.Body != thousand {
		t.Errorf("padded 1,000-character body = %d characters, want the trimmed 1,000", len([]rune(padded.Body)))
	}
	if n := e.rows(t); n != 4 {
		t.Errorf("module_comment rows = %d, want 4", n)
	}

	// Append-only: the triggers refuse UPDATE and DELETE, and nothing changes.
	if _, err := e.DB.SQL().ExecContext(ctx, `UPDATE module_comment SET body = 'changed' WHERE id = ?`, first.ID); err == nil {
		t.Errorf("UPDATE module_comment succeeded, want the trigger's refusal")
	}
	if _, err := e.DB.SQL().ExecContext(ctx, `DELETE FROM module_comment WHERE id = ?`, first.ID); err == nil {
		t.Errorf("DELETE FROM module_comment succeeded, want the trigger's refusal")
	}
	got, err := e.C.ListComments(ctx, e.Ravi, fixture.Order, metro)
	if err != nil {
		t.Fatalf("ListComments after UPDATE and DELETE: %v", err)
	}
	eqStrings(t, "bodies after UPDATE and DELETE", bodies(got), []string{first.Body, second.Body, thousand, thousand})
}

// PO spec §22 and §25: RecentComments lists comments from the last 7 days by
// the clock, on registered documents p owns and can read, newest first, at
// most 10, each with its document's display.
func TestComments_Recent(t *testing.T) {
	e := newCommentsEnv(t)
	ctx := t.Context()
	t0 := e.Clock.Now()
	day := 24 * time.Hour

	mine := e.order(t, e.Priya, "PRJ-METRO")
	mine2 := e.order(t, e.Priya, "PRJ-METRO")
	mineHidden := e.order(t, e.Priya, "PRJ-TECHPARK") // Priya owns it but her restriction hides it
	ravis := e.order(t, e.Ravi, "PRJ-METRO")
	claim := e.NewClaim(t, e.Priya, "EMP-004", fixturetest.CL{Type: "Travel", Amount: "1500.00"}).ID

	at := func(when time.Time, p auth.Principal, id int64, body string) modkit.Comment {
		t.Helper()
		e.Clock.Set(when)
		return e.add(t, p, fixture.Order, id, body)
	}
	at(t0.Add(-8*day), e.Ravi, mine, "Eight days ago.")                      // outside the window
	sixDays := at(t0.Add(-6*day), e.Ravi, mine2, "Six days ago.")            // in
	at(t0.Add(-2*day), e.Meena, ravis, "On Ravi's order.")                   // not Priya's
	at(t0.Add(-1*day), e.Admin, mineHidden, "On an order Priya can't read.") // Priya can't read it
	hour := at(t0.Add(-time.Hour), e.Meena, mine, "Please check the rate.")  // in
	// A comment on a claim Priya owns and reads: the claim never opted in.
	if _, err := e.DB.SQL().ExecContext(ctx, `INSERT INTO module_comment (doctype, record_id, body, created_by, created_at)
		SELECT ?, ?, 'On a claim.', created_by, created_at FROM module_comment WHERE id = ?`, fixture.Claim, claim, hour.ID); err != nil {
		t.Fatalf("insert the claim's comment: %v", err)
	}
	e.Clock.Set(t0)

	recent := func(p auth.Principal) []modkit.Comment {
		t.Helper()
		got, err := e.C.RecentComments(ctx, p)
		if err != nil {
			t.Fatalf("RecentComments as %s: %v", p.Name, err)
		}
		return got
	}

	got := recent(e.Priya)
	eqStrings(t, "Priya's recent comments", bodies(got), []string{"Please check the rate.", "Six days ago."})
	if len(got) == 2 {
		want := []struct {
			c       modkit.Comment
			id      int64
			name    string
			display string
		}{
			{hour, mine, nameMeena, e.Display(t, fixture.Order, mine)},
			{sixDays, mine2, nameRavi, e.Display(t, fixture.Order, mine2)},
		}
		for i, w := range want {
			g := got[i]
			if g.ID != w.c.ID || g.DocType != fixture.Order || g.RecordID != w.id || g.CreatedByName != w.name ||
				!g.CreatedAt.Equal(w.c.CreatedAt) || g.Display != w.display || w.display == "" {
				t.Errorf("RecentComments[%d] = %+v, want comment %d on %s %d by %q at %v, Display %q",
					i, g, w.c.ID, fixture.Order, w.id, w.name, w.c.CreatedAt, w.display)
			}
		}
	}

	// Ownership: Ravi sees the comment on his order only.
	eqStrings(t, "Ravi's recent comments", bodies(recent(e.Ravi)), []string{"On Ravi's order."})
	// Owning nothing, Deepak sees none.
	eqStrings(t, "Deepak's recent comments", bodies(recent(e.Deepak)), nil)

	// The window moves with the clock: two days on, the six-day-old comment is out.
	e.Clock.Set(t0.Add(2 * day))
	eqStrings(t, "Priya's recent comments two days on", bodies(recent(e.Priya)), []string{"Please check the rate."})

	// At most 10, newest first.
	var want []string
	for i := 1; i <= 11; i++ {
		body := "Note " + strconv.Itoa(i) + "."
		at(t0.Add(2*day+time.Duration(i)*time.Minute), e.Ravi, mine, body)
		want = append([]string{body}, want...)
	}
	e.Clock.Set(t0.Add(2*day + time.Hour))
	eqStrings(t, "Priya's recent comments past the cap", bodies(recent(e.Priya)), want[:10])
}

// CD-213: AddComment and ListComments read AUTH in their own transaction, so
// a non-Administrator never takes a second connection.
func TestNoSecondConn_Comments(t *testing.T) {
	e := newCommentsEnv(t)
	metro := e.order(t, e.Ravi, "PRJ-METRO")
	techpark := e.order(t, e.Ravi, "PRJ-TECHPARK")
	err := testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		if _, err := e.C.AddComment(ctx, e.Priya, fixture.Order, metro, "Checked the rate with the vendor."); err != nil {
			return err
		}
		got, err := e.C.ListComments(ctx, e.Priya, fixture.Order, metro)
		if err != nil {
			return err
		}
		if len(got) != 1 || got[0].CreatedByName != namePriya {
			return errors.New("ListComments: want Priya's one comment, got " + strings.Join(bodies(got), " | "))
		}
		return nil
	})
	if err != nil {
		t.Errorf("Add and List as Priya on the pool of one: %v, want ok", err)
	}
	// The refusal path reads AUTH in the same transaction too.
	err = testkit.NoSecondConn(t, e.DB, func(ctx context.Context) error {
		_, err := e.C.AddComment(ctx, e.Priya, fixture.Order, techpark, "Hidden.")
		return err
	})
	wantNotFound(t, "AddComment on a hidden order on the pool of one", err)
}
