package purchase

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/kernel/naming"
	"github.com/vardhan/internaltools/internal/masters/party"
	"github.com/vardhan/internaltools/internal/modules/modkit"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// Eligible is one person a PO can be handed over to (UX 5.12).
type Eligible struct {
	ID    types.UserID
	Name  string
	Roles string // the user's role names, sorted, joined with ", "
}

// HandOverState is what the hand-over page shows: the PO, the version the
// form posts, the people it can go to (by name) and Refusal, the state
// refusal HandOver would give (nil on a draft or rejected PO).
type HandOverState struct {
	Display, Vendor, Total, Owner string
	Version                       int
	Eligible                      []Eligible
	Refusal                       error
}

// eligibleOf is the people with ids, with their names and roles, sorted by
// name. LoadPrincipal opens its own read, so it runs after the caller's tx.
func (s *Service) eligibleOf(ctx context.Context, ids []types.UserID) ([]Eligible, error) {
	var out []Eligible
	for _, id := range ids {
		u, _, err := s.d.Auth.LoadPrincipal(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, Eligible{ID: id, Name: u.Name, Roles: strings.Join(u.Roles, ", ")})
	}
	slices.SortStableFunc(out, func(a, b Eligible) int { return cmp.Compare(a.Name, b.Name) })
	return out, nil
}

// HandOverState reads the hand-over page's state for p in one read, with
// Reassign's caller checks first (Engine.EligibleOwners, CD-259). A missing
// or hidden PO is AUTH's 404 (CD-245) and a missing right AUTH's 403, as the
// error; Refusal is the same error HandOver returns for the PO's state
// (CD-142).
func (s *Service) HandOverState(ctx context.Context, p auth.Principal, poID int64) (HandOverState, error) {
	var st HandOverState
	var ids []types.UserID
	err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		var err error
		if ids, err = s.d.Engine.EligibleOwners(ctx, tx, p, DocType, poID); err != nil {
			return err
		}
		d, err := doc.LoadDoc(ctx, tx, DocType, poID)
		if err != nil {
			return err
		}
		owner, _, err := s.d.Auth.User(ctx, tx, d.CreatedBy)
		if err != nil {
			return err
		}
		vendors, _ := doc.Lookup(party.DocType)
		vendorID, _ := d.Header["vendor_id"].(int64)
		vendor, _, err := vendors.Master.Lookup(ctx, tx, vendorID)
		if err != nil {
			return err
		}
		total, err := amountOrder(ctx, tx, poID)
		if err != nil {
			return err
		}
		st = HandOverState{
			Display: naming.Label(d.Number, d.ID), Vendor: vendor.Name, Total: types.FormatINR(total), Owner: owner.Name,
			Version: d.Version, Refusal: stateRefusal(d.State),
		}
		return nil
	})
	if err != nil {
		return HandOverState{}, handOverNotFound(err)
	}
	if st.Eligible, err = s.eligibleOf(ctx, ids); err != nil {
		return HandOverState{}, err
	}
	return st, nil
}

// HandOver makes to the owner of draft or rejected PO poID at the version the
// page posted (spec §21.2), then posts a non-blank note, trimmed, as p's
// comment (§22). Nobody picked and a refused note are checked first, so a
// refused hand-over changes nothing.
func (s *Service) HandOver(ctx context.Context, p auth.Principal, poID int64, version int, to types.UserID, note string) error {
	if to == 0 {
		return &doc.Error{Kind: doc.KindInvalid, Message: msgPickOwner}
	}
	note = strings.TrimSpace(note)
	if note != "" {
		if err := modkit.CheckCommentBody(note); err != nil {
			return &doc.Error{Kind: doc.KindInvalid, Message: err.Error(), Err: err}
		}
	}
	err := s.d.Engine.Reassign(ctx, p, DocType, poID, version, to)
	var re *doc.ReassignError
	if errors.As(err, &re) {
		return s.reassignRefusal(ctx, re, to)
	}
	if err != nil {
		return handOverNotFound(err)
	}
	if note == "" {
		return nil
	}
	// The PO is handed over; a note that fails now is logged, not a refusal.
	if _, err := s.d.Comments.AddComment(ctx, p, DocType, poID, note); err != nil {
		slog.Error("purchase: hand-over note not saved", "po", poID, "err", err)
	}
	return nil
}

// reassignRefusal is §21.2's text for DOC's refusal: the state refusals 409,
// the person-specific ones 422 at the "to" radios with to's name.
func (s *Service) reassignRefusal(ctx context.Context, re *doc.ReassignError, to types.UserID) error {
	var format string
	switch re.Reason {
	case doc.ReassignPending:
		return doc.Refused(msgHandOverPending)
	case doc.ReassignNotDraft:
		return doc.Refused(msgHandOverNotDraft)
	case doc.ReassignSameOwner:
		format = msgHandOverSameOwner
	case doc.ReassignCantCreate:
		format = msgHandOverCantCreate
	case doc.ReassignCantSee:
		format = msgHandOverCantSee
	case doc.ReassignDisabled:
		format = msgHandOverDisabled
	default:
		return re
	}
	var name string
	if err := s.d.DB.Read(ctx, func(tx *db.Tx) error {
		u, _, err := s.d.Auth.User(ctx, tx, to)
		name = u.Name
		return err
	}); err != nil {
		return err
	}
	msg := fmt.Sprintf(format, name)
	return &doc.Error{Kind: doc.KindInvalid, Message: msg, Fields: []doc.FieldError{{Field: "to", Message: msg}}}
}

// stateRefusal is the hand-over refusal for a PO in state st, nil when it
// can be handed over (draft or rejected).
func stateRefusal(st doc.State) error {
	switch st {
	case doc.StateDraft, doc.StateRejected:
		return nil
	case doc.StatePending:
		return doc.Refused(msgHandOverPending)
	}
	return doc.Refused(msgHandOverNotDraft)
}

// handOverNotFound answers a missing PO with AUTH's 404, as a hidden one
// (CD-245): Reassign and LoadDoc give DOC's missing text.
func handOverNotFound(err error) error {
	if errors.Is(err, doctype.ErrNotFound) {
		return auth.ErrNotFound
	}
	return err
}
