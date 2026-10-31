package modkit

import (
	"context"
	"database/sql"
	"unicode/utf8"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/modules/modkit/dbq"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

const (
	// settingsDocType is the audit log's name for the business settings row.
	settingsDocType = "modkit.BusinessSettings"
	termsMaxLen     = 2000 // characters of default_po_terms
)

// Settings is the one business_settings row (PO spec §14.1).
type Settings struct {
	LockedUpTo     sql.Null[types.Date] // the period lock (§14.2); blank = off
	DefaultPOTerms string
	Version        int
}

// SettingsInput is what a settings save posts.
type SettingsInput struct {
	LockedUpTo     sql.Null[types.Date]
	DefaultPOTerms string
}

// GetSettings returns the settings to an Administrator.
func (k *Kit) GetSettings(ctx context.Context, p auth.Principal) (Settings, error) {
	var s Settings
	err := k.d.DB.Read(ctx, func(tx *db.Tx) error {
		if err := k.checkAdmin(ctx, tx, p); err != nil {
			return err
		}
		var err error
		s, err = readSettings(ctx, tx)
		return err
	})
	return s, err
}

// SaveSettings saves in at the version the page posted (CD-94) and audits the
// changed fields. Only an Administrator may; a stale version is refused. A
// save that changes nothing writes nothing (company's rule, CD-208).
func (k *Kit) SaveSettings(ctx context.Context, p auth.Principal, version int, in SettingsInput) error {
	return k.d.DB.Write(ctx, func(tx *db.Tx) error {
		if err := k.checkAdmin(ctx, tx, p); err != nil {
			return err
		}
		if utf8.RuneCountInString(in.DefaultPOTerms) > termsMaxLen {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgTermsTooLong,
				Fields: []doc.FieldError{{Field: "default_po_terms", Message: msgTermsTooLong}}}
		}
		before, err := readSettings(ctx, tx)
		if err != nil {
			return err
		}
		lock := isoOrBlank(in.LockedUpTo)
		// An unchanged save writes nothing: no version bump, no audit (CD-208).
		if lock == isoOrBlank(before.LockedUpTo) && in.DefaultPOTerms == before.DefaultPOTerms {
			if version != before.Version {
				return errStale()
			}
			return nil
		}
		n, err := dbq.New(tx.SQL()).UpdateSettings(ctx, dbq.UpdateSettingsParams{
			LockedUpTo:     sql.NullString{String: lock, Valid: in.LockedUpTo.Valid},
			DefaultPoTerms: in.DefaultPOTerms,
			UpdatedAt:      types.TimestampOf(k.d.Clock.Now()).String(),
			UpdatedBy:      sql.NullInt64{Int64: int64(p.ID), Valid: true},
			Version:        int64(version),
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return errStale()
		}
		var changes []audit.Change
		for _, c := range []audit.Change{
			{Field: "locked_up_to", Before: isoOrBlank(before.LockedUpTo), After: lock},
			{Field: "default_po_terms", Before: before.DefaultPOTerms, After: in.DefaultPOTerms},
		} {
			if c.Before != c.After {
				c.Op = audit.OpSet
				changes = append(changes, c)
			}
		}
		return k.d.Audit.Append(ctx, tx, audit.Entry{
			DocType: settingsDocType, DocID: 1, Action: "save", Changes: changes, By: p.ID, At: k.d.Clock.Now(),
		})
	})
}

// checkAdmin refuses anyone AUTH doesn't hold as an Administrator, read in
// tx (CD-213).
func (k *Kit) checkAdmin(ctx context.Context, tx *db.Tx, p auth.Principal) error {
	ok, err := k.d.Auth.HasRoleTx(ctx, tx, p.ID, auth.RoleAdministrator)
	if err != nil {
		return err
	}
	if !ok {
		return auth.Forbidden(msgSettingsAdminOnly)
	}
	return nil
}

// readSettings reads the row in tx.
func readSettings(ctx context.Context, tx *db.Tx) (Settings, error) {
	row, err := dbq.New(tx.SQL()).GetSettings(ctx)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{DefaultPOTerms: row.DefaultPoTerms, Version: int(row.Version)}
	if row.LockedUpTo.Valid {
		d, err := types.DateFromISO(row.LockedUpTo.String)
		if err != nil {
			return Settings{}, err
		}
		s.LockedUpTo = sql.Null[types.Date]{V: d, Valid: true}
	}
	return s, nil
}

// errStale is DOC's stale refusal (409) for the settings (CD-94).
func errStale() error { return &doc.Error{Kind: doc.KindStale, Message: msgSettingsStale} }

// isoOrBlank is d's stored form, or "" when blank.
func isoOrBlank(d sql.Null[types.Date]) string {
	if !d.Valid {
		return ""
	}
	return d.V.ISO()
}
