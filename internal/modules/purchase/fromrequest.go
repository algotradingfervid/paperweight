package purchase

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/vardhan/internaltools/internal/kernel/auth"
	"github.com/vardhan/internaltools/internal/kernel/doc"
	"github.com/vardhan/internaltools/internal/kernel/doctype"
	"github.com/vardhan/internaltools/internal/platform/db"
	"github.com/vardhan/internaltools/internal/types"
)

// CreateFromRequest creates p's PO draft for vendorID from the pending rows of
// request requestID, posted at version, and returns the new PO's id (PO spec
// §20.4 item 1). In this order (rights, then version, then vendor, CD-259):
// load and read the request (missing or hidden is AUTH's 404, CD-245), create
// on the PO, the version (DOC's stale text), a blank vendor, then the rows
// with pending to order above 0 (none is §20.4's text). The request is read
// before create's own transaction, so checkRequestRows and DOC's LinkSpec
// check inside create are the backstop for a request that changed in between.
// Create runs through Engine.Do with the vendor, so Validate fills the last
// price and its discount; its refusals pass through.
func (s *Service) CreateFromRequest(ctx context.Context, p auth.Principal, requestID int64, version int, vendorID int64) (poID int64, err error) {
	poID, _, err = s.CreateFromRequestWithSource(ctx, p, requestID, version, vendorID)
	return poID, err
}

// CreateFromRequestWithSource is CreateFromRequest, also returning the
// request's display for the caller's message (as CopyWithSource does).
func (s *Service) CreateFromRequestWithSource(ctx context.Context, p auth.Principal, requestID int64, version int, vendorID int64) (poID int64, source string, err error) {
	var in doc.ActionInput
	err = s.d.DB.Read(ctx, func(tx *db.Tx) error {
		info, err := s.fromRequestRights(ctx, tx, p, requestID)
		if err != nil {
			return err
		}
		source = info.Display
		req, err := doc.LoadDoc(ctx, tx, RequestDocType, requestID)
		if err != nil {
			return err
		}
		if req.Version != version {
			return &doc.Error{Kind: doc.KindStale, Message: fmt.Sprintf(msgFromRequestStale, info.Display)}
		}
		if vendorID == 0 {
			return &doc.Error{Kind: doc.KindInvalid, Message: msgFromRequestNoVendor,
				Fields: []doc.FieldError{{Field: "vendor_id", Message: msgFromRequestNoVendor}}}
		}
		ids := make([]int64, len(req.Lines))
		for i, l := range req.Lines {
			ids[i] = l.ID
		}
		pending, err := PendingToOrder(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, l := range req.Lines {
			if pending[l.ID] <= 0 {
				continue
			}
			in.Lines = append(in.Lines, map[string]string{
				"item_id": plainOf(l.Values["item_id"]), "description": plainOf(l.Values["description"]),
				"uom_id": plainOf(l.Values["uom_id"]), "qty": pending[l.ID].Plain(),
				"request_line_id": strconv.FormatInt(l.ID, 10),
				// Posted blank, not left out: DOC fills Default ("0") for an
				// absent key, and the last price's discount applies only
				// while the discount is blank (lineDefaults).
				"discount_pct": "",
			})
		}
		if len(in.Lines) == 0 {
			return &doc.Error{Kind: doc.KindInvalid, Message: fmt.Sprintf(msgFromRequestNothing, info.Display)}
		}
		in.Values = map[string]string{
			"vendor_id": strconv.FormatInt(vendorID, 10), "site_id": plainOf(req.Header["site_id"]),
			"project_id": plainOf(req.Header["project_id"]), "required_by": plainOf(req.Header["required_by"]),
		}
		return nil
	})
	if err != nil {
		return 0, "", err
	}
	res, err := s.d.Engine.Do(ctx, p, DocType, 0, doctype.ActionSave, in)
	if err != nil {
		return 0, "", err
	}
	return res.ID, source, nil
}

// fromRequestRights loads request requestID's info and checks, in this
// order, that p may read it (missing or hidden is AUTH's 404, CD-245) and
// create a PO (CD-259).
func (s *Service) fromRequestRights(ctx context.Context, tx *db.Tx, p auth.Principal, requestID int64) (doctype.Info, error) {
	info, err := s.d.Engine.Info(ctx, tx, RequestDocType, requestID)
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
	if err := s.d.Auth.CanTx(ctx, tx, p, RequestDocType, auth.RightRead, ref); err != nil {
		return doctype.Info{}, err
	}
	return info, s.d.Auth.CanTx(ctx, tx, p, DocType, auth.RightCreate, nil)
}

// plainOf is a stored link, text or date value as a posted form value; nil
// is blank.
func plainOf(v any) string {
	switch x := v.(type) {
	case int64:
		return strconv.FormatInt(x, 10)
	case string:
		return x
	case types.Date:
		return x.ISO()
	}
	return ""
}
