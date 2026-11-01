package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Expected is a PO's Expected on record (spec §27.1), kept outside the PO so
// R7 holds.
type Expected struct {
	ExpectedOn types.Date
	Note       string
	SetBy      types.UserID
	SetAt      time.Time
	Version    int
}

// ExpectedState is what the Expected on page shows: the PO's display, vendor,
// rounded total and required by (its summary, UX §5.20; RequiredBy is the
// zero Date when blank), the record when Set, the version the form posts (0
// with no row), Today for the date's buttons and Refusal, the not-approved
// text when the PO isn't approved ("" otherwise).
type ExpectedState struct {
	Display, Vendor, Total string
	RequiredBy, Today      types.Date
	Expected               Expected
	Set                    bool
	Version                int
	Refusal                string
}

// ExpectedInfo reads poID's Expected on record in tx; ok false when there is
// none.
func ExpectedInfo(ctx context.Context, tx *db.Tx, poID int64) (Expected, bool, error) {
	row, err := dbq.New(tx.SQL()).GetExpected(ctx, poID)
	if errors.Is(err, sql.ErrNoRows) {
		return Expected{}, false, nil
	}
	if err != nil {
		return Expected{}, false, err
	}
	on, err := types.DateFromISO(row.ExpectedOn)
	if err != nil {
		return Expected{}, false, err
	}
	at, err := types.ParseTimestamp(row.SetAt)
	if err != nil {
		return Expected{}, false, err
	}
	return Expected{ExpectedOn: on, Note: row.Note, SetBy: types.UserID(row.SetBy), SetAt: at.Time(), Version: int(row.Version)}, true, nil
}

// ParseExpectedOn reads the date the Expected on form posts, trimmed. Blank
// (also only spaces) is the zero date, which SetExpectedPosted refuses with its own
// message; anything else that doesn't parse is DOC's unreadable-value text
// (422).
func ParseExpectedOn(s string) (types.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return types.Date{}, nil
	}
	d, err := types.ParseDate(s)
	if err != nil {
		return types.Date{}, expectedInvalid(msgExpectedUnreadable)
	}
	return d, nil
}

// ExpectedState reads the Expected on page's state for p, with the same
// load and right checks as SetExpected.
func (s *Service) ExpectedState(ctx context.Context, p auth.Principal, poID int64) (ExpectedState, error) {
	var st ExpectedState
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.orderFor(ctx, tx, p, poID, auth.RightAmend)
		if err != nil {
			return err
		}
		e, ok, err := ExpectedInfo(ctx, tx, poID)
		if err != nil {
			return err
		}
		vendor, total, requiredBy, err := orderSummary(ctx, tx, poID)
		if err != nil {
			return err
		}
		st = ExpectedState{Display: info.Display, Vendor: vendor, Total: total, RequiredBy: requiredBy, Today: types.Today(s.d.Clock), Expected: e, Set: ok, Version: e.Version}
		if info.State != doctype.StateApproved {
			st.Refusal = msgExpectedNotApproved
		}
		return nil
	})
	return st, err
}

// SetExpected sets poID's Expected on to on at the version the page posted
// (CD-94): an insert at version 1 when posted 0 and there is no row, else an
// update to version + 1. The version is read and checked inside the write
// (BEGIN IMMEDIATE), so two posts of 0 can't both insert. Inside one write,
// in this order: load (missing or hidden is AUTH's 404, CD-245), the right,
// the version (CD-259), then the state and the date rules. A past date is
// allowed (a late update).
func (s *Service) SetExpected(ctx context.Context, p auth.Principal, poID int64, version int, on types.Date, note string) error {
	return s.setExpected(ctx, p, poID, version, func() (types.Date, error) { return on, nil }, note)
}

// SetExpectedPosted is SetExpected for the form: on is the posted date as
// typed, parsed with ParseExpectedOn after the load, right, version and state
// checks (CD-259), so a bad date never hides a 403, 404, 409 or the state
// refusal.
func (s *Service) SetExpectedPosted(ctx context.Context, p auth.Principal, poID int64, version int, on, note string) error {
	return s.setExpected(ctx, p, poID, version, func() (types.Date, error) { return ParseExpectedOn(on) }, note)
}

