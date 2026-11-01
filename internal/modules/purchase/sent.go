package purchase

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// SentInput is what the Send page's form posts (spec §23).
type SentInput struct {
	SentOn  types.Date
	SentVia string // email, whatsapp, courier, by_hand or other; "" = none chosen
	Note    string
}

// Sent is a PO's Sent record (spec §23), kept outside the PO so R7 holds.
type Sent struct {
	SentOn  types.Date
	SentVia string
	Note    string
	SentBy  types.UserID
	SentAt  time.Time
	Version int
}

// SendState is what the Send page shows: the PO's display, vendor and
// rounded total (its summary, UX §5.13), the record (nil with no row) with
// SentByName, its author's name, the version the form posts (0 with no row),
// Today for the date's buttons and Refusal, the not-approved error the POST
// returns when the PO isn't approved (nil otherwise).
type SendState struct {
	Display, Vendor, Total string
	Sent                   *Sent
	SentByName             string
	Version                int
	Today                  types.Date
	Refusal                error
}

// SentInfo reads poID's Sent record in tx; ok false when there is none.
func SentInfo(ctx context.Context, tx *db.Tx, poID int64) (Sent, bool, error) {
	row, err := dbq.New(tx.SQL()).GetSent(ctx, poID)
	if errors.Is(err, sql.ErrNoRows) {
		return Sent{}, false, nil
	}
	if err != nil {
		return Sent{}, false, err
	}
	on, err := types.DateFromISO(row.SentOn)
	if err != nil {
		return Sent{}, false, err
	}
	at, err := types.ParseTimestamp(row.SentAt)
	if err != nil {
		return Sent{}, false, err
	}
	return Sent{SentOn: on, SentVia: row.SentVia, Note: row.Note, SentBy: types.UserID(row.SentBy), SentAt: at.Time(), Version: int(row.Version)}, true, nil
}

// SendState reads the Send page's state for p, with the same load and right
// checks as MarkSent.
func (s *Service) SendState(ctx context.Context, p auth.Principal, poID int64) (SendState, error) {
	var st SendState
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.orderFor(ctx, tx, p, poID, auth.RightWrite)
		if err != nil {
			return err
		}
		e, ok, err := SentInfo(ctx, tx, poID)
		if err != nil {
			return err
		}
		vendor, total, _, err := orderSummary(ctx, tx, poID)
		if err != nil {
			return err
		}
		st = SendState{Display: info.Display, Vendor: vendor, Total: total, Version: e.Version, Today: types.Today(s.d.Clock)}
		if ok {
			st.Sent = &e
			if st.SentByName, err = s.userName(ctx, tx, map[int64]string{}, int64(e.SentBy)); err != nil {
				return err
			}
		}
		if info.State != doctype.StateApproved {
			st.Refusal = sentNotApproved()
		}
		return nil
	})
	return st, err
}

// MarkSent records poID as sent at the version the page posted (CD-94): an
// insert at version 1 when posted 0 and there is no row, else an update to
// version + 1. The version is read and checked inside the write (BEGIN
// IMMEDIATE), so two posts of 0 can't both insert. Inside one write, in this
// order: load (missing or hidden is AUTH's 404, CD-245), the right, the
// version (CD-259), the state, then the date rules: a zero SentOn (the form's
// date didn't parse, blank included) is DOC's unreadable-value text (AD-13).
// An unknown sent_via is left to the table's CHECK (a plain internal error).
func (s *Service) MarkSent(ctx context.Context, p auth.Principal, poID int64, version int, in SentInput) error {
	return s.d.DB.Write(ctx, func(tx *db.Tx) error {
		info, cur, err := s.sentWrite(ctx, tx, p, poID, version)
		if err != nil {
			return err
		}
		if info.State != doctype.StateApproved {
			return sentNotApproved()
		}
		if in.SentOn == (types.Date{}) {
			return sentInvalid("sent_on", msgSentUnreadable)
		}
		now := s.d.Clock.Now()
		if in.SentOn.Compare(types.Today(s.d.Clock)) > 0 {
			return sentInvalid("sent_on", msgSentFuture)
		}
		orderOn, err := dateOrder(ctx, tx, poID)
		if err != nil {
			return err
		}
		if in.SentOn.Compare(orderOn) < 0 {
			return sentInvalid("sent_on", msgSentBeforeOrder)
		}
		if in.SentVia == "" {
			return sentInvalid("sent_via", msgSentNoMethod)
		}
		if utf8.RuneCountInString(in.Note) > sentNoteMaxLen {
			return sentInvalid("note", msgSentNoteLong)
		}
		q := dbq.New(tx.SQL())
		at := types.TimestampOf(now).String()
		if cur == 0 {
			err = q.InsertSent(ctx, dbq.InsertSentParams{PoID: poID, SentOn: in.SentOn.ISO(), SentVia: in.SentVia, Note: in.Note, SentBy: int64(p.ID), SentAt: at})
		} else {
			err = q.UpdateSent(ctx, dbq.UpdateSentParams{SentOn: in.SentOn.ISO(), SentVia: in.SentVia, Note: in.Note, SentBy: int64(p.ID), SentAt: at, PoID: poID})
		}
		if err != nil {
			return err
		}
		return s.orderAudit(ctx, tx, p, poID, auditSentMarked, now)
	})
}

