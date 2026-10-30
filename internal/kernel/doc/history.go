package doc

import (
	"context"

	"github.com/vardhan/internaltools/internal/kernel/audit"
	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/platform/db"
)

// History returns the audit entries of one document, oldest first. It needs
// the read right on the record: AUTH's errors pass through unchanged.
func History(ctx context.Context, d *db.DB, a auth.Service, p auth.Principal, doctype string, id int64) ([]audit.Entry, error) {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return nil, errGone()
	}
	var out []audit.Entry
	err := d.Read(ctx, func(tx *db.Tx) error {
		rec, err := loadDoc(ctx, tx, dt, id)
		if err != nil {
			return err
		}
		if err := a.CanTx(ctx, tx, p, dt.Name, auth.RightRead, recordRef(dt, rec)); err != nil {
			return err
		}
		out, err = audit.ForDoc(ctx, tx, dt.Name, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// DeletedHistory returns the audit entries of a deleted document, oldest
// first. Only an Administrator may read them.
func DeletedHistory(ctx context.Context, d *db.DB, a auth.Service, p auth.Principal, doctype string, id int64) ([]audit.Entry, error) {
	dt, ok := Lookup(doctype)
	if !ok || !dt.IsDocument() {
		return nil, errGone()
	}
	admin, err := a.HasRole(ctx, p.ID, auth.RoleAdministrator)
	if err != nil {
		return nil, err
	}
	if !admin {
		return nil, auth.ErrForbidden
	}
	var out []audit.Entry
	err = d.Read(ctx, func(tx *db.Tx) (err error) {
		out, err = audit.ForDoc(ctx, tx, doctype, id)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