// setExpected is SetExpected with the date from parse, called after the
// state check.
func (s *Service) setExpected(ctx context.Context, p auth.Principal, poID int64, version int, parse func() (types.Date, error), note string) error {
	return s.d.DB.Write(ctx, func(tx *db.Tx) error {
		info, cur, err := s.expectedWrite(ctx, tx, p, poID, version)
		if err != nil {
			return err
		}
		if info.State != doctype.StateApproved {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgExpectedNotApproved}
		}
		on, err := parse()
		if err != nil {
			return err
		}
		if on == (types.Date{}) {
			return expectedInvalid(msgExpectedBlank)
		}
		orderOn, err := dateOrder(ctx, tx, poID)
		if err != nil {
			return err
		}
		if on.Compare(orderOn) < 0 {
			return expectedInvalid(msgExpectedBeforeOrder)
		}
		if utf8.RuneCountInString(note) > noteMaxLen {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgExpectedNoteLong,
				Fields: []doc.FieldError{{Field: "note", Message: msgExpectedNoteLong}}}
		}
		now := s.d.Clock.Now()
		q := dbq.New(tx.SQL())
		at := types.TimestampOf(now).String()
		if cur == 0 {
			err = q.InsertExpected(ctx, dbq.InsertExpectedParams{PoID: poID, ExpectedOn: on.ISO(), Note: note, SetBy: int64(p.ID), SetAt: at})
		} else {
			err = q.UpdateExpected(ctx, dbq.UpdateExpectedParams{ExpectedOn: on.ISO(), Note: note, SetBy: int64(p.ID), SetAt: at, PoID: poID})
		}
		if err != nil {
			return err
		}
		return s.orderAudit(ctx, tx, p, poID, fmt.Sprintf(auditExpectedSet, types.FormatDate(on)), now)
	})
}

// ClearExpected deletes poID's Expected on record at the posted current
// version. Inside one write, in this order: SetExpected's load, right and
// version checks, then the state: a PO that isn't approved is refused (422),
// also with no record. On an approved PO with no record (posted 0) it changes
// nothing and writes no audit entry (AD-13 rulings).
func (s *Service) ClearExpected(ctx context.Context, p auth.Principal, poID int64, version int) error {
	return s.d.DB.Write(ctx, func(tx *db.Tx) error {
		info, cur, err := s.expectedWrite(ctx, tx, p, poID, version)
		if err != nil {
			return err
		}
		if info.State != doctype.StateApproved {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgExpectedNotApproved}
		}
		if cur == 0 {
			return nil
		}
		if err := dbq.New(tx.SQL()).DeleteExpected(ctx, poID); err != nil {
			return err
		}
		return s.orderAudit(ctx, tx, p, poID, auditExpectedCleared, s.d.Clock.Now())
	})
}

// expectedWrite runs a write's load and right checks, then refuses a posted
// version other than the row's (0 with no row) as stale. It returns the PO
// and the row's version.
func (s *Service) expectedWrite(ctx context.Context, tx *db.Tx, p auth.Principal, poID int64, version int) (doctype.Info, int, error) {
	info, err := s.orderFor(ctx, tx, p, poID, auth.RightAmend)
	if err != nil {
		return doctype.Info{}, 0, err
	}
	e, _, err := ExpectedInfo(ctx, tx, poID)
	if err != nil {
		return doctype.Info{}, 0, err
	}
	if version != e.Version {
		return doctype.Info{}, 0, expectedStale(info.Display)
	}
	return info, e.Version, nil
}

// expectedStale is DOC's stale refusal (409) for display.
func expectedStale(display string) error {
	return &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgExpectedStale, display)}
}

// expectedInvalid is a 422 on the expected_on field.
func expectedInvalid(msg string) error {
	return &doc.Error{Kind: doc.KindInvalid, Message: msg, Fields: []doc.FieldError{{Field: "expected_on", Message: msg}}}
}