// UnmarkSent deletes poID's Sent record at the posted current version, with
// MarkSent's load, right and version checks. With no record (posted 0) it
// changes nothing and writes no audit entry (AD-13 ruling).
func (s *Service) UnmarkSent(ctx context.Context, p auth.Principal, poID int64, version int) error {
	return s.d.DB.Write(ctx, func(tx *db.Tx) error {
		_, cur, err := s.sentWrite(ctx, tx, p, poID, version)
		if err != nil || cur == 0 {
			return err
		}
		if err := dbq.New(tx.SQL()).DeleteSent(ctx, poID); err != nil {
			return err
		}
		return s.orderAudit(ctx, tx, p, poID, auditSentUnmarked, s.d.Clock.Now())
	})
}

// sentWrite runs a write's load and right checks, then refuses a posted
// version other than the row's (0 with no row) as stale. It returns the PO
// and the row's version.
func (s *Service) sentWrite(ctx context.Context, tx *db.Tx, p auth.Principal, poID int64, version int) (doctype.Info, int, error) {
	info, err := s.orderFor(ctx, tx, p, poID, auth.RightWrite)
	if err != nil {
		return doctype.Info{}, 0, err
	}
	e, _, err := SentInfo(ctx, tx, poID)
	if err != nil {
		return doctype.Info{}, 0, err
	}
	if version != e.Version {
		return doctype.Info{}, 0, &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgSentStale, info.Display)}
	}
	return info, e.Version, nil
}

// orderSummary is PO poID's vendor (its snapshot), rounded total and
// required by (the zero Date when blank), for the Send, Expected on and
// hand-over pages (UX §5.13, §5.20).
func orderSummary(ctx context.Context, tx *db.Tx, poID int64) (vendor, total string, requiredBy types.Date, err error) {
	row, err := dbq.New(tx.SQL()).OrderPageSummary(ctx, poID)
	if err != nil {
		return "", "", types.Date{}, err
	}
	amount, err := amountOrder(ctx, tx, poID)
	if err != nil {
		return "", "", types.Date{}, err
	}
	if row.RequiredBy != "" {
		if requiredBy, err = types.DateFromISO(row.RequiredBy); err != nil {
			return "", "", types.Date{}, err
		}
	}
	return row.SnapVendorName, types.FormatINR(amount), requiredBy, nil
}

// orderFor loads poID in tx and checks that p holds right or approve on it
// (Send: write, spec §23; Expected on: amend, spec §27.1). A missing PO is
// AUTH's 404, as a hidden one is (CD-245); with neither right the error is
// AUTH's refusal of right.
func (s *Service) orderFor(ctx context.Context, tx *db.Tx, p auth.Principal, poID int64, right auth.Right) (doctype.Info, error) {
	info, err := s.d.Engine.Info(ctx, tx, DocType, poID)
	if errors.Is(err, doctype.ErrNotFound) {
		return doctype.Info{}, auth.ErrNotFound
	}
	if err != nil {
		return doctype.Info{}, err
	}
	ref := &auth.RecordRef{
		DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
		EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
	}
	err = s.d.Auth.CanTx(ctx, tx, p, DocType, right, ref)
	var denied *auth.DeniedError
	if errors.As(err, &denied) && s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightApprove, ref) == nil {
		err = nil
	}
	if err != nil {
		return doctype.Info{}, err
	}
	return info, nil
}

// orderAudit writes a Send or Expected on change's audit entry on the PO,
// action in History's words.
func (s *Service) orderAudit(ctx context.Context, tx *db.Tx, p auth.Principal, poID int64, action string, at time.Time) error {
	return s.d.Audit.Append(ctx, tx, audit.Entry{DocType: DocType, DocID: poID, Action: action, By: p.ID, At: at.UTC()})
}

// sentNotApproved is spec §23's state refusal (422), shared by MarkSent and
// SendState.
func sentNotApproved() error {
	return &doc.Error{Kind: doc.KindInvalid, Message: msgSentNotApproved}
}

// sentInvalid is a 422 on field.
func sentInvalid(field, msg string) error {
	return &doc.Error{Kind: doc.KindInvalid, Message: msg, Fields: []doc.FieldError{{Field: field, Message: msg}}}
}
