package purchase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/modules/purchase/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// copyHeader and copyLine are the columns Copy carries over (spec §24.1).
// copyChecked are the header links Copy leaves blank when disabled; a
// disabled site or project goes to create, which refuses it (AD-13 ruling).
var (
	copyHeader  = []string{"vendor_id", "vendor_address_id", "site_id", "project_id", "payment_terms_id", "terms", "reverse_charge"}
	copyChecked = []string{"vendor_id", "vendor_address_id", "payment_terms_id"}
	copyLine    = []string{"item_id", "description", "uom_id", "qty", "price", "discount_pct", "tax_code_id", "hsn_sac"}
)

// Copy creates a draft copy of PO poID owned by p (spec §24.1) and returns
// its id; see CopyWithSource.
func (s *Service) Copy(ctx context.Context, p auth.Principal, poID int64) (newID int64, err error) {
	newID, _, err = s.CopyWithSource(ctx, p, poID)
	return newID, err
}

// CopyWithSource is Copy, also returning the source's display for the
// caller's message. It reads poID with a read check (missing or hidden is
// AUTH's 404, CD-245), leaves disabled header links blank and leaves out lines
// with a disabled link, then creates the draft through Engine.Do with only the
// copied columns, copied_from_no and copied_from_id (create's own checks
// refuse a missing create right or a restricted site or project, AUTH-099).
// Last it writes copy_notes while the draft is still at version 1; a draft
// already changed keeps no notes, and that is logged (plan P12).
func (s *Service) CopyWithSource(ctx context.Context, p auth.Principal, poID int64) (newID int64, source string, err error) {
	var in doc.ActionInput
	var notes []string
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.d.Engine.Info(ctx, tx, DocType, poID)
		if errors.Is(err, doctype.ErrNotFound) {
			return auth.ErrNotFound
		}
		if err != nil {
			return err
		}
		ref := &auth.RecordRef{
			DocType: info.DocType, ID: info.ID, CreatedBy: info.CreatedBy,
			EmployeeID: info.EmployeeID, ProjectIDs: info.ProjectIDs, SiteIDs: info.SiteIDs,
		}
		if err := s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightRead, ref); err != nil {
			return err
		}
		d, err := doc.LoadDoc(ctx, tx, DocType, poID)
		if err != nil {
			return err
		}
		source = info.Display
		in, notes, err = copyInput(ctx, tx, d, source)
		if err != nil {
			return err
		}
		in.Values["order_date"] = types.Today(s.d.Clock).ISO()
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	res, err := s.d.Engine.Do(ctx, p, DocType, 0, doctype.ActionSave, in)
	if err != nil {
		return 0, "", err
	}
	var n int64
	err = s.d.DB.Write(ctx, func(tx *db.Tx) error {
		n, err = dbq.New(tx.SQL()).SetCopyNotes(ctx, dbq.SetCopyNotesParams{CopyNotes: strings.Join(notes, "\n"), ID: res.ID})
		return err
	})
	if err != nil {
		return 0, "", err
	}
	if n == 0 {
		slog.WarnContext(ctx, "purchase: copy notes skipped, the draft changed first", "po", res.ID, "source", poID)
	}
	return res.ID, source, nil
}

// copyInput is the create input for a copy of d, shown as display, with its
// notes: the first note, then one per disabled header link (left blank) and
// one per line left out for its first disabled link, in line order.
func copyInput(ctx context.Context, tx *db.Tx, d doc.Doc, display string) (doc.ActionInput, []string, error) {
	dt, _ := doc.Lookup(DocType)
	in := doc.ActionInput{Values: map[string]string{
		"copied_from_no": display, "copied_from_id": fmt.Sprint(d.ID),
	}}
	notes := []string{fmt.Sprintf(msgCopiedFrom, display)}
	for _, name := range copyHeader {
		f, _ := dt.Field(name)
		if slices.Contains(copyChecked, name) {
			code, disabled, err := disabledLink(ctx, tx, f, d.Header[name])
			if err != nil {
				return doc.ActionInput{}, nil, err
			}
			if disabled {
				notes = append(notes, fmt.Sprintf(msgCopyBlank, f.Label, code))
				continue
			}
		}
		in.Values[name] = doc.FormatPlain(f, d.Header[name])
	}
lines:
	for _, l := range d.Lines {
		vals := map[string]string{}
		for _, name := range copyLine {
			f, _ := dt.Field(name)
			code, disabled, err := disabledLink(ctx, tx, f, l.Values[name])
			if err != nil {
				return doc.ActionInput{}, nil, err
			}
			if disabled {
				notes = append(notes, fmt.Sprintf(msgCopyLeftOut, l.LineNo, display, strings.ToLower(f.Label), code))
				continue lines
			}
			vals[name] = doc.FormatPlain(f, l.Values[name])
		}
		in.Lines = append(in.Lines, vals)
	}
	return in, notes, nil
}

// disabledLink reports whether v, the value of f, is a link to a disabled
// master, with the master's code, or its name for a master with no code (an
// address), as DOC names it (kernel/doc/validate.go).
func disabledLink(ctx context.Context, tx *db.Tx, f doctype.Field, v any) (code string, disabled bool, err error) {
	id, ok := v.(int64)
	if f.Type != doctype.Link || !ok {
		return "", false, nil
	}
	target, _ := doc.Lookup(f.Link)
	m, _, err := target.Master.Lookup(ctx, tx, id)
	if m.Code == "" {
		return m.Name, m.Disabled, err
	}
	return m.Code, m.Disabled, err
}
